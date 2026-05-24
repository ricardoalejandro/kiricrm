package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/kommo"
	"github.com/naperu/clarin/internal/ws"
)

type cloudWebhookPayload struct {
	Object string              `json:"object"`
	Entry  []cloudWebhookEntry `json:"entry"`
}

type cloudWebhookEntry struct {
	ID      string               `json:"id"`
	Changes []cloudWebhookChange `json:"changes"`
}

type cloudWebhookChange struct {
	Field string            `json:"field"`
	Value cloudWebhookValue `json:"value"`
}

type cloudWebhookValue struct {
	MessagingProduct string                   `json:"messaging_product"`
	Metadata         cloudWebhookMetadata     `json:"metadata"`
	Contacts         []cloudWebhookContact    `json:"contacts"`
	Messages         []cloudWebhookMessage    `json:"messages"`
	Statuses         []cloudWebhookStatus     `json:"statuses"`
	Errors           []map[string]interface{} `json:"errors"`
}

type cloudWebhookMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type cloudWebhookContact struct {
	WAID    string `json:"wa_id"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

type cloudWebhookMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      *struct {
		Body string `json:"body"`
	} `json:"text"`
	Image *struct {
		Caption  string `json:"caption"`
		MimeType string `json:"mime_type"`
		ID       string `json:"id"`
	} `json:"image"`
	Video *struct {
		Caption  string `json:"caption"`
		MimeType string `json:"mime_type"`
		ID       string `json:"id"`
	} `json:"video"`
	Document *struct {
		Caption  string `json:"caption"`
		MimeType string `json:"mime_type"`
		Filename string `json:"filename"`
		ID       string `json:"id"`
	} `json:"document"`
	Audio *struct {
		MimeType string `json:"mime_type"`
		ID       string `json:"id"`
	} `json:"audio"`
	Interactive map[string]interface{} `json:"interactive"`
	Button      map[string]interface{} `json:"button"`
}

type cloudWebhookStatus struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
}

func (s *Server) handleWhatsAppCloudVerify(c *fiber.Ctx) error {
	mode := c.Query("hub.mode")
	token := c.Query("hub.verify_token")
	challenge := c.Query("hub.challenge")
	expected := strings.TrimSpace(os.Getenv("WHATSAPP_CLOUD_VERIFY_TOKEN"))
	if expected == "" {
		return c.Status(fiber.StatusForbidden).SendString("verify token is not configured")
	}
	if mode == "subscribe" && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1 {
		return c.SendString(challenge)
	}
	return c.SendStatus(fiber.StatusForbidden)
}

func (s *Server) handleWhatsAppCloudWebhook(c *fiber.Ctx) error {
	raw := append([]byte(nil), c.Body()...)
	if len(raw) == 0 {
		return c.JSON(fiber.Map{"success": true})
	}

	var payload cloudWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[WHATSAPP_API] invalid webhook payload: %v", err)
		return c.JSON(fiber.Map{"success": true})
	}

	if err := s.processWhatsAppCloudWebhook(c.Context(), payload); err != nil {
		log.Printf("[WHATSAPP_API] webhook processing error: %v", err)
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) processWhatsAppCloudWebhook(ctx context.Context, payload cloudWebhookPayload) error {
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			phoneNumberID := change.Value.Metadata.PhoneNumberID
			changePayload, _ := json.Marshal(change.Value)

			device, err := s.repos.WhatsAppAPI.GetCloudDeviceByPhoneNumberID(ctx, phoneNumberID)
			if err != nil {
				return err
			}

			var accountID *uuid.UUID
			var deviceID *uuid.UUID
			if device != nil {
				accountID = &device.AccountID
				deviceID = &device.ID
				_ = s.repos.WhatsAppAPI.UpdateDeviceWebhookStatus(ctx, device.ID, "receiving")
			}

			contactNames := map[string]string{}
			for _, contact := range change.Value.Contacts {
				contactNames[contact.WAID] = contact.Profile.Name
			}

			if len(change.Value.Messages) == 0 && len(change.Value.Statuses) == 0 {
				event := &domain.WhatsAppWebhookEvent{
					AccountID:     accountID,
					DeviceID:      deviceID,
					PhoneNumberID: phoneNumberID,
					EventID:       fmt.Sprintf("%s:%s:%d", phoneNumberID, change.Field, time.Now().UnixNano()),
					EventType:     defaultString(change.Field, "change"),
					Payload:       changePayload,
					Processed:     device != nil,
				}
				if device == nil {
					event.ErrorMessage = strPtr("Cloud API channel not configured for phone_number_id")
				}
				if err := s.repos.WhatsAppAPI.CreateWebhookEvent(ctx, event); err != nil {
					return err
				}
			}

			for _, message := range change.Value.Messages {
				event := &domain.WhatsAppWebhookEvent{
					AccountID:     accountID,
					DeviceID:      deviceID,
					PhoneNumberID: phoneNumberID,
					EventID:       defaultString(message.ID, fmt.Sprintf("%s:%s:%s", phoneNumberID, message.From, message.Timestamp)),
					EventType:     "message_received",
					Payload:       changePayload,
				}
				if device == nil {
					event.ErrorMessage = strPtr("Cloud API channel not configured for phone_number_id")
				} else if !device.ReceiveMessages {
					event.Processed = true
					event.ErrorMessage = strPtr("receive_messages disabled for channel")
				} else if err := s.processCloudAPIMessage(ctx, device, contactNames[message.From], message); err != nil {
					event.ErrorMessage = strPtr(err.Error())
				} else {
					event.Processed = true
				}
				if err := s.repos.WhatsAppAPI.CreateWebhookEvent(ctx, event); err != nil {
					return err
				}
			}

			for _, status := range change.Value.Statuses {
				eventType := "message_status"
				if status.Status != "" {
					eventType = "message_status_" + status.Status
				}
				event := &domain.WhatsAppWebhookEvent{
					AccountID:     accountID,
					DeviceID:      deviceID,
					PhoneNumberID: phoneNumberID,
					EventID:       fmt.Sprintf("%s:%s", status.ID, defaultString(status.Status, "status")),
					EventType:     eventType,
					Payload:       changePayload,
					Processed:     device != nil,
				}
				if device == nil {
					event.ErrorMessage = strPtr("Cloud API channel not configured for phone_number_id")
				}
				if err := s.repos.WhatsAppAPI.CreateWebhookEvent(ctx, event); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Server) processCloudAPIMessage(ctx context.Context, device *domain.Device, contactName string, message cloudWebhookMessage) error {
	phone := kommo.NormalizePhone(message.From)
	if phone == "" {
		phone = message.From
	}
	jid := phone + "@s.whatsapp.net"
	if contactName == "" {
		contactName = phone
	}

	contact, _ := s.repos.Contact.GetOrCreate(ctx, device.AccountID, &device.ID, jid, phone, contactName, contactName, false)
	if contact != nil {
		_ = s.repos.Contact.SyncToLead(ctx, contact)
	}

	chat, err := s.repos.Chat.GetOrCreate(ctx, device.AccountID, device.ID, jid, contactName)
	if err != nil {
		return fmt.Errorf("failed to get/create chat: %w", err)
	}

	body, msgType, mediaMimetype, mediaFilename := cloudMessageBody(message)
	timestamp := parseCloudTimestamp(message.Timestamp)
	provider := domain.DeviceProviderWhatsAppCloudAPI
	status := "received"
	dbMessage := &domain.Message{
		AccountID:     device.AccountID,
		DeviceID:      &device.ID,
		ChatID:        chat.ID,
		MessageID:     message.ID,
		FromJID:       strPtr(jid),
		FromName:      strPtr(contactName),
		Body:          strPtr(body),
		MessageType:   strPtr(msgType),
		MediaMimetype: mediaMimetype,
		MediaFilename: mediaFilename,
		IsFromMe:      false,
		Status:        &status,
		Provider:      &provider,
		Timestamp:     timestamp,
	}
	if err := s.repos.Message.Create(ctx, dbMessage); err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to save message: %w", err)
	}
	_ = s.repos.Chat.UpdateLastMessage(ctx, chat.ID, body, timestamp, true)
	_ = s.repos.WhatsAppAPI.UpdateChatServiceWindow(ctx, chat.ID, provider, true, timestamp)

	lead, _ := s.repos.Lead.GetByJID(ctx, device.AccountID, jid)
	if lead == nil {
		newLead := &domain.Lead{
			AccountID: device.AccountID,
			ContactID: func() *uuid.UUID {
				if contact != nil {
					return &contact.ID
				}
				return nil
			}(),
			JID:    jid,
			Name:   strPtr(contactName),
			Phone:  strPtr(phone),
			Status: strPtr(domain.LeadStatusNew),
			Source: strPtr("whatsapp_api"),
		}
		if pipelineID, stageID, err := s.repos.Pipeline.ResolveIncomingLeadDestination(ctx, device.AccountID); err == nil {
			newLead.PipelineID = pipelineID
			newLead.StageID = stageID
		}
		_ = s.repos.Lead.Create(ctx, newLead)
	}

	s.invalidateChatsCache(device.AccountID)
	if s.hub != nil {
		s.hub.BroadcastToAccount(device.AccountID, ws.EventNewMessage, map[string]interface{}{
			"chat_id": chat.ID.String(),
			"message": dbMessage,
		})
		s.hub.BroadcastToAccount(device.AccountID, ws.EventChatUpdate, map[string]interface{}{
			"chat_id": chat.ID.String(),
		})
	}
	return nil
}

func (s *Server) handleWhatsAppAPIOverview(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	overview, err := s.repos.WhatsAppAPI.GetOverview(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "overview": overview})
}

func (s *Server) handleListWhatsAppTemplates(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	templates, err := s.repos.WhatsAppAPI.ListTemplates(c.Context(), accountID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if templates == nil {
		templates = []*domain.WhatsAppMessageTemplate{}
	}
	return c.JSON(fiber.Map{"success": true, "templates": templates})
}

func (s *Server) handleCreateWhatsAppTemplate(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	var req struct {
		DeviceID   *string     `json:"device_id"`
		Name       string      `json:"name"`
		Language   string      `json:"language"`
		Category   string      `json:"category"`
		Status     string      `json:"status"`
		Components interface{} `json:"components"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	if strings.TrimSpace(req.Name) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "name is required"})
	}
	deviceID, err := parseOptionalUUID(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device_id"})
	}
	components := marshalJSONDefault(req.Components, "[]")
	template := &domain.WhatsAppMessageTemplate{
		AccountID:  accountID,
		DeviceID:   deviceID,
		Name:       strings.TrimSpace(req.Name),
		Language:   defaultString(req.Language, "es"),
		Category:   defaultString(req.Category, "UTILITY"),
		Status:     defaultString(req.Status, domain.WhatsAppTemplateStatusDraft),
		Components: components,
	}
	if err := s.repos.WhatsAppAPI.CreateTemplate(c.Context(), template); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "template": template})
}

