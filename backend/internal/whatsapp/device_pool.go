package whatsapp

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver for whatsmeow sqlstore
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/repository"
	"github.com/naperu/clarin/internal/storage"
	"github.com/naperu/clarin/internal/ws"
	"github.com/naperu/clarin/pkg/cache"
	"github.com/naperu/clarin/pkg/config"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

const (
	avatarExistingRefreshTTL = 7 * 24 * time.Hour
	avatarMissingRefreshTTL  = 24 * time.Hour
	avatarFetchTimeout       = 12 * time.Second
)

// DeviceHealthMetrics tracks health-related counters per device
type DeviceHealthMetrics struct {
	DisconnectCount  int64     `json:"disconnect_count"`
	ReconnectCount   int64     `json:"reconnect_count"`
	SendErrorCount   int64     `json:"send_error_count"`
	SendSuccessCount int64     `json:"send_success_count"`
	LastConnected    time.Time `json:"last_connected"`
	LastDisconnected time.Time `json:"last_disconnected"`
	LastSendError    time.Time `json:"last_send_error,omitempty"`
	LastSendSuccess  time.Time `json:"last_send_success,omitempty"`
	UptimeStart      time.Time `json:"uptime_start"`
}

// DeviceHealthSummary is returned by the health endpoint
type DeviceHealthSummary struct {
	ID        uuid.UUID           `json:"id"`
	JID       string              `json:"jid"`
	Status    string              `json:"status"`
	Connected bool                `json:"connected"`
	Metrics   DeviceHealthMetrics `json:"metrics"`
}

// DeviceInstance represents a single WhatsApp connection
type DeviceInstance struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	Client          *whatsmeow.Client
	JID             string
	Status          string
	QRCode          string
	ReceiveMessages bool // when false, incoming messages are silently dropped
	Metrics         DeviceHealthMetrics
	mu              sync.RWMutex
	// reconnect control
	reconnecting  bool
	stopReconnect chan struct{}
}

// DevicePool manages multiple WhatsApp connections
// onDemandSyncTarget tracks a pending on-demand history sync request
type onDemandSyncTarget struct {
	AccountID uuid.UUID
	DeviceID  uuid.UUID
	ChatID    uuid.UUID
	ChatJID   string
}

type DevicePool struct {
	devices            map[uuid.UUID]*DeviceInstance
	store              *sqlstore.Container
	repos              *repository.Repositories
	hub                *ws.Hub
	cfg                *config.Config
	storage            *storage.Storage
	cache              *cache.Cache
	mu                 sync.RWMutex
	startTime          time.Time
	onDemandSyncTarget *onDemandSyncTarget // currently active on-demand sync target for auto-chaining
}

// NewDevicePool creates a new device pool
func NewDevicePool(cfg *config.Config, repos *repository.Repositories, hub *ws.Hub) (*DevicePool, error) {
	// Initialize whatsmeow store with PostgreSQL
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New(context.Background(), "pgx", cfg.DatabaseURL, dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize whatsmeow store: %w", err)
	}

	// Warm up LID mapping cache for @lid -> phone resolution
	if container.LIDMap != nil {
		if err := container.LIDMap.FillCache(context.Background()); err != nil {
			log.Printf("[DevicePool] Warning: failed to fill LID cache: %v", err)
		} else {
			log.Printf("[DevicePool] LID mapping cache loaded")
		}
	}

	return &DevicePool{
		devices:   make(map[uuid.UUID]*DeviceInstance),
		store:     container,
		repos:     repos,
		hub:       hub,
		cfg:       cfg,
		startTime: time.Now(),
	}, nil
}

// SetStorage sets the storage instance for media handling
func (p *DevicePool) SetStorage(s *storage.Storage) {
	p.storage = s
}

// SetCache sets the Redis cache for invalidation on data changes
func (p *DevicePool) SetCache(c *cache.Cache) {
	p.cache = c
}

func (p *DevicePool) invalidateChatCaches(accountID, chatID uuid.UUID) {
	if p.cache == nil {
		return
	}
	_ = p.cache.DelPattern(context.Background(), "chats:"+accountID.String()+":*")
	_ = p.cache.DelPattern(context.Background(), "messages:"+accountID.String()+":"+chatID.String()+":*")
}

func (p *DevicePool) invalidateAccountMessageCaches(accountID uuid.UUID) {
	if p.cache == nil {
		return
	}
	_ = p.cache.DelPattern(context.Background(), "messages:"+accountID.String()+":*")
}

// SetReceiveMessages updates the in-memory receive flag for a connected device
func (p *DevicePool) SetReceiveMessages(deviceID uuid.UUID, value bool) {
	p.mu.RLock()
	instance, ok := p.devices[deviceID]
	p.mu.RUnlock()
	if ok {
		instance.mu.Lock()
		instance.ReceiveMessages = value
		instance.mu.Unlock()
	}
}

// LoadExistingDevices loads all existing devices and connects them
func (p *DevicePool) LoadExistingDevices(ctx context.Context) error {
	devices, err := p.repos.Device.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	for _, device := range devices {
		if device.JID != nil && *device.JID != "" {
			// Device was previously connected, try to reconnect
			go func(d *domain.Device) {
				if err := p.ConnectDevice(ctx, d.ID); err != nil {
					log.Printf("[DevicePool] Failed to reconnect device %s: %v", d.ID, err)
				}
			}(device)
		}
	}

	return nil
}

// CreateDevice creates a new device entry and returns it
func (p *DevicePool) CreateDevice(ctx context.Context, accountID uuid.UUID, name string) (*domain.Device, error) {
	status := domain.DeviceStatusDisconnected
	device := &domain.Device{
		AccountID: accountID,
		Name:      &name,
		Status:    &status,
	}

	if err := p.repos.Device.Create(ctx, device); err != nil {
		return nil, fmt.Errorf("failed to create device: %w", err)
	}

	return device, nil
}

// ConnectDevice initializes and connects a WhatsApp client for a device
func (p *DevicePool) ConnectDevice(ctx context.Context, deviceID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if already connected
	if instance, exists := p.devices[deviceID]; exists {
		if instance.Client != nil && instance.Client.IsConnected() {
			return nil // Already connected
		}
	}

	// Get device from database
	device, err := p.repos.Device.GetByID(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}
	if device == nil {
		return fmt.Errorf("device not found: %s", deviceID)
	}

	// Update status to connecting
	_ = p.repos.Device.UpdateStatus(ctx, deviceID, domain.DeviceStatusConnecting)
	p.hub.BroadcastDeviceStatus(device.AccountID, deviceID, domain.DeviceStatusConnecting, "")

	// Get or create whatsmeow device store
	var waDevice *store.Device
	if device.JID != nil && *device.JID != "" {
		// Try to get existing device by JID
		jid, _ := types.ParseJID(*device.JID)
		waDevice, err = p.store.GetDevice(ctx, jid)
		if err != nil {
			waDevice = nil // Create new if not found
		}
	}

	if waDevice == nil {
		waDevice = p.store.NewDevice()
	}

	// Configure device properties
	store.DeviceProps.Os = proto.String("Clarin CRM")
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	// Enable on-demand history sync support — required for BuildHistorySyncRequest/SendPeerMessage
	store.DeviceProps.HistorySyncConfig.OnDemandReady = proto.Bool(true)
	store.DeviceProps.HistorySyncConfig.CompleteOnDemandReady = proto.Bool(true)

	// Create client
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(waDevice, clientLog)
	client.EnableAutoReconnect = true
	client.AutoTrustIdentity = true

	// Configure media HTTP client with SOCKS5 proxy to bypass CDN throttling.
	// WhatsApp CDN rate-limits/blocks large uploads (>100KB) from certain IPs.
	// Route media uploads through Cloudflare WARP SOCKS5 proxy for a clean IP.
	mediaTransport := &http.Transport{
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}
	// If MEDIA_SOCKS5_PROXY is set (e.g. socks5://172.23.0.1:40001), route CDN uploads through it
	if proxyURL := os.Getenv("MEDIA_SOCKS5_PROXY"); proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			mediaTransport.Proxy = http.ProxyURL(parsed)
			log.Printf("[DevicePool] Media uploads will use SOCKS5 proxy: %s", proxyURL)
		} else {
			log.Printf("[DevicePool] WARNING: Invalid MEDIA_SOCKS5_PROXY URL: %s (%v)", proxyURL, err)
		}
	}
	client.SetMediaHTTPClient(&http.Client{
		Timeout:   120 * time.Second,
		Transport: mediaTransport,
	})

	// Create device instance
	instance := &DeviceInstance{
		ID:              deviceID,
		AccountID:       device.AccountID,
		Client:          client,
		Status:          domain.DeviceStatusConnecting,
		ReceiveMessages: device.ReceiveMessages,
	}
	p.devices[deviceID] = instance

	// Add event handler
	client.AddEventHandler(func(evt interface{}) {
		p.handleEvent(ctx, instance, evt)
	})

	// Connect
	if client.Store.ID == nil {
		// New device, need QR code
		qrChan, _ := client.GetQRChannel(ctx)
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}

		// Handle QR codes in goroutine
		go p.handleQRChannel(ctx, instance, qrChan)
	} else {
		// Existing device, just connect
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	return nil
}

// handleQRChannel handles QR code events
func (p *DevicePool) handleQRChannel(ctx context.Context, instance *DeviceInstance, qrChan <-chan whatsmeow.QRChannelItem) {
	for evt := range qrChan {
		switch evt.Event {
		case "code":
			// Generate QR code image as base64
			qr, err := qrcode.Encode(evt.Code, qrcode.Medium, 256)
			if err != nil {
				log.Printf("[QR] Failed to generate QR code: %v", err)
				continue
			}
			qrBase64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qr)

			instance.mu.Lock()
			instance.QRCode = qrBase64
			instance.Status = domain.DeviceStatusConnecting
			instance.mu.Unlock()

			// Update database
			_ = p.repos.Device.UpdateQRCode(ctx, instance.ID, qrBase64)

			// Broadcast to frontend
			p.hub.BroadcastQRCode(instance.AccountID, instance.ID, qrBase64)
			log.Printf("[QR] New QR code generated for device %s", instance.ID)

		case "success":
			log.Printf("[QR] Login successful for device %s", instance.ID)

		case "timeout":
			log.Printf("[QR] QR code timeout for device %s", instance.ID)
			instance.mu.Lock()
			instance.Status = domain.DeviceStatusDisconnected
			instance.QRCode = ""
			instance.mu.Unlock()
			_ = p.repos.Device.UpdateStatus(ctx, instance.ID, domain.DeviceStatusDisconnected)
			p.hub.BroadcastDeviceStatus(instance.AccountID, instance.ID, domain.DeviceStatusDisconnected, "")
		}
	}
}

// handleEvent processes WhatsApp events
func (p *DevicePool) handleEvent(ctx context.Context, instance *DeviceInstance, rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Connected:
		p.handleConnected(ctx, instance)

	case *events.LoggedOut:
		p.handleLoggedOut(ctx, instance, evt)

	case *events.Disconnected:
		p.handleDisconnected(ctx, instance)

	case *events.Message:
		p.handleMessage(ctx, instance, evt)

	case *events.Receipt:
		p.handleReceipt(ctx, instance, evt)

	case *events.ChatPresence:
		p.handleChatPresence(ctx, instance, evt)

	case *events.Presence:
		p.handlePresence(ctx, instance, evt)

	case *events.PushName:
		p.handlePushName(ctx, instance, evt)

	case *events.Contact:
		p.handleContactEvent(ctx, instance, evt)

	case *events.HistorySync:
		log.Printf("[HistorySync] EVENT RECEIVED: type=%v, conversations=%d, device=%s",
			evt.Data.GetSyncType(), len(evt.Data.Conversations), instance.ID)
		p.handleHistorySync(ctx, instance, evt)
	}
}

// handleConnected processes connection events
func (p *DevicePool) handleConnected(ctx context.Context, instance *DeviceInstance) {
	if instance.Client.Store.ID == nil {
		return
	}

	jid := instance.Client.Store.ID.String()
	phone := strings.Split(instance.Client.Store.ID.User, "@")[0]

	instance.mu.Lock()
	instance.JID = jid
	instance.Status = domain.DeviceStatusConnected
	instance.QRCode = ""
	instance.Metrics.LastConnected = time.Now()
	instance.Metrics.UptimeStart = time.Now()
	// Stop any active reconnect supervisor since we're connected now
	if instance.reconnecting {
		instance.reconnecting = false
		if instance.stopReconnect != nil {
			close(instance.stopReconnect)
			instance.stopReconnect = nil
		}
	}
	instance.mu.Unlock()

	// Update database
	_ = p.repos.Device.UpdateJID(ctx, instance.ID, jid, phone)

	// Broadcast status
	p.hub.BroadcastDeviceStatus(instance.AccountID, instance.ID, domain.DeviceStatusConnected, "")

	log.Printf("[Device %s] Connected as %s", instance.ID, jid)

	// Sync contacts in background after connection
	go p.syncContacts(context.Background(), instance)
}

// handleLoggedOut processes logout events
func (p *DevicePool) handleLoggedOut(ctx context.Context, instance *DeviceInstance, evt *events.LoggedOut) {
	instance.mu.Lock()
	instance.Status = domain.DeviceStatusLoggedOut
	instance.JID = ""
	// Stop reconnect supervisor — explicit logout means don't retry
	if instance.reconnecting {
		instance.reconnecting = false
		if instance.stopReconnect != nil {
			close(instance.stopReconnect)
			instance.stopReconnect = nil
		}
	}
	instance.mu.Unlock()

	_ = p.repos.Device.UpdateStatus(ctx, instance.ID, domain.DeviceStatusLoggedOut)
	p.hub.BroadcastDeviceStatus(instance.AccountID, instance.ID, domain.DeviceStatusLoggedOut, "")

	log.Printf("[Device %s] Logged out: %s", instance.ID, evt.Reason)
}

// handleDisconnected processes disconnection events
func (p *DevicePool) handleDisconnected(ctx context.Context, instance *DeviceInstance) {
	instance.mu.Lock()
	instance.Status = domain.DeviceStatusDisconnected
	instance.Metrics.DisconnectCount++
	instance.Metrics.LastDisconnected = time.Now()
	alreadyReconnecting := instance.reconnecting
	instance.mu.Unlock()

	_ = p.repos.Device.UpdateStatus(ctx, instance.ID, domain.DeviceStatusDisconnected)
	p.hub.BroadcastDeviceStatus(instance.AccountID, instance.ID, domain.DeviceStatusDisconnected, "")

	log.Printf("[Device %s] Disconnected (total disconnects: %d)", instance.ID, instance.Metrics.DisconnectCount)

	// Launch reconnect supervisor if not already running.
	// whatsmeow has EnableAutoReconnect, but if it gives up, this supervisor
	// provides an additional safety net with exponential backoff.
	if !alreadyReconnecting {
		go p.reconnectSupervisor(instance)
	}
}

