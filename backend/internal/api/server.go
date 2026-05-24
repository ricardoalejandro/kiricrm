package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	fiberRecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/formula"
	googleclient "github.com/naperu/clarin/internal/google"
	"github.com/naperu/clarin/internal/kommo"
	"github.com/naperu/clarin/internal/repository"
	"github.com/naperu/clarin/internal/service"
	"github.com/naperu/clarin/internal/storage"
	"github.com/naperu/clarin/internal/whatsapp"
	"github.com/naperu/clarin/internal/ws"
	"github.com/naperu/clarin/pkg/cache"
	"github.com/naperu/clarin/pkg/config"
	"github.com/naperu/clarin/pkg/database"
	"golang.org/x/crypto/bcrypt"
)

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

type Server struct {
	app          *fiber.App
	cfg          *config.Config
	services     *service.Services
	repos        *repository.Repositories
	hub          *ws.Hub
	pool         *whatsapp.DevicePool
	storage      *storage.Storage
	kommoSync    *kommo.SyncService
	kommoManager *kommo.Manager
	cache        *cache.Cache
	googleClient *googleclient.Client
	version      string
	changelog    string
}

func NewServer(cfg *config.Config, services *service.Services, repos *repository.Repositories, hub *ws.Hub, pool *whatsapp.DevicePool, store *storage.Storage, kommoSyncSvc *kommo.SyncService, kommoManager *kommo.Manager, c *cache.Cache, gc *googleclient.Client, version string) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "Clarin CRM",
		BodyLimit:             32 * 1024 * 1024, // 32MB max upload
		DisableStartupMessage: false,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"error":   err.Error(),
			})
		},
	})

	// Middleware
	app.Use(fiberRecover.New())
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} ${path}\n",
		TimeFormat: "15:04:05",
	}))

	// Security Headers (Helmet)
	app.Use(helmet.New(helmet.Config{
		XSSProtection:             "1; mode=block",
		ContentTypeNosniff:        "nosniff",
		XFrameOptions:             "DENY",
		ReferrerPolicy:            "strict-origin-when-cross-origin",
		CrossOriginEmbedderPolicy: "require-corp",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
		PermissionPolicy:          "geolocation=(), microphone=(), camera=()",
		ContentSecurityPolicy:     "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; connect-src 'self' wss: https:; font-src 'self' data:; frame-ancestors 'none'",
	}))

	// Rate Limiting - 500 requests per minute per IP (skip media file serving)
	app.Use(limiter.New(limiter.Config{
		Max:        500,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"success": false,
				"error":   "too many requests, please slow down",
			})
		},
		SkipFailedRequests:     false,
		SkipSuccessfulRequests: false,
		Next: func(c *fiber.Ctx) bool {
			// Skip rate limiting for media file endpoints and websocket
			path := c.Path()
			return strings.HasPrefix(path, "/api/media/file/") || strings.HasPrefix(path, "/ws")
		},
	}))

	// CORS Configuration
	corsOrigins := "http://localhost:3000,http://localhost:8080"
	if cfg.IsProduction() && len(cfg.CORSOrigins) > 0 {
		corsOrigins = strings.Join(cfg.CORSOrigins, ",")
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization,Upgrade,Connection",
		AllowCredentials: true,
	}))

	// Load changelog content from file (copied during deploy)
	changelogContent := ""
	for _, path := range []string{"CHANGELOG.md", "/app/CHANGELOG.md"} {
		if data, err := os.ReadFile(path); err == nil {
			changelogContent = string(data)
			break
		}
	}

	server := &Server{
		app:          app,
		cfg:          cfg,
		services:     services,
		repos:        repos,
		hub:          hub,
		pool:         pool,
		storage:      store,
		kommoSync:    kommoSyncSvc,
		kommoManager: kommoManager,
		cache:        c,
		googleClient: gc,
		version:      version,
		changelog:    changelogContent,
	}

	// Version header middleware — adds X-Clarin-Version to all API responses
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Clarin-Version", server.version)
		return c.Next()
	})

	server.setupRoutes()

	// Broadcast version update to all connected clients after a short delay
	// This covers the case where users are connected when a new version is deployed
	if hub != nil {
		go func() {
			time.Sleep(15 * time.Second)
			hub.BroadcastToAll(ws.EventVersionUpdate, map[string]interface{}{
				"version": version,
			})
		}()
	}

	return server
}

func (s *Server) kommoForAccount(ctx context.Context, accountID uuid.UUID) *kommo.SyncService {
	if s.kommoManager != nil {
		if svc := s.kommoManager.ForAccount(ctx, accountID); svc != nil {
			return svc
		}
	}
	return s.kommoSync
}

func (s *Server) kommoForWebhook(secret string) *kommo.SyncService {
	if s.kommoManager != nil {
		if svc := s.kommoManager.ForWebhook(secret); svc != nil {
			return svc
		}
	}
	if s.kommoSync != nil && s.kommoSync.WebhookSecret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(s.kommoSync.WebhookSecret)) == 1 {
		return s.kommoSync
	}
	return nil
}

func (s *Server) defaultKommoSync() *kommo.SyncService {
	if s.kommoManager != nil {
		if svc := s.kommoManager.Primary(); svc != nil {
			return svc
		}
	}
	return s.kommoSync
}

func (s *Server) setupRoutes() {
	// Health check — deep health probe checking all dependencies
	s.app.Get("/health", s.handleHealthCheck)

	// API routes
	api := s.app.Group("/api")

	// Version endpoint — public, returns version info and changelog
	api.Get("/version", s.handleGetVersion)
	api.Get("/public/plans", s.handleListPlans)

	// Device health endpoint (protected) — detailed per-device metrics
	// Registered after auth middleware setup below

	// Media proxy - public access for displaying images/videos in chat
	// MUST be registered before protected group to avoid auth middleware
	api.Get("/media/file/*", s.handleMediaProxy)

	// Public survey routes (no auth required)
	api.Get("/public/surveys/:slug", s.handleGetPublicSurvey)
	api.Post("/public/surveys/:slug/submit", s.handleSubmitSurveyResponse)
	api.Post("/public/surveys/:slug/upload", s.handleUploadSurveyFile)

	// Public dynamic routes (no auth required)
	// Order matters: specific paths before the catch-all :slug
	api.Get("/public/dynamics/check-registration", s.handleCheckRegistration)
	api.Post("/public/dynamics/send-whatsapp", s.handleSendDynamicWhatsApp)
	api.Post("/public/dynamics/register", s.handleRegisterOnLink)
	api.Post("/public/dynamics/share", s.handleShareOnLink)
	api.Get("/public/dynamics/:slug", s.handleGetPublicDynamic)

	// Auth routes (no auth required)
	auth := api.Group("/auth")
	auth.Post("/login", s.handleLogin)
	auth.Post("/register", s.handleRegister)
	auth.Post("/refresh", s.handleRefreshToken)

	// Kommo webhook (public — called by Kommo, secret in URL for validation)
	api.Post("/kommo/webhook/:secret", s.handleKommoWebhook)

	// WhatsApp Cloud API webhook (public — verification token in env, device resolved by phone_number_id)
	api.Get("/whatsapp/cloud/webhook", s.handleWhatsAppCloudVerify)
	api.Post("/whatsapp/cloud/webhook", s.handleWhatsAppCloudWebhook)

	// Protected routes
	protected := api.Group("", s.authMiddleware)

	// User routes
	protected.Get("/me", s.handleGetMe)
	protected.Get("/me/accounts", s.handleGetMyAccounts)
	protected.Post("/auth/logout", s.handleLogout)
	protected.Post("/auth/switch-account", s.handleSwitchAccount)

	// Settings routes
	protected.Get("/plans", s.handleListPlans)
	protected.Get("/subscription", s.handleGetSubscription)
	protected.Get("/settings", s.handleGetSettings)
	protected.Put("/settings/profile", s.handleUpdateProfile)
	protected.Put("/settings/account", s.handleUpdateAccount)
	protected.Get("/storage/usage", s.handleGetStorageUsage)
	protected.Get("/storage/files", s.handleListStorageFiles)
	protected.Delete("/storage/files", s.handleDeleteStorageFiles)
	protected.Post("/storage/dedupe", s.handleStartStorageDedupe)
	protected.Get("/storage/dedupe/:id", s.handleGetStorageDedupeJob)
	protected.Put("/settings/password", s.handleChangePassword)
	protected.Put("/settings/incoming-stage", s.handleSetIncomingStage)

	// Account users — any authenticated user can list users in their account (for assignment dropdowns)
	protected.Get("/account/users", s.handleGetAccountUsers)

	// API Key management routes
	protected.Post("/settings/api-keys", s.handleCreateAPIKey)
	protected.Get("/settings/api-keys", s.handleListAPIKeys)
	protected.Delete("/settings/api-keys/:id", s.handleDeleteAPIKey)

	protected.Use(s.subscriptionAccessMiddleware)

	// Device routes
	// GET /devices — list available devices for sending; accessible by any authenticated user
	// (needed by chats, contacts, leads, broadcasts, events, programs pages to populate device pickers)
	protected.Get("/devices", s.handleGetDevices)
	// Device management — requires PermDevices (add, edit, delete, connect, disconnect)
	devices := protected.Group("/devices", s.requirePermission(domain.PermDevices))
	devices.Post("/", s.handleCreateDevice)
	devices.Get("/:id", s.handleGetDevice)
	devices.Put("/:id", s.handleUpdateDevice)
	devices.Post("/:id/connect", s.handleConnectDevice)
	devices.Post("/:id/disconnect", s.handleDisconnectDevice)
	devices.Post("/:id/reset", s.handleResetDevice)
	devices.Delete("/:id", s.handleDeleteDevice)
	devices.Get("/health/all", s.handleDeviceHealth)

	// Chat routes
	chats := protected.Group("/chats", s.requirePermission(domain.PermChats))
	chats.Get("/", s.handleGetChats)
	chats.Get("/find-by-phone/:phone", s.handleFindChatByPhone)
	chats.Post("/new", s.handleCreateNewChat)
	chats.Delete("/batch", s.handleDeleteChatsBatch)
	chats.Get("/:id", s.handleGetChatDetails)
	chats.Get("/:id/messages", s.handleGetMessages)
	chats.Post("/:id/read", s.handleMarkAsRead)
	chats.Post("/:id/sync-history", s.handleRequestHistorySync)
	chats.Delete("/:id", s.handleDeleteChat)

	// Message routes
	messages := protected.Group("/messages", s.requirePermission(domain.PermChats))
	messages.Post("/send", s.handleSendMessage)
	messages.Post("/send-contact", s.handleSendContact)
	messages.Post("/forward", s.handleForwardMessage)
	messages.Post("/react", s.handleSendReaction)
	messages.Post("/poll", s.handleSendPoll)

	messages.Post("/typing", s.handleSendTyping)
	messages.Post("/read-receipt", s.handleSendReadReceipt)
	messages.Post("/delete", s.handleDeleteMessage)
	messages.Post("/edit", s.handleEditMessage)

	// WhatsApp utilities
	protected.Post("/contacts/check-whatsapp", s.requirePermission(domain.PermChats), s.handleCheckWhatsApp)

	// Sticker routes
	protected.Get("/stickers/recent", s.handleGetRecentStickers)
	protected.Get("/stickers/saved", s.handleGetSavedStickers)
	protected.Post("/stickers/saved", s.handleSaveSticker)
	protected.Delete("/stickers/saved", s.handleDeleteSavedSticker)

	// Media routes (upload requires auth)
	media := protected.Group("/media")
	media.Get("/upload-url", s.handleGetUploadURL)
	media.Post("/upload", s.handleDirectUpload)

	// Lead routes
	leads := protected.Group("/leads", s.requirePermission(domain.PermLeads))
	leads.Get("/", s.handleGetLeads)
	leads.Get("/paginated", s.handleGetLeadsPaginated)
	leads.Get("/list-paginated", s.handleGetLeadsListPaginated)
	leads.Get("/counts", s.handleGetLeadCounts)
	leads.Get("/by-stage/:stageId", s.handleGetLeadsByStage)
	leads.Post("/", s.handleCreateLead)
	leads.Delete("/batch", s.handleDeleteLeadsBatch)
	leads.Post("/observations/batch", s.handleBatchLeadObservations)
	leads.Patch("/batch/archive", s.handleArchiveLeadsBatch)
	leads.Patch("/batch/block", s.handleBlockLeadsBatch)
	leads.Get("/:id", s.handleGetLead)
	leads.Put("/:id", s.handleUpdateLead)
	leads.Delete("/:id", s.handleDeleteLead)
	leads.Patch("/:id/status", s.handleUpdateLeadStatus)
	leads.Patch("/:id/stage", s.handleUpdateLeadStage)
	leads.Get("/:id/interactions", s.handleGetLeadInteractions)
	leads.Post("/:id/sync-kommo", s.requirePlanFeature("kommo_sync"), s.handleSyncLeadFromKommo)
	leads.Patch("/:id/archive", s.handleArchiveLead)
	leads.Patch("/:id/block", s.handleBlockLead)

	// Pipeline routes
	pipelines := protected.Group("/pipelines", s.requirePermission(domain.PermLeads))
	pipelines.Get("/", s.handleGetPipelines)
	pipelines.Post("/", s.handleCreatePipeline)
	pipelines.Put("/:id", s.handleUpdatePipeline)
	pipelines.Delete("/:id", s.handleDeletePipeline)
	pipelines.Post("/:id/stages", s.handleCreatePipelineStage)
	pipelines.Put("/:id/stages/reorder", s.handleReorderPipelineStages)
	pipelines.Put("/:id/stages/:stageId", s.handleUpdatePipelineStage)
	pipelines.Delete("/:id/stages/:stageId", s.handleDeletePipelineStage)

	// Tag routes
	tags := protected.Group("/tags", s.requirePermission(domain.PermTags))
	tags.Get("/", s.handleGetTags)
	tags.Post("/", s.handleCreateTag)
	tags.Put("/:id", s.handleUpdateTag)
	tags.Delete("/batch", s.handleDeleteTagsBatch)
	tags.Delete("/:id", s.handleDeleteTag)
	tags.Post("/assign", s.handleAssignTag)
	tags.Post("/remove", s.handleRemoveTag)
	tags.Get("/entity/:type/:id", s.handleGetEntityTags)

	// Campaign routes
	campaigns := protected.Group("/campaigns", s.requirePermission(domain.PermBroadcasts), s.requirePlanFeature("broadcasts"))
	campaigns.Get("/", s.handleGetCampaigns)
	campaigns.Post("/", s.handleCreateCampaign)
	campaigns.Get("/:id", s.handleGetCampaign)

	// Program routes
	programs := protected.Group("/programs", s.requirePermission(domain.PermPrograms))
	programs.Get("/", s.handleListPrograms)
	programs.Post("/", s.handleCreateProgram)
	// Folder routes — must be declared BEFORE /:id to avoid param collision
	programs.Get("/folders", s.handleGetProgramFolders)
	programs.Post("/folders", s.handleCreateProgramFolder)
	programs.Put("/folders/:fid", s.handleUpdateProgramFolder)
	programs.Delete("/folders/:fid", s.handleDeleteProgramFolder)
	programs.Get("/:id", s.handleGetProgram)
	programs.Put("/:id", s.handleUpdateProgram)
	programs.Delete("/:id", s.handleDeleteProgram)
	programs.Patch("/:id/move-folder", s.handleMoveProgramToFolder)
	programs.Get("/:id/attendance-stats", s.handleGetAttendanceStats)

	programs.Get("/:id/participants", s.handleListParticipants)
	programs.Post("/:id/participants", s.handleAddParticipant)
	programs.Delete("/:id/participants/:participantId", s.handleRemoveParticipant)
	programs.Patch("/:id/participants/:participantId/stage", s.handleUpdateProgramParticipantStage)

	programs.Get("/:id/sessions", s.handleListSessions)
	programs.Post("/:id/sessions", s.handleCreateSession)
	programs.Put("/:id/sessions/:sessionId", s.handleUpdateSession)
	programs.Delete("/:id/sessions/:sessionId", s.handleDeleteSession)

	programs.Get("/:id/sessions/:sessionId/attendance", s.handleGetAttendance)
	programs.Post("/:id/sessions/:sessionId/attendance", s.handleMarkAttendance)
	programs.Post("/:id/sessions/:sessionId/attendance/batch", s.handleBatchMarkAttendance)
	programs.Get("/:id/sessions/:sessionId/attendance/filter", s.handleGetParticipantsByAttendanceStatus)
	programs.Post("/:id/sessions/generate", s.handleGenerateSessions)
	programs.Post("/:id/campaign", s.handleCreateCampaignFromProgram)
	campaigns.Put("/:id", s.handleUpdateCampaign)
	campaigns.Delete("/:id", s.handleDeleteCampaign)
	campaigns.Post("/batch-delete", s.handleBatchDeleteCampaigns)
	campaigns.Post("/:id/recipients", s.handleAddCampaignRecipients)
	campaigns.Post("/:id/recipients/from-leads", s.handleAddCampaignRecipientsFromLeads)
	campaigns.Get("/:id/recipients", s.handleGetCampaignRecipients)
	campaigns.Delete("/:id/recipients/:rid", s.handleDeleteCampaignRecipient)
	campaigns.Put("/:id/recipients/:rid", s.handleUpdateCampaignRecipient)
	campaigns.Post("/:id/start", s.handleStartCampaign)
	campaigns.Post("/:id/pause", s.handlePauseCampaign)
	campaigns.Post("/:id/cancel", s.handleCancelCampaign)
	campaigns.Post("/:id/duplicate", s.handleDuplicateCampaign)
	campaigns.Post("/:id/recipients/:rid/retry", s.handleRetryCampaignRecipient)
	campaigns.Put("/:id/attachments", s.handleUpdateCampaignAttachments)

	// Import CSV route
	protected.Post("/import/csv/preview", s.handlePreviewImportCSV)
	protected.Post("/import/csv", s.handleImportCSV)

	// Contact routes
	contacts := protected.Group("/contacts", s.requirePermission(domain.PermContacts))
	contacts.Get("/", s.handleGetContacts)
	contacts.Post("/", s.handleCreateContact)
	contacts.Post("/bulk", s.handleCreateContactsBulk)
	contacts.Get("/duplicates", s.handleGetContactDuplicates)
	contacts.Get("/lead-duplicates", s.handleGetContactLeadDuplicates)
	contacts.Post("/merge", s.handleMergeContacts)
	contacts.Delete("/batch", s.handleDeleteContactsBatch)
	contacts.Get("/:id", s.handleGetContact)
	contacts.Get("/:id/leads", s.handleGetContactLeads)
	contacts.Put("/:id", s.handleUpdateContact)
	contacts.Post("/:id/reset", s.handleResetContactFromDevice)
	contacts.Post("/:id/sync-kommo", s.requirePlanFeature("kommo_sync"), s.handleSyncContactFromKommo)
	contacts.Delete("/:id", s.handleDeleteContact)

	// Custom field value routes (under contacts, all authenticated users)
	contacts.Get("/:id/custom-fields", s.handleGetCustomFieldValues)
	contacts.Put("/:id/custom-fields", s.handleBatchUpsertCustomFieldValues)
	contacts.Put("/:id/custom-fields/:fieldId", s.handleUpsertCustomFieldValue)

	// Custom field definition routes (read: all authenticated, write: admin)
	customFields := protected.Group("/custom-fields", s.requirePermission(domain.PermSettings))
	customFields.Get("/", s.handleGetCustomFieldDefinitions)
	customFields.Post("/", s.handleCreateCustomFieldDefinition)
	customFields.Put("/reorder", s.handleReorderCustomFieldDefinitions)
	customFields.Put("/:id", s.handleUpdateCustomFieldDefinition)
	customFields.Delete("/:id", s.handleDeleteCustomFieldDefinition)

	// Sync contacts route (under devices)
	devices.Post("/:id/sync-contacts", s.handleSyncDeviceContacts)

	// People unified search (contacts + leads)
	protected.Get("/people/search", s.handleSearchPeople)

	// Event routes
	events := protected.Group("/events", s.requirePermission(domain.PermEvents))
	events.Get("/", s.handleGetEvents)
	events.Post("/", s.handleCreateEvent)
	events.Post("/from-leads", s.handleCreateEventFromLeads)
	events.Get("/upcoming-actions", s.handleGetUpcomingActions)
	// Pipeline routes
	events.Get("/pipelines", s.handleGetEventPipelines)
	events.Post("/pipelines", s.handleCreateEventPipeline)
	events.Get("/pipelines/:pid", s.handleGetEventPipeline)
	events.Put("/pipelines/:pid", s.handleUpdateEventPipeline)
	events.Delete("/pipelines/:pid", s.handleDeleteEventPipeline)
	events.Put("/pipelines/:pid/stages", s.handleReplaceEventPipelineStages)
	// Folder routes — must be declared BEFORE /:id to avoid param collision
	events.Get("/folders", s.handleGetEventFolders)
	events.Post("/folders", s.handleCreateEventFolder)
	events.Put("/folders/:fid", s.handleUpdateEventFolder)
	events.Delete("/folders/:fid", s.handleDeleteEventFolder)
	events.Get("/:id", s.handleGetEvent)
	events.Put("/:id", s.handleUpdateEvent)
	events.Delete("/:id", s.handleDeleteEvent)
	events.Patch("/:id/move-folder", s.handleMoveEventToFolder)
	// Event tag auto-sync
	events.Get("/:id/tags", s.handleGetEventTags)
	events.Put("/:id/tags", s.handleSetEventTags)
	events.Post("/formula/validate", s.handleValidateFormula)
	events.Get("/:id/participants/paginated", s.handleGetEventParticipantsPaginated)
	events.Get("/:id/participants/by-stage/:stageId", s.handleGetEventParticipantsByStage)
	events.Post("/:id/participants/observations/batch", s.handleBatchParticipantObservations)
	events.Get("/:id/participants", s.handleGetEventParticipants)
	events.Post("/:id/participants", s.handleAddEventParticipant)
	events.Post("/:id/participants/bulk", s.handleBulkAddEventParticipants)
	events.Patch("/:id/participants/bulk-status", s.handleBulkUpdateEventParticipantStatus)
	events.Patch("/:id/participants/bulk-stage", s.handleBulkUpdateEventParticipantStage)
	events.Put("/:id/participants/:pid", s.handleUpdateEventParticipant)
	events.Patch("/:id/participants/:pid/status", s.handleUpdateEventParticipantStatus)
	events.Patch("/:id/participants/:pid/stage", s.handleUpdateEventParticipantStage)
	events.Delete("/:id/participants/:pid", s.handleDeleteEventParticipant)
	events.Post("/:id/participants/:pid/check-tag-impact", s.handleCheckTagImpact)
	events.Post("/:id/campaign", s.handleCreateCampaignFromEvent)

	// Event Google Contacts sync
	events.Get("/:id/google-sync-status", s.handleEventGoogleSyncStatus)
	events.Post("/:id/google-sync", s.requirePlanFeature("google_contacts"), s.handleEventGoogleSync)

	// Event Logbook (Bitácora) routes
	events.Get("/:id/logbooks", s.handleGetEventLogbooks)
	events.Post("/:id/logbooks", s.handleCreateEventLogbook)
	events.Post("/:id/logbooks/auto-create", s.handleAutoCreateLogbooks)
	events.Get("/:id/logbooks/:lid", s.handleGetEventLogbook)
	events.Put("/:id/logbooks/:lid", s.handleUpdateEventLogbook)
	events.Delete("/:id/logbooks/:lid", s.handleDeleteEventLogbook)
	events.Post("/:id/logbooks/:lid/capture", s.handleCaptureLogbookSnapshot)
	events.Get("/:id/logbooks/:lid/preview", s.handleLogbookPreview)
	events.Put("/:id/logbooks/:lid/entries/:eid", s.handleUpdateLogbookEntry)

	// Interaction routes
	interactions := protected.Group("/interactions", s.requirePermission(domain.PermLeads))
	interactions.Post("/", s.handleLogInteraction)
	interactions.Get("/", s.handleGetInteractions)
	interactions.Delete("/:id", s.handleDeleteInteraction)

	// Task routes
	tasks := protected.Group("/tasks", s.requirePermission(domain.PermTasks))
	tasks.Get("/lists", s.handleGetTaskLists)
	tasks.Post("/lists", s.handleCreateTaskList)
	tasks.Post("/lists/reorder", s.handleReorderLists)
	tasks.Put("/lists/:listId", s.handleUpdateTaskList)
	tasks.Delete("/lists/:listId", s.handleDeleteTaskList)
	tasks.Get("/calendar", s.handleGetTasksCalendar)
	tasks.Get("/stats", s.handleGetTaskStats)
	tasks.Post("/reorder", s.handleReorderTasks)
	tasks.Post("/", s.handleCreateTask)
	tasks.Get("/", s.handleGetTasks)
	tasks.Get("/:id", s.handleGetTask)
	tasks.Put("/:id", s.handleUpdateTask)
	tasks.Delete("/:id", s.handleDeleteTask)
	tasks.Post("/:id/complete", s.handleCompleteTask)
	tasks.Post("/:id/star", s.handleToggleStar)
	tasks.Get("/:id/subtasks", s.handleGetSubtasks)
	tasks.Post("/:id/subtasks", s.handleCreateSubtask)
	tasks.Put("/:id/subtasks/:subId", s.handleUpdateSubtask)
	tasks.Delete("/:id/subtasks/:subId", s.handleDeleteSubtask)
	tasks.Post("/:id/subtasks/:subId/toggle", s.handleToggleSubtask)

	// Contact interactions and events
	contacts.Get("/:id/interactions", s.handleGetContactInteractions)
	contacts.Get("/:id/events", s.handleGetContactEvents)

	// Document template routes
	docTemplates := protected.Group("/document-templates", s.requirePermission(domain.PermDocuments))
	docTemplates.Get("/", s.handleListDocumentTemplates)
	docTemplates.Post("/", s.handleCreateDocumentTemplate)
	docTemplates.Post("/import", s.handleImportDocumentTemplate)
	docTemplates.Get("/:id", s.handleGetDocumentTemplate)
	docTemplates.Put("/:id", s.handleUpdateDocumentTemplate)
	docTemplates.Delete("/:id", s.handleDeleteDocumentTemplate)
	docTemplates.Post("/:id/duplicate", s.handleDuplicateDocumentTemplate)

	// Quick replies (canned responses)
	quickReplies := protected.Group("/quick-replies", s.requirePermission(domain.PermChats))
	quickReplies.Get("/", s.handleGetQuickReplies)
	quickReplies.Post("/", s.handleCreateQuickReply)
	quickReplies.Put("/:id", s.handleUpdateQuickReply)
	quickReplies.Delete("/:id", s.handleDeleteQuickReply)

	// Legacy per-account Kommo configuration routes are disabled. Kommo is now
	// administered centrally through /admin/integrations and assigned to account groups.
	kommoGroup := protected.Group("/kommo")
	kommoGroup.All("/", s.handleKommoLegacyDisabled)
	kommoGroup.All("/*", s.handleKommoLegacyDisabled)

	// Google Contacts integration routes
	googleGroup := protected.Group("/google", s.requirePermission(domain.PermIntegrations))
	googleGroup.Get("/auth-url", s.handleGoogleAuthURL)
	googleGroup.Delete("/disconnect", s.handleGoogleDisconnect)
	googleGroup.Get("/status", s.handleGoogleStatus)
	// Google callback (public — called by Google redirect, state carries accountID)
	api.Get("/google/callback", s.handleGoogleCallback)
	// Google Contacts sync routes (requires contacts permission)
	googleContacts := protected.Group("/google/contacts", s.requirePermission(domain.PermContacts), s.requirePlanFeature("google_contacts"))
	googleContacts.Post("/:id/sync", s.handleGoogleSyncContact)
	googleContacts.Delete("/:id/sync", s.handleGoogleDesyncContact)
	googleContacts.Post("/batch/sync", s.handleGoogleBatchSync)
	googleContacts.Post("/batch/desync", s.handleGoogleBatchDesync)
	googleContacts.Post("/batch/sync-from-leads", s.handleGoogleBatchSyncFromLeads)
	googleContacts.Post("/batch/desync-from-leads", s.handleGoogleBatchDesyncFromLeads)

	// WhatsApp Cloud API administration (configuration/audit only; outbound is guarded)
	whatsappAPI := protected.Group("/whatsapp-api", s.requirePermission(domain.PermIntegrations))
	whatsappAPI.Get("/overview", s.handleWhatsAppAPIOverview)
	whatsappAPI.Get("/templates", s.handleListWhatsAppTemplates)
	whatsappAPI.Post("/templates", s.handleCreateWhatsAppTemplate)
	whatsappAPI.Put("/templates/:id", s.handleUpdateWhatsAppTemplate)
	whatsappAPI.Delete("/templates/:id", s.handleDeleteWhatsAppTemplate)
	whatsappAPI.Get("/webhook-events", s.handleListWhatsAppWebhookEvents)
	whatsappAPI.Get("/windows", s.handleListWhatsAppWindows)

	// Chat bots v1 — administrable and simulable, no automatic paid sends in this phase
	bots := protected.Group("/bots", s.requirePermission(domain.PermBots))
	bots.Get("/", s.handleListBots)
	bots.Post("/", s.handleCreateBot)
	bots.Get("/:id", s.handleGetBot)
	bots.Put("/:id", s.handleUpdateBot)
	bots.Delete("/:id", s.handleDeleteBot)
	bots.Post("/:id/publish", s.handlePublishBot)
	bots.Post("/:id/simulate", s.handleSimulateBot)
	bots.Get("/:id/logs", s.handleListBotLogs)

	// Automation routes
	automations := protected.Group("/automations", s.requirePermission(domain.PermAutomations), s.requirePlanFeature("automations"))
	automations.Get("/", s.handleListAutomations)
	automations.Post("/", s.handleCreateAutomation)
	automations.Get("/:id", s.handleGetAutomation)
	automations.Put("/:id", s.handleUpdateAutomation)
	automations.Delete("/:id", s.handleDeleteAutomation)
	automations.Patch("/:id/toggle", s.handleToggleAutomation)
	automations.Post("/:id/trigger", s.handleTriggerAutomation)
	automations.Get("/:id/executions", s.handleGetAutomationExecutions)
	automations.Get("/:id/executions/:execId/logs", s.handleGetExecutionLogs)

	// Survey routes
	surveys := protected.Group("/surveys", s.requirePermission(domain.PermSurveys))
	surveys.Get("/", s.handleListSurveys)
	surveys.Post("/", s.handleCreateSurvey)
	surveys.Post("/check-slug", s.handleCheckSurveySlug)
	surveys.Get("/:id", s.handleGetSurvey)
	surveys.Put("/:id", s.handleUpdateSurvey)
	surveys.Delete("/:id", s.handleDeleteSurvey)
	surveys.Patch("/:id/status", s.handleSetSurveyStatus)
	surveys.Post("/:id/duplicate", s.handleDuplicateSurvey)
	surveys.Get("/:id/questions", s.handleGetSurveyQuestions)
	surveys.Put("/:id/questions", s.handleSaveSurveyQuestions)
	surveys.Get("/:id/responses", s.handleListSurveyResponses)
	surveys.Get("/:id/responses/:rid", s.handleGetSurveyResponse)
	surveys.Delete("/:id/responses/:rid", s.handleDeleteSurveyResponse)
	surveys.Get("/:id/analytics", s.handleGetSurveyAnalytics)
	surveys.Get("/:id/export", s.handleExportSurveyCSV)

	// Dynamic routes
	dynamics := protected.Group("/dynamics", s.requirePermission(domain.PermDynamics))
	dynamics.Get("/", s.handleListDynamics)
	dynamics.Post("/", s.handleCreateDynamic)
	dynamics.Post("/check-slug", s.handleCheckDynamicSlug)
	dynamics.Get("/:id", s.handleGetDynamic)
	dynamics.Put("/:id", s.handleUpdateDynamic)
	dynamics.Delete("/:id", s.handleDeleteDynamic)
	dynamics.Patch("/:id/active", s.handleSetDynamicActive)
	dynamics.Get("/:id/items", s.handleListDynamicItems)
	dynamics.Post("/:id/items/bulk-delete", s.handleBulkDeleteDynamicItems)
	dynamics.Post("/:id/items", s.handleCreateDynamicItem)
	dynamics.Put("/:id/items/reorder", s.handleReorderDynamicItems)
	dynamics.Put("/:id/items/:itemId", s.handleUpdateDynamicItem)
	dynamics.Put("/:id/items/:itemId/options", s.handleSetItemOptions)
	dynamics.Post("/:id/items/bulk-assign", s.handleBulkAssignOption)
	dynamics.Delete("/:id/items/:itemId", s.handleDeleteDynamicItem)
	// Dynamic options
	dynamics.Get("/:id/options", s.handleListDynamicOptions)
	dynamics.Post("/:id/options", s.handleCreateDynamicOption)
	dynamics.Put("/:id/options/reorder", s.handleReorderDynamicOptions)
	dynamics.Put("/:id/options/:optionId", s.handleUpdateDynamicOption)
	dynamics.Delete("/:id/options/:optionId", s.handleDeleteDynamicOption)
	// Dynamic links
	dynamics.Get("/:id/links", s.handleListDynamicLinks)
	dynamics.Post("/:id/links", s.handleCreateDynamicLink)
	dynamics.Post("/:id/links/check-slug", s.handleCheckDynamicLinkSlug)
	dynamics.Put("/:id/links/:linkId", s.handleUpdateDynamicLink)
	dynamics.Delete("/:id/links/:linkId", s.handleDeleteDynamicLink)
	dynamics.Post("/:id/links/:linkId/extra-media", s.handleUploadLinkExtraMedia)
	dynamics.Delete("/:id/links/:linkId/extra-media", s.handleDeleteLinkExtraMedia)
	// Multi extra media (up to 10 per link)
	dynamics.Get("/:id/links/:linkId/media", s.handleListLinkMedia)
	dynamics.Post("/:id/links/:linkId/media", s.handleCreateLinkMedia)
	dynamics.Post("/:id/links/:linkId/media/reorder", s.handleReorderLinkMedia)
	dynamics.Patch("/:id/links/:linkId/media/:mediaId", s.handleUpdateLinkMediaCaption)
	dynamics.Delete("/:id/links/:linkId/media/:mediaId", s.handleDeleteLinkMedia)
	// Dynamic link registrations
	dynamics.Get("/:id/links/:linkId/registrations", s.handleListLinkRegistrations)
	dynamics.Get("/:id/links/:linkId/registrations/export", s.handleExportLinkRegistrations)
	dynamics.Delete("/:id/links/:linkId/registrations/:regId", s.handleDeleteLinkRegistration)

	// WebSocket route
	s.app.Use("/ws", s.wsUpgrade)
	s.app.Get("/ws", websocket.New(s.handleWebSocket))

	// Stats
	protected.Get("/stats", s.handleGetStats)

	// AI Assistant (Eros)
	protected.Get("/ai/config", s.handleGetAIConfig)
	protected.Put("/ai/config", s.handleSetAIConfig)
	protected.Post("/ai/config/validate", s.handleValidateAIConfig)
	protected.Post("/ai/models", s.handleListAIModels)
	protected.Post("/ai/chat", s.handleAIChat)
	protected.Get("/ai/conversations", s.handleListErosConversations)
	protected.Get("/ai/conversations/:id", s.handleGetErosConversation)
	protected.Delete("/ai/conversations/:id", s.handleDeleteErosConversation)

	// Super Admin routes
	admin := protected.Group("/admin", s.superAdminMiddleware)

	// Account management
	admin.Get("/plans", s.handleListPlans)
	adminAccounts := admin.Group("/accounts")
	adminAccounts.Get("/", s.handleAdminGetAccounts)
	adminAccounts.Post("/", s.handleAdminCreateAccount)
	adminAccounts.Get("/:id/subscription", s.handleAdminGetAccountSubscription)
	adminAccounts.Put("/:id/subscription", s.handleAdminUpdateAccountSubscription)
	adminAccounts.Post("/:id/extend-trial", s.handleAdminExtendTrial)
	adminAccounts.Post("/:id/suspend-subscription", s.handleAdminSuspendSubscription)
	adminAccounts.Post("/:id/reactivate-subscription", s.handleAdminReactivateSubscription)
	adminAccounts.Get("/:id", s.handleAdminGetAccount)
	adminAccounts.Put("/:id", s.handleAdminUpdateAccount)
	adminAccounts.Patch("/:id/toggle", s.handleAdminToggleAccount)
	adminAccounts.Get("/:id/purge-preview", s.handleAdminAccountPurgePreview)
	adminAccounts.Delete("/:id/purge", s.handleAdminPurgeAccount)
	adminAccounts.Delete("/:id", s.handleAdminDeleteAccount)

	// User management
	adminUsers := admin.Group("/users")
	adminUsers.Get("/", s.handleAdminGetUsers)
	adminUsers.Post("/", s.handleAdminCreateUser)
	adminUsers.Put("/:id", s.handleAdminUpdateUser)
	adminUsers.Patch("/:id/toggle", s.handleAdminToggleUser)
	adminUsers.Patch("/:id/password", s.handleAdminResetPassword)
	adminUsers.Delete("/:id", s.handleAdminDeleteUser)

	// User-Account assignments
	adminUsers.Get("/:id/accounts", s.handleAdminGetUserAccounts)
	adminUsers.Post("/:id/accounts", s.handleAdminAssignUserAccount)
	adminUsers.Delete("/:id/accounts/:account_id", s.handleAdminRemoveUserAccount)

	// Role management
	adminRoles := admin.Group("/roles")
	adminRoles.Get("/", s.handleAdminGetRoles)
	adminRoles.Post("/", s.handleAdminCreateRole)
	adminRoles.Put("/:id", s.handleAdminUpdateRole)
	adminRoles.Delete("/:id", s.handleAdminDeleteRole)

	// Integration management
	adminIntegrations := admin.Group("/integrations")
	adminIntegrations.Get("/", s.handleAdminListIntegrations)
	adminIntegrations.Post("/", s.handleAdminCreateIntegration)
	adminIntegrations.Put("/:id", s.handleAdminUpdateIntegration)
	adminIntegrations.Delete("/:id", s.handleAdminDeleteIntegration)
	adminIntegrations.Post("/:id/accounts", s.handleAdminAssignIntegrationAccount)
	adminIntegrations.Delete("/:id/accounts/:account_id", s.handleAdminRemoveIntegrationAccount)
	adminIntegrations.Post("/:id/reload", s.handleAdminReloadIntegrations)
	adminIntegrations.Get("/:id/monitor", s.handleAdminIntegrationMonitor)
	adminIntegrations.Get("/:id/health", s.handleAdminIntegrationHealth)
	adminIntegrations.Get("/:id/outbox", s.handleAdminIntegrationOutbox)
	adminIntegrations.Post("/:id/poll", s.handleAdminForceIntegrationPoll)
}

// Auth middleware
func (s *Server) authMiddleware(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		// Try cookie
		authHeader = c.Cookies("auth-token")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		// Try query param (for file downloads)
		token = c.Query("token")
	}
	if token == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"error":   "Unauthorized",
		})
	}

	claims, err := s.services.Auth.ValidateToken(token, s.cfg.JWTSecret)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid token",
		})
	}

	// Check if user sessions were invalidated (admin toggled/deleted the user)
	if s.services.Auth.IsUserSessionInvalidated(claims) {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"error":   "Session invalidated",
		})
	}

	c.Locals("claims", claims)
	c.Locals("user_id", claims.UserID)
	c.Locals("account_id", claims.AccountID)
	return c.Next()
}

// Super admin middleware
func (s *Server) superAdminMiddleware(c *fiber.Ctx) error {
	claims := c.Locals("claims").(*service.JWTClaims)
	if !claims.IsSuperAdmin {
		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"error":   "Forbidden: super admin access required",
		})
	}
	return c.Next()
}

// requirePermission returns a middleware that checks if the caller has the given module permission.
// Admins and super_admins bypass this check entirely.
func (s *Server) requirePermission(module string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*service.JWTClaims)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"success": false, "error": "Unauthorized"})
		}

		// Derive admin status from JWT role (covers old tokens with stale IsAdmin flag)
		isAdmin := claims.IsAdmin || claims.IsSuperAdmin || claims.Role == domain.RoleAdmin || claims.Role == domain.RoleSuperAdmin

		// Admins always have full access
		if isAdmin {
			return c.Next()
		}
		// Check permissions slice
		for _, p := range claims.Permissions {
			if p == domain.PermAll || p == module {
				return c.Next()
			}
		}

		// Fallback: check actual per-account role from DB (handles stale JWTs)
		var dbRole string
		err := s.repos.DB().QueryRow(c.Context(),
			`SELECT role FROM user_accounts WHERE user_id = $1 AND account_id = $2`,
			claims.UserID, claims.AccountID).Scan(&dbRole)
		if err == nil && (dbRole == domain.RoleAdmin || dbRole == domain.RoleSuperAdmin) {
			return c.Next()
		}

		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"error":   "No tienes permiso para acceder a este módulo",
		})
	}
}

// WebSocket upgrade middleware
func (s *Server) wsUpgrade(c *fiber.Ctx) error {
	if websocket.IsWebSocketUpgrade(c) {
		// Validate token from query param
		token := c.Query("token")
		if token == "" {
			return c.Status(401).JSON(fiber.Map{"error": "Missing token"})
		}

		claims, err := s.services.Auth.ValidateToken(token, s.cfg.JWTSecret)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "Invalid token"})
		}
		if !claims.IsSuperAdmin {
			decision, accessErr := s.services.Subscription.CheckAccess(c.Context(), claims.AccountID)
			if accessErr != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Subscription validation failed"})
			}
			if decision != nil && !decision.Allowed {
				return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"error": decision.Message, "code": "subscription_required"})
			}
		}

		c.Locals("claims", claims)
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

// --- Auth Handlers ---

func (s *Server) handleLogin(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	token, refreshToken, user, userAccounts, err := s.services.Auth.Login(c.Context(), req.Username, req.Password, s.cfg.JWTSecret)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Set access token cookie (short-lived, 1 hour)
	c.Cookie(&fiber.Cookie{
		Name:     "auth-token",
		Value:    token,
		Expires:  time.Now().Add(1 * time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})

	// Set refresh token cookie (long-lived, 7 days, httpOnly)
	c.Cookie(&fiber.Cookie{
		Name:     "refresh-token",
		Value:    refreshToken,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Strict",
		Path:     "/api/auth",
	})

	// Build accounts list for response
	accountsList := make([]fiber.Map, 0)
	for _, ua := range userAccounts {
		accountsList = append(accountsList, fiber.Map{
			"account_id":   ua.AccountID,
			"account_name": ua.AccountName,
			"account_slug": ua.AccountSlug,
			"role":         ua.Role,
			"is_default":   ua.IsDefault,
		})
	}

	// Build permissions for response (mirrors JWT logic)
	// Per-account admin/super_admin gets full access
	activeRole := user.Role
	for _, ua := range userAccounts {
		if ua.AccountID == user.AccountID {
			activeRole = ua.Role
			break
		}
	}
	isAdmin := user.IsAdmin || user.IsSuperAdmin || activeRole == domain.RoleAdmin || activeRole == domain.RoleSuperAdmin
	permissions := []string{domain.PermAll}
	if !isAdmin {
		for _, ua := range userAccounts {
			if ua.AccountID == user.AccountID {
				if ua.Permissions != nil {
					permissions = ua.Permissions
				} else {
					permissions = []string{}
				}
				break
			}
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"token":   token,
		"user": fiber.Map{
			"id":             user.ID,
			"username":       user.Username,
			"email":          user.Email,
			"display_name":   user.DisplayName,
			"is_admin":       isAdmin,
			"is_super_admin": user.IsSuperAdmin,
			"role":           user.Role,
			"account_id":     user.AccountID,
			"account_name":   user.AccountName,
			"permissions":    permissions,
		},
		"accounts": accountsList,
	})
}

func (s *Server) clearAuthCookies(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     "auth-token",
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})
	c.Cookie(&fiber.Cookie{
		Name:     "refresh-token",
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Strict",
		Path:     "/api/auth",
	})
}

func (s *Server) handleLogout(c *fiber.Ctx) error {
	// Revoke JWT + delete refresh token
	claims, _ := c.Locals("claims").(*service.JWTClaims)
	refreshToken := c.Cookies("refresh-token")
	if claims != nil {
		s.services.Auth.Logout(c.Context(), claims, refreshToken)
	}

	s.clearAuthCookies(c)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleRefreshToken(c *fiber.Ctx) error {
	// Get refresh token from cookie
	refreshToken := c.Cookies("refresh-token")
	if refreshToken == "" {
		s.clearAuthCookies(c)
		return c.Status(401).JSON(fiber.Map{"success": false, "error": "No refresh token"})
	}

	// Blacklist the old JWT before issuing a new one
	oldToken := c.Cookies("auth-token")
	if oldToken != "" {
		if oldClaims, err := s.services.Auth.ValidateToken(oldToken, s.cfg.JWTSecret); err == nil && oldClaims != nil {
			s.services.Auth.BlacklistJTI(oldClaims)
		}
	}

	newToken, newRefreshToken, err := s.services.Auth.RefreshToken(c.Context(), refreshToken, s.cfg.JWTSecret)
	if err != nil {
		s.clearAuthCookies(c)
		return c.Status(401).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Set new access token cookie
	c.Cookie(&fiber.Cookie{
		Name:     "auth-token",
		Value:    newToken,
		Expires:  time.Now().Add(1 * time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})

	// Set rotated refresh token cookie
	c.Cookie(&fiber.Cookie{
		Name:     "refresh-token",
		Value:    newRefreshToken,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Strict",
		Path:     "/api/auth",
	})

	return c.JSON(fiber.Map{
		"success": true,
		"token":   newToken,
	})
}

func (s *Server) handleGetMe(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)
	accountID := c.Locals("account_id").(uuid.UUID)
	user, err := s.services.Auth.GetUser(c.Context(), userID)
	if err != nil || user == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "User not found"})
	}

	// Get user's accounts
	userAccounts, _ := s.services.Auth.GetUserAccounts(c.Context(), userID)
	accountsList := make([]fiber.Map, 0)
	activeAccountName := user.AccountName
	for _, ua := range userAccounts {
		accountsList = append(accountsList, fiber.Map{
			"account_id":   ua.AccountID,
			"account_name": ua.AccountName,
			"account_slug": ua.AccountSlug,
			"role":         ua.Role,
			"is_default":   ua.IsDefault,
		})
		if ua.AccountID == accountID {
			activeAccountName = ua.AccountName
		}
	}

	// Extract permissions from JWT claims (already computed and embedded)
	claims := c.Locals("claims").(*service.JWTClaims)

	// Compute per-account admin status (same logic as login/switchAccount)
	var activeRole string
	for _, ua := range userAccounts {
		if ua.AccountID == accountID {
			activeRole = ua.Role
			break
		}
	}
	isAdmin := user.IsAdmin || user.IsSuperAdmin || activeRole == domain.RoleAdmin || activeRole == domain.RoleSuperAdmin

	// Compute permissions: admins get wildcard, agents get role-based permissions
	var permissions []string
	if isAdmin {
		permissions = []string{domain.PermAll}
	} else {
		permissions = claims.Permissions
		// If JWT permissions are empty, try fetching from DB
		if len(permissions) == 0 {
			permissions, _ = s.repos.UserAccount.GetUserPermissions(c.Context(), userID, accountID)
		}
	}

	// Check if current account has Kommo integration enabled
	var kommoEnabled bool
	_ = s.repos.DB().QueryRow(c.Context(), `SELECT COALESCE(kommo_enabled, false) FROM accounts WHERE id = $1`, accountID).Scan(&kommoEnabled)

	plan := ""
	subscriptionStatus := ""
	subscriptionIsActive := true
	subscriptionReason := ""
	var subscriptionDaysLeft *int
	var trialEndsAt *time.Time
	var currentPeriodEnd *time.Time
	var graceEndsAt *time.Time
	decision, err := s.services.Subscription.CheckAccess(c.Context(), accountID)
	if err != nil {
		log.Printf("[API] Failed to load subscription for /me account %s: %v", accountID, err)
		account, accountErr := s.services.Account.GetByID(c.Context(), accountID)
		if accountErr == nil && account != nil {
			plan = account.Plan
			subscriptionStatus = account.SubscriptionStatus
		}
	} else if decision != nil && decision.Overview != nil {
		subscriptionIsActive = decision.Allowed
		subscriptionReason = decision.Reason
		subscriptionDaysLeft = decision.Overview.DaysLeft
		if decision.Overview.Subscription != nil {
			subscriptionStatus = decision.Overview.Subscription.Status
			trialEndsAt = decision.Overview.Subscription.TrialEndsAt
			currentPeriodEnd = decision.Overview.Subscription.CurrentPeriodEnd
			graceEndsAt = decision.Overview.Subscription.GraceEndsAt
		}
		if decision.Overview.Plan != nil {
			plan = decision.Overview.Plan.Code
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"user": fiber.Map{
			"id":                     user.ID,
			"username":               user.Username,
			"email":                  user.Email,
			"display_name":           user.DisplayName,
			"is_admin":               isAdmin,
			"is_super_admin":         user.IsSuperAdmin,
			"role":                   user.Role,
			"account_id":             accountID,
			"account_name":           activeAccountName,
			"plan":                   plan,
			"subscription_status":    subscriptionStatus,
			"subscription_active":    subscriptionIsActive,
			"subscription_reason":    subscriptionReason,
			"subscription_days_left": subscriptionDaysLeft,
			"trial_ends_at":          trialEndsAt,
			"current_period_end":     currentPeriodEnd,
			"grace_ends_at":          graceEndsAt,
			"permissions":            permissions,
			"kommo_enabled":          kommoEnabled,
		},
		"accounts": accountsList,
	})
}

// --- Account Users Handler ---

func (s *Server) handleGetAccountUsers(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Query users from both users.account_id and user_accounts to include multi-account users
	rows, err := s.repos.DB().Query(c.Context(), `
		SELECT DISTINCT u.id, u.display_name, u.username, u.role, u.is_active
		FROM users u
		WHERE u.id IN (
			SELECT u2.id FROM users u2 WHERE u2.account_id = $1
			UNION
			SELECT ua.user_id FROM user_accounts ua WHERE ua.account_id = $1
		)
		ORDER BY u.display_name
	`, accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to fetch users"})
	}
	defer rows.Close()

	result := make([]fiber.Map, 0)
	for rows.Next() {
		var id uuid.UUID
		var displayName, username, role string
		var isActive bool
		if err := rows.Scan(&id, &displayName, &username, &role, &isActive); err != nil {
			continue
		}
		if !isActive {
			continue
		}
		result = append(result, fiber.Map{
			"id":           id,
			"display_name": displayName,
			"username":     username,
			"role":         role,
		})
	}

	return c.JSON(fiber.Map{"success": true, "users": result})
}

// --- Settings Handlers ---

func (s *Server) handleGetSettings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)
	accountID := c.Locals("account_id").(uuid.UUID)

	user, err := s.services.Auth.GetUser(c.Context(), userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "User not found"})
	}

	account, _ := s.services.Account.GetByID(c.Context(), accountID)

	result := fiber.Map{
		"success": true,
		"user": fiber.Map{
			"id":    user.ID,
			"name":  user.DisplayName,
			"email": user.Email,
			"role":  user.Role,
		},
	}

	if account != nil {
		result["account"] = fiber.Map{
			"id":                        account.ID,
			"name":                      account.Name,
			"slug":                      account.Slug,
			"plan":                      account.Plan,
			"storage_limit_bytes":       account.StorageLimitBytes,
			"subscription_status":       account.SubscriptionStatus,
			"trial_ends_at":             account.TrialEndsAt,
			"current_period_end":        account.CurrentPeriodEnd,
			"grace_ends_at":             account.GraceEndsAt,
			"created_at":                account.CreatedAt,
			"default_incoming_stage_id": account.DefaultIncomingStageID,
		}
	}

	return c.JSON(result)
}

func (s *Server) handleSetIncomingStage(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		StageID *string `json:"stage_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if req.StageID == nil || *req.StageID == "" {
		// Clear the setting
		_, err := s.repos.DB().Exec(c.Context(), `UPDATE accounts SET default_incoming_stage_id = NULL WHERE id = $1`, accountID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to update"})
		}
	} else {
		stageID, err := uuid.Parse(*req.StageID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid stage ID"})
		}
		// Verify stage belongs to a pipeline of this account
		var exists bool
		err = s.repos.DB().QueryRow(c.Context(), `
			SELECT EXISTS(
				SELECT 1 FROM pipeline_stages ps
				JOIN pipelines p ON p.id = ps.pipeline_id
				WHERE ps.id = $1 AND p.account_id = $2
			)
		`, stageID, accountID).Scan(&exists)
		if err != nil || !exists {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Stage not found"})
		}
		_, err = s.repos.DB().Exec(c.Context(), `UPDATE accounts SET default_incoming_stage_id = $1 WHERE id = $2`, stageID, accountID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to update"})
		}
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleUpdateProfile(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)

	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	user, err := s.services.Auth.GetUser(c.Context(), userID)
	if err != nil || user == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "User not found"})
	}

	if req.Name != "" {
		user.DisplayName = req.Name
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	if err := s.services.Account.UpdateUser(c.Context(), user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to update profile"})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleUpdateAccount(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	account, err := s.services.Account.GetByID(c.Context(), accountID)
	if err != nil || account == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Account not found"})
	}

	if req.Name != "" {
		account.Name = req.Name
	}

	if err := s.services.Account.Update(c.Context(), account); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to update account"})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleChangePassword(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if len(req.NewPassword) < 8 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "La contraseña debe tener al menos 8 caracteres"})
	}

	user, err := s.services.Auth.GetUser(c.Context(), userID)
	if err != nil || user == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "User not found"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Contraseña actual incorrecta"})
	}

	if err := s.services.Account.ResetPassword(c.Context(), userID, req.NewPassword); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to change password"})
	}

	return c.JSON(fiber.Map{"success": true})
}

// --- Device Handlers ---

func cleanDeviceString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cleanDeviceStringDefault(value *string, fallback string) *string {
	cleaned := cleanDeviceString(value)
	if cleaned != nil {
		return cleaned
	}
	return &fallback
}

func getDeviceProvider(device *domain.Device) string {
	if device == nil || device.Provider == nil || *device.Provider == "" {
		return domain.DeviceProviderWhatsAppWeb
	}
	return *device.Provider
}

func isCloudAPIDevice(device *domain.Device) bool {
	return getDeviceProvider(device) == domain.DeviceProviderWhatsAppCloudAPI
}

func (s *Server) handleGetDevices(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	devices, err := s.services.Device.GetByAccountID(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "devices": devices})
}

func (s *Server) handleCreateDevice(c *fiber.Ctx) error {
	var req struct {
		Name                string  `json:"name"`
		Provider            string  `json:"provider"`
		WABAID              *string `json:"waba_id"`
		PhoneNumberID       *string `json:"phone_number_id"`
		APIDisplayPhone     *string `json:"api_display_phone"`
		APIWebhookStatus    *string `json:"api_webhook_status"`
		APIBillingStatus    *string `json:"api_billing_status"`
		APISendingEnabled   bool    `json:"api_sending_enabled"`
		APITemplatesEnabled bool    `json:"api_templates_enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "El nombre del dispositivo es obligatorio"})
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = domain.DeviceProviderWhatsAppWeb
	}

	accountID := c.Locals("account_id").(uuid.UUID)
	if err := s.enforcePlanLimit(c.Context(), accountID, "max_devices", 1); err != nil {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_devices"})
	}
	if provider == domain.DeviceProviderWhatsAppWeb {
		device, err := s.services.Device.Create(c.Context(), accountID, req.Name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"success": true, "device": device})
	}

	if provider != domain.DeviceProviderWhatsAppCloudAPI {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Proveedor de WhatsApp no soportado"})
	}
	if req.APISendingEnabled || req.APITemplatesEnabled {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "El envío por WhatsApp API Oficial aún está bloqueado en esta fase"})
	}

	status := domain.DeviceStatusDisconnected
	webhookStatus := "not_configured"
	billingStatus := "not_configured"
	displayPhone := cleanDeviceString(req.APIDisplayPhone)
	device := &domain.Device{
		AccountID:           accountID,
		Name:                &req.Name,
		Phone:               displayPhone,
		Status:              &status,
		Provider:            &provider,
		WABAID:              cleanDeviceString(req.WABAID),
		PhoneNumberID:       cleanDeviceString(req.PhoneNumberID),
		APIDisplayPhone:     displayPhone,
		APIWebhookStatus:    cleanDeviceStringDefault(req.APIWebhookStatus, webhookStatus),
		APIBillingStatus:    cleanDeviceStringDefault(req.APIBillingStatus, billingStatus),
		APISendingEnabled:   false,
		APITemplatesEnabled: false,
		Capabilities:        json.RawMessage(`["cloud_api_config"]`),
	}
	if err := s.repos.Device.Create(c.Context(), device); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "device": device})
}

func (s *Server) handleGetDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}

	device, err := s.services.Device.GetByID(c.Context(), deviceID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if device == nil || device.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	return c.JSON(fiber.Map{"success": true, "device": device})
}

func (s *Server) handleConnectDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	dev, _ := s.services.Device.GetByID(c.Context(), deviceID)
	if dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}
	if isCloudAPIDevice(dev) {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Este canal usa WhatsApp API Oficial y no se conecta por QR"})
	}

	if err := s.services.Device.Connect(c.Context(), deviceID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Connecting device..."})
}

func (s *Server) handleDisconnectDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	dev, _ := s.services.Device.GetByID(c.Context(), deviceID)
	if dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}
	if isCloudAPIDevice(dev) {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Este canal usa WhatsApp API Oficial y no usa desconexión QR"})
	}

	if err := s.services.Device.Disconnect(c.Context(), deviceID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Device disconnected"})
}

func (s *Server) handleResetDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	dev, _ := s.services.Device.GetByID(c.Context(), deviceID)
	if dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}
	if isCloudAPIDevice(dev) {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Este canal usa WhatsApp API Oficial y no se re-vincula por QR"})
	}

	if err := s.services.Device.Reset(c.Context(), deviceID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Device reset. Reconnect to generate QR code for re-pairing."})
}

func (s *Server) handleDeleteDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	dev, _ := s.services.Device.GetByID(c.Context(), deviceID)
	if dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	var deleteErr error
	if isCloudAPIDevice(dev) {
		deleteErr = s.repos.Device.Delete(c.Context(), deviceID)
	} else {
		deleteErr = s.services.Device.Delete(c.Context(), deviceID)
	}
	if deleteErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": deleteErr.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Device deleted"})
}

func (s *Server) handleUpdateDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deviceID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	dev, _ := s.services.Device.GetByID(c.Context(), deviceID)
	if dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}
	var req struct {
		Name                *string `json:"name"`
		ReceiveMessages     *bool   `json:"receive_messages"`
		WABAID              *string `json:"waba_id"`
		PhoneNumberID       *string `json:"phone_number_id"`
		APIDisplayPhone     *string `json:"api_display_phone"`
		APIWebhookStatus    *string `json:"api_webhook_status"`
		APIBillingStatus    *string `json:"api_billing_status"`
		APISendingEnabled   *bool   `json:"api_sending_enabled"`
		APITemplatesEnabled *bool   `json:"api_templates_enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "El nombre del dispositivo es obligatorio"})
		}
		if err := s.repos.Device.UpdateName(c.Context(), deviceID, name); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}
	if isCloudAPIDevice(dev) {
		if req.APISendingEnabled != nil && *req.APISendingEnabled {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "El envío por WhatsApp API Oficial aún está bloqueado en esta fase"})
		}
		if req.APITemplatesEnabled != nil && *req.APITemplatesEnabled {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Las plantillas de WhatsApp API Oficial aún están bloqueadas en esta fase"})
		}
		apiConfig := *dev
		if req.WABAID != nil {
			apiConfig.WABAID = cleanDeviceString(req.WABAID)
		}
		if req.PhoneNumberID != nil {
			apiConfig.PhoneNumberID = cleanDeviceString(req.PhoneNumberID)
		}
		if req.APIDisplayPhone != nil {
			apiConfig.APIDisplayPhone = cleanDeviceString(req.APIDisplayPhone)
			apiConfig.Phone = apiConfig.APIDisplayPhone
		}
		if req.APIWebhookStatus != nil {
			apiConfig.APIWebhookStatus = cleanDeviceStringDefault(req.APIWebhookStatus, "not_configured")
		}
		if req.APIBillingStatus != nil {
			apiConfig.APIBillingStatus = cleanDeviceStringDefault(req.APIBillingStatus, "not_configured")
		}
		apiConfig.APISendingEnabled = false
		apiConfig.APITemplatesEnabled = false
		if len(apiConfig.Capabilities) == 0 {
			apiConfig.Capabilities = json.RawMessage(`["cloud_api_config"]`)
		}
		if err := s.repos.Device.UpdateCloudAPIConfig(c.Context(), deviceID, &apiConfig); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}
	if req.ReceiveMessages != nil {
		if err := s.repos.Device.UpdateReceiveMessages(c.Context(), deviceID, *req.ReceiveMessages); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		// Update in-memory flag in device pool so it takes effect immediately
		if s.pool != nil && !isCloudAPIDevice(dev) {
			s.pool.SetReceiveMessages(deviceID, *req.ReceiveMessages)
		}
	}
	device, _ := s.services.Device.GetByID(c.Context(), deviceID)
	return c.JSON(fiber.Map{"success": true, "device": device})
}

// --- Chat Handlers ---

func (s *Server) handleGetChats(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Parse filters
	filter := domain.ChatFilter{
		UnreadOnly: c.QueryBool("unread_only", false),
		Archived:   c.QueryBool("archived", false),
		Search:     c.Query("search", ""),
		Limit:      c.QueryInt("limit", 50),
		Offset:     c.QueryInt("offset", 0),
	}

	// Parse device_ids filter (supports both comma-separated and repeated params)
	deviceIDsRaw := c.Context().QueryArgs().PeekMulti("device_ids")
	for _, raw := range deviceIDsRaw {
		for _, idStr := range strings.Split(string(raw), ",") {
			if uid, err := uuid.Parse(strings.TrimSpace(idStr)); err == nil {
				filter.DeviceIDs = append(filter.DeviceIDs, uid)
			}
		}
	}

	// Parse tag_ids filter (same pattern as device_ids)
	tagIDsRaw := c.Context().QueryArgs().PeekMulti("tag_ids")
	for _, raw := range tagIDsRaw {
		for _, idStr := range strings.Split(string(raw), ",") {
			if uid, err := uuid.Parse(strings.TrimSpace(idStr)); err == nil {
				filter.TagIDs = append(filter.TagIDs, uid)
			}
		}
	}

	// Parse reaction filter
	filter.HasReaction = c.QueryBool("has_reaction", false)
	if filter.HasReaction {
		// reaction_from_me: "true"=operator, "false"=client, missing/"any"=both
		switch strings.ToLower(c.Query("reaction_from_me", "")) {
		case "true", "1", "me", "operator":
			v := true
			filter.ReactionFromMe = &v
		case "false", "0", "client", "contact":
			v := false
			filter.ReactionFromMe = &v
		}

		// reaction_emojis: comma-separated or repeated param
		emojisRaw := c.Context().QueryArgs().PeekMulti("reaction_emojis")
		for _, raw := range emojisRaw {
			for _, e := range strings.Split(string(raw), ",") {
				if v := strings.TrimSpace(e); v != "" {
					filter.ReactionEmojis = append(filter.ReactionEmojis, v)
				}
			}
		}

		// reaction_since / reaction_until: RFC3339 or unix seconds
		if v := c.Query("reaction_since", ""); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				filter.ReactionSince = &t
			} else if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
				t := time.Unix(secs, 0)
				filter.ReactionSince = &t
			}
		}
		if v := c.Query("reaction_until", ""); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				filter.ReactionUntil = &t
			} else if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
				t := time.Unix(secs, 0)
				filter.ReactionUntil = &t
			}
		}
	}

	// Redis cache for default load (no search/filters) — 15s TTL
	isDefaultLoad := filter.Search == "" && !filter.UnreadOnly && !filter.Archived && len(filter.DeviceIDs) == 0 && len(filter.TagIDs) == 0 && !filter.HasReaction && filter.Offset == 0
	cacheKey := ""
	if isDefaultLoad && s.cache != nil {
		cacheKey = fmt.Sprintf("chats:%s:%d", accountID.String(), filter.Limit)
		if cached, err := s.cache.Get(c.Context(), cacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	chats, total, err := s.services.Chat.GetByAccountIDWithFilters(c.Context(), accountID, filter)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	result := fiber.Map{
		"success": true,
		"chats":   chats,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	}

	// Cache default load result
	if cacheKey != "" && s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), cacheKey, data, 15*time.Second)
		}
	}

	return c.JSON(result)
}

// invalidateChatsCache invalidates the cached chats for an account
func (s *Server) invalidateChatsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "chats:"+accountID.String()+":*")
	}
}

func (s *Server) invalidateMessagesCache(accountID uuid.UUID, chatID *uuid.UUID) {
	if s.cache == nil {
		return
	}
	if chatID != nil {
		_ = s.cache.DelPattern(context.Background(), "messages:"+accountID.String()+":"+chatID.String()+":*")
		return
	}
	_ = s.cache.DelPattern(context.Background(), "messages:"+accountID.String()+":*")
}

func (s *Server) invalidateChatCaches(accountID uuid.UUID, chatID *uuid.UUID) {
	s.invalidateChatsCache(accountID)
	s.invalidateMessagesCache(accountID, chatID)
}

func (s *Server) handleGetChatDetails(c *fiber.Ctx) error {
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid chat ID"})
	}

	details, err := s.services.Chat.GetChatDetails(c.Context(), chatID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if details == nil || details.Chat == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Chat not found"})
	}

	// Load structured tags for contact
	if details.Contact != nil {
		tags, _ := s.services.Tag.GetByEntity(c.Context(), "contact", details.Contact.ID)
		details.Contact.StructuredTags = tags
	}
	if details.Lead != nil {
		tags, _ := s.services.Tag.GetByEntity(c.Context(), "lead", details.Lead.ID)
		details.Lead.StructuredTags = tags
	}

	return c.JSON(fiber.Map{
		"success": true,
		"chat":    details.Chat,
		"contact": details.Contact,
		"lead":    details.Lead,
	})
}

func (s *Server) handleFindChatByPhone(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	phone := c.Params("phone")
	if phone == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Phone is required"})
	}

	// Normalize and build JID
	normalized := kommo.NormalizePhone(phone)
	jid := normalized + "@s.whatsapp.net"

	chat, err := s.services.Chat.FindByJID(c.Context(), accountID, jid)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if chat == nil {
		return c.JSON(fiber.Map{"success": true, "chat": nil})
	}

	return c.JSON(fiber.Map{"success": true, "chat": chat})
}

func (s *Server) handleCreateNewChat(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		DeviceID       string `json:"device_id"`
		Phone          string `json:"phone"`
		InitialMessage string `json:"initial_message,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}

	if req.Phone == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Phone number is required"})
	}

	// Create chat
	chat, err := s.services.Chat.CreateNewChat(c.Context(), accountID, deviceID, req.Phone)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Send initial message if provided
	if req.InitialMessage != "" {
		_, err := s.services.Chat.SendMessage(c.Context(), deviceID, chat.JID, req.InitialMessage)
		if err != nil {
			// Chat created but message failed - still return chat
			s.invalidateChatsCache(accountID)
			return c.Status(201).JSON(fiber.Map{
				"success": true,
				"chat":    chat,
				"warning": "Chat created but initial message failed to send",
			})
		}
	}

	s.invalidateChatsCache(accountID)
	return c.Status(201).JSON(fiber.Map{"success": true, "chat": chat})
}

func (s *Server) handleGetMessages(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid chat ID"})
	}

	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	chat, err := s.services.Chat.GetByID(c.Context(), chatID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if chat == nil || chat.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Chat not found"})
	}

	cacheKey := ""
	if s.cache != nil && offset == 0 && limit <= 50 {
		cacheKey = fmt.Sprintf("messages:%s:%s:%d:%d", accountID.String(), chatID.String(), limit, offset)
		if cached, err := s.cache.Get(c.Context(), cacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	messages, err := s.services.Chat.GetMessages(c.Context(), chatID, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Load reactions for this chat
	reactions, _ := s.services.Chat.GetReactions(c.Context(), chatID)
	reactionsByMsg := make(map[string][]*domain.MessageReaction)
	for _, r := range reactions {
		reactionsByMsg[r.TargetMessageID] = append(reactionsByMsg[r.TargetMessageID], r)
	}

	// Attach reactions and poll data to messages
	for _, msg := range messages {
		if rxns, ok := reactionsByMsg[msg.MessageID]; ok {
			msg.Reactions = rxns
		}
		if msg.MessageType != nil && *msg.MessageType == domain.MessageTypePoll {
			options, votes, _ := s.services.Chat.GetPollData(c.Context(), msg.ID)
			msg.PollOptions = options
			msg.PollVotes = votes
		}
	}

	result := fiber.Map{"success": true, "messages": messages}
	if cacheKey != "" && s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), cacheKey, data, 15*time.Second)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleMarkAsRead(c *fiber.Ctx) error {
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid chat ID"})
	}

	if err := s.services.Chat.MarkAsRead(c.Context(), chatID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	accountID := c.Locals("account_id").(uuid.UUID)
	s.invalidateChatCaches(accountID, &chatID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleDeleteChat(c *fiber.Ctx) error {
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid chat ID"})
	}

	if err := s.services.Chat.Delete(c.Context(), chatID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	accountID := c.Locals("account_id").(uuid.UUID)
	s.invalidateChatCaches(accountID, &chatID)
	return c.JSON(fiber.Map{"success": true, "message": "Chat deleted"})
}

func (s *Server) handleRequestHistorySync(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid chat ID"})
	}

	// Get the chat to find its JID and device
	chat, err := s.services.Chat.GetByID(c.Context(), chatID)
	if err != nil || chat == nil || chat.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Chat not found"})
	}

	if chat.DeviceID == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Chat has no associated device"})
	}

	if err := s.services.Chat.RequestHistorySync(c.Context(), accountID, *chat.DeviceID, chatID, chat.JID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "message": "History sync requested"})
}

func (s *Server) handleDeleteChatsBatch(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		IDs       []string `json:"ids"`
		DeleteAll bool     `json:"delete_all"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if req.DeleteAll {
		if err := s.services.Chat.DeleteAll(c.Context(), accountID); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		s.invalidateChatCaches(accountID, nil)
		return c.JSON(fiber.Map{"success": true, "message": "All chats deleted"})
	}

	if len(req.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No IDs provided"})
	}

	var uuids []uuid.UUID
	for _, id := range req.IDs {
		if uid, err := uuid.Parse(id); err == nil {
			uuids = append(uuids, uid)
		}
	}

	if len(uuids) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No valid IDs provided"})
	}

	if err := s.services.Chat.DeleteBatch(c.Context(), uuids); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateChatCaches(accountID, nil)
	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("%d chats deleted", len(uuids))})
}

func (s *Server) handleSendMessage(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID        string `json:"device_id"`
		To              string `json:"to"`
		Body            string `json:"body"`
		MediaURL        string `json:"media_url,omitempty"`
		MediaType       string `json:"media_type,omitempty"` // image, video, audio, document
		QuotedMessageID string `json:"quoted_message_id,omitempty"`
		QuotedBody      string `json:"quoted_body,omitempty"`
		QuotedSender    string `json:"quoted_sender,omitempty"`
		QuotedIsFromMe  bool   `json:"quoted_is_from_me,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	var message *domain.Message

	if req.MediaURL != "" && req.MediaType != "" {
		// Send media message
		message, err = s.services.Chat.SendMediaMessage(c.Context(), deviceID, req.To, req.Body, req.MediaURL, req.MediaType)
	} else if req.QuotedMessageID != "" {
		// Send reply message
		message, err = s.services.Chat.SendReplyMessage(c.Context(), deviceID, req.To, req.Body, req.QuotedMessageID, req.QuotedBody, req.QuotedSender, req.QuotedIsFromMe)
	} else {
		// Send text message
		message, err = s.services.Chat.SendMessage(c.Context(), deviceID, req.To, req.Body)
	}

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if message != nil {
		s.invalidateChatCaches(accountID, &message.ChatID)
	} else {
		s.invalidateChatCaches(accountID, nil)
	}

	return c.JSON(fiber.Map{"success": true, "message": message})
}

func (s *Server) handleSendContact(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID     string `json:"device_id"`
		To           string `json:"to"`
		ContactName  string `json:"contact_name"`
		ContactPhone string `json:"contact_phone"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	if req.ContactName == "" || req.ContactPhone == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "contact_name and contact_phone are required"})
	}

	message, err := s.services.Chat.SendContactMessage(c.Context(), deviceID, req.To, req.ContactName, req.ContactPhone)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if message != nil {
		s.invalidateChatCaches(accountID, &message.ChatID)
	} else {
		s.invalidateChatCaches(accountID, nil)
	}

	return c.JSON(fiber.Map{"success": true, "message": message})
}

func (s *Server) handleForwardMessage(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID  string `json:"device_id"`
		To        string `json:"to"`         // target chat JID
		ChatID    string `json:"chat_id"`    // source chat UUID
		MessageID string `json:"message_id"` // WhatsApp message_id of the original message
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid chat ID"})
	}

	// Get original message
	originalMsg, err := s.services.Chat.GetMessageByID(c.Context(), chatID, req.MessageID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Original message not found"})
	}

	// Forward it
	message, err := s.services.Chat.ForwardMessage(c.Context(), deviceID, req.To, originalMsg)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if message != nil {
		s.invalidateChatCaches(accountID, &message.ChatID)
	} else {
		s.invalidateChatCaches(accountID, nil)
	}

	return c.JSON(fiber.Map{"success": true, "message": message})
}

func (s *Server) handleSendReaction(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID        string `json:"device_id"`
		To              string `json:"to"`
		TargetMessageID string `json:"target_message_id"`
		TargetFromMe    bool   `json:"target_from_me"`
		Emoji           string `json:"emoji"` // empty to remove
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	if req.TargetMessageID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "target_message_id is required"})
	}

	if err := s.services.Chat.SendReaction(c.Context(), deviceID, req.To, req.TargetMessageID, req.Emoji, req.TargetFromMe); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if chat, _ := s.services.Chat.FindByJID(c.Context(), accountID, req.To); chat != nil {
		s.invalidateMessagesCache(accountID, &chat.ID)
	} else {
		s.invalidateMessagesCache(accountID, nil)
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleSendPoll(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID      string   `json:"device_id"`
		To            string   `json:"to"`
		Question      string   `json:"question"`
		Options       []string `json:"options"`
		MaxSelections int      `json:"max_selections"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	if req.Question == "" || len(req.Options) < 2 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Question and at least 2 options are required"})
	}

	if len(req.Options) > 12 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Maximum 12 options allowed"})
	}

	message, err := s.services.Chat.SendPoll(c.Context(), deviceID, req.To, req.Question, req.Options, req.MaxSelections)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if message != nil {
		s.invalidateChatCaches(accountID, &message.ChatID)
	} else {
		s.invalidateChatCaches(accountID, nil)
	}

	return c.JSON(fiber.Map{"success": true, "message": message})
}

func (s *Server) handleSendTyping(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID  string `json:"device_id"`
		To        string `json:"to"`
		Composing bool   `json:"composing"`
		Media     string `json:"media"` // "" or "audio"
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	if err := s.services.Chat.SendChatPresence(c.Context(), deviceID, req.To, req.Composing, req.Media); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleSendReadReceipt(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID   string   `json:"device_id"`
		ChatJID    string   `json:"chat_jid"`
		SenderJID  string   `json:"sender_jid"`
		MessageIDs []string `json:"message_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	if dev, _ := s.services.Device.GetByID(c.Context(), deviceID); dev == nil || dev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Device not found"})
	}

	if len(req.MessageIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "message_ids is required"})
	}

	if err := s.services.Chat.SendReadReceipt(c.Context(), deviceID, req.ChatJID, req.SenderJID, req.MessageIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if chat, _ := s.services.Chat.FindByJID(c.Context(), accountID, req.ChatJID); chat != nil {
		s.invalidateMessagesCache(accountID, &chat.ID)
	} else {
		s.invalidateMessagesCache(accountID, nil)
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleDeleteMessage(c *fiber.Ctx) error {
	var req struct {
		DeviceID  string `json:"device_id"`
		ChatJID   string `json:"chat_jid"`
		SenderJID string `json:"sender_jid"`
		MessageID string `json:"message_id"`
		IsFromMe  bool   `json:"is_from_me"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}

	if req.MessageID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "message_id is required"})
	}

	if err := s.services.Chat.RevokeMessage(c.Context(), deviceID, req.ChatJID, req.SenderJID, req.MessageID, req.IsFromMe); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Mark as revoked in DB
	accountID := c.Locals("account_id").(uuid.UUID)
	_ = s.repos.Message.MarkAsRevoked(c.Context(), accountID, req.ChatJID, req.MessageID)
	if chat, _ := s.services.Chat.FindByJID(c.Context(), accountID, req.ChatJID); chat != nil {
		s.invalidateMessagesCache(accountID, &chat.ID)
	} else {
		s.invalidateMessagesCache(accountID, nil)
	}

	// Broadcast revocation to frontend
	s.hub.BroadcastToAccount(accountID, "message_revoked", map[string]interface{}{
		"chat_jid":   req.ChatJID,
		"message_id": req.MessageID,
		"is_from_me": req.IsFromMe,
	})

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleEditMessage(c *fiber.Ctx) error {
	var req struct {
		DeviceID  string `json:"device_id"`
		ChatJID   string `json:"chat_jid"`
		MessageID string `json:"message_id"`
		NewBody   string `json:"new_body"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}

	if req.MessageID == "" || req.NewBody == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "message_id and new_body are required"})
	}

	if err := s.services.Chat.EditMessage(c.Context(), deviceID, req.ChatJID, req.MessageID, req.NewBody); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Update in DB
	accountID := c.Locals("account_id").(uuid.UUID)
	_ = s.repos.Message.UpdateBody(c.Context(), accountID, req.ChatJID, req.MessageID, req.NewBody)
	if chat, _ := s.services.Chat.FindByJID(c.Context(), accountID, req.ChatJID); chat != nil {
		s.invalidateMessagesCache(accountID, &chat.ID)
	} else {
		s.invalidateMessagesCache(accountID, nil)
	}

	// Broadcast to frontend
	s.hub.BroadcastToAccount(accountID, ws.EventMessageEdited, map[string]interface{}{
		"chat_jid":   req.ChatJID,
		"message_id": req.MessageID,
		"new_body":   req.NewBody,
		"is_from_me": true,
	})

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleCheckWhatsApp(c *fiber.Ctx) error {
	var req struct {
		DeviceID string   `json:"device_id"`
		Phones   []string `json:"phones"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}

	if len(req.Phones) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "phones is required"})
	}

	results, err := s.services.Chat.IsOnWhatsApp(c.Context(), deviceID, req.Phones)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true, "results": results})
}

// --- Media Handlers ---

func classifyStorageMediaType(objectKey, contentType string) string {
	lower := strings.ToLower(objectKey)
	if strings.HasPrefix(contentType, "image/") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".gif") || strings.HasSuffix(lower, ".webp") {
		return "image"
	}
	if strings.HasPrefix(contentType, "video/") || strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".webm") || strings.HasSuffix(lower, ".mov") || strings.HasSuffix(lower, ".3gp") {
		return "video"
	}
	if strings.HasPrefix(contentType, "audio/") || strings.HasSuffix(lower, ".ogg") || strings.HasSuffix(lower, ".mp3") || strings.HasSuffix(lower, ".wav") || strings.HasSuffix(lower, ".opus") || strings.HasSuffix(lower, ".aac") {
		return "audio"
	}
	if strings.Contains(lower, "/chats/") || strings.Contains(lower, "/uploads/") || strings.Contains(lower, "/documents/") {
		return "document"
	}
	return "other"
}

func objectKeyFromMediaURL(mediaURL string) string {
	mediaURL = strings.TrimSpace(mediaURL)
	if mediaURL == "" {
		return ""
	}
	if strings.HasPrefix(mediaURL, "/api/media/file/") {
		key := strings.TrimPrefix(mediaURL, "/api/media/file/")
		if decoded, err := url.PathUnescape(key); err == nil {
			return decoded
		}
		return key
	}
	if sidx := strings.Index(mediaURL, "/clarin-media/"); sidx >= 0 {
		return mediaURL[sidx+len("/clarin-media/"):]
	}
	return ""
}

func mediaProxyURLFromObjectKey(objectKey string) string {
	return "/api/media/file/" + objectKey
}

func storageFolderFromObjectKey(accountID uuid.UUID, objectKey string) string {
	rel := strings.TrimPrefix(objectKey, accountID.String()+"/")
	if idx := strings.Index(rel, "/"); idx > 0 {
		return rel[:idx]
	}
	return "other"
}

func (s *Server) accountStorageUsage(ctx context.Context, accountID uuid.UUID) (int64, int64, error) {
	if s.storage == nil {
		return 0, 0, nil
	}
	return s.storage.UsagePrefix(ctx, accountID.String()+"/")
}

func (s *Server) ensureStorageQuota(ctx context.Context, accountID uuid.UUID, incomingBytes int64) error {
	if incomingBytes <= 0 || s.storage == nil {
		return nil
	}
	account, err := s.services.Account.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil || account.StorageLimitBytes <= 0 {
		return nil
	}
	used, _, err := s.accountStorageUsage(ctx, accountID)
	if err != nil {
		return err
	}
	if used+incomingBytes > account.StorageLimitBytes {
		return fmt.Errorf("storage limit reached")
	}
	return nil
}

func (s *Server) userCanManageStorage(c *fiber.Ctx) bool {
	claims, ok := c.Locals("claims").(*service.JWTClaims)
	if !ok {
		return false
	}
	if claims.IsSuperAdmin || claims.IsAdmin || claims.Role == domain.RoleAdmin || claims.Role == domain.RoleSuperAdmin {
		return true
	}
	for _, p := range claims.Permissions {
		if p == domain.PermAll || p == domain.PermSettings {
			return true
		}
	}
	return false
}

func (s *Server) handleGetStorageUsage(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	account, err := s.services.Account.GetByID(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	objects, err := s.storage.ListPrefix(c.Context(), accountID.String()+"/")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	associated, err := s.storageAssociatedURLs(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	byType := map[string]int64{"image": 0, "video": 0, "audio": 0, "document": 0, "other": 0}
	byFolder := map[string]int64{"chats": 0, "uploads": 0, "avatars": 0, "other": 0}
	var used int64
	var associatedBytes int64
	var orphanBytes int64
	var associatedCount int
	var orphanCount int
	for _, object := range objects {
		mediaType := classifyStorageMediaType(object.Key, "")
		folder := storageFolderFromObjectKey(accountID, object.Key)
		if _, ok := byFolder[folder]; !ok {
			folder = "other"
		}
		byType[mediaType] += object.Size
		byFolder[folder] += object.Size
		used += object.Size
		if _, ok := associated[mediaProxyURLFromObjectKey(object.Key)]; ok {
			associatedBytes += object.Size
			associatedCount++
		} else {
			orphanBytes += object.Size
			orphanCount++
		}
	}

	var limit int64
	if account != nil {
		limit = account.StorageLimitBytes
	}
	available := int64(0)
	percent := float64(0)
	if limit > 0 {
		available = limit - used
		if available < 0 {
			available = 0
		}
		percent = float64(used) / float64(limit) * 100
		if percent > 100 {
			percent = 100
		}
	}

	return c.JSON(fiber.Map{
		"success":          true,
		"limit_bytes":      limit,
		"used_bytes":       used,
		"available_bytes":  available,
		"object_count":     len(objects),
		"percent_used":     percent,
		"by_type":          byType,
		"by_folder":        byFolder,
		"associated_bytes": associatedBytes,
		"orphan_bytes":     orphanBytes,
		"associated_count": associatedCount,
		"orphan_count":     orphanCount,
		"can_manage":       s.userCanManageStorage(c),
	})
}

type storageMessageRef struct {
	mediaType    string
	filename     string
	dbSize       int64
	lastUsed     time.Time
	references   int64
	mediaAssetID *uuid.UUID
	contentHash  string
}

func (s *Server) storageAssociatedURLs(ctx context.Context, accountID uuid.UUID) (map[string]storageMessageRef, error) {
	rows, err := s.repos.DB().Query(ctx, `
		SELECT m.media_url,
		       COALESCE(message_type, 'document') AS message_type,
		       COALESCE(media_filename, '') AS filename,
		       COALESCE(MAX(media_size), 0) AS db_size,
		       MAX(timestamp) AS last_used_at,
		       COUNT(*) AS references_count,
		       (ARRAY_AGG(m.media_asset_id) FILTER (WHERE m.media_asset_id IS NOT NULL))[1] AS media_asset_id,
		       COALESCE(MAX(ma.content_hash), '') AS content_hash
		FROM messages m
		LEFT JOIN media_assets ma ON ma.id = m.media_asset_id AND ma.account_id = m.account_id
		WHERE m.account_id = $1
		  AND COALESCE(m.media_deleted, false) = false
		  AND m.media_url IS NOT NULL
		  AND m.media_url <> ''
		  AND m.media_url LIKE $2
		GROUP BY m.media_url, message_type, media_filename
	`, accountID, "/api/media/file/"+accountID.String()+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]storageMessageRef)
	for rows.Next() {
		var mediaURL string
		ref := storageMessageRef{}
		if err := rows.Scan(&mediaURL, &ref.mediaType, &ref.filename, &ref.dbSize, &ref.lastUsed, &ref.references, &ref.mediaAssetID, &ref.contentHash); err != nil {
			return nil, err
		}
		result[mediaURL] = ref
		if objectKey := objectKeyFromMediaURL(mediaURL); objectKey != "" {
			result[mediaProxyURLFromObjectKey(objectKey)] = ref
		}
	}
	return result, rows.Err()
}

func (s *Server) handleListStorageFiles(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	mediaType := strings.TrimSpace(c.Query("type", ""))
	query := strings.ToLower(strings.TrimSpace(c.Query("q", "")))
	status := strings.TrimSpace(c.Query("status", "all"))
	sortBy := strings.TrimSpace(c.Query("sort", "date"))
	order := strings.TrimSpace(c.Query("order", "desc"))
	limit := c.QueryInt("limit", 50)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := c.QueryInt("offset", 0)
	if offset < 0 {
		offset = 0
	}

	associated, err := s.storageAssociatedURLs(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	objects, err := s.storage.ListPrefix(c.Context(), accountID.String()+"/")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	type storageFileRow struct {
		objectKey       string
		mediaURL        string
		mediaType       string
		filename        string
		sizeBytes       int64
		lastModified    time.Time
		lastUsed        time.Time
		referencesCount int64
		mediaAssetID    *uuid.UUID
		contentHash     string
		status          string
		folder          string
	}

	allFiles := make([]storageFileRow, 0, len(objects))
	for _, object := range objects {
		mediaURL := mediaProxyURLFromObjectKey(object.Key)
		ref, isAssociated := associated[mediaURL]
		fileStatus := "orphan"
		if isAssociated {
			fileStatus = "associated"
		}
		if status != "" && status != "all" && status != fileStatus {
			continue
		}
		typ := classifyStorageMediaType(object.Key, "")
		if ref.mediaType != "" {
			typ = ref.mediaType
		}
		if mediaType != "" && mediaType != typ {
			continue
		}
		filename := ref.filename
		if filename == "" {
			filename = filepath.Base(object.Key)
		}
		if query != "" && !strings.Contains(strings.ToLower(filename), query) && !strings.Contains(strings.ToLower(object.Key), query) {
			continue
		}
		allFiles = append(allFiles, storageFileRow{
			objectKey:       object.Key,
			mediaURL:        mediaURL,
			mediaType:       typ,
			filename:        filename,
			sizeBytes:       object.Size,
			lastModified:    object.LastModified,
			lastUsed:        ref.lastUsed,
			referencesCount: ref.references,
			mediaAssetID:    ref.mediaAssetID,
			contentHash:     ref.contentHash,
			status:          fileStatus,
			folder:          storageFolderFromObjectKey(accountID, object.Key),
		})
	}

	sort.SliceStable(allFiles, func(i, j int) bool {
		less := false
		switch sortBy {
		case "name":
			less = strings.ToLower(allFiles[i].filename) < strings.ToLower(allFiles[j].filename)
		case "size":
			less = allFiles[i].sizeBytes < allFiles[j].sizeBytes
		default:
			left := allFiles[i].lastModified
			right := allFiles[j].lastModified
			if !allFiles[i].lastUsed.IsZero() {
				left = allFiles[i].lastUsed
			}
			if !allFiles[j].lastUsed.IsZero() {
				right = allFiles[j].lastUsed
			}
			less = left.Before(right)
		}
		if order == "asc" {
			return less
		}
		return !less
	})

	total := len(allFiles)
	end := offset + limit
	if end > total {
		end = total
	}
	if offset > total {
		offset = total
	}
	files := make([]fiber.Map, 0, end-offset)
	for _, file := range allFiles[offset:end] {
		files = append(files, fiber.Map{
			"object_key":           file.objectKey,
			"media_url":            file.mediaURL,
			"preview_url":          file.mediaURL,
			"media_type":           file.mediaType,
			"filename":             file.filename,
			"size_bytes":           file.sizeBytes,
			"last_modified":        file.lastModified,
			"last_used_at":         file.lastUsed,
			"references_count":     file.referencesCount,
			"media_asset_id":       file.mediaAssetID,
			"content_hash":         file.contentHash,
			"is_shared":            file.referencesCount > 1,
			"canonical_object_key": file.objectKey,
			"status":               file.status,
			"folder":               file.folder,
		})
	}

	return c.JSON(fiber.Map{
		"success":     true,
		"files":       files,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
		"next_offset": offset + len(files),
		"can_manage":  s.userCanManageStorage(c),
	})
}

func (s *Server) handleDeleteStorageFiles(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}
	if !s.userCanManageStorage(c) {
		return c.Status(403).JSON(fiber.Map{"success": false, "error": "No tienes permiso para eliminar archivos"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)
	var req struct {
		ObjectKeys    []string `json:"object_keys"`
		MediaAssetIDs []string `json:"media_asset_ids"`
		Confirmation  string   `json:"confirmation"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Confirmation != "DELETE_MEDIA" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Confirmation must be DELETE_MEDIA"})
	}
	if len(req.ObjectKeys) == 0 && len(req.MediaAssetIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "object_keys or media_asset_ids is required"})
	}
	if len(req.ObjectKeys)+len(req.MediaAssetIDs) > 100 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Máximo 100 archivos por operación"})
	}

	deleted := 0
	var freedBytes int64
	var messagesAffected int64
	errors := make([]fiber.Map, 0)
	accountPrefix := accountID.String() + "/"
	for _, rawAssetID := range req.MediaAssetIDs {
		assetID, err := uuid.Parse(strings.TrimSpace(rawAssetID))
		if err != nil {
			errors = append(errors, fiber.Map{"media_asset_id": rawAssetID, "error": "ID inválido"})
			continue
		}
		var objectKey string
		var sizeBytes int64
		if err := s.repos.DB().QueryRow(c.Context(), `
			SELECT object_key, size_bytes
			FROM media_assets
			WHERE id = $1 AND account_id = $2 AND status = 'active'
		`, assetID, accountID).Scan(&objectKey, &sizeBytes); err != nil {
			errors = append(errors, fiber.Map{"media_asset_id": rawAssetID, "error": "No se pudo encontrar el asset"})
			continue
		}
		if !strings.HasPrefix(objectKey, accountPrefix) {
			errors = append(errors, fiber.Map{"media_asset_id": rawAssetID, "error": "Archivo fuera del alcance permitido"})
			continue
		}
		if info, statErr := s.storage.GetFileInfo(c.Context(), objectKey); statErr == nil {
			sizeBytes = info.Size
		}
		if err := s.storage.DeleteFile(c.Context(), objectKey); err != nil {
			errors = append(errors, fiber.Map{"media_asset_id": rawAssetID, "error": err.Error()})
			continue
		}
		result, _ := s.repos.DB().Exec(c.Context(), `
			UPDATE messages
			SET media_url = NULL,
			    media_size = NULL,
			    media_asset_id = NULL,
			    media_deleted = TRUE,
			    media_deleted_at = NOW()
			WHERE account_id = $1 AND media_asset_id = $2 AND COALESCE(media_deleted, false) = false
		`, accountID, assetID)
		messagesAffected += result.RowsAffected()
		_, _ = s.repos.DB().Exec(c.Context(), `
			UPDATE media_assets
			SET status = 'deleted', deleted_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND account_id = $2
		`, assetID, accountID)
		_, _ = s.repos.DB().Exec(c.Context(), `
			INSERT INTO storage_objects (account_id, object_key, media_type, filename, size_bytes, source, status, deleted_at, deleted_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, 'chat', 'deleted', NOW(), $6, NOW())
			ON CONFLICT (account_id, object_key) DO UPDATE
			SET status = 'deleted', deleted_at = NOW(), deleted_by = $6, updated_at = NOW()
		`, accountID, objectKey, classifyStorageMediaType(objectKey, ""), filepath.Base(objectKey), sizeBytes, userID)
		freedBytes += sizeBytes
		deleted++
	}
	for _, objectKey := range req.ObjectKeys {
		objectKey = strings.TrimSpace(objectKey)
		if !strings.HasPrefix(objectKey, accountPrefix) {
			errors = append(errors, fiber.Map{"object_key": objectKey, "error": "Archivo fuera del alcance permitido"})
			continue
		}
		proxyURL := mediaProxyURLFromObjectKey(objectKey)
		publicURL := ""
		if s.storage != nil {
			publicURL = s.storage.GetPublicURL(objectKey)
		}
		var activeMessageRefs int
		if err := s.repos.DB().QueryRow(c.Context(), `
			SELECT COUNT(*)
			FROM messages
			WHERE account_id = $1
			  AND (media_url = $2 OR media_url = $3)
			  AND COALESCE(media_deleted, false) = false
		`, accountID, proxyURL, publicURL).Scan(&activeMessageRefs); err != nil {
			errors = append(errors, fiber.Map{"object_key": objectKey, "error": "No se pudo validar el archivo"})
			continue
		}
		if activeMessageRefs == 0 {
			info, statErr := s.storage.GetFileInfo(c.Context(), objectKey)
			sizeBytes := int64(0)
			if statErr == nil {
				sizeBytes = info.Size
				freedBytes += sizeBytes
			}
			if err := s.storage.DeleteFile(c.Context(), objectKey); err != nil && statErr == nil {
				errors = append(errors, fiber.Map{"object_key": objectKey, "error": err.Error()})
				continue
			}
			_, _ = s.repos.DB().Exec(c.Context(), `
				INSERT INTO storage_objects (account_id, object_key, media_type, filename, size_bytes, source, status, deleted_at, deleted_by, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, 'deleted', NOW(), $7, NOW())
				ON CONFLICT (account_id, object_key) DO UPDATE
				SET status = 'deleted', deleted_at = NOW(), deleted_by = $7, updated_at = NOW()
			`, accountID, objectKey, classifyStorageMediaType(objectKey, ""), filepath.Base(objectKey), sizeBytes, storageFolderFromObjectKey(accountID, objectKey), userID)
			deleted++
			continue
		}
		info, statErr := s.storage.GetFileInfo(c.Context(), objectKey)
		sizeBytes := int64(0)
		if statErr == nil {
			sizeBytes = info.Size
			freedBytes += sizeBytes
		}
		if err := s.storage.DeleteFile(c.Context(), objectKey); err != nil && statErr == nil {
			errors = append(errors, fiber.Map{"object_key": objectKey, "error": err.Error()})
			continue
		}
		result, _ := s.repos.DB().Exec(c.Context(), `
			UPDATE messages
			SET media_url = NULL,
			    media_size = NULL,
			    media_asset_id = NULL,
			    media_deleted = TRUE,
			    media_deleted_at = NOW()
			WHERE account_id = $1 AND (media_url = $2 OR media_url = $3)
		`, accountID, proxyURL, publicURL)
		messagesAffected += result.RowsAffected()
		_, _ = s.repos.DB().Exec(c.Context(), `
			INSERT INTO storage_objects (account_id, object_key, media_type, filename, size_bytes, source, status, deleted_at, deleted_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, 'chat', 'deleted', NOW(), $6, NOW())
			ON CONFLICT (account_id, object_key) DO UPDATE
			SET status = 'deleted', deleted_at = NOW(), deleted_by = $6, updated_at = NOW()
		`, accountID, objectKey, classifyStorageMediaType(objectKey, ""), filepath.Base(objectKey), sizeBytes, userID)
		deleted++
	}

	if messagesAffected > 0 {
		s.invalidateMessagesCache(accountID, nil)
	}

	return c.JSON(fiber.Map{
		"success":           true,
		"deleted":           deleted,
		"freed_bytes":       freedBytes,
		"messages_affected": messagesAffected,
		"errors":            errors,
	})
}

func (s *Server) handleStartStorageDedupe(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}
	if !s.userCanManageStorage(c) {
		return c.Status(403).JSON(fiber.Map{"success": false, "error": "No tienes permiso para compactar almacenamiento"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	var running int
	if err := s.repos.DB().QueryRow(c.Context(), `
		SELECT COUNT(*) FROM storage_dedupe_jobs
		WHERE account_id = $1 AND status IN ('queued', 'running')
	`, accountID).Scan(&running); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if running > 0 {
		return c.Status(409).JSON(fiber.Map{"success": false, "error": "Ya hay una compactación en progreso"})
	}
	var jobID uuid.UUID
	if err := s.repos.DB().QueryRow(c.Context(), `
		INSERT INTO storage_dedupe_jobs (account_id, status)
		VALUES ($1, 'queued')
		RETURNING id
	`, accountID).Scan(&jobID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	go s.runStorageDedupeJob(context.Background(), accountID, jobID)
	return c.Status(202).JSON(fiber.Map{"success": true, "job_id": jobID})
}

func (s *Server) handleGetStorageDedupeJob(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	jobID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid job ID"})
	}
	var status, errText string
	var total, processed, found, deleted, freed int64
	var startedAt, completedAt *time.Time
	if err := s.repos.DB().QueryRow(c.Context(), `
		SELECT status, total_objects, processed_objects, duplicates_found, duplicates_deleted, bytes_freed, error, started_at, completed_at
		FROM storage_dedupe_jobs
		WHERE id = $1 AND account_id = $2
	`, jobID, accountID).Scan(&status, &total, &processed, &found, &deleted, &freed, &errText, &startedAt, &completedAt); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Job not found"})
	}
	return c.JSON(fiber.Map{
		"success":            true,
		"job_id":             jobID,
		"status":             status,
		"total_objects":      total,
		"processed_objects":  processed,
		"duplicates_found":   found,
		"duplicates_deleted": deleted,
		"bytes_freed":        freed,
		"error":              errText,
		"started_at":         startedAt,
		"completed_at":       completedAt,
	})
}

func (s *Server) runStorageDedupeJob(ctx context.Context, accountID, jobID uuid.UUID) {
	_, _ = s.repos.DB().Exec(ctx, `
		UPDATE storage_dedupe_jobs
		SET status = 'running', started_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND account_id = $2
	`, jobID, accountID)
	failJob := func(err error) {
		_, _ = s.repos.DB().Exec(ctx, `
			UPDATE storage_dedupe_jobs
			SET status = 'failed', error = $3, completed_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND account_id = $2
		`, jobID, accountID, err.Error())
	}

	objects, err := s.storage.ListPrefix(ctx, accountID.String()+"/chats/")
	if err != nil {
		failJob(err)
		return
	}
	_, _ = s.repos.DB().Exec(ctx, `UPDATE storage_dedupe_jobs SET total_objects = $3, updated_at = NOW() WHERE id = $1 AND account_id = $2`, jobID, accountID, len(objects))
	type canonical struct {
		assetID   uuid.UUID
		objectKey string
		sizeBytes int64
		mediaURL  string
	}
	seen := make(map[string]canonical)
	var processed, found, deleted, freed int64
	for _, object := range objects {
		processed++
		data, err := s.storage.GetFile(ctx, object.Key)
		if err != nil {
			_, _ = s.repos.DB().Exec(ctx, `UPDATE storage_dedupe_jobs SET processed_objects = $3, error = $4, updated_at = NOW() WHERE id = $1 AND account_id = $2`, jobID, accountID, processed, err.Error())
			continue
		}
		hash := sha256.Sum256(data)
		contentHash := fmt.Sprintf("%x", hash[:])
		mediaType := classifyStorageMediaType(object.Key, "")
		if mediaType == "other" {
			mediaType = strings.TrimPrefix(strings.ToLower(filepath.Ext(object.Key)), ".")
			if mediaType == "" {
				mediaType = "bin"
			}
		}
		ext := strings.ToLower(filepath.Ext(object.Key))
		if ext == "" {
			ext = ".bin"
		}
		can, ok := seen[contentHash]
		if !ok {
			if existing, err := s.repos.MediaAsset.GetByHash(ctx, accountID, contentHash); err == nil && existing != nil {
				can = canonical{assetID: existing.ID, objectKey: existing.ObjectKey, sizeBytes: existing.SizeBytes, mediaURL: mediaProxyURLFromObjectKey(existing.ObjectKey)}
			} else {
				canonicalObjectKey := fmt.Sprintf("%s/media/%s/%s%s", accountID.String(), mediaType, contentHash, ext)
				if canonicalObjectKey != object.Key {
					if _, err := s.storage.UploadObject(ctx, canonicalObjectKey, data, ""); err != nil {
						_, _ = s.repos.DB().Exec(ctx, `UPDATE storage_dedupe_jobs SET processed_objects = $3, error = $4, updated_at = NOW() WHERE id = $1 AND account_id = $2`, jobID, accountID, processed, err.Error())
						continue
					}
				}
				asset, err := s.repos.MediaAsset.Upsert(ctx, repository.MediaAssetUpsert{
					AccountID:   accountID,
					ContentHash: contentHash,
					ObjectKey:   canonicalObjectKey,
					MediaType:   mediaType,
					ContentType: "",
					Filename:    filepath.Base(object.Key),
					SizeBytes:   int64(len(data)),
				})
				if err != nil {
					_, _ = s.repos.DB().Exec(ctx, `UPDATE storage_dedupe_jobs SET processed_objects = $3, error = $4, updated_at = NOW() WHERE id = $1 AND account_id = $2`, jobID, accountID, processed, err.Error())
					continue
				}
				can = canonical{assetID: asset.ID, objectKey: asset.ObjectKey, sizeBytes: asset.SizeBytes, mediaURL: mediaProxyURLFromObjectKey(asset.ObjectKey)}
			}
			seen[contentHash] = can
		}

		oldURL := mediaProxyURLFromObjectKey(object.Key)
		publicURL := s.storage.GetPublicURL(object.Key)
		result, _ := s.repos.DB().Exec(ctx, `
			UPDATE messages
			SET media_url = $4,
			    media_asset_id = $5,
			    media_size = $6
			WHERE account_id = $1
			  AND COALESCE(media_deleted, false) = false
			  AND (media_url = $2 OR media_url = $3)
		`, accountID, oldURL, publicURL, can.mediaURL, can.assetID, can.sizeBytes)
		if object.Key != can.objectKey {
			found++
			if result.RowsAffected() > 0 {
				if err := s.storage.DeleteFile(ctx, object.Key); err == nil {
					deleted++
					freed += object.Size
					_, _ = s.repos.DB().Exec(ctx, `
						INSERT INTO storage_objects (account_id, object_key, media_type, filename, size_bytes, source, status, deleted_at, updated_at)
						VALUES ($1, $2, $3, $4, $5, 'dedupe', 'deleted', NOW(), NOW())
						ON CONFLICT (account_id, object_key) DO UPDATE
						SET status = 'deleted', deleted_at = NOW(), updated_at = NOW()
					`, accountID, object.Key, mediaType, filepath.Base(object.Key), object.Size)
				}
			}
		}
		_, _ = s.repos.DB().Exec(ctx, `
			UPDATE storage_dedupe_jobs
			SET processed_objects = $3,
			    duplicates_found = $4,
			    duplicates_deleted = $5,
			    bytes_freed = $6,
			    updated_at = NOW()
			WHERE id = $1 AND account_id = $2
		`, jobID, accountID, processed, found, deleted, freed)
	}
	_, _ = s.repos.DB().Exec(ctx, `
		UPDATE storage_dedupe_jobs
		SET status = 'completed',
		    processed_objects = $3,
		    duplicates_found = $4,
		    duplicates_deleted = $5,
		    bytes_freed = $6,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1 AND account_id = $2
	`, jobID, accountID, processed, found, deleted, freed)
}

func (s *Server) handleGetUploadURL(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}

	accountID := c.Locals("account_id").(uuid.UUID)

	filename := c.Query("filename", "")
	folder := c.Query("folder", "uploads")
	size := c.QueryInt("size", 0)

	if filename == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Filename is required"})
	}
	if err := s.ensureStorageQuota(c.Context(), accountID, int64(size)); err != nil {
		return c.Status(fiber.StatusInsufficientStorage).JSON(fiber.Map{"success": false, "error": "Límite de almacenamiento alcanzado", "code": "storage_limit_reached"})
	}

	// Generate unique filename to avoid collisions
	uniqueFilename := uuid.New().String() + "_" + filename

	uploadURL, err := s.storage.GetPresignedUploadURL(c.Context(), accountID, folder, uniqueFilename)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Generate the public URL for the file after upload
	publicURL := s.storage.GetPublicURL(accountID.String() + "/" + folder + "/" + uniqueFilename)

	return c.JSON(fiber.Map{
		"success":    true,
		"upload_url": uploadURL,
		"public_url": publicURL,
		"filename":   uniqueFilename,
	})
}

// handleDirectUpload handles direct file upload through the backend
func (s *Server) handleDirectUpload(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}

	accountID := c.Locals("account_id").(uuid.UUID)

	// Get the file from form
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No file provided"})
	}

	folder := c.FormValue("folder", "uploads")

	// Validate file size (max 50MB)
	if file.Size > 50*1024*1024 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "File too large (max 50MB)"})
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to read file"})
	}
	defer src.Close()

	cleanFilename := filepath.Base(file.Filename)

	// Detect content type
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	data, err := io.ReadAll(src)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to read file"})
	}
	hashBytes := sha256.Sum256(data)
	contentHash := fmt.Sprintf("%x", hashBytes[:])
	ext := strings.ToLower(filepath.Ext(cleanFilename))
	if ext == "" {
		ext = ".bin"
	}
	mediaType := classifyStorageMediaType(cleanFilename, contentType)
	if existing, err := s.repos.MediaAsset.GetByHash(c.Context(), accountID, contentHash); err == nil && existing != nil {
		proxyURL := mediaProxyURLFromObjectKey(existing.ObjectKey)
		return c.JSON(fiber.Map{
			"success":        true,
			"public_url":     s.storage.GetPublicURL(existing.ObjectKey),
			"proxy_url":      proxyURL,
			"filename":       existing.Filename,
			"media_asset_id": existing.ID,
			"content_hash":   existing.ContentHash,
			"deduped":        true,
		})
	}
	if err := s.ensureStorageQuota(c.Context(), accountID, int64(len(data))); err != nil {
		return c.Status(fiber.StatusInsufficientStorage).JSON(fiber.Map{"success": false, "error": "Límite de almacenamiento alcanzado", "code": "storage_limit_reached"})
	}

	uniqueFilename := contentHash + ext
	objectKey := accountID.String() + "/" + strings.Trim(folder, "/") + "/" + uniqueFilename
	publicURL, err := s.storage.UploadObject(c.Context(), objectKey, data, contentType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to upload: " + err.Error()})
	}
	proxyURL := fmt.Sprintf("/api/media/file/%s", objectKey)
	asset, assetErr := s.repos.MediaAsset.Upsert(c.Context(), repository.MediaAssetUpsert{
		AccountID:   accountID,
		ContentHash: contentHash,
		ObjectKey:   objectKey,
		MediaType:   mediaType,
		ContentType: contentType,
		Filename:    cleanFilename,
		SizeBytes:   int64(len(data)),
	})
	if assetErr != nil {
		log.Printf("[Storage] Failed to upsert media asset: %v", assetErr)
	}
	_, _ = s.repos.DB().Exec(c.Context(), `
		INSERT INTO storage_objects (account_id, object_key, media_type, content_type, filename, size_bytes, source, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', NOW())
		ON CONFLICT (account_id, object_key) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes, content_type = EXCLUDED.content_type, media_type = EXCLUDED.media_type, status = 'active', updated_at = NOW()
	`, accountID, objectKey, mediaType, contentType, cleanFilename, int64(len(data)), folder)
	var mediaAssetID interface{}
	if asset != nil {
		mediaAssetID = asset.ID
	}

	return c.JSON(fiber.Map{
		"success":        true,
		"public_url":     publicURL,
		"proxy_url":      proxyURL,
		"filename":       uniqueFilename,
		"media_asset_id": mediaAssetID,
		"content_hash":   contentHash,
		"deduped":        false,
	})
}

// handleMediaProxy serves files from MinIO through the backend
func (s *Server) handleMediaProxy(c *fiber.Ctx) error {
	if s.storage == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Storage not configured"})
	}

	// Get the path after /file/ and URL-decode it
	objectKey := c.Params("*")
	if objectKey == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid path"})
	}
	// Fiber returns URL-encoded path for wildcard params, decode for MinIO lookup
	if decoded, err := url.PathUnescape(objectKey); err == nil {
		objectKey = decoded
	}

	// Detect content type from extension
	contentType := "application/octet-stream"
	if dotIdx := strings.LastIndex(objectKey, "."); dotIdx >= 0 {
		ext := strings.ToLower(objectKey[dotIdx:])
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		case ".mp4":
			contentType = "video/mp4"
		case ".webm":
			contentType = "video/webm"
		case ".mp3":
			contentType = "audio/mpeg"
		case ".ogg":
			contentType = "audio/ogg"
		case ".pdf":
			contentType = "application/pdf"
		case ".doc":
			contentType = "application/msword"
		case ".docx":
			contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case ".xls":
			contentType = "application/vnd.ms-excel"
		case ".xlsx":
			contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		case ".ppt":
			contentType = "application/vnd.ms-powerpoint"
		case ".pptx":
			contentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
		case ".txt":
			contentType = "text/plain; charset=utf-8"
		}
	}

	info, err := s.storage.GetFileInfo(c.Context(), objectKey)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "File not found"})
	}
	etagSeed := fmt.Sprintf("%s:%d:%d", objectKey, info.Size, info.LastModified.UnixNano())
	etagHash := sha256.Sum256([]byte(etagSeed))
	etag := fmt.Sprintf("\"%x\"", etagHash[:])
	lastModified := info.LastModified.UTC().Format(time.RFC1123)
	setMediaCacheHeaders := func() {
		c.Set("ETag", etag)
		c.Set("Last-Modified", lastModified)
		c.Set("Cache-Control", "public, max-age=31536000")
		c.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(objectKey)))
	}

	ifNoneMatch := c.Get("If-None-Match")
	if c.Get("Range") == "" && (ifNoneMatch == etag || strings.Contains(ifNoneMatch, etag)) {
		setMediaCacheHeaders()
		return c.SendStatus(fiber.StatusNotModified)
	}

	// Check for Range header (needed for video streaming)
	rangeHeader := c.Get("Range")
	if rangeHeader != "" {
		totalSize := info.Size

		// Parse range header: "bytes=start-end"
		rangeHeader = strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		var start, end int64
		if parts[0] != "" {
			fmt.Sscanf(parts[0], "%d", &start)
		}
		if len(parts) > 1 && parts[1] != "" {
			fmt.Sscanf(parts[1], "%d", &end)
		} else {
			// Serve chunks of 1MB max for streaming
			end = start + 1024*1024 - 1
			if end >= totalSize {
				end = totalSize - 1
			}
		}
		if end >= totalSize {
			end = totalSize - 1
		}

		length := end - start + 1
		data, err := s.storage.GetFileRange(c.Context(), objectKey, start, length)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to read file"})
		}

		c.Set("Content-Type", contentType)
		c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
		c.Set("Accept-Ranges", "bytes")
		c.Set("Content-Length", fmt.Sprintf("%d", len(data)))
		setMediaCacheHeaders()
		return c.Status(206).Send(data)
	}

	// Full file download
	data, err := s.storage.GetFile(c.Context(), objectKey)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "File not found"})
	}

	c.Set("Content-Type", contentType)
	c.Set("Accept-Ranges", "bytes")
	c.Set("Content-Length", fmt.Sprintf("%d", len(data)))
	setMediaCacheHeaders()
	return c.Send(data)
}

// --- Lead Handlers ---

func (s *Server) handleGetLeads(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Parse optional device_ids filter
	deviceIDs := c.Context().QueryArgs().PeekMulti("device_ids")
	var deviceUUIDs []uuid.UUID
	for _, did := range deviceIDs {
		if id, err := uuid.Parse(string(did)); err == nil {
			deviceUUIDs = append(deviceUUIDs, id)
		}
	}

	// Build cache key including device filter
	cacheKey := "leads:" + accountID.String()
	if len(deviceUUIDs) > 0 {
		for _, d := range deviceUUIDs {
			cacheKey += ":" + d.String()
		}
	}

	// Try Redis cache first
	if s.cache != nil {
		if cached, err := s.cache.Get(c.Context(), cacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	// --- Parallel: load leads + tags simultaneously ---
	var leads []*domain.Lead
	var leadsErr error
	tagMap := make(map[uuid.UUID][]*domain.Tag)
	var tagsErr error

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: load leads (slim — no notes/custom_fields for list)
	go func() {
		defer wg.Done()
		if len(deviceUUIDs) > 0 {
			rows, qErr := s.repos.DB().Query(c.Context(), `
				SELECT l.id, l.account_id, l.contact_id, l.jid,
				       COALESCE(c.custom_name, c.name, l.name), COALESCE(c.last_name, l.last_name), COALESCE(c.short_name, l.short_name),
				       COALESCE(c.phone, l.phone), COALESCE(c.email, l.email), COALESCE(c.company, l.company),
				       COALESCE(c.age, l.age), COALESCE(c.dni, l.dni), COALESCE(c.birth_date, l.birth_date), COALESCE(c.address, l.address),
				       COALESCE(NULLIF(c.distrito, ''), NULLIF(l.distrito, '')), COALESCE(NULLIF(c.ocupacion, ''), NULLIF(l.ocupacion, '')),
				       l.status, l.source, COALESCE(c.notes, l.notes),
				       l.tags, l.custom_fields, l.assigned_to, l.pipeline_id, l.stage_id, l.created_at, l.updated_at,
				       ps.name, ps.color, ps.position, l.kommo_id,
				       l.is_archived, l.archived_at, l.is_blocked, l.blocked_at, l.block_reason, l.kommo_deleted_at
				FROM leads l
				LEFT JOIN contacts c ON c.id = l.contact_id
				LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
				WHERE l.account_id = $1
				  AND l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($2))
				ORDER BY l.created_at DESC
			`, accountID, deviceUUIDs)
			if qErr != nil {
				leadsErr = qErr
				return
			}
			defer rows.Close()
			for rows.Next() {
				lead := &domain.Lead{}
				if scanErr := rows.Scan(
					&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName, &lead.Phone,
					&lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes, &lead.Tags,
					&lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID, &lead.CreatedAt, &lead.UpdatedAt,
					&lead.StageName, &lead.StageColor, &lead.StagePosition, &lead.KommoID,
					&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
				); scanErr != nil {
					leadsErr = scanErr
					return
				}
				leads = append(leads, lead)
			}
		} else {
			leads, leadsErr = s.services.Lead.GetByAccountID(c.Context(), accountID)
		}
	}()

	// Goroutine 2: load all tags for account's leads (via contact_tags)
	go func() {
		defer wg.Done()
		rows, err := s.repos.DB().Query(c.Context(), `
			SELECT l.id, t.id, t.account_id, t.name, t.color
			FROM leads l
			JOIN contact_tags ct ON ct.contact_id = l.contact_id
			JOIN tags t ON t.id = ct.tag_id
			WHERE l.account_id = $1
			ORDER BY t.name
		`, accountID)
		if err != nil {
			tagsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var leadID uuid.UUID
			t := &domain.Tag{}
			if err := rows.Scan(&leadID, &t.ID, &t.AccountID, &t.Name, &t.Color); err != nil {
				continue
			}
			tagMap[leadID] = append(tagMap[leadID], t)
		}
	}()

	wg.Wait()

	if leadsErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": leadsErr.Error()})
	}
	// tagsErr is non-fatal — leads still returned without tags
	if tagsErr != nil {
		log.Printf("[LEADS] Warning: failed to load tags: %v", tagsErr)
	}

	// Assign tags to leads
	for _, lead := range leads {
		lead.StructuredTags = tagMap[lead.ID]
	}

	// Optionally load custom field values via contact_id
	if c.QueryBool("include_custom_fields", false) && len(leads) > 0 {
		contactIDSet := make(map[uuid.UUID]bool)
		for _, lead := range leads {
			if lead.ContactID != nil {
				contactIDSet[*lead.ContactID] = true
			}
		}
		if len(contactIDSet) > 0 {
			contactIDs := make([]uuid.UUID, 0, len(contactIDSet))
			for cid := range contactIDSet {
				contactIDs = append(contactIDs, cid)
			}
			cfMap, cfErr := s.repos.CustomField.GetValuesByContacts(c.Context(), contactIDs)
			if cfErr == nil {
				for _, lead := range leads {
					if lead.ContactID != nil {
						lead.CustomFieldValues = cfMap[*lead.ContactID]
					}
				}
			}
		}
	}

	result := fiber.Map{"success": true, "leads": leads}

	// Store in Redis cache (60s TTL — longer to improve hit rate)
	if s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), cacheKey, data, 60*time.Second)
		}
	}

	return c.JSON(result)
}

// invalidateLeadsCache invalidates ALL cached leads keys for an account (base + device-filtered + paginated + detail)
func (s *Server) invalidateLeadsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.Del(context.Background(), "leads:"+accountID.String())
		_ = s.cache.DelPattern(context.Background(), "leads:"+accountID.String()+":*")
		_ = s.cache.DelPattern(context.Background(), "leads_paged:"+accountID.String()+":*")
		_ = s.cache.DelPattern(context.Background(), "leads_stage:"+accountID.String()+":*")
		_ = s.cache.DelPattern(context.Background(), "leads_list:"+accountID.String()+":*")
	}
}

// invalidateLeadDetailCache invalidates the detail + interactions cache for a specific lead
func (s *Server) invalidateLeadDetailCache(leadID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.Del(context.Background(), "lead_detail:"+leadID.String())
		_ = s.cache.DelPattern(context.Background(), "lead_interactions:"+leadID.String()+":*")
	}
}

// invalidatePipelinesCache invalidates the cached pipelines for an account
func (s *Server) invalidatePipelinesCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.Del(context.Background(), "pipelines:"+accountID.String())
	}
}

// invalidateContactsCache invalidates the cached contacts for an account
func (s *Server) invalidateContactsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "contacts:"+accountID.String()+":*")
	}
}

// invalidateEventsCache invalidates the cached events for an account
func (s *Server) invalidateEventsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "events:"+accountID.String()+":*")
	}
}

// invalidateTagsCache invalidates the cached tags for an account
func (s *Server) invalidateTagsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "tags:"+accountID.String()+":*")
	}
}

// invalidateTasksCache invalidates the cached tasks for an account
func (s *Server) invalidateTasksCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "tasks:"+accountID.String()+":*")
	}
}

// invalidateCampaignsCache invalidates the cached campaigns for an account
func (s *Server) invalidateCampaignsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "campaigns:"+accountID.String()+":*")
	}
}

// invalidateProgramsCache invalidates the cached programs for an account
func (s *Server) invalidateProgramsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "programs:"+accountID.String()+":*")
	}
}

// invalidateSurveysCache invalidates the cached surveys for an account
func (s *Server) invalidateSurveysCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "surveys:"+accountID.String()+":*")
	}
}

// invalidateDynamicsCache invalidates the cached dynamics for an account
func (s *Server) invalidateDynamicsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "dynamics:"+accountID.String()+":*")
	}
}

// invalidateAutomationsCache invalidates the cached automations for an account
func (s *Server) invalidateAutomationsCache(accountID uuid.UUID) {
	if s.cache != nil {
		_ = s.cache.DelPattern(context.Background(), "automations:"+accountID.String()+":*")
	}
}

// ─── Shared filter helpers ──────────────────────────────────────────────────

// addKommoSyncFilter appends a WHERE clause based on the kommo_sync query param.
// "kommo" = only leads that exist and are active in Kommo.
// "clarin" = only leads that are local-only or were deleted from Kommo.
// "all" (default) = no filter.
func addKommoSyncFilter(kommoSync string, whereClauses *[]string) {
	switch kommoSync {
	case "kommo":
		*whereClauses = append(*whereClauses, "l.kommo_id IS NOT NULL AND l.kommo_deleted_at IS NULL")
	case "clarin":
		*whereClauses = append(*whereClauses, "(l.kommo_id IS NULL OR l.kommo_deleted_at IS NOT NULL)")
	}
}

// addDateFilter parses date_field, date_from, date_to from the query and appends a date range WHERE clause.
// Allowed fields are validated by allowedFields map. tableAlias is the SQL alias (e.g. "l" for leads, "p" for participants).
func addDateFilter(c *fiber.Ctx, tableAlias string, allowedFields map[string]bool, whereClauses *[]string, args *[]interface{}, argIdx *int) {
	dateField := c.Query("date_field")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	if dateField == "" || (dateFrom == "" && dateTo == "") {
		return
	}
	if !allowedFields[dateField] {
		return
	}
	col := tableAlias + "." + dateField
	if dateFrom != "" {
		t, err := time.Parse(time.RFC3339, dateFrom)
		if err == nil {
			*whereClauses = append(*whereClauses, fmt.Sprintf("%s >= $%d", col, *argIdx))
			*args = append(*args, t)
			*argIdx++
		}
	}
	if dateTo != "" {
		t, err := time.Parse(time.RFC3339, dateTo)
		if err == nil {
			*whereClauses = append(*whereClauses, fmt.Sprintf("%s < $%d", col, *argIdx))
			*args = append(*args, t)
			*argIdx++
		}
	}
}

var leadDateFields = map[string]bool{"created_at": true, "updated_at": true}
var participantDateFields = map[string]bool{"created_at": true, "updated_at": true, "invited_at": true, "confirmed_at": true, "attended_at": true}

// buildTagFormulaSQL builds a WHERE sub-clause for formula-based tag filtering.
// Returns the SQL clause, updated args, and updated argIdx.
// Supports tag_mode=AND (leads must have ALL tags), OR (any tag), and exclude_tag_names.
func buildTagFormulaSQL(tagNames []string, excludeTagNames []string, tagMode string, accountID uuid.UUID, args []interface{}, argIdx int) (string, []interface{}, int) {
	var clauses []string

	if len(tagNames) > 0 {
		if tagMode == "AND" {
			// Lead must have ALL of the specified tag names (scoped to this account's tags)
			clauses = append(clauses, fmt.Sprintf(
				"l.contact_id IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.account_id = $%d AND t.name = ANY($%d) GROUP BY ct.contact_id HAVING COUNT(DISTINCT t.name) = $%d)",
				argIdx, argIdx+1, argIdx+2,
			))
			args = append(args, accountID, tagNames, len(tagNames))
			argIdx += 3
		} else {
			// OR mode (default): lead has at least one tag
			clauses = append(clauses, fmt.Sprintf(
				"l.contact_id IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.account_id = $%d AND t.name = ANY($%d))",
				argIdx, argIdx+1,
			))
			args = append(args, accountID, tagNames)
			argIdx += 2
		}
	}

	if len(excludeTagNames) > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"l.contact_id NOT IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.account_id = $%d AND t.name = ANY($%d))",
			argIdx, argIdx+1,
		))
		args = append(args, accountID, excludeTagNames)
		argIdx += 2
	}

	return strings.Join(clauses, " AND "), args, argIdx
}

// buildAdvancedFormulaSQL builds a WHERE sub-clause from a text formula.
// Returns the SQL clause, updated args, and updated argIdx.
func buildAdvancedFormulaSQL(formulaText string, accountID uuid.UUID, args []interface{}, argIdx int) (string, []interface{}, int, error) {
	ast, err := formula.Parse(formulaText)
	if err != nil {
		return "", args, argIdx, err
	}
	if ast == nil {
		return "", args, argIdx, nil
	}

	// Build the inner query using formula.BuildSQL (which uses $1 for accountID)
	innerSQL, innerArgs, err := formula.BuildSQL(ast, accountID)
	if err != nil {
		return "", args, argIdx, err
	}

	remappedSQL := formula.RemapSQLParams(innerSQL, len(innerArgs), argIdx)

	clause := fmt.Sprintf("l.id IN (%s)", remappedSQL)
	args = append(args, innerArgs...)
	argIdx += len(innerArgs)

	return clause, args, argIdx, nil
}

// buildAdvancedFormulaSQLAll is like buildAdvancedFormulaSQL but uses BuildSQLAll
// which does NOT filter by is_archived/is_blocked.
func buildAdvancedFormulaSQLAll(formulaText string, accountID uuid.UUID, args []interface{}, argIdx int) (string, []interface{}, int, error) {
	ast, err := formula.Parse(formulaText)
	if err != nil {
		return "", args, argIdx, err
	}
	if ast == nil {
		return "", args, argIdx, nil
	}
	innerSQL, innerArgs, err := formula.BuildSQLAll(ast, accountID)
	if err != nil {
		return "", args, argIdx, err
	}
	remappedSQL := formula.RemapSQLParams(innerSQL, len(innerArgs), argIdx)
	clause := fmt.Sprintf("l.id IN (%s)", remappedSQL)
	args = append(args, innerArgs...)
	argIdx += len(innerArgs)
	return clause, args, argIdx, nil
}

// handleGetLeadsPaginated returns leads grouped by stage with server-side filtering, first N per stage + total counts.
// This enables instant load for any number of leads (100K+) — only the first page per column is returned.
func (s *Server) handleGetLeadsPaginated(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Parse query params
	pipelineID := c.Query("pipeline_id")
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw := c.Query("tag_formula")
	stageIDsRaw := c.Query("stage_ids")
	perStage, _ := strconv.Atoi(c.Query("per_stage", "50"))
	if perStage <= 0 || perStage > 200 {
		perStage = 50
	}

	// Parse device_ids
	deviceIDs := c.Context().QueryArgs().PeekMulti("device_ids")
	var deviceUUIDs []uuid.UUID
	for _, did := range deviceIDs {
		if id, err := uuid.Parse(string(did)); err == nil {
			deviceUUIDs = append(deviceUUIDs, id)
		}
	}

	// Parse tag names
	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}

	// Parse stage_ids filter
	var stageIDs []string
	if stageIDsRaw != "" {
		stageIDs = strings.Split(stageIDsRaw, ",")
	}

	// Build WHERE clause dynamically
	args := []interface{}{accountID}
	argIdx := 2
	whereClauses := []string{"l.account_id = $1"}

	// Status filter: active (default), archived, blocked, all
	statusFilter := c.Query("status_filter", "active")
	switch statusFilter {
	case "archived":
		whereClauses = append(whereClauses, "l.is_archived = true AND l.is_blocked = false")
	case "blocked":
		whereClauses = append(whereClauses, "l.is_blocked = true")
	case "all":
		// no filter
	default: // active
		whereClauses = append(whereClauses, "l.is_archived = false AND l.is_blocked = false")
	}

	if pipelineID == "__no_pipeline__" {
		whereClauses = append(whereClauses, "l.pipeline_id IS NULL")
	} else if pipelineID != "" {
		if pid, err := uuid.Parse(pipelineID); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("l.pipeline_id = $%d", argIdx))
			args = append(args, pid)
			argIdx++
		}
	}

	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(c.name,l.name,'')) LIKE $%d OR LOWER(COALESCE(c.phone,l.phone,'')) LIKE $%d OR LOWER(COALESCE(c.email,l.email,'')) LIKE $%d OR LOWER(COALESCE(c.company,l.company,'')) LIKE $%d OR LOWER(COALESCE(c.last_name,l.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}

	if len(deviceUUIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($%d))", argIdx))
		args = append(args, deviceUUIDs)
		argIdx++
	}

	if tagFormulaRaw != "" {
		fSQL, newArgs, newIdx, fErr := buildAdvancedFormulaSQL(tagFormulaRaw, accountID, args, argIdx)
		if fErr != nil {
			log.Printf("[LEADS] Formula parse/build error: %v (formula: %s)", fErr, tagFormulaRaw)
		} else if fSQL != "" {
			whereClauses = append(whereClauses, fSQL)
			args = newArgs
			argIdx = newIdx
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		tagSQL, newArgs, newIdx := buildTagFormulaSQL(tagNames, excludeTagNames, tagMode, accountID, args, argIdx)
		if tagSQL != "" {
			whereClauses = append(whereClauses, tagSQL)
			args = newArgs
			argIdx = newIdx
		}
	}

	if len(stageIDs) > 0 {
		var validStageUUIDs []uuid.UUID
		for _, sid := range stageIDs {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageUUIDs = append(validStageUUIDs, id)
			}
		}
		if len(validStageUUIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("l.stage_id = ANY($%d)", argIdx))
			args = append(args, validStageUUIDs)
			argIdx++
		}
	}

	addDateFilter(c, "l", leadDateFields, &whereClauses, &args, &argIdx)
	addKommoSyncFilter(c.Query("kommo_sync", "all"), &whereClauses)

	whereSQL := strings.Join(whereClauses, " AND ")

	// --- Run 3 queries in parallel: stages, counts per stage, first N leads per stage + tags ---
	var wg sync.WaitGroup

	// 1. Get pipeline stages
	type stageInfo struct {
		ID         uuid.UUID
		PipelineID uuid.UUID
		Name       string
		Color      string
		Position   int
	}
	var stagesList []stageInfo
	var stagesErr error

	// 2. Counts per stage
	type stageCount struct {
		StageID uuid.UUID
		Count   int
	}
	var counts []stageCount
	var countsErr error

	// 3. First N leads per stage (window function)
	var paginatedLeads []*domain.Lead
	var leadsErr error

	// 4. Tags map
	tagMap := make(map[uuid.UUID][]*domain.Tag)
	var tagsErr error

	// 5. Unassigned count
	var unassignedCount int
	var unassignedErr error

	wg.Add(5)

	// Goroutine 1: pipeline stages
	go func() {
		defer wg.Done()
		if pipelineID == "" {
			return
		}
		pid, err := uuid.Parse(pipelineID)
		if err != nil {
			return
		}
		rows, err := s.repos.DB().Query(c.Context(),
			`SELECT id, pipeline_id, name, color, position FROM pipeline_stages WHERE pipeline_id = $1 ORDER BY position`,
			pid,
		)
		if err != nil {
			stagesErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var si stageInfo
			if err := rows.Scan(&si.ID, &si.PipelineID, &si.Name, &si.Color, &si.Position); err != nil {
				stagesErr = err
				return
			}
			stagesList = append(stagesList, si)
		}
	}()

	// Goroutine 2: count leads per stage
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`SELECT l.stage_id, COUNT(*) FROM leads l LEFT JOIN contacts c ON c.id = l.contact_id WHERE %s AND l.stage_id IS NOT NULL GROUP BY l.stage_id`, whereSQL)
		rows, err := s.repos.DB().Query(c.Context(), q, args...)
		if err != nil {
			countsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var sc stageCount
			if err := rows.Scan(&sc.StageID, &sc.Count); err != nil {
				countsErr = err
				return
			}
			counts = append(counts, sc)
		}
	}()

	// Goroutine 3: first N leads per stage using ROW_NUMBER() window function
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`
			WITH ranked AS (
				SELECT l.id, l.account_id, l.contact_id, l.jid,
				       COALESCE(c.custom_name, c.name, l.name) AS name, COALESCE(c.last_name, l.last_name) AS last_name, COALESCE(c.short_name, l.short_name) AS short_name,
				       COALESCE(c.phone, l.phone) AS phone, COALESCE(c.email, l.email) AS email, COALESCE(c.company, l.company) AS company,
				       COALESCE(c.age, l.age) AS age, COALESCE(c.dni, l.dni) AS dni, COALESCE(c.birth_date, l.birth_date) AS birth_date, COALESCE(c.address, l.address) AS address, COALESCE(NULLIF(c.distrito, ''), NULLIF(l.distrito, '')) AS distrito, COALESCE(NULLIF(c.ocupacion, ''), NULLIF(l.ocupacion, '')) AS ocupacion,
				       l.status, l.source, COALESCE(c.notes, l.notes) AS notes,
				       l.tags, l.custom_fields, l.assigned_to, l.pipeline_id, l.stage_id,
				       l.created_at, l.updated_at, l.kommo_id,
				       l.is_archived, l.archived_at, l.is_blocked, l.blocked_at, l.block_reason, l.kommo_deleted_at,
				       ps.name AS stage_name, ps.color AS stage_color, ps.position AS stage_position,
				       ROW_NUMBER() OVER (PARTITION BY l.stage_id ORDER BY l.created_at DESC) AS rn
				FROM leads l
				LEFT JOIN contacts c ON c.id = l.contact_id
				LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
				WHERE %s AND l.stage_id IS NOT NULL
			)
			SELECT id, account_id, contact_id, jid, name, last_name, short_name,
			       phone, email, company, age, dni, birth_date, address, distrito, ocupacion, status, source, notes,
			       tags, custom_fields, assigned_to, pipeline_id, stage_id,
			       created_at, updated_at, kommo_id,
			       is_archived, archived_at, is_blocked, blocked_at, block_reason, kommo_deleted_at,
			       stage_name, stage_color, stage_position
			FROM ranked WHERE rn <= %d
			ORDER BY stage_position NULLS LAST, created_at DESC
		`, whereSQL, perStage)
		rows, err := s.repos.DB().Query(c.Context(), q, args...)
		if err != nil {
			leadsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			lead := &domain.Lead{}
			if err := rows.Scan(
				&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName,
				&lead.Phone, &lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes,
				&lead.Tags, &lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID,
				&lead.CreatedAt, &lead.UpdatedAt, &lead.KommoID,
				&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
				&lead.StageName, &lead.StageColor, &lead.StagePosition,
			); err != nil {
				leadsErr = err
				return
			}
			paginatedLeads = append(paginatedLeads, lead)
		}
	}()

	// Goroutine 4: tags for account leads (via contact_tags)
	go func() {
		defer wg.Done()
		rows, err := s.repos.DB().Query(c.Context(), `
			SELECT l.id, t.id, t.account_id, t.name, t.color
			FROM leads l
			JOIN contact_tags ct ON ct.contact_id = l.contact_id
			JOIN tags t ON t.id = ct.tag_id
			WHERE l.account_id = $1
			ORDER BY t.name
		`, accountID)
		if err != nil {
			tagsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var leadID uuid.UUID
			t := &domain.Tag{}
			if err := rows.Scan(&leadID, &t.ID, &t.AccountID, &t.Name, &t.Color); err != nil {
				continue
			}
			tagMap[leadID] = append(tagMap[leadID], t)
		}
	}()

	// Goroutine 5: unassigned leads count + first N
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`SELECT COUNT(*) FROM leads l LEFT JOIN contacts c ON c.id = l.contact_id WHERE %s AND (l.stage_id IS NULL)`, whereSQL)
		err := s.repos.DB().QueryRow(c.Context(), q, args...).Scan(&unassignedCount)
		if err != nil {
			unassignedErr = err
		}
	}()

	wg.Wait()

	if leadsErr != nil {
		log.Printf("[LEADS] Paginated leads error: %v", leadsErr)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": leadsErr.Error()})
	}
	if countsErr != nil {
		log.Printf("[LEADS] Counts error: %v", countsErr)
	}
	if stagesErr != nil {
		log.Printf("[LEADS] Stages error: %v", stagesErr)
	}
	if tagsErr != nil {
		log.Printf("[LEADS] Tags error: %v", tagsErr)
	}
	if unassignedErr != nil {
		log.Printf("[LEADS] Unassigned error: %v", unassignedErr)
	}

	// Hidden by status: count leads matching filters (ignoring status) to show how many are hidden
	hiddenByStatus := 0
	hasFormulaOrTagFilter := tagFormulaRaw != "" || len(tagNames) > 0 || len(excludeTagNames) > 0
	if statusFilter != "all" && hasFormulaOrTagFilter {
		hArgs := []interface{}{accountID}
		hIdx := 2
		hClauses := []string{"l.account_id = $1"}
		// NO status filter — count ALL statuses
		if pipelineID == "__no_pipeline__" {
			hClauses = append(hClauses, "l.pipeline_id IS NULL")
		} else if pipelineID != "" {
			if pid, err := uuid.Parse(pipelineID); err == nil {
				hClauses = append(hClauses, fmt.Sprintf("l.pipeline_id = $%d", hIdx))
				hArgs = append(hArgs, pid)
				hIdx++
			}
		}
		if search != "" {
			searchPattern := "%" + strings.ToLower(search) + "%"
			hClauses = append(hClauses, fmt.Sprintf(
				"(LOWER(COALESCE(c.name,l.name,'')) LIKE $%d OR LOWER(COALESCE(c.phone,l.phone,'')) LIKE $%d OR LOWER(COALESCE(c.email,l.email,'')) LIKE $%d OR LOWER(COALESCE(c.company,l.company,'')) LIKE $%d OR LOWER(COALESCE(c.last_name,l.last_name,'')) LIKE $%d)",
				hIdx, hIdx, hIdx, hIdx, hIdx,
			))
			hArgs = append(hArgs, searchPattern)
			hIdx++
		}
		if len(deviceUUIDs) > 0 {
			hClauses = append(hClauses, fmt.Sprintf("l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($%d))", hIdx))
			hArgs = append(hArgs, deviceUUIDs)
			hIdx++
		}
		if tagFormulaRaw != "" {
			fSQL, newArgs, newIdx, fErr := buildAdvancedFormulaSQLAll(tagFormulaRaw, accountID, hArgs, hIdx)
			if fErr != nil {
				log.Printf("[LEADS] Formula parse/build error (hidden count): %v (formula: %s)", fErr, tagFormulaRaw)
			} else if fSQL != "" {
				hClauses = append(hClauses, fSQL)
				hArgs = newArgs
				hIdx = newIdx
			}
		} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
			tagSQL, newArgs, newIdx := buildTagFormulaSQL(tagNames, excludeTagNames, tagMode, accountID, hArgs, hIdx)
			if tagSQL != "" {
				hClauses = append(hClauses, tagSQL)
				hArgs = newArgs
				hIdx = newIdx
			}
		}
		if len(stageIDs) > 0 {
			var validStageUUIDs []uuid.UUID
			for _, sid := range stageIDs {
				if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
					validStageUUIDs = append(validStageUUIDs, id)
				}
			}
			if len(validStageUUIDs) > 0 {
				hClauses = append(hClauses, fmt.Sprintf("l.stage_id = ANY($%d)", hIdx))
				hArgs = append(hArgs, validStageUUIDs)
				hIdx++
			}
		}
		addDateFilter(c, "l", leadDateFields, &hClauses, &hArgs, &hIdx)
		addKommoSyncFilter(c.Query("kommo_sync", "all"), &hClauses)
		hWhereSQL := strings.Join(hClauses, " AND ")
		var totalAll int
		err := s.repos.DB().QueryRow(c.Context(),
			fmt.Sprintf("SELECT COUNT(*) FROM leads l LEFT JOIN contacts c ON c.id = l.contact_id WHERE %s", hWhereSQL),
			hArgs...,
		).Scan(&totalAll)
		if err == nil {
			visibleTotal := unassignedCount
			for _, sc := range counts {
				visibleTotal += sc.Count
			}
			hiddenByStatus = totalAll - visibleTotal
			if hiddenByStatus < 0 {
				hiddenByStatus = 0
			}
		}
	}

	// Assign tags to leads
	for _, lead := range paginatedLeads {
		lead.StructuredTags = tagMap[lead.ID]
	}

	// Build counts map
	countMap := make(map[uuid.UUID]int)
	for _, sc := range counts {
		countMap[sc.StageID] = sc.Count
	}

	// Build stages response with leads grouped
	type stageResponse struct {
		ID         uuid.UUID      `json:"id"`
		PipelineID uuid.UUID      `json:"pipeline_id"`
		Name       string         `json:"name"`
		Color      string         `json:"color"`
		Position   int            `json:"position"`
		TotalCount int            `json:"total_count"`
		Leads      []*domain.Lead `json:"leads"`
		HasMore    bool           `json:"has_more"`
	}

	stagesResp := make([]stageResponse, 0, len(stagesList))
	for _, si := range stagesList {
		sr := stageResponse{
			ID:         si.ID,
			PipelineID: si.PipelineID,
			Name:       si.Name,
			Color:      si.Color,
			Position:   si.Position,
			TotalCount: countMap[si.ID],
			Leads:      make([]*domain.Lead, 0),
		}
		for _, lead := range paginatedLeads {
			if lead.StageID != nil && *lead.StageID == si.ID {
				sr.Leads = append(sr.Leads, lead)
			}
		}
		sr.HasMore = sr.TotalCount > len(sr.Leads)
		stagesResp = append(stagesResp, sr)
	}

	// Unassigned leads (first N)
	unassignedLeads := make([]*domain.Lead, 0)
	for _, lead := range paginatedLeads {
		if lead.StageID == nil {
			unassignedLeads = append(unassignedLeads, lead)
		}
	}
	// If window function didn't catch unassigned (stage_id IS NOT NULL filter), fetch them separately
	if unassignedCount > 0 && len(unassignedLeads) == 0 {
		unassignedQ := fmt.Sprintf(`
			SELECT l.id, l.account_id, l.contact_id, l.jid,
			       COALESCE(c.custom_name, c.name, l.name), COALESCE(c.last_name, l.last_name), COALESCE(c.short_name, l.short_name),
			       COALESCE(c.phone, l.phone), COALESCE(c.email, l.email), COALESCE(c.company, l.company),
			       COALESCE(c.age, l.age), COALESCE(c.dni, l.dni), COALESCE(c.birth_date, l.birth_date), COALESCE(c.address, l.address),
			       COALESCE(NULLIF(c.distrito, ''), NULLIF(l.distrito, '')), COALESCE(NULLIF(c.ocupacion, ''), NULLIF(l.ocupacion, '')),
			       l.status, l.source, COALESCE(c.notes, l.notes),
			       l.tags, l.custom_fields, l.assigned_to, l.pipeline_id, l.stage_id,
			       l.created_at, l.updated_at, l.kommo_id,
			       l.is_archived, l.archived_at, l.is_blocked, l.blocked_at, l.block_reason, l.kommo_deleted_at
			FROM leads l
			LEFT JOIN contacts c ON c.id = l.contact_id
			WHERE %s AND l.stage_id IS NULL
			ORDER BY l.created_at DESC
			LIMIT %d
		`, whereSQL, perStage)
		rows, err := s.repos.DB().Query(c.Context(), unassignedQ, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				lead := &domain.Lead{}
				if err := rows.Scan(
					&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName,
					&lead.Phone, &lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes,
					&lead.Tags, &lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID,
					&lead.CreatedAt, &lead.UpdatedAt, &lead.KommoID,
					&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
				); err == nil {
					lead.StructuredTags = tagMap[lead.ID]
					unassignedLeads = append(unassignedLeads, lead)
				}
			}
		}
	}

	// Collect all unique tags for filter dropdown
	type tagInfo struct {
		Name  string `json:"name"`
		Color string `json:"color"`
		Count int    `json:"count"`
	}
	tagCountMap := make(map[string]*tagInfo)
	for _, tags := range tagMap {
		for _, t := range tags {
			if existing, ok := tagCountMap[t.Name]; ok {
				existing.Count++
			} else {
				tagCountMap[t.Name] = &tagInfo{Name: t.Name, Color: t.Color, Count: 1}
			}
		}
	}
	tagsList := make([]*tagInfo, 0, len(tagCountMap))
	for _, ti := range tagCountMap {
		tagsList = append(tagsList, ti)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"stages":  stagesResp,
		"unassigned": fiber.Map{
			"total_count": unassignedCount,
			"leads":       unassignedLeads,
			"has_more":    unassignedCount > len(unassignedLeads),
		},
		"all_tags":         tagsList,
		"hidden_by_status": hiddenByStatus,
	})
}

// handleGetLeadsByStage returns paginated leads for a single stage (used by infinite scroll)
func (s *Server) handleGetLeadsByStage(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	stageIDParam := c.Params("stageId")

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw2 := c.Query("tag_formula")
	pipelineID := c.Query("pipeline_id")

	// Parse device_ids
	deviceIDs := c.Context().QueryArgs().PeekMulti("device_ids")
	var deviceUUIDs []uuid.UUID
	for _, did := range deviceIDs {
		if id, err := uuid.Parse(string(did)); err == nil {
			deviceUUIDs = append(deviceUUIDs, id)
		}
	}

	// Build WHERE
	args := []interface{}{accountID}
	argIdx := 2
	whereClauses := []string{"l.account_id = $1"}

	// Status filter
	statusFilter2 := c.Query("status_filter", "active")
	switch statusFilter2 {
	case "archived":
		whereClauses = append(whereClauses, "l.is_archived = true AND l.is_blocked = false")
	case "blocked":
		whereClauses = append(whereClauses, "l.is_blocked = true")
	case "all":
		// no filter
	default:
		whereClauses = append(whereClauses, "l.is_archived = false AND l.is_blocked = false")
	}

	// Handle stage: "unassigned" or UUID
	isUnassigned := stageIDParam == "unassigned"
	if isUnassigned {
		whereClauses = append(whereClauses, "l.stage_id IS NULL")
	} else {
		if stageUUID, err := uuid.Parse(stageIDParam); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("l.stage_id = $%d", argIdx))
			args = append(args, stageUUID)
			argIdx++
		} else {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid stage_id"})
		}
	}

	if pipelineID == "__no_pipeline__" {
		whereClauses = append(whereClauses, "l.pipeline_id IS NULL")
	} else if pipelineID != "" {
		if pid, err := uuid.Parse(pipelineID); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("l.pipeline_id = $%d", argIdx))
			args = append(args, pid)
			argIdx++
		}
	}

	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(c.name,l.name,'')) LIKE $%d OR LOWER(COALESCE(c.phone,l.phone,'')) LIKE $%d OR LOWER(COALESCE(c.email,l.email,'')) LIKE $%d OR LOWER(COALESCE(c.company,l.company,'')) LIKE $%d OR LOWER(COALESCE(c.last_name,l.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}

	if len(deviceUUIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($%d))", argIdx))
		args = append(args, deviceUUIDs)
		argIdx++
	}

	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}
	if tagFormulaRaw2 != "" {
		fSQL, newArgs, newIdx, fErr := buildAdvancedFormulaSQL(tagFormulaRaw2, accountID, args, argIdx)
		if fErr != nil {
			log.Printf("[LEADS] Formula parse/build error (list): %v (formula: %s)", fErr, tagFormulaRaw2)
		} else if fSQL != "" {
			whereClauses = append(whereClauses, fSQL)
			args = newArgs
			argIdx = newIdx
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		tagSQL, newArgs, newIdx := buildTagFormulaSQL(tagNames, excludeTagNames, tagMode, accountID, args, argIdx)
		if tagSQL != "" {
			whereClauses = append(whereClauses, tagSQL)
			args = newArgs
			argIdx = newIdx
		}
	}

	addDateFilter(c, "l", leadDateFields, &whereClauses, &args, &argIdx)
	addKommoSyncFilter(c.Query("kommo_sync", "all"), &whereClauses)

	whereSQL := strings.Join(whereClauses, " AND ")

	// Query leads with OFFSET/LIMIT
	q := fmt.Sprintf(`
		SELECT l.id, l.account_id, l.contact_id, l.jid,
		       COALESCE(c.custom_name, c.name, l.name), COALESCE(c.last_name, l.last_name), COALESCE(c.short_name, l.short_name),
		       COALESCE(c.phone, l.phone), COALESCE(c.email, l.email), COALESCE(c.company, l.company),
		       COALESCE(c.age, l.age), COALESCE(c.dni, l.dni), COALESCE(c.birth_date, l.birth_date), COALESCE(c.address, l.address),
		       COALESCE(NULLIF(c.distrito, ''), NULLIF(l.distrito, '')), COALESCE(NULLIF(c.ocupacion, ''), NULLIF(l.ocupacion, '')),
		       l.status, l.source, COALESCE(c.notes, l.notes),
		       l.tags, l.custom_fields, l.assigned_to, l.pipeline_id, l.stage_id,
		       l.created_at, l.updated_at, l.kommo_id,
		       l.is_archived, l.archived_at, l.is_blocked, l.blocked_at, l.block_reason, l.kommo_deleted_at,
		       ps.name, ps.color, ps.position
		FROM leads l
		LEFT JOIN contacts c ON c.id = l.contact_id
		LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
		WHERE %s
		ORDER BY l.created_at DESC
		LIMIT %d OFFSET %d
	`, whereSQL, limit+1, offset) // +1 to detect has_more

	rows, err := s.repos.DB().Query(c.Context(), q, args...)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	leads := make([]*domain.Lead, 0)
	for rows.Next() {
		lead := &domain.Lead{}
		if err := rows.Scan(
			&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName,
			&lead.Phone, &lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes,
			&lead.Tags, &lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID,
			&lead.CreatedAt, &lead.UpdatedAt, &lead.KommoID,
			&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
			&lead.StageName, &lead.StageColor, &lead.StagePosition,
		); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		leads = append(leads, lead)
	}

	hasMore := len(leads) > limit
	if hasMore {
		leads = leads[:limit]
	}

	// Load tags for these leads (via contact_tags)
	if len(leads) > 0 {
		leadIDs := make([]uuid.UUID, len(leads))
		for i, l := range leads {
			leadIDs[i] = l.ID
		}
		tagRows, err := s.repos.DB().Query(c.Context(), `
			SELECT l.id, t.id, t.account_id, t.name, t.color
			FROM leads l
			JOIN contact_tags ct ON ct.contact_id = l.contact_id
			JOIN tags t ON t.id = ct.tag_id
			WHERE l.id = ANY($1)
			ORDER BY t.name
		`, leadIDs)
		if err == nil {
			defer tagRows.Close()
			tagMap := make(map[uuid.UUID][]*domain.Tag)
			for tagRows.Next() {
				var leadID uuid.UUID
				t := &domain.Tag{}
				if err := tagRows.Scan(&leadID, &t.ID, &t.AccountID, &t.Name, &t.Color); err == nil {
					tagMap[leadID] = append(tagMap[leadID], t)
				}
			}
			for _, lead := range leads {
				lead.StructuredTags = tagMap[lead.ID]
			}
		}
	}

	return c.JSON(fiber.Map{
		"success":  true,
		"leads":    leads,
		"has_more": hasMore,
	})
}

// handleGetLeadsListPaginated returns a flat paginated list of leads for the list view
func (s *Server) handleGetLeadsListPaginated(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 100000 {
		limit = 100000
	}
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw3 := c.Query("tag_formula")
	stageIDsRaw := c.Query("stage_ids")
	pipelineID := c.Query("pipeline_id")

	// Parse device_ids
	deviceIDs := c.Context().QueryArgs().PeekMulti("device_ids")
	var deviceUUIDs []uuid.UUID
	for _, did := range deviceIDs {
		if id, err := uuid.Parse(string(did)); err == nil {
			deviceUUIDs = append(deviceUUIDs, id)
		}
	}

	// Build WHERE
	args := []interface{}{accountID}
	argIdx := 2
	whereClauses := []string{"l.account_id = $1"}

	// Status filter
	statusFilter3 := c.Query("status_filter", "active")
	switch statusFilter3 {
	case "archived":
		whereClauses = append(whereClauses, "l.is_archived = true AND l.is_blocked = false")
	case "blocked":
		whereClauses = append(whereClauses, "l.is_blocked = true")
	case "all":
		// no filter
	default:
		whereClauses = append(whereClauses, "l.is_archived = false AND l.is_blocked = false")
	}

	if pipelineID == "__no_pipeline__" {
		whereClauses = append(whereClauses, "l.pipeline_id IS NULL")
	} else if pipelineID != "" {
		if pid, err := uuid.Parse(pipelineID); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("l.pipeline_id = $%d", argIdx))
			args = append(args, pid)
			argIdx++
		}
	}
	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(c.name,l.name,'')) LIKE $%d OR LOWER(COALESCE(c.phone,l.phone,'')) LIKE $%d OR LOWER(COALESCE(c.email,l.email,'')) LIKE $%d OR LOWER(COALESCE(c.company,l.company,'')) LIKE $%d OR LOWER(COALESCE(c.last_name,l.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}
	if len(deviceUUIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($%d))", argIdx))
		args = append(args, deviceUUIDs)
		argIdx++
	}
	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}
	if tagFormulaRaw3 != "" {
		fSQL, newArgs, newIdx, fErr := buildAdvancedFormulaSQL(tagFormulaRaw3, accountID, args, argIdx)
		if fErr != nil {
			log.Printf("[LEADS] Formula parse/build error (load-more): %v (formula: %s)", fErr, tagFormulaRaw3)
		} else if fSQL != "" {
			whereClauses = append(whereClauses, fSQL)
			args = newArgs
			argIdx = newIdx
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		tagSQL, newArgs, newIdx := buildTagFormulaSQL(tagNames, excludeTagNames, tagMode, accountID, args, argIdx)
		if tagSQL != "" {
			whereClauses = append(whereClauses, tagSQL)
			args = newArgs
			argIdx = newIdx
		}
	}
	if stageIDsRaw != "" {
		var validStageIDs []uuid.UUID
		for _, sid := range strings.Split(stageIDsRaw, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageIDs = append(validStageIDs, id)
			}
		}
		if len(validStageIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("l.stage_id = ANY($%d)", argIdx))
			args = append(args, validStageIDs)
			argIdx++
		}
	}

	addDateFilter(c, "l", leadDateFields, &whereClauses, &args, &argIdx)
	addKommoSyncFilter(c.Query("kommo_sync", "all"), &whereClauses)

	// Custom field filters for leads (via contact_id)
	if cfFilterRaw := c.Query("cf_filter"); cfFilterRaw != "" {
		var cfFilters []repository.CustomFieldFilterParam
		if err := json.Unmarshal([]byte(cfFilterRaw), &cfFilters); err == nil && len(cfFilters) > 0 {
			matchIDs, err := s.repos.CustomField.FindContactIDsByFilters(c.Context(), accountID, cfFilters)
			if err == nil {
				if len(matchIDs) == 0 {
					return c.JSON(fiber.Map{
						"success": true, "leads": []interface{}{}, "total": 0, "has_more": false,
					})
				}
				whereClauses = append(whereClauses, fmt.Sprintf("l.contact_id = ANY($%d)", argIdx))
				args = append(args, matchIDs)
				argIdx++
			}
		}
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	// Count + fetch in parallel
	var total int
	leads := make([]*domain.Lead, 0)
	var countErr, leadsErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`SELECT COUNT(*) FROM leads l LEFT JOIN contacts c ON c.id = l.contact_id WHERE %s`, whereSQL)
		countErr = s.repos.DB().QueryRow(c.Context(), q, args...).Scan(&total)
	}()

	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`
			SELECT l.id, l.account_id, l.contact_id, l.jid,
			       COALESCE(c.custom_name, c.name, l.name), COALESCE(c.last_name, l.last_name), COALESCE(c.short_name, l.short_name),
			       COALESCE(c.phone, l.phone), COALESCE(c.email, l.email), COALESCE(c.company, l.company),
			       COALESCE(c.age, l.age), COALESCE(c.dni, l.dni), COALESCE(c.birth_date, l.birth_date), COALESCE(c.address, l.address),
			       COALESCE(NULLIF(c.distrito, ''), NULLIF(l.distrito, '')), COALESCE(NULLIF(c.ocupacion, ''), NULLIF(l.ocupacion, '')),
			       l.status, l.source, COALESCE(c.notes, l.notes),
			       l.tags, l.custom_fields, l.assigned_to, l.pipeline_id, l.stage_id,
			       l.created_at, l.updated_at, l.kommo_id,
			       l.is_archived, l.archived_at, l.is_blocked, l.blocked_at, l.block_reason, l.kommo_deleted_at,
			       ps.name, ps.color, ps.position
			FROM leads l
			LEFT JOIN contacts c ON c.id = l.contact_id
			LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
			WHERE %s
			ORDER BY l.updated_at DESC
			LIMIT %d OFFSET %d
		`, whereSQL, limit, offset)
		rows, err := s.repos.DB().Query(c.Context(), q, args...)
		if err != nil {
			leadsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			lead := &domain.Lead{}
			if err := rows.Scan(
				&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName,
				&lead.Phone, &lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes,
				&lead.Tags, &lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID,
				&lead.CreatedAt, &lead.UpdatedAt, &lead.KommoID,
				&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
				&lead.StageName, &lead.StageColor, &lead.StagePosition,
			); err != nil {
				leadsErr = err
				return
			}
			leads = append(leads, lead)
		}
	}()

	wg.Wait()

	if leadsErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": leadsErr.Error()})
	}
	if countErr != nil {
		log.Printf("[LEADS] List count error: %v", countErr)
	}

	// Load tags (via contact_tags)
	if len(leads) > 0 {
		leadIDs := make([]uuid.UUID, len(leads))
		for i, l := range leads {
			leadIDs[i] = l.ID
		}
		tagRows, err := s.repos.DB().Query(c.Context(), `
			SELECT l.id, t.id, t.account_id, t.name, t.color
			FROM leads l
			JOIN contact_tags ct ON ct.contact_id = l.contact_id
			JOIN tags t ON t.id = ct.tag_id
			WHERE l.id = ANY($1)
			ORDER BY t.name
		`, leadIDs)
		if err == nil {
			defer tagRows.Close()
			tagMap := make(map[uuid.UUID][]*domain.Tag)
			for tagRows.Next() {
				var leadID uuid.UUID
				t := &domain.Tag{}
				if err := tagRows.Scan(&leadID, &t.ID, &t.AccountID, &t.Name, &t.Color); err == nil {
					tagMap[leadID] = append(tagMap[leadID], t)
				}
			}
			for _, lead := range leads {
				lead.StructuredTags = tagMap[lead.ID]
			}
		}
	}

	return c.JSON(fiber.Map{
		"success":  true,
		"leads":    leads,
		"total":    total,
		"has_more": offset+len(leads) < total,
	})
}
func (s *Server) broadcastLeadDelta(accountID uuid.UUID, action string, lead *domain.Lead) {
	if s.hub == nil {
		return
	}
	payload := map[string]interface{}{
		"action": action,
	}
	if lead != nil {
		payload["lead"] = lead
	}
	s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, payload)
}

func (s *Server) handleCreateLead(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		Name      string     `json:"name"`
		Phone     string     `json:"phone"`
		Email     string     `json:"email"`
		Source    string     `json:"source"`
		Notes     string     `json:"notes"`
		DNI       string     `json:"dni"`
		BirthDate *string    `json:"birth_date"`
		Address   string     `json:"address"`
		Distrito  string     `json:"distrito"`
		Ocupacion string     `json:"ocupacion"`
		StageID   *uuid.UUID `json:"stage_id"`
		Tags      []string   `json:"tags"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	phone := kommo.NormalizePhone(req.Phone)
	jid := ""
	if phone != "" {
		jid = phone + "@s.whatsapp.net"
		// Check if a lead with this phone already exists
		existingLead, _ := s.services.Lead.GetByJID(c.Context(), accountID, jid)
		if existingLead != nil {
			existingName := ""
			if existingLead.Name != nil {
				existingName = *existingLead.Name
			}
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"error":   fmt.Sprintf("Ya existe un lead con el teléfono %s (%s)", req.Phone, existingName),
			})
		}
	} else {
		// Leads without phone get a unique JID to avoid conflicts
		jid = fmt.Sprintf("manual_%s@clarin.lead", uuid.New().String()[:8])
	}

	// Parse birth_date if provided
	var birthDate *time.Time
	if req.BirthDate != nil && *req.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", *req.BirthDate); err == nil {
			birthDate = &t
		}
	}

	lead := &domain.Lead{
		AccountID: accountID,
		JID:       jid,
		Name:      strPtr(req.Name),
		Phone:     strPtr(req.Phone),
		Email:     strPtr(req.Email),
		Source:    strPtr(req.Source),
		Notes:     strPtr(req.Notes),
		DNI:       strPtr(req.DNI),
		BirthDate: birthDate,
		Address:   strPtr(req.Address),
		Distrito:  strPtr(req.Distrito),
		Ocupacion: strPtr(req.Ocupacion),
		Status:    strPtr(domain.LeadStatusNew),
	}

	// Auto-assign pipeline and stage. An explicit stage is an override, but it
	// must belong to this account; otherwise use the account incoming default.
	if req.StageID != nil {
		pipelineID, stageID, err := s.repos.Pipeline.ResolveStageDestination(c.Context(), accountID, *req.StageID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		if pipelineID == nil || stageID == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid stage_id"})
		}
		lead.PipelineID = pipelineID
		lead.StageID = stageID
	} else {
		pipelineID, stageID, err := s.repos.Pipeline.ResolveIncomingLeadDestination(c.Context(), accountID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		lead.PipelineID = pipelineID
		lead.StageID = stageID
	}

	// Auto-link or auto-create contact by JID
	contact, _ := s.repos.Contact.GetByJID(c.Context(), accountID, jid)
	if contact != nil {
		lead.ContactID = &contact.ID
		// Copy contact fields to lead if lead fields are empty
		if lead.Name == nil || *lead.Name == "" {
			dn := contact.DisplayName()
			lead.Name = &dn
		}
		if (lead.Phone == nil || *lead.Phone == "") && contact.Phone != nil {
			lead.Phone = contact.Phone
		}
		if (lead.Email == nil || *lead.Email == "") && contact.Email != nil {
			lead.Email = contact.Email
		}
		if lead.DNI == nil || *lead.DNI == "" {
			lead.DNI = contact.DNI
		}
		if lead.BirthDate == nil {
			lead.BirthDate = contact.BirthDate
		}
		if lead.Address == nil || *lead.Address == "" {
			lead.Address = contact.Address
		}
		lead.LastName = contact.LastName
		lead.ShortName = contact.ShortName
		lead.Company = contact.Company
		lead.Age = contact.Age

		// Propagate new lead data back to existing contact
		contact.Email = lead.Email
		contact.Notes = lead.Notes
		contact.DNI = lead.DNI
		contact.BirthDate = lead.BirthDate
		contact.Address = lead.Address
		if lead.Name != nil && *lead.Name != "" {
			contact.CustomName = lead.Name
		}
		_ = s.repos.Contact.Update(c.Context(), contact)
	} else {
		// Auto-create contact from lead data (for both real phone and manual leads)
		contact, _ = s.repos.Contact.GetOrCreate(c.Context(), accountID, nil, jid, phone, req.Name, "", false)
		if contact != nil {
			lead.ContactID = &contact.ID
			// Update contact with extra fields from lead
			contact.Email = lead.Email
			contact.Notes = lead.Notes
			contact.Source = strPtr("manual")
			contact.DNI = lead.DNI
			contact.BirthDate = lead.BirthDate
			contact.Address = lead.Address
			if req.Name != "" {
				contact.CustomName = &req.Name
			}
			_ = s.repos.Contact.Update(c.Context(), contact)
			log.Printf("[API] Auto-created contact %s for new lead (jid=%s)", contact.ID, jid)
		}
	}

	if err := s.services.Lead.Create(c.Context(), lead); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Assign tags if provided
	if len(req.Tags) > 0 {
		if err := s.repos.Tag.SyncLeadTagsByNames(c.Context(), accountID, lead.ID, req.Tags); err != nil {
			log.Printf("[API] Failed to sync tags for new lead %s: %v", lead.ID, err)
		}
	}

	// Push new lead to Kommo (async, only if pipeline is Kommo-connected)
	if kommoSync := s.kommoForAccount(c.Context(), accountID); kommoSync != nil {
		go kommoSync.PushNewLead(accountID, lead.ID)
	}

	s.invalidateLeadsCache(accountID)
	s.broadcastLeadDelta(accountID, "created", lead)

	// Fire lead_created automation trigger
	s.triggerAutomationLeadCreated(accountID, lead.ID)

	return c.Status(201).JSON(fiber.Map{"success": true, "lead": lead})
}

func (s *Server) handleGetLead(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	lead, err := s.services.Lead.GetByID(c.Context(), leadID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if lead == nil || lead.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Lead not found"})
	}

	// Try Redis cache for enriched response (tags)
	cacheKey := "lead_detail:" + leadID.String()
	if s.cache != nil {
		if cached, err := s.cache.Get(c.Context(), cacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	tags, _ := s.services.Tag.GetByEntity(c.Context(), "lead", lead.ID)
	lead.StructuredTags = tags

	result := fiber.Map{"success": true, "lead": lead}
	if s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), cacheKey, data, 30*time.Second)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleSyncLeadFromKommo(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Kommo integration not configured"})
	}
	if lead, _ := s.services.Lead.GetByID(c.Context(), leadID); lead == nil || lead.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Lead not found"})
	}

	if err := kommoSync.SyncSingleLead(c.Context(), accountID, leadID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Invalidate cache after sync
	s.invalidateLeadsCache(accountID)
	s.invalidateLeadDetailCache(leadID)

	// Return the updated lead
	lead, err := s.services.Lead.GetByID(c.Context(), leadID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	tags, _ := s.services.Tag.GetByEntity(c.Context(), "lead", lead.ID)
	lead.StructuredTags = tags

	return c.JSON(fiber.Map{"success": true, "lead": lead})
}

func (s *Server) handleUpdateLead(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	// Get existing lead
	lead, err := s.services.Lead.GetByID(c.Context(), leadID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if lead == nil || lead.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Lead not found"})
	}

	// Track old name for Kommo push (before overwriting)
	var oldName string
	if lead.Name != nil {
		oldName = *lead.Name
	}

	// Parse update request
	var req struct {
		Name         *string                `json:"name"`
		LastName     *string                `json:"last_name"`
		ShortName    *string                `json:"short_name"`
		Phone        *string                `json:"phone"`
		Email        *string                `json:"email"`
		Company      *string                `json:"company"`
		Age          *int                   `json:"age"`
		DNI          *string                `json:"dni"`
		BirthDate    *string                `json:"birth_date"`
		Address      *string                `json:"address"`
		Distrito     *string                `json:"distrito"`
		Ocupacion    *string                `json:"ocupacion"`
		Status       *string                `json:"status"`
		Source       *string                `json:"source"`
		Notes        *string                `json:"notes"`
		Tags         []string               `json:"tags"`
		CustomFields map[string]interface{} `json:"custom_fields"`
		AssignedTo   *string                `json:"assigned_to"`
		StageID      *string                `json:"stage_id"`
		PipelineID   *string                `json:"pipeline_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	// Update fields if provided
	if req.Name != nil {
		if *req.Name == "" {
			lead.Name = nil
		} else {
			lead.Name = req.Name
		}
	}
	if req.LastName != nil {
		if *req.LastName == "" {
			lead.LastName = nil
		} else {
			lead.LastName = req.LastName
		}
	}
	if req.ShortName != nil {
		if *req.ShortName == "" {
			lead.ShortName = nil
		} else {
			lead.ShortName = req.ShortName
		}
	}
	if req.Phone != nil {
		if *req.Phone == "" {
			lead.Phone = nil
		} else {
			lead.Phone = req.Phone
		}
	}
	if req.Email != nil {
		if *req.Email == "" {
			lead.Email = nil
		} else {
			lead.Email = req.Email
		}
	}
	if req.Company != nil {
		if *req.Company == "" {
			lead.Company = nil
		} else {
			lead.Company = req.Company
		}
	}
	if req.Age != nil {
		if *req.Age == 0 {
			lead.Age = nil
		} else {
			lead.Age = req.Age
		}
	}
	if req.DNI != nil {
		if *req.DNI == "" {
			lead.DNI = nil
		} else {
			lead.DNI = req.DNI
		}
	}
	if req.BirthDate != nil {
		if *req.BirthDate == "" {
			lead.BirthDate = nil
		} else if t, err := time.Parse("2006-01-02", *req.BirthDate); err == nil {
			lead.BirthDate = &t
		}
	}
	if req.Address != nil {
		if *req.Address == "" {
			lead.Address = nil
		} else {
			lead.Address = req.Address
		}
	}
	if req.Distrito != nil {
		if *req.Distrito == "" {
			lead.Distrito = nil
		} else {
			lead.Distrito = req.Distrito
		}
	}
	if req.Ocupacion != nil {
		if *req.Ocupacion == "" {
			lead.Ocupacion = nil
		} else {
			lead.Ocupacion = req.Ocupacion
		}
	}
	if req.Status != nil {
		if *req.Status == "" {
			lead.Status = nil
		} else {
			lead.Status = req.Status
		}
	}
	if req.Source != nil {
		if *req.Source == "" {
			lead.Source = nil
		} else {
			lead.Source = req.Source
		}
	}
	if req.Notes != nil {
		if *req.Notes == "" {
			lead.Notes = nil
		} else {
			lead.Notes = req.Notes
		}
	}
	if req.Tags != nil {
		lead.Tags = req.Tags
	}
	if req.CustomFields != nil {
		lead.CustomFields = req.CustomFields
	}
	if req.AssignedTo != nil {
		if *req.AssignedTo == "" {
			lead.AssignedTo = nil
		} else if uid, err := uuid.Parse(*req.AssignedTo); err == nil {
			lead.AssignedTo = &uid
		}
	}
	if req.StageID != nil {
		if *req.StageID == "" {
			lead.StageID = nil
		} else if uid, err := uuid.Parse(*req.StageID); err == nil {
			lead.StageID = &uid
		}
	}
	if req.PipelineID != nil {
		if *req.PipelineID == "" {
			lead.PipelineID = nil
		} else if uid, err := uuid.Parse(*req.PipelineID); err == nil {
			lead.PipelineID = &uid
		}
	}

	if err := s.services.Lead.Update(c.Context(), lead); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Re-fetch lead to get updated JOIN fields (stage_name, stage_color, stage_position)
	if req.StageID != nil || req.PipelineID != nil {
		if refreshed, err := s.services.Lead.GetByID(c.Context(), lead.ID); err == nil && refreshed != nil {
			lead.StageName = refreshed.StageName
			lead.StageColor = refreshed.StageColor
			lead.StagePosition = refreshed.StagePosition
			lead.PipelineID = refreshed.PipelineID
			lead.StageID = refreshed.StageID
		}
	}

	// Sync shared fields to linked contact
	_ = s.services.Lead.SyncToContact(c.Context(), lead)

	// Propagate name changes to event_participants and campaign_recipients
	if lead.ContactID != nil {
		if contact, _ := s.repos.Contact.GetByID(c.Context(), *lead.ContactID); contact != nil {
			_ = s.services.Contact.SyncToParticipants(c.Context(), contact)
		}
	}

	// Kommo Sync
	if kommoSync := s.kommoForAccount(c.Context(), lead.AccountID); kommoSync != nil {
		// If lead is not linked to Kommo yet, try to create it there (PushNewLead handles checks)
		if lead.KommoID == nil || *lead.KommoID == 0 {
			go kommoSync.PushNewLead(lead.AccountID, lead.ID)
		} else {
			// Already linked, push updates (batched via outbox when enabled)
			queuedContactProfile := false
			if req.Name != nil {
				newName := ""
				if lead.Name != nil {
					newName = *lead.Name
				}
				if newName != oldName {
					kommoSync.EnqueuePushLeadName(lead.AccountID, lead.ID)
					queuedContactProfile = true
				}
			}
			if !queuedContactProfile && lead.ContactID != nil && (req.Age != nil || req.DNI != nil || req.BirthDate != nil || req.Ocupacion != nil) {
				kommoSync.EnqueuePushContactProfile(lead.AccountID, *lead.ContactID)
			}
			// Push pipeline/stage change
			if req.PipelineID != nil || req.StageID != nil {
				kommoSync.EnqueuePushLeadStage(lead.AccountID, lead.ID)
			}
		}
	}

	// Auto-sync to Google Contacts if linked contact is synced
	if s.googleClient != nil && lead.ContactID != nil {
		go func() {
			contact, err := s.repos.Contact.GetByID(context.Background(), *lead.ContactID)
			if err == nil && contact != nil && contact.GoogleSync {
				log.Printf("[GOOGLE] Auto-sync triggered from handleUpdateLead for lead %s → contact %s (google_sync=%v)", lead.ID, contact.ID, contact.GoogleSync)
				if _, err := s.syncContactToGoogle(context.Background(), lead.AccountID, contact.ID); err != nil {
					log.Printf("[GOOGLE] Auto-sync from lead %s failed: %v", lead.ID, err)
				}
			} else if err != nil {
				log.Printf("[GOOGLE] Auto-sync skipped: contact load error: %v", err)
			} else if contact != nil && !contact.GoogleSync {
				log.Printf("[GOOGLE] Auto-sync skipped for lead %s → contact %s: google_sync=false", lead.ID, contact.ID)
			}
		}()
	}

	// Populate structured_tags before responding
	tags, err := s.repos.Tag.GetByLead(c.Context(), lead.ID)
	if err == nil {
		lead.StructuredTags = tags
	}

	s.invalidateLeadsCache(lead.AccountID)
	s.invalidateLeadDetailCache(lead.ID)
	s.broadcastLeadDelta(lead.AccountID, "updated", lead)
	return c.JSON(fiber.Map{"success": true, "lead": lead})
}

func (s *Server) handleUpdateLeadStatus(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}
	if lead, _ := s.services.Lead.GetByID(c.Context(), leadID); lead == nil || lead.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Lead not found"})
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if err := s.services.Lead.UpdateStatus(c.Context(), leadID, req.Status); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateLeadsCache(c.Locals("account_id").(uuid.UUID))
	s.invalidateLeadDetailCache(leadID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleDeleteLead(c *fiber.Ctx) error {
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	accountID := c.Locals("account_id").(uuid.UUID)

	// If delete_from_kommo=true, enqueue a "Perdido" (status 143) move in Kommo.
	// The outbox will flush it in batch; the local delete below runs immediately.
	if c.Query("delete_from_kommo") == "true" {
		kommoSync := s.kommoForAccount(c.Context(), accountID)
		if kommoSync == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Kommo integration not configured"})
		}
		var kommoLeadID *int64
		var kommoPipelineID *int64
		err := s.repos.DB().QueryRow(c.Context(), `
			SELECT l.kommo_id, p.kommo_id
			FROM leads l
			LEFT JOIN pipelines p ON l.pipeline_id = p.id
			WHERE l.id = $1 AND l.account_id = $2
		`, leadID, accountID).Scan(&kommoLeadID, &kommoPipelineID)
		if err != nil || kommoLeadID == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Lead no está vinculado a Kommo"})
		}
		if kommoPipelineID == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Pipeline no está vinculado a Kommo"})
		}
		kommoSync.EnqueuePushLeadStageForced(accountID, leadID, *kommoLeadID, 143, *kommoPipelineID)
		log.Printf("[DELETE+KOMMO] Lead %s (Kommo %d) enqueued move to Perdido (143) in Kommo pipeline %d", leadID, *kommoLeadID, *kommoPipelineID)
	}

	// Transfer orphaned interactions to the lead's contact before deleting
	_, _ = s.repos.DB().Exec(c.Context(), `
		UPDATE interactions SET contact_id = l.contact_id
		FROM leads l
		WHERE interactions.lead_id = $1
		  AND l.id = $1
		  AND interactions.contact_id IS NULL
		  AND l.contact_id IS NOT NULL
	`, leadID)

	if err := s.services.Lead.Delete(c.Context(), leadID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateLeadsCache(accountID)
	s.invalidateLeadDetailCache(leadID)
	// Broadcast delete with just the ID
	deletedLead := &domain.Lead{ID: leadID}
	s.broadcastLeadDelta(accountID, "deleted", deletedLead)
	return c.JSON(fiber.Map{"success": true, "message": "Lead deleted"})
}

func (s *Server) handleDeleteLeadsBatch(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	deleteFromKommo := c.Query("delete_from_kommo") == "true"

	var req struct {
		IDs       []string `json:"ids"`
		DeleteAll bool     `json:"delete_all"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if req.DeleteAll {
		// Transfer all orphaned interactions to their contacts before bulk delete
		_, _ = s.repos.DB().Exec(c.Context(), `
			UPDATE interactions SET contact_id = l.contact_id
			FROM leads l
			WHERE interactions.lead_id = l.id
			  AND l.account_id = $1
			  AND interactions.contact_id IS NULL
			  AND l.contact_id IS NOT NULL
		`, accountID)
		if err := s.services.Lead.DeleteAll(c.Context(), accountID); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		s.invalidateLeadsCache(accountID)
		return c.JSON(fiber.Map{"success": true, "message": "All leads deleted"})
	}

	if len(req.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No IDs provided"})
	}

	var uuids []uuid.UUID
	for _, id := range req.IDs {
		if uid, err := uuid.Parse(id); err == nil {
			uuids = append(uuids, uid)
		}
	}

	if len(uuids) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No valid IDs provided"})
	}

	// If delete_from_kommo=true, enqueue "Perdido" moves for synced leads in Kommo
	if deleteFromKommo {
		kommoSync := s.kommoForAccount(c.Context(), accountID)
		if kommoSync == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Kommo integration not configured"})
		}
		rows, err := s.repos.DB().Query(c.Context(), `
			SELECT l.id, l.kommo_id, p.kommo_id
			FROM leads l
			LEFT JOIN pipelines p ON l.pipeline_id = p.id
			WHERE l.id = ANY($1) AND l.account_id = $2
			  AND l.kommo_id IS NOT NULL AND l.kommo_deleted_at IS NULL
		`, uuids, accountID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var leadID uuid.UUID
				var kommoLeadID, kommoPipelineID *int64
				if err := rows.Scan(&leadID, &kommoLeadID, &kommoPipelineID); err != nil {
					continue
				}
				if kommoLeadID == nil || kommoPipelineID == nil {
					continue
				}
				kommoSync.EnqueuePushLeadStageForced(accountID, leadID, *kommoLeadID, 143, *kommoPipelineID)
				log.Printf("[DELETE+KOMMO BATCH] Lead %s (Kommo %d) enqueued move to Perdido (143) in Kommo pipeline %d", leadID, *kommoLeadID, *kommoPipelineID)
			}
		}
	}

	// Transfer orphaned interactions to contacts before batch delete
	_, _ = s.repos.DB().Exec(c.Context(), `
		UPDATE interactions SET contact_id = l.contact_id
		FROM leads l
		WHERE interactions.lead_id = ANY($1)
		  AND interactions.lead_id = l.id
		  AND interactions.contact_id IS NULL
		  AND l.contact_id IS NOT NULL
	`, uuids)

	if err := s.services.Lead.DeleteBatch(c.Context(), uuids); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateLeadsCache(accountID)
	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("%d leads deleted", len(uuids))})
}

// --- Archive & Block Handlers ---

// desyncGoogleForLeadIDs resolves lead IDs → contacts with google_sync=true, deletes from Google API, and clears local sync flags.
// Designed to be called from a goroutine (synchronous internally).
func (s *Server) desyncGoogleForLeadIDs(accountID uuid.UUID, leadIDs []uuid.UUID) {
	if s.googleClient == nil || len(leadIDs) == 0 {
		return
	}
	ctx := context.Background()

	contactIDs, err := s.repos.Contact.GetContactIDsFromLeadIDs(ctx, accountID, leadIDs)
	if err != nil || len(contactIDs) == 0 {
		return
	}

	contacts, err := s.repos.Contact.GetContactsByIDs(ctx, accountID, contactIDs)
	if err != nil || len(contacts) == 0 {
		return
	}

	// Fetch Google tokens once for the account
	_, accessToken, refreshToken, _, tokErr := s.repos.Account.GetGoogleTokens(ctx, accountID)
	var validToken string
	if tokErr == nil && accessToken != "" {
		validToken, _ = s.ensureValidToken(ctx, accountID, accessToken, refreshToken)
	}

	for _, c := range contacts {
		if !c.GoogleSync {
			continue
		}
		// Delete from Google if resource name exists
		if validToken != "" && c.GoogleResourceName != nil && *c.GoogleResourceName != "" {
			if err := s.googleClient.DeleteContact(ctx, validToken, *c.GoogleResourceName); err != nil {
				log.Printf("[GOOGLE] Error deleting contact %s from Google on archive/block: %v", c.ID, err)
			}
		}
		if err := s.repos.Contact.ClearGoogleSync(ctx, c.ID); err != nil {
			log.Printf("[GOOGLE] Error clearing sync for contact %s on archive/block: %v", c.ID, err)
		} else {
			log.Printf("[GOOGLE] Auto-desynced contact %s due to lead archive/block", c.ID)
		}
	}
}

// logArchiveBlockObservation creates an auto-observation when a lead is archived or blocked.
// Designed to be called from a goroutine.
func (s *Server) logArchiveBlockObservation(accountID, userID, leadID uuid.UUID, actionLabel, reason string) {
	ctx := context.Background()
	userName := ""
	if u, err := s.repos.User.GetByID(ctx, userID); err == nil && u != nil {
		userName = u.DisplayName
		if userName == "" {
			userName = u.Email
		}
	}
	notes := fmt.Sprintf("%s por %s. Motivo: %s", actionLabel, userName, reason)
	interaction := &domain.Interaction{
		AccountID: accountID,
		LeadID:    &leadID,
		Type:      "note",
		Notes:     &notes,
		CreatedBy: &userID,
	}
	// Auto-link contact_id
	var contactID *uuid.UUID
	_ = s.repos.DB().QueryRow(ctx, `SELECT contact_id FROM leads WHERE id = $1`, leadID).Scan(&contactID)
	if contactID != nil {
		interaction.ContactID = contactID
	}
	if err := s.services.Interaction.LogInteraction(ctx, interaction); err != nil {
		log.Printf("[ARCHIVE/BLOCK] Error creating observation: %v", err)
	}
	// Broadcast so the detail panel refreshes observations
	s.hub.BroadcastToAccount(accountID, ws.EventInteractionUpdate, map[string]interface{}{
		"action":  "created",
		"lead_id": leadID.String(),
	})
}

func (s *Server) handleArchiveLead(c *fiber.Ctx) error {
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	var req struct {
		Archive bool   `json:"archive"`
		Reason  string `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if err := s.services.Lead.ArchiveLead(c.Context(), leadID, req.Archive, req.Reason); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)
	s.invalidateLeadsCache(accountID)
	s.invalidateLeadDetailCache(leadID)
	action := "archived"
	if !req.Archive {
		action = "unarchived"
	}
	s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
		"action":  action,
		"lead_id": leadID.String(),
	})
	// Broadcast event participant update so event pages refresh
	if req.Archive {
		s.hub.BroadcastToAccount(accountID, ws.EventEventParticipantUpdate, map[string]interface{}{
			"action":  "lead_archived",
			"lead_id": leadID.String(),
		})
		go s.desyncGoogleForLeadIDs(accountID, []uuid.UUID{leadID})
		// Auto-create observation note
		if req.Reason != "" {
			go s.logArchiveBlockObservation(accountID, userID, leadID, "📦 Archivado", req.Reason)
		}
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleBlockLead(c *fiber.Ctx) error {
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	var req struct {
		Block  bool   `json:"block"`
		Reason string `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if req.Block && req.Reason == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Reason is required when blocking"})
	}

	if err := s.services.Lead.BlockLead(c.Context(), leadID, req.Block, req.Reason); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)
	s.invalidateLeadsCache(accountID)
	s.invalidateLeadDetailCache(leadID)
	action := "blocked"
	if !req.Block {
		action = "unblocked"
	}
	s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
		"action":  action,
		"lead_id": leadID.String(),
	})
	if req.Block {
		go s.desyncGoogleForLeadIDs(accountID, []uuid.UUID{leadID})
		// Auto-create observation note
		if req.Reason != "" {
			go s.logArchiveBlockObservation(accountID, userID, leadID, "🚫 Bloqueado", req.Reason)
		}
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleArchiveLeadsBatch(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)

	var req struct {
		IDs     []string `json:"ids"`
		Archive bool     `json:"archive"`
		Reason  string   `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if len(req.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No IDs provided"})
	}

	var uuids []uuid.UUID
	for _, id := range req.IDs {
		if uid, err := uuid.Parse(id); err == nil {
			uuids = append(uuids, uid)
		}
	}
	if len(uuids) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No valid IDs provided"})
	}

	if err := s.services.Lead.ArchiveLeadsBatch(c.Context(), uuids, req.Archive, req.Reason); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateLeadsCache(accountID)
	action := "archived"
	if !req.Archive {
		action = "unarchived"
	}
	s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
		"action": action + "_batch",
		"count":  len(uuids),
	})
	if req.Archive {
		go s.desyncGoogleForLeadIDs(accountID, uuids)
		if req.Reason != "" {
			go func() {
				for _, uid := range uuids {
					s.logArchiveBlockObservation(accountID, userID, uid, "📦 Archivado", req.Reason)
				}
			}()
		}
	}
	return c.JSON(fiber.Map{"success": true, "count": len(uuids)})
}

func (s *Server) handleBlockLeadsBatch(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		IDs    []string `json:"ids"`
		Block  bool     `json:"block"`
		Reason string   `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if len(req.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No IDs provided"})
	}
	if req.Block && req.Reason == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Reason is required when blocking"})
	}

	var uuids []uuid.UUID
	for _, id := range req.IDs {
		if uid, err := uuid.Parse(id); err == nil {
			uuids = append(uuids, uid)
		}
	}
	if len(uuids) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No valid IDs provided"})
	}

	if err := s.services.Lead.BlockLeadsBatch(c.Context(), uuids, req.Block, req.Reason); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateLeadsCache(accountID)
	action := "blocked"
	if !req.Block {
		action = "unblocked"
	}
	s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
		"action": action + "_batch",
		"count":  len(uuids),
	})
	if req.Block {
		go s.desyncGoogleForLeadIDs(accountID, uuids)
	}
	return c.JSON(fiber.Map{"success": true, "count": len(uuids)})
}

func (s *Server) handleGetLeadCounts(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := c.Query("kommo_sync", "all")
	pipelineID := c.Query("pipeline_id")

	var extraWhere string
	var args []interface{}
	args = append(args, accountID)

	switch kommoSync {
	case "kommo":
		extraWhere += " AND kommo_id IS NOT NULL AND kommo_deleted_at IS NULL"
	case "clarin":
		extraWhere += " AND (kommo_id IS NULL OR kommo_deleted_at IS NOT NULL)"
	}

	// Filter by pipeline if specified
	if pipelineID != "" && pipelineID != "__no_pipeline__" {
		if uid, err := uuid.Parse(pipelineID); err == nil {
			extraWhere += fmt.Sprintf(" AND stage_id IN (SELECT id FROM pipeline_stages WHERE pipeline_id = $%d)", len(args)+1)
			args = append(args, uid)
		}
	} else if pipelineID == "__no_pipeline__" {
		extraWhere += " AND stage_id IS NULL"
	}

	var active, archived, blocked int
	err := s.repos.DB().QueryRow(c.Context(), fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE NOT is_archived AND NOT is_blocked),
			COUNT(*) FILTER (WHERE is_archived AND NOT is_blocked),
			COUNT(*) FILTER (WHERE is_blocked)
		FROM leads WHERE account_id = $1%s
	`, extraWhere), args...).Scan(&active, &archived, &blocked)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"success":  true,
		"active":   active,
		"archived": archived,
		"blocked":  blocked,
	})
}

// --- Pipeline Handlers ---

func (s *Server) handleUpdateLeadStage(c *fiber.Ctx) error {
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}

	var req struct {
		StageID string `json:"stage_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	stageID, err := uuid.Parse(req.StageID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid stage ID"})
	}

	if err := s.services.Lead.UpdateStage(c.Context(), leadID, stageID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Push stage change to Kommo (batched via outbox when enabled)
	accountID := c.Locals("account_id").(uuid.UUID)
	if kommoSync := s.kommoForAccount(c.Context(), accountID); kommoSync != nil {
		kommoSync.EnqueuePushLeadStage(accountID, leadID)
	}

	s.invalidateLeadsCache(accountID)
	s.invalidateLeadDetailCache(leadID)
	// Broadcast delta with stage info
	s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
		"action":   "stage_changed",
		"lead_id":  leadID.String(),
		"stage_id": stageID.String(),
	})

	// Fire lead_stage_changed automation trigger
	s.triggerAutomationLeadStageChanged(accountID, leadID, stageID)

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleGetPipelines(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Try Redis cache first (5 min TTL — pipelines rarely change)
	cacheKey := "pipelines:" + accountID.String()
	if s.cache != nil {
		if cached, err := s.cache.Get(c.Context(), cacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	pipelines, err := s.services.Pipeline.GetByAccountID(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	result := fiber.Map{"success": true, "pipelines": pipelines}
	if s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), cacheKey, data, 5*time.Minute)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleCreatePipeline(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil || req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	pipeline := &domain.Pipeline{
		AccountID:   accountID,
		Name:        req.Name,
		Description: req.Description,
	}
	if err := s.services.Pipeline.Create(c.Context(), pipeline); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(accountID)
	return c.Status(201).JSON(fiber.Map{"success": true, "pipeline": pipeline})
}

func (s *Server) handleUpdatePipeline(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pipelineID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	pipeline, err := s.services.Pipeline.GetByID(c.Context(), pipelineID)
	if err != nil || pipeline == nil || pipeline.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Pipeline not found"})
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		pipeline.Name = *req.Name
	}
	if req.Description != nil {
		pipeline.Description = req.Description
	}
	if err := s.services.Pipeline.Update(c.Context(), pipeline); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(pipeline.AccountID)
	return c.JSON(fiber.Map{"success": true, "pipeline": pipeline})
}

func (s *Server) handleDeletePipeline(c *fiber.Ctx) error {
	pipelineID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	if err := s.services.Pipeline.Delete(c.Context(), pipelineID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(c.Locals("account_id").(uuid.UUID))
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleCreatePipelineStage(c *fiber.Ctx) error {
	pipelineID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.BodyParser(&req); err != nil || req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	if req.Color == "" {
		req.Color = "#6366f1"
	}
	stage := &domain.PipelineStage{
		PipelineID: pipelineID,
		Name:       req.Name,
		Color:      req.Color,
	}
	if err := s.services.Pipeline.CreateStage(c.Context(), stage); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(c.Locals("account_id").(uuid.UUID))
	return c.Status(201).JSON(fiber.Map{"success": true, "stage": stage})
}

func (s *Server) handleUpdatePipelineStage(c *fiber.Ctx) error {
	stageID, err := uuid.Parse(c.Params("stageId"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid stage ID"})
	}
	var req struct {
		Name     *string `json:"name"`
		Color    *string `json:"color"`
		Position *int    `json:"position"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	stage := &domain.PipelineStage{ID: stageID}
	if req.Name != nil {
		stage.Name = *req.Name
	}
	if req.Color != nil {
		stage.Color = *req.Color
	}
	if req.Position != nil {
		stage.Position = *req.Position
	}
	if err := s.services.Pipeline.UpdateStage(c.Context(), stage); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(c.Locals("account_id").(uuid.UUID))
	return c.JSON(fiber.Map{"success": true, "stage": stage})
}

func (s *Server) handleDeletePipelineStage(c *fiber.Ctx) error {
	stageID, err := uuid.Parse(c.Params("stageId"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid stage ID"})
	}
	if err := s.services.Pipeline.DeleteStage(c.Context(), stageID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(c.Locals("account_id").(uuid.UUID))
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleReorderPipelineStages(c *fiber.Ctx) error {
	pipelineID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	var req struct {
		StageIDs []string `json:"stage_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	var stageIDs []uuid.UUID
	for _, s := range req.StageIDs {
		if uid, err := uuid.Parse(s); err == nil {
			stageIDs = append(stageIDs, uid)
		}
	}
	if err := s.services.Pipeline.ReorderStages(c.Context(), pipelineID, stageIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidatePipelinesCache(c.Locals("account_id").(uuid.UUID))
	return c.JSON(fiber.Map{"success": true})
}

// --- Import CSV Handler ---

type csvImportPreviewRow struct {
	Row            int    `json:"row"`
	Action         string `json:"action"`
	Reason         string `json:"reason,omitempty"`
	Name           string `json:"name,omitempty"`
	Phone          string `json:"phone,omitempty"`
	KommoID        *int64 `json:"kommo_id,omitempty"`
	ExistingLeadID string `json:"existing_lead_id,omitempty"`
}

type csvImportSummary struct {
	ImportType          string                `json:"import_type"`
	Source              string                `json:"source"`
	FileName            string                `json:"file_name"`
	ImportTag           string                `json:"import_tag,omitempty"`
	TotalRows           int                   `json:"total_rows"`
	New                 int                   `json:"new"`
	Existing            int                   `json:"existing"`
	Created             int                   `json:"created"`
	Updated             int                   `json:"updated"`
	Skipped             int                   `json:"skipped"`
	Duplicates          int                   `json:"duplicates"`
	ErrorCount          int                   `json:"error_count"`
	NewContacts         int                   `json:"new_contacts"`
	SafeMode            bool                  `json:"safe_mode"`
	IncomingDestination string                `json:"incoming_destination,omitempty"`
	Rows                []csvImportPreviewRow `json:"rows,omitempty"`
	Errors              []string              `json:"errors"`
}

type csvImportRecord struct {
	RowNum             int
	Action             string
	Reason             string
	KommoID            *int64
	Phone              string
	JID                string
	Name               string
	LastName           string
	Email              string
	Company            string
	Notes              string
	DNI                string
	BirthDate          *time.Time
	Tags               []string
	CustomFields       map[string]interface{}
	ExistingLeadID     *uuid.UUID
	ExistingContactID  *uuid.UUID
	LinkKommoToLead    bool
	WillCreateContact  bool
	ExistingLeadIDText string
}

type csvImportPlan struct {
	Summary csvImportSummary
	Records []csvImportRecord
}

func (s *Server) handlePreviewImportCSV(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	importType, importTag, fileName, rawBytes, status, errMsg := readCSVImportUpload(c)
	if errMsg != "" {
		return c.Status(status).JSON(fiber.Map{"success": false, "error": errMsg})
	}
	plan, err := s.buildCSVImportPlan(c.Context(), accountID, importType, importTag, fileName, rawBytes)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "preview": plan.Summary})
}

func (s *Server) handleImportCSV(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	userID, _ := c.Locals("user_id").(uuid.UUID)
	importType, importTag, fileName, rawBytes, status, errMsg := readCSVImportUpload(c)
	if errMsg != "" {
		return c.Status(status).JSON(fiber.Map{"success": false, "error": errMsg})
	}

	plan, err := s.buildCSVImportPlan(c.Context(), accountID, importType, importTag, fileName, rawBytes)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if plan.Summary.NewContacts > 0 {
		if err := s.enforcePlanLimit(c.Context(), accountID, "max_contacts", plan.Summary.NewContacts); err != nil {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_contacts"})
		}
	}

	result := s.executeCSVImportPlan(c.Context(), accountID, plan)
	s.recordCSVImportLog(c.Context(), accountID, userID, result)

	if result.Created > 0 || result.Updated > 0 {
		s.invalidateLeadsCache(accountID)
		s.invalidateContactsCache(accountID)
		s.invalidateTagsCache(accountID)
	}
	if result.Created > 0 {
		go s.services.Event.ReconcileAllAccountEvents(context.Background(), accountID)
	}

	return c.JSON(fiber.Map{
		"success":      true,
		"imported":     result.Created + result.Updated,
		"created":      result.Created,
		"updated":      result.Updated,
		"existing":     result.Existing,
		"skipped":      result.Skipped,
		"duplicates":   result.Duplicates,
		"new_contacts": result.NewContacts,
		"errors":       result.Errors,
		"summary":      result,
	})
}

func readCSVImportUpload(c *fiber.Ctx) (string, string, string, []byte, int, string) {
	importType := c.FormValue("import_type")
	if importType == "" {
		importType = "leads"
	}
	if importType != "leads" && importType != "contacts" && importType != "both" {
		return "", "", "", nil, fiber.StatusBadRequest, "import_type must be 'leads', 'contacts', or 'both'"
	}
	importTag := cleanCSVValue(c.FormValue("import_tag"))
	file, err := c.FormFile("file")
	if err != nil {
		return "", "", "", nil, fiber.StatusBadRequest, "CSV file is required"
	}
	f, err := file.Open()
	if err != nil {
		return "", "", "", nil, fiber.StatusInternalServerError, "Cannot read file"
	}
	defer f.Close()
	rawBytes, err := io.ReadAll(f)
	if err != nil {
		return "", "", "", nil, fiber.StatusInternalServerError, "Cannot read file content"
	}
	return importType, importTag, file.Filename, rawBytes, fiber.StatusOK, ""
}

func (s *Server) buildCSVImportPlan(ctx context.Context, accountID uuid.UUID, importType, importTag, fileName string, rawBytes []byte) (*csvImportPlan, error) {
	rawContent := strings.TrimPrefix(string(rawBytes), "\ufeff")
	headerLine, dataContent := splitCSVHeader(rawContent)
	if strings.TrimSpace(headerLine) == "" || strings.TrimSpace(dataContent) == "" {
		return nil, fmt.Errorf("CSV file must have at least a header and one data row")
	}

	headerSep := detectCSVSeparator(headerLine)
	headers, err := readCSVRecord(headerLine, headerSep)
	if err != nil {
		return nil, fmt.Errorf("cannot parse CSV headers")
	}
	firstDataRow, firstDataLine := firstCSVDataRow(dataContent)
	dataSep := detectCSVSeparator(firstDataLine)
	if len(firstDataRow) == 0 {
		return nil, fmt.Errorf("CSV file must have at least one data row")
	}

	colMap := make(map[string]int)
	for i, h := range headers {
		key := normalizeImportHeader(h)
		if key != "" {
			colMap[key] = i
		}
	}

	phoneCol := findCol(colMap, "phone", "telefono", "teléfono", "celular", "número", "numero", "movil", "móvil")
	if phoneCol == -1 {
		phoneCol = detectPhoneColumn(headers, firstDataRow)
	}
	if phoneCol == -1 {
		return nil, fmt.Errorf("CSV must have a phone/telefono/celular column or a Kommo phone column")
	}

	idCol := findCol(colMap, "id", "kommo id", "kommo_id", "lead id", "id lead")
	nameCol := findCol(colMap, "nombre completo", "name", "nombre", "nombre_completo")
	leadNameCol := findCol(colMap, "nombre del lead", "lead name")
	emailCol := findCol(colMap, "email", "correo", "e-mail", "e-mail priv.", "otro e-mail")
	notesCol := findCol(colMap, "notes", "notas", "observaciones", "nota")
	tagsCol := findCol(colMap, "tags", "etiquetas")
	companyCol := findCol(colMap, "company", "empresa")
	lastNameCol := findCol(colMap, "last_name", "apellido", "apellidos")
	dniCol := findCol(colMap, "dni", "documento", "doc_identidad")
	birthDateCol := findCol(colMap, "fecha_nacimiento", "birth_date", "nacimiento", "cumpleanos", "cumpleaños")
	kommoFieldCols := kommoCSVFieldColumns(colMap)

	plan := &csvImportPlan{
		Summary: csvImportSummary{
			ImportType: importType,
			Source:     detectImportSource(colMap),
			FileName:   fileName,
			ImportTag:  importTag,
			SafeMode:   true,
		},
	}
	if pid, sid, err := s.repos.Pipeline.ResolveIncomingLeadDestination(ctx, accountID); err == nil && pid != nil && sid != nil {
		var stageName string
		_ = s.repos.DB().QueryRow(ctx, `SELECT name FROM pipeline_stages WHERE id = $1`, *sid).Scan(&stageName)
		if stageName != "" {
			plan.Summary.IncomingDestination = stageName
		}
	}

	reader := csv.NewReader(strings.NewReader(dataContent))
	reader.Comma = dataSep
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	seenKommoIDs := map[int64]bool{}
	seenNewJIDs := map[string]bool{}
	rowNum := 1
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			plan.Summary.Skipped++
			plan.Summary.ErrorCount++
			plan.Summary.Errors = append(plan.Summary.Errors, fmt.Sprintf("fila %d: no se pudo leer (%v)", rowNum, err))
			plan.addPreview(csvImportPreviewRow{Row: rowNum, Action: "skip", Reason: "No se pudo leer la fila"})
			continue
		}
		if rowIsEmpty(row) {
			continue
		}
		plan.Summary.TotalRows++

		record := csvImportRecord{RowNum: rowNum, Action: "create"}
		record.Phone = kommo.NormalizePhone(safeCol(row, phoneCol))
		if record.Phone == "" || len(record.Phone) < 6 {
			record.Action = "skip"
			record.Reason = "Sin teléfono válido"
			plan.Summary.Skipped++
			plan.addPreview(csvImportPreviewRow{Row: rowNum, Action: "skip", Reason: record.Reason})
			plan.Records = append(plan.Records, record)
			continue
		}
		record.JID = record.Phone + "@s.whatsapp.net"
		record.KommoID = parseOptionalInt64(safeCol(row, idCol))
		record.Name = cleanCSVValue(safeCol(row, nameCol))
		if record.Name == "" {
			record.Name = cleanLeadNameFallback(safeCol(row, leadNameCol))
		}
		record.Email = cleanCSVValue(safeCol(row, emailCol))
		record.Notes = cleanCSVValue(safeCol(row, notesCol))
		record.Company = cleanCSVValue(safeCol(row, companyCol))
		record.LastName = cleanCSVValue(safeCol(row, lastNameCol))
		record.DNI = cleanCSVValue(safeCol(row, dniCol))
		record.BirthDate = parseImportDate(safeCol(row, birthDateCol))
		record.Tags = splitImportTags(safeCol(row, tagsCol))
		record.CustomFields = extractKommoCustomFields(row, headers, kommoFieldCols)

		if record.KommoID != nil {
			if seenKommoIDs[*record.KommoID] {
				record.Action = "skip"
				record.Reason = "ID de Kommo duplicado dentro del archivo"
				plan.Summary.Skipped++
				plan.Summary.Duplicates++
				plan.addPreview(record.previewRow())
				plan.Records = append(plan.Records, record)
				continue
			}
			seenKommoIDs[*record.KommoID] = true
		}

		leadID, leadContactID, linkKommo, err := s.findCSVImportLead(ctx, accountID, record.KommoID, record.JID)
		if err != nil {
			record.Action = "skip"
			record.Reason = err.Error()
			plan.Summary.Skipped++
			plan.Summary.ErrorCount++
			plan.Summary.Errors = append(plan.Summary.Errors, fmt.Sprintf("fila %d: %s", rowNum, err.Error()))
			plan.addPreview(record.previewRow())
			plan.Records = append(plan.Records, record)
			continue
		}
		record.ExistingLeadID = leadID
		record.LinkKommoToLead = linkKommo
		if leadID != nil {
			record.Action = "update_existing"
			record.ExistingLeadIDText = leadID.String()
			plan.Summary.Existing++
		} else {
			if seenNewJIDs[record.JID] {
				record.Action = "skip"
				record.Reason = "Teléfono duplicado dentro del archivo"
				plan.Summary.Skipped++
				plan.Summary.Duplicates++
				plan.addPreview(record.previewRow())
				plan.Records = append(plan.Records, record)
				continue
			}
			seenNewJIDs[record.JID] = true
			plan.Summary.New++
		}

		contactID, err := s.findCSVImportContact(ctx, accountID, record.JID)
		if err != nil {
			record.Action = "skip"
			record.Reason = err.Error()
			plan.Summary.Skipped++
			plan.Summary.ErrorCount++
			plan.Summary.Errors = append(plan.Summary.Errors, fmt.Sprintf("fila %d: %s", rowNum, err.Error()))
			plan.addPreview(record.previewRow())
			plan.Records = append(plan.Records, record)
			continue
		}
		if contactID == nil && leadContactID != nil {
			contactID = leadContactID
		}
		record.ExistingContactID = contactID
		if contactID == nil && (importType == "leads" || importType == "contacts" || importType == "both") {
			record.WillCreateContact = true
			plan.Summary.NewContacts++
		}

		plan.addPreview(record.previewRow())
		plan.Records = append(plan.Records, record)
	}
	return plan, nil
}

func (p *csvImportPlan) addPreview(row csvImportPreviewRow) {
	if len(p.Summary.Rows) < 50 {
		p.Summary.Rows = append(p.Summary.Rows, row)
	}
}

func (r csvImportRecord) previewRow() csvImportPreviewRow {
	row := csvImportPreviewRow{
		Row:            r.RowNum,
		Action:         r.Action,
		Reason:         r.Reason,
		Name:           r.Name,
		Phone:          r.Phone,
		KommoID:        r.KommoID,
		ExistingLeadID: r.ExistingLeadIDText,
	}
	if row.Reason == "" {
		switch r.Action {
		case "create":
			row.Reason = "Nuevo lead; usará la etapa entrante configurada"
		case "update_existing":
			row.Reason = "Existente; no se moverá de etapa ni se reemplazarán tags/notas"
		}
	}
	return row
}

func (s *Server) executeCSVImportPlan(ctx context.Context, accountID uuid.UUID, plan *csvImportPlan) csvImportSummary {
	result := plan.Summary
	result.Created = 0
	result.Updated = 0
	for _, record := range plan.Records {
		if record.Action == "skip" {
			continue
		}
		contact, contactCreated, contactUpdated, err := s.ensureCSVImportContact(ctx, accountID, record)
		if err != nil {
			result.Skipped++
			result.ErrorCount++
			result.Errors = append(result.Errors, fmt.Sprintf("fila %d: contacto: %s", record.RowNum, err.Error()))
			continue
		}
		if contactCreated {
			// Already included in preview NewContacts; no extra counter needed here.
		}
		if record.Action == "update_existing" {
			if record.ExistingLeadID != nil {
				var contactID *uuid.UUID
				if contact != nil {
					contactID = &contact.ID
				}
				if err := s.linkCSVImportLead(ctx, *record.ExistingLeadID, contactID, record.KommoID); err != nil {
					result.Skipped++
					result.ErrorCount++
					result.Errors = append(result.Errors, fmt.Sprintf("fila %d: lead existente: %s", record.RowNum, err.Error()))
					continue
				}
			}
			if contactUpdated || record.LinkKommoToLead {
				result.Updated++
			} else {
				result.Updated++
			}
			continue
		}
		if plan.Summary.ImportType == "contacts" {
			if contactCreated {
				result.Created++
			} else {
				result.Updated++
			}
			continue
		}
		if err := s.createCSVImportLead(ctx, accountID, record, contact, plan.Summary.ImportTag); err != nil {
			result.Skipped++
			result.ErrorCount++
			result.Errors = append(result.Errors, fmt.Sprintf("fila %d: lead: %s", record.RowNum, err.Error()))
			continue
		}
		result.Created++
	}
	return result
}

func (s *Server) createCSVImportLead(ctx context.Context, accountID uuid.UUID, record csvImportRecord, contact *domain.Contact, importTag string) error {
	pipelineID, stageID, err := s.repos.Pipeline.ResolveIncomingLeadDestination(ctx, accountID)
	if err != nil {
		return err
	}
	source := "kommo_csv_import"
	tags := appendImportTag(record.Tags, importTag)
	lead := &domain.Lead{
		AccountID:    accountID,
		JID:          record.JID,
		Name:         strPtr(record.Name),
		LastName:     strPtr(record.LastName),
		Phone:        strPtr(record.Phone),
		Email:        strPtr(record.Email),
		Company:      strPtr(record.Company),
		Notes:        strPtr(record.Notes),
		DNI:          strPtr(record.DNI),
		BirthDate:    record.BirthDate,
		Status:       strPtr(domain.LeadStatusNew),
		Source:       &source,
		PipelineID:   pipelineID,
		StageID:      stageID,
		Tags:         tags,
		CustomFields: record.CustomFields,
		KommoID:      record.KommoID,
	}
	if contact != nil {
		lead.ContactID = &contact.ID
	}
	if err := s.services.Lead.Create(ctx, lead); err != nil {
		return err
	}
	if len(tags) > 0 {
		if err := s.repos.Tag.SyncLeadTagsByNames(ctx, accountID, lead.ID, tags); err != nil {
			log.Printf("[CSV Import] Failed to sync tags for lead %s: %v", lead.ID, err)
		}
	}
	return nil
}

func (s *Server) ensureCSVImportContact(ctx context.Context, accountID uuid.UUID, record csvImportRecord) (*domain.Contact, bool, bool, error) {
	contact, err := s.repos.Contact.GetByJID(ctx, accountID, record.JID)
	if err != nil {
		return nil, false, false, err
	}
	created := false
	if contact == nil {
		contact, err = s.repos.Contact.GetOrCreate(ctx, accountID, nil, record.JID, record.Phone, record.Name, "", false)
		if err != nil {
			return nil, false, false, err
		}
		created = true
	}
	updated := fillEmptyContactFields(contact, record)
	if updated {
		if err := s.repos.Contact.Update(ctx, contact); err != nil {
			return nil, created, false, err
		}
	}
	return contact, created, updated, nil
}

func fillEmptyContactFields(contact *domain.Contact, record csvImportRecord) bool {
	changed := false
	if record.Name != "" && stringPtrEmpty(contact.Name) && stringPtrEmpty(contact.CustomName) {
		contact.Name = strPtr(record.Name)
		changed = true
	}
	if record.LastName != "" && stringPtrEmpty(contact.LastName) {
		contact.LastName = strPtr(record.LastName)
		changed = true
	}
	if record.Email != "" && stringPtrEmpty(contact.Email) {
		contact.Email = strPtr(record.Email)
		changed = true
	}
	if record.Company != "" && stringPtrEmpty(contact.Company) {
		contact.Company = strPtr(record.Company)
		changed = true
	}
	if record.Notes != "" && stringPtrEmpty(contact.Notes) {
		contact.Notes = strPtr(record.Notes)
		changed = true
	}
	if record.DNI != "" && stringPtrEmpty(contact.DNI) {
		contact.DNI = strPtr(record.DNI)
		changed = true
	}
	if record.BirthDate != nil && contact.BirthDate == nil {
		contact.BirthDate = record.BirthDate
		changed = true
	}
	if stringPtrEmpty(contact.Source) {
		contact.Source = strPtr("kommo_csv_import")
		changed = true
	}
	return changed
}

func (s *Server) linkCSVImportLead(ctx context.Context, leadID uuid.UUID, contactID *uuid.UUID, kommoID *int64) error {
	_, err := s.repos.DB().Exec(ctx, `
		UPDATE leads SET
			contact_id = COALESCE(contact_id, $1::uuid),
			kommo_id = COALESCE(kommo_id, $2::bigint),
			source = COALESCE(source, 'kommo_csv_import'),
			kommo_deleted_at = CASE WHEN $2::bigint IS NOT NULL THEN NULL ELSE kommo_deleted_at END,
			updated_at = CASE
				WHEN (contact_id IS NULL AND $1::uuid IS NOT NULL)
				  OR (kommo_id IS NULL AND $2::bigint IS NOT NULL)
				  OR source IS NULL
				THEN NOW()
				ELSE updated_at
			END
		WHERE id = $3
	`, contactID, kommoID, leadID)
	return err
}

func (s *Server) findCSVImportLead(ctx context.Context, accountID uuid.UUID, kommoID *int64, jid string) (*uuid.UUID, *uuid.UUID, bool, error) {
	var leadID uuid.UUID
	var contactID *uuid.UUID
	if kommoID != nil {
		err := s.repos.DB().QueryRow(ctx, `
			SELECT id, contact_id FROM leads
			WHERE account_id = $1 AND kommo_id = $2
			ORDER BY updated_at DESC LIMIT 1
		`, accountID, *kommoID).Scan(&leadID, &contactID)
		if err == nil {
			return &leadID, contactID, false, nil
		}
		if err != pgx.ErrNoRows {
			return nil, nil, false, err
		}
		var existingKommoID *int64
		err = s.repos.DB().QueryRow(ctx, `
			SELECT id, contact_id, kommo_id FROM leads
			WHERE account_id = $1 AND jid = $2
			ORDER BY (kommo_id IS NULL) DESC, updated_at DESC LIMIT 1
		`, accountID, jid).Scan(&leadID, &contactID, &existingKommoID)
		if err == nil {
			return &leadID, contactID, existingKommoID == nil, nil
		}
		if err != pgx.ErrNoRows {
			return nil, nil, false, err
		}
		return nil, nil, false, nil
	}
	err := s.repos.DB().QueryRow(ctx, `
		SELECT id, contact_id FROM leads
		WHERE account_id = $1 AND jid = $2
		ORDER BY updated_at DESC LIMIT 1
	`, accountID, jid).Scan(&leadID, &contactID)
	if err == nil {
		return &leadID, contactID, false, nil
	}
	if err != pgx.ErrNoRows {
		return nil, nil, false, err
	}
	return nil, nil, false, nil
}

func (s *Server) findCSVImportContact(ctx context.Context, accountID uuid.UUID, jid string) (*uuid.UUID, error) {
	var contactID uuid.UUID
	err := s.repos.DB().QueryRow(ctx, `SELECT id FROM contacts WHERE account_id = $1 AND jid = $2`, accountID, jid).Scan(&contactID)
	if err == nil {
		return &contactID, nil
	}
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return nil, err
}

func (s *Server) recordCSVImportLog(ctx context.Context, accountID, userID uuid.UUID, summary csvImportSummary) {
	details, _ := json.Marshal(fiber.Map{
		"incoming_destination": summary.IncomingDestination,
		"import_tag":           summary.ImportTag,
		"safe_mode":            summary.SafeMode,
		"errors":               summary.Errors,
	})
	_, err := s.repos.DB().Exec(ctx, `
		INSERT INTO csv_import_logs (
			account_id, uploaded_by, import_type, source, file_name, total_rows,
			created_count, updated_count, existing_count, skipped_count,
			duplicate_count, error_count, new_contacts_count, details
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, accountID, userID, summary.ImportType, summary.Source, summary.FileName, summary.TotalRows,
		summary.Created, summary.Updated, summary.Existing, summary.Skipped,
		summary.Duplicates, summary.ErrorCount, summary.NewContacts, details)
	if err != nil {
		log.Printf("[CSV Import] failed to write import log: %v", err)
	}
}

func splitCSVHeader(rawContent string) (string, string) {
	rawContent = strings.TrimLeft(rawContent, "\r\n\t ")
	for i, ch := range rawContent {
		if ch == '\n' {
			header := strings.TrimRight(rawContent[:i], "\r")
			return header, rawContent[i+1:]
		}
		if ch == '\r' {
			header := rawContent[:i]
			rest := rawContent[i+1:]
			rest = strings.TrimPrefix(rest, "\n")
			return header, rest
		}
	}
	return rawContent, ""
}

func readCSVRecord(line string, sep rune) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(line))
	reader.Comma = sep
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	return reader.Read()
}

func firstCSVDataRow(dataContent string) ([]string, string) {
	for _, line := range strings.Split(dataContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		sep := detectCSVSeparator(trimmed)
		row, err := readCSVRecord(trimmed, sep)
		if err == nil {
			return row, trimmed
		}
		return nil, trimmed
	}
	return nil, ""
}

func normalizeImportHeader(header string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(header, "\ufeff")))
}

func detectPhoneColumn(headers []string, row []string) int {
	bestCol := -1
	bestScore := 0
	for i, val := range row {
		if i < len(headers) {
			hdr := normalizeImportHeader(headers[i])
			if hdr == "id" || hdr == "edad" || hdr == "age" || hdr == "dni" || hdr == "dni_ce" {
				continue
			}
		}
		raw := strings.TrimSpace(val)
		cleaned := strings.Trim(raw, "'\"` ")
		hasPlus := strings.HasPrefix(cleaned, "+")
		hasTick := strings.HasPrefix(raw, "'") || strings.HasPrefix(raw, "\"'")
		normalized := kommo.NormalizePhone(cleaned)
		if len(normalized) < 8 || len(normalized) > 15 {
			continue
		}
		allDigits := true
		for _, ch := range normalized {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if !allDigits {
			continue
		}
		score := 1
		if hasPlus || hasTick {
			score += 10
		}
		if i < len(headers) && strings.TrimSpace(headers[i]) == "" {
			score += 5
		}
		if len(normalized) >= 10 {
			score += 2
		}
		if score > bestScore {
			bestScore = score
			bestCol = i
		}
	}
	return bestCol
}

func detectImportSource(colMap map[string]int) string {
	if _, hasLeadName := colMap["nombre del lead"]; hasLeadName {
		if _, hasPipeline := colMap["embudo de ventas"]; hasPipeline {
			return "kommo_csv"
		}
	}
	return "csv"
}

func kommoCSVFieldColumns(colMap map[string]int) map[string]int {
	keys := []string{
		"estatus del lead", "embudo de ventas", "status", "detec cam", "grupo", "otras",
		"prueba", "bot 1.0", "utm_content", "utm_medium", "utm_campaign", "utm_source",
		"utm_term", "utm_referrer", "referrer", "gclientid", "gclid", "fbclid", "ttad_name", "ttad_id",
	}
	cols := make(map[string]int)
	for _, key := range keys {
		if idx, ok := colMap[key]; ok {
			cols[key] = idx
		}
	}
	return cols
}

func parseOptionalInt64(value string) *int64 {
	value = strings.TrimSpace(strings.Trim(value, "'\"` "))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func cleanCSVValue(value string) string {
	return strings.TrimSpace(strings.Trim(value, "'\"` "))
}

func cleanLeadNameFallback(value string) string {
	value = cleanCSVValue(value)
	if strings.HasPrefix(strings.ToLower(value), "lead #") {
		return ""
	}
	return value
}

func parseImportDate(value string) *time.Time {
	value = cleanCSVValue(value)
	if value == "" {
		return nil
	}
	formats := []string{"2006-01-02", "02.01.2006", "02/01/2006", "02-01-2006", time.RFC3339}
	for _, layout := range formats {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}

func splitImportTags(value string) []string {
	value = cleanCSVValue(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		tags = append(tags, tag)
	}
	return tags
}

func appendImportTag(tags []string, importTag string) []string {
	importTag = cleanCSVValue(importTag)
	if importTag == "" {
		return tags
	}
	merged := make([]string, 0, len(tags)+1)
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, tag)
	}
	if !seen[strings.ToLower(importTag)] {
		merged = append(merged, importTag)
	}
	return merged
}

func extractKommoCustomFields(row, headers []string, cols map[string]int) map[string]interface{} {
	fields := map[string]interface{}{}
	for normalizedHeader, idx := range cols {
		value := cleanCSVValue(safeCol(row, idx))
		if value == "" {
			continue
		}
		key := "kommo_" + strings.NewReplacer(" ", "_", ".", "_", "-", "_").Replace(normalizedHeader)
		fields[key] = value
		if idx < len(headers) {
			fields[key+"_label"] = strings.TrimSpace(headers[idx])
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func rowIsEmpty(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func stringPtrEmpty(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}

// detectCSVSeparator counts commas, semicolons and tabs outside quotes and returns the most frequent one
func detectCSVSeparator(line string) rune {
	counts := map[rune]int{',': 0, ';': 0, '\t': 0}
	inQuote := false
	for _, ch := range line {
		if ch == '"' {
			inQuote = !inQuote
		}
		if !inQuote {
			if _, ok := counts[ch]; ok {
				counts[ch]++
			}
		}
	}
	best := ','
	bestCount := 0
	for sep, cnt := range counts {
		if cnt > bestCount {
			bestCount = cnt
			best = sep
		}
	}
	return best
}

// findCol returns the column index for the first matching key, or -1
func findCol(colMap map[string]int, keys ...string) int {
	for _, key := range keys {
		if idx, ok := colMap[key]; ok {
			return idx
		}
	}
	return -1
}

// safeCol returns the trimmed value at the given index, or "" if out of bounds
func safeCol(row []string, idx int) string {
	if idx >= 0 && idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

// --- Contact Handlers ---

func (s *Server) handleGetContacts(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Parse filters
	filter := domain.ContactFilter{
		Search:  c.Query("search"),
		Limit:   c.QueryInt("limit", 50),
		Offset:  c.QueryInt("offset", 0),
		IsGroup: c.QueryBool("is_group", false),
	}

	if deviceIDStr := c.Query("device_id"); deviceIDStr != "" {
		if did, err := uuid.Parse(deviceIDStr); err == nil {
			filter.DeviceID = &did
		}
	}

	if c.QueryBool("has_phone", false) {
		filter.HasPhone = true
	}

	if tagsStr := c.Query("tags"); tagsStr != "" {
		filter.Tags = strings.Split(tagsStr, ",")
	}

	if tagIDsStr := c.Query("tag_ids"); tagIDsStr != "" {
		for _, tidStr := range strings.Split(tagIDsStr, ",") {
			if tid, err := uuid.Parse(strings.TrimSpace(tidStr)); err == nil {
				filter.TagIDs = append(filter.TagIDs, tid)
			}
		}
	}

	// Advanced tag filtering: formula or tag_names
	tagFormulaRaw := c.Query("tag_formula")
	if tagFormulaRaw != "" {
		ast, err := formula.Parse(tagFormulaRaw)
		if err == nil && ast != nil {
			innerSQL, innerArgs, err := formula.BuildSQLForContacts(ast, accountID)
			if err == nil {
				rows, err := s.repos.DB().Query(c.Context(), innerSQL, innerArgs...)
				if err == nil {
					defer rows.Close()
					for rows.Next() {
						var cid uuid.UUID
						if rows.Scan(&cid) == nil {
							filter.MatchingContactIDs = append(filter.MatchingContactIDs, cid)
						}
					}
				}
				if len(filter.MatchingContactIDs) == 0 {
					// Formula matched nothing — force empty result
					return c.JSON(fiber.Map{
						"success": true, "contacts": []interface{}{},
						"total": 0, "limit": filter.Limit, "offset": filter.Offset,
					})
				}
			}
		}
	} else {
		if tagNamesRaw := c.Query("tag_names"); tagNamesRaw != "" {
			filter.TagNames = strings.Split(tagNamesRaw, ",")
		}
		if excludeRaw := c.Query("exclude_tag_names"); excludeRaw != "" {
			filter.ExcludeTagNames = strings.Split(excludeRaw, ",")
		}
		filter.TagMode = strings.ToUpper(c.Query("tag_mode", "OR"))
	}

	// Date filters
	filter.DateField = c.Query("date_field")
	filter.DateFrom = c.Query("date_from")
	filter.DateTo = c.Query("date_to")

	// Sort
	if sortBy := c.Query("sort_by"); sortBy != "" {
		switch sortBy {
		case "name", "lead_count", "created_at":
			filter.SortBy = sortBy
		}
	}
	if sortOrder := strings.ToLower(c.Query("sort_order")); sortOrder == "asc" || sortOrder == "desc" {
		filter.SortOrder = sortOrder
	}

	// Custom field filters
	if cfFilterRaw := c.Query("cf_filter"); cfFilterRaw != "" {
		var cfFilters []repository.CustomFieldFilterParam
		if err := json.Unmarshal([]byte(cfFilterRaw), &cfFilters); err == nil && len(cfFilters) > 0 {
			matchIDs, err := s.repos.CustomField.FindContactIDsByFilters(c.Context(), accountID, cfFilters)
			if err == nil {
				if len(matchIDs) == 0 {
					return c.JSON(fiber.Map{
						"success": true, "contacts": []interface{}{},
						"total": 0, "limit": filter.Limit, "offset": filter.Offset,
					})
				}
				filter.CfFilterContactIDs = matchIDs
			}
		}
	}

	// Redis cache for default load (no complex filters) — 30s TTL
	isDefaultContactsLoad := filter.Search == "" && len(filter.Tags) == 0 && len(filter.TagIDs) == 0 && len(filter.TagNames) == 0 && len(filter.MatchingContactIDs) == 0 && len(filter.CfFilterContactIDs) == 0 && filter.DeviceID == nil && filter.DateField == "" && !filter.HasPhone
	contactsCacheKey := ""
	if isDefaultContactsLoad && s.cache != nil {
		contactsCacheKey = fmt.Sprintf("contacts:%s:%d:%d", accountID.String(), filter.Limit, filter.Offset)
		if cached, err := s.cache.Get(c.Context(), contactsCacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	contacts, total, err := s.services.Contact.GetByAccountIDWithFilters(c.Context(), accountID, filter)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Load structured tags for all contacts in one batch query
	if len(contacts) > 0 {
		contactIDs := make([]uuid.UUID, len(contacts))
		for i, c := range contacts {
			contactIDs[i] = c.ID
		}
		tagMap, err := s.repos.Tag.GetByContactsBatch(c.Context(), contactIDs)
		if err == nil {
			for _, contact := range contacts {
				contact.StructuredTags = tagMap[contact.ID]
			}
		}

		// Optionally load custom field values
		if c.QueryBool("include_custom_fields", false) {
			cfMap, cfErr := s.repos.CustomField.GetValuesByContacts(c.Context(), contactIDs)
			if cfErr == nil {
				for _, contact := range contacts {
					contact.CustomFieldValues = cfMap[contact.ID]
				}
			}
		}
	}

	result := fiber.Map{
		"success":  true,
		"contacts": contacts,
		"total":    total,
		"limit":    filter.Limit,
		"offset":   filter.Offset,
	}

	// Cache default load result
	if contactsCacheKey != "" && s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), contactsCacheKey, data, 30*time.Second)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleGetContact(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid id"})
	}

	contact, err := s.services.Contact.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if contact == nil || contact.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "contact not found"})
	}

	tags, _ := s.services.Tag.GetByEntity(c.Context(), "contact", contact.ID)
	contact.StructuredTags = tags

	return c.JSON(fiber.Map{"success": true, "contact": contact})
}

func (s *Server) handleSyncContactFromKommo(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	contactID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid contact ID"})
	}

	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Kommo integration not configured"})
	}
	if contact, _ := s.services.Contact.GetByID(c.Context(), contactID); contact == nil || contact.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "contact not found"})
	}

	if err := kommoSync.SyncSingleContact(c.Context(), accountID, contactID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Return the updated contact
	contact, err := s.services.Contact.GetByID(c.Context(), contactID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	tags, _ := s.services.Tag.GetByEntity(c.Context(), "contact", contact.ID)
	contact.StructuredTags = tags

	return c.JSON(fiber.Map{"success": true, "contact": contact})
}

func (s *Server) handleUpdateContact(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid id"})
	}

	contact, err := s.services.Contact.GetByID(c.Context(), id)
	if err != nil || contact == nil || contact.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "contact not found"})
	}

	var body struct {
		CustomName *string  `json:"custom_name"`
		LastName   *string  `json:"last_name"`
		ShortName  *string  `json:"short_name"`
		Phone      *string  `json:"phone"`
		Email      *string  `json:"email"`
		Company    *string  `json:"company"`
		Age        *int     `json:"age"`
		DNI        *string  `json:"dni"`
		BirthDate  *string  `json:"birth_date"`
		Address    *string  `json:"address"`
		Distrito   *string  `json:"distrito"`
		Ocupacion  *string  `json:"ocupacion"`
		Tags       []string `json:"tags"`
		Notes      *string  `json:"notes"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid body"})
	}

	if body.CustomName != nil {
		if *body.CustomName == "" {
			contact.CustomName = nil
		} else {
			contact.CustomName = body.CustomName
		}
	}
	if body.LastName != nil {
		if *body.LastName == "" {
			contact.LastName = nil
		} else {
			contact.LastName = body.LastName
		}
	}
	if body.ShortName != nil {
		if *body.ShortName == "" {
			contact.ShortName = nil
		} else {
			contact.ShortName = body.ShortName
		}
	}
	if body.Phone != nil {
		if *body.Phone == "" {
			contact.Phone = nil
		} else {
			contact.Phone = body.Phone
		}
	}
	if body.Email != nil {
		if *body.Email == "" {
			contact.Email = nil
		} else {
			contact.Email = body.Email
		}
	}
	if body.Company != nil {
		if *body.Company == "" {
			contact.Company = nil
		} else {
			contact.Company = body.Company
		}
	}
	if body.Age != nil {
		if *body.Age == 0 {
			contact.Age = nil
		} else {
			contact.Age = body.Age
		}
	}
	if body.DNI != nil {
		if *body.DNI == "" {
			contact.DNI = nil
		} else {
			contact.DNI = body.DNI
		}
	}
	if body.BirthDate != nil {
		if *body.BirthDate == "" {
			contact.BirthDate = nil
		} else {
			if t, err := time.Parse("2006-01-02", *body.BirthDate); err == nil {
				contact.BirthDate = &t
			}
		}
	}
	if body.Address != nil {
		if *body.Address == "" {
			contact.Address = nil
		} else {
			contact.Address = body.Address
		}
	}
	if body.Distrito != nil {
		if *body.Distrito == "" {
			contact.Distrito = nil
		} else {
			contact.Distrito = body.Distrito
		}
	}
	if body.Ocupacion != nil {
		if *body.Ocupacion == "" {
			contact.Ocupacion = nil
		} else {
			contact.Ocupacion = body.Ocupacion
		}
	}
	if body.Tags != nil {
		contact.Tags = body.Tags
	}
	if body.Notes != nil {
		if *body.Notes == "" {
			contact.Notes = nil
		} else {
			contact.Notes = body.Notes
		}
	}

	if err := s.services.Contact.Update(c.Context(), contact); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Sync tags to contact_tags table
	if body.Tags != nil {
		_ = s.repos.Tag.SyncContactTagsByNames(c.Context(), contact.AccountID, contact.ID, body.Tags)
	}

	// Sync shared fields to all linked event_participants
	_ = s.services.Contact.SyncToParticipants(c.Context(), contact)

	// Sync shared fields to linked lead
	_ = s.services.Contact.SyncToLead(c.Context(), contact)

	if body.CustomName != nil || body.LastName != nil || body.ShortName != nil || body.Age != nil || body.DNI != nil || body.BirthDate != nil || body.Ocupacion != nil {
		if kommoSync := s.kommoForAccount(c.Context(), contact.AccountID); kommoSync != nil {
			queuedViaLead := false
			if lead, err := s.repos.Lead.GetByContactID(c.Context(), contact.ID); err == nil && lead != nil && lead.KommoID != nil && *lead.KommoID > 0 {
				kommoSync.EnqueuePushLeadName(contact.AccountID, lead.ID)
				queuedViaLead = true
			}
			if !queuedViaLead {
				kommoSync.EnqueuePushContactProfile(contact.AccountID, contact.ID)
			}
		}
	}

	// Broadcast contact update via WebSocket
	s.hub.BroadcastToAccount(contact.AccountID, ws.EventContactUpdate, map[string]interface{}{
		"action":     "updated",
		"contact_id": contact.ID.String(),
		"jid":        contact.JID,
	})

	// Auto-sync to Google Contacts if synced
	if s.googleClient != nil && contact.GoogleSync {
		log.Printf("[GOOGLE] Auto-sync triggered from handleUpdateContact for contact %s (google_sync=%v)", contact.ID, contact.GoogleSync)
		go func() {
			if _, err := s.syncContactToGoogle(context.Background(), contact.AccountID, contact.ID); err != nil {
				log.Printf("[GOOGLE] Auto-sync contact %s failed: %v", contact.ID, err)
			}
		}()
	} else if s.googleClient != nil && !contact.GoogleSync {
		log.Printf("[GOOGLE] Auto-sync skipped for contact %s: google_sync=false", contact.ID)
	}

	// Populate structured_tags so the frontend keeps tags in sync after an update.
	if tags, err := s.services.Tag.GetByEntity(c.Context(), "contact", contact.ID); err == nil {
		contact.StructuredTags = tags
	}

	// Populate structured_tags so the frontend keeps tags in sync after an update.
	if tags, err := s.services.Tag.GetByEntity(c.Context(), "contact", contact.ID); err == nil {
		contact.StructuredTags = tags
	}

	s.invalidateContactsCache(contact.AccountID)
	return c.JSON(fiber.Map{"success": true, "contact": contact})
}

func (s *Server) handleResetContactFromDevice(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid id"})
	}
	if contact, _ := s.services.Contact.GetByID(c.Context(), id); contact == nil || contact.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "contact not found"})
	}

	if err := s.services.Contact.ResetFromDevice(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Return updated contact
	contact, _ := s.services.Contact.GetByID(c.Context(), id)
	return c.JSON(fiber.Map{"success": true, "contact": contact})
}

func (s *Server) handleDeleteContact(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid id"})
	}

	if err := s.services.Contact.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateContactsCache(accountID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleGetContactLeads(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	contactID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid id"})
	}

	rows, err := s.repos.DB().Query(c.Context(), `
		SELECT l.id, l.account_id, l.contact_id, l.jid,
		       COALESCE(c.custom_name, c.name, l.name) AS name,
		       COALESCE(c.last_name, l.last_name) AS last_name,
		       COALESCE(c.phone, l.phone) AS phone,
		       COALESCE(c.email, l.email) AS email,
		       l.pipeline_id, l.stage_id,
		       ps.name AS stage_name, ps.color AS stage_color,
		       pp.name AS pipeline_name,
		       l.is_archived, l.is_blocked, l.created_at
		FROM leads l
		LEFT JOIN contacts c ON c.id = l.contact_id
		LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id
		LEFT JOIN pipelines pp ON pp.id = l.pipeline_id
		WHERE l.account_id = $1 AND l.contact_id = $2
		ORDER BY l.created_at DESC
	`, accountID, contactID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	type contactLead struct {
		ID           uuid.UUID     `json:"id"`
		AccountID    uuid.UUID     `json:"account_id"`
		ContactID    *uuid.UUID    `json:"contact_id"`
		JID          *string       `json:"jid"`
		Name         *string       `json:"name"`
		LastName     *string       `json:"last_name"`
		Phone        *string       `json:"phone"`
		Email        *string       `json:"email"`
		PipelineID   *uuid.UUID    `json:"pipeline_id"`
		StageID      *uuid.UUID    `json:"stage_id"`
		StageName    *string       `json:"stage_name"`
		StageColor   *string       `json:"stage_color"`
		PipelineName *string       `json:"pipeline_name"`
		IsArchived   bool          `json:"is_archived"`
		IsBlocked    bool          `json:"is_blocked"`
		CreatedAt    time.Time     `json:"created_at"`
		Tags         []*domain.Tag `json:"tags"`
	}

	var leads []contactLead
	for rows.Next() {
		var cl contactLead
		if err := rows.Scan(
			&cl.ID, &cl.AccountID, &cl.ContactID, &cl.JID,
			&cl.Name, &cl.LastName, &cl.Phone, &cl.Email,
			&cl.PipelineID, &cl.StageID, &cl.StageName, &cl.StageColor, &cl.PipelineName,
			&cl.IsArchived, &cl.IsBlocked, &cl.CreatedAt,
		); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		// Load tags for this lead's contact
		tags, _ := s.services.Tag.GetByEntity(c.Context(), "contact", contactID)
		cl.Tags = tags
		leads = append(leads, cl)
	}

	return c.JSON(fiber.Map{"success": true, "leads": leads})
}

func (s *Server) handleDeleteContactsBatch(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var body struct {
		IDs       []uuid.UUID `json:"ids"`
		DeleteAll bool        `json:"delete_all"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid request"})
	}

	if body.DeleteAll {
		if err := s.services.Contact.DeleteAll(c.Context(), accountID); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		s.invalidateContactsCache(accountID)
		return c.JSON(fiber.Map{"success": true, "message": "All contacts deleted"})
	}

	if len(body.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "provide ids array or delete_all"})
	}

	if err := s.services.Contact.DeleteBatch(c.Context(), body.IDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateContactsCache(accountID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleGetContactDuplicates(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	groups, err := s.services.Contact.FindDuplicates(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "duplicates": groups})
}

func (s *Server) handleGetContactLeadDuplicates(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	count, err := s.services.Contact.GetDuplicateLeadsCount(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "count": count})
}

func (s *Server) handleMergeContacts(c *fiber.Ctx) error {
	var body struct {
		KeepID   uuid.UUID   `json:"keep_id"`
		MergeIDs []uuid.UUID `json:"merge_ids"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid body"})
	}
	if len(body.MergeIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "provide merge_ids"})
	}

	if err := s.services.Contact.MergeContacts(c.Context(), body.KeepID, body.MergeIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleSyncDeviceContacts(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid device id"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	if err := s.enforcePlanLimit(c.Context(), accountID, "max_contacts", 1); err != nil {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_contacts"})
	}

	if err := s.services.Contact.SyncDevice(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "sync started"})
}

func (s *Server) handleCreateContact(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var body struct {
		Phone     string   `json:"phone"`
		Name      string   `json:"name"`
		LastName  string   `json:"last_name"`
		Email     string   `json:"email"`
		Company   string   `json:"company"`
		Notes     string   `json:"notes"`
		DNI       string   `json:"dni"`
		BirthDate string   `json:"birth_date"`
		Address   string   `json:"address"`
		Distrito  string   `json:"distrito"`
		Ocupacion string   `json:"ocupacion"`
		Tags      []string `json:"tags"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid body"})
	}
	if err := s.enforcePlanLimit(c.Context(), accountID, "max_contacts", 1); err != nil {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_contacts"})
	}

	normalizedPhone := kommo.NormalizePhone(body.Phone)
	jid := ""
	if normalizedPhone != "" {
		jid = normalizedPhone + "@s.whatsapp.net"
	} else {
		// Contacts without phone — require at least a name
		if strings.TrimSpace(body.Name) == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Se requiere teléfono o nombre"})
		}
		jid = fmt.Sprintf("manual_%s@clarin.contact", uuid.New().String()[:8])
	}

	contact, err := s.services.Contact.GetOrCreate(c.Context(), accountID, nil, jid, normalizedPhone, body.Name, "", false)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	updated := false
	// Set source to manual for manually created contacts
	src := "manual"
	contact.Source = &src
	updated = true
	// When manually creating, set custom_name from the provided name
	if body.Name != "" {
		contact.CustomName = &body.Name
		updated = true
	}
	if body.LastName != "" {
		contact.LastName = &body.LastName
		updated = true
	}
	if body.Email != "" {
		contact.Email = &body.Email
		updated = true
	}
	if body.Company != "" {
		contact.Company = &body.Company
		updated = true
	}
	if body.Notes != "" {
		contact.Notes = &body.Notes
		updated = true
	}
	if len(body.Tags) > 0 {
		contact.Tags = body.Tags
		updated = true
	}
	if body.DNI != "" {
		contact.DNI = &body.DNI
		updated = true
	}
	if body.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", body.BirthDate); err == nil {
			contact.BirthDate = &t
			updated = true
		}
	}
	if body.Address != "" {
		contact.Address = &body.Address
		updated = true
	}
	if body.Distrito != "" {
		contact.Distrito = &body.Distrito
		updated = true
	}
	if body.Ocupacion != "" {
		contact.Ocupacion = &body.Ocupacion
		updated = true
	}
	if updated {
		_ = s.services.Contact.Update(c.Context(), contact)
	}

	// Sync tags to contact_tags table
	if len(body.Tags) > 0 {
		_ = s.repos.Tag.SyncContactTagsByNames(c.Context(), accountID, contact.ID, body.Tags)
	}

	tags, _ := s.services.Tag.GetByEntity(c.Context(), "contact", contact.ID)
	contact.StructuredTags = tags

	s.invalidateContactsCache(accountID)
	return c.Status(201).JSON(fiber.Map{"success": true, "contact": contact})
}

func (s *Server) handleCreateContactsBulk(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var body struct {
		Contacts []struct {
			Phone     string   `json:"phone"`
			Name      string   `json:"name"`
			LastName  string   `json:"last_name"`
			Email     string   `json:"email"`
			Company   string   `json:"company"`
			Notes     string   `json:"notes"`
			DNI       string   `json:"dni"`
			BirthDate string   `json:"birth_date"`
			Address   string   `json:"address"`
			Tags      []string `json:"tags"`
		} `json:"contacts"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid body"})
	}
	if len(body.Contacts) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "contacts array is empty"})
	}
	if err := s.enforcePlanLimit(c.Context(), accountID, "max_contacts", len(body.Contacts)); err != nil {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_contacts"})
	}

	created := 0
	skipped := 0
	var importErrors []string

	for i, row := range body.Contacts {
		normalizedPhone := kommo.NormalizePhone(row.Phone)
		if normalizedPhone == "" {
			skipped++
			importErrors = append(importErrors, fmt.Sprintf("fila %d: teléfono inválido (%q)", i+1, row.Phone))
			continue
		}

		jid := normalizedPhone + "@s.whatsapp.net"
		contact, err := s.services.Contact.GetOrCreate(c.Context(), accountID, nil, jid, normalizedPhone, row.Name, "", false)
		if err != nil {
			skipped++
			importErrors = append(importErrors, fmt.Sprintf("fila %d: %s", i+1, err.Error()))
			continue
		}

		updated := false
		if row.LastName != "" {
			contact.LastName = &row.LastName
			updated = true
		}
		if row.Email != "" {
			contact.Email = &row.Email
			updated = true
		}
		if row.Company != "" {
			contact.Company = &row.Company
			updated = true
		}
		if row.Notes != "" {
			contact.Notes = &row.Notes
			updated = true
		}
		if len(row.Tags) > 0 {
			contact.Tags = row.Tags
			updated = true
		}
		if row.DNI != "" {
			contact.DNI = &row.DNI
			updated = true
		}
		if row.BirthDate != "" {
			if t, err := time.Parse("2006-01-02", row.BirthDate); err == nil {
				contact.BirthDate = &t
				updated = true
			}
		}
		if row.Address != "" {
			contact.Address = &row.Address
			updated = true
		}
		// Bulk import = manual source
		src := "manual"
		contact.Source = &src
		updated = true
		if updated {
			_ = s.services.Contact.Update(c.Context(), contact)
		}
		// Sync tags to contact_tags table
		if len(row.Tags) > 0 {
			_ = s.repos.Tag.SyncContactTagsByNames(c.Context(), accountID, contact.ID, row.Tags)
		}
		created++
	}

	s.invalidateContactsCache(accountID)
	return c.JSON(fiber.Map{
		"success": true,
		"created": created,
		"skipped": skipped,
		"errors":  importErrors,
	})
}

// --- Tag Handlers ---

func (s *Server) handleGetTags(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// If limit param is present, use paginated query
	if c.Query("limit") != "" {
		limit := c.QueryInt("limit", 50)
		offset := c.QueryInt("offset", 0)
		search := c.Query("search", "")

		// Redis cache for default paginated load — 30s TTL
		tagsCacheKey := ""
		if search == "" && s.cache != nil {
			tagsCacheKey = fmt.Sprintf("tags:%s:%d:%d", accountID.String(), limit, offset)
			if cached, err := s.cache.Get(c.Context(), tagsCacheKey); err == nil && cached != nil {
				c.Set("Content-Type", "application/json")
				return c.Send(cached)
			}
		}

		tags, total, err := s.services.Tag.ListPaginated(c.Context(), accountID, search, limit, offset)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		if tags == nil {
			tags = make([]*domain.Tag, 0)
		}
		result := fiber.Map{"success": true, "tags": tags, "total": total}

		if tagsCacheKey != "" && s.cache != nil {
			if data, err := json.Marshal(result); err == nil {
				_ = s.cache.Set(c.Context(), tagsCacheKey, data, 30*time.Second)
			}
		}

		return c.JSON(result)
	}

	// Full list (no pagination) — cache for 30s
	tagsCacheKeyAll := ""
	if s.cache != nil {
		tagsCacheKeyAll = fmt.Sprintf("tags:%s:all", accountID.String())
		if cached, err := s.cache.Get(c.Context(), tagsCacheKeyAll); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	tags, err := s.services.Tag.GetByAccountID(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if tags == nil {
		tags = make([]*domain.Tag, 0)
	}
	result := fiber.Map{"success": true, "tags": tags}

	if tagsCacheKeyAll != "" && s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), tagsCacheKeyAll, data, 30*time.Second)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleCreateTag(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	if req.Color == "" {
		req.Color = "#6366f1"
	}
	tag := &domain.Tag{AccountID: accountID, Name: req.Name, Color: req.Color}
	if err := s.services.Tag.Create(c.Context(), tag); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateTagsCache(accountID)
	return c.Status(201).JSON(fiber.Map{"success": true, "tag": tag})
}

func (s *Server) handleUpdateTag(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid tag ID"})
	}
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	tag := &domain.Tag{ID: id, Name: req.Name, Color: req.Color}
	if err := s.services.Tag.Update(c.Context(), tag); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	s.invalidateTagsCache(accountID)
	return c.JSON(fiber.Map{"success": true, "tag": tag})
}

func (s *Server) handleDeleteTagsBatch(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var body struct {
		DeleteAll bool `json:"delete_all"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if body.DeleteAll {
		if err := s.services.Tag.DeleteAll(c.Context(), accountID); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		// Reconcile event participants after bulk tag deletion
		go s.services.Event.ReconcileAllAccountEvents(context.Background(), accountID)
		s.invalidateTagsCache(accountID)
		return c.JSON(fiber.Map{"success": true, "message": "All tags deleted"})
	}

	return c.Status(400).JSON(fiber.Map{"success": false, "error": "provide delete_all: true"})
}

func (s *Server) handleDeleteTag(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid tag ID"})
	}
	if err := s.services.Tag.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	// Reconcile event participants after tag deletion (contact_tags rows were removed)
	go s.services.Event.ReconcileAllAccountEvents(context.Background(), accountID)
	s.invalidateTagsCache(accountID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAssignTag(c *fiber.Ctx) error {
	var req struct {
		EntityType string `json:"entity_type"` // contact, lead, chat
		EntityID   string `json:"entity_id"`
		TagID      string `json:"tag_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	entityID, err := uuid.Parse(req.EntityID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid entity ID"})
	}
	tagID, err := uuid.Parse(req.TagID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid tag ID"})
	}
	if err := s.services.Tag.Assign(c.Context(), req.EntityType, entityID, tagID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Push tag change to Kommo (batched via outbox) — only for leads, NOT contacts
	accountID := c.Locals("account_id").(uuid.UUID)
	if kommoSync := s.kommoForAccount(c.Context(), accountID); kommoSync != nil {
		switch req.EntityType {
		case "lead":
			kommoSync.EnqueuePushLeadTags(accountID, entityID)
		}
	}

	// Event tag auto-sync: when a tag is assigned to a lead, add to matching events
	if req.EntityType == "lead" {
		go s.services.Event.HandleLeadTagAssigned(context.Background(), accountID, entityID, tagID)
		// Fire tag_assigned automation trigger
		s.triggerAutomationTagAssigned(accountID, entityID, tagID)
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleRemoveTag(c *fiber.Ctx) error {
	var req struct {
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		TagID      string `json:"tag_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	entityID, err := uuid.Parse(req.EntityID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid entity ID"})
	}
	tagID, err := uuid.Parse(req.TagID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid tag ID"})
	}
	if err := s.services.Tag.Remove(c.Context(), req.EntityType, entityID, tagID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Push tag change to Kommo (batched via outbox) — only for leads, NOT contacts
	accountID := c.Locals("account_id").(uuid.UUID)
	if kommoSync := s.kommoForAccount(c.Context(), accountID); kommoSync != nil {
		switch req.EntityType {
		case "lead":
			kommoSync.EnqueuePushLeadTags(accountID, entityID)
		}
	}

	// Event tag auto-sync: when a tag is removed from a lead, check event membership
	if req.EntityType == "lead" {
		go s.services.Event.HandleLeadTagRemoved(context.Background(), accountID, entityID, tagID)
		// Fire tag_removed automation trigger
		s.triggerAutomationTagRemoved(accountID, entityID, tagID)
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleGetEntityTags(c *fiber.Ctx) error {
	entityType := c.Params("type")
	entityID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}
	tags, err := s.services.Tag.GetByEntity(c.Context(), entityType, entityID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if tags == nil {
		tags = make([]*domain.Tag, 0)
	}
	return c.JSON(fiber.Map{"success": true, "tags": tags})
}

// --- Campaign Handlers ---

func (s *Server) handleGetCampaigns(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Redis cache — 30s TTL
	campaignsCacheKey := ""
	if s.cache != nil {
		campaignsCacheKey = fmt.Sprintf("campaigns:%s:all", accountID.String())
		if cached, err := s.cache.Get(c.Context(), campaignsCacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	campaigns, err := s.services.Campaign.GetByAccountID(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if campaigns == nil {
		campaigns = make([]*domain.Campaign, 0)
	}
	// Load attachments for each campaign
	for _, camp := range campaigns {
		attachments, _ := s.repos.CampaignAttachment.GetByCampaignID(c.Context(), camp.ID)
		camp.Attachments = attachments
	}

	result := fiber.Map{"success": true, "campaigns": campaigns}

	if campaignsCacheKey != "" && s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), campaignsCacheKey, data, 30*time.Second)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleCreateCampaign(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		Name            string                 `json:"name"`
		DeviceID        string                 `json:"device_id"`
		MessageTemplate string                 `json:"message_template"`
		MediaURL        *string                `json:"media_url"`
		MediaType       *string                `json:"media_type"`
		ScheduledAt     *time.Time             `json:"scheduled_at"`
		Settings        map[string]interface{} `json:"settings"`
		EventID         *string                `json:"event_id"`
		Source          *string                `json:"source"`
		Attachments     []struct {
			MediaURL  string `json:"media_url"`
			MediaType string `json:"media_type"`
			Caption   string `json:"caption"`
			FileName  string `json:"file_name"`
			FileSize  int64  `json:"file_size"`
			Position  int    `json:"position"`
		} `json:"attachments"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" || req.DeviceID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "name and device_id are required"})
	}
	// At least message or attachments required
	if req.MessageTemplate == "" && len(req.Attachments) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "message_template or attachments required"})
	}
	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}
	campaign := &domain.Campaign{
		AccountID:       accountID,
		DeviceID:        deviceID,
		Name:            req.Name,
		MessageTemplate: req.MessageTemplate,
		MediaURL:        req.MediaURL,
		MediaType:       req.MediaType,
		ScheduledAt:     req.ScheduledAt,
		Settings:        req.Settings,
	}
	// Set created_by from authenticated user
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		campaign.CreatedBy = &userID
	}
	if req.EventID != nil {
		eid, err := uuid.Parse(*req.EventID)
		if err == nil {
			campaign.EventID = &eid
		}
	}
	if req.Source != nil {
		campaign.Source = req.Source
	}
	if err := s.services.Campaign.Create(c.Context(), campaign); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Save attachments if provided
	if len(req.Attachments) > 0 {
		var attachments []*domain.CampaignAttachment
		for _, a := range req.Attachments {
			attachments = append(attachments, &domain.CampaignAttachment{
				MediaURL:  a.MediaURL,
				MediaType: a.MediaType,
				Caption:   a.Caption,
				FileName:  a.FileName,
				FileSize:  a.FileSize,
				Position:  a.Position,
			})
		}
		if err := s.repos.CampaignAttachment.CreateBatch(c.Context(), campaign.ID, attachments); err != nil {
			log.Printf("[Campaign] Failed to save attachments: %v", err)
		}
		campaign.Attachments = attachments
	}

	s.invalidateCampaignsCache(accountID)
	return c.Status(201).JSON(fiber.Map{"success": true, "campaign": campaign})
}

func (s *Server) handleGetCampaign(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	campaign, err := s.services.Campaign.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if campaign == nil || campaign.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Campaign not found"})
	}
	// Load attachments
	attachments, _ := s.repos.CampaignAttachment.GetByCampaignID(c.Context(), id)
	campaign.Attachments = attachments
	return c.JSON(fiber.Map{"success": true, "campaign": campaign})
}

func (s *Server) handleUpdateCampaign(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	campaign, err := s.services.Campaign.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if campaign == nil || campaign.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Campaign not found"})
	}
	var req struct {
		Name            *string                `json:"name"`
		DeviceID        *string                `json:"device_id"`
		MessageTemplate *string                `json:"message_template"`
		MediaURL        *string                `json:"media_url"`
		MediaType       *string                `json:"media_type"`
		ScheduledAt     *time.Time             `json:"scheduled_at"`
		Status          *string                `json:"status"`
		Settings        map[string]interface{} `json:"settings"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		campaign.Name = *req.Name
	}
	if req.DeviceID != nil {
		if did, err := uuid.Parse(*req.DeviceID); err == nil {
			campaign.DeviceID = did
		}
	}
	if req.MessageTemplate != nil {
		campaign.MessageTemplate = *req.MessageTemplate
	}
	if req.MediaURL != nil {
		campaign.MediaURL = req.MediaURL
	}
	if req.MediaType != nil {
		campaign.MediaType = req.MediaType
	}
	if req.ScheduledAt != nil {
		campaign.ScheduledAt = req.ScheduledAt
	}
	if req.Status != nil && (*req.Status == domain.CampaignStatusScheduled || *req.Status == domain.CampaignStatusDraft) {
		campaign.Status = *req.Status
	}
	if req.Settings != nil {
		campaign.Settings = req.Settings
	}
	if err := s.services.Campaign.Update(c.Context(), campaign); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	// Load attachments for response
	attachments, _ := s.repos.CampaignAttachment.GetByCampaignID(c.Context(), campaign.ID)
	campaign.Attachments = attachments
	s.invalidateCampaignsCache(campaign.AccountID)
	return c.JSON(fiber.Map{"success": true, "campaign": campaign})
}

func (s *Server) handleDeleteCampaign(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	if err := s.services.Campaign.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateCampaignsCache(accountID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleBatchDeleteCampaigns(c *fiber.Ctx) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	deleted := 0
	for _, idStr := range req.IDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		if err := s.services.Campaign.Delete(c.Context(), id); err == nil {
			deleted++
		}
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	s.invalidateCampaignsCache(accountID)
	return c.JSON(fiber.Map{"success": true, "deleted": deleted})
}

func (s *Server) handleAddCampaignRecipients(c *fiber.Ctx) error {
	campaignID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	acctUUID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		Recipients []struct {
			ContactID *string                `json:"contact_id"`
			JID       string                 `json:"jid"`
			Name      *string                `json:"name"`
			Phone     *string                `json:"phone"`
			Metadata  map[string]interface{} `json:"metadata"`
		} `json:"recipients"`
		SaveAsContacts bool `json:"save_as_contacts"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	var recipients []*domain.CampaignRecipient
	for _, r := range req.Recipients {
		rec := &domain.CampaignRecipient{
			CampaignID: campaignID,
			JID:        r.JID,
			Name:       r.Name,
			Phone:      r.Phone,
			Metadata:   r.Metadata,
		}
		if r.ContactID != nil {
			if cid, err := uuid.Parse(*r.ContactID); err == nil {
				rec.ContactID = &cid
			}
		}
		// Optionally create/link as contacts
		if req.SaveAsContacts && rec.ContactID == nil && r.Phone != nil && *r.Phone != "" {
			jid := r.JID
			phone := *r.Phone
			name := ""
			if r.Name != nil {
				name = *r.Name
			}
			contact, err := s.services.Contact.GetOrCreate(c.Context(), acctUUID, nil, jid, phone, name, "", false)
			if err == nil && contact != nil {
				rec.ContactID = &contact.ID
			}
		}
		// Auto-populate nombre_corto from contact's short_name if not already set
		if rec.ContactID != nil {
			if rec.Metadata == nil || rec.Metadata["nombre_corto"] == nil || rec.Metadata["nombre_corto"] == "" {
				ct, _ := s.repos.Contact.GetByID(c.Context(), *rec.ContactID)
				if ct != nil && ct.ShortName != nil && *ct.ShortName != "" {
					if rec.Metadata == nil {
						rec.Metadata = make(map[string]interface{})
					}
					rec.Metadata["nombre_corto"] = *ct.ShortName
				}
			}
		}
		recipients = append(recipients, rec)
	}
	if err := s.services.Campaign.AddRecipients(c.Context(), recipients); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "count": len(recipients)})
}

// handleAddCampaignRecipientsFromLeads adds all leads matching filter criteria
// as campaign recipients server-side. This avoids the client-side pagination
// limitation where only loaded leads (e.g. 50) would be sent.
func (s *Server) handleAddCampaignRecipientsFromLeads(c *fiber.Ctx) error {
	campaignID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)

	// Parse the same filter params used by the leads list endpoint
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw := c.Query("tag_formula")
	stageIDsRaw := c.Query("stage_ids")
	pipelineID := c.Query("pipeline_id")

	// Parse device_ids
	deviceIDs := c.Context().QueryArgs().PeekMulti("device_ids")
	var deviceUUIDs []uuid.UUID
	for _, did := range deviceIDs {
		if id, err := uuid.Parse(string(did)); err == nil {
			deviceUUIDs = append(deviceUUIDs, id)
		}
	}

	// Build WHERE — same logic as handleGetLeadsListPaginated
	args := []interface{}{accountID}
	argIdx := 2
	whereClauses := []string{"l.account_id = $1", "COALESCE(c.phone, l.phone, '') != ''", "l.is_blocked = false", "l.is_archived = false"}

	if pipelineID == "__no_pipeline__" {
		whereClauses = append(whereClauses, "l.pipeline_id IS NULL")
	} else if pipelineID != "" {
		if pid, err := uuid.Parse(pipelineID); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("l.pipeline_id = $%d", argIdx))
			args = append(args, pid)
			argIdx++
		}
	}
	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(c.name,l.name,'')) LIKE $%d OR LOWER(COALESCE(c.phone,l.phone,'')) LIKE $%d OR LOWER(COALESCE(c.email,l.email,'')) LIKE $%d OR LOWER(COALESCE(c.company,l.company,'')) LIKE $%d OR LOWER(COALESCE(c.last_name,l.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}
	if len(deviceUUIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($%d))", argIdx))
		args = append(args, deviceUUIDs)
		argIdx++
	}
	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}
	if tagFormulaRaw != "" {
		fSQL, newArgs, newIdx, fErr := buildAdvancedFormulaSQL(tagFormulaRaw, accountID, args, argIdx)
		if fErr != nil {
			log.Printf("[LEADS] Formula parse/build error (contacts): %v (formula: %s)", fErr, tagFormulaRaw)
		} else if fSQL != "" {
			whereClauses = append(whereClauses, fSQL)
			args = newArgs
			argIdx = newIdx
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		tagSQL, newArgs, newIdx := buildTagFormulaSQL(tagNames, excludeTagNames, tagMode, accountID, args, argIdx)
		if tagSQL != "" {
			whereClauses = append(whereClauses, tagSQL)
			args = newArgs
			argIdx = newIdx
		}
	}
	if stageIDsRaw != "" {
		var validStageIDs []uuid.UUID
		for _, sid := range strings.Split(stageIDsRaw, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageIDs = append(validStageIDs, id)
			}
		}
		if len(validStageIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("l.stage_id = ANY($%d)", argIdx))
			args = append(args, validStageIDs)
			argIdx++
		}
	}

	addDateFilter(c, "l", leadDateFields, &whereClauses, &args, &argIdx)

	whereSQL := strings.Join(whereClauses, " AND ")

	// Query all matching leads with phone — no LIMIT (we need all for the campaign)
	q := fmt.Sprintf(`
		SELECT l.id, COALESCE(c.name,l.name,''), COALESCE(c.last_name,l.last_name,''), COALESCE(c.short_name,l.short_name,''),
		       COALESCE(c.phone,l.phone,''), COALESCE(c.company,l.company,''), l.contact_id
		FROM leads l
		LEFT JOIN contacts c ON c.id = l.contact_id
		WHERE %s
		ORDER BY l.updated_at DESC
	`, whereSQL)

	rows, err := s.repos.DB().Query(c.Context(), q, args...)
	if err != nil {
		log.Printf("[API] Error querying leads for campaign recipients: %v", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to query leads"})
	}
	defer rows.Close()

	var recipients []*domain.CampaignRecipient
	for rows.Next() {
		var id uuid.UUID
		var name, lastName, shortName, phone, company string
		var contactID *uuid.UUID
		if err := rows.Scan(&id, &name, &lastName, &shortName, &phone, &company, &contactID); err != nil {
			continue
		}
		cleanPhone := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, phone)
		if cleanPhone == "" {
			continue
		}
		jid := cleanPhone + "@s.whatsapp.net"

		meta := make(map[string]interface{})
		if shortName != "" {
			meta["nombre_corto"] = shortName
		}
		if company != "" {
			meta["empresa"] = company
		}

		fullName := name
		if lastName != "" {
			fullName = name + " " + lastName
		}

		rec := &domain.CampaignRecipient{
			CampaignID: campaignID,
			JID:        jid,
			Name:       &fullName,
			Phone:      &cleanPhone,
			ContactID:  contactID,
			Metadata:   meta,
		}
		recipients = append(recipients, rec)
	}

	if len(recipients) == 0 {
		return c.JSON(fiber.Map{"success": true, "count": 0, "message": "No leads with phone found matching filters"})
	}

	if err := s.services.Campaign.AddRecipients(c.Context(), recipients); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	log.Printf("[API] Added %d recipients from leads to campaign %s", len(recipients), campaignID)
	return c.JSON(fiber.Map{"success": true, "count": len(recipients)})
}

func (s *Server) handleGetCampaignRecipients(c *fiber.Ctx) error {
	campaignID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	recipients, err := s.services.Campaign.GetRecipients(c.Context(), campaignID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if recipients == nil {
		recipients = make([]*domain.CampaignRecipient, 0)
	}

	// Enrich recipients with lead_id via contact_id, fallback to JID
	contactIDs := make([]uuid.UUID, 0, len(recipients))
	jids := make([]string, 0, len(recipients))
	for _, r := range recipients {
		if r.ContactID != nil {
			contactIDs = append(contactIDs, *r.ContactID)
		} else if r.JID != "" {
			jids = append(jids, r.JID)
		}
	}

	// Map contact_id → lead_id
	contactToLead := make(map[uuid.UUID]string)
	if len(contactIDs) > 0 {
		rows, err := s.repos.DB().Query(c.Context(), `SELECT id, contact_id FROM leads WHERE contact_id = ANY($1)`, contactIDs)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var lid, cid uuid.UUID
				if rows.Scan(&lid, &cid) == nil {
					contactToLead[cid] = lid.String()
				}
			}
		}
	}

	// Fallback: JID → lead_id (for recipients without contact_id)
	jidToLead := make(map[string]string)
	if len(jids) > 0 {
		rows, err := s.repos.DB().Query(c.Context(), `SELECT l.id, l.jid FROM leads l WHERE l.jid = ANY($1)`, jids)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var lid uuid.UUID
				var jid string
				if rows.Scan(&lid, &jid) == nil {
					jidToLead[jid] = lid.String()
				}
			}
		}
	}

	// Bulk-fetch contact short_name for metadata enrichment
	type contactInfo struct {
		shortName string
	}
	contactShortNames := make(map[uuid.UUID]contactInfo)
	if len(contactIDs) > 0 {
		cRows, cErr := s.repos.DB().Query(c.Context(),
			`SELECT id, COALESCE(short_name,'') FROM contacts WHERE id = ANY($1)`, contactIDs)
		if cErr == nil {
			for cRows.Next() {
				var cid uuid.UUID
				var ci contactInfo
				if cRows.Scan(&cid, &ci.shortName) == nil {
					contactShortNames[cid] = ci
				}
			}
			cRows.Close()
		}
	}

	type enrichedRecipient struct {
		*domain.CampaignRecipient
		LeadID *string `json:"lead_id,omitempty"`
	}
	enriched := make([]enrichedRecipient, len(recipients))
	for i, r := range recipients {
		enriched[i] = enrichedRecipient{CampaignRecipient: r}
		if r.ContactID != nil {
			if lid, ok := contactToLead[*r.ContactID]; ok {
				enriched[i].LeadID = &lid
			}
			// Enrich metadata with contact short_name if missing
			if ci, ok := contactShortNames[*r.ContactID]; ok && ci.shortName != "" {
				if r.Metadata == nil {
					r.Metadata = make(map[string]interface{})
				}
				if r.Metadata["nombre_corto"] == nil || r.Metadata["nombre_corto"] == "" {
					r.Metadata["nombre_corto"] = ci.shortName
				}
			}
		} else if r.JID != "" {
			if lid, ok := jidToLead[r.JID]; ok {
				enriched[i].LeadID = &lid
			}
		}
	}

	return c.JSON(fiber.Map{"success": true, "recipients": enriched})
}

func (s *Server) handleDeleteCampaignRecipient(c *fiber.Ctx) error {
	campaignID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	recipientID, err := uuid.Parse(c.Params("rid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid recipient ID"})
	}
	if err := s.services.Campaign.DeleteRecipient(c.Context(), campaignID, recipientID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleUpdateCampaignRecipient(c *fiber.Ctx) error {
	campaignID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	recipientID, err := uuid.Parse(c.Params("rid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid recipient ID"})
	}
	var body struct {
		Name     *string                `json:"name"`
		Phone    *string                `json:"phone"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request body"})
	}
	rec, err := s.services.Campaign.UpdateRecipientData(c.Context(), campaignID, recipientID, body.Name, body.Phone, body.Metadata)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Enrich with lead_id (contact_id first, then JID fallback)
	type enrichedRecipient struct {
		*domain.CampaignRecipient
		LeadID *string `json:"lead_id,omitempty"`
	}
	result := enrichedRecipient{CampaignRecipient: rec}
	var lid uuid.UUID
	if rec.ContactID != nil {
		if err := s.repos.DB().QueryRow(c.Context(), `SELECT id FROM leads WHERE contact_id = $1 LIMIT 1`, *rec.ContactID).Scan(&lid); err == nil {
			lidStr := lid.String()
			result.LeadID = &lidStr
		}
	} else if rec.JID != "" {
		if err := s.repos.DB().QueryRow(c.Context(), `SELECT id FROM leads WHERE jid = $1 LIMIT 1`, rec.JID).Scan(&lid); err == nil {
			lidStr := lid.String()
			result.LeadID = &lidStr
		}
	}

	return c.JSON(fiber.Map{"success": true, "recipient": result})
}

func (s *Server) handleStartCampaign(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	var startedBy *uuid.UUID
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		startedBy = &userID
	}
	if err := s.services.Campaign.Start(c.Context(), id, startedBy); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Campaign started"})
}

func (s *Server) handlePauseCampaign(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	if err := s.services.Campaign.Pause(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Campaign paused"})
}

func (s *Server) handleCancelCampaign(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	if err := s.services.Campaign.Cancel(c.Context(), id); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Campaign cancelled"})
}

func (s *Server) handleRetryCampaignRecipient(c *fiber.Ctx) error {
	campaignID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	recipientID, err := uuid.Parse(c.Params("rid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid recipient ID"})
	}
	if err := s.services.Campaign.RetryRecipient(c.Context(), campaignID, recipientID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Mensaje reenviado exitosamente"})
}

func (s *Server) handleDuplicateCampaign(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	var req struct {
		MessageTemplate *string `json:"message_template"`
	}
	c.BodyParser(&req)
	newCampaign, err := s.services.Campaign.Duplicate(c.Context(), id, req.MessageTemplate)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "campaign": newCampaign})
}

func (s *Server) handleUpdateCampaignAttachments(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid campaign ID"})
	}
	var req struct {
		Attachments []struct {
			MediaURL  string `json:"media_url"`
			MediaType string `json:"media_type"`
			Caption   string `json:"caption"`
			FileName  string `json:"file_name"`
			FileSize  int64  `json:"file_size"`
			Position  int    `json:"position"`
		} `json:"attachments"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	// Delete existing and re-create
	s.repos.CampaignAttachment.DeleteByCampaignID(c.Context(), id)
	if len(req.Attachments) > 0 {
		var attachments []*domain.CampaignAttachment
		for _, a := range req.Attachments {
			attachments = append(attachments, &domain.CampaignAttachment{
				MediaURL:  a.MediaURL,
				MediaType: a.MediaType,
				Caption:   a.Caption,
				FileName:  a.FileName,
				FileSize:  a.FileSize,
				Position:  a.Position,
			})
		}
		if err := s.repos.CampaignAttachment.CreateBatch(c.Context(), id, attachments); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}
	result, _ := s.repos.CampaignAttachment.GetByCampaignID(c.Context(), id)
	return c.JSON(fiber.Map{"success": true, "attachments": result})
}

// --- People Unified Search Handler ---

func (s *Server) handleSearchPeople(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	search := c.Query("search", "")
	sourceType := c.Query("type", "all") // "all", "contact", "lead"
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)
	hasPhone := c.QueryBool("has_phone", false)

	if limit > 200 {
		limit = 200
	}

	var tagIDs []uuid.UUID
	if tagIDsStr := c.Query("tag_ids"); tagIDsStr != "" {
		for _, tidStr := range strings.Split(tagIDsStr, ",") {
			if tid, err := uuid.Parse(strings.TrimSpace(tidStr)); err == nil {
				tagIDs = append(tagIDs, tid)
			}
		}
	}

	// Build shared args: $1 = accountID, $2 = search pattern (if any), $3 = tagIDs (if any)
	args := []interface{}{accountID} // $1
	argNum := 2

	searchArgNum := 0
	if search != "" {
		searchArgNum = argNum
		args = append(args, "%"+search+"%")
		argNum++
	}

	tagArgNum := 0
	if len(tagIDs) > 0 {
		tagArgNum = argNum
		args = append(args, tagIDs)
		argNum++
	}

	var parts []string

	// Contacts sub-query
	if sourceType == "all" || sourceType == "contact" {
		q := `SELECT id, COALESCE(custom_name, name, push_name, phone, jid) as display_name,
		             COALESCE(phone, '') as phone, COALESCE(email, '') as email, 'contact'::text as source_type
		      FROM contacts WHERE account_id = $1 AND is_group = false`
		if searchArgNum > 0 {
			q += fmt.Sprintf(` AND (name ILIKE $%d OR custom_name ILIKE $%d OR push_name ILIKE $%d OR phone ILIKE $%d OR email ILIKE $%d)`,
				searchArgNum, searchArgNum, searchArgNum, searchArgNum, searchArgNum)
		}
		if hasPhone {
			q += " AND phone IS NOT NULL AND phone != ''"
		}
		if tagArgNum > 0 {
			q += fmt.Sprintf(` AND id IN (SELECT contact_id FROM contact_tags WHERE tag_id = ANY($%d))`, tagArgNum)
		}
		parts = append(parts, q)
	}

	// Leads sub-query
	if sourceType == "all" || sourceType == "lead" {
		q := `SELECT id, COALESCE(name, phone, '') as display_name,
		             COALESCE(phone, '') as phone, COALESCE(email, '') as email, 'lead'::text as source_type
		      FROM leads WHERE account_id = $1`
		if searchArgNum > 0 {
			q += fmt.Sprintf(` AND (name ILIKE $%d OR last_name ILIKE $%d OR phone ILIKE $%d OR email ILIKE $%d OR company ILIKE $%d)`,
				searchArgNum, searchArgNum, searchArgNum, searchArgNum, searchArgNum)
		}
		if hasPhone {
			q += " AND phone IS NOT NULL AND phone != ''"
		}
		if tagArgNum > 0 {
			q += fmt.Sprintf(` AND id IN (SELECT l.id FROM leads l JOIN contact_tags ct ON ct.contact_id = l.contact_id WHERE ct.tag_id = ANY($%d))`, tagArgNum)
		}
		parts = append(parts, q)
	}

	if len(parts) == 0 {
		return c.JSON(fiber.Map{"success": true, "people": []domain.Person{}, "total": 0, "limit": limit, "offset": offset})
	}

	unionQuery := strings.Join(parts, " UNION ALL ")

	// Count
	var total int
	if err := s.repos.DB().QueryRow(c.Context(), fmt.Sprintf("SELECT COUNT(*) FROM (%s) sub", unionQuery), args...).Scan(&total); err != nil {
		log.Printf("[API] Error counting people search: %v", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "internal error"})
	}

	// Data with pagination
	dataQuery := fmt.Sprintf(
		"SELECT id, display_name, phone, email, source_type FROM (%s) sub ORDER BY display_name ASC LIMIT $%d OFFSET $%d",
		unionQuery, argNum, argNum+1,
	)
	args = append(args, limit, offset)

	rows, err := s.repos.DB().Query(c.Context(), dataQuery, args...)
	if err != nil {
		log.Printf("[API] Error searching people: %v", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "internal error"})
	}
	defer rows.Close()

	people := make([]domain.Person, 0, limit)
	contactIDs := make([]uuid.UUID, 0)
	leadIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var p domain.Person
		if err := rows.Scan(&p.ID, &p.Name, &p.Phone, &p.Email, &p.SourceType); err != nil {
			continue
		}
		people = append(people, p)
		if p.SourceType == "contact" {
			contactIDs = append(contactIDs, p.ID)
		} else {
			leadIDs = append(leadIDs, p.ID)
		}
	}

	// Batch load tags
	tagMap := make(map[uuid.UUID][]*domain.Tag)

	if len(contactIDs) > 0 {
		tagRows, err := s.repos.DB().Query(c.Context(), `
			SELECT ct.contact_id, t.id, t.name, t.color
			FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id
			WHERE ct.contact_id = ANY($1) ORDER BY t.name
		`, contactIDs)
		if err == nil {
			defer tagRows.Close()
			for tagRows.Next() {
				var entityID uuid.UUID
				tag := &domain.Tag{}
				if err := tagRows.Scan(&entityID, &tag.ID, &tag.Name, &tag.Color); err == nil {
					tagMap[entityID] = append(tagMap[entityID], tag)
				}
			}
		}
	}

	if len(leadIDs) > 0 {
		tagRows, err := s.repos.DB().Query(c.Context(), `
			SELECT l.id, t.id, t.name, t.color
			FROM leads l JOIN contact_tags ct ON ct.contact_id = l.contact_id JOIN tags t ON t.id = ct.tag_id
			WHERE l.id = ANY($1) ORDER BY t.name
		`, leadIDs)
		if err == nil {
			defer tagRows.Close()
			for tagRows.Next() {
				var entityID uuid.UUID
				tag := &domain.Tag{}
				if err := tagRows.Scan(&entityID, &tag.ID, &tag.Name, &tag.Color); err == nil {
					tagMap[entityID] = append(tagMap[entityID], tag)
				}
			}
		}
	}

	for i := range people {
		people[i].Tags = tagMap[people[i].ID]
	}

	return c.JSON(fiber.Map{
		"success": true,
		"people":  people,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// --- Event Handlers ---

func (s *Server) handleGetEvents(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	filter := domain.EventFilter{
		Search:       c.Query("search"),
		Status:       c.Query("status"),
		FolderFilter: c.Query("folder"),
		Limit:        c.QueryInt("limit", 50),
		Offset:       c.QueryInt("offset", 0),
	}

	// Redis cache for default load — 30s TTL
	isDefaultEventsLoad := filter.Search == "" && filter.Status == "" && filter.FolderFilter == ""
	eventsCacheKey := ""
	if isDefaultEventsLoad && s.cache != nil {
		eventsCacheKey = fmt.Sprintf("events:%s:%d:%d", accountID.String(), filter.Limit, filter.Offset)
		if cached, err := s.cache.Get(c.Context(), eventsCacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	events, total, err := s.services.Event.GetByAccountID(c.Context(), accountID, filter)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if events == nil {
		events = make([]*domain.Event, 0)
	}
	// Populate tags for all events in one batch query
	if len(events) > 0 {
		eventIDs := make([]uuid.UUID, len(events))
		for i, ev := range events {
			eventIDs[i] = ev.ID
		}
		tagMap, err := s.repos.Event.GetEventTagsBatch(c.Context(), eventIDs)
		if err == nil {
			for _, ev := range events {
				ev.Tags = tagMap[ev.ID]
			}
		}
	}

	result := fiber.Map{"success": true, "events": events, "total": total}

	// Cache default load result
	if eventsCacheKey != "" && s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), eventsCacheKey, data, 30*time.Second)
		}
	}

	return c.JSON(result)
}

func (s *Server) handleCreateEvent(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)
	var req struct {
		Name        string     `json:"name"`
		Description *string    `json:"description"`
		EventDate   *time.Time `json:"event_date"`
		EventEnd    *time.Time `json:"event_end"`
		Location    *string    `json:"location"`
		Color       string     `json:"color"`
		Status      string     `json:"status"`
		PipelineID  *string    `json:"pipeline_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	event := &domain.Event{
		AccountID:   accountID,
		Name:        req.Name,
		Description: req.Description,
		EventDate:   req.EventDate,
		EventEnd:    req.EventEnd,
		Location:    req.Location,
		Color:       req.Color,
		Status:      req.Status,
		CreatedBy:   &userID,
	}
	if req.PipelineID != nil {
		if pid, err := uuid.Parse(*req.PipelineID); err == nil {
			event.PipelineID = &pid
		}
	}
	// If no pipeline specified, assign default
	if event.PipelineID == nil {
		defPipeline, err := s.repos.EventPipeline.EnsureDefaultByAccountID(c.Context(), accountID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		if defPipeline != nil {
			event.PipelineID = &defPipeline.ID
		}
	}
	if err := s.services.Event.Create(c.Context(), event); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateEventsCache(accountID)
	return c.Status(201).JSON(fiber.Map{"success": true, "event": event})
}

func (s *Server) handleGetEvent(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	event, err := s.services.Event.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if event == nil || event.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	// Populate event tags
	event.Tags, _ = s.services.Event.GetEventTags(c.Context(), event.ID)
	return c.JSON(fiber.Map{"success": true, "event": event})
}

func (s *Server) handleUpdateEvent(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	event, err := s.services.Event.GetByID(c.Context(), id)
	if err != nil || event == nil || event.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	var req struct {
		Name        *string    `json:"name"`
		Description *string    `json:"description"`
		EventDate   *time.Time `json:"event_date"`
		EventEnd    *time.Time `json:"event_end"`
		Location    *string    `json:"location"`
		Color       *string    `json:"color"`
		Status      *string    `json:"status"`
		PipelineID  *string    `json:"pipeline_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		event.Name = *req.Name
	}
	if req.Description != nil {
		event.Description = req.Description
	}
	if req.EventDate != nil {
		event.EventDate = req.EventDate
	}
	if req.EventEnd != nil {
		event.EventEnd = req.EventEnd
	}
	if req.Location != nil {
		event.Location = req.Location
	}
	if req.Color != nil {
		event.Color = *req.Color
	}
	if req.Status != nil {
		event.Status = *req.Status
	}
	if req.PipelineID != nil {
		if pid, err := uuid.Parse(*req.PipelineID); err == nil {
			event.PipelineID = &pid
		}
	}
	if err := s.services.Event.Update(c.Context(), event); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateEventsCache(event.AccountID)
	return c.JSON(fiber.Map{"success": true, "event": event})
}

func (s *Server) handleDeleteEvent(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	if err := s.services.Event.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateEventsCache(accountID)
	return c.JSON(fiber.Map{"success": true})
}

// handleGetEventTags returns the tags configured for auto-sync on an event (with negate flag and formula mode).
func (s *Server) handleGetEventTags(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	event, err := s.services.Event.GetByID(c.Context(), eventID)
	if err != nil || event == nil || event.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	tags, err := s.services.Event.GetEventTags(c.Context(), eventID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if tags == nil {
		tags = make([]*domain.Tag, 0)
	}
	mode := event.TagFormulaMode
	if mode == "" {
		mode = "OR"
	}
	formulaType := event.TagFormulaType
	if formulaType == "" {
		formulaType = "simple"
	}
	return c.JSON(fiber.Map{
		"success":          true,
		"tags":             tags,
		"formula_mode":     mode,
		"tag_formula":      event.TagFormula,
		"tag_formula_type": formulaType,
	})
}

// handleSetEventTags sets the tags for auto-sync on an event with formula support (AND/OR + excludes).
func (s *Server) handleSetEventTags(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	event, err := s.services.Event.GetByID(c.Context(), eventID)
	if err != nil || event == nil || event.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}

	var req struct {
		TagIDs         []string `json:"tag_ids"`      // backward compat (all include, no exclude)
		FormulaMode    string   `json:"formula_mode"` // "AND" or "OR" (default "OR")
		IncludeTagIDs  []string `json:"include_tag_ids"`
		ExcludeTagIDs  []string `json:"exclude_tag_ids"`
		TagFormula     string   `json:"tag_formula"`      // text-based formula (advanced mode)
		TagFormulaType string   `json:"tag_formula_type"` // "simple" or "advanced"
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	formulaType := req.TagFormulaType
	if formulaType == "" {
		formulaType = "simple"
	}

	mode := req.FormulaMode
	if mode == "" {
		mode = "OR"
	}

	// Validate advanced formula syntax
	if formulaType == "advanced" && req.TagFormula != "" {
		if err := formula.Validate(req.TagFormula); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid formula: " + err.Error()})
		}
	}

	// Parse include tag UUIDs — prefer include_tag_ids, fall back to tag_ids
	var includes []uuid.UUID
	srcIDs := req.IncludeTagIDs
	if len(srcIDs) == 0 {
		srcIDs = req.TagIDs
	}
	for _, tidStr := range srcIDs {
		tid, err := uuid.Parse(tidStr)
		if err != nil {
			continue
		}
		includes = append(includes, tid)
	}

	// Parse exclude tag UUIDs
	var excludes []uuid.UUID
	for _, tidStr := range req.ExcludeTagIDs {
		tid, err := uuid.Parse(tidStr)
		if err != nil {
			continue
		}
		excludes = append(excludes, tid)
	}

	// Update formula fields on the event
	event.TagFormulaMode = mode
	event.TagFormula = req.TagFormula
	event.TagFormulaType = formulaType
	if err := s.services.Event.Update(c.Context(), event); err != nil {
		log.Printf("[EVENT-SYNC] Error updating formula for event %s: %v", eventID, err)
	}

	// Save simple-mode tag entries (include/exclude)
	if err := s.services.Event.SetEventTags(c.Context(), eventID, includes, excludes); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Trigger async reconciliation if event is active
	if event.Status == domain.EventStatusActive {
		go func() {
			ctx := context.Background()
			var stageID *uuid.UUID
			if event.PipelineID != nil {
				stages, _ := s.services.Event.GetPipelineStages(ctx, *event.PipelineID)
				if len(stages) > 0 {
					stageID = &stages[0].ID
				}
			}

			var added, removed int
			var reconcileErr error
			if formulaType == "advanced" && req.TagFormula != "" {
				added, removed, reconcileErr = s.services.Event.ReconcileEventParticipantsAdvanced(ctx, eventID, event.AccountID, req.TagFormula, stageID)
			} else if len(includes) > 0 {
				added, removed, reconcileErr = s.services.Event.ReconcileEventParticipants(ctx, eventID, event.AccountID, mode, includes, excludes, stageID)
			}

			if reconcileErr != nil {
				log.Printf("[EVENT-SYNC] Error reconciling after tag config change for event '%s': %v", event.Name, reconcileErr)
				return
			}
			if added > 0 || removed > 0 {
				log.Printf("[EVENT-SYNC] Event '%s' tag config changed (type=%s): +%d added, -%d removed", event.Name, formulaType, added, removed)
			}
			if s.hub != nil {
				s.hub.BroadcastToAccount(event.AccountID, "event_participant_update", map[string]interface{}{
					"event_id": eventID,
					"action":   "tag_sync_reconcile",
					"added":    added,
					"removed":  removed,
				})
			}
		}()
	}

	// Return updated tags
	tags, _ := s.services.Event.GetEventTags(c.Context(), eventID)
	if tags == nil {
		tags = make([]*domain.Tag, 0)
	}
	return c.JSON(fiber.Map{
		"success":          true,
		"tags":             tags,
		"formula_mode":     mode,
		"tag_formula":      req.TagFormula,
		"tag_formula_type": formulaType,
	})
}

// handleValidateFormula validates a text-based tag formula and returns its structure.
func (s *Server) handleValidateFormula(c *fiber.Ctx) error {
	var req struct {
		Formula string `json:"formula"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	ast, err := formula.Parse(req.Formula)
	if err != nil {
		return c.JSON(fiber.Map{"success": true, "valid": false, "error": err.Error()})
	}

	literals := formula.ExtractLiterals(ast)
	if literals == nil {
		literals = []string{}
	}

	return c.JSON(fiber.Map{
		"success":  true,
		"valid":    true,
		"literals": literals,
		"tree":     ast.String(),
	})
}

func (s *Server) handleGetEventParticipants(c *fiber.Ctx) error {
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}

	// Pagination
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "10000"))
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}

	// Filters — same params as paginated endpoint
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw := c.Query("tag_formula")
	stageIDsRaw := c.Query("stage_ids")
	var hasPhone *bool
	if hp := c.Query("has_phone"); hp == "true" {
		t := true
		hasPhone = &t
	}

	// Build WHERE
	args := []interface{}{eventID}
	argIdx := 2
	whereClauses := []string{"p.event_id = $1"}

	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(p.name,'')) LIKE $%d OR LOWER(COALESCE(p.phone,'')) LIKE $%d OR LOWER(COALESCE(p.email,'')) LIKE $%d OR LOWER(COALESCE(p.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}

	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}

	if tagFormulaRaw != "" {
		ast, parseErr := formula.Parse(tagFormulaRaw)
		if parseErr == nil && ast != nil {
			innerSQL, innerArgs, buildErr := formula.BuildSQLForParticipants(ast, eventID)
			if buildErr == nil && innerSQL != "" {
				remappedSQL := formula.RemapSQLParams(innerSQL, len(innerArgs), argIdx)
				whereClauses = append(whereClauses, fmt.Sprintf("p.id IN (%s)", remappedSQL))
				args = append(args, innerArgs...)
				argIdx += len(innerArgs)
			}
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		if len(tagNames) > 0 {
			if tagMode == "AND" {
				whereClauses = append(whereClauses, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d) GROUP BY p2.id HAVING COUNT(DISTINCT t.name) = $%d)",
					argIdx, argIdx+1,
				))
				args = append(args, tagNames, len(tagNames))
				argIdx += 2
			} else {
				whereClauses = append(whereClauses, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
					argIdx,
				))
				args = append(args, tagNames)
				argIdx++
			}
		}
		if len(excludeTagNames) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf(
				"p.id NOT IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
				argIdx,
			))
			args = append(args, excludeTagNames)
			argIdx++
		}
	}

	if hasPhone != nil && *hasPhone {
		whereClauses = append(whereClauses, "p.phone IS NOT NULL AND p.phone != ''")
	}

	if stageIDsRaw != "" {
		var validStageIDs []uuid.UUID
		for _, sid := range strings.Split(stageIDsRaw, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageIDs = append(validStageIDs, id)
			}
		}
		if len(validStageIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("p.stage_id = ANY($%d)", argIdx))
			args = append(args, validStageIDs)
			argIdx++
		}
	}

	addDateFilter(c, "p", participantDateFields, &whereClauses, &args, &argIdx)

	whereSQL := strings.Join(whereClauses, " AND ")

	// Count total
	var total int
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM event_participants p WHERE %s", whereSQL)
	_ = s.repos.DB().QueryRow(c.Context(), countQ, args...).Scan(&total)

	// Fetch page
	dataQ := fmt.Sprintf(`
		SELECT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id,
		       p.name, p.last_name, p.short_name, p.phone, p.email, p.age,
		       p.company, p.dni, p.birth_date, p.address, p.distrito, p.ocupacion,
		       p.status, p.notes, p.next_action, p.next_action_date,
		       p.invited_at, p.confirmed_at, p.attended_at,
		       p.created_at, p.updated_at,
		       eps.name AS stage_name, eps.color AS stage_color,
		       l.pipeline_id AS lead_pipeline_id, l.stage_id AS lead_stage_id, lps.name AS lead_stage_name, lps.color AS lead_stage_color,
		       COALESCE(l.is_archived, false) AS is_archived, COALESCE(l.is_blocked, false) AS is_blocked
		FROM event_participants p
		LEFT JOIN event_pipeline_stages eps ON eps.id = p.stage_id
		LEFT JOIN leads l ON l.id = p.lead_id
		LEFT JOIN pipeline_stages lps ON lps.id = l.stage_id
		WHERE %s
		ORDER BY p.next_action_date ASC NULLS LAST, p.name ASC
		OFFSET %d LIMIT %d
	`, whereSQL, offset, limit)

	rows, err := s.repos.DB().Query(c.Context(), dataQ, args...)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	var participants []*domain.EventParticipant
	for rows.Next() {
		p := &domain.EventParticipant{}
		if err := rows.Scan(
			&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID,
			&p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age,
			&p.Company, &p.DNI, &p.BirthDate, &p.Address, &p.Distrito, &p.Ocupacion,
			&p.Status, &p.Notes, &p.NextAction, &p.NextActionDate,
			&p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt,
			&p.CreatedAt, &p.UpdatedAt,
			&p.StageName, &p.StageColor,
			&p.LeadPipelineID, &p.LeadStageID, &p.LeadStageName, &p.LeadStageColor,
			&p.IsArchived, &p.IsBlocked,
		); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		participants = append(participants, p)
	}

	// Load tags for each participant
	for _, p := range participants {
		if p.LeadID == nil {
			p.Tags = make([]*domain.Tag, 0)
			continue
		}
		tagRows, err := s.repos.DB().Query(c.Context(), `
			SELECT t.id, t.account_id, t.name, t.color, t.created_at
			FROM tags t JOIN contact_tags ct ON ct.tag_id = t.id
			JOIN leads l ON l.contact_id = ct.contact_id
			WHERE l.id = $1
		`, *p.LeadID)
		if err == nil {
			for tagRows.Next() {
				tag := &domain.Tag{}
				if err := tagRows.Scan(&tag.ID, &tag.AccountID, &tag.Name, &tag.Color, &tag.CreatedAt); err == nil {
					p.Tags = append(p.Tags, tag)
				}
			}
			tagRows.Close()
		}
		if p.Tags == nil {
			p.Tags = make([]*domain.Tag, 0)
		}
	}

	if participants == nil {
		participants = make([]*domain.EventParticipant, 0)
	}

	// Mark participants that share a contact_id with another participant in this event
	contactCount := make(map[uuid.UUID]int)
	for _, p := range participants {
		if p.ContactID != nil {
			contactCount[*p.ContactID]++
		}
	}
	for _, p := range participants {
		if p.ContactID != nil && contactCount[*p.ContactID] > 1 {
			p.DuplicateContact = true
		}
	}

	return c.JSON(fiber.Map{"success": true, "participants": participants, "total": total})
}

// handleGetEventParticipantsPaginated returns first N participants per stage using ROW_NUMBER()
func (s *Server) handleGetEventParticipantsPaginated(c *fiber.Ctx) error {
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}

	perStage, _ := strconv.Atoi(c.Query("per_stage", "50"))
	if perStage <= 0 || perStage > 200 {
		perStage = 50
	}
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw := c.Query("tag_formula")
	stageIDsRaw := c.Query("stage_ids")
	var hasPhone *bool
	if hp := c.Query("has_phone"); hp == "true" {
		t := true
		hasPhone = &t
	}

	// Build WHERE clause for participants
	args := []interface{}{eventID}
	argIdx := 2
	whereClauses := []string{"p.event_id = $1"}

	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(p.name,'')) LIKE $%d OR LOWER(COALESCE(p.phone,'')) LIKE $%d OR LOWER(COALESCE(p.email,'')) LIKE $%d OR LOWER(COALESCE(p.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}

	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}

	if tagFormulaRaw != "" {
		ast, parseErr := formula.Parse(tagFormulaRaw)
		if parseErr == nil && ast != nil {
			innerSQL, innerArgs, buildErr := formula.BuildSQLForParticipants(ast, eventID)
			if buildErr == nil && innerSQL != "" {
				remappedSQL := formula.RemapSQLParams(innerSQL, len(innerArgs), argIdx)
				whereClauses = append(whereClauses, fmt.Sprintf("p.id IN (%s)", remappedSQL))
				args = append(args, innerArgs...)
				argIdx += len(innerArgs)
			}
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		if len(tagNames) > 0 {
			if tagMode == "AND" {
				whereClauses = append(whereClauses, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d) GROUP BY p2.id HAVING COUNT(DISTINCT t.name) = $%d)",
					argIdx, argIdx+1,
				))
				args = append(args, tagNames, len(tagNames))
				argIdx += 2
			} else {
				whereClauses = append(whereClauses, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
					argIdx,
				))
				args = append(args, tagNames)
				argIdx++
			}
		}
		if len(excludeTagNames) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf(
				"p.id NOT IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
				argIdx,
			))
			args = append(args, excludeTagNames)
			argIdx++
		}
	}

	if hasPhone != nil && *hasPhone {
		whereClauses = append(whereClauses, "p.phone IS NOT NULL AND p.phone != ''")
	}

	if stageIDsRaw != "" {
		var validStageIDs []uuid.UUID
		for _, sid := range strings.Split(stageIDsRaw, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageIDs = append(validStageIDs, id)
			}
		}
		if len(validStageIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("p.stage_id = ANY($%d)", argIdx))
			args = append(args, validStageIDs)
			argIdx++
		}
	}

	addDateFilter(c, "p", participantDateFields, &whereClauses, &args, &argIdx)

	whereSQL := strings.Join(whereClauses, " AND ")

	// Run 5 goroutines in parallel
	type stageInfo struct {
		ID         uuid.UUID
		PipelineID uuid.UUID
		Name       string
		Color      string
		Position   int
	}
	type stageCount struct {
		StageID uuid.UUID
		Count   int
	}

	var (
		stagesList                                             []stageInfo
		counts                                                 []stageCount
		paginatedParts                                         []*domain.EventParticipant
		tagMap                                                 = make(map[uuid.UUID][]*domain.Tag)
		unassignedCount                                        int
		stagesErr, countsErr, partsErr, tagsErr, unassignedErr error
		wg                                                     sync.WaitGroup
	)

	wg.Add(5)

	// Goroutine 1: Fetch pipeline stages for this event
	go func() {
		defer wg.Done()
		rows, err := s.repos.DB().Query(c.Context(), `
			SELECT s.id, s.pipeline_id, s.name, s.color, s.position
			FROM event_pipeline_stages s
			JOIN events e ON e.pipeline_id = s.pipeline_id
			WHERE e.id = $1
			ORDER BY s.position
		`, eventID)
		if err != nil {
			stagesErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var si stageInfo
			if err := rows.Scan(&si.ID, &si.PipelineID, &si.Name, &si.Color, &si.Position); err != nil {
				stagesErr = err
				return
			}
			stagesList = append(stagesList, si)
		}
	}()

	// Goroutine 2: Count participants per stage
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`SELECT p.stage_id, COUNT(*) FROM event_participants p WHERE %s AND p.stage_id IS NOT NULL GROUP BY p.stage_id`, whereSQL)
		rows, err := s.repos.DB().Query(c.Context(), q, args...)
		if err != nil {
			countsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var sc stageCount
			if err := rows.Scan(&sc.StageID, &sc.Count); err != nil {
				countsErr = err
				return
			}
			counts = append(counts, sc)
		}
	}()

	// Goroutine 3: First N participants per stage using ROW_NUMBER()
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`
			WITH ranked AS (
				SELECT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id,
				       p.name, p.last_name, p.short_name, p.phone, p.email, p.age,
				       p.company, p.dni, p.birth_date, p.address, p.distrito, p.ocupacion,
				       p.status, p.notes, p.next_action, p.next_action_date,
				       p.invited_at, p.confirmed_at, p.attended_at,
				       p.created_at, p.updated_at,
				       s.name AS stage_name, s.color AS stage_color, s.position AS stage_position,
				       l.pipeline_id AS lead_pipeline_id, l.stage_id AS lead_stage_id, lps.name AS lead_stage_name, lps.color AS lead_stage_color,
				       COALESCE(l.is_archived, false) AS is_archived, COALESCE(l.is_blocked, false) AS is_blocked,
				       ROW_NUMBER() OVER (PARTITION BY p.stage_id ORDER BY p.created_at DESC) AS rn
				FROM event_participants p
				LEFT JOIN event_pipeline_stages s ON s.id = p.stage_id
				LEFT JOIN leads l ON l.id = p.lead_id
				LEFT JOIN pipeline_stages lps ON lps.id = l.stage_id
				WHERE %s AND p.stage_id IS NOT NULL
			)
			SELECT id, event_id, contact_id, lead_id, stage_id,
			       name, last_name, short_name, phone, email, age,
			       company, dni, birth_date, address, distrito, ocupacion,
			       status, notes, next_action, next_action_date,
			       invited_at, confirmed_at, attended_at,
			       created_at, updated_at,
			       stage_name, stage_color, stage_position,
			       lead_pipeline_id, lead_stage_id, lead_stage_name, lead_stage_color,
			       is_archived, is_blocked
			FROM ranked WHERE rn <= %d
			ORDER BY stage_position NULLS LAST, created_at DESC
		`, whereSQL, perStage)
		rows, err := s.repos.DB().Query(c.Context(), q, args...)
		if err != nil {
			partsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			p := &domain.EventParticipant{}
			var stagePosition *int
			if err := rows.Scan(
				&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID,
				&p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age,
				&p.Company, &p.DNI, &p.BirthDate, &p.Address, &p.Distrito, &p.Ocupacion,
				&p.Status, &p.Notes, &p.NextAction, &p.NextActionDate,
				&p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt,
				&p.CreatedAt, &p.UpdatedAt,
				&p.StageName, &p.StageColor, &stagePosition,
				&p.LeadPipelineID, &p.LeadStageID, &p.LeadStageName, &p.LeadStageColor,
				&p.IsArchived, &p.IsBlocked,
			); err != nil {
				partsErr = err
				return
			}
			paginatedParts = append(paginatedParts, p)
		}
	}()

	// Goroutine 4: Tags for all event participants (via contact_tags, with lead fallback)
	go func() {
		defer wg.Done()
		rows, err := s.repos.DB().Query(c.Context(), `
			SELECT p.id, t.id, t.account_id, t.name, t.color
			FROM event_participants p
			LEFT JOIN leads l ON l.id = p.lead_id
			JOIN contact_tags ct ON ct.contact_id = COALESCE(p.contact_id, l.contact_id)
			JOIN tags t ON t.id = ct.tag_id
			WHERE p.event_id = $1 AND COALESCE(p.contact_id, l.contact_id) IS NOT NULL
			ORDER BY t.name
		`, eventID)
		if err != nil {
			tagsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var partID uuid.UUID
			t := &domain.Tag{}
			if err := rows.Scan(&partID, &t.ID, &t.AccountID, &t.Name, &t.Color); err != nil {
				continue
			}
			tagMap[partID] = append(tagMap[partID], t)
		}
	}()

	// Goroutine 5: Unassigned participants count
	go func() {
		defer wg.Done()
		q := fmt.Sprintf(`SELECT COUNT(*) FROM event_participants p WHERE %s AND (p.stage_id IS NULL)`, whereSQL)
		err := s.repos.DB().QueryRow(c.Context(), q, args...).Scan(&unassignedCount)
		if err != nil {
			unassignedErr = err
		}
	}()

	wg.Wait()

	if partsErr != nil {
		log.Printf("[EVENTS] Paginated participants error: %v", partsErr)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": partsErr.Error()})
	}
	if countsErr != nil {
		log.Printf("[EVENTS] Counts error: %v", countsErr)
	}
	if stagesErr != nil {
		log.Printf("[EVENTS] Stages error: %v", stagesErr)
	}
	if tagsErr != nil {
		log.Printf("[EVENTS] Tags error: %v", tagsErr)
	}
	if unassignedErr != nil {
		log.Printf("[EVENTS] Unassigned count error: %v", unassignedErr)
	}

	// Assign tags to participants
	for _, p := range paginatedParts {
		p.Tags = tagMap[p.ID]
	}

	// Mark participants that share a contact_id (duplicate leads for same contact)
	contactCountPag := make(map[uuid.UUID]int)
	for _, p := range paginatedParts {
		if p.ContactID != nil {
			contactCountPag[*p.ContactID]++
		}
	}
	for _, p := range paginatedParts {
		if p.ContactID != nil && contactCountPag[*p.ContactID] > 1 {
			p.DuplicateContact = true
		}
	}

	// Build count map
	countMap := make(map[uuid.UUID]int)
	for _, sc := range counts {
		countMap[sc.StageID] = sc.Count
	}

	// Build response stages
	type stageDataResp struct {
		ID           string                     `json:"id"`
		PipelineID   string                     `json:"pipeline_id"`
		Name         string                     `json:"name"`
		Color        string                     `json:"color"`
		Position     int                        `json:"position"`
		TotalCount   int                        `json:"total_count"`
		Participants []*domain.EventParticipant `json:"participants"`
		HasMore      bool                       `json:"has_more"`
	}

	stages := make([]stageDataResp, 0, len(stagesList))
	for _, si := range stagesList {
		total := countMap[si.ID]
		var stageParticipants []*domain.EventParticipant
		for _, p := range paginatedParts {
			if p.StageID != nil && *p.StageID == si.ID {
				stageParticipants = append(stageParticipants, p)
			}
		}
		if stageParticipants == nil {
			stageParticipants = make([]*domain.EventParticipant, 0)
		}
		stages = append(stages, stageDataResp{
			ID:           si.ID.String(),
			PipelineID:   si.PipelineID.String(),
			Name:         si.Name,
			Color:        si.Color,
			Position:     si.Position,
			TotalCount:   total,
			Participants: stageParticipants,
			HasMore:      len(stageParticipants) < total,
		})
	}

	// Unassigned participants
	var unassignedParts []*domain.EventParticipant
	for _, p := range paginatedParts {
		if p.StageID == nil {
			unassignedParts = append(unassignedParts, p)
		}
	}
	// Also query unassigned if not already in paginated (they have stage_id IS NULL excluded from ranked)
	if unassignedCount > 0 {
		q := fmt.Sprintf(`
			SELECT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id,
			       p.name, p.last_name, p.short_name, p.phone, p.email, p.age,
			       p.company, p.dni, p.birth_date, p.address, p.distrito, p.ocupacion,
			       p.status, p.notes, p.next_action, p.next_action_date,
			       p.invited_at, p.confirmed_at, p.attended_at,
			       p.created_at, p.updated_at
			FROM event_participants p
			WHERE %s AND p.stage_id IS NULL
			ORDER BY p.created_at DESC
			LIMIT %d
		`, whereSQL, perStage)
		rows, err := s.repos.DB().Query(c.Context(), q, args...)
		if err == nil {
			defer rows.Close()
			unassignedParts = make([]*domain.EventParticipant, 0)
			for rows.Next() {
				p := &domain.EventParticipant{}
				if err := rows.Scan(
					&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID,
					&p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age,
					&p.Company, &p.DNI, &p.BirthDate, &p.Address, &p.Distrito, &p.Ocupacion,
					&p.Status, &p.Notes, &p.NextAction, &p.NextActionDate,
					&p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt,
					&p.CreatedAt, &p.UpdatedAt,
				); err != nil {
					continue
				}
				p.Tags = tagMap[p.ID]
				unassignedParts = append(unassignedParts, p)
			}
		}
	}
	if unassignedParts == nil {
		unassignedParts = make([]*domain.EventParticipant, 0)
	}
	// Mark duplicate contacts in unassigned parts too
	for _, p := range unassignedParts {
		if p.ContactID != nil && contactCountPag[*p.ContactID] > 1 {
			p.DuplicateContact = true
		}
	}

	// All account tags for filter sidebar (via contact_tags, with lead fallback)
	var allTags []fiber.Map
	tagRows, err := s.repos.DB().Query(c.Context(), `
		SELECT t.name, t.color, COUNT(DISTINCT ep.id) as cnt
		FROM event_participants ep
		LEFT JOIN leads l ON l.id = ep.lead_id
		JOIN contact_tags ct ON ct.contact_id = COALESCE(ep.contact_id, l.contact_id)
		JOIN tags t ON t.id = ct.tag_id
		WHERE ep.event_id = $1 AND COALESCE(ep.contact_id, l.contact_id) IS NOT NULL
		GROUP BY t.name, t.color
		ORDER BY cnt DESC, t.name
	`, eventID)
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var name, color string
			var cnt int
			if err := tagRows.Scan(&name, &color, &cnt); err == nil {
				allTags = append(allTags, fiber.Map{"name": name, "color": color, "count": cnt})
			}
		}
	}
	if allTags == nil {
		allTags = make([]fiber.Map, 0)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"stages":  stages,
		"unassigned": fiber.Map{
			"total_count":  unassignedCount,
			"participants": unassignedParts,
			"has_more":     len(unassignedParts) < unassignedCount,
		},
		"all_tags": allTags,
	})
}

// handleGetEventParticipantsByStage returns paginated participants for a single stage (infinite scroll)
func (s *Server) handleGetEventParticipantsByStage(c *fiber.Ctx) error {
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	stageIDParam := c.Params("stageId")

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	search := strings.TrimSpace(c.Query("search"))
	tagNamesRaw := c.Query("tag_names")
	tagMode := strings.ToUpper(c.Query("tag_mode", "OR"))
	excludeTagNamesRaw := c.Query("exclude_tag_names")
	tagFormulaRaw := c.Query("tag_formula")
	var hasPhone *bool
	if hp := c.Query("has_phone"); hp == "true" {
		t := true
		hasPhone = &t
	}

	// Build WHERE
	args := []interface{}{eventID}
	argIdx := 2
	whereClauses := []string{"p.event_id = $1"}

	isUnassigned := stageIDParam == "unassigned"
	if isUnassigned {
		whereClauses = append(whereClauses, "p.stage_id IS NULL")
	} else {
		if stageUUID, err := uuid.Parse(stageIDParam); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("p.stage_id = $%d", argIdx))
			args = append(args, stageUUID)
			argIdx++
		} else {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid stage_id"})
		}
	}

	if search != "" {
		searchPattern := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(p.name,'')) LIKE $%d OR LOWER(COALESCE(p.phone,'')) LIKE $%d OR LOWER(COALESCE(p.email,'')) LIKE $%d OR LOWER(COALESCE(p.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}

	var tagNames []string
	if tagNamesRaw != "" {
		tagNames = strings.Split(tagNamesRaw, ",")
	}
	var excludeTagNames []string
	if excludeTagNamesRaw != "" {
		excludeTagNames = strings.Split(excludeTagNamesRaw, ",")
	}

	if tagFormulaRaw != "" {
		ast, parseErr := formula.Parse(tagFormulaRaw)
		if parseErr == nil && ast != nil {
			innerSQL, innerArgs, buildErr := formula.BuildSQLForParticipants(ast, eventID)
			if buildErr == nil && innerSQL != "" {
				remappedSQL := formula.RemapSQLParams(innerSQL, len(innerArgs), argIdx)
				whereClauses = append(whereClauses, fmt.Sprintf("p.id IN (%s)", remappedSQL))
				args = append(args, innerArgs...)
				argIdx += len(innerArgs)
			}
		}
	} else if len(tagNames) > 0 || len(excludeTagNames) > 0 {
		if len(tagNames) > 0 {
			if tagMode == "AND" {
				whereClauses = append(whereClauses, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d) GROUP BY p2.id HAVING COUNT(DISTINCT t.name) = $%d)",
					argIdx, argIdx+1,
				))
				args = append(args, tagNames, len(tagNames))
				argIdx += 2
			} else {
				whereClauses = append(whereClauses, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
					argIdx,
				))
				args = append(args, tagNames)
				argIdx++
			}
		}
		if len(excludeTagNames) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf(
				"p.id NOT IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
				argIdx,
			))
			args = append(args, excludeTagNames)
			argIdx++
		}
	}

	if hasPhone != nil && *hasPhone {
		whereClauses = append(whereClauses, "p.phone IS NOT NULL AND p.phone != ''")
	}

	addDateFilter(c, "p", participantDateFields, &whereClauses, &args, &argIdx)

	whereSQL := strings.Join(whereClauses, " AND ")

	// Query with LIMIT+1 OFFSET
	q := fmt.Sprintf(`
		SELECT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id,
		       p.name, p.last_name, p.short_name, p.phone, p.email, p.age,
		       p.company, p.dni, p.birth_date, p.address, p.distrito, p.ocupacion,
		       p.status, p.notes, p.next_action, p.next_action_date,
		       p.invited_at, p.confirmed_at, p.attended_at,
		       p.created_at, p.updated_at,
		       COALESCE(s.name, '') AS stage_name, COALESCE(s.color, '') AS stage_color,
		       l.pipeline_id AS lead_pipeline_id, l.stage_id AS lead_stage_id, lps.name AS lead_stage_name, lps.color AS lead_stage_color,
		       COALESCE(l.is_archived, false) AS is_archived, COALESCE(l.is_blocked, false) AS is_blocked
		FROM event_participants p
		LEFT JOIN event_pipeline_stages s ON s.id = p.stage_id
		LEFT JOIN leads l ON l.id = p.lead_id
		LEFT JOIN pipeline_stages lps ON lps.id = l.stage_id
		WHERE %s
		ORDER BY p.created_at DESC
		LIMIT %d OFFSET %d
	`, whereSQL, limit+1, offset)

	rows, err := s.repos.DB().Query(c.Context(), q, args...)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	participants := make([]*domain.EventParticipant, 0)
	for rows.Next() {
		p := &domain.EventParticipant{}
		if err := rows.Scan(
			&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID,
			&p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age,
			&p.Company, &p.DNI, &p.BirthDate, &p.Address, &p.Distrito, &p.Ocupacion,
			&p.Status, &p.Notes, &p.NextAction, &p.NextActionDate,
			&p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt,
			&p.CreatedAt, &p.UpdatedAt,
			&p.StageName, &p.StageColor,
			&p.LeadPipelineID, &p.LeadStageID, &p.LeadStageName, &p.LeadStageColor,
			&p.IsArchived, &p.IsBlocked,
		); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		participants = append(participants, p)
	}

	hasMore := len(participants) > limit
	if hasMore {
		participants = participants[:limit]
	}

	// Load tags for returned participants (via contact_tags, with lead fallback)
	if len(participants) > 0 {
		partIDs := make([]uuid.UUID, 0, len(participants))
		for _, p := range participants {
			partIDs = append(partIDs, p.ID)
		}
		tagRows, err := s.repos.DB().Query(c.Context(), `
			SELECT p.id, t.id, t.account_id, t.name, t.color
			FROM event_participants p
			LEFT JOIN leads l ON l.id = p.lead_id
			JOIN contact_tags ct ON ct.contact_id = COALESCE(p.contact_id, l.contact_id)
			JOIN tags t ON t.id = ct.tag_id
			WHERE p.id = ANY($1)
			ORDER BY t.name
		`, partIDs)
		if err == nil {
			defer tagRows.Close()
			partTagMap := make(map[uuid.UUID][]*domain.Tag)
			for tagRows.Next() {
				var partID uuid.UUID
				t := &domain.Tag{}
				if err := tagRows.Scan(&partID, &t.ID, &t.AccountID, &t.Name, &t.Color); err == nil {
					partTagMap[partID] = append(partTagMap[partID], t)
				}
			}
			for _, p := range participants {
				p.Tags = partTagMap[p.ID]
			}
		}
	}

	return c.JSON(fiber.Map{
		"success":      true,
		"participants": participants,
		"has_more":     hasMore,
	})
}

// handleBatchParticipantObservations returns observations for multiple participants
// It searches interactions by participant_id, contact_id, and lead_id
func (s *Server) handleBatchParticipantObservations(c *fiber.Ctx) error {
	var req struct {
		ParticipantIDs []string `json:"participant_ids"`
		Limit          int      `json:"limit"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if len(req.ParticipantIDs) == 0 {
		return c.JSON(fiber.Map{"success": true, "observations": map[string]interface{}{}})
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Limit > 20 {
		req.Limit = 20
	}

	var partUUIDs []uuid.UUID
	for _, id := range req.ParticipantIDs {
		if uid, err := uuid.Parse(id); err == nil {
			partUUIDs = append(partUUIDs, uid)
		}
	}
	if len(partUUIDs) == 0 {
		return c.JSON(fiber.Map{"success": true, "observations": map[string]interface{}{}})
	}

	// Get participant_id → contact_id and lead_id mappings
	mapRows, err := s.repos.DB().Query(c.Context(), `
		SELECT id, contact_id, lead_id FROM event_participants WHERE id = ANY($1)
	`, partUUIDs)
	if err != nil {
		log.Printf("[API] Error querying participant mapping: %v", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer mapRows.Close()

	type partMapping struct {
		contactID *uuid.UUID
		leadID    *uuid.UUID
	}
	partMap := make(map[uuid.UUID]partMapping) // participant_id → mapping
	contactToPart := make(map[uuid.UUID]uuid.UUID)
	leadToPart := make(map[uuid.UUID]uuid.UUID)
	contactUUIDs := make([]uuid.UUID, 0)
	leadUUIDs := make([]uuid.UUID, 0)

	for mapRows.Next() {
		var partID uuid.UUID
		var contactID, leadID *uuid.UUID
		if err := mapRows.Scan(&partID, &contactID, &leadID); err == nil {
			partMap[partID] = partMapping{contactID: contactID, leadID: leadID}
			if contactID != nil {
				contactToPart[*contactID] = partID
				contactUUIDs = append(contactUUIDs, *contactID)
			}
			if leadID != nil {
				leadToPart[*leadID] = partID
				leadUUIDs = append(leadUUIDs, *leadID)
			}
		}
	}

	// Query interactions matching participant_id, contact_id, or lead_id using UNION
	// Priority: direct participant_id match first, then contact_id, then lead_id
	rows, err := s.repos.DB().Query(c.Context(), `
		SELECT participant_id, contact_id, lead_id, id, type, direction, outcome, notes, created_by_name, created_at
		FROM (
			SELECT i.participant_id, i.contact_id, i.lead_id, i.id, i.type, i.direction, i.outcome, i.notes,
			       u.display_name as created_by_name, i.created_at,
			       ROW_NUMBER() OVER (
			         PARTITION BY COALESCE(i.participant_id::text, i.contact_id::text, i.lead_id::text)
			         ORDER BY i.created_at DESC
			       ) as rn
			FROM interactions i
			LEFT JOIN users u ON i.created_by = u.id
			WHERE i.participant_id = ANY($1)
			   OR i.contact_id = ANY($2)
			   OR i.lead_id = ANY($3)
		) sub
		WHERE rn <= $4
		ORDER BY created_at DESC
	`, partUUIDs, contactUUIDs, leadUUIDs, req.Limit)
	if err != nil {
		log.Printf("[API] Error querying batch participant observations: %v", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	// Deduplicate: track which interactions we've already added per participant
	type obsKey struct {
		partID string
		obsID  string
	}
	seen := make(map[obsKey]bool)
	result := make(map[string][]*domain.Interaction)

	for rows.Next() {
		var participantID, contactID, leadID *uuid.UUID
		i := &domain.Interaction{}
		if err := rows.Scan(&participantID, &contactID, &leadID, &i.ID, &i.Type, &i.Direction, &i.Outcome, &i.Notes, &i.CreatedByName, &i.CreatedAt); err != nil {
			log.Printf("[API] Error scanning batch participant observation row: %v", err)
			continue
		}

		// Resolve which participant this interaction belongs to
		var targetPartID string
		if participantID != nil {
			// Direct participant_id match
			pid := *participantID
			if _, ok := partMap[pid]; ok {
				targetPartID = pid.String()
			}
		}
		if targetPartID == "" && contactID != nil {
			if pid, ok := contactToPart[*contactID]; ok {
				targetPartID = pid.String()
			}
		}
		if targetPartID == "" && leadID != nil {
			if pid, ok := leadToPart[*leadID]; ok {
				targetPartID = pid.String()
			}
		}

		if targetPartID == "" {
			continue
		}

		key := obsKey{partID: targetPartID, obsID: i.ID.String()}
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(result[targetPartID]) < req.Limit {
			result[targetPartID] = append(result[targetPartID], i)
		}
	}

	return c.JSON(fiber.Map{"success": true, "observations": result})
}

func (s *Server) handleAddEventParticipant(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	if ev, _ := s.services.Event.GetByID(c.Context(), eventID); ev == nil || ev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	var req struct {
		ContactID *string `json:"contact_id"`
		LeadID    *string `json:"lead_id"`
		Name      string  `json:"name"`
		LastName  *string `json:"last_name"`
		ShortName *string `json:"short_name"`
		Phone     *string `json:"phone"`
		Email     *string `json:"email"`
		Age       *int    `json:"age"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	p := &domain.EventParticipant{
		EventID:   eventID,
		Name:      req.Name,
		LastName:  req.LastName,
		ShortName: req.ShortName,
		Phone:     req.Phone,
		Email:     req.Email,
		Age:       req.Age,
	}
	if req.ContactID != nil {
		if cid, err := uuid.Parse(*req.ContactID); err == nil {
			p.ContactID = &cid
		}
	}
	if req.LeadID != nil {
		if lid, err := uuid.Parse(*req.LeadID); err == nil {
			p.LeadID = &lid
			// If no contact_id provided, look up the lead's contact_id
			if p.ContactID == nil {
				if lead, err := s.services.Lead.GetByID(c.Context(), lid); err == nil && lead != nil && lead.ContactID != nil {
					p.ContactID = lead.ContactID
				}
			}
		}
	}
	if err := s.services.Event.AddParticipant(c.Context(), p); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "idx_event_participants_unique_phone") {
			return c.Status(409).JSON(fiber.Map{"success": false, "error": "Ya existe un participante con ese teléfono en este evento"})
		}
		if strings.Contains(errMsg, "idx_event_participants_unique_email") {
			return c.Status(409).JSON(fiber.Map{"success": false, "error": "Ya existe un participante con ese email en este evento"})
		}
		if strings.Contains(errMsg, "idx_event_participants_unique_contact") {
			return c.Status(409).JSON(fiber.Map{"success": false, "error": "Este contacto ya está registrado en este evento"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "error": errMsg})
	}
	if ev, err := s.services.Event.GetByID(c.Context(), eventID); err == nil && ev != nil && s.hub != nil {
		s.hub.BroadcastToAccount(ev.AccountID, ws.EventEventParticipantUpdate, map[string]interface{}{"event_id": eventID.String(), "action": "added"})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "participant": p})
}

func (s *Server) handleBulkAddEventParticipants(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	if ev, _ := s.services.Event.GetByID(c.Context(), eventID); ev == nil || ev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	var req struct {
		Participants []struct {
			ContactID *string `json:"contact_id"`
			LeadID    *string `json:"lead_id"`
			Name      string  `json:"name"`
			LastName  *string `json:"last_name"`
			ShortName *string `json:"short_name"`
			Phone     *string `json:"phone"`
			Email     *string `json:"email"`
			Age       *int    `json:"age"`
		} `json:"participants"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	var participants []*domain.EventParticipant
	for _, r := range req.Participants {
		p := &domain.EventParticipant{
			Name:      r.Name,
			LastName:  r.LastName,
			ShortName: r.ShortName,
			Phone:     r.Phone,
			Email:     r.Email,
			Age:       r.Age,
		}
		if r.ContactID != nil {
			if cid, err := uuid.Parse(*r.ContactID); err == nil {
				p.ContactID = &cid
			}
		}
		if r.LeadID != nil {
			if lid, err := uuid.Parse(*r.LeadID); err == nil {
				p.LeadID = &lid
				if p.ContactID == nil {
					if lead, err := s.services.Lead.GetByID(c.Context(), lid); err == nil && lead != nil && lead.ContactID != nil {
						p.ContactID = lead.ContactID
					}
				}
			}
		}
		participants = append(participants, p)
	}
	if err := s.services.Event.BulkAddParticipants(c.Context(), eventID, participants); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "idx_event_participants_unique") {
			return c.Status(409).JSON(fiber.Map{"success": false, "error": "Uno o más participantes ya están registrados en este evento"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "error": errMsg})
	}
	return c.JSON(fiber.Map{"success": true, "count": len(participants)})
}

func (s *Server) handleUpdateEventParticipant(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid participant ID"})
	}
	p, err := s.services.Event.GetParticipant(c.Context(), pid)
	if err != nil || p == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
	}
	// Verify participant's event belongs to the account
	if ev, _ := s.services.Event.GetByID(c.Context(), p.EventID); ev == nil || ev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
	}
	var req struct {
		Name           *string    `json:"name"`
		LastName       *string    `json:"last_name"`
		ShortName      *string    `json:"short_name"`
		Phone          *string    `json:"phone"`
		Email          *string    `json:"email"`
		Age            *int       `json:"age"`
		Company        *string    `json:"company"`
		DNI            *string    `json:"dni"`
		BirthDate      *string    `json:"birth_date"`
		Address        *string    `json:"address"`
		Distrito       *string    `json:"distrito"`
		Ocupacion      *string    `json:"ocupacion"`
		Notes          *string    `json:"notes"`
		NextAction     *string    `json:"next_action"`
		NextActionDate *time.Time `json:"next_action_date"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.LastName != nil {
		if *req.LastName == "" {
			p.LastName = nil
		} else {
			p.LastName = req.LastName
		}
	}
	if req.ShortName != nil {
		if *req.ShortName == "" {
			p.ShortName = nil
		} else {
			p.ShortName = req.ShortName
		}
	}
	if req.Phone != nil {
		if *req.Phone == "" {
			p.Phone = nil
		} else {
			p.Phone = req.Phone
		}
	}
	if req.Email != nil {
		if *req.Email == "" {
			p.Email = nil
		} else {
			p.Email = req.Email
		}
	}
	if req.Age != nil {
		if *req.Age == 0 {
			p.Age = nil
		} else {
			p.Age = req.Age
		}
	}
	if req.Company != nil {
		if *req.Company == "" {
			p.Company = nil
		} else {
			p.Company = req.Company
		}
	}
	if req.DNI != nil {
		if *req.DNI == "" {
			p.DNI = nil
		} else {
			p.DNI = req.DNI
		}
	}
	if req.BirthDate != nil {
		if *req.BirthDate == "" {
			p.BirthDate = nil
		} else if t, err := time.Parse("2006-01-02", *req.BirthDate); err == nil {
			p.BirthDate = &t
		}
	}
	if req.Address != nil {
		if *req.Address == "" {
			p.Address = nil
		} else {
			p.Address = req.Address
		}
	}
	if req.Distrito != nil {
		if *req.Distrito == "" {
			p.Distrito = nil
		} else {
			p.Distrito = req.Distrito
		}
	}
	if req.Ocupacion != nil {
		if *req.Ocupacion == "" {
			p.Ocupacion = nil
		} else {
			p.Ocupacion = req.Ocupacion
		}
	}
	if req.Notes != nil {
		if *req.Notes == "" {
			p.Notes = nil
		} else {
			p.Notes = req.Notes
		}
	}
	if req.NextAction != nil {
		if *req.NextAction == "" {
			p.NextAction = nil
		} else {
			p.NextAction = req.NextAction
		}
	}
	if req.NextActionDate != nil {
		p.NextActionDate = req.NextActionDate
	}
	if err := s.services.Event.UpdateParticipant(c.Context(), p); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if ev, err := s.services.Event.GetByID(c.Context(), p.EventID); err == nil && ev != nil && s.hub != nil {
		s.hub.BroadcastToAccount(ev.AccountID, ws.EventEventParticipantUpdate, map[string]interface{}{"event_id": p.EventID.String(), "action": "updated"})
	}

	// If participant has no contact_id, try to find and link by phone
	if p.ContactID == nil && p.Phone != nil && *p.Phone != "" {
		// Get account_id from the event
		event, _ := s.services.Event.GetByID(c.Context(), p.EventID)
		if event != nil {
			contact, _ := s.repos.Contact.GetByPhone(c.Context(), event.AccountID, *p.Phone)
			if contact != nil {
				p.ContactID = &contact.ID
				// Update the participant's contact_id in DB
				s.repos.Participant.LinkContact(c.Context(), p.ID, contact.ID)
			}
		}
	}

	// Sync shared fields back to the linked contact
	if p.ContactID != nil {
		_ = s.services.Event.SyncParticipantToContact(c.Context(), p)

		// Auto-sync to Google Contacts if linked contact has google_sync enabled
		if s.googleClient != nil {
			go func() {
				contact, err := s.repos.Contact.GetByID(context.Background(), *p.ContactID)
				if err == nil && contact != nil && contact.GoogleSync {
					log.Printf("[GOOGLE] Auto-sync triggered from handleUpdateEventParticipant for participant %s → contact %s (google_sync=%v)", p.ID, contact.ID, contact.GoogleSync)
					if _, err := s.syncContactToGoogle(context.Background(), contact.AccountID, contact.ID); err != nil {
						log.Printf("[GOOGLE] Auto-sync from participant %s failed: %v", p.ID, err)
					}
				}
			}()
		}
	}

	return c.JSON(fiber.Map{"success": true, "participant": p})
}

func (s *Server) handleUpdateEventParticipantStatus(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid participant ID"})
	}
	// Verify participant's event belongs to the account
	if p, _ := s.services.Event.GetParticipant(c.Context(), pid); p != nil {
		if ev, _ := s.services.Event.GetByID(c.Context(), p.EventID); ev == nil || ev.AccountID != accountID {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
		}
	} else {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if err := s.services.Event.UpdateParticipantStatus(c.Context(), pid, req.Status); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleBulkUpdateEventParticipantStatus(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	// Verify event belongs to account
	ev, err := s.services.Event.GetByID(c.Context(), eventID)
	if err != nil || ev == nil || ev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	var req struct {
		ParticipantIDs []string `json:"participant_ids"`
		Status         string   `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil || len(req.ParticipantIDs) == 0 || req.Status == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	ids := make([]uuid.UUID, 0, len(req.ParticipantIDs))
	for _, s := range req.ParticipantIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid participant ID: " + s})
		}
		ids = append(ids, id)
	}
	if err := s.services.Event.BulkUpdateParticipantStatus(c.Context(), ids, req.Status); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "updated": len(ids)})
}

func (s *Server) handleDeleteEventParticipant(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid participant ID"})
	}
	// Get participant's event_id before deleting and verify ownership
	delPart, _ := s.services.Event.GetParticipant(c.Context(), pid)
	if delPart != nil {
		if ev, _ := s.services.Event.GetByID(c.Context(), delPart.EventID); ev == nil || ev.AccountID != accountID {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
		}
	}
	if err := s.services.Event.DeleteParticipant(c.Context(), pid); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if delPart != nil {
		if ev, err := s.services.Event.GetByID(c.Context(), delPart.EventID); err == nil && ev != nil && s.hub != nil {
			s.hub.BroadcastToAccount(ev.AccountID, ws.EventEventParticipantUpdate, map[string]interface{}{"event_id": delPart.EventID.String(), "action": "deleted"})
		}
	}
	return c.JSON(fiber.Map{"success": true})
}

// handleCheckTagImpact checks if adding/removing a tag would cause a participant to leave/join the event
func (s *Server) handleCheckTagImpact(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid participant ID"})
	}
	var req struct {
		TagID  string `json:"tag_id"`
		Action string `json:"action"` // "assign" or "remove"
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	tagID, err := uuid.Parse(req.TagID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid tag ID"})
	}

	// Get the event to check its formula
	event, err := s.services.Event.GetByID(c.Context(), eventID)
	if err != nil || event == nil || event.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	// If no formula, tag changes can't affect membership
	if event.TagFormula == "" {
		return c.JSON(fiber.Map{"success": true, "would_remove": false, "would_add": false})
	}

	// Get the participant to find their lead_id
	participant, err := s.services.Event.GetParticipant(c.Context(), pid)
	if err != nil || participant == nil || participant.LeadID == nil {
		return c.JSON(fiber.Map{"success": true, "would_remove": false, "would_add": false})
	}

	// Get current lead tags
	currentTags, err := s.services.Tag.GetByEntity(c.Context(), "lead", *participant.LeadID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Get the tag name being changed
	tag, err := s.repos.Tag.GetByID(c.Context(), tagID)
	if err != nil || tag == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Tag not found"})
	}

	// Build current tag names (lowercased)
	currentTagNames := make([]string, len(currentTags))
	for i, t := range currentTags {
		currentTagNames[i] = strings.ToLower(t.Name)
	}

	// Simulate tag change
	newTagNames := make([]string, 0, len(currentTagNames)+1)
	tagNameLower := strings.ToLower(tag.Name)
	if req.Action == "assign" {
		newTagNames = append(newTagNames, currentTagNames...)
		// Check if already present
		found := false
		for _, tn := range currentTagNames {
			if tn == tagNameLower {
				found = true
				break
			}
		}
		if !found {
			newTagNames = append(newTagNames, tagNameLower)
		}
	} else {
		// Remove
		for _, tn := range currentTagNames {
			if tn != tagNameLower {
				newTagNames = append(newTagNames, tn)
			}
		}
	}

	// Parse and evaluate formula
	ast, parseErr := formula.Parse(event.TagFormula)
	if parseErr != nil {
		return c.JSON(fiber.Map{"success": true, "would_remove": false, "would_add": false})
	}

	matchesBefore := formula.Evaluate(ast, currentTagNames)
	matchesAfter := formula.Evaluate(ast, newTagNames)

	return c.JSON(fiber.Map{
		"success":      true,
		"would_remove": matchesBefore && !matchesAfter,
		"would_add":    !matchesBefore && matchesAfter,
	})
}

func (s *Server) handleCreateCampaignFromEvent(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}

	var req struct {
		Name            string                 `json:"name"`
		DeviceID        string                 `json:"device_id"`
		MessageTemplate string                 `json:"message_template"`
		MediaURL        *string                `json:"media_url"`
		MediaType       *string                `json:"media_type"`
		ScheduledAt     *time.Time             `json:"scheduled_at"`
		Settings        map[string]interface{} `json:"settings"`
		Attachments     []struct {
			MediaURL  string `json:"media_url"`
			MediaType string `json:"media_type"`
			Caption   string `json:"caption"`
			FileName  string `json:"file_name"`
			FileSize  int64  `json:"file_size"`
			Position  int    `json:"position"`
		} `json:"attachments"`
		// Filters to select participants
		StageIDs        string   `json:"stage_ids"`
		TagNames        []string `json:"tag_names"`
		TagMode         string   `json:"tag_mode"`
		ExcludeTagNames []string `json:"exclude_tag_names"`
		TagFormula      string   `json:"tag_formula"`
		HasPhone        *bool    `json:"has_phone"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" || req.DeviceID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "name and device_id are required"})
	}
	if req.MessageTemplate == "" && len(req.Attachments) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "message_template or attachments required"})
	}

	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device ID"})
	}

	// Build WHERE clause for participant filtering (same logic as handleGetEventParticipants)
	pArgs := []interface{}{eventID}
	pArgIdx := 2
	pWhere := []string{"p.event_id = $1", "p.phone IS NOT NULL", "p.phone != ''"}

	if req.TagFormula != "" {
		ast, parseErr := formula.Parse(req.TagFormula)
		if parseErr == nil && ast != nil {
			innerSQL, innerArgs, buildErr := formula.BuildSQLForParticipants(ast, eventID)
			if buildErr == nil && innerSQL != "" {
				remappedSQL := formula.RemapSQLParams(innerSQL, len(innerArgs), pArgIdx)
				pWhere = append(pWhere, fmt.Sprintf("p.id IN (%s)", remappedSQL))
				pArgs = append(pArgs, innerArgs...)
				pArgIdx += len(innerArgs)
			}
		}
	} else {
		tagMode := strings.ToUpper(req.TagMode)
		if tagMode == "" {
			tagMode = "OR"
		}
		if len(req.TagNames) > 0 {
			if tagMode == "AND" {
				pWhere = append(pWhere, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d) GROUP BY p2.id HAVING COUNT(DISTINCT t.name) = $%d)",
					pArgIdx, pArgIdx+1,
				))
				pArgs = append(pArgs, req.TagNames, len(req.TagNames))
				pArgIdx += 2
			} else {
				pWhere = append(pWhere, fmt.Sprintf(
					"p.id IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
					pArgIdx,
				))
				pArgs = append(pArgs, req.TagNames)
				pArgIdx++
			}
		}
		if len(req.ExcludeTagNames) > 0 {
			pWhere = append(pWhere, fmt.Sprintf(
				"p.id NOT IN (SELECT p2.id FROM event_participants p2 LEFT JOIN leads ll ON ll.id = p2.lead_id JOIN contact_tags ct ON ct.contact_id = COALESCE(p2.contact_id, ll.contact_id) JOIN tags t ON t.id = ct.tag_id WHERE p2.event_id = $1 AND t.name = ANY($%d))",
				pArgIdx,
			))
			pArgs = append(pArgs, req.ExcludeTagNames)
			pArgIdx++
		}
	}

	if req.StageIDs != "" {
		var validStageIDs []uuid.UUID
		for _, sid := range strings.Split(req.StageIDs, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageIDs = append(validStageIDs, id)
			}
		}
		if len(validStageIDs) > 0 {
			pWhere = append(pWhere, fmt.Sprintf("p.stage_id = ANY($%d)", pArgIdx))
			pArgs = append(pArgs, validStageIDs)
			pArgIdx++
		}
	}
	_ = pArgIdx // suppress unused

	// Query participants
	whereSQL := strings.Join(pWhere, " AND ")
	dataQ := fmt.Sprintf(`
		SELECT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id,
		       p.name, p.last_name, p.short_name, p.phone, p.email, p.age,
		       p.status, p.notes, p.next_action, p.next_action_date,
		       p.invited_at, p.confirmed_at, p.attended_at,
		       p.created_at, p.updated_at,
		       eps.name AS stage_name, eps.color AS stage_color
		FROM event_participants p
		LEFT JOIN event_pipeline_stages eps ON eps.id = p.stage_id
		WHERE %s
		ORDER BY p.name ASC
	`, whereSQL)

	rows, err := s.repos.DB().Query(c.Context(), dataQ, pArgs...)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	var participants []*domain.EventParticipant
	for rows.Next() {
		p := &domain.EventParticipant{}
		if err := rows.Scan(
			&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID,
			&p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age,
			&p.Status, &p.Notes, &p.NextAction, &p.NextActionDate,
			&p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt,
			&p.CreatedAt, &p.UpdatedAt,
			&p.StageName, &p.StageColor,
		); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		participants = append(participants, p)
	}

	if len(participants) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No hay participantes con teléfono que coincidan con los filtros"})
	}

	// Create campaign
	source := "event"
	campaign := &domain.Campaign{
		AccountID:       accountID,
		DeviceID:        deviceID,
		Name:            req.Name,
		MessageTemplate: req.MessageTemplate,
		MediaURL:        req.MediaURL,
		MediaType:       req.MediaType,
		ScheduledAt:     req.ScheduledAt,
		Settings:        req.Settings,
		EventID:         &eventID,
		Source:          &source,
	}
	// Set created_by from authenticated user
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		campaign.CreatedBy = &userID
	}
	if err := s.services.Campaign.Create(c.Context(), campaign); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Save attachments if provided
	if len(req.Attachments) > 0 {
		var attachments []*domain.CampaignAttachment
		for _, a := range req.Attachments {
			attachments = append(attachments, &domain.CampaignAttachment{
				MediaURL:  a.MediaURL,
				MediaType: a.MediaType,
				Caption:   a.Caption,
				FileName:  a.FileName,
				FileSize:  a.FileSize,
				Position:  a.Position,
			})
		}
		if err := s.repos.CampaignAttachment.CreateBatch(c.Context(), campaign.ID, attachments); err != nil {
			log.Printf("[Campaign] Failed to save event campaign attachments: %v", err)
		}
		campaign.Attachments = attachments
	}

	// Add participants as recipients
	var recipients []*domain.CampaignRecipient
	for _, p := range participants {
		if p.Phone == nil || *p.Phone == "" {
			continue
		}
		phone := strings.TrimPrefix(*p.Phone, "+")
		jid := phone + "@s.whatsapp.net"
		fullName := p.Name
		if p.LastName != nil && *p.LastName != "" {
			fullName += " " + *p.LastName
		}
		rec := &domain.CampaignRecipient{
			CampaignID: campaign.ID,
			ContactID:  p.ContactID,
			JID:        jid,
			Name:       &fullName,
			Phone:      p.Phone,
		}
		// Store participant's short_name in metadata so {{nombre_corto}} resolves
		if p.ShortName != nil && *p.ShortName != "" {
			rec.Metadata = map[string]interface{}{"nombre_corto": *p.ShortName}
		}
		recipients = append(recipients, rec)
	}

	if len(recipients) > 0 {
		if err := s.services.Campaign.AddRecipients(c.Context(), recipients); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}

	return c.Status(201).JSON(fiber.Map{
		"success":          true,
		"campaign":         campaign,
		"recipients_count": len(recipients),
	})
}

func (s *Server) handleGetUpcomingActions(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	limit := c.QueryInt("limit", 20)
	actions, err := s.services.Event.GetUpcomingActions(c.Context(), accountID, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if actions == nil {
		actions = make([]*domain.EventParticipant, 0)
	}
	return c.JSON(fiber.Map{"success": true, "actions": actions})
}

// --- Event Folder Handlers ---

func (s *Server) handleGetEventFolders(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	folders, err := s.services.Event.GetFolders(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if folders == nil {
		folders = make([]*domain.EventFolder, 0)
	}
	return c.JSON(fiber.Map{"success": true, "folders": folders})
}

func (s *Server) handleCreateEventFolder(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		ParentID *string `json:"parent_id"`
		Name     string  `json:"name"`
		Color    string  `json:"color"`
		Icon     string  `json:"icon"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	folder := &domain.EventFolder{
		AccountID: accountID,
		Name:      req.Name,
		Color:     req.Color,
		Icon:      req.Icon,
	}
	if req.ParentID != nil && *req.ParentID != "" {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid parent folder ID"})
		}
		folder.ParentID = &pid
	}
	if err := s.services.Event.CreateFolder(c.Context(), folder); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "folder": folder})
}

func (s *Server) handleUpdateEventFolder(c *fiber.Ctx) error {
	fid, err := uuid.Parse(c.Params("fid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid folder ID"})
	}
	folder, err := s.services.Event.GetFolderByID(c.Context(), fid)
	if err != nil || folder == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Folder not found"})
	}
	var req struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
		Icon  *string `json:"icon"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		folder.Name = *req.Name
	}
	if req.Color != nil {
		folder.Color = *req.Color
	}
	if req.Icon != nil {
		folder.Icon = *req.Icon
	}
	if err := s.services.Event.UpdateFolder(c.Context(), folder); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "folder": folder})
}

func (s *Server) handleDeleteEventFolder(c *fiber.Ctx) error {
	fid, err := uuid.Parse(c.Params("fid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid folder ID"})
	}
	if err := s.services.Event.DeleteFolder(c.Context(), fid); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleMoveEventToFolder(c *fiber.Ctx) error {
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	var req struct {
		FolderID *string `json:"folder_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	var folderID *uuid.UUID
	if req.FolderID != nil && *req.FolderID != "" {
		fid, err := uuid.Parse(*req.FolderID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid folder ID"})
		}
		folderID = &fid
	}
	if err := s.services.Event.MoveEventToFolder(c.Context(), eventID, folderID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// --- Event Pipeline Handlers ---

func (s *Server) handleGetEventPipelines(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pipelines, err := s.services.Event.GetPipelines(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if pipelines == nil {
		pipelines = make([]*domain.EventPipeline, 0)
	}
	return c.JSON(fiber.Map{"success": true, "pipelines": pipelines})
}

func (s *Server) handleCreateEventPipeline(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Stages      []struct {
			Name     string `json:"name"`
			Color    string `json:"color"`
			Position int    `json:"position"`
		} `json:"stages"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	pipeline := &domain.EventPipeline{
		AccountID:   accountID,
		Name:        req.Name,
		Description: req.Description,
	}
	if err := s.services.Event.CreatePipeline(c.Context(), pipeline); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	// Create stages if provided
	if len(req.Stages) > 0 {
		var stages []*domain.EventPipelineStage
		for _, s := range req.Stages {
			stages = append(stages, &domain.EventPipelineStage{
				PipelineID: pipeline.ID,
				Name:       s.Name,
				Color:      s.Color,
				Position:   s.Position,
			})
		}
		if err := s.services.Event.ReplaceStages(c.Context(), pipeline.ID, stages); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		pipeline.Stages = stages
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "pipeline": pipeline})
}

func (s *Server) handleGetEventPipeline(c *fiber.Ctx) error {
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	pipeline, err := s.services.Event.GetPipeline(c.Context(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Pipeline not found"})
	}
	// Load participant counts per stage
	counts, _, err := s.services.Event.GetParticipantCountsByStage(c.Context(), pid)
	if err == nil && pipeline.Stages != nil {
		for _, stage := range pipeline.Stages {
			if cnt, ok := counts[stage.ID]; ok {
				stage.ParticipantCount = cnt
			}
		}
	}
	return c.JSON(fiber.Map{"success": true, "pipeline": pipeline})
}

func (s *Server) handleUpdateEventPipeline(c *fiber.Ctx) error {
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	pipeline, err := s.services.Event.GetPipeline(c.Context(), pid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Pipeline not found"})
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name != nil {
		pipeline.Name = *req.Name
	}
	if req.Description != nil {
		pipeline.Description = req.Description
	}
	if err := s.services.Event.UpdatePipeline(c.Context(), pipeline); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "pipeline": pipeline})
}

func (s *Server) handleDeleteEventPipeline(c *fiber.Ctx) error {
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	if err := s.services.Event.DeletePipeline(c.Context(), pid); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleReplaceEventPipelineStages(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	if pipeline, _ := s.services.Event.GetPipeline(c.Context(), pid); pipeline == nil || pipeline.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Pipeline not found"})
	}
	var req struct {
		Stages []struct {
			ID       *string `json:"id"`
			Name     string  `json:"name"`
			Color    string  `json:"color"`
			Position int     `json:"position"`
		} `json:"stages"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	var stages []*domain.EventPipelineStage
	for _, s := range req.Stages {
		stage := &domain.EventPipelineStage{
			PipelineID: pid,
			Name:       s.Name,
			Color:      s.Color,
			Position:   s.Position,
		}
		if s.ID != nil {
			if id, err := uuid.Parse(*s.ID); err == nil {
				stage.ID = id
			}
		}
		stages = append(stages, stage)
	}
	if err := s.services.Event.ReplaceStages(c.Context(), pid, stages); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	// Return updated stages
	updatedStages, _ := s.services.Event.GetPipelineStages(c.Context(), pid)
	return c.JSON(fiber.Map{"success": true, "stages": updatedStages})
}

func (s *Server) handleUpdateEventParticipantStage(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	pid, err := uuid.Parse(c.Params("pid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid participant ID"})
	}
	if part, _ := s.services.Event.GetParticipant(c.Context(), pid); part == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
	} else if ev, _ := s.services.Event.GetByID(c.Context(), part.EventID); ev == nil || ev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Participant not found"})
	}
	var req struct {
		StageID string `json:"stage_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	stageID, err := uuid.Parse(req.StageID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid stage ID"})
	}
	if err := s.services.Event.UpdateParticipantStage(c.Context(), pid, stageID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if stagePart, err := s.services.Event.GetParticipant(c.Context(), pid); err == nil && stagePart != nil {
		if ev, err := s.services.Event.GetByID(c.Context(), stagePart.EventID); err == nil && ev != nil && s.hub != nil {
			s.hub.BroadcastToAccount(ev.AccountID, ws.EventEventParticipantUpdate, map[string]interface{}{"event_id": stagePart.EventID.String(), "action": "stage_changed"})
		}
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleBulkUpdateEventParticipantStage(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	eventID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid event ID"})
	}
	if ev, _ := s.services.Event.GetByID(c.Context(), eventID); ev == nil || ev.AccountID != accountID {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Event not found"})
	}
	var req struct {
		ParticipantIDs []string `json:"participant_ids"`
		StageID        string   `json:"stage_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	stageID, err := uuid.Parse(req.StageID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid stage ID"})
	}
	var ids []uuid.UUID
	for _, idStr := range req.ParticipantIDs {
		if id, err := uuid.Parse(idStr); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No valid participant IDs"})
	}
	if err := s.services.Event.BulkUpdateParticipantStage(c.Context(), ids, stageID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if ev, err := s.services.Event.GetByID(c.Context(), eventID); err == nil && ev != nil && s.hub != nil {
		s.hub.BroadcastToAccount(ev.AccountID, ws.EventEventParticipantUpdate, map[string]interface{}{"event_id": eventID.String(), "action": "bulk_stage_changed"})
	}
	return c.JSON(fiber.Map{"success": true, "updated": len(ids)})
}

func (s *Server) handleCreateEventFromLeads(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)

	var req struct {
		// Event data
		Name        string     `json:"name"`
		Description *string    `json:"description"`
		EventDate   *time.Time `json:"event_date"`
		EventEnd    *time.Time `json:"event_end"`
		Location    *string    `json:"location"`
		Color       string     `json:"color"`
		PipelineID  *string    `json:"pipeline_id"`
		// Lead filter criteria
		LeadPipelineID *string  `json:"lead_pipeline_id"`
		Search         string   `json:"search"`
		TagNames       []string `json:"tag_names"`
		StageIDs       []string `json:"stage_ids"`
		DeviceIDs      []string `json:"device_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}

	// Build leads filter query
	args := []interface{}{accountID}
	argIdx := 2
	whereClauses := []string{"l.account_id = $1"}

	if req.LeadPipelineID != nil && *req.LeadPipelineID != "" {
		if *req.LeadPipelineID == "__no_pipeline__" {
			whereClauses = append(whereClauses, "l.pipeline_id IS NULL")
		} else if pid, err := uuid.Parse(*req.LeadPipelineID); err == nil {
			whereClauses = append(whereClauses, fmt.Sprintf("l.pipeline_id = $%d", argIdx))
			args = append(args, pid)
			argIdx++
		}
	}
	if req.Search != "" {
		searchPattern := "%" + strings.ToLower(req.Search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(LOWER(COALESCE(c.name,l.name,'')) LIKE $%d OR LOWER(COALESCE(c.phone,l.phone,'')) LIKE $%d OR LOWER(COALESCE(c.email,l.email,'')) LIKE $%d OR LOWER(COALESCE(c.company,l.company,'')) LIKE $%d OR LOWER(COALESCE(c.last_name,l.last_name,'')) LIKE $%d)",
			argIdx, argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, searchPattern)
		argIdx++
	}
	if len(req.DeviceIDs) > 0 {
		var deviceUUIDs []uuid.UUID
		for _, did := range req.DeviceIDs {
			if id, err := uuid.Parse(did); err == nil {
				deviceUUIDs = append(deviceUUIDs, id)
			}
		}
		if len(deviceUUIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("l.jid IN (SELECT DISTINCT jid FROM chats WHERE device_id = ANY($%d))", argIdx))
			args = append(args, deviceUUIDs)
			argIdx++
		}
	}
	if len(req.TagNames) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf(
			"l.id IN (SELECT l2.id FROM leads l2 JOIN contact_tags ct ON ct.contact_id = l2.contact_id JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d))",
			argIdx,
		))
		args = append(args, req.TagNames)
		argIdx++
	}
	if len(req.StageIDs) > 0 {
		var validStageUUIDs []uuid.UUID
		for _, sid := range req.StageIDs {
			if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
				validStageUUIDs = append(validStageUUIDs, id)
			}
		}
		if len(validStageUUIDs) > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("l.stage_id = ANY($%d)", argIdx))
			args = append(args, validStageUUIDs)
			argIdx++
		}
	}

	whereSQL := strings.Join(whereClauses, " AND ")
	query := fmt.Sprintf(`SELECT l.id, l.contact_id, COALESCE(c.name,l.name,''), COALESCE(c.last_name,l.last_name,''), COALESCE(c.short_name,l.short_name,''), COALESCE(c.phone,l.phone), COALESCE(c.email,l.email), COALESCE(c.age,l.age) FROM leads l LEFT JOIN contacts c ON c.id = l.contact_id WHERE %s ORDER BY l.created_at DESC`, whereSQL)

	rows, err := s.repos.DB().Query(c.Context(), query, args...)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to query leads: " + err.Error()})
	}
	defer rows.Close()

	type leadRow struct {
		ID        uuid.UUID
		ContactID *uuid.UUID
		Name      string
		LastName  string
		ShortName string
		Phone     *string
		Email     *string
		Age       *int
	}
	var leads []leadRow
	for rows.Next() {
		var lr leadRow
		if err := rows.Scan(&lr.ID, &lr.ContactID, &lr.Name, &lr.LastName, &lr.ShortName, &lr.Phone, &lr.Email, &lr.Age); err != nil {
			continue
		}
		leads = append(leads, lr)
	}
	if len(leads) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No leads match the given filters"})
	}

	// Create the event
	event := &domain.Event{
		AccountID:   accountID,
		Name:        req.Name,
		Description: req.Description,
		EventDate:   req.EventDate,
		EventEnd:    req.EventEnd,
		Location:    req.Location,
		Color:       req.Color,
		Status:      "active",
		CreatedBy:   &userID,
	}
	if req.PipelineID != nil {
		if pid, err := uuid.Parse(*req.PipelineID); err == nil {
			event.PipelineID = &pid
		}
	}
	if event.PipelineID == nil {
		defPipeline, err := s.repos.EventPipeline.EnsureDefaultByAccountID(c.Context(), accountID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to ensure event pipeline: " + err.Error()})
		}
		if defPipeline != nil {
			event.PipelineID = &defPipeline.ID
		}
	}
	if err := s.services.Event.Create(c.Context(), event); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to create event: " + err.Error()})
	}

	// Get the first stage of the event pipeline to assign participants
	var firstStageID *uuid.UUID
	if event.PipelineID != nil {
		stages, err := s.services.Event.GetPipelineStages(c.Context(), *event.PipelineID)
		if err == nil && len(stages) > 0 {
			firstStageID = &stages[0].ID
		}
	}

	// Create participants from leads
	added := 0
	for _, lr := range leads {
		p := &domain.EventParticipant{
			EventID: event.ID,
			Name:    lr.Name,
			Phone:   lr.Phone,
			Email:   lr.Email,
			Age:     lr.Age,
			LeadID:  &lr.ID,
			StageID: firstStageID,
		}
		if lr.LastName != "" {
			p.LastName = &lr.LastName
		}
		if lr.ShortName != "" {
			p.ShortName = &lr.ShortName
		}
		if lr.ContactID != nil {
			p.ContactID = lr.ContactID
		}
		if err := s.services.Event.AddParticipant(c.Context(), p); err != nil {
			log.Printf("[EVENT] Failed to add lead %s as participant: %v", lr.ID, err)
			continue
		}
		added++
	}

	return c.Status(201).JSON(fiber.Map{
		"success":            true,
		"event":              event,
		"leads_found":        len(leads),
		"participants_added": added,
	})
}

// --- Interaction Handlers ---

func (s *Server) handleLogInteraction(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	userID := c.Locals("user_id").(uuid.UUID)
	var req struct {
		ContactID      *string    `json:"contact_id"`
		LeadID         *string    `json:"lead_id"`
		EventID        *string    `json:"event_id"`
		ParticipantID  *string    `json:"participant_id"`
		Type           string     `json:"type"`
		Direction      *string    `json:"direction"`
		Outcome        *string    `json:"outcome"`
		Notes          *string    `json:"notes"`
		NextAction     *string    `json:"next_action"`
		NextActionDate *time.Time `json:"next_action_date"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Type == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Type is required"})
	}
	interaction := &domain.Interaction{
		AccountID:      accountID,
		Type:           req.Type,
		Direction:      req.Direction,
		Outcome:        req.Outcome,
		Notes:          req.Notes,
		NextAction:     req.NextAction,
		NextActionDate: req.NextActionDate,
		CreatedBy:      &userID,
	}
	if req.ContactID != nil {
		if cid, err := uuid.Parse(*req.ContactID); err == nil {
			interaction.ContactID = &cid
		}
	}
	if req.LeadID != nil {
		if lid, err := uuid.Parse(*req.LeadID); err == nil {
			interaction.LeadID = &lid
		}
	}
	if req.EventID != nil {
		if eid, err := uuid.Parse(*req.EventID); err == nil {
			interaction.EventID = &eid
		}
	}
	if req.ParticipantID != nil {
		if pid, err := uuid.Parse(*req.ParticipantID); err == nil {
			interaction.ParticipantID = &pid
		}
	}
	// Auto-link contact_id: if interaction has lead_id but no contact_id, resolve from lead
	if interaction.LeadID != nil && interaction.ContactID == nil {
		var contactID *uuid.UUID
		_ = s.repos.DB().QueryRow(c.Context(), `SELECT contact_id FROM leads WHERE id = $1`, *interaction.LeadID).Scan(&contactID)
		if contactID != nil {
			interaction.ContactID = contactID
		}
	}

	if err := s.services.Interaction.LogInteraction(c.Context(), interaction); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Invalidate lead detail + interactions cache
	if interaction.LeadID != nil {
		s.invalidateLeadDetailCache(*interaction.LeadID)
	}

	// Broadcast interaction update via WebSocket
	if s.hub != nil {
		leadIDStr := ""
		if interaction.LeadID != nil {
			leadIDStr = interaction.LeadID.String()
		}
		s.hub.BroadcastToAccount(accountID, ws.EventInteractionUpdate, map[string]interface{}{
			"action":  "created",
			"lead_id": leadIDStr,
		})
	}

	return c.Status(201).JSON(fiber.Map{"success": true, "interaction": interaction})
}

func (s *Server) handleGetInteractions(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	if participantID := c.Query("participant_id"); participantID != "" {
		if pid, err := uuid.Parse(participantID); err == nil {
			accountID := c.Locals("account_id").(uuid.UUID)
			var contactID, leadID *uuid.UUID
			err := s.repos.DB().QueryRow(c.Context(), `
				SELECT COALESCE(ep.contact_id, l.contact_id), ep.lead_id
				FROM event_participants ep
				JOIN events e ON e.id = ep.event_id
				LEFT JOIN leads l ON l.id = ep.lead_id
				WHERE ep.id = $1 AND e.account_id = $2
			`, pid, accountID).Scan(&contactID, &leadID)
			if err != nil {
				if err == pgx.ErrNoRows {
					return c.JSON(fiber.Map{"success": true, "interactions": []*domain.Interaction{}})
				}
				return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
			}

			rows, err := s.repos.DB().Query(c.Context(), `
				SELECT DISTINCT ON (i.id)
				       i.id, i.account_id, i.contact_id, i.lead_id, i.event_id, i.participant_id,
				       i.type, i.direction, i.outcome, i.notes, i.next_action, i.next_action_date,
				       i.created_by, i.created_at, u.display_name AS created_by_name
				FROM interactions i
				LEFT JOIN users u ON u.id = i.created_by
				WHERE i.account_id = $1
				  AND (
				    i.participant_id = $2
				    OR ($3::uuid IS NOT NULL AND i.contact_id = $3)
				    OR ($4::uuid IS NOT NULL AND i.lead_id = $4)
				    OR ($3::uuid IS NOT NULL AND i.lead_id IN (
				      SELECT l2.id FROM leads l2 WHERE l2.account_id = $1 AND l2.contact_id = $3
				    ))
				  )
				ORDER BY i.id, i.created_at DESC
			`, accountID, pid, contactID, leadID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			defer rows.Close()

			var all []*domain.Interaction
			for rows.Next() {
				it := &domain.Interaction{}
				if err := rows.Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID, &it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate, &it.CreatedBy, &it.CreatedAt, &it.CreatedByName); err != nil {
					return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
				}
				all = append(all, it)
			}
			if err := rows.Err(); err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			sort.Slice(all, func(i, j int) bool {
				return all[i].CreatedAt.After(all[j].CreatedAt)
			})
			if offset > len(all) {
				all = []*domain.Interaction{}
			} else {
				all = all[offset:]
			}
			if limit > 0 && len(all) > limit {
				all = all[:limit]
			}
			interactions := all
			if interactions == nil {
				interactions = make([]*domain.Interaction, 0)
			}
			return c.JSON(fiber.Map{"success": true, "interactions": interactions})
		}
	}
	if eventID := c.Query("event_id"); eventID != "" {
		if eid, err := uuid.Parse(eventID); err == nil {
			interactions, err := s.services.Interaction.GetByEventID(c.Context(), eid, limit, offset)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			if interactions == nil {
				interactions = make([]*domain.Interaction, 0)
			}
			return c.JSON(fiber.Map{"success": true, "interactions": interactions})
		}
	}
	if contactID := c.Query("contact_id"); contactID != "" {
		if cid, err := uuid.Parse(contactID); err == nil {
			interactions, err := s.services.Interaction.GetByContactID(c.Context(), cid, limit, offset)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			if interactions == nil {
				interactions = make([]*domain.Interaction, 0)
			}
			return c.JSON(fiber.Map{"success": true, "interactions": interactions})
		}
	}
	if leadID := c.Query("lead_id"); leadID != "" {
		if lid, err := uuid.Parse(leadID); err == nil {
			interactions, err := s.services.Interaction.GetByLeadID(c.Context(), lid, limit, offset)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
			}
			if interactions == nil {
				interactions = make([]*domain.Interaction, 0)
			}
			return c.JSON(fiber.Map{"success": true, "interactions": interactions})
		}
	}
	return c.Status(400).JSON(fiber.Map{"success": false, "error": "Provide participant_id, event_id, contact_id, or lead_id query parameter"})
}

func (s *Server) handleDeleteInteraction(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid interaction ID"})
	}

	// Before deleting, capture lead_id and type for Kommo re-push
	accountID := c.Locals("account_id").(uuid.UUID)
	var interactionLeadID *uuid.UUID
	var interactionType string
	_ = s.repos.DB().QueryRow(c.Context(), `SELECT lead_id, type FROM interactions WHERE id = $1`, id).Scan(&interactionLeadID, &interactionType)

	if err := s.services.Interaction.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Invalidate lead detail + interactions cache
	if interactionLeadID != nil {
		s.invalidateLeadDetailCache(*interactionLeadID)
	}

	// Broadcast interaction update via WebSocket
	if s.hub != nil {
		leadIDStr := ""
		if interactionLeadID != nil {
			leadIDStr = interactionLeadID.String()
		}
		s.hub.BroadcastToAccount(accountID, ws.EventInteractionUpdate, map[string]interface{}{
			"action":  "deleted",
			"lead_id": leadIDStr,
		})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleGetContactInteractions(c *fiber.Ctx) error {
	contactID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid contact ID"})
	}
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)
	interactions, err := s.services.Interaction.GetByContactID(c.Context(), contactID, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if interactions == nil {
		interactions = make([]*domain.Interaction, 0)
	}
	return c.JSON(fiber.Map{"success": true, "interactions": interactions})
}

func (s *Server) handleGetLeadInteractions(c *fiber.Ctx) error {
	leadID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid lead ID"})
	}
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	// Try Redis cache first (30s TTL)
	cacheKey := fmt.Sprintf("lead_interactions:%s:%d:%d", leadID.String(), limit, offset)
	if s.cache != nil {
		if cached, err := s.cache.Get(c.Context(), cacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	interactions, err := s.services.Interaction.GetByLeadID(c.Context(), leadID, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if interactions == nil {
		interactions = make([]*domain.Interaction, 0)
	}

	result := fiber.Map{"success": true, "interactions": interactions}
	if s.cache != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(c.Context(), cacheKey, data, 30*time.Second)
		}
	}

	return c.JSON(result)
}

// handleBatchLeadObservations returns observations for multiple leads in a single request
func (s *Server) handleBatchLeadObservations(c *fiber.Ctx) error {
	var req struct {
		LeadIDs []string `json:"lead_ids"`
		Limit   int      `json:"limit"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if len(req.LeadIDs) == 0 {
		return c.JSON(fiber.Map{"success": true, "observations": map[string]interface{}{}})
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Limit > 20 {
		req.Limit = 20
	}

	var leadUUIDs []uuid.UUID
	for _, id := range req.LeadIDs {
		if uid, err := uuid.Parse(id); err == nil {
			leadUUIDs = append(leadUUIDs, uid)
		}
	}
	if len(leadUUIDs) == 0 {
		return c.JSON(fiber.Map{"success": true, "observations": map[string]interface{}{}})
	}

	// Use a window function to get top N observations per lead in a single query
	rows, err := s.repos.DB().Query(c.Context(), `
		SELECT lead_id, id, type, direction, outcome, notes, created_by_name, created_at
		FROM (
			SELECT i.lead_id, i.id, i.type, i.direction, i.outcome, i.notes,
			       u.display_name as created_by_name, i.created_at,
			       ROW_NUMBER() OVER (PARTITION BY i.lead_id ORDER BY i.created_at DESC) as rn
			FROM interactions i
			LEFT JOIN users u ON i.created_by = u.id
			WHERE i.lead_id = ANY($1)
		) sub
		WHERE rn <= $2
		ORDER BY lead_id, created_at DESC
	`, leadUUIDs, req.Limit)
	if err != nil {
		log.Printf("[API] Error querying batch observations: %v", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer rows.Close()

	result := make(map[string][]*domain.Interaction)
	for rows.Next() {
		var leadID uuid.UUID
		i := &domain.Interaction{}
		if err := rows.Scan(&leadID, &i.ID, &i.Type, &i.Direction, &i.Outcome, &i.Notes, &i.CreatedByName, &i.CreatedAt); err != nil {
			log.Printf("[API] Error scanning batch observation row: %v", err)
			continue
		}
		lid := leadID.String()
		result[lid] = append(result[lid], i)
	}

	return c.JSON(fiber.Map{"success": true, "observations": result})
}

func (s *Server) handleGetContactEvents(c *fiber.Ctx) error {
	contactID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid contact ID"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	events, err := s.services.Event.GetByContactID(c.Context(), accountID, contactID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if events == nil {
		events = make([]*domain.Event, 0)
	}
	return c.JSON(fiber.Map{"success": true, "events": events})
}

// --- Stats Handler ---

func (s *Server) handleGetStats(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var leadCount, contactCount int
	_ = s.repos.DB().QueryRow(c.Context(),
		`SELECT COUNT(*) FROM leads WHERE account_id = $1`, accountID).Scan(&leadCount)
	_ = s.repos.DB().QueryRow(c.Context(),
		`SELECT COUNT(*) FROM contacts WHERE account_id = $1`, accountID).Scan(&contactCount)

	return c.JSON(fiber.Map{
		"success": true,
		"stats": fiber.Map{
			"connected_devices": s.pool.GetConnectedCount(),
			"ws_clients":        s.hub.GetClientCount(),
			"leads":             leadCount,
			"contacts":          contactCount,
		},
	})
}

// --- WebSocket Handler ---

func (s *Server) handleWebSocket(c *websocket.Conn) {
	claims := c.Locals("claims").(*service.JWTClaims)

	client := &ws.Client{
		ID:        uuid.New().String(),
		AccountID: claims.AccountID,
		UserID:    claims.UserID,
		Conn:      c,
		Send:      make(chan []byte, 256),
		Hub:       s.hub,
	}

	s.hub.Register(client)

	go client.WritePump()
	client.ReadPump()
}

func (s *Server) Listen(addr string) error {
	return s.app.Listen(addr)
}

func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

// StartEventTagSyncWorker starts the background worker that periodically reconciles
// event participants based on configured tags. Should be called from main.go.
func (s *Server) StartEventTagSyncWorker(ctx context.Context) {
	go func() {
		log.Println("[EVENT-SYNC] 🏷️ Event tag sync worker started (interval: 60s)")
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		// Run initial reconciliation after a short delay
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			s.runEventTagSync(ctx)
		}

		for {
			select {
			case <-ctx.Done():
				log.Println("[EVENT-SYNC] Worker stopped")
				return
			case <-ticker.C:
				s.runEventTagSync(ctx)
			}
		}
	}()
}

func (s *Server) runEventTagSync(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[EVENT-SYNC] ⚠️ PANIC recovered: %v", rec)
		}
	}()

	eventsWithTags, err := s.repos.Event.GetActiveEventsWithTags(ctx)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error fetching events with tags: %v", err)
		return
	}
	if len(eventsWithTags) == 0 {
		return
	}

	for _, ewt := range eventsWithTags {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Get default stage for the event
		var stageID *uuid.UUID
		if ewt.Event.PipelineID != nil {
			stages, _ := s.services.Event.GetPipelineStages(ctx, *ewt.Event.PipelineID)
			if len(stages) > 0 {
				stageID = &stages[0].ID
			}
		}

		var added, removed int
		var reconcileErr error
		if ewt.Event.TagFormulaType == "advanced" && ewt.Event.TagFormula != "" {
			added, removed, reconcileErr = s.services.Event.ReconcileEventParticipantsAdvanced(
				ctx, ewt.Event.ID, ewt.Event.AccountID, ewt.Event.TagFormula, stageID,
			)
		} else {
			added, removed, reconcileErr = s.services.Event.ReconcileEventParticipants(
				ctx, ewt.Event.ID, ewt.Event.AccountID, ewt.Event.TagFormulaMode, ewt.Includes, ewt.Excludes, stageID,
			)
		}
		if reconcileErr != nil {
			log.Printf("[EVENT-SYNC] Error reconciling event '%s': %v", ewt.Event.Name, reconcileErr)
			continue
		}
		if added > 0 || removed > 0 {
			log.Printf("[EVENT-SYNC] Event '%s': +%d added, -%d removed", ewt.Event.Name, added, removed)
			if s.hub != nil {
				s.hub.BroadcastToAccount(ewt.Event.AccountID, "event_participant_update", map[string]interface{}{
					"event_id": ewt.Event.ID,
					"action":   "tag_sync_reconcile",
					"added":    added,
					"removed":  removed,
				})
			}
		}
	}
}

func (s *Server) handleGetRecentStickers(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	urls, err := s.services.Chat.GetRecentStickers(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	if urls == nil {
		urls = []string{}
	}

	return c.JSON(fiber.Map{"success": true, "stickers": urls})
}

func (s *Server) handleGetSavedStickers(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	urls, err := s.services.Chat.GetSavedStickers(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if urls == nil {
		urls = []string{}
	}
	return c.JSON(fiber.Map{"success": true, "stickers": urls})
}

func (s *Server) handleSaveSticker(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		MediaURL string `json:"media_url"`
	}
	if err := c.BodyParser(&req); err != nil || req.MediaURL == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "media_url is required"})
	}

	if err := s.services.Chat.SaveSticker(c.Context(), accountID, req.MediaURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleDeleteSavedSticker(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		MediaURL string `json:"media_url"`
	}
	if err := c.BodyParser(&req); err != nil || req.MediaURL == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "media_url is required"})
	}

	if err := s.services.Chat.DeleteSavedSticker(c.Context(), accountID, req.MediaURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// --- Super Admin Handlers ---

func (s *Server) handleAdminGetAccounts(c *fiber.Ctx) error {
	accounts, err := s.services.Account.GetAll(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if accounts == nil {
		accounts = []*domain.Account{}
	}
	return c.JSON(fiber.Map{"success": true, "accounts": accounts})
}

func (s *Server) handleAdminCreateAccount(c *fiber.Ctx) error {
	var req struct {
		Name              string `json:"name"`
		Slug              string `json:"slug"`
		Plan              string `json:"plan"`
		MaxDevices        int    `json:"max_devices"`
		MaxUsersOverride  *int   `json:"max_users_override"`
		StorageLimitBytes int64  `json:"storage_limit_bytes"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	if req.Plan == "" {
		req.Plan = "basic"
	}
	if req.MaxDevices <= 0 {
		req.MaxDevices = 5
	}
	if req.MaxUsersOverride != nil && *req.MaxUsersOverride < 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "max_users_override must be 0 or greater"})
	}

	account := &domain.Account{
		Name:              req.Name,
		Slug:              req.Slug,
		Plan:              req.Plan,
		MaxDevices:        req.MaxDevices,
		MaxUsersOverride:  req.MaxUsersOverride,
		StorageLimitBytes: req.StorageLimitBytes,
		IsActive:          true,
	}

	if err := s.services.Account.Create(c.Context(), account); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if err := s.services.Subscription.CreateForAccount(c.Context(), account.ID, account.Plan, domain.SubscriptionStatusActive, 0); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Seed template surveys for the new account
	if err := database.SeedTemplateSurveysForAccount(s.repos.DB(), account.ID.String()); err != nil {
		log.Printf("[API] Warning: failed to seed template surveys for new account %s: %v", account.ID, err)
	}

	return c.Status(201).JSON(fiber.Map{"success": true, "account": account})
}

func (s *Server) handleAdminGetAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	account, err := s.services.Account.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if account == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Account not found"})
	}

	return c.JSON(fiber.Map{"success": true, "account": account})
}

func (s *Server) handleAdminUpdateAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	var req struct {
		Name              string `json:"name"`
		Slug              string `json:"slug"`
		Plan              string `json:"plan"`
		MaxDevices        int    `json:"max_devices"`
		MaxUsersOverride  *int   `json:"max_users_override"`
		StorageLimitBytes int64  `json:"storage_limit_bytes"`
		MCPEnabled        bool   `json:"mcp_enabled"`
		KommoEnabled      bool   `json:"kommo_enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Plan == "" {
		existing, err := s.services.Account.GetByID(c.Context(), id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
		if existing == nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "error": "Account not found"})
		}
		req.Plan = existing.Plan
	}
	if req.MaxUsersOverride != nil && *req.MaxUsersOverride < 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "max_users_override must be 0 or greater"})
	}

	account := &domain.Account{
		ID:                id,
		Name:              req.Name,
		Slug:              req.Slug,
		Plan:              req.Plan,
		MaxDevices:        req.MaxDevices,
		MaxUsersOverride:  req.MaxUsersOverride,
		StorageLimitBytes: req.StorageLimitBytes,
		MCPEnabled:        req.MCPEnabled,
		KommoEnabled:      req.KommoEnabled,
	}

	if err := s.services.Account.Update(c.Context(), account); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	subOverview, err := s.services.Subscription.GetOverview(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if subOverview != nil && subOverview.Subscription != nil {
		subOverview.Subscription.PlanCode = req.Plan
		if err := s.services.Subscription.Upsert(c.Context(), subOverview.Subscription); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}

	return c.JSON(fiber.Map{"success": true, "account": account})
}

func (s *Server) handleAdminToggleAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	if err := s.services.Account.ToggleActive(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAdminDeleteAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	// Safety: prevent deleting account that has devices, chats, or contacts
	account, err := s.services.Account.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if account == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Account not found"})
	}
	if account.DeviceCount > 0 || account.ChatCount > 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No se puede eliminar una cuenta que tiene dispositivos o chats. Elimine primero los dispositivos."})
	}
	if account.UserCount > 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No se puede eliminar una cuenta que tiene usuarios. Elimine primero los usuarios."})
	}

	if err := s.services.Account.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) adminAccountPurgeSummary(ctx context.Context, accountID uuid.UUID) (fiber.Map, error) {
	tables := []string{
		"user_accounts", "users", "devices", "contacts", "chats", "messages", "leads", "pipelines", "tags",
		"campaigns", "events", "programs", "documents", "quick_replies", "automation_flows", "google_contacts_sync",
		"kommo_connected_pipelines", "kommo_push_outbox", "integration_instance_accounts",
	}
	counts := fiber.Map{}
	for _, table := range tables {
		var count int64
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE account_id = $1`, table)
		if err := s.repos.DB().QueryRow(ctx, query, accountID).Scan(&count); err != nil {
			counts[table] = nil
			continue
		}
		counts[table] = count
	}
	var storageObjects int64
	if s.storage != nil {
		count, err := s.storage.CountPrefix(ctx, accountID.String()+"/")
		if err != nil {
			return nil, err
		}
		storageObjects = count
	}
	return fiber.Map{"tables": counts, "storage_objects": storageObjects}, nil
}

func (s *Server) handleAdminAccountPurgePreview(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}
	account, err := s.services.Account.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if account == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Account not found"})
	}
	summary, err := s.adminAccountPurgeSummary(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "account": account, "summary": summary, "confirmation": account.Name})
}

func (s *Server) handleAdminPurgeAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}
	account, err := s.services.Account.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if account == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Account not found"})
	}
	var req struct {
		Confirmation string `json:"confirmation"`
		DeleteFiles  *bool  `json:"delete_files"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Confirmation != account.Name {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Confirmation must match the account name"})
	}
	deleteFiles := true
	if req.DeleteFiles != nil {
		deleteFiles = *req.DeleteFiles
	}

	summary, err := s.adminAccountPurgeSummary(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	var deletedFiles int64
	if deleteFiles && s.storage != nil {
		deletedFiles, err = s.storage.DeletePrefix(c.Context(), id.String()+"/")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}

	tx, err := s.repos.DB().Begin(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	defer tx.Rollback(c.Context())

	_, _ = tx.Exec(c.Context(), `
		UPDATE users u
		SET account_id = (
			SELECT ua.account_id
			FROM user_accounts ua
			WHERE ua.user_id = u.id AND ua.account_id <> $1
			ORDER BY ua.created_at ASC
			LIMIT 1
		)
		WHERE u.account_id = $1
		  AND EXISTS (SELECT 1 FROM user_accounts ua WHERE ua.user_id = u.id AND ua.account_id <> $1)
	`, id)
	if _, err := tx.Exec(c.Context(), `DELETE FROM accounts WHERE id = $1`, id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if err := tx.Commit(c.Context()); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.reloadKommoManager(c.Context())
	return c.JSON(fiber.Map{"success": true, "purged": true, "deleted_files": deletedFiles, "summary": summary})
}

func (s *Server) handleAdminGetUsers(c *fiber.Ctx) error {
	var accountID *uuid.UUID
	if aid := c.Query("account_id"); aid != "" {
		parsed, err := uuid.Parse(aid)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid account_id"})
		}
		accountID = &parsed
	}

	users, err := s.services.Account.GetUsers(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if users == nil {
		users = []*domain.User{}
	}
	s.attachAdminUserAccounts(c.Context(), users)

	return c.JSON(fiber.Map{"success": true, "users": users})
}

func (s *Server) handleAdminCreateUser(c *fiber.Ctx) error {
	type accountAssignmentRequest struct {
		AccountID string  `json:"account_id"`
		Role      string  `json:"role"`
		RoleID    *string `json:"role_id"`
		IsDefault bool    `json:"is_default"`
	}
	var req struct {
		AccountID   string                     `json:"account_id"`
		Username    string                     `json:"username"`
		Email       string                     `json:"email"`
		Password    string                     `json:"password"`
		DisplayName string                     `json:"display_name"`
		Role        string                     `json:"role"`
		RoleID      *string                    `json:"role_id"`
		Accounts    []accountAssignmentRequest `json:"accounts"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "username and password are required"})
	}
	if req.Email == "" {
		req.Email = fmt.Sprintf("%s@users.clarin.local", strings.ToLower(req.Username))
	}

	assignments := req.Accounts
	if len(assignments) == 0 && req.AccountID != "" {
		assignments = []accountAssignmentRequest{{AccountID: req.AccountID, Role: req.Role, RoleID: req.RoleID, IsDefault: true}}
	}
	if len(assignments) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "at least one account assignment is required"})
	}

	defaultIdx := 0
	for i := range assignments {
		if assignments[i].IsDefault {
			defaultIdx = i
			break
		}
	}
	assignments[defaultIdx].IsDefault = true
	accountID, err := uuid.Parse(assignments[defaultIdx].AccountID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid account_id"})
	}
	checkedAccounts := make(map[uuid.UUID]bool)
	for _, assignment := range assignments {
		assignmentAccountID, parseErr := uuid.Parse(assignment.AccountID)
		if parseErr != nil || checkedAccounts[assignmentAccountID] {
			continue
		}
		checkedAccounts[assignmentAccountID] = true
		if err := s.enforcePlanLimit(c.Context(), assignmentAccountID, "max_users", 1); err != nil {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_users"})
		}
	}
	primaryRole := assignments[defaultIdx].Role
	if primaryRole == "" {
		primaryRole = domain.RoleAgent
	}

	user := &domain.User{
		AccountID:    accountID,
		Username:     req.Username,
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		Role:         primaryRole,
		IsAdmin:      primaryRole == domain.RoleAdmin || primaryRole == domain.RoleSuperAdmin,
		IsSuperAdmin: primaryRole == domain.RoleSuperAdmin,
	}

	if err := s.services.Account.CreateUser(c.Context(), user, req.Password); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	for i, assignment := range assignments {
		assignmentAccountID, err := uuid.Parse(assignment.AccountID)
		if err != nil {
			continue
		}
		role := assignment.Role
		if role == "" {
			role = domain.RoleAgent
		}
		ua := &domain.UserAccount{
			UserID:    user.ID,
			AccountID: assignmentAccountID,
			Role:      role,
			IsDefault: assignment.IsDefault || i == defaultIdx,
		}
		if assignment.RoleID != nil && *assignment.RoleID != "" {
			if parsed, err := uuid.Parse(*assignment.RoleID); err == nil {
				ua.RoleID = &parsed
			}
		}
		if err := s.services.Account.AssignUserAccount(c.Context(), ua); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
		}
	}
	assignmentsResp, _ := s.services.Account.GetUserAccountAssignments(c.Context(), user.ID)
	if assignmentsResp != nil {
		user.Accounts = make([]domain.UserAccount, 0, len(assignmentsResp))
		for _, ua := range assignmentsResp {
			user.Accounts = append(user.Accounts, *ua)
		}
	}

	return c.Status(201).JSON(fiber.Map{"success": true, "user": user})
}

func (s *Server) handleAdminUpdateUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	if strings.TrimSpace(req.Email) == "" && strings.TrimSpace(req.Username) != "" {
		req.Email = fmt.Sprintf("%s@users.clarin.local", strings.ToLower(strings.TrimSpace(req.Username)))
	}
	if req.Role == "" {
		existing, _ := s.services.Auth.GetUser(c.Context(), id)
		if existing != nil {
			req.Role = existing.Role
		}
	}
	if req.Role == "" {
		req.Role = domain.RoleAgent
	}
	user := &domain.User{
		ID:           id,
		Username:     strings.TrimSpace(req.Username),
		Email:        strings.TrimSpace(req.Email),
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		IsAdmin:      req.Role == domain.RoleAdmin || req.Role == domain.RoleSuperAdmin,
		IsSuperAdmin: req.Role == domain.RoleSuperAdmin,
	}

	if err := s.services.Account.UpdateUser(c.Context(), user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) attachAdminUserAccounts(ctx context.Context, users []*domain.User) {
	for _, user := range users {
		assignments, err := s.services.Account.GetUserAccountAssignments(ctx, user.ID)
		if err != nil || assignments == nil {
			continue
		}
		user.Accounts = make([]domain.UserAccount, 0, len(assignments))
		names := make([]string, 0, len(assignments))
		for _, ua := range assignments {
			user.Accounts = append(user.Accounts, *ua)
			label := ua.AccountName
			if ua.RoleName != "" {
				label += " · " + ua.RoleName
			} else if ua.Role != "" {
				label += " · " + ua.Role
			}
			names = append(names, label)
		}
		if len(names) > 0 {
			user.AccountName = strings.Join(names, ", ")
		}
	}
}

func (s *Server) handleAdminToggleUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	if err := s.services.Account.ToggleUserActive(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Invalidate active sessions so the user must re-login
	s.services.Auth.InvalidateUserSessions(id)

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAdminResetPassword(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Password is required"})
	}

	if err := s.services.Account.ResetPassword(c.Context(), id, req.Password); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAdminDeleteUser(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid ID"})
	}

	// Safety: cannot delete yourself
	claims := c.Locals("claims").(*service.JWTClaims)
	if claims.UserID == id {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No puedes eliminar tu propia cuenta de usuario"})
	}

	// Safety: cannot delete a super admin
	user, err := s.services.Auth.GetUser(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if user != nil && user.IsSuperAdmin {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "No se puede eliminar un super administrador"})
	}

	// Invalidate active sessions before deleting
	s.services.Auth.InvalidateUserSessions(id)

	if err := s.services.Account.DeleteUser(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	return c.JSON(fiber.Map{"success": true})
}

// --- Switch Account Handler ---

func (s *Server) handleSwitchAccount(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)

	var req struct {
		AccountID string `json:"account_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	targetAccountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid account_id"})
	}

	token, refreshToken, user, err := s.services.Auth.SwitchAccount(c.Context(), userID, targetAccountID, s.cfg.JWTSecret)
	if err != nil {
		return c.Status(403).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Set access token cookie
	c.Cookie(&fiber.Cookie{
		Name:     "auth-token",
		Value:    token,
		Expires:  time.Now().Add(1 * time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Lax",
		Path:     "/",
	})

	// Set refresh token cookie
	c.Cookie(&fiber.Cookie{
		Name:     "refresh-token",
		Value:    refreshToken,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HTTPOnly: true,
		Secure:   s.cfg.IsProduction(),
		SameSite: "Strict",
		Path:     "/api/auth",
	})

	// Compute permissions for response — per-account admin gets full access
	isAdmin := user.IsAdmin || user.IsSuperAdmin || user.Role == domain.RoleAdmin || user.Role == domain.RoleSuperAdmin
	perms := []string{domain.PermAll}
	if !isAdmin {
		perms, _ = s.repos.UserAccount.GetUserPermissions(c.Context(), userID, targetAccountID)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"token":   token,
		"user": fiber.Map{
			"id":             user.ID,
			"username":       user.Username,
			"email":          user.Email,
			"display_name":   user.DisplayName,
			"is_admin":       isAdmin,
			"is_super_admin": user.IsSuperAdmin,
			"role":           user.Role,
			"account_id":     user.AccountID,
			"account_name":   user.AccountName,
			"permissions":    perms,
		},
	})
}

func (s *Server) handleGetMyAccounts(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)

	userAccounts, err := s.services.Auth.GetUserAccounts(c.Context(), userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	accountsList := make([]fiber.Map, 0)
	for _, ua := range userAccounts {
		accountsList = append(accountsList, fiber.Map{
			"account_id":   ua.AccountID,
			"account_name": ua.AccountName,
			"account_slug": ua.AccountSlug,
			"role":         ua.Role,
			"is_default":   ua.IsDefault,
		})
	}

	return c.JSON(fiber.Map{"success": true, "accounts": accountsList})
}

// --- Admin User-Account Assignment Handlers ---

func (s *Server) handleAdminGetUserAccounts(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid user ID"})
	}

	userAccounts, err := s.services.Auth.GetUserAccounts(c.Context(), userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	accountsList := make([]fiber.Map, 0)
	for _, ua := range userAccounts {
		accountsList = append(accountsList, fiber.Map{
			"id":           ua.ID,
			"account_id":   ua.AccountID,
			"account_name": ua.AccountName,
			"role":         ua.Role,
			"role_id":      ua.RoleID,
			"role_name":    ua.RoleName,
			"permissions":  ua.Permissions,
			"is_default":   ua.IsDefault,
		})
	}

	return c.JSON(fiber.Map{"success": true, "accounts": accountsList})
}

func (s *Server) handleAdminAssignUserAccount(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid user ID"})
	}

	var req struct {
		AccountID string  `json:"account_id"`
		Role      string  `json:"role"`
		RoleID    *string `json:"role_id"`
		IsDefault bool    `json:"is_default"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid account_id"})
	}

	if req.Role == "" {
		req.Role = domain.RoleAgent
	}
	exists, err := s.repos.UserAccount.Exists(c.Context(), userID, accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if !exists {
		if err := s.enforcePlanLimit(c.Context(), accountID, "max_users", 1); err != nil {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{"success": false, "error": err.Error(), "code": "plan_limit_reached", "limit": "max_users"})
		}
	}

	ua := &domain.UserAccount{
		UserID:    userID,
		AccountID: accountID,
		Role:      req.Role,
		IsDefault: req.IsDefault,
	}

	// Parse optional role_id
	if req.RoleID != nil && *req.RoleID != "" {
		parsed, err := uuid.Parse(*req.RoleID)
		if err == nil {
			ua.RoleID = &parsed
		}
	}

	if err := s.services.Account.AssignUserAccount(c.Context(), ua); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.services.Auth.InvalidateUserSessions(userID)

	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAdminRemoveUserAccount(c *fiber.Ctx) error {
	userID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid user ID"})
	}

	accountID, err := uuid.Parse(c.Params("account_id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid account_id"})
	}

	if err := s.services.Account.RemoveUserAccount(c.Context(), userID, accountID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.services.Auth.InvalidateUserSessions(userID)

	return c.JSON(fiber.Map{"success": true})
}

// --- Quick Reply Handlers ---

func (s *Server) handleGetQuickReplies(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	replies, err := s.services.QuickReply.GetByAccountID(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if replies == nil {
		replies = make([]*domain.QuickReply, 0)
	}
	return c.JSON(fiber.Map{"success": true, "quick_replies": replies})
}

func (s *Server) handleCreateQuickReply(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		Shortcut      string `json:"shortcut"`
		Title         string `json:"title"`
		Body          string `json:"body"`
		MediaURL      string `json:"media_url"`
		MediaType     string `json:"media_type"`
		MediaFilename string `json:"media_filename"`
		Attachments   []struct {
			MediaURL      string `json:"media_url"`
			MediaType     string `json:"media_type"`
			MediaFilename string `json:"media_filename"`
			Caption       string `json:"caption"`
		} `json:"attachments"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Shortcut == "" || (req.Body == "" && req.MediaURL == "" && len(req.Attachments) == 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Shortcut and body or media are required"})
	}
	qr := &domain.QuickReply{AccountID: accountID, Shortcut: req.Shortcut, Title: req.Title, Body: req.Body, MediaURL: req.MediaURL, MediaType: req.MediaType, MediaFilename: req.MediaFilename}
	for i, a := range req.Attachments {
		if i >= 5 {
			break
		}
		qr.Attachments = append(qr.Attachments, domain.QuickReplyAttachment{
			MediaURL: a.MediaURL, MediaType: a.MediaType, MediaFilename: a.MediaFilename, Caption: a.Caption, Position: i,
		})
	}
	if err := s.services.QuickReply.Create(c.Context(), qr); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "quick_reply": qr})
}

func (s *Server) handleUpdateQuickReply(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid quick reply ID"})
	}
	var req struct {
		Shortcut      string `json:"shortcut"`
		Title         string `json:"title"`
		Body          string `json:"body"`
		MediaURL      string `json:"media_url"`
		MediaType     string `json:"media_type"`
		MediaFilename string `json:"media_filename"`
		Attachments   []struct {
			MediaURL      string `json:"media_url"`
			MediaType     string `json:"media_type"`
			MediaFilename string `json:"media_filename"`
			Caption       string `json:"caption"`
		} `json:"attachments"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	qr := &domain.QuickReply{ID: id, Shortcut: req.Shortcut, Title: req.Title, Body: req.Body, MediaURL: req.MediaURL, MediaType: req.MediaType, MediaFilename: req.MediaFilename}
	for i, a := range req.Attachments {
		if i >= 5 {
			break
		}
		qr.Attachments = append(qr.Attachments, domain.QuickReplyAttachment{
			MediaURL: a.MediaURL, MediaType: a.MediaType, MediaFilename: a.MediaFilename, Caption: a.Caption, Position: i,
		})
	}
	if err := s.services.QuickReply.Update(c.Context(), qr); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "quick_reply": qr})
}

func (s *Server) handleDeleteQuickReply(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid quick reply ID"})
	}
	if err := s.services.QuickReply.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// --- Kommo Webhook Handler (public, no auth — secret in URL) ---

// handleKommoWebhook processes incoming webhooks from Kommo.
// Validates secret, parses the form-encoded payload, fetches the full lead,
// and syncs it to ALL accounts with Kommo integration enabled.
func (s *Server) handleKommoWebhook(c *fiber.Ctx) error {
	secret := c.Params("secret")
	kommoSync := s.kommoForWebhook(secret)
	if kommoSync == nil {
		return c.SendStatus(fiber.StatusNotFound) // Return 404 to not reveal webhook exists
	}

	// Kommo sends form-encoded data with bracket notation:
	// leads[update][0][id]=12345, leads[add][0][id]=12345, etc.
	// We just need to extract the lead IDs from the form data.
	body := string(c.Body())
	if body == "" {
		return c.SendStatus(fiber.StatusOK)
	}

	// Parse form values
	args := c.Request().PostArgs()

	// Collect all lead IDs from the webhook payload
	kommoLeadIDs := make(map[int]bool)

	// Check various event patterns: leads[update], leads[add], leads[status]
	for _, action := range []string{"update", "add", "status"} {
		for i := 0; i < 50; i++ { // Kommo batches up to ~50 leads per webhook
			key := fmt.Sprintf("leads[%s][%d][id]", action, i)
			val := args.Peek(key)
			if len(val) == 0 {
				break
			}
			if id, err := strconv.Atoi(string(val)); err == nil && id > 0 {
				kommoLeadIDs[id] = true
			}
		}
	}

	// Check for delete events: leads[delete][0][id]
	var deletedLeadIDs []int
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("leads[delete][%d][id]", i)
		val := args.Peek(key)
		if len(val) == 0 {
			break
		}
		if id, err := strconv.Atoi(string(val)); err == nil && id > 0 {
			deletedLeadIDs = append(deletedLeadIDs, id)
		}
	}

	if len(kommoLeadIDs) == 0 && len(deletedLeadIDs) == 0 {
		// Might be a contact or other event — acknowledge but don't process
		return c.SendStatus(fiber.StatusOK)
	}

	log.Printf("[WEBHOOK] Received %d lead IDs, %d deleted from Kommo", len(kommoLeadIDs), len(deletedLeadIDs))

	// Process asynchronously to avoid blocking the webhook response
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		for kommoID := range kommoLeadIDs {
			kommoSync.ProcessWebhookLead(ctx, kommoID)
		}

		// Mark deleted leads across all accounts
		for _, kommoID := range deletedLeadIDs {
			res, err := s.repos.DB().Exec(ctx,
				`UPDATE leads SET kommo_deleted_at = NOW(), updated_at = NOW() WHERE kommo_id = $1 AND kommo_deleted_at IS NULL`,
				int64(kommoID))
			if err != nil {
				log.Printf("[WEBHOOK] Failed to mark lead Kommo %d as deleted: %v", kommoID, err)
				continue
			}
			if res.RowsAffected() > 0 {
				log.Printf("[WEBHOOK] Marked lead Kommo %d as deleted (%d rows)", kommoID, res.RowsAffected())
			}
		}
	}()

	return c.SendStatus(fiber.StatusOK)
}

// --- Kommo Handlers ---

func (s *Server) handleKommoLegacyDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusGone).JSON(fiber.Map{
		"success": false,
		"error":   "La configuración de Kommo se administra desde Admin > Integraciones",
	})
}

func (s *Server) handleKommoStatus(c *fiber.Ctx) error {
	kommoSync := s.defaultKommoSync()
	configured := kommoSync != nil
	result := fiber.Map{
		"success":    true,
		"configured": configured,
	}
	if s.kommoManager != nil {
		result["runtime"] = s.kommoManager.RuntimeStatus()
	}
	if configured {
		client := kommoSync.GetClient()
		acc, err := client.GetAccount()
		if err != nil {
			result["connected"] = false
			result["error"] = err.Error()
		} else {
			result["connected"] = true
			result["account"] = fiber.Map{
				"id":       acc.ID,
				"name":     acc.Name,
				"currency": acc.Currency,
				"country":  acc.Country,
			}
		}
	}
	return c.JSON(result)
}

func (s *Server) handleKommoSync(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}

	// Check if Kommo is enabled for this account
	var kommoEnabled bool
	_ = s.repos.DB().QueryRow(c.Context(), `SELECT COALESCE(kommo_enabled, false) FROM accounts WHERE id = $1`, accountID).Scan(&kommoEnabled)
	if !kommoEnabled {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Kommo está deshabilitado para esta cuenta"})
	}

	started := kommoSync.StartFullSyncAsync(accountID)
	if !started {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"error":   "Ya hay una sincronización en curso para esta cuenta",
		})
	}

	// Invalidate cache when sync starts (will be stale)
	s.invalidateLeadsCache(accountID)

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Sincronización iniciada en segundo plano",
	})
}

func (s *Server) handleKommoFullSyncStatus(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	status := kommoSync.GetFullSyncStatus(accountID)
	if status == nil {
		return c.JSON(fiber.Map{"success": true, "status": nil})
	}
	return c.JSON(fiber.Map{"success": true, "status": status})
}

func (s *Server) handleKommoGetPipelines(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	client := kommoSync.GetClient()
	pipelines, err := client.GetPipelines()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	// Also get connected pipelines to mark status
	connected, _ := kommoSync.GetConnectedPipelines(c.Context(), accountID)
	connectedMap := make(map[int64]bool)
	for _, cp := range connected {
		if cp.Enabled {
			connectedMap[cp.KommoPipelineID] = true
		}
	}

	type pipelineInfo struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		IsMain    bool   `json:"is_main"`
		Stages    int    `json:"stages"`
		Connected bool   `json:"connected"`
	}
	var result []pipelineInfo
	for _, p := range pipelines {
		result = append(result, pipelineInfo{
			ID:        p.ID,
			Name:      p.Name,
			IsMain:    p.IsMain,
			Stages:    len(p.Statuses),
			Connected: connectedMap[int64(p.ID)],
		})
	}

	return c.JSON(fiber.Map{"success": true, "pipelines": result})
}

func (s *Server) handleKommoGetConnected(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	connected, err := kommoSync.GetConnectedPipelines(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if connected == nil {
		connected = []kommo.ConnectedPipeline{}
	}
	return c.JSON(fiber.Map{"success": true, "connected": connected})
}

func (s *Server) handleKommoConnectPipeline(c *fiber.Ctx) error {
	kommoID, err := strconv.Atoi(c.Params("kommoId"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	cp, err := kommoSync.ConnectPipeline(c.Context(), accountID, kommoID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "connected_pipeline": cp})
}

func (s *Server) handleKommoDisconnectPipeline(c *fiber.Ctx) error {
	kommoID, err := strconv.Atoi(c.Params("kommoId"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid pipeline ID"})
	}
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	if err := kommoSync.DisconnectPipeline(c.Context(), accountID, kommoID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleKommoSyncStatus(c *fiber.Ctx) error {
	kommoSync := s.defaultKommoSync()
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	status := kommoSync.GetStatus()
	result := fiber.Map{"success": true, "status": status}
	if s.kommoManager != nil {
		result["runtime"] = s.kommoManager.RuntimeStatus()
	}
	return c.JSON(result)
}

// handleEventsPollerStatus returns the current status of the Kommo Events API poller.
func (s *Server) handleEventsPollerStatus(c *fiber.Ctx) error {
	kommoSync := s.defaultKommoSync()
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	status := kommoSync.GetEventsPollerStatus()
	return c.JSON(fiber.Map{"success": true, "events_poller": status})
}

// handleForceEventsPoll triggers an immediate events poll cycle.
func (s *Server) handleForceEventsPoll(c *fiber.Ctx) error {
	kommoSync := s.defaultKommoSync()
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	events, leads := kommoSync.ForceEventsPoll()
	return c.JSON(fiber.Map{"success": true, "events_found": events, "leads_synced": leads})
}

// handleSyncMonitor returns the sync monitor data (ring buffer + per-subsystem stats).
func (s *Server) handleSyncMonitor(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		kommoSync = s.defaultKommoSync()
	}
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}
	data := kommoSync.Monitor.GetData()
	return c.JSON(fiber.Map{"success": true, "monitor": data})
}

// handleKommoToggleEnabled enables or disables Kommo integration for the current account.
func (s *Server) handleKommoToggleEnabled(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}

	_, err := s.repos.DB().Exec(c.Context(), `UPDATE accounts SET kommo_enabled = $1 WHERE id = $2`, req.Enabled, accountID)
	if err != nil {
		log.Printf("[API] Failed to toggle kommo_enabled for account %s: %v", accountID, err)
		return c.Status(500).JSON(fiber.Map{"success": false, "error": "Failed to update"})
	}

	log.Printf("[API] Account %s kommo_enabled set to %v", accountID, req.Enabled)
	return c.JSON(fiber.Map{"success": true, "kommo_enabled": req.Enabled})
}

// handleKommoToggleAllPipelines enables or disables ALL connected pipelines for the account.
func (s *Server) handleKommoToggleAllPipelines(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	kommoSync := s.kommoForAccount(c.Context(), accountID)
	if kommoSync == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": "Kommo not configured"})
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "invalid body"})
	}

	result, err := s.repos.DB().Exec(c.Context(),
		`UPDATE kommo_connected_pipelines SET enabled = $2 WHERE account_id = $1 AND integration_instance_id IS NOT DISTINCT FROM $3`,
		accountID, body.Enabled, kommoSync.InstanceID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	s.invalidateLeadsCache(accountID)

	return c.JSON(fiber.Map{
		"success": true,
		"count":   result.RowsAffected(),
		"enabled": body.Enabled,
	})
}

// --- Admin Role Handlers ---

func normalizeRolePermissions(input []string) ([]string, string) {
	allowed := map[string]bool{domain.PermAll: true}
	for _, permission := range domain.AllPermissions {
		allowed[permission] = true
	}

	seen := make(map[string]bool, len(input))
	normalized := make([]string, 0, len(input))
	for _, permission := range input {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		if !allowed[permission] {
			return nil, permission
		}
		if seen[permission] {
			continue
		}
		seen[permission] = true
		normalized = append(normalized, permission)
	}

	return normalized, ""
}

func (s *Server) invalidateUsersWithRole(ctx context.Context, roleID uuid.UUID) {
	rows, err := s.repos.DB().Query(ctx, `SELECT DISTINCT user_id FROM user_accounts WHERE role_id = $1`, roleID)
	if err != nil {
		log.Printf("[ADMIN] failed to load users for role invalidation %s: %v", roleID, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			log.Printf("[ADMIN] failed to scan user for role invalidation %s: %v", roleID, err)
			continue
		}
		s.services.Auth.InvalidateUserSessions(userID)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[ADMIN] failed during role invalidation %s: %v", roleID, err)
	}
}

func (s *Server) handleAdminGetRoles(c *fiber.Ctx) error {
	roles, err := s.services.Role.GetAll(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if roles == nil {
		roles = make([]*domain.Role, 0)
	}
	return c.JSON(fiber.Map{"success": true, "roles": roles})
}

func (s *Server) handleAdminCreateRole(c *fiber.Ctx) error {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	if req.Permissions == nil {
		req.Permissions = []string{}
	}
	permissions, invalid := normalizeRolePermissions(req.Permissions)
	if invalid != "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fmt.Sprintf("Invalid permission: %s", invalid)})
	}

	role := &domain.Role{
		Name:        req.Name,
		Description: req.Description,
		Permissions: permissions,
	}
	if err := s.services.Role.Create(c.Context(), role); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "role": role})
}

func (s *Server) handleAdminUpdateRole(c *fiber.Ctx) error {
	roleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid role ID"})
	}

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	if req.Permissions == nil {
		req.Permissions = []string{}
	}
	permissions, invalid := normalizeRolePermissions(req.Permissions)
	if invalid != "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fmt.Sprintf("Invalid permission: %s", invalid)})
	}

	role := &domain.Role{
		ID:          roleID,
		Name:        req.Name,
		Description: req.Description,
		Permissions: permissions,
	}
	if err := s.services.Role.Update(c.Context(), role); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.invalidateUsersWithRole(c.Context(), roleID)
	return c.JSON(fiber.Map{"success": true, "role": role})
}

func (s *Server) handleAdminDeleteRole(c *fiber.Ctx) error {
	roleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid role ID"})
	}

	if err := s.services.Role.Delete(c.Context(), roleID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

type adminIntegrationRequest struct {
	Provider      string      `json:"provider"`
	Scope         string      `json:"scope"`
	Name          string      `json:"name"`
	Status        string      `json:"status"`
	IsActive      *bool       `json:"is_active"`
	Subdomain     string      `json:"subdomain"`
	ClientID      string      `json:"client_id"`
	ClientSecret  string      `json:"client_secret"`
	AccessToken   string      `json:"access_token"`
	RefreshToken  string      `json:"refresh_token"`
	RedirectURI   string      `json:"redirect_uri"`
	WebhookSecret string      `json:"webhook_secret"`
	Config        []byte      `json:"config"`
	Accounts      []uuid.UUID `json:"accounts"`
}

func integrationResponse(instance *domain.IntegrationInstance) fiber.Map {
	if instance == nil {
		return fiber.Map{}
	}
	return fiber.Map{
		"id":                 instance.ID,
		"provider":           instance.Provider,
		"scope":              instance.Scope,
		"name":               instance.Name,
		"status":             instance.Status,
		"is_active":          instance.IsActive,
		"subdomain":          instance.Subdomain,
		"client_id":          instance.ClientID,
		"redirect_uri":       instance.RedirectURI,
		"config":             instance.Config,
		"last_sync_at":       instance.LastSyncAt,
		"created_at":         instance.CreatedAt,
		"updated_at":         instance.UpdatedAt,
		"has_client_secret":  instance.ClientSecret != "",
		"has_access_token":   instance.AccessToken != "",
		"has_refresh_token":  instance.RefreshToken != "",
		"has_webhook_secret": instance.WebhookSecret != "",
		"accounts":           instance.Accounts,
	}
}

func (s *Server) reloadKommoManager(ctx context.Context) {
	if s.kommoManager == nil {
		return
	}
	if err := s.kommoManager.Reload(ctx); err != nil {
		log.Printf("[API] Failed to reload Kommo manager: %v", err)
	}
	s.kommoSync = s.kommoManager.Primary()
}

func (s *Server) handleAdminListIntegrations(c *fiber.Ctx) error {
	instances, err := s.repos.Integration.List(c.Context(), c.Query("provider"))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	items := make([]fiber.Map, 0, len(instances))
	for _, instance := range instances {
		items = append(items, integrationResponse(instance))
	}
	return c.JSON(fiber.Map{"success": true, "integrations": items})
}

func (s *Server) handleAdminCreateIntegration(c *fiber.Ctx) error {
	var req adminIntegrationRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = domain.IntegrationProviderKommo
	}
	if strings.TrimSpace(req.Scope) == "" {
		req.Scope = domain.IntegrationScopeMultiAccount
	}
	if strings.TrimSpace(req.Status) == "" {
		req.Status = domain.IntegrationStatusActive
	}
	if strings.TrimSpace(req.Name) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Name is required"})
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	configData := req.Config
	if len(configData) == 0 {
		configData = []byte(`{}`)
	}
	instance := &domain.IntegrationInstance{
		Provider:      strings.TrimSpace(req.Provider),
		Scope:         strings.TrimSpace(req.Scope),
		Name:          strings.TrimSpace(req.Name),
		Status:        strings.TrimSpace(req.Status),
		IsActive:      active,
		Subdomain:     strings.TrimSpace(req.Subdomain),
		ClientID:      strings.TrimSpace(req.ClientID),
		ClientSecret:  strings.TrimSpace(req.ClientSecret),
		AccessToken:   strings.TrimSpace(req.AccessToken),
		RefreshToken:  strings.TrimSpace(req.RefreshToken),
		RedirectURI:   strings.TrimSpace(req.RedirectURI),
		WebhookSecret: strings.TrimSpace(req.WebhookSecret),
		Config:        configData,
	}
	if err := s.repos.Integration.Create(c.Context(), instance); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	for _, accountID := range req.Accounts {
		_ = s.repos.Integration.AssignAccount(c.Context(), instance.ID, accountID, true)
		if instance.Provider == domain.IntegrationProviderKommo {
			_, _ = s.repos.DB().Exec(c.Context(), `UPDATE accounts SET kommo_enabled = TRUE WHERE id = $1`, accountID)
		}
	}
	s.reloadKommoManager(c.Context())
	created, _ := s.repos.Integration.GetByID(c.Context(), instance.ID)
	return c.Status(201).JSON(fiber.Map{"success": true, "integration": integrationResponse(created)})
}

func (s *Server) handleAdminUpdateIntegration(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	instance, err := s.repos.Integration.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Integration not found"})
	}
	var req adminIntegrationRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if strings.TrimSpace(req.Scope) != "" {
		instance.Scope = strings.TrimSpace(req.Scope)
	}
	if strings.TrimSpace(req.Name) != "" {
		instance.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.Status) != "" {
		instance.Status = strings.TrimSpace(req.Status)
	}
	if req.IsActive != nil {
		instance.IsActive = *req.IsActive
	}
	if strings.TrimSpace(req.Subdomain) != "" {
		instance.Subdomain = strings.TrimSpace(req.Subdomain)
	}
	if strings.TrimSpace(req.ClientID) != "" {
		instance.ClientID = strings.TrimSpace(req.ClientID)
	}
	if strings.TrimSpace(req.ClientSecret) != "" {
		instance.ClientSecret = strings.TrimSpace(req.ClientSecret)
	}
	if strings.TrimSpace(req.AccessToken) != "" {
		instance.AccessToken = strings.TrimSpace(req.AccessToken)
	}
	if strings.TrimSpace(req.RefreshToken) != "" {
		instance.RefreshToken = strings.TrimSpace(req.RefreshToken)
	}
	if strings.TrimSpace(req.RedirectURI) != "" {
		instance.RedirectURI = strings.TrimSpace(req.RedirectURI)
	}
	if strings.TrimSpace(req.WebhookSecret) != "" {
		instance.WebhookSecret = strings.TrimSpace(req.WebhookSecret)
	}
	if len(req.Config) > 0 {
		instance.Config = req.Config
	}
	if err := s.repos.Integration.Update(c.Context(), instance); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.reloadKommoManager(c.Context())
	updated, _ := s.repos.Integration.GetByID(c.Context(), id)
	return c.JSON(fiber.Map{"success": true, "integration": integrationResponse(updated)})
}

func (s *Server) handleAdminDeleteIntegration(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	if err := s.repos.Integration.Delete(c.Context(), id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	s.reloadKommoManager(c.Context())
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAdminAssignIntegrationAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	var req struct {
		AccountID uuid.UUID `json:"account_id"`
		Enabled   *bool     `json:"enabled"`
	}
	if err := c.BodyParser(&req); err != nil || req.AccountID == uuid.Nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	instance, err := s.repos.Integration.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Integration not found"})
	}
	if err := s.repos.Integration.AssignAccount(c.Context(), id, req.AccountID, enabled); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance.Provider == domain.IntegrationProviderKommo {
		_, _ = s.repos.DB().Exec(c.Context(), `UPDATE accounts SET kommo_enabled = $1 WHERE id = $2`, enabled, req.AccountID)
	}
	s.reloadKommoManager(c.Context())
	updated, _ := s.repos.Integration.GetByID(c.Context(), id)
	return c.JSON(fiber.Map{"success": true, "integration": integrationResponse(updated)})
}

func (s *Server) handleAdminRemoveIntegrationAccount(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	accountID, err := uuid.Parse(c.Params("account_id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid account ID"})
	}
	instance, _ := s.repos.Integration.GetByID(c.Context(), id)
	if err := s.repos.Integration.RemoveAccount(c.Context(), id, accountID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance != nil && instance.Provider == domain.IntegrationProviderKommo {
		_, _ = s.repos.DB().Exec(c.Context(), `UPDATE accounts SET kommo_enabled = FALSE WHERE id = $1`, accountID)
	}
	s.reloadKommoManager(c.Context())
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleAdminReloadIntegrations(c *fiber.Ctx) error {
	s.reloadKommoManager(c.Context())
	if s.kommoManager == nil {
		return c.JSON(fiber.Map{"success": true, "runtime": fiber.Map{"running_instances": 0}})
	}
	return c.JSON(fiber.Map{"success": true, "runtime": s.kommoManager.RuntimeStatus()})
}

func (s *Server) handleAdminIntegrationMonitor(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	instance, err := s.repos.Integration.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Integration not found"})
	}

	if svc := s.kommoSyncForInstance(id); svc != nil && svc.Monitor != nil {
		return c.JSON(fiber.Map{"success": true, "monitor": svc.Monitor.GetData()})
	}

	monitor := kommo.NewSyncMonitorForInstance(s.repos.DB(), &id)
	defer monitor.Stop()
	return c.JSON(fiber.Map{"success": true, "monitor": monitor.GetData()})
}

func (s *Server) handleAdminIntegrationHealth(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	instance, err := s.repos.Integration.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Integration not found"})
	}

	svc := s.kommoSyncForInstance(id)
	outbox, err := s.integrationOutboxSummary(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}

	health := fiber.Map{
		"integration":           integrationResponse(instance),
		"runtime_running":       svc != nil,
		"manager":               fiber.Map{"available": s.kommoManager != nil},
		"webhook_configured":    instance.WebhookSecret != "",
		"webhook_url":           redactedKommoWebhookURL(s.cfg.PublicURL, instance.WebhookSecret),
		"public_url_configured": strings.TrimSpace(s.cfg.PublicURL) != "",
		"assigned_accounts":     instance.Accounts,
		"assigned_count":        len(instance.Accounts),
		"outbox":                outbox["totals"],
	}
	if svc != nil {
		health["worker"] = svc.GetStatus()
		health["events_poller"] = svc.GetEventsPollerStatus()
	}
	if s.kommoManager != nil {
		health["manager"] = s.kommoManager.RuntimeStatus()
	}

	return c.JSON(fiber.Map{"success": true, "health": health})
}

func (s *Server) handleAdminIntegrationOutbox(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	instance, err := s.repos.Integration.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if instance == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Integration not found"})
	}
	outbox, err := s.integrationOutboxSummary(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "outbox": outbox})
}

func (s *Server) handleAdminForceIntegrationPoll(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid integration ID"})
	}
	svc := s.kommoSyncForInstance(id)
	if svc == nil {
		return c.Status(409).JSON(fiber.Map{"success": false, "error": "Integration runtime is not active"})
	}
	events, leads := svc.ForceEventsPoll()
	return c.JSON(fiber.Map{"success": true, "events": events, "leads": leads})
}

func (s *Server) kommoSyncForInstance(id uuid.UUID) *kommo.SyncService {
	if s.kommoManager != nil {
		if svc := s.kommoManager.ForInstance(id); svc != nil {
			return svc
		}
	}
	if s.kommoSync != nil && s.kommoSync.InstanceID != nil && *s.kommoSync.InstanceID == id {
		return s.kommoSync
	}
	return nil
}

func redactedKommoWebhookURL(publicURL, secret string) string {
	if strings.TrimSpace(secret) == "" {
		return ""
	}
	redacted := "****"
	if len(secret) > 4 {
		redacted += secret[len(secret)-4:]
	}
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" {
		return "/api/kommo/webhook/" + redacted
	}
	return base + "/api/kommo/webhook/" + redacted
}

func (s *Server) integrationOutboxSummary(ctx context.Context, instanceID uuid.UUID) (fiber.Map, error) {
	rows, err := s.repos.DB().Query(ctx, `
		SELECT k.operation,
		       COALESCE(k.account_id::text, ''),
		       COALESCE(a.name, ''),
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE k.processing_started_at IS NULL) AS pending,
		       COUNT(*) FILTER (WHERE k.processing_started_at IS NOT NULL AND COALESCE(k.last_error, '') = '') AS processing,
		       COUNT(*) FILTER (WHERE COALESCE(k.last_error, '') <> '') AS errored,
		       COUNT(*) FILTER (WHERE k.attempts > 0) AS retried,
		       COALESCE(MIN(k.enqueued_at)::text, ''),
		       COALESCE(MAX(k.last_error), '')
		FROM kommo_push_outbox k
		LEFT JOIN accounts a ON a.id = k.account_id
		WHERE k.integration_instance_id IS NOT DISTINCT FROM $1
		GROUP BY k.operation, k.account_id, a.name
		ORDER BY total DESC, k.operation ASC
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	totals := fiber.Map{"total": int64(0), "pending": int64(0), "processing": int64(0), "errored": int64(0), "retried": int64(0)}
	for rows.Next() {
		var operation, accountID, accountName, oldest, lastError string
		var total, pending, processing, errored, retried int64
		if err := rows.Scan(&operation, &accountID, &accountName, &total, &pending, &processing, &errored, &retried, &oldest, &lastError); err != nil {
			return nil, err
		}
		items = append(items, fiber.Map{
			"operation":    operation,
			"account_id":   accountID,
			"account_name": accountName,
			"total":        total,
			"pending":      pending,
			"processing":   processing,
			"errored":      errored,
			"retried":      retried,
			"oldest_at":    oldest,
			"last_error":   lastError,
		})
		totals["total"] = totals["total"].(int64) + total
		totals["pending"] = totals["pending"].(int64) + pending
		totals["processing"] = totals["processing"].(int64) + processing
		totals["errored"] = totals["errored"].(int64) + errored
		totals["retried"] = totals["retried"].(int64) + retried
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return fiber.Map{"items": items, "totals": totals}, nil
}

// ─────────────────────────────────────────────────────────
// Health Check Endpoints
// ─────────────────────────────────────────────────────────

// handleHealthCheck is a deep health probe that checks all dependencies.
// Returns 200 with "healthy" if all systems are operational,
// 503 with "degraded" if some dependencies are down.
func (s *Server) handleGetVersion(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"version":   s.version,
		"changelog": s.changelog,
	})
}

func (s *Server) handleHealthCheck(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	status := "healthy"
	httpStatus := 200

	// Check PostgreSQL
	dbOk := true
	if err := s.repos.DB().Ping(ctx); err != nil {
		dbOk = false
		status = "degraded"
		httpStatus = 503
	}

	// Check Redis
	redisOk := true
	if s.cache != nil {
		if err := s.cache.Ping(ctx); err != nil {
			redisOk = false
			status = "degraded"
			httpStatus = 503
		}
	} else {
		redisOk = false
	}

	// WhatsApp devices
	devicesConnected := 0
	devicesTotal := 0
	if s.pool != nil {
		devicesConnected = s.pool.GetConnectedCount()
		devicesTotal = s.pool.GetTotalCount()
	}

	// WebSocket clients
	wsClients := 0
	if s.hub != nil {
		wsClients = s.hub.GetClientCount()
	}

	// Uptime
	var uptime string
	if s.pool != nil {
		uptime = time.Since(s.pool.GetStartTime()).Truncate(time.Second).String()
	}

	return c.Status(httpStatus).JSON(fiber.Map{
		"status": status,
		"time":   time.Now(),
		"uptime": uptime,
		"dependencies": fiber.Map{
			"postgres": fiber.Map{"ok": dbOk},
			"redis":    fiber.Map{"ok": redisOk},
		},
		"whatsapp": fiber.Map{
			"devices_connected": devicesConnected,
			"devices_total":     devicesTotal,
		},
		"websocket": fiber.Map{
			"clients": wsClients,
		},
	})
}

// handleDeviceHealth returns detailed per-device health metrics.
// Protected endpoint — requires PermDevices.
func (s *Server) handleDeviceHealth(c *fiber.Ctx) error {
	if s.pool == nil {
		return c.JSON(fiber.Map{"success": true, "devices": []interface{}{}})
	}
	summaries := s.pool.GetHealthSummary()
	return c.JSON(fiber.Map{"success": true, "devices": summaries})
}