func (s *Server) handleUpdateWhatsAppTemplate(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid template ID"})
	}
	existing, err := s.repos.WhatsAppAPI.GetTemplateByID(c.Context(), id, accountID)
	if err != nil || existing == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": "Template not found"})
	}
	var req struct {
		DeviceID   *string     `json:"device_id"`
		Name       string      `json:"name"`
		Language   string      `json:"language"`
		Category   string      `json:"category"`
		Status     string      `json:"status"`
		Components interface{} `json:"components"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid request"})
	}
	deviceID, err := parseOptionalUUID(req.DeviceID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid device_id"})
	}
	existing.DeviceID = deviceID
	existing.Name = strings.TrimSpace(req.Name)
	existing.Language = defaultString(req.Language, existing.Language)
	existing.Category = defaultString(req.Category, existing.Category)
	existing.Status = defaultString(req.Status, existing.Status)
	existing.Components = marshalJSONDefault(req.Components, string(existing.Components))
	if existing.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "name is required"})
	}
	if err := s.repos.WhatsAppAPI.UpdateTemplate(c.Context(), existing); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "template": existing})
}

func (s *Server) handleDeleteWhatsAppTemplate(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": "Invalid template ID"})
	}
	if err := s.repos.WhatsAppAPI.DeleteTemplate(c.Context(), id, accountID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleListWhatsAppWebhookEvents(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	events, err := s.repos.WhatsAppAPI.ListWebhookEvents(c.Context(), accountID, c.QueryInt("limit", 50))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if events == nil {
		events = []*domain.WhatsAppWebhookEvent{}
	}
	return c.JSON(fiber.Map{"success": true, "events": events})
}

func (s *Server) handleListWhatsAppWindows(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	windows, err := s.repos.WhatsAppAPI.ListConversationWindows(c.Context(), accountID, c.QueryInt("limit", 50))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": err.Error()})
	}
	if windows == nil {
		windows = []*domain.WhatsAppConversationWindow{}
	}
	return c.JSON(fiber.Map{"success": true, "windows": windows})
}

func parseOptionalUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func marshalJSONDefault(value interface{}, fallback string) json.RawMessage {
	if value == nil {
		return json.RawMessage(fallback)
	}
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return json.RawMessage(fallback)
	}
	return raw
}

func parseCloudTimestamp(value string) time.Time {
	if value == "" {
		return time.Now()
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 {
		return time.Now()
	}
	return time.Unix(seconds, 0)
}

func cloudMessageBody(message cloudWebhookMessage) (string, string, *string, *string) {
	msgType := message.Type
	if msgType == "" {
		msgType = domain.MessageTypeText
	}
	var body string
	var mimetype *string
	var filename *string
	if message.Text != nil {
		body = message.Text.Body
	}
	if message.Image != nil {
		body = message.Image.Caption
		mimetype = strPtr(message.Image.MimeType)
	}
	if message.Video != nil {
		body = message.Video.Caption
		mimetype = strPtr(message.Video.MimeType)
	}
	if message.Document != nil {
		body = message.Document.Caption
		mimetype = strPtr(message.Document.MimeType)
		filename = strPtr(message.Document.Filename)
	}
	if message.Audio != nil {
		mimetype = strPtr(message.Audio.MimeType)
	}
	if body == "" && msgType != domain.MessageTypeText {
		body = fmt.Sprintf("[%s]", msgType)
	}
	return body, msgType, mimetype, filename
}