// reconnectSupervisor attempts to reconnect a device with exponential backoff.
// It stops on: successful connect, explicit logout/delete, or pool shutdown.
func (p *DevicePool) reconnectSupervisor(instance *DeviceInstance) {
	instance.mu.Lock()
	// Guard: if already reconnecting or logged out, don't start
	if instance.reconnecting || instance.Status == domain.DeviceStatusLoggedOut {
		instance.mu.Unlock()
		return
	}
	instance.reconnecting = true
	instance.stopReconnect = make(chan struct{})
	stopCh := instance.stopReconnect
	instance.mu.Unlock()

	const (
		initialBackoff = 5 * time.Second
		maxBackoff     = 5 * time.Minute
		maxAttempts    = 50 // ~2.5 hours at max backoff
	)

	backoff := initialBackoff

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Wait with backoff, respecting stop signal
		select {
		case <-stopCh:
			log.Printf("[Reconnect %s] Supervisor stopped (attempt %d)", instance.ID, attempt)
			return
		case <-time.After(backoff):
		}

		// Check if device still exists and isn't logged out
		instance.mu.RLock()
		if instance.Status == domain.DeviceStatusConnected {
			// whatsmeow reconnected on its own
			instance.mu.RUnlock()
			log.Printf("[Reconnect %s] Already connected, supervisor exiting", instance.ID)
			return
		}
		if instance.Status == domain.DeviceStatusLoggedOut {
			instance.mu.RUnlock()
			log.Printf("[Reconnect %s] Device logged out, supervisor exiting", instance.ID)
			return
		}
		instance.mu.RUnlock()

		// Check if device was deleted from the pool
		p.mu.RLock()
		_, exists := p.devices[instance.ID]
		p.mu.RUnlock()
		if !exists {
			log.Printf("[Reconnect %s] Device removed from pool, supervisor exiting", instance.ID)
			return
		}

		log.Printf("[Reconnect %s] Attempt %d/%d (backoff: %v)", instance.ID, attempt, maxAttempts, backoff)

		instance.mu.Lock()
		instance.Metrics.ReconnectCount++
		instance.mu.Unlock()

		err := p.ConnectDevice(context.Background(), instance.ID)
		if err == nil {
			log.Printf("[Reconnect %s] ✅ Reconnected successfully on attempt %d", instance.ID, attempt)
			return
		}

		log.Printf("[Reconnect %s] Attempt %d failed: %v", instance.ID, attempt, err)

		// Exponential backoff: 5s, 10s, 20s, 40s, 80s, 160s, 300s (capped)
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	log.Printf("[Reconnect %s] ❌ Gave up after %d attempts", instance.ID, maxAttempts)
	instance.mu.Lock()
	instance.reconnecting = false
	instance.stopReconnect = nil
	instance.mu.Unlock()
}

// messageContentResult holds extracted message content fields
type messageContentResult struct {
	Body          string
	MessageType   string
	MediaURL      *string
	MediaMimetype *string
	MediaFilename *string
	MediaSize     *int64
	MediaAssetID  *uuid.UUID
	Latitude      *float64
	Longitude     *float64
	ContactName   *string
	ContactPhone  *string
	ContactVCard  *string
	IsViewOnce    bool
}

type storedMediaResult struct {
	URL        string
	AssetID    *uuid.UUID
	SizeBytes  int64
	ObjectKey  string
	Hash       string
	Deduped    bool
	MediaType  string
	Filename   string
	ContentTyp string
}

// extractMessageContent extracts body, type, and media info from a waE2E.Message.
// Used by both handleMessage (live) and handleHistorySync (historical).
// If instance is nil, media download is skipped (metadata-only extraction).
func (p *DevicePool) extractMessageContent(ctx context.Context, instance *DeviceInstance, waMsg *waE2E.Message, chatJID, msgID string) messageContentResult {
	r := messageContentResult{MessageType: domain.MessageTypeText}

	if waMsg == nil {
		return r
	}

	if waMsg.GetConversation() != "" {
		r.Body = waMsg.GetConversation()
	} else if waMsg.GetExtendedTextMessage() != nil {
		r.Body = waMsg.GetExtendedTextMessage().GetText()
	} else if imgMsg := waMsg.GetImageMessage(); imgMsg != nil {
		r.Body = imgMsg.GetCaption()
		r.MessageType = domain.MessageTypeImage
		r.MediaMimetype = strPtr(imgMsg.GetMimetype())
		if p.storage != nil && instance != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, imgMsg, chatJID, msgID, imgMsg.GetMimetype(), ".jpg")
			if err == nil {
				r.MediaURL = &stored.URL
				r.MediaAssetID = stored.AssetID
				r.MediaSize = &stored.SizeBytes
			}
		}
	} else if vidMsg := waMsg.GetVideoMessage(); vidMsg != nil {
		r.Body = vidMsg.GetCaption()
		r.MessageType = domain.MessageTypeVideo
		r.MediaMimetype = strPtr(vidMsg.GetMimetype())
		if p.storage != nil && instance != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, vidMsg, chatJID, msgID, vidMsg.GetMimetype(), ".mp4")
			if err == nil {
				r.MediaURL = &stored.URL
				r.MediaAssetID = stored.AssetID
				r.MediaSize = &stored.SizeBytes
			}
		}
	} else if audMsg := waMsg.GetAudioMessage(); audMsg != nil {
		r.MessageType = domain.MessageTypeAudio
		r.MediaMimetype = strPtr(audMsg.GetMimetype())
		ext := ".ogg"
		if p.storage != nil && instance != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, audMsg, chatJID, msgID, audMsg.GetMimetype(), ext)
			if err == nil {
				r.MediaURL = &stored.URL
				r.MediaAssetID = stored.AssetID
				r.MediaSize = &stored.SizeBytes
			}
		}
	} else if docMsg := waMsg.GetDocumentMessage(); docMsg != nil {
		r.Body = docMsg.GetFileName()
		r.MessageType = domain.MessageTypeDocument
		r.MediaMimetype = strPtr(docMsg.GetMimetype())
		r.MediaFilename = strPtr(docMsg.GetFileName())
		if docMsg.FileLength != nil {
			size := int64(*docMsg.FileLength)
			r.MediaSize = &size
		}
		ext := filepath.Ext(docMsg.GetFileName())
		if ext == "" {
			ext = ".bin"
		}
		if p.storage != nil && instance != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, docMsg, chatJID, msgID, docMsg.GetMimetype(), ext)
			if err == nil {
				r.MediaURL = &stored.URL
				r.MediaAssetID = stored.AssetID
				r.MediaSize = &stored.SizeBytes
			}
		}
	} else if stickerMsg := waMsg.GetStickerMessage(); stickerMsg != nil {
		r.MessageType = domain.MessageTypeSticker
		r.MediaMimetype = strPtr(stickerMsg.GetMimetype())
		if p.storage != nil && instance != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, stickerMsg, chatJID, msgID, stickerMsg.GetMimetype(), ".webp")
			if err == nil {
				r.MediaURL = &stored.URL
				r.MediaAssetID = stored.AssetID
				r.MediaSize = &stored.SizeBytes
			}
		}
	} else if locMsg := waMsg.GetLocationMessage(); locMsg != nil {
		r.MessageType = domain.MessageTypeLocation
		r.Body = locMsg.GetName()
		if r.Body == "" {
			r.Body = locMsg.GetAddress()
		}
		lat := locMsg.GetDegreesLatitude()
		lng := locMsg.GetDegreesLongitude()
		r.Latitude = &lat
		r.Longitude = &lng
	} else if contactMsg := waMsg.GetContactMessage(); contactMsg != nil {
		r.MessageType = domain.MessageTypeContact
		r.Body = contactMsg.GetDisplayName()
		r.ContactName = strPtr(contactMsg.GetDisplayName())
		r.ContactVCard = strPtr(contactMsg.GetVcard())
		if phone := extractPhoneFromVCard(contactMsg.GetVcard()); phone != "" {
			r.ContactPhone = strPtr(phone)
		}
	}

	// Handle view-once messages
	if viewOnce := waMsg.GetViewOnceMessage(); viewOnce != nil {
		r.IsViewOnce = true
		if inner := viewOnce.GetMessage(); inner != nil {
			if imgMsg := inner.GetImageMessage(); imgMsg != nil {
				r.MessageType = domain.MessageTypeImage
				r.Body = imgMsg.GetCaption()
				r.MediaMimetype = strPtr(imgMsg.GetMimetype())
				if p.storage != nil && instance != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, imgMsg, chatJID, msgID, imgMsg.GetMimetype(), ".jpg")
					if err == nil {
						r.MediaURL = &stored.URL
						r.MediaAssetID = stored.AssetID
						r.MediaSize = &stored.SizeBytes
					}
				}
			} else if vidMsg := inner.GetVideoMessage(); vidMsg != nil {
				r.MessageType = domain.MessageTypeVideo
				r.Body = vidMsg.GetCaption()
				r.MediaMimetype = strPtr(vidMsg.GetMimetype())
				if p.storage != nil && instance != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, vidMsg, chatJID, msgID, vidMsg.GetMimetype(), ".mp4")
					if err == nil {
						r.MediaURL = &stored.URL
						r.MediaAssetID = stored.AssetID
						r.MediaSize = &stored.SizeBytes
					}
				}
			}
		}
	}
	if viewOnce := waMsg.GetViewOnceMessageV2(); viewOnce != nil {
		r.IsViewOnce = true
		if inner := viewOnce.GetMessage(); inner != nil {
			if imgMsg := inner.GetImageMessage(); imgMsg != nil {
				r.MessageType = domain.MessageTypeImage
				r.Body = imgMsg.GetCaption()
				r.MediaMimetype = strPtr(imgMsg.GetMimetype())
				if p.storage != nil && instance != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, imgMsg, chatJID, msgID, imgMsg.GetMimetype(), ".jpg")
					if err == nil {
						r.MediaURL = &stored.URL
						r.MediaAssetID = stored.AssetID
						r.MediaSize = &stored.SizeBytes
					}
				}
			} else if vidMsg := inner.GetVideoMessage(); vidMsg != nil {
				r.MessageType = domain.MessageTypeVideo
				r.Body = vidMsg.GetCaption()
				r.MediaMimetype = strPtr(vidMsg.GetMimetype())
				if p.storage != nil && instance != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, vidMsg, chatJID, msgID, vidMsg.GetMimetype(), ".mp4")
					if err == nil {
						r.MediaURL = &stored.URL
						r.MediaAssetID = stored.AssetID
						r.MediaSize = &stored.SizeBytes
					}
				}
			}
		}
	}

	return r
}

// handleMessage processes incoming messages
func (p *DevicePool) handleMessage(ctx context.Context, instance *DeviceInstance, evt *events.Message) {
	// Skip status broadcasts
	if evt.Info.Chat.Server == "broadcast" {
		return
	}

	// Skip group messages - only process 1-to-1 chats
	if evt.Info.Chat.Server == "g.us" {
		return
	}

	// Skip newsletter/channel messages
	if evt.Info.Chat.Server == "newsletter" {
		return
	}

	// If receive_messages is disabled, silently drop incoming messages (but allow sent messages through)
	if !evt.Info.IsFromMe && !instance.ReceiveMessages {
		return
	}

	// Handle reactions separately — they are NOT regular messages
	if reactionMsg := evt.Message.GetReactionMessage(); reactionMsg != nil {
		p.handleReaction(ctx, instance, evt, reactionMsg)
		return
	}

	// Handle poll creation messages
	if pollMsg := evt.Message.GetPollCreationMessage(); pollMsg != nil {
		p.handlePollCreation(ctx, instance, evt, pollMsg)
		return
	}

	// Handle poll vote updates
	if pollUpdate := evt.Message.GetPollUpdateMessage(); pollUpdate != nil {
		p.handlePollUpdate(ctx, instance, evt, pollUpdate)
		return
	}

	// Handle protocol messages (revoke, edit, etc.)
	if protocolMsg := evt.Message.GetProtocolMessage(); protocolMsg != nil {
		if protocolMsg.GetType() == waE2E.ProtocolMessage_REVOKE {
			revokedID := protocolMsg.GetKey().GetID()
			chatJID := evt.Info.Chat.ToNonAD().String()
			if evt.Info.Chat.Server == types.HiddenUserServer {
				if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
					chatJID = pnJID.User + "@s.whatsapp.net"
				}
			}

			// Mark message as revoked in DB
			if err := p.repos.Message.MarkAsRevoked(ctx, instance.AccountID, chatJID, revokedID); err != nil {
				log.Printf("[Revoke] Failed to mark message %s as revoked: %v", revokedID, err)
			}
			if chat, err := p.repos.Chat.FindByJID(ctx, instance.AccountID, chatJID); err == nil && chat != nil {
				p.invalidateChatCaches(instance.AccountID, chat.ID)
			} else {
				p.invalidateAccountMessageCaches(instance.AccountID)
			}

			// Broadcast revocation to frontend
			p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageRevoked, map[string]interface{}{
				"chat_jid":   chatJID,
				"message_id": revokedID,
				"is_from_me": evt.Info.IsFromMe,
			})

			log.Printf("[Revoke] Message %s revoked in chat %s", revokedID, chatJID)
			return
		}

		if protocolMsg.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
			editedMsgID := protocolMsg.GetKey().GetID()
			chatJID := evt.Info.Chat.ToNonAD().String()
			if evt.Info.Chat.Server == types.HiddenUserServer {
				if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
					chatJID = pnJID.User + "@s.whatsapp.net"
				}
			}

			newBody := ""
			if editedMsg := protocolMsg.GetEditedMessage(); editedMsg != nil {
				if editedMsg.GetConversation() != "" {
					newBody = editedMsg.GetConversation()
				} else if editedMsg.GetExtendedTextMessage() != nil {
					newBody = editedMsg.GetExtendedTextMessage().GetText()
				}
			}

			if err := p.repos.Message.UpdateBody(ctx, instance.AccountID, chatJID, editedMsgID, newBody); err != nil {
				log.Printf("[Edit] Failed to update message %s: %v", editedMsgID, err)
			}
			if chat, err := p.repos.Chat.FindByJID(ctx, instance.AccountID, chatJID); err == nil && chat != nil {
				p.invalidateChatCaches(instance.AccountID, chat.ID)
			} else {
				p.invalidateAccountMessageCaches(instance.AccountID)
			}

			p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageEdited, map[string]interface{}{
				"chat_jid":   chatJID,
				"message_id": editedMsgID,
				"new_body":   newBody,
				"is_from_me": evt.Info.IsFromMe,
			})

			log.Printf("[Edit] Message %s edited in chat %s", editedMsgID, chatJID)
			return
		}

		// Other protocol messages (ephemeral settings, etc.) — skip
		return
	}

	// Get message content
	body := ""
	msgType := domain.MessageTypeText
	var mediaURL, mediaMimetype, mediaFilename *string
	var mediaSize *int64
	var mediaAssetID *uuid.UUID

	if evt.Message.GetConversation() != "" {
		body = evt.Message.GetConversation()
	} else if evt.Message.GetExtendedTextMessage() != nil {
		body = evt.Message.GetExtendedTextMessage().GetText()
	} else if imgMsg := evt.Message.GetImageMessage(); imgMsg != nil {
		body = imgMsg.GetCaption()
		msgType = domain.MessageTypeImage
		mediaMimetype = strPtr(imgMsg.GetMimetype())
		// Download and store the image
		if p.storage != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, imgMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, imgMsg.GetMimetype(), ".jpg")
			if err == nil {
				mediaURL = &stored.URL
				mediaAssetID = stored.AssetID
				mediaSize = &stored.SizeBytes
			}
		}
	} else if vidMsg := evt.Message.GetVideoMessage(); vidMsg != nil {
		body = vidMsg.GetCaption()
		msgType = domain.MessageTypeVideo
		mediaMimetype = strPtr(vidMsg.GetMimetype())
		if p.storage != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, vidMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, vidMsg.GetMimetype(), ".mp4")
			if err == nil {
				mediaURL = &stored.URL
				mediaAssetID = stored.AssetID
				mediaSize = &stored.SizeBytes
			}
		}
	} else if audMsg := evt.Message.GetAudioMessage(); audMsg != nil {
		msgType = domain.MessageTypeAudio
		mediaMimetype = strPtr(audMsg.GetMimetype())
		ext := ".ogg"
		if audMsg.GetPTT() {
			ext = ".ogg"
		}
		if p.storage != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, audMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, audMsg.GetMimetype(), ext)
			if err == nil {
				mediaURL = &stored.URL
				mediaAssetID = stored.AssetID
				mediaSize = &stored.SizeBytes
			}
		}
	} else if docMsg := evt.Message.GetDocumentMessage(); docMsg != nil {
		body = docMsg.GetFileName()
		msgType = domain.MessageTypeDocument
		mediaMimetype = strPtr(docMsg.GetMimetype())
		mediaFilename = strPtr(docMsg.GetFileName())
		if docMsg.FileLength != nil {
			size := int64(*docMsg.FileLength)
			mediaSize = &size
		}
		ext := filepath.Ext(docMsg.GetFileName())
		if ext == "" {
			ext = ".bin"
		}
		if p.storage != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, docMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, docMsg.GetMimetype(), ext)
			if err == nil {
				mediaURL = &stored.URL
				mediaAssetID = stored.AssetID
				mediaSize = &stored.SizeBytes
			}
		}
	} else if stickerMsg := evt.Message.GetStickerMessage(); stickerMsg != nil {
		msgType = domain.MessageTypeSticker
		mediaMimetype = strPtr(stickerMsg.GetMimetype())
		if p.storage != nil {
			stored, err := p.downloadAndStoreMedia(ctx, instance, stickerMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, stickerMsg.GetMimetype(), ".webp")
			if err == nil {
				mediaURL = &stored.URL
				mediaAssetID = stored.AssetID
				mediaSize = &stored.SizeBytes
			}
		}
	} else if locMsg := evt.Message.GetLocationMessage(); locMsg != nil {
		msgType = domain.MessageTypeLocation
		body = locMsg.GetName()
		if body == "" {
			body = locMsg.GetAddress()
		}
	} else if contactMsg := evt.Message.GetContactMessage(); contactMsg != nil {
		msgType = domain.MessageTypeContact
		body = contactMsg.GetDisplayName()
	}

	// Get sender info - normalize JIDs to remove device suffix for consistent chat matching
	// ToNonAD() converts JIDs like "user:5@s.whatsapp.net" to "user@s.whatsapp.net"
	chatJID := evt.Info.Chat.ToNonAD().String()
	senderJID := evt.Info.Sender.ToNonAD().String()
	senderName := evt.Info.PushName
	isFromMe := evt.Info.IsFromMe

	// Resolve phone number BEFORE creating chat — so we use a consistent JID
	phone := evt.Info.Sender.ToNonAD().User
	if evt.Info.Chat.Server == types.HiddenUserServer {
		// Chat JID is @lid — try to resolve to @s.whatsapp.net for consistent chat identity
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			chatJID = pnJID.User + "@s.whatsapp.net"
			phone = pnJID.User
			log.Printf("[Message] Resolved chat LID %s -> %s", evt.Info.Chat.ToNonAD().String(), chatJID)
		}
	}
	if !isFromMe && evt.Info.Sender.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Sender.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			phone = pnJID.User
		} else if evt.Info.Chat.Server == types.DefaultUserServer {
			phone = evt.Info.Chat.ToNonAD().User
		}
	}

	// Get or create chat - only use sender name for incoming messages (not our own)
	chatName := ""
	if !isFromMe {
		chatName = senderName
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, chatJID, chatName)
	if err != nil {
		log.Printf("[Message] Failed to get/create chat: %v", err)
		return
	}

	// Extract quoted/reply context from incoming message
	var quotedMessageID, quotedBody, quotedSender *string
	// Check ContextInfo from various message types
	var contextInfo *waE2E.ContextInfo
	if ext := evt.Message.GetExtendedTextMessage(); ext != nil && ext.GetContextInfo() != nil {
		contextInfo = ext.GetContextInfo()
	} else if img := evt.Message.GetImageMessage(); img != nil && img.GetContextInfo() != nil {
		contextInfo = img.GetContextInfo()
	} else if vid := evt.Message.GetVideoMessage(); vid != nil && vid.GetContextInfo() != nil {
		contextInfo = vid.GetContextInfo()
	} else if aud := evt.Message.GetAudioMessage(); aud != nil && aud.GetContextInfo() != nil {
		contextInfo = aud.GetContextInfo()
	} else if doc := evt.Message.GetDocumentMessage(); doc != nil && doc.GetContextInfo() != nil {
		contextInfo = doc.GetContextInfo()
	} else if stk := evt.Message.GetStickerMessage(); stk != nil && stk.GetContextInfo() != nil {
		contextInfo = stk.GetContextInfo()
	}
	if contextInfo != nil && contextInfo.GetStanzaID() != "" {
		quotedMessageID = strPtr(contextInfo.GetStanzaID())
		quotedSender = strPtr(contextInfo.GetParticipant())
		// Extract quoted message body
		if qm := contextInfo.GetQuotedMessage(); qm != nil {
			if qm.GetConversation() != "" {
				quotedBody = strPtr(qm.GetConversation())
			} else if qm.GetExtendedTextMessage() != nil {
				quotedBody = strPtr(qm.GetExtendedTextMessage().GetText())
			} else if qm.GetImageMessage() != nil && qm.GetImageMessage().GetCaption() != "" {
				quotedBody = strPtr(qm.GetImageMessage().GetCaption())
			} else if qm.GetVideoMessage() != nil && qm.GetVideoMessage().GetCaption() != "" {
				quotedBody = strPtr(qm.GetVideoMessage().GetCaption())
			} else if qm.GetDocumentMessage() != nil {
				quotedBody = strPtr(qm.GetDocumentMessage().GetFileName())
			} else {
				quotedBody = strPtr("[media]")
			}
		}
	}

	// Create message
	msg := &domain.Message{
		AccountID:     instance.AccountID,
		DeviceID:      &instance.ID,
		ChatID:        chat.ID,
		MessageID:     evt.Info.ID,
		FromJID:       strPtr(senderJID),
		FromName:      strPtr(senderName),
		Body:          strPtr(body),
		MessageType:   strPtr(msgType),
		MediaURL:      mediaURL,
		MediaMimetype: mediaMimetype,
		MediaFilename: mediaFilename,
		MediaSize:     mediaSize,
		MediaAssetID:  mediaAssetID,
		IsFromMe:      isFromMe,
		Status: strPtr(func() string {
			if isFromMe {
				return "sent"
			}
			return "received"
		}()),
		Timestamp:       evt.Info.Timestamp,
		QuotedMessageID: quotedMessageID,
		QuotedBody:      quotedBody,
		QuotedSender:    quotedSender,
	}

	// Populate location data
	if locMsg := evt.Message.GetLocationMessage(); locMsg != nil {
		lat := locMsg.GetDegreesLatitude()
		lng := locMsg.GetDegreesLongitude()
		msg.Latitude = &lat
		msg.Longitude = &lng
	}

	// Populate contact card data
	if contactMsg := evt.Message.GetContactMessage(); contactMsg != nil {
		msg.ContactName = strPtr(contactMsg.GetDisplayName())
		msg.ContactVCard = strPtr(contactMsg.GetVcard())
		// Extract phone from vCard
		vcard := contactMsg.GetVcard()
		if phone := extractPhoneFromVCard(vcard); phone != "" {
			msg.ContactPhone = strPtr(phone)
		}
	}

	// Handle view-once messages (wrapped in ViewOnceMessage or ViewOnceMessageV2)
	if viewOnce := evt.Message.GetViewOnceMessage(); viewOnce != nil {
		msg.IsViewOnce = true
		// Process the inner message for media
		if inner := viewOnce.GetMessage(); inner != nil {
			if imgMsg := inner.GetImageMessage(); imgMsg != nil {
				msg.MessageType = strPtr(domain.MessageTypeImage)
				msg.Body = strPtr(imgMsg.GetCaption())
				mediaMimetype = strPtr(imgMsg.GetMimetype())
				msg.MediaMimetype = mediaMimetype
				if p.storage != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, imgMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, imgMsg.GetMimetype(), ".jpg")
					if err == nil {
						msg.MediaURL = &stored.URL
						msg.MediaAssetID = stored.AssetID
						msg.MediaSize = &stored.SizeBytes
					}
				}
			} else if vidMsg := inner.GetVideoMessage(); vidMsg != nil {
				msg.MessageType = strPtr(domain.MessageTypeVideo)
				msg.Body = strPtr(vidMsg.GetCaption())
				mediaMimetype = strPtr(vidMsg.GetMimetype())
				msg.MediaMimetype = mediaMimetype
				if p.storage != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, vidMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, vidMsg.GetMimetype(), ".mp4")
					if err == nil {
						msg.MediaURL = &stored.URL
						msg.MediaAssetID = stored.AssetID
						msg.MediaSize = &stored.SizeBytes
					}
				}
			}
		}
	}
	if viewOnce := evt.Message.GetViewOnceMessageV2(); viewOnce != nil {
		msg.IsViewOnce = true
		if inner := viewOnce.GetMessage(); inner != nil {
			if imgMsg := inner.GetImageMessage(); imgMsg != nil {
				msg.MessageType = strPtr(domain.MessageTypeImage)
				msg.Body = strPtr(imgMsg.GetCaption())
				mediaMimetype = strPtr(imgMsg.GetMimetype())
				msg.MediaMimetype = mediaMimetype
				if p.storage != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, imgMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, imgMsg.GetMimetype(), ".jpg")
					if err == nil {
						msg.MediaURL = &stored.URL
						msg.MediaAssetID = stored.AssetID
						msg.MediaSize = &stored.SizeBytes
					}
				}
			} else if vidMsg := inner.GetVideoMessage(); vidMsg != nil {
				msg.MessageType = strPtr(domain.MessageTypeVideo)
				msg.Body = strPtr(vidMsg.GetCaption())
				mediaMimetype = strPtr(vidMsg.GetMimetype())
				msg.MediaMimetype = mediaMimetype
				if p.storage != nil {
					stored, err := p.downloadAndStoreMedia(ctx, instance, vidMsg, evt.Info.Chat.ToNonAD().String(), evt.Info.ID, vidMsg.GetMimetype(), ".mp4")
					if err == nil {
						msg.MediaURL = &stored.URL
						msg.MediaAssetID = stored.AssetID
						msg.MediaSize = &stored.SizeBytes
					}
				}
			}
		}
	}

	if err := p.repos.Message.Create(ctx, msg); err != nil {
		log.Printf("[Message] Failed to save message: %v", err)
		return
	}

	// Update chat last message
	_ = p.repos.Chat.UpdateLastMessage(ctx, chat.ID, body, evt.Info.Timestamp, !isFromMe)

	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Use chatJID for contact in 1-to-1 chats so the LEFT JOIN in queries matches
	contactJID := senderJID
	if !isFromMe {
		contactJID = chatJID
	}
	contact, _ := p.repos.Contact.GetOrCreate(ctx, instance.AccountID, &instance.ID, contactJID, phone, senderName, evt.Info.PushName, false)
	// Sync contact name/fields to linked lead (if lead exists)
	if contact != nil {
		_ = p.repos.Contact.SyncToLead(ctx, contact)
	}

	// Refresh avatar opportunistically for active contacts only, bounded by TTL.
	if contact != nil && !isFromMe {
		avatarJID := evt.Info.Chat.ToNonAD()
		// If chat JID is @lid, resolve to @s.whatsapp.net for GetProfilePictureInfo
		if avatarJID.Server == types.HiddenUserServer {
			if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, avatarJID); err == nil && !pnJID.IsEmpty() {
				avatarJID = pnJID
			}
		}
		if ttl := avatarRefreshTTL(contact); ttl > 0 {
			claimed, err := p.repos.Contact.ClaimAvatarRefresh(ctx, instance.AccountID, contactJID, ttl)
			if err != nil {
				log.Printf("[Avatar] Failed to claim avatar refresh for %s: %v", contactJID, err)
			} else if claimed {
				go p.fetchAndStoreAvatar(instance, contactJID, avatarJID)
			}
		}
	}

	// Auto-create lead if not exists and is incoming message
	if !isFromMe {
		lead, _ := p.repos.Lead.GetByJID(ctx, instance.AccountID, contactJID)
		if lead == nil {
			newLead := &domain.Lead{
				AccountID: instance.AccountID,
				JID:       contactJID,
				Name:      strPtr(senderName),
				Phone:     strPtr(phone),
				Status:    strPtr(domain.LeadStatusNew),
				Source:    strPtr("whatsapp"),
			}
			if contact != nil {
				newLead.ContactID = &contact.ID
			}
			if pipelineID, stageID, err := p.repos.Pipeline.ResolveIncomingLeadDestination(ctx, instance.AccountID); err == nil {
				newLead.PipelineID = pipelineID
				newLead.StageID = stageID
			}
			if err := p.repos.Lead.Create(ctx, newLead); err == nil {
				log.Printf("[Lead] Auto-created lead for %s (pipeline=%v, stage=%v, contact=%v)", contactJID, newLead.PipelineID, newLead.StageID, newLead.ContactID)
				// Invalidate leads cache so the API returns fresh data
				if p.cache != nil {
					_ = p.cache.Del(context.Background(), "leads:"+instance.AccountID.String())
				}
				// Notify frontend via WebSocket
				p.hub.BroadcastToAccount(instance.AccountID, ws.EventLeadUpdate, map[string]interface{}{
					"action": "created",
				})
			} else {
				log.Printf("[Lead] Failed to auto-create lead for %s: %v", contactJID, err)
			}
		}
	}

	// Broadcast to frontend
	p.hub.BroadcastNewMessage(instance.AccountID, map[string]interface{}{
		"chat_id":      chat.ID.String(),
		"message":      msg,
		"chat_jid":     chatJID,
		"sender_name":  senderName,
		"is_from_me":   isFromMe,
		"unread_count": chat.UnreadCount + 1,
	})

	log.Printf("[Message] %s -> %s: %s", senderName, chatJID, truncate(body, 50))
}

// fetchAndStoreAvatar fetches a WhatsApp profile picture and stores it
func (p *DevicePool) fetchAndStoreAvatar(instance *DeviceInstance, contactJID string, jid types.JID) {
	if p.storage == nil || instance.Client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), avatarFetchTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Avatar] Panic recovering avatar for %s: %v", jid.String(), r)
		}
	}()

	picInfo, err := instance.Client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{})
	if err != nil || picInfo == nil {
		log.Printf("[Avatar] No profile picture for %s: %v", jid.String(), err)
		return
	}

	// Download the avatar image
	resp, err := http.Get(picInfo.URL)
	if err != nil {
		log.Printf("[Avatar] Failed to download avatar for %s: %v", jid.String(), err)
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		log.Printf("[Avatar] Failed to read avatar for %s: %v", jid.String(), err)
		return
	}

	// Upload to MinIO using a content hash so unchanged pictures keep the same URL.
	hash := sha256.Sum256(data)
	filename := fmt.Sprintf("%s_%x.jpg", jid.ToNonAD().User, hash[:8])
	_, err = p.storage.UploadFile(ctx, instance.AccountID, "avatars", filename, data, "image/jpeg")
	if err != nil {
		log.Printf("[Avatar] Failed to store avatar for %s: %v", jid.String(), err)
		return
	}

	// Store proxy URL in contact
	proxyURL := fmt.Sprintf("/api/media/file/%s/avatars/%s", instance.AccountID.String(), filename)
	changed, err := p.repos.Contact.UpdateAvatarURL(ctx, instance.AccountID, contactJID, proxyURL)
	if err != nil {
		log.Printf("[Avatar] Failed to update contact avatar: %v", err)
		return
	}
	if changed {
		if p.cache != nil {
			_ = p.cache.DelPattern(context.Background(), "chats:"+instance.AccountID.String()+":*")
			_ = p.cache.DelPattern(context.Background(), "contacts:"+instance.AccountID.String()+":*")
		}
		if p.hub != nil {
			p.hub.BroadcastToAccount(instance.AccountID, ws.EventContactUpdate, map[string]interface{}{
				"action":     "avatar_updated",
				"jid":        contactJID,
				"avatar_url": proxyURL,
			})
			p.hub.BroadcastToAccount(instance.AccountID, ws.EventChatUpdate, map[string]interface{}{
				"jid":        contactJID,
				"avatar_url": proxyURL,
			})
		}
	}

	log.Printf("[Avatar] Stored avatar for %s", jid.String())
}

func avatarRefreshTTL(contact *domain.Contact) time.Duration {
	if contact == nil || contact.IsGroup || strings.HasSuffix(contact.JID, "@g.us") || strings.HasSuffix(contact.JID, "@newsletter") || strings.HasSuffix(contact.JID, "@broadcast") {
		return 0
	}
	if contact.AvatarURL == nil || strings.TrimSpace(*contact.AvatarURL) == "" {
		return avatarMissingRefreshTTL
	}
	return avatarExistingRefreshTTL
}

// downloadAndStoreMedia downloads media from WhatsApp and stores one canonical object per account/content hash.
func (p *DevicePool) downloadAndStoreMedia(ctx context.Context, instance *DeviceInstance, msg whatsmeow.DownloadableMessage, chatJID, msgID, mimetype, extension string) (*storedMediaResult, error) {
	if p.storage == nil {
		return nil, fmt.Errorf("storage not configured")
	}

	// Download media
	data, err := instance.Client.Download(ctx, msg)
	if err != nil {
		log.Printf("[Media] Failed to download: %v", err)
		return nil, err
	}
	hashBytes := sha256.Sum256(data)
	contentHash := fmt.Sprintf("%x", hashBytes[:])
	mediaType := strings.TrimPrefix(strings.ToLower(extension), ".")
	if mediaType == "" {
		mediaType = "bin"
	}
	filename := contentHash + extension
	objectKey := fmt.Sprintf("%s/media/%s/%s", instance.AccountID.String(), mediaType, filename)
	proxyURL := "/api/media/file/" + objectKey
	sizeBytes := int64(len(data))

	if p.repos != nil && p.repos.MediaAsset != nil {
		if existing, err := p.repos.MediaAsset.GetByHash(ctx, instance.AccountID, contentHash); err == nil && existing != nil {
			existingURL := "/api/media/file/" + existing.ObjectKey
			log.Printf("[Media] Reused %s for message %s (%d bytes)", existingURL, msgID, existing.SizeBytes)
			return &storedMediaResult{
				URL:        existingURL,
				AssetID:    &existing.ID,
				SizeBytes:  existing.SizeBytes,
				ObjectKey:  existing.ObjectKey,
				Hash:       existing.ContentHash,
				Deduped:    true,
				MediaType:  existing.MediaType,
				Filename:   existing.Filename,
				ContentTyp: existing.ContentType,
			}, nil
		}
	}

	if p.repos != nil && p.storage != nil {
		if account, accErr := p.repos.Account.GetByID(ctx, instance.AccountID); accErr == nil && account != nil && account.StorageLimitBytes > 0 {
			used, _, usageErr := p.storage.UsagePrefix(ctx, instance.AccountID.String()+"/")
			if usageErr == nil && used+sizeBytes > account.StorageLimitBytes {
				log.Printf("[Media] Storage quota reached for account %s; skipping media %s (%d bytes)", instance.AccountID, msgID, len(data))
				return nil, fmt.Errorf("storage limit reached")
			}
		}
	}

	if _, err = p.storage.UploadObject(ctx, objectKey, data, mimetype); err != nil {
		log.Printf("[Media] Failed to upload: %v", err)
		return nil, err
	}

	var assetID *uuid.UUID
	if p.repos != nil && p.repos.MediaAsset != nil {
		asset, assetErr := p.repos.MediaAsset.Upsert(ctx, repository.MediaAssetUpsert{
			AccountID:   instance.AccountID,
			ContentHash: contentHash,
			ObjectKey:   objectKey,
			MediaType:   mediaType,
			ContentType: mimetype,
			Filename:    filename,
			SizeBytes:   sizeBytes,
		})
		if assetErr != nil {
			log.Printf("[Media] Failed to upsert media asset: %v", assetErr)
		} else if asset != nil {
			assetID = &asset.ID
			objectKey = asset.ObjectKey
			proxyURL = "/api/media/file/" + objectKey
		}
	}

	_, _ = p.repos.DB().Exec(ctx, `
		INSERT INTO storage_objects (account_id, object_key, media_type, content_type, filename, size_bytes, source, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'chat', 'active', NOW())
		ON CONFLICT (account_id, object_key) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes, content_type = EXCLUDED.content_type, status = 'active', updated_at = NOW()
	`, instance.AccountID, objectKey, mediaType, mimetype, filename, sizeBytes)

	log.Printf("[Media] Stored %s (%d bytes)", proxyURL, len(data))
	return &storedMediaResult{
		URL:        proxyURL,
		AssetID:    assetID,
		SizeBytes:  sizeBytes,
		ObjectKey:  objectKey,
		Hash:       contentHash,
		Deduped:    false,
		MediaType:  mediaType,
		Filename:   filename,
		ContentTyp: mimetype,
	}, nil
}

// handleReceipt processes delivery/read receipts
func (p *DevicePool) handleReceipt(ctx context.Context, instance *DeviceInstance, evt *events.Receipt) {
	// Determine status from receipt type
	var status string
	switch evt.Type {
	case types.ReceiptTypeRead:
		status = "read"
	case types.ReceiptTypePlayed:
		status = "read" // played media = read
	case types.ReceiptTypeDelivered:
		status = "delivered"
	case types.ReceiptTypeReadSelf, types.ReceiptTypePlayedSelf:
		return // ignore self-read/played receipts
	case types.ReceiptTypeSender:
		return // confirmation for our other devices — ignore
	case types.ReceiptTypeRetry:
		return // decryption retry — not a delivery status
	case types.ReceiptTypeServerError:
		return // server error — not a delivery status
	case types.ReceiptTypeInactive:
		return // inactive notification — not a delivery status
	default:
		// Unknown receipt type — log and ignore to avoid wrongly setting "delivered"
		log.Printf("[Receipt] Ignoring unknown receipt type=%q chat=%s msgs=%v", evt.Type, evt.Chat.ToNonAD().String(), evt.MessageIDs)
		return
	}

	chatJID := evt.Chat.ToNonAD().String()

	// Resolve LID to phone JID for consistent matching
	if evt.Chat.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			chatJID = pnJID.User + "@s.whatsapp.net"
		} else {
			log.Printf("[Receipt] WARNING: Could not resolve LID %s to phone JID, receipt may not match", evt.Chat.ToNonAD().String())
		}
	}

	// Also try resolving via Sender for receipts where Chat might differ
	if evt.MessageSource.Sender.Server == types.HiddenUserServer && evt.Chat.Server != types.HiddenUserServer {
		// Chat already has phone JID, no need to resolve
	} else if evt.MessageSource.Sender.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.MessageSource.Sender.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			resolvedJID := pnJID.User + "@s.whatsapp.net"
			if chatJID != resolvedJID && evt.Chat.Server == types.HiddenUserServer {
				log.Printf("[Receipt] Resolved sender LID %s -> %s (chat was %s)", evt.MessageSource.Sender.ToNonAD().String(), resolvedJID, chatJID)
				chatJID = resolvedJID
			}
		}
	}

	log.Printf("[Receipt] type=%s status=%s chat=%s msgs=%v", evt.Type, status, chatJID, evt.MessageIDs)

	// Persist receipt status in database (only upgrade: sent→delivered→read)
	if len(evt.MessageIDs) > 0 {
		for _, msgID := range evt.MessageIDs {
			if err := p.repos.Message.UpdateStatusUpgrade(ctx, instance.AccountID, chatJID, msgID, status); err != nil {
				log.Printf("[Receipt] Failed to update status for %s: %v", msgID, err)
			}
		}
	}

	// Broadcast receipt status to frontend
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageStatus, map[string]interface{}{
		"message_ids": evt.MessageIDs,
		"chat_jid":    chatJID,
		"status":      status,
		"timestamp":   evt.Timestamp,
	})
}

// handleChatPresence processes typing/recording indicators from contacts
func (p *DevicePool) handleChatPresence(ctx context.Context, instance *DeviceInstance, evt *events.ChatPresence) {
	jid := evt.MessageSource.Chat.ToNonAD().String()
	senderJID := evt.MessageSource.Sender.ToNonAD().String()

	// Resolve LID to phone JID
	if evt.MessageSource.Chat.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.MessageSource.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			jid = pnJID.User + "@s.whatsapp.net"
		}
	}

	media := "text"
	if evt.Media == types.ChatPresenceMediaAudio {
		media = "audio"
	}

	p.hub.BroadcastToAccount(instance.AccountID, ws.EventTyping, map[string]interface{}{
		"jid":       jid,
		"sender":    senderJID,
		"composing": evt.State == types.ChatPresenceComposing,
		"media":     media,
	})
}

// handlePresence processes presence updates
func (p *DevicePool) handlePresence(ctx context.Context, instance *DeviceInstance, evt *events.Presence) {
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventPresence, map[string]interface{}{
		"jid":          evt.From.ToNonAD().String(),
		"available":    evt.Unavailable == false,
		"last_seen_at": evt.LastSeen,
	})
}

// handleContactEvent processes contact update events from WhatsApp
// events.Contact comes from app state sync and has Action (*waSyncAction.ContactAction)
// with GetFullName() and GetFirstName() methods (no BusinessName/PushName here)
func (p *DevicePool) handleContactEvent(ctx context.Context, instance *DeviceInstance, evt *events.Contact) {
	jid := evt.JID.ToNonAD().String()
	phone := evt.JID.User

	// Resolve LID
	if evt.JID.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.JID.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			jid = pnJID.User + "@s.whatsapp.net"
			phone = pnJID.User
		}
	}

	name := evt.Action.GetFullName()
	if name == "" {
		name = evt.Action.GetFirstName()
	}

	contact, err := p.repos.Contact.GetOrCreate(ctx, instance.AccountID, &instance.ID, jid, phone, name, "", false)
	if err != nil {
		log.Printf("[ContactEvent] Failed to upsert contact %s: %v", jid, err)
		return
	}

	// Update per-device name
	cdn := &domain.ContactDeviceName{
		ContactID: contact.ID,
		DeviceID:  instance.ID,
		Name:      strPtr(name),
	}
	_ = p.repos.ContactDeviceName.Upsert(ctx, cdn)

	log.Printf("[ContactEvent] Updated contact %s: %s", jid, name)
}

// handlePushName processes push name updates
func (p *DevicePool) handlePushName(ctx context.Context, instance *DeviceInstance, evt *events.PushName) {
	jid := evt.JID.ToNonAD().String()
	log.Printf("[PushName] %s -> %s", jid, evt.NewPushName)

	// Update contact push name
	contact, _ := p.repos.Contact.GetByJID(ctx, instance.AccountID, jid)
	if contact != nil {
		// Update the per-device name
		cdn := &domain.ContactDeviceName{
			ContactID: contact.ID,
			DeviceID:  instance.ID,
			PushName:  strPtr(evt.NewPushName),
		}
		_ = p.repos.ContactDeviceName.Upsert(ctx, cdn)
	}
}

// handleHistorySync processes history sync events
func (p *DevicePool) handleHistorySync(ctx context.Context, instance *DeviceInstance, evt *events.HistorySync) {
	// If receive_messages is disabled, skip importing historical messages
	if !instance.ReceiveMessages {
		log.Printf("[HistorySync] Skipping history sync for device %s (receive_messages disabled)", instance.ID)
		return
	}

	totalConversations := len(evt.Data.Conversations)
	syncType := evt.Data.GetSyncType().String()
	log.Printf("[HistorySync] Received %d conversations for device %s (type=%s)", totalConversations, instance.ID, syncType)

	totalSaved := 0
	totalDuplicates := 0
	totalGroups := 0
	totalLIDFail := 0
	totalEmpty := 0
	totalProtocol := 0
	totalParseErr := 0

	for convIdx, conv := range evt.Data.Conversations {
		convJID := conv.GetID()
		if convJID == "" {
			continue
		}

		// Parse the JID — skip groups, broadcasts, newsletters
		parsed, err := types.ParseJID(convJID)
		if err != nil {
			log.Printf("[HistorySync] Failed to parse JID %s: %v", convJID, err)
			continue
		}
		if parsed.Server != types.DefaultUserServer && parsed.Server != types.HiddenUserServer {
			totalGroups += len(conv.Messages)
			continue // skip groups (g.us), broadcast, newsletter
		}

		// Resolve LID to phone JID for consistent chat identity
		chatJID := parsed.ToNonAD().String()
		phone := parsed.User
		if parsed.Server == types.HiddenUserServer {
			if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, parsed.ToNonAD()); err == nil && !pnJID.IsEmpty() {
				chatJID = pnJID.User + "@s.whatsapp.net"
				phone = pnJID.User
			} else {
				totalLIDFail += len(conv.Messages)
				continue
			}
		}

		if len(conv.Messages) == 0 {
			continue
		}

		// Get or create chat
		chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, chatJID, "")
		if err != nil {
			log.Printf("[HistorySync] Failed to get/create chat for %s: %v", chatJID, err)
			continue
		}

		// Log details for small batches (ON_DEMAND, debug)
		if totalConversations <= 5 {
			log.Printf("[HistorySync] Conv[%d] rawJID=%s resolvedJID=%s chatID=%s msgs=%d",
				convIdx, convJID, chatJID, chat.ID, len(conv.Messages))
		}

		convSaved := 0
		var oldestTimestamp time.Time

		for _, histMsg := range conv.Messages {
			webMsg := histMsg.GetMessage()
			if webMsg == nil {
				continue
			}

			// Parse the web message into a standard events.Message
			parsedEvt, err := instance.Client.ParseWebMessage(parsed, webMsg)
			if err != nil {
				totalParseErr++
				continue
			}

			// Skip reactions, polls, protocol messages — only regular content
			if parsedEvt.Message == nil {
				totalProtocol++
				continue
			}
			if parsedEvt.Message.GetReactionMessage() != nil ||
				parsedEvt.Message.GetPollCreationMessage() != nil ||
				parsedEvt.Message.GetPollUpdateMessage() != nil ||
				parsedEvt.Message.GetProtocolMessage() != nil {
				totalProtocol++
				continue
			}

			// Extract message content — pass nil for instance to SKIP media downloads during history sync.
			// Media downloads block the event handler for too long with hundreds of conversations.
			// Messages will have type/mimetype metadata but no media URL.
			content := p.extractMessageContent(ctx, nil, parsedEvt.Message, chatJID, parsedEvt.Info.ID)

			// Skip empty text messages (media messages without URL are kept — they have type info)
			if content.Body == "" && content.MessageType == domain.MessageTypeText {
				totalEmpty++
				continue
			}

			senderJID := parsedEvt.Info.Sender.ToNonAD().String()
			if parsedEvt.Info.Sender.Server == types.HiddenUserServer {
				if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, parsedEvt.Info.Sender.ToNonAD()); err == nil && !pnJID.IsEmpty() {
					senderJID = pnJID.User + "@s.whatsapp.net"
				}
			}

			isFromMe := parsedEvt.Info.IsFromMe
			status := "received"
			if isFromMe {
				status = "sent"
			}

			msg := &domain.Message{
				AccountID:     instance.AccountID,
				DeviceID:      &instance.ID,
				ChatID:        chat.ID,
				MessageID:     parsedEvt.Info.ID,
				FromJID:       strPtr(senderJID),
				FromName:      strPtr(parsedEvt.Info.PushName),
				Body:          strPtr(content.Body),
				MessageType:   strPtr(content.MessageType),
				MediaURL:      content.MediaURL,
				MediaMimetype: content.MediaMimetype,
				MediaFilename: content.MediaFilename,
				MediaSize:     content.MediaSize,
				IsFromMe:      isFromMe,
				Status:        strPtr(status),
				Timestamp:     parsedEvt.Info.Timestamp,
				Latitude:      content.Latitude,
				Longitude:     content.Longitude,
				ContactName:   content.ContactName,
				ContactPhone:  content.ContactPhone,
				ContactVCard:  content.ContactVCard,
				IsViewOnce:    content.IsViewOnce,
			}

			if err := p.repos.Message.Create(ctx, msg); err != nil {
				totalDuplicates++
				// Debug: log details for small batches (ON_DEMAND, etc.)
				if totalConversations <= 5 {
					log.Printf("[HistorySync] SKIP msgID=%s ts=%s fromMe=%v type=%s err=%v",
						parsedEvt.Info.ID, parsedEvt.Info.Timestamp.Format(time.RFC3339), isFromMe, content.MessageType, err)
				}
				continue
			}

			convSaved++
			totalSaved++
			if totalConversations <= 5 {
				log.Printf("[HistorySync] SAVED msgID=%s ts=%s fromMe=%v type=%s",
					parsedEvt.Info.ID, parsedEvt.Info.Timestamp.Format(time.RFC3339), isFromMe, content.MessageType)
			}

			if oldestTimestamp.IsZero() || parsedEvt.Info.Timestamp.Before(oldestTimestamp) {
				oldestTimestamp = parsedEvt.Info.Timestamp
			}
		}

		// Update chat last message with the newest history message if chat is empty
		if convSaved > 0 {
			// Ensure contact exists
			p.repos.Contact.GetOrCreate(ctx, instance.AccountID, &instance.ID, chatJID, phone, "", "", false)

			log.Printf("[HistorySync] %s: saved %d messages", chatJID, convSaved)
		}
	}

	log.Printf("[HistorySync] Complete: saved=%d duplicates=%d groups=%d lidFail=%d empty=%d protocol=%d parseErr=%d conversations=%d",
		totalSaved, totalDuplicates, totalGroups, totalLIDFail, totalEmpty, totalProtocol, totalParseErr, totalConversations)

	// Notify frontend that history sync completed
	if totalSaved > 0 {
		if p.cache != nil {
			_ = p.cache.DelPattern(context.Background(), "chats:"+instance.AccountID.String()+":*")
		}
		p.invalidateAccountMessageCaches(instance.AccountID)

		p.hub.BroadcastToAccount(instance.AccountID, ws.EventHistorySyncComplete, map[string]interface{}{
			"device_id":      instance.ID.String(),
			"messages_saved": totalSaved,
			"duplicates":     totalDuplicates,
		})
	}

	// Auto-chain: if this was an ON_DEMAND response with saved messages, fire another request
	// Only process on the device that owns the sync target to avoid duplicates
	if syncType == "ON_DEMAND" {
		p.mu.RLock()
		target := p.onDemandSyncTarget
		p.mu.RUnlock()

		// Only act if this device is the target device (avoids duplicate event processing)
		if target != nil && target.DeviceID == instance.ID {
			if totalSaved > 0 {
				log.Printf("[HistorySync] Auto-chaining: requesting more messages for %s (saved %d in this batch)", target.ChatJID, totalSaved)
				// Small delay to avoid hammering the phone
				go func() {
					time.Sleep(3 * time.Second)
					if err := p.RequestHistorySync(context.Background(), target.AccountID, target.DeviceID, target.ChatID, target.ChatJID); err != nil {
						log.Printf("[HistorySync] Auto-chain failed: %v", err)
					}
				}()
			} else {
				// No more messages to fetch — clear the target
				p.mu.Lock()
				p.onDemandSyncTarget = nil
				p.mu.Unlock()
				log.Printf("[HistorySync] On-demand sync complete — no more older messages available")
			}
		} else if target != nil {
			log.Printf("[HistorySync] Ignoring ON_DEMAND event from device %s (target device is %s)", instance.ID, target.DeviceID)
		}
	}
}

// RequestHistorySync sends an on-demand history sync request for a specific chat.
// It finds the oldest message timestamp and requests messages before that point.
// deviceID specifies which device to use (must be the device that owns the chat).
func (p *DevicePool) RequestHistorySync(ctx context.Context, accountID uuid.UUID, deviceID uuid.UUID, chatID uuid.UUID, chatJID string) error {
	// Find the specific device for this chat
	p.mu.RLock()
	var instance *DeviceInstance
	for _, inst := range p.devices {
		if inst.ID == deviceID && inst.Client != nil && inst.Client.IsConnected() {
			instance = inst
			break
		}
	}
	p.mu.RUnlock()

	if instance == nil {
		return fmt.Errorf("device %s not connected", deviceID)
	}

	// Parse chat JID
	targetJID, err := types.ParseJID(chatJID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	// Find the oldest message in this chat to paginate before it
	oldestMsg, err := p.repos.Message.GetOldestByChatID(ctx, chatID)

	var msgInfo *types.MessageInfo
	if err == nil && oldestMsg != nil {
		// Build message info from oldest known message
		msgInfo = &types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     targetJID,
				IsFromMe: oldestMsg.IsFromMe,
			},
			ID:        oldestMsg.MessageID,
			Timestamp: oldestMsg.Timestamp,
		}
		if oldestMsg.IsFromMe {
			msgInfo.Sender = instance.Client.Store.ID.ToNonAD()
		} else {
			msgInfo.Sender = targetJID
		}
	}

	// Log exact details being sent
	if msgInfo != nil {
		log.Printf("[HistorySync] Building request: chat=%s, msgID=%s, isFromMe=%v, timestamp=%s, sender=%s",
			msgInfo.Chat.String(), msgInfo.ID, msgInfo.IsFromMe, msgInfo.Timestamp.Format(time.RFC3339), msgInfo.Sender.String())
	} else {
		log.Printf("[HistorySync] Building request with nil msgInfo (fetch most recent)")
	}

	// Build and send the history sync request (50 messages)
	histReq := instance.Client.BuildHistorySyncRequest(msgInfo, 50)
	resp, err := instance.Client.SendPeerMessage(ctx, histReq)
	if err != nil {
		return fmt.Errorf("failed to send history sync request: %w", err)
	}

	// Track this as the active on-demand sync target for auto-chaining
	p.mu.Lock()
	p.onDemandSyncTarget = &onDemandSyncTarget{
		AccountID: accountID,
		DeviceID:  deviceID,
		ChatID:    chatID,
		ChatJID:   chatJID,
	}
	p.mu.Unlock()

	log.Printf("[HistorySync] Requested on-demand sync for chat %s (device=%s, before=%v, peerMsgID=%s, timestamp=%s)",
		chatJID, instance.ID, msgInfo != nil, resp.ID, resp.Timestamp.Format(time.RFC3339))
	return nil
}

// handleReaction processes incoming reaction messages
func (p *DevicePool) handleReaction(ctx context.Context, instance *DeviceInstance, evt *events.Message, reactionMsg *waE2E.ReactionMessage) {
	key := reactionMsg.GetKey()
	if key == nil {
		return
	}

	targetMsgID := key.GetID()
	emoji := reactionMsg.GetText()
	senderJID := evt.Info.Sender.ToNonAD().String()
	isFromMe := evt.Info.IsFromMe

	// Resolve chat JID
	chatJID := evt.Info.Chat.ToNonAD().String()
	if evt.Info.Chat.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			chatJID = pnJID.User + "@s.whatsapp.net"
		}
	}

	// Get the chat
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, chatJID, "")
	if err != nil {
		log.Printf("[Reaction] Failed to get chat: %v", err)
		return
	}

	if emoji == "" {
		// Remove reaction
		_ = p.repos.Reaction.Delete(ctx, chat.ID, targetMsgID, senderJID)
		log.Printf("[Reaction] %s removed reaction from %s", senderJID, targetMsgID)
	} else {
		// Upsert reaction
		reaction := &domain.MessageReaction{
			AccountID:       instance.AccountID,
			ChatID:          chat.ID,
			TargetMessageID: targetMsgID,
			SenderJID:       senderJID,
			SenderName:      strPtr(evt.Info.PushName),
			Emoji:           emoji,
			IsFromMe:        isFromMe,
			Timestamp:       evt.Info.Timestamp,
		}
		if err := p.repos.Reaction.Upsert(ctx, reaction); err != nil {
			log.Printf("[Reaction] Failed to save reaction: %v", err)
			return
		}
		log.Printf("[Reaction] %s reacted %s to %s", senderJID, emoji, targetMsgID)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Broadcast to frontend
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageReaction, map[string]interface{}{
		"chat_id":           chat.ID.String(),
		"target_message_id": targetMsgID,
		"sender_jid":        senderJID,
		"sender_name":       evt.Info.PushName,
		"emoji":             emoji,
		"is_from_me":        isFromMe,
		"removed":           emoji == "",
	})
}

// handlePollCreation processes incoming poll creation messages
func (p *DevicePool) handlePollCreation(ctx context.Context, instance *DeviceInstance, evt *events.Message, pollMsg *waE2E.PollCreationMessage) {
	chatJID := evt.Info.Chat.ToNonAD().String()
	senderJID := evt.Info.Sender.ToNonAD().String()
	isFromMe := evt.Info.IsFromMe
	phone := evt.Info.Sender.ToNonAD().User

	if evt.Info.Chat.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			chatJID = pnJID.User + "@s.whatsapp.net"
			phone = pnJID.User
		}
	}

	chatName := ""
	if !isFromMe {
		chatName = evt.Info.PushName
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, chatJID, chatName)
	if err != nil {
		log.Printf("[Poll] Failed to get/create chat: %v", err)
		return
	}

	question := pollMsg.GetName()
	var optionNames []string
	for _, opt := range pollMsg.GetOptions() {
		optionNames = append(optionNames, opt.GetOptionName())
	}
	maxSelections := int(pollMsg.GetSelectableOptionsCount())
	if maxSelections <= 0 {
		maxSelections = 1
	}

	// Build display body
	body := "📊 " + question
	for i, opt := range optionNames {
		body += fmt.Sprintf("\n%d. %s", i+1, opt)
	}

	msg := &domain.Message{
		AccountID:   instance.AccountID,
		DeviceID:    &instance.ID,
		ChatID:      chat.ID,
		MessageID:   evt.Info.ID,
		FromJID:     strPtr(senderJID),
		FromName:    strPtr(evt.Info.PushName),
		Body:        strPtr(body),
		MessageType: strPtr(domain.MessageTypePoll),
		IsFromMe:    isFromMe,
		Status: strPtr(func() string {
			if isFromMe {
				return "sent"
			}
			return "received"
		}()),
		Timestamp:         evt.Info.Timestamp,
		PollQuestion:      strPtr(question),
		PollMaxSelections: maxSelections,
	}

	if err := p.repos.Message.Create(ctx, msg); err != nil {
		log.Printf("[Poll] Failed to save message: %v", err)
		return
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Create poll options
	if err := p.repos.Poll.CreateOptions(ctx, msg.ID, optionNames); err != nil {
		log.Printf("[Poll] Failed to save options: %v", err)
	}

	// Load options for response
	msg.PollOptions, _ = p.repos.Poll.GetOptions(ctx, msg.ID)

	_ = p.repos.Chat.UpdateLastMessage(ctx, chat.ID, "📊 "+question, evt.Info.Timestamp, !isFromMe)

	contactJID := senderJID
	if !isFromMe {
		contactJID = chatJID
	}
	p.repos.Contact.GetOrCreate(ctx, instance.AccountID, &instance.ID, contactJID, phone, evt.Info.PushName, evt.Info.PushName, false)

	p.hub.BroadcastNewMessage(instance.AccountID, map[string]interface{}{
		"chat_id":      chat.ID.String(),
		"message":      msg,
		"chat_jid":     chatJID,
		"sender_name":  evt.Info.PushName,
		"is_from_me":   isFromMe,
		"unread_count": chat.UnreadCount + 1,
	})

	log.Printf("[Poll] %s created poll: %s (%d options)", senderJID, question, len(optionNames))
}

// handlePollUpdate processes incoming poll vote updates
func (p *DevicePool) handlePollUpdate(ctx context.Context, instance *DeviceInstance, evt *events.Message, pollUpdate *waE2E.PollUpdateMessage) {
	chatJID := evt.Info.Chat.ToNonAD().String()
	if evt.Info.Chat.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, evt.Info.Chat.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			chatJID = pnJID.User + "@s.whatsapp.net"
		}
	}

	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, chatJID, "")
	if err != nil {
		log.Printf("[PollVote] Failed to get chat: %v", err)
		return
	}

	// Get the poll message stanza ID from the vote's key
	pollKey := pollUpdate.GetPollCreationMessageKey()
	if pollKey == nil {
		log.Printf("[PollVote] No poll key in update")
		return
	}
	pollStanzaID := pollKey.GetID()

	// Decrypt poll vote
	decrypted, err := instance.Client.DecryptPollVote(ctx, evt)
	if err != nil {
		log.Printf("[PollVote] Failed to decrypt vote: %v", err)
		return
	}

	// Find the poll message in DB
	pollMsg, err := p.repos.Message.GetByMessageID(ctx, chat.ID, pollStanzaID)
	if err != nil || pollMsg == nil {
		log.Printf("[PollVote] Poll message not found: %s", pollStanzaID)
		return
	}

	// Match selected option hashes to option names
	// decrypted.GetSelectedOptions() returns SHA256 hashes of option names
	var selectedNames []string
	pollOptions, _ := p.repos.Poll.GetOptions(ctx, pollMsg.ID)

	for _, hashBytes := range decrypted.GetSelectedOptions() {
		for _, opt := range pollOptions {
			optHash := sha256.Sum256([]byte(opt.Name))
			if string(hashBytes) == string(optHash[:]) {
				selectedNames = append(selectedNames, opt.Name)
				break
			}
		}
	}

	voterJID := evt.Info.Sender.ToNonAD().String()
	vote := &domain.PollVote{
		MessageID:     pollMsg.ID,
		VoterJID:      voterJID,
		SelectedNames: selectedNames,
		Timestamp:     evt.Info.Timestamp,
	}
	if err := p.repos.Poll.UpsertVote(ctx, vote); err != nil {
		log.Printf("[PollVote] Failed to save vote: %v", err)
		return
	}

	// Recalculate vote counts
	_ = p.repos.Poll.RecalculateVoteCounts(ctx, pollMsg.ID)

	// Load updated data
	updatedOptions, _ := p.repos.Poll.GetOptions(ctx, pollMsg.ID)
	allVotes, _ := p.repos.Poll.GetVotes(ctx, pollMsg.ID)

	// Broadcast to frontend
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventPollUpdate, map[string]interface{}{
		"chat_id":    chat.ID.String(),
		"message_id": pollMsg.MessageID,
		"options":    updatedOptions,
		"votes":      allVotes,
		"voter_jid":  voterJID,
	})

	log.Printf("[PollVote] %s voted on poll %s: %v", voterJID, pollStanzaID, selectedNames)
}

// syncContacts syncs all contacts from a WhatsApp device
func (p *DevicePool) syncContacts(ctx context.Context, instance *DeviceInstance) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ContactSync] Panic for device %s: %v", instance.ID, r)
		}
	}()

	if instance.Client == nil || instance.Client.Store == nil || instance.Client.Store.Contacts == nil {
		log.Printf("[ContactSync] Device %s: no contact store available", instance.ID)
		return
	}

	allContacts, err := instance.Client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		log.Printf("[ContactSync] Failed to get contacts for device %s: %v", instance.ID, err)
		return
	}

	log.Printf("[ContactSync] Device %s: syncing %d contacts", instance.ID, len(allContacts))

	synced := 0
	for jid, info := range allContacts {
		// Skip non-user contacts (groups, broadcasts, etc.)
		if jid.Server != "s.whatsapp.net" && jid.Server != types.HiddenUserServer {
			continue
		}

		normalizedJID := jid.ToNonAD().String()
		phone := jid.User

		// Resolve LID to phone JID if possible
		if jid.Server == types.HiddenUserServer {
			if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, jid.ToNonAD()); err == nil && !pnJID.IsEmpty() {
				normalizedJID = pnJID.User + "@s.whatsapp.net"
				phone = pnJID.User
			}
		}

		// Determine best name
		name := info.FullName
		if name == "" {
			name = info.FirstName
		}
		if name == "" {
			name = info.BusinessName
		}
		pushName := info.PushName

		// Get or create the contact
		contact, err := p.repos.Contact.GetOrCreate(ctx, instance.AccountID, &instance.ID, normalizedJID, phone, name, pushName, false)
		if err != nil {
			log.Printf("[ContactSync] Failed to upsert contact %s: %v", normalizedJID, err)
			continue
		}

		// Upsert the per-device name
		cdn := &domain.ContactDeviceName{
			ContactID: contact.ID,
			DeviceID:  instance.ID,
			Name:      strPtr(name),
			PushName:  strPtr(pushName),
		}
		if info.BusinessName != "" {
			cdn.BusinessName = strPtr(info.BusinessName)
		}
		_ = p.repos.ContactDeviceName.Upsert(ctx, cdn)

		synced++
	}

	log.Printf("[ContactSync] Device %s: synced %d contacts", instance.ID, synced)

	// Notify frontend that contacts were updated
	p.hub.BroadcastToAccount(instance.AccountID, "contacts_synced", map[string]interface{}{
		"device_id": instance.ID.String(),
		"count":     synced,
	})
}

// SyncDeviceContacts is a public method to trigger contact sync for a device
func (p *DevicePool) SyncDeviceContacts(ctx context.Context, deviceID uuid.UUID) error {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil || !instance.Client.IsConnected() {
		return fmt.Errorf("device not connected: %s", deviceID)
	}

	go p.syncContacts(ctx, instance)
	return nil
}

// SendChatPresence sends a typing or recording indicator to a chat
func (p *DevicePool) SendChatPresence(ctx context.Context, deviceID uuid.UUID, to string, composing bool, media string) error {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse recipient JID
	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	// Determine presence state
	state := types.ChatPresencePaused
	if composing {
		state = types.ChatPresenceComposing
	}

	// Determine media type
	mediaType := types.ChatPresenceMediaText
	if media == "audio" {
		mediaType = types.ChatPresenceMediaAudio
	}

	return instance.Client.SendChatPresence(ctx, jid, state, mediaType)
}

// SendReadReceipt sends read receipts (blue ticks) for messages in a chat
func (p *DevicePool) SendReadReceipt(ctx context.Context, deviceID uuid.UUID, chatJID string, senderJID string, messageIDs []string) error {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse chat JID
	var chat types.JID
	if strings.Contains(chatJID, "@") {
		var err error
		chat, err = types.ParseJID(chatJID)
		if err != nil {
			return fmt.Errorf("invalid chat JID: %s", chatJID)
		}
	} else {
		chat = types.NewJID(chatJID, types.DefaultUserServer)
	}

	// Parse sender JID (empty for 1-on-1 chats)
	var sender types.JID
	if senderJID != "" {
		var err error
		sender, err = types.ParseJID(senderJID)
		if err != nil {
			return fmt.Errorf("invalid sender JID: %s", senderJID)
		}
	}

	// Build message ID list
	ids := make([]types.MessageID, len(messageIDs))
	for i, id := range messageIDs {
		ids[i] = types.MessageID(id)
	}

	return instance.Client.MarkRead(ctx, ids, time.Now(), chat, sender)
}

// IsOnWhatsApp checks if phone numbers are registered on WhatsApp
func (p *DevicePool) IsOnWhatsApp(ctx context.Context, deviceID uuid.UUID, phones []string) ([]domain.WhatsAppCheckResult, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	results, err := instance.Client.IsOnWhatsApp(ctx, phones)
	if err != nil {
		return nil, fmt.Errorf("failed to check WhatsApp: %w", err)
	}

	var checkResults []domain.WhatsAppCheckResult
	for _, r := range results {
		checkResults = append(checkResults, domain.WhatsAppCheckResult{
			Phone:        r.Query,
			IsOnWhatsApp: r.IsIn,
			JID:          r.JID.String(),
		})
	}

	return checkResults, nil
}

// RevokeMessage deletes/revokes a message for everyone
func (p *DevicePool) RevokeMessage(ctx context.Context, deviceID uuid.UUID, chatJID string, senderJID string, messageID string, isFromMe bool) error {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse chat JID
	var chat types.JID
	if strings.Contains(chatJID, "@") {
		var err error
		chat, err = types.ParseJID(chatJID)
		if err != nil {
			return fmt.Errorf("invalid chat JID: %s", chatJID)
		}
	} else {
		chat = types.NewJID(chatJID, types.DefaultUserServer)
	}

	_, err := instance.Client.RevokeMessage(ctx, chat, messageID)
	if err != nil {
		return fmt.Errorf("failed to revoke message: %w", err)
	}

	return nil
}

// EditMessage edits a previously sent text message
func (p *DevicePool) EditMessage(ctx context.Context, deviceID uuid.UUID, chatJID string, messageID string, newBody string) error {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse chat JID
	var chat types.JID
	if strings.Contains(chatJID, "@") {
		var err error
		chat, err = types.ParseJID(chatJID)
		if err != nil {
			return fmt.Errorf("invalid chat JID: %s", chatJID)
		}
	} else {
		chat = types.NewJID(chatJID, types.DefaultUserServer)
	}

	editedMsg := instance.Client.BuildEdit(chat, messageID, &waE2E.Message{
		Conversation: proto.String(newBody),
	})

	_, err := instance.Client.SendMessage(ctx, chat, editedMsg)
	if err != nil {
		return fmt.Errorf("failed to edit message: %w", err)
	}

	log.Printf("[Edit] Successfully edited message %s in chat %s", messageID, chatJID)
	return nil
}

// SendMessage sends a text message
func (p *DevicePool) SendMessage(ctx context.Context, deviceID uuid.UUID, to, body string) (*domain.Message, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse recipient JID - construct directly for phone numbers to avoid ParseJID misparse
	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return nil, fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	// Create message
	msg := &waE2E.Message{
		Conversation: proto.String(body),
	}

	// Send message
	resp, sendJID, err := p.sendMessageWithLIDFallback(ctx, instance, jid, msg, "SendMessage")
	if err != nil {
		instance.mu.Lock()
		instance.Metrics.SendErrorCount++
		instance.Metrics.LastSendError = time.Now()
		instance.mu.Unlock()
		return nil, fmt.Errorf("failed to send message: %w", err)
	}
	instance.mu.Lock()
	instance.Metrics.SendSuccessCount++
	instance.Metrics.LastSendSuccess = time.Now()
	instance.mu.Unlock()

	// Get or create chat using normalized JID (without device suffix)
	// Resolve @lid to @s.whatsapp.net for consistent chat identity
	normalizedJID := sendJID.ToNonAD().String()
	if sendJID.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, sendJID.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			normalizedJID = pnJID.User + "@s.whatsapp.net"
			log.Printf("[SendMessage] Resolved LID %s -> %s", sendJID.ToNonAD().String(), normalizedJID)
		}
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, normalizedJID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get/create chat: %w", err)
	}

	// Create message record
	message := &domain.Message{
		AccountID:   instance.AccountID,
		DeviceID:    &instance.ID,
		ChatID:      chat.ID,
		MessageID:   resp.ID,
		FromJID:     strPtr(instance.JID),
		FromName:    strPtr("Me"),
		Body:        strPtr(body),
		MessageType: strPtr(domain.MessageTypeText),
		IsFromMe:    true,
		Status:      strPtr("sent"),
		Timestamp:   resp.Timestamp,
	}

	if err := p.repos.Message.Create(ctx, message); err != nil {
		log.Printf("[SendMessage] Failed to save message: %v", err)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Update chat
	_ = p.repos.Chat.UpdateLastMessage(ctx, chat.ID, body, resp.Timestamp, false)

	// Broadcast to frontend
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageSent, map[string]interface{}{
		"chat_id": chat.ID.String(),
		"message": message,
	})

	return message, nil
}

func (p *DevicePool) sendMessageWithLIDFallback(ctx context.Context, instance *DeviceInstance, jid types.JID, msg *waE2E.Message, label string) (whatsmeow.SendResponse, types.JID, error) {
	resp, err := instance.Client.SendMessage(ctx, jid, msg)
	if err == nil {
		return resp, jid, nil
	}

	if jid.Server != types.DefaultUserServer || !strings.Contains(err.Error(), "server returned error 463") {
		return resp, jid, err
	}

	lidJID, lidErr := p.store.LIDMap.GetLIDForPN(ctx, jid.ToNonAD())
	if lidErr != nil || lidJID.IsEmpty() {
		if lidErr != nil {
			log.Printf("[%s] WhatsApp returned 463 for %s and LID lookup failed: %v", label, jid.ToNonAD().String(), lidErr)
		}
		return resp, jid, err
	}

	log.Printf("[%s] WhatsApp returned 463 for %s, retrying via LID %s", label, jid.ToNonAD().String(), lidJID.ToNonAD().String())
	lidResp, retryErr := instance.Client.SendMessage(ctx, lidJID, msg)
	if retryErr != nil {
		return resp, jid, fmt.Errorf("%w; LID retry to %s also failed: %v", err, lidJID.ToNonAD().String(), retryErr)
	}
	return lidResp, lidJID, nil
}

// SendReplyMessage sends a text message as a reply to another message
func (p *DevicePool) SendReplyMessage(ctx context.Context, deviceID uuid.UUID, to, body, quotedID, quotedBody, quotedSender string, quotedIsFromMe bool) (*domain.Message, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse recipient JID - construct directly for phone numbers to avoid ParseJID misparse
	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return nil, fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	// Build the quoted sender JID for ContextInfo
	var quotedParticipant *string
	if quotedSender != "" {
		quotedParticipant = proto.String(quotedSender)
	}

	// Build quoted message proto
	quotedMsg := &waE2E.Message{
		Conversation: proto.String(quotedBody),
	}

	// Create message with ContextInfo for reply
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(body),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(quotedID),
				Participant:   quotedParticipant,
				QuotedMessage: quotedMsg,
			},
		},
	}

	// Send message
	resp, sendJID, err := p.sendMessageWithLIDFallback(ctx, instance, jid, msg, "SendReplyMessage")
	if err != nil {
		return nil, fmt.Errorf("failed to send reply: %w", err)
	}

	// Get or create chat
	normalizedJID := sendJID.ToNonAD().String()
	if sendJID.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, sendJID.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			normalizedJID = pnJID.User + "@s.whatsapp.net"
		}
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, normalizedJID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get/create chat: %w", err)
	}

	// Create message record
	message := &domain.Message{
		AccountID:       instance.AccountID,
		DeviceID:        &instance.ID,
		ChatID:          chat.ID,
		MessageID:       resp.ID,
		FromJID:         strPtr(instance.JID),
		FromName:        strPtr("Me"),
		Body:            strPtr(body),
		MessageType:     strPtr(domain.MessageTypeText),
		IsFromMe:        true,
		Status:          strPtr("sent"),
		Timestamp:       resp.Timestamp,
		QuotedMessageID: strPtr(quotedID),
		QuotedBody:      strPtr(quotedBody),
		QuotedSender:    strPtr(quotedSender),
	}

	if err := p.repos.Message.Create(ctx, message); err != nil {
		log.Printf("[SendReplyMessage] Failed to save message: %v", err)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Update chat
	_ = p.repos.Chat.UpdateLastMessage(ctx, chat.ID, body, resp.Timestamp, false)

	// Broadcast to frontend
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageSent, map[string]interface{}{
		"chat_id": chat.ID.String(),
		"message": message,
	})

	return message, nil
}

// ForwardMessage forwards a message to another chat
func (p *DevicePool) ForwardMessage(ctx context.Context, deviceID uuid.UUID, to string, originalMsg *domain.Message) (*domain.Message, error) {
	// If it has media, forward as media; otherwise forward as text
	if originalMsg.MediaURL != nil && *originalMsg.MediaURL != "" && originalMsg.MessageType != nil {
		body := ""
		if originalMsg.Body != nil {
			body = *originalMsg.Body
		}
		return p.SendMediaMessage(ctx, deviceID, to, body, *originalMsg.MediaURL, *originalMsg.MessageType)
	}

	body := ""
	if originalMsg.Body != nil {
		body = *originalMsg.Body
	}
	return p.SendMessage(ctx, deviceID, to, body)
}

// SendReaction sends a reaction emoji to a message
func (p *DevicePool) SendReaction(ctx context.Context, deviceID uuid.UUID, to, targetMessageID, emoji string, targetFromMe bool) error {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return fmt.Errorf("device not connected: %s", deviceID)
	}

	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	msg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				RemoteJID: proto.String(jid.String()),
				FromMe:    proto.Bool(targetFromMe),
				ID:        proto.String(targetMessageID),
			},
			Text:              proto.String(emoji),
			SenderTimestampMS: proto.Int64(0),
		},
	}

	_, err := instance.Client.SendMessage(ctx, jid, msg)
	if err != nil {
		return fmt.Errorf("failed to send reaction: %w", err)
	}

	// Get chat for storing reaction
	normalizedJID := jid.ToNonAD().String()
	if jid.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, jid.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			normalizedJID = pnJID.User + "@s.whatsapp.net"
		}
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, normalizedJID, "")
	if err != nil {
		return err
	}

	senderJID := instance.JID
	if emoji == "" {
		_ = p.repos.Reaction.Delete(ctx, chat.ID, targetMessageID, senderJID)
	} else {
		reaction := &domain.MessageReaction{
			AccountID:       instance.AccountID,
			ChatID:          chat.ID,
			TargetMessageID: targetMessageID,
			SenderJID:       senderJID,
			SenderName:      strPtr("Me"),
			Emoji:           emoji,
			IsFromMe:        true,
			Timestamp:       time.Now(),
		}
		_ = p.repos.Reaction.Upsert(ctx, reaction)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Broadcast
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageReaction, map[string]interface{}{
		"chat_id":           chat.ID.String(),
		"target_message_id": targetMessageID,
		"sender_jid":        senderJID,
		"sender_name":       "Me",
		"emoji":             emoji,
		"is_from_me":        true,
		"removed":           emoji == "",
	})

	log.Printf("[Reaction] Sent %s to %s on %s", emoji, targetMessageID, to)
	return nil
}

// SendPoll sends a poll creation message
func (p *DevicePool) SendPoll(ctx context.Context, deviceID uuid.UUID, to, question string, options []string, maxSelections int) (*domain.Message, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return nil, fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	if maxSelections <= 0 {
		maxSelections = 1
	}

	var pollOptions []*waE2E.PollCreationMessage_Option
	for _, opt := range options {
		pollOptions = append(pollOptions, &waE2E.PollCreationMessage_Option{
			OptionName: proto.String(opt),
		})
	}

	msg := &waE2E.Message{
		PollCreationMessage: &waE2E.PollCreationMessage{
			Name:                   proto.String(question),
			Options:                pollOptions,
			SelectableOptionsCount: proto.Uint32(uint32(maxSelections)),
		},
	}

	resp, sendJID, err := p.sendMessageWithLIDFallback(ctx, instance, jid, msg, "SendPoll")
	if err != nil {
		return nil, fmt.Errorf("failed to send poll: %w", err)
	}

	// Build display body
	body := "📊 " + question
	for i, opt := range options {
		body += fmt.Sprintf("\n%d. %s", i+1, opt)
	}

	// Get or create chat
	normalizedJID := sendJID.ToNonAD().String()
	if sendJID.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, sendJID.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			normalizedJID = pnJID.User + "@s.whatsapp.net"
		}
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, normalizedJID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get/create chat: %w", err)
	}

	message := &domain.Message{
		AccountID:         instance.AccountID,
		DeviceID:          &instance.ID,
		ChatID:            chat.ID,
		MessageID:         resp.ID,
		FromJID:           strPtr(instance.JID),
		FromName:          strPtr("Me"),
		Body:              strPtr(body),
		MessageType:       strPtr(domain.MessageTypePoll),
		IsFromMe:          true,
		Status:            strPtr("sent"),
		Timestamp:         resp.Timestamp,
		PollQuestion:      strPtr(question),
		PollMaxSelections: maxSelections,
	}

	if err := p.repos.Message.Create(ctx, message); err != nil {
		log.Printf("[SendPoll] Failed to save message: %v", err)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Create poll options
	_ = p.repos.Poll.CreateOptions(ctx, message.ID, options)

	// Load options for response
	message.PollOptions, _ = p.repos.Poll.GetOptions(ctx, message.ID)

	_ = p.repos.Chat.UpdateLastMessage(ctx, chat.ID, "📊 "+question, resp.Timestamp, false)

	// Broadcast
	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageSent, map[string]interface{}{
		"chat_id": chat.ID.String(),
		"message": message,
	})

	log.Printf("[SendPoll] Sent poll '%s' to %s", question, to)
	return message, nil
}

// publicToProxyURL converts a MinIO public URL to a backend proxy URL
func (p *DevicePool) publicToProxyURL(publicURL string) string {
	if strings.HasPrefix(publicURL, "/api/media/") {
		return publicURL // Already a proxy URL
	}
	// Extract path after bucket name: https://host/bucket/objectKey -> /api/media/file/objectKey
	bucketPrefix := fmt.Sprintf("%s/%s/", p.cfg.MinioPublicURL, p.cfg.MinioBucket)
	if strings.HasPrefix(publicURL, bucketPrefix) {
		objectPath := strings.TrimPrefix(publicURL, bucketPrefix)
		return "/api/media/file/" + objectPath
	}
	// Fallback: try to find bucket name in URL
	marker := "/" + p.cfg.MinioBucket + "/"
	if idx := strings.Index(publicURL, marker); idx >= 0 {
		objectPath := publicURL[idx+len(marker):]
		return "/api/media/file/" + objectPath
	}
	return publicURL
}

// SendMediaMessage sends a media message (image, video, audio, document)
// PreUploadedMedia contains the WhatsApp upload metadata for reuse across multiple recipients.
// Upload once, send many — avoids re-uploading the same file per recipient during campaigns.
type PreUploadedMedia struct {
	URL           string
	DirectPath    string
	MediaKey      []byte
	FileEncSHA256 []byte
	FileSHA256    []byte
	FileLength    uint64
	Mimetype      string
	MediaType     string // domain.MessageType*
	OriginalURL   string // original media URL for proxy URL resolution
}

// UploadMedia downloads a file from storage/URL and uploads it to WhatsApp ONCE.
// Returns a PreUploadedMedia that can be reused with SendPreUploadedMediaMessage for many recipients.
func (p *DevicePool) UploadMedia(ctx context.Context, deviceID uuid.UUID, mediaURL, mediaType string) (*PreUploadedMedia, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	// Download media - handle proxy URLs and public URLs
	var data []byte
	var mimetype string

	if strings.HasPrefix(mediaURL, "/api/media/file/") {
		objectKey := strings.TrimPrefix(mediaURL, "/api/media/file/")
		log.Printf("[UploadMedia] Reading from storage: %s", objectKey)
		var err2 error
		data, err2 = p.storage.GetFile(ctx, objectKey)
		if err2 != nil {
			return nil, fmt.Errorf("failed to read media from storage: %w", err2)
		}
		mimetype = "application/octet-stream"
		if dotIdx := strings.LastIndex(objectKey, "."); dotIdx >= 0 {
			ext := strings.ToLower(objectKey[dotIdx:])
			switch ext {
			case ".jpg", ".jpeg":
				mimetype = "image/jpeg"
			case ".png":
				mimetype = "image/png"
			case ".gif":
				mimetype = "image/gif"
			case ".webp":
				mimetype = "image/webp"
			case ".mp4":
				mimetype = "video/mp4"
			case ".webm":
				mimetype = "video/webm"
			case ".mp3":
				mimetype = "audio/mpeg"
			case ".ogg":
				mimetype = "audio/ogg"
			case ".pdf":
				mimetype = "application/pdf"
			}
		}
	} else {
		downloadURL := mediaURL
		if p.cfg.MinioPublicURL != "" && p.cfg.MinioEndpoint != "" {
			scheme := "http"
			if p.cfg.MinioUseSSL {
				scheme = "https"
			}
			internalURL := fmt.Sprintf("%s://%s", scheme, p.cfg.MinioEndpoint)
			downloadURL = strings.Replace(mediaURL, p.cfg.MinioPublicURL, internalURL, 1)
			log.Printf("[UploadMedia] Converted URL: %s -> %s", mediaURL, downloadURL)
		}
		resp, err := http.Get(downloadURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download media: Get %q: %w", downloadURL, err)
		}
		defer resp.Body.Close()
		var err2 error
		data, err2 = io.ReadAll(resp.Body)
		if err2 != nil {
			return nil, fmt.Errorf("failed to read media: %w", err2)
		}
		mimetype = resp.Header.Get("Content-Type")
		if mimetype == "" {
			mimetype = "application/octet-stream"
		}
	}

	// Determine the correct WhatsApp media type for upload
	var waMediaType whatsmeow.MediaType
	switch mediaType {
	case domain.MessageTypeImage:
		waMediaType = whatsmeow.MediaImage
	case domain.MessageTypeVideo:
		waMediaType = whatsmeow.MediaVideo
	case domain.MessageTypeAudio:
		waMediaType = whatsmeow.MediaAudio
	case domain.MessageTypeDocument:
		waMediaType = whatsmeow.MediaDocument
	case domain.MessageTypeSticker:
		waMediaType = whatsmeow.MediaImage
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	// Upload to WhatsApp with retry for transient network errors
	log.Printf("[UploadMedia] Uploading %s (%d bytes)", mediaType, len(data))
	var uploaded whatsmeow.UploadResponse
	var uploadErr error
	for attempt := 0; attempt < 5; attempt++ {
		uploaded, uploadErr = instance.Client.Upload(ctx, data, waMediaType)
		if uploadErr == nil {
			break
		}
		errStr := uploadErr.Error()
		isTransient := strings.Contains(errStr, "connection reset by peer") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "broken pipe")
		if !isTransient {
			break
		}
		backoff := time.Duration(2<<attempt) * time.Second
		log.Printf("[UploadMedia] Upload attempt %d failed (retrying in %v): %v", attempt+1, backoff, uploadErr)
		time.Sleep(backoff)
		if _, err := instance.Client.DangerousInternals().RefreshMediaConn(ctx, true); err != nil {
			log.Printf("[UploadMedia] Failed to refresh media connection: %v", err)
		} else {
			log.Printf("[UploadMedia] Refreshed media connection for retry %d", attempt+2)
		}
	}
	if uploadErr != nil {
		return nil, fmt.Errorf("failed to upload to WhatsApp: %w", uploadErr)
	}

	log.Printf("[UploadMedia] Upload complete: %s (%d bytes) -> %s", mediaType, len(data), uploaded.URL)
	return &PreUploadedMedia{
		URL:           uploaded.URL,
		DirectPath:    uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    uint64(len(data)),
		Mimetype:      mimetype,
		MediaType:     mediaType,
		OriginalURL:   mediaURL,
	}, nil
}

// SendPreUploadedMediaMessage sends a pre-uploaded media to a recipient, without re-downloading/re-uploading.
// Used by campaign worker to send the same media to many recipients efficiently.
func (p *DevicePool) SendPreUploadedMediaMessage(ctx context.Context, deviceID uuid.UUID, to, caption string, media *PreUploadedMedia) (*domain.Message, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse recipient JID
	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return nil, fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	// Build message from pre-uploaded metadata
	var msg *waE2E.Message
	switch media.MediaType {
	case domain.MessageTypeImage:
		msg = &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:           proto.String(media.URL),
				DirectPath:    proto.String(media.DirectPath),
				MediaKey:      media.MediaKey,
				Mimetype:      proto.String(media.Mimetype),
				FileEncSHA256: media.FileEncSHA256,
				FileSHA256:    media.FileSHA256,
				FileLength:    proto.Uint64(media.FileLength),
				Caption:       proto.String(caption),
			},
		}
	case domain.MessageTypeVideo:
		msg = &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				URL:           proto.String(media.URL),
				DirectPath:    proto.String(media.DirectPath),
				MediaKey:      media.MediaKey,
				Mimetype:      proto.String(media.Mimetype),
				FileEncSHA256: media.FileEncSHA256,
				FileSHA256:    media.FileSHA256,
				FileLength:    proto.Uint64(media.FileLength),
				Caption:       proto.String(caption),
			},
		}
	case domain.MessageTypeAudio:
		msg = &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(media.URL),
				DirectPath:    proto.String(media.DirectPath),
				MediaKey:      media.MediaKey,
				Mimetype:      proto.String(media.Mimetype),
				FileEncSHA256: media.FileEncSHA256,
				FileSHA256:    media.FileSHA256,
				FileLength:    proto.Uint64(media.FileLength),
				PTT:           proto.Bool(true),
			},
		}
	case domain.MessageTypeDocument:
		filename := filepath.Base(media.OriginalURL)
		msg = &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				URL:           proto.String(media.URL),
				DirectPath:    proto.String(media.DirectPath),
				MediaKey:      media.MediaKey,
				Mimetype:      proto.String(media.Mimetype),
				FileEncSHA256: media.FileEncSHA256,
				FileSHA256:    media.FileSHA256,
				FileLength:    proto.Uint64(media.FileLength),
				FileName:      proto.String(filename),
				Caption:       proto.String(caption),
			},
		}
	case domain.MessageTypeSticker:
		msg = &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				URL:           proto.String(media.URL),
				DirectPath:    proto.String(media.DirectPath),
				MediaKey:      media.MediaKey,
				Mimetype:      proto.String("image/webp"),
				FileEncSHA256: media.FileEncSHA256,
				FileSHA256:    media.FileSHA256,
				FileLength:    proto.Uint64(media.FileLength),
			},
		}
	}

	// Send message
	sendResp, sendJID, err := p.sendMessageWithLIDFallback(ctx, instance, jid, msg, "SendPreUploadedMedia")
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Get or create chat using normalized JID
	normalizedJID := sendJID.ToNonAD().String()
	if sendJID.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, sendJID.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			normalizedJID = pnJID.User + "@s.whatsapp.net"
			log.Printf("[SendPreUploadedMedia] Resolved LID %s -> %s", sendJID.ToNonAD().String(), normalizedJID)
		}
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, normalizedJID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get/create chat: %w", err)
	}

	// Create message record
	proxyMediaURL := p.publicToProxyURL(media.OriginalURL)
	size := int64(media.FileLength)
	message := &domain.Message{
		AccountID:     instance.AccountID,
		DeviceID:      &instance.ID,
		ChatID:        chat.ID,
		MessageID:     sendResp.ID,
		FromJID:       strPtr(instance.JID),
		FromName:      strPtr("Me"),
		Body:          strPtr(caption),
		MessageType:   strPtr(media.MediaType),
		MediaURL:      strPtr(proxyMediaURL),
		MediaMimetype: strPtr(media.Mimetype),
		MediaSize:     &size,
		IsFromMe:      true,
		Status:        strPtr("sent"),
		Timestamp:     sendResp.Timestamp,
	}

	if err := p.repos.Message.Create(ctx, message); err != nil {
		log.Printf("[SendPreUploadedMedia] Failed to save message: %v", err)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	lastMsg := caption
	if lastMsg == "" {
		lastMsg = fmt.Sprintf("[%s]", media.MediaType)
	}
	_ = p.repos.Chat.UpdateLastMessage(ctx, chat.ID, lastMsg, sendResp.Timestamp, false)

	p.hub.BroadcastToAccount(instance.AccountID, ws.EventMessageSent, map[string]interface{}{
		"chat_id": chat.ID.String(),
		"message": message,
	})

	log.Printf("[SendPreUploadedMedia] %s -> %s: [%s]", instance.JID, jid.String(), media.MediaType)
	return message, nil
}

// SendMediaMessage sends a media message (downloads, uploads, and sends in one call).
// For bulk sends, prefer UploadMedia + SendPreUploadedMediaMessage to avoid redundant uploads.
func (p *DevicePool) SendMediaMessage(ctx context.Context, deviceID uuid.UUID, to, caption, mediaURL, mediaType string) (*domain.Message, error) {
	media, err := p.UploadMedia(ctx, deviceID, mediaURL, mediaType)
	if err != nil {
		return nil, err
	}
	return p.SendPreUploadedMediaMessage(ctx, deviceID, to, caption, media)
}

// SendContactMessage sends a contact vCard message
func (p *DevicePool) SendContactMessage(ctx context.Context, deviceID uuid.UUID, to, contactName, contactPhone string) (*domain.Message, error) {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists || instance.Client == nil {
		return nil, fmt.Errorf("device not connected: %s", deviceID)
	}

	// Parse recipient JID
	var jid types.JID
	if strings.Contains(to, "@") {
		var err error
		jid, err = types.ParseJID(to)
		if err != nil {
			return nil, fmt.Errorf("invalid JID: %s", to)
		}
	} else {
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	// Build vCard
	// Clean phone number - ensure it has country code prefix
	phone := strings.ReplaceAll(contactPhone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")
	phone = strings.ReplaceAll(phone, "+", "")
	if len(phone) == 9 && strings.HasPrefix(phone, "9") {
		phone = "51" + phone
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}

	vcard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nFN:%s\nTEL;type=CELL;type=VOICE;waid=%s:%s\nEND:VCARD",
		contactName,
		strings.TrimPrefix(phone, "+"),
		phone,
	)

	msg := &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String(contactName),
			Vcard:       proto.String(vcard),
		},
	}

	sendResp, sendJID, err := p.sendMessageWithLIDFallback(ctx, instance, jid, msg, "SendContactMessage")
	if err != nil {
		return nil, fmt.Errorf("failed to send contact message: %w", err)
	}

	// Get or create chat
	normalizedJID := sendJID.ToNonAD().String()
	if sendJID.Server == types.HiddenUserServer {
		if pnJID, err := p.store.LIDMap.GetPNForLID(ctx, sendJID.ToNonAD()); err == nil && !pnJID.IsEmpty() {
			normalizedJID = pnJID.User + "@s.whatsapp.net"
		}
	}
	chat, err := p.repos.Chat.GetOrCreate(ctx, instance.AccountID, instance.ID, normalizedJID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get/create chat: %w", err)
	}

	// Create message record
	dbMsg := &domain.Message{
		AccountID:    instance.AccountID,
		DeviceID:     &instance.ID,
		ChatID:       chat.ID,
		MessageID:    sendResp.ID,
		FromJID:      strPtr(instance.Client.Store.ID.ToNonAD().String()),
		Body:         strPtr(contactName),
		MessageType:  strPtr(domain.MessageTypeContact),
		IsFromMe:     true,
		Status:       strPtr("sent"),
		Timestamp:    sendResp.Timestamp,
		ContactName:  strPtr(contactName),
		ContactPhone: strPtr(strings.TrimPrefix(phone, "+")),
		ContactVCard: strPtr(vcard),
	}

	if err := p.repos.Message.Create(ctx, dbMsg); err != nil {
		log.Printf("[SendContactMessage] Failed to save message: %v", err)
	}
	p.invalidateChatCaches(instance.AccountID, chat.ID)

	// Broadcast via WebSocket
	if p.hub != nil {
		p.hub.BroadcastToAccount(instance.AccountID, "new_message", map[string]interface{}{
			"chat_id": chat.ID.String(),
			"message": dbMsg,
		})
	}

	return dbMsg, nil
}

// GetDevice returns a device instance by ID
func (p *DevicePool) GetDevice(deviceID uuid.UUID) *DeviceInstance {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.devices[deviceID]
}

// GetDeviceStatus returns the status of a device
func (p *DevicePool) GetDeviceStatus(deviceID uuid.UUID) string {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists {
		return domain.DeviceStatusDisconnected
	}

	instance.mu.RLock()
	defer instance.mu.RUnlock()
	return instance.Status
}

// GetQRCode returns the current QR code for a device
func (p *DevicePool) GetQRCode(deviceID uuid.UUID) string {
	p.mu.RLock()
	instance, exists := p.devices[deviceID]
	p.mu.RUnlock()

	if !exists {
		return ""
	}

	instance.mu.RLock()
	defer instance.mu.RUnlock()
	return instance.QRCode
}

// DisconnectDevice disconnects a device
func (p *DevicePool) DisconnectDevice(ctx context.Context, deviceID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	instance, exists := p.devices[deviceID]
	if !exists {
		return nil
	}

	if instance.Client != nil {
		instance.Client.Disconnect()
	}

	instance.mu.Lock()
	instance.Status = domain.DeviceStatusDisconnected
	instance.mu.Unlock()

	_ = p.repos.Device.UpdateStatus(ctx, deviceID, domain.DeviceStatusDisconnected)

	return nil
}

// ResetDevice forces a device re-pair by logging out from WhatsApp and clearing the session.
// After reset, the device must be reconnected (which will generate a new QR code for pairing).
// This is needed when DeviceProps change (e.g. enabling OnDemandReady, RequireFullSync)
// since those are only sent during the initial pairing handshake.
func (p *DevicePool) ResetDevice(ctx context.Context, deviceID uuid.UUID) error {
	p.mu.Lock()
	instance, exists := p.devices[deviceID]
	p.mu.Unlock()

	if exists && instance.Client != nil {
		p.logoutAndDeleteClientStore(ctx, instance.Client, fmt.Sprintf("device %s reset", deviceID))

		// Remove from pool
		p.mu.Lock()
		delete(p.devices, deviceID)
		p.mu.Unlock()
	}

	// Clear JID in database so next connect generates QR
	_ = p.repos.Device.UpdateJID(ctx, deviceID, "", "")
	_ = p.repos.Device.UpdateStatus(ctx, deviceID, domain.DeviceStatusDisconnected)

	log.Printf("[DevicePool] Device %s reset — needs re-pairing via QR code", deviceID)
	return nil
}

// DeleteDevice removes a device completely
func (p *DevicePool) DeleteDevice(ctx context.Context, deviceID uuid.UUID) error {
	device, _ := p.repos.Device.GetByID(ctx, deviceID)
	var savedJID string
	if device != nil && device.JID != nil {
		savedJID = strings.TrimSpace(*device.JID)
	}

	p.mu.Lock()
	instance, exists := p.devices[deviceID]
	if exists {
		if instance.Client != nil {
			p.logoutAndDeleteClientStore(ctx, instance.Client, fmt.Sprintf("device %s delete", deviceID))
		}
		delete(p.devices, deviceID)
	}
	p.mu.Unlock()

	if !exists && savedJID != "" {
		p.deleteStoredWhatsAppDevice(ctx, savedJID, fmt.Sprintf("device %s delete", deviceID))
	}

	// Delete from database
	return p.repos.Device.Delete(ctx, deviceID)
}

func (p *DevicePool) logoutAndDeleteClientStore(ctx context.Context, client *whatsmeow.Client, label string) {
	if client == nil {
		return
	}
	if client.Store == nil || client.Store.ID == nil {
		client.Disconnect()
		return
	}

	waStore := client.Store
	jid := waStore.ID.String()
	if err := client.Logout(ctx); err != nil {
		log.Printf("[DevicePool] WhatsApp logout failed for %s (%s), forcing local store cleanup: %v", label, jid, err)
		client.Disconnect()
		if waStore.ID != nil {
			if deleteErr := waStore.Delete(ctx); deleteErr != nil {
				log.Printf("[DevicePool] Failed to force-delete WhatsApp store for %s (%s): %v", label, jid, deleteErr)
			} else {
				log.Printf("[DevicePool] Force-deleted WhatsApp store for %s (%s)", label, jid)
			}
		}
		return
	}
	log.Printf("[DevicePool] Logged out and deleted WhatsApp store for %s (%s)", label, jid)
}

func (p *DevicePool) deleteStoredWhatsAppDevice(ctx context.Context, jid string, label string) {
	parsed, err := types.ParseJID(jid)
	if err != nil {
		log.Printf("[DevicePool] Cannot clean WhatsApp store for %s: invalid JID %q: %v", label, jid, err)
		return
	}
	waDevice, err := p.store.GetDevice(ctx, parsed)
	if err != nil {
		log.Printf("[DevicePool] Failed to load WhatsApp store for cleanup %s (%s): %v", label, jid, err)
		return
	}
	if waDevice == nil {
		return
	}
	if err := waDevice.Delete(ctx); err != nil {
		log.Printf("[DevicePool] Failed to delete WhatsApp store for %s (%s): %v", label, jid, err)
		return
	}
	log.Printf("[DevicePool] Deleted stored WhatsApp device for %s (%s)", label, jid)
}

// Shutdown closes all connections gracefully
func (p *DevicePool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, instance := range p.devices {
		// Stop reconnect supervisor
		instance.mu.Lock()
		if instance.reconnecting && instance.stopReconnect != nil {
			close(instance.stopReconnect)
			instance.stopReconnect = nil
			instance.reconnecting = false
		}
		instance.mu.Unlock()

		if instance.Client != nil {
			instance.Client.Disconnect()
		}
		log.Printf("[DevicePool] Disconnected device %s", id)
	}
}

// GetConnectedCount returns the number of connected devices
func (p *DevicePool) GetConnectedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, instance := range p.devices {
		if instance.Client != nil && instance.Client.IsConnected() {
			count++
		}
	}
	return count
}

// IsDeviceConnected checks if a specific device is connected
func (p *DevicePool) IsDeviceConnected(deviceID uuid.UUID) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	instance, exists := p.devices[deviceID]
	return exists && instance.Client != nil && instance.Client.IsConnected()
}

// GetFirstConnectedDeviceForAccount returns the ID of the first connected device for a given account
func (p *DevicePool) GetFirstConnectedDeviceForAccount(accountID uuid.UUID) (uuid.UUID, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, instance := range p.devices {
		if instance.AccountID == accountID && instance.Client != nil && instance.Client.IsConnected() {
			return instance.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("no connected device found for account %s", accountID)
}

// GetTotalCount returns the total number of devices in the pool
func (p *DevicePool) GetTotalCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.devices)
}

// GetStartTime returns when the pool was started
func (p *DevicePool) GetStartTime() time.Time {
	return p.startTime
}

// GetHealthSummary returns health metrics for all devices
func (p *DevicePool) GetHealthSummary() []DeviceHealthSummary {
	p.mu.RLock()
	defer p.mu.RUnlock()

	summaries := make([]DeviceHealthSummary, 0, len(p.devices))
	for _, instance := range p.devices {
		instance.mu.RLock()
		connected := instance.Client != nil && instance.Client.IsConnected()
		s := DeviceHealthSummary{
			ID:        instance.ID,
			JID:       instance.JID,
			Status:    instance.Status,
			Connected: connected,
			Metrics:   instance.Metrics,
		}
		instance.mu.RUnlock()
		summaries = append(summaries, s)
	}
	return summaries
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// extractPhoneFromVCard extracts the first phone number from a vCard string
func extractPhoneFromVCard(vcard string) string {
	for _, line := range strings.Split(vcard, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "TEL") {
			// TEL;type=CELL:+51993738489 or TEL:+51993738489
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				phone := strings.TrimSpace(parts[1])
				// Remove common formatting characters
				phone = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "").Replace(phone)
				return phone
			}
		}
	}
	return ""
}
