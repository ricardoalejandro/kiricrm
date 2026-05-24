package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Account represents a tenant in the multi-tenant system
type Account struct {
	ID                     uuid.UUID  `json:"id"`
	Name                   string     `json:"name"`
	Slug                   string     `json:"slug"`
	Plan                   string     `json:"plan"`
	MaxDevices             int        `json:"max_devices"`
	MaxUsersOverride       *int       `json:"max_users_override,omitempty"`
	MaxUsersEffective      int        `json:"max_users_effective,omitempty"`
	StorageLimitBytes      int64      `json:"storage_limit_bytes"`
	IsActive               bool       `json:"is_active"`
	MCPEnabled             bool       `json:"mcp_enabled"`
	KommoEnabled           bool       `json:"kommo_enabled"`
	DefaultIncomingStageID *uuid.UUID `json:"default_incoming_stage_id,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`

	// Google Contacts integration
	GoogleEmail          *string    `json:"google_email,omitempty"`
	GoogleAccessToken    *string    `json:"-"`
	GoogleRefreshToken   *string    `json:"-"`
	GoogleContactGroupID *string    `json:"google_contact_group_id,omitempty"`
	GoogleConnectedAt    *time.Time `json:"google_connected_at,omitempty"`
	GoogleSyncLimit      int        `json:"google_sync_limit"`

	// Populated on demand
	UserCount       int `json:"user_count,omitempty"`
	DeviceCount     int `json:"device_count,omitempty"`
	ChatCount       int `json:"chat_count,omitempty"`
	GoogleSyncCount int `json:"google_sync_count,omitempty"`

	// Subscription snapshot populated from subscriptions when available.
	SubscriptionStatus string     `json:"subscription_status,omitempty"`
	TrialEndsAt        *time.Time `json:"trial_ends_at,omitempty"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end,omitempty"`
	GraceEndsAt        *time.Time `json:"grace_ends_at,omitempty"`
}

const (
	SubscriptionStatusTrialing   = "trialing"
	SubscriptionStatusActive     = "active"
	SubscriptionStatusPastDue    = "past_due"
	SubscriptionStatusGrace      = "grace"
	SubscriptionStatusSuspended  = "suspended"
	SubscriptionStatusCanceled   = "canceled"
	SubscriptionStatusIncomplete = "incomplete"
)

// Plan defines the commercial package available to an account.
type Plan struct {
	Code         string                     `json:"code"`
	Name         string                     `json:"name"`
	Description  string                     `json:"description"`
	TrialDays    int                        `json:"trial_days"`
	IsPublic     bool                       `json:"is_public"`
	SortOrder    int                        `json:"sort_order"`
	Entitlements map[string]json.RawMessage `json:"entitlements,omitempty"`
	CreatedAt    time.Time                  `json:"created_at"`
	UpdatedAt    time.Time                  `json:"updated_at"`
}

// PlanEntitlement stores a typed JSON value for a plan limit or feature flag.
type PlanEntitlement struct {
	PlanCode  string          `json:"plan_code"`
	Key       string          `json:"key"`
	ValueJSON json.RawMessage `json:"value_json"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Subscription tracks the lifecycle of one account's SaaS access.
type Subscription struct {
	ID                     uuid.UUID       `json:"id"`
	AccountID              uuid.UUID       `json:"account_id"`
	PlanCode               string          `json:"plan_code"`
	Status                 string          `json:"status"`
	TrialStartedAt         *time.Time      `json:"trial_started_at,omitempty"`
	TrialEndsAt            *time.Time      `json:"trial_ends_at,omitempty"`
	CurrentPeriodStart     *time.Time      `json:"current_period_start,omitempty"`
	CurrentPeriodEnd       *time.Time      `json:"current_period_end,omitempty"`
	GraceEndsAt            *time.Time      `json:"grace_ends_at,omitempty"`
	CanceledAt             *time.Time      `json:"canceled_at,omitempty"`
	SuspendedAt            *time.Time      `json:"suspended_at,omitempty"`
	BillingProvider        string          `json:"billing_provider,omitempty"`
	ProviderCustomerID     string          `json:"provider_customer_id,omitempty"`
	ProviderSubscriptionID string          `json:"provider_subscription_id,omitempty"`
	Metadata               json.RawMessage `json:"metadata,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// SubscriptionUsage is an operational snapshot used for billing/admin screens.
type SubscriptionUsage struct {
	Users    int `json:"users"`
	Devices  int `json:"devices"`
	Contacts int `json:"contacts"`
	Leads    int `json:"leads"`
	Chats    int `json:"chats"`
}

// SubscriptionOverview combines commercial state with current account usage.
type SubscriptionOverview struct {
	Subscription *Subscription     `json:"subscription"`
	Plan         *Plan             `json:"plan"`
	Usage        SubscriptionUsage `json:"usage"`
	DaysLeft     *int              `json:"days_left,omitempty"`
	Entitlements map[string]any    `json:"entitlements,omitempty"`
	IsActive     bool              `json:"is_active"`
	IsTrial      bool              `json:"is_trial"`
	IsSuspended  bool              `json:"is_suspended"`
}

// User represents a user in the system
type User struct {
	ID               uuid.UUID `json:"id"`
	AccountID        uuid.UUID `json:"account_id"`
	Username         string    `json:"username"`
	Email            string    `json:"email"`
	PasswordHash     string    `json:"-"`
	DisplayName      string    `json:"display_name"`
	Role             string    `json:"role"` // super_admin, admin, agent
	IsAdmin          bool      `json:"is_admin"`
	IsSuperAdmin     bool      `json:"is_super_admin"`
	IsActive         bool      `json:"is_active"`
	GroqAPIKey       string    `json:"-"`
	ErosModel        string    `json:"eros_model,omitempty"`
	ErosRole         string    `json:"eros_role,omitempty"`
	ErosInstructions string    `json:"eros_instructions,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	// Populated on demand
	AccountName string        `json:"account_name,omitempty"`
	Accounts    []UserAccount `json:"accounts,omitempty"`
}

// User role constants
const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleAgent      = "agent"
)

// Permission module constants
const (
	PermChats        = "chats"
	PermContacts     = "contacts"
	PermPrograms     = "programs"
	PermDevices      = "devices"
	PermLeads        = "leads"
	PermEvents       = "events"
	PermBroadcasts   = "broadcasts"
	PermTags         = "tags"
	PermSettings     = "settings"
	PermIntegrations = "integrations"
	PermAutomations  = "automations"
	PermBots         = "bots"
	PermSurveys      = "surveys"
	PermDynamics     = "dynamics"
	PermTasks        = "tasks"
	PermDocuments    = "documents"
	PermAll          = "*"
)

// AllPermissions contains all available permission modules in display order
var AllPermissions = []string{
	PermChats, PermContacts, PermLeads, PermPrograms,
	PermAutomations, PermBots, PermDevices, PermEvents,
	PermBroadcasts, PermSurveys, PermTasks, PermDynamics,
	PermDocuments, PermTags, PermSettings, PermIntegrations,
}

// Role represents a named set of module permissions
type Role struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserAccount represents a user's assignment to an account (many-to-many)
type UserAccount struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	AccountID uuid.UUID  `json:"account_id"`
	Role      string     `json:"role"`
	RoleID    *uuid.UUID `json:"role_id,omitempty"`
	IsDefault bool       `json:"is_default"`
	CreatedAt time.Time  `json:"created_at"`

	// Populated on demand
	AccountName       string   `json:"account_name,omitempty"`
	AccountSlug       string   `json:"account_slug,omitempty"`
	AccountMCPEnabled bool     `json:"account_mcp_enabled,omitempty"`
	RoleName          string   `json:"role_name,omitempty"`
	Permissions       []string `json:"permissions,omitempty"`
}

// Integration providers and scopes.
const (
	IntegrationProviderKommo     = "kommo"
	IntegrationScopeGlobal       = "global"
	IntegrationScopeMultiAccount = "multi_account"
	IntegrationScopeAccount      = "account"

	IntegrationStatusActive = "active"
	IntegrationStatusPaused = "paused"
	IntegrationStatusError  = "error"
)

// IntegrationInstance represents one configured external integration instance.
// For Kommo, one instance maps to one Kommo license/subdomain and may serve many accounts.
type IntegrationInstance struct {
	ID            uuid.UUID       `json:"id"`
	Provider      string          `json:"provider"`
	Scope         string          `json:"scope"`
	Name          string          `json:"name"`
	Status        string          `json:"status"`
	IsActive      bool            `json:"is_active"`
	Subdomain     string          `json:"subdomain,omitempty"`
	ClientID      string          `json:"client_id,omitempty"`
	ClientSecret  string          `json:"-"`
	AccessToken   string          `json:"-"`
	RefreshToken  string          `json:"-"`
	RedirectURI   string          `json:"redirect_uri,omitempty"`
	WebhookSecret string          `json:"-"`
	Config        json.RawMessage `json:"config,omitempty"`
	LastSyncAt    *time.Time      `json:"last_sync_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`

	Accounts []IntegrationInstanceAccount `json:"accounts,omitempty"`
}

// IntegrationInstanceAccount links a shared integration instance to a Clarin account.
type IntegrationInstanceAccount struct {
	ID                    uuid.UUID  `json:"id"`
	IntegrationInstanceID uuid.UUID  `json:"integration_instance_id"`
	AccountID             uuid.UUID  `json:"account_id"`
	Enabled               bool       `json:"enabled"`
	LastSyncedAt          *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`

	AccountName string `json:"account_name,omitempty"`
	AccountSlug string `json:"account_slug,omitempty"`
}

// Device represents a WhatsApp connection
type Device struct {
	ID                  uuid.UUID       `json:"id"`
	AccountID           uuid.UUID       `json:"account_id"`
	Name                *string         `json:"name,omitempty"`
	Phone               *string         `json:"phone,omitempty"`
	JID                 *string         `json:"jid,omitempty"`
	Status              *string         `json:"status,omitempty"` // disconnected, connecting, connected, logged_out
	QRCode              *string         `json:"qr_code,omitempty"`
	ReceiveMessages     bool            `json:"receive_messages"`
	Provider            *string         `json:"provider,omitempty"`
	WABAID              *string         `json:"waba_id,omitempty"`
	PhoneNumberID       *string         `json:"phone_number_id,omitempty"`
	APIDisplayPhone     *string         `json:"api_display_phone,omitempty"`
	APIWebhookStatus    *string         `json:"api_webhook_status,omitempty"`
	APIBillingStatus    *string         `json:"api_billing_status,omitempty"`
	APISendingEnabled   bool            `json:"api_sending_enabled"`
	APITemplatesEnabled bool            `json:"api_templates_enabled"`
	Capabilities        json.RawMessage `json:"capabilities,omitempty"`
	LastSeenAt          *time.Time      `json:"last_seen_at,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// Device provider constants
const (
	DeviceProviderWhatsAppWeb      = "whatsapp_web"
	DeviceProviderWhatsAppCloudAPI = "whatsapp_cloud_api"
)

// DeviceStatus constants
const (
	DeviceStatusDisconnected = "disconnected"
	DeviceStatusConnecting   = "connecting"
	DeviceStatusConnected    = "connected"
	DeviceStatusLoggedOut    = "logged_out"
)

// Contact represents a WhatsApp contact
type Contact struct {
	ID              uuid.UUID  `json:"id"`
	AccountID       uuid.UUID  `json:"account_id"`
	DeviceID        *uuid.UUID `json:"device_id,omitempty"`
	JID             string     `json:"jid"`
	Phone           *string    `json:"phone,omitempty"`
	Name            *string    `json:"name,omitempty"`
	LastName        *string    `json:"last_name,omitempty"`
	ShortName       *string    `json:"short_name,omitempty"`
	CustomName      *string    `json:"custom_name,omitempty"`
	PushName        *string    `json:"push_name,omitempty"`
	AvatarURL       *string    `json:"avatar_url,omitempty"`
	AvatarCheckedAt *time.Time `json:"avatar_checked_at,omitempty"`
	Email           *string    `json:"email,omitempty"`
	Company         *string    `json:"company,omitempty"`
	Age             *int       `json:"age,omitempty"`
	DNI             *string    `json:"dni,omitempty"`
	BirthDate       *time.Time `json:"birth_date,omitempty"`
	Address         *string    `json:"address,omitempty"`
	Distrito        *string    `json:"distrito,omitempty"`
	Ocupacion       *string    `json:"ocupacion,omitempty"`
	Tags            []string   `json:"tags,omitempty"`
	Notes           *string    `json:"notes,omitempty"`
	Source          *string    `json:"source,omitempty"`
	IsGroup         bool       `json:"is_group"`
	KommoID         *int64     `json:"kommo_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastActivity    *time.Time `json:"last_activity,omitempty"`
	LeadCount       int        `json:"lead_count"`

	// Google Contacts sync
	GoogleSync         bool       `json:"google_sync"`
	GoogleResourceName *string    `json:"google_resource_name,omitempty"`
	GoogleSyncedAt     *time.Time `json:"google_synced_at,omitempty"`
	GoogleSyncError    *string    `json:"google_sync_error,omitempty"`

	// Relations (populated on demand)
	DeviceNames       []ContactDeviceName `json:"device_names,omitempty"`
	StructuredTags    []*Tag              `json:"structured_tags,omitempty"`
	ExtraPhones       []ContactPhone      `json:"extra_phones,omitempty"`
	CustomFieldValues []*CustomFieldValue `json:"custom_field_values,omitempty"`
}

// ContactPhone represents an additional phone number for a contact
type ContactPhone struct {
	ID        uuid.UUID `json:"id"`
	ContactID uuid.UUID `json:"contact_id"`
	Phone     string    `json:"phone"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// DisplayName returns the best available name for the contact
func (c *Contact) DisplayName() string {
	if c.CustomName != nil && *c.CustomName != "" {
		return *c.CustomName
	}
	if c.Name != nil && *c.Name != "" {
		return *c.Name
	}
	if c.PushName != nil && *c.PushName != "" {
		return *c.PushName
	}
	if c.Phone != nil && *c.Phone != "" {
		return *c.Phone
	}
	return c.JID
}

// ContactDeviceName stores the name a device has for a contact
type ContactDeviceName struct {
	ID           uuid.UUID `json:"id"`
	ContactID    uuid.UUID `json:"contact_id"`
	DeviceID     uuid.UUID `json:"device_id"`
	Name         *string   `json:"name,omitempty"`
	PushName     *string   `json:"push_name,omitempty"`
	BusinessName *string   `json:"business_name,omitempty"`
	SyncedAt     time.Time `json:"synced_at"`

	// Populated on demand
	DeviceName *string `json:"device_name,omitempty"`
}

// ContactFilter defines filter options for listing contacts
type ContactFilter struct {
	Search             string
	DeviceID           *uuid.UUID
	HasPhone           bool
	IsGroup            bool
	Tags               []string
	TagIDs             []uuid.UUID
	TagNames           []string
	ExcludeTagNames    []string
	TagMode            string      // OR or AND
	MatchingContactIDs []uuid.UUID // pre-computed from formula
	CfFilterContactIDs []uuid.UUID // pre-computed from custom field filters
	DateField          string
	DateFrom           string
	DateTo             string
	SortBy             string // name, lead_count, created_at
	SortOrder          string // asc, desc
	Limit              int
	Offset             int
}

// Chat represents a conversation
type Chat struct {
	ID                             uuid.UUID  `json:"id"`
	AccountID                      uuid.UUID  `json:"account_id"`
	DeviceID                       *uuid.UUID `json:"device_id,omitempty"`
	ContactID                      *uuid.UUID `json:"contact_id,omitempty"`
	JID                            string     `json:"jid"`
	Name                           *string    `json:"name,omitempty"`
	LastMessage                    *string    `json:"last_message,omitempty"`
	LastMessageAt                  *time.Time `json:"last_message_at,omitempty"`
	UnreadCount                    int        `json:"unread_count"`
	IsArchived                     bool       `json:"is_archived"`
	IsPinned                       bool       `json:"is_pinned"`
	LastInboundAt                  *time.Time `json:"last_inbound_at,omitempty"`
	LastOutboundAt                 *time.Time `json:"last_outbound_at,omitempty"`
	CustomerServiceWindowExpiresAt *time.Time `json:"customer_service_window_expires_at,omitempty"`
	LastMessageProvider            *string    `json:"last_message_provider,omitempty"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`

	// Device info (populated on demand)
	DeviceName   *string `json:"device_name,omitempty"`
	DevicePhone  *string `json:"device_phone,omitempty"`
	DeviceStatus *string `json:"device_status,omitempty"`

	// Contact info (populated via JOIN)
	ContactPhone      *string `json:"contact_phone,omitempty"`
	ContactAvatarURL  *string `json:"contact_avatar_url,omitempty"`
	ContactCustomName *string `json:"contact_custom_name,omitempty"`
	ContactName       *string `json:"contact_name,omitempty"`

	// Lead blocked status (populated via JOIN on JID)
	LeadIsBlocked bool `json:"lead_is_blocked"`

	// Relations (populated on demand)
	Contact  *Contact   `json:"contact,omitempty"`
	Messages []*Message `json:"messages,omitempty"`
}

// ChatFilter defines filter options for listing chats
type ChatFilter struct {
	DeviceIDs  []uuid.UUID
	TagIDs     []uuid.UUID
	UnreadOnly bool
	Archived   bool
	Search     string
	Limit      int
	Offset     int

	// Reaction-based filtering
	HasReaction    bool       // when true, only chats with at least one reaction matching the criteria below
	ReactionFromMe *bool      // nil = either, true = operator's reactions, false = client's reactions
	ReactionEmojis []string   // empty = any emoji, otherwise whitelist
	ReactionSince  *time.Time // optional lower bound on reaction timestamp
	ReactionUntil  *time.Time // optional upper bound on reaction timestamp
}

// ChatDetails contains full chat information with related data
type ChatDetails struct {
	Chat    *Chat    `json:"chat"`
	Contact *Contact `json:"contact,omitempty"`
	Lead    *Lead    `json:"lead,omitempty"`
}

// Message represents a WhatsApp message
type Message struct {
	ID            uuid.UUID  `json:"id"`
	AccountID     uuid.UUID  `json:"account_id"`
	DeviceID      *uuid.UUID `json:"device_id,omitempty"`
	ChatID        uuid.UUID  `json:"chat_id"`
	MessageID     string     `json:"message_id"`
	FromJID       *string    `json:"from_jid,omitempty"`
	FromName      *string    `json:"from_name,omitempty"`
	Body          *string    `json:"body,omitempty"`
	MessageType   *string    `json:"message_type,omitempty"` // text, image, video, audio, document, sticker, location, contact
	MediaURL      *string    `json:"media_url,omitempty"`
	MediaMimetype *string    `json:"media_mimetype,omitempty"`
	MediaFilename *string    `json:"media_filename,omitempty"`
	MediaSize     *int64     `json:"media_size,omitempty"`
	MediaAssetID  *uuid.UUID `json:"media_asset_id,omitempty"`
	MediaDeleted  bool       `json:"media_deleted"`
	IsFromMe      bool       `json:"is_from_me"`
	IsRead        bool       `json:"is_read"`
	IsRevoked     bool       `json:"is_revoked"`
	IsEdited      bool       `json:"is_edited"`
	IsViewOnce    bool       `json:"is_view_once"`
	Status        *string    `json:"status,omitempty"` // sent, delivered, read, failed
	Provider      *string    `json:"provider,omitempty"`
	TemplateName  *string    `json:"template_name,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
	CreatedAt     time.Time  `json:"created_at"`

	// Quoted/reply fields
	QuotedMessageID *string `json:"quoted_message_id,omitempty"`
	QuotedBody      *string `json:"quoted_body,omitempty"`
	QuotedSender    *string `json:"quoted_sender,omitempty"`

	// Location data (when message_type = location)
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`

	// Contact card data (when message_type = contact)
	ContactName  *string `json:"contact_name,omitempty"`
	ContactPhone *string `json:"contact_phone,omitempty"`
	ContactVCard *string `json:"contact_vcard,omitempty"`

	// Reactions (populated on demand)
	Reactions []*MessageReaction `json:"reactions,omitempty"`

	// Poll data (populated when message_type = poll)
	PollQuestion      *string       `json:"poll_question,omitempty"`
	PollOptions       []*PollOption `json:"poll_options,omitempty"`
	PollVotes         []*PollVote   `json:"poll_votes,omitempty"`
	PollMaxSelections int           `json:"poll_max_selections,omitempty"`
}

type MediaAsset struct {
	ID          uuid.UUID  `json:"id"`
	AccountID   uuid.UUID  `json:"account_id"`
	ContentHash string     `json:"content_hash"`
	ObjectKey   string     `json:"object_key"`
	MediaType   string     `json:"media_type"`
	ContentType string     `json:"content_type"`
	Filename    string     `json:"filename"`
	SizeBytes   int64      `json:"size_bytes"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// MessageType constants
const (
	MessageTypeText     = "text"
	MessageTypeImage    = "image"
	MessageTypeVideo    = "video"
	MessageTypeAudio    = "audio"
	MessageTypeDocument = "document"
	MessageTypeSticker  = "sticker"
	MessageTypeLocation = "location"
	MessageTypeContact  = "contact"
	MessageTypePoll     = "poll"
	MessageTypeReaction = "reaction"
)

// MessageReaction represents an emoji reaction on a message
type MessageReaction struct {
	ID              uuid.UUID `json:"id"`
	AccountID       uuid.UUID `json:"account_id"`
	ChatID          uuid.UUID `json:"chat_id"`
	TargetMessageID string    `json:"target_message_id"` // WhatsApp stanza ID of the reacted message
	SenderJID       string    `json:"sender_jid"`
	SenderName      *string   `json:"sender_name,omitempty"`
	Emoji           string    `json:"emoji"`
	IsFromMe        bool      `json:"is_from_me"`
	Timestamp       time.Time `json:"timestamp"`
	CreatedAt       time.Time `json:"created_at"`
}

// PollOption represents one option in a poll message
type PollOption struct {
	ID        uuid.UUID `json:"id"`
	MessageID uuid.UUID `json:"message_id"` // DB ID of the poll message
	Name      string    `json:"name"`
	VoteCount int       `json:"vote_count"`
}

// PollVote represents a user's vote on a poll
type PollVote struct {
	ID            uuid.UUID `json:"id"`
	MessageID     uuid.UUID `json:"message_id"` // DB ID of the poll message
	VoterJID      string    `json:"voter_jid"`
	SelectedNames []string  `json:"selected_names"` // Option names selected
	Timestamp     time.Time `json:"timestamp"`
}

// WhatsAppMessageTemplate represents an official WhatsApp Cloud API template.
type WhatsAppMessageTemplate struct {
	ID              uuid.UUID       `json:"id"`
	AccountID       uuid.UUID       `json:"account_id"`
	DeviceID        *uuid.UUID      `json:"device_id,omitempty"`
	Name            string          `json:"name"`
	Language        string          `json:"language"`
	Category        string          `json:"category"`
	Status          string          `json:"status"`
	Components      json.RawMessage `json:"components"`
	MetaTemplateID  *string         `json:"meta_template_id,omitempty"`
	RejectionReason *string         `json:"rejection_reason,omitempty"`
	LastSyncedAt    *time.Time      `json:"last_synced_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// WhatsAppWebhookEvent stores raw Cloud API webhooks for auditing and replay.
type WhatsAppWebhookEvent struct {
	ID            uuid.UUID       `json:"id"`
	AccountID     *uuid.UUID      `json:"account_id,omitempty"`
	DeviceID      *uuid.UUID      `json:"device_id,omitempty"`
	PhoneNumberID string          `json:"phone_number_id"`
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	Processed     bool            `json:"processed"`
	ErrorMessage  *string         `json:"error_message,omitempty"`
	ReceivedAt    time.Time       `json:"received_at"`
}

// WhatsAppConversationWindow summarizes the 24-hour official API service window.
type WhatsAppConversationWindow struct {
	ChatID                         uuid.UUID  `json:"chat_id"`
	AccountID                      uuid.UUID  `json:"account_id"`
	DeviceID                       *uuid.UUID `json:"device_id,omitempty"`
	JID                            string     `json:"jid"`
	Name                           *string    `json:"name,omitempty"`
	DeviceName                     *string    `json:"device_name,omitempty"`
	Provider                       string     `json:"provider"`
	LastInboundAt                  *time.Time `json:"last_inbound_at,omitempty"`
	LastOutboundAt                 *time.Time `json:"last_outbound_at,omitempty"`
	CustomerServiceWindowExpiresAt *time.Time `json:"customer_service_window_expires_at,omitempty"`
	CanReply                       bool       `json:"can_reply"`
}

// BotFlow represents a lead/chat bot definition independent from legacy automations.
type BotFlow struct {
	ID               uuid.UUID              `json:"id"`
	AccountID        uuid.UUID              `json:"account_id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Channel          string                 `json:"channel"`
	TriggerType      string                 `json:"trigger_type"`
	TriggerConfig    map[string]interface{} `json:"trigger_config"`
	Graph            json.RawMessage        `json:"graph"`
	IsActive         bool                   `json:"is_active"`
	IsPublished      bool                   `json:"is_published"`
	DraftVersion     int                    `json:"draft_version"`
	PublishedVersion int                    `json:"published_version"`
	ExecutionCount   int                    `json:"execution_count"`
	LastTriggeredAt  *time.Time             `json:"last_triggered_at,omitempty"`
	PublishedAt      *time.Time             `json:"published_at,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// BotFlowVersion stores immutable published bot versions.
type BotFlowVersion struct {
	ID        uuid.UUID       `json:"id"`
	FlowID    uuid.UUID       `json:"flow_id"`
	Version   int             `json:"version"`
	Graph     json.RawMessage `json:"graph"`
	CreatedBy *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// BotSession tracks a conversation currently handled by a bot.
type BotSession struct {
	ID            uuid.UUID              `json:"id"`
	AccountID     uuid.UUID              `json:"account_id"`
	FlowID        uuid.UUID              `json:"flow_id"`
	ChatID        *uuid.UUID             `json:"chat_id,omitempty"`
	ContactID     *uuid.UUID             `json:"contact_id,omitempty"`
	LeadID        *uuid.UUID             `json:"lead_id,omitempty"`
	Status        string                 `json:"status"`
	CurrentNodeID string                 `json:"current_node_id"`
	ContextData   map[string]interface{} `json:"context_data"`
	StartedAt     time.Time              `json:"started_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	EndedAt       *time.Time             `json:"ended_at,omitempty"`
}

// BotExecutionLog records a simulated or real bot step.
type BotExecutionLog struct {
	ID        uuid.UUID              `json:"id"`
	AccountID uuid.UUID              `json:"account_id"`
	FlowID    uuid.UUID              `json:"flow_id"`
	SessionID *uuid.UUID             `json:"session_id,omitempty"`
	NodeID    string                 `json:"node_id"`
	NodeType  string                 `json:"node_type"`
	Status    string                 `json:"status"`
	Input     map[string]interface{} `json:"input"`
	Output    map[string]interface{} `json:"output"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type BotSimulationStep struct {
	NodeID   string                 `json:"node_id"`
	NodeType string                 `json:"node_type"`
	Label    string                 `json:"label"`
	Status   string                 `json:"status"`
	Output   map[string]interface{} `json:"output"`
}

type BotSimulationResult struct {
	FlowID uuid.UUID           `json:"flow_id"`
	Steps  []BotSimulationStep `json:"steps"`
	Ended  bool                `json:"ended"`
	Error  string              `json:"error,omitempty"`
}

const (
	WhatsAppTemplateStatusDraft    = "draft"
	WhatsAppTemplateStatusPending  = "pending"
	WhatsAppTemplateStatusApproved = "approved"
	WhatsAppTemplateStatusRejected = "rejected"

	BotChannelWhatsApp        = "whatsapp"
	BotTriggerMessageReceived = "message_received"
	BotTriggerManual          = "manual"
	BotSessionActive          = "active"
	BotSessionCompleted       = "completed"
	BotSessionPaused          = "paused"
)

// Lead represents a potential customer
type Lead struct {
	ID             uuid.UUID              `json:"id"`
	AccountID      uuid.UUID              `json:"account_id"`
	ContactID      *uuid.UUID             `json:"contact_id,omitempty"`
	JID            string                 `json:"jid"`
	Name           *string                `json:"name,omitempty"`
	LastName       *string                `json:"last_name,omitempty"`
	ShortName      *string                `json:"short_name,omitempty"`
	Phone          *string                `json:"phone,omitempty"`
	Email          *string                `json:"email,omitempty"`
	Company        *string                `json:"company,omitempty"`
	Age            *int                   `json:"age,omitempty"`
	DNI            *string                `json:"dni,omitempty"`
	BirthDate      *time.Time             `json:"birth_date,omitempty"`
	Address        *string                `json:"address,omitempty"`
	Distrito       *string                `json:"distrito,omitempty"`
	Ocupacion      *string                `json:"ocupacion,omitempty"`
	Status         *string                `json:"status,omitempty"` // legacy, kept for backward compat
	PipelineID     *uuid.UUID             `json:"pipeline_id,omitempty"`
	StageID        *uuid.UUID             `json:"stage_id,omitempty"`
	Source         *string                `json:"source,omitempty"`
	Notes          *string                `json:"notes,omitempty"`
	Tags           []string               `json:"tags,omitempty"`
	CustomFields   map[string]interface{} `json:"custom_fields,omitempty"`
	AssignedTo     *uuid.UUID             `json:"assigned_to,omitempty"`
	KommoID        *int64                 `json:"kommo_id,omitempty"`
	IsArchived     bool                   `json:"is_archived"`
	ArchivedAt     *time.Time             `json:"archived_at,omitempty"`
	ArchiveReason  string                 `json:"archive_reason,omitempty"`
	IsBlocked      bool                   `json:"is_blocked"`
	BlockedAt      *time.Time             `json:"blocked_at,omitempty"`
	BlockReason    string                 `json:"block_reason,omitempty"`
	KommoDeletedAt *time.Time             `json:"kommo_deleted_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`

	// Relations (populated on demand)
	Contact           *Contact            `json:"contact,omitempty"`
	StructuredTags    []*Tag              `json:"structured_tags,omitempty"`
	CustomFieldValues []*CustomFieldValue `json:"custom_field_values,omitempty"`
	StageName         *string             `json:"stage_name,omitempty"`
	StageColor        *string             `json:"stage_color,omitempty"`
	StagePosition     *int                `json:"stage_position,omitempty"`
}

// LeadStatus constants
const (
	LeadStatusNew       = "new"
	LeadStatusContacted = "contacted"
	LeadStatusQualified = "qualified"
	LeadStatusProposal  = "proposal"
	LeadStatusWon       = "won"
	LeadStatusLost      = "lost"
)

// Pipeline represents a sales pipeline
type Pipeline struct {
	ID          uuid.UUID        `json:"id"`
	AccountID   uuid.UUID        `json:"account_id"`
	Name        string           `json:"name"`
	Description *string          `json:"description,omitempty"`
	IsDefault   bool             `json:"is_default"`
	KommoID     *int64           `json:"kommo_id,omitempty"`
	Stages      []*PipelineStage `json:"stages,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// PipelineStage represents a stage in a pipeline
type PipelineStage struct {
	ID         uuid.UUID `json:"id"`
	PipelineID uuid.UUID `json:"pipeline_id"`
	Name       string    `json:"name"`
	Color      string    `json:"color"`
	Position   int       `json:"position"`
	KommoID    *int64    `json:"kommo_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LeadCount  int       `json:"lead_count,omitempty"`
}

// LeadFilter defines filter options for listing leads
type LeadFilter struct {
	Search     string
	PipelineID *uuid.UUID
	StageID    *uuid.UUID
	TagIDs     []uuid.UUID
	Limit      int
	Offset     int
}

// Person represents a unified search result from contacts and leads
type Person struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Phone      string    `json:"phone,omitempty"`
	Email      string    `json:"email,omitempty"`
	SourceType string    `json:"source_type"` // "contact" or "lead"
	Tags       []*Tag    `json:"tags,omitempty"`
}

// Tag represents a global label with color
type Tag struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	KommoID   *int64    `json:"kommo_id,omitempty"`
	Negate    bool      `json:"negate,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Campaign represents a mass messaging campaign
type Campaign struct {
	ID              uuid.UUID              `json:"id"`
	AccountID       uuid.UUID              `json:"account_id"`
	DeviceID        uuid.UUID              `json:"device_id"`
	EventID         *uuid.UUID             `json:"event_id,omitempty"`
	Source          *string                `json:"source,omitempty"` // contacts, event
	Name            string                 `json:"name"`
	MessageTemplate string                 `json:"message_template"`
	MediaURL        *string                `json:"media_url,omitempty"`
	MediaType       *string                `json:"media_type,omitempty"` // text, image, video, document, audio
	Status          string                 `json:"status"`               // draft, scheduled, running, paused, completed, failed
	ScheduledAt     *time.Time             `json:"scheduled_at,omitempty"`
	StartedAt       *time.Time             `json:"started_at,omitempty"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	TotalRecipients int                    `json:"total_recipients"`
	SentCount       int                    `json:"sent_count"`
	FailedCount     int                    `json:"failed_count"`
	Settings        map[string]interface{} `json:"settings"`
	CreatedBy       *uuid.UUID             `json:"created_by,omitempty"`
	StartedBy       *uuid.UUID             `json:"started_by,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`

	// Populated on demand
	DeviceName    *string               `json:"device_name,omitempty"`
	CreatedByName *string               `json:"created_by_name,omitempty"`
	StartedByName *string               `json:"started_by_name,omitempty"`
	Attachments   []*CampaignAttachment `json:"attachments,omitempty"`
}

// CampaignAttachment represents a media file attached to a campaign
type CampaignAttachment struct {
	ID         uuid.UUID `json:"id"`
	CampaignID uuid.UUID `json:"campaign_id"`
	MediaURL   string    `json:"media_url"`
	MediaType  string    `json:"media_type"` // image, video, audio, document
	Caption    string    `json:"caption"`
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
}

// CampaignRecipient represents a single recipient in a campaign
type CampaignRecipient struct {
	ID           uuid.UUID              `json:"id"`
	CampaignID   uuid.UUID              `json:"campaign_id"`
	ContactID    *uuid.UUID             `json:"contact_id,omitempty"`
	JID          string                 `json:"jid"`
	Name         *string                `json:"name,omitempty"`
	Phone        *string                `json:"phone,omitempty"`
	Status       string                 `json:"status"` // pending, sent, delivered, failed, skipped
	SentAt       *time.Time             `json:"sent_at,omitempty"`
	ErrorMessage *string                `json:"error_message,omitempty"`
	WaitTimeMs   *int                   `json:"wait_time_ms,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Campaign status constants
const (
	CampaignStatusDraft     = "draft"
	CampaignStatusScheduled = "scheduled"
	CampaignStatusRunning   = "running"
	CampaignStatusPaused    = "paused"
	CampaignStatusCompleted = "completed"
	CampaignStatusCancelled = "cancelled"
	CampaignStatusFailed    = "failed"
)

// EventPipeline represents a pipeline for tracking event participant progression
type EventPipeline struct {
	ID          uuid.UUID             `json:"id"`
	AccountID   uuid.UUID             `json:"account_id"`
	Name        string                `json:"name"`
	Description *string               `json:"description,omitempty"`
	IsDefault   bool                  `json:"is_default"`
	Stages      []*EventPipelineStage `json:"stages,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

// EventPipelineStage represents a stage in an event pipeline
type EventPipelineStage struct {
	ID               uuid.UUID `json:"id"`
	PipelineID       uuid.UUID `json:"pipeline_id"`
	Name             string    `json:"name"`
	Color            string    `json:"color"`
	Position         int       `json:"position"`
	CreatedAt        time.Time `json:"created_at"`
	ParticipantCount int       `json:"participant_count,omitempty"`
}

// Event represents an activity/event to track contact interactions
type Event struct {
	ID             uuid.UUID  `json:"id"`
	AccountID      uuid.UUID  `json:"account_id"`
	FolderID       *uuid.UUID `json:"folder_id,omitempty"`
	PipelineID     *uuid.UUID `json:"pipeline_id,omitempty"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	EventDate      *time.Time `json:"event_date,omitempty"`
	EventEnd       *time.Time `json:"event_end,omitempty"`
	Location       *string    `json:"location,omitempty"`
	Status         string     `json:"status"` // draft, active, completed, cancelled
	Color          string     `json:"color"`
	TagFormulaMode string     `json:"tag_formula_mode"` // OR, AND (used in simple mode)
	TagFormula     string     `json:"tag_formula"`      // text-based formula (advanced mode)
	TagFormulaType string     `json:"tag_formula_type"` // simple, advanced
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	// Populated on demand
	ParticipantCounts map[string]int `json:"participant_counts,omitempty"`
	StageCounts       map[string]int `json:"stage_counts,omitempty"`
	TotalParticipants int            `json:"total_participants"`
	PipelineName      *string        `json:"pipeline_name,omitempty"`
	Tags              []*Tag         `json:"tags,omitempty"`
}

// EventFolder represents a folder for organising events (Windows Explorer style)
type EventFolder struct {
	ID        uuid.UUID  `json:"id"`
	AccountID uuid.UUID  `json:"account_id"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	Name      string     `json:"name"`
	Color     string     `json:"color"`
	Icon      string     `json:"icon"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// Populated on demand
	EventCount int `json:"event_count,omitempty"`
}

// Event status constants
const (
	EventStatusDraft     = "draft"
	EventStatusActive    = "active"
	EventStatusCompleted = "completed"
	EventStatusCancelled = "cancelled"
)

// EventParticipant represents a contact participating in an event
type EventParticipant struct {
	ID             uuid.UUID  `json:"id"`
	EventID        uuid.UUID  `json:"event_id"`
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	LeadID         *uuid.UUID `json:"lead_id,omitempty"`
	StageID        *uuid.UUID `json:"stage_id,omitempty"`
	Name           string     `json:"name"`
	LastName       *string    `json:"last_name,omitempty"`
	ShortName      *string    `json:"short_name,omitempty"`
	Phone          *string    `json:"phone,omitempty"`
	Email          *string    `json:"email,omitempty"`
	Age            *int       `json:"age,omitempty"`
	Company        *string    `json:"company,omitempty"`
	DNI            *string    `json:"dni,omitempty"`
	BirthDate      *time.Time `json:"birth_date,omitempty"`
	Address        *string    `json:"address,omitempty"`
	Distrito       *string    `json:"distrito,omitempty"`
	Ocupacion      *string    `json:"ocupacion,omitempty"`
	Status         string     `json:"status"` // invited, contacted, confirmed, declined, attended, no_show
	Notes          *string    `json:"notes,omitempty"`
	NextAction     *string    `json:"next_action,omitempty"`
	NextActionDate *time.Time `json:"next_action_date,omitempty"`
	InvitedAt      *time.Time `json:"invited_at,omitempty"`
	ConfirmedAt    *time.Time `json:"confirmed_at,omitempty"`
	AttendedAt     *time.Time `json:"attended_at,omitempty"`
	AutoTagSync    bool       `json:"auto_tag_sync"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	// Populated on demand
	LastInteraction *Interaction `json:"last_interaction,omitempty"`
	Tags            []*Tag       `json:"tags,omitempty"`
	StageName       *string      `json:"stage_name,omitempty"`
	StageColor      *string      `json:"stage_color,omitempty"`
	// Lead pipeline info (populated on demand)
	LeadPipelineID *uuid.UUID `json:"lead_pipeline_id,omitempty"`
	LeadStageID    *uuid.UUID `json:"lead_stage_id,omitempty"`
	LeadStageName  *string    `json:"lead_stage_name,omitempty"`
	LeadStageColor *string    `json:"lead_stage_color,omitempty"`
	// Lead archive/block status (populated on demand)
	IsArchived bool `json:"is_archived,omitempty"`
	IsBlocked  bool `json:"is_blocked,omitempty"`
	// Duplicate detection (populated on demand)
	DuplicateContact bool `json:"duplicate_contact,omitempty"`
}

// Participant status constants
const (
	ParticipantStatusInvited   = "invited"
	ParticipantStatusContacted = "contacted"
	ParticipantStatusConfirmed = "confirmed"
	ParticipantStatusDeclined  = "declined"
	ParticipantStatusAttended  = "attended"
	ParticipantStatusNoShow    = "no_show"
)

// Interaction represents a communication log entry with a contact
type Interaction struct {
	ID             uuid.UUID  `json:"id"`
	AccountID      uuid.UUID  `json:"account_id"`
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	LeadID         *uuid.UUID `json:"lead_id,omitempty"`
	EventID        *uuid.UUID `json:"event_id,omitempty"`
	ParticipantID  *uuid.UUID `json:"participant_id,omitempty"`
	Type           string     `json:"type"`                // call, whatsapp, note, email, meeting
	Direction      *string    `json:"direction,omitempty"` // inbound, outbound
	Outcome        *string    `json:"outcome,omitempty"`   // answered, no_answer, voicemail, busy, confirmed, declined, rescheduled, callback
	Notes          *string    `json:"notes,omitempty"`
	NextAction     *string    `json:"next_action,omitempty"`
	NextActionDate *time.Time `json:"next_action_date,omitempty"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	KommoCallSlot  *int       `json:"kommo_call_slot,omitempty"`

	// Populated on demand
	CreatedByName *string `json:"created_by_name,omitempty"`
	EventName     *string `json:"event_name,omitempty"`
}

// Interaction type constants
const (
	InteractionTypeCall     = "call"
	InteractionTypeWhatsApp = "whatsapp"
	InteractionTypeNote     = "note"
	InteractionTypeEmail    = "email"
	InteractionTypeMeeting  = "meeting"
)

// Interaction outcome constants
const (
	InteractionOutcomeAnswered    = "answered"
	InteractionOutcomeNoAnswer    = "no_answer"
	InteractionOutcomeVoicemail   = "voicemail"
	InteractionOutcomeBusy        = "busy"
	InteractionOutcomeConfirmed   = "confirmed"
	InteractionOutcomeDeclined    = "declined"
	InteractionOutcomeRescheduled = "rescheduled"
	InteractionOutcomeCallback    = "callback"
)

// TaskList represents a named grouping for tasks
type TaskList struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"`
	SortOrder int       `json:"sort_order"`
	CreatedBy uuid.UUID `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	TaskCount int       `json:"task_count"`
}

// Task represents a scheduled action (call, follow-up, meeting, reminder)
type Task struct {
	ID                 uuid.UUID  `json:"id"`
	AccountID          uuid.UUID  `json:"account_id"`
	CreatedBy          uuid.UUID  `json:"created_by"`
	AssignedTo         uuid.UUID  `json:"assigned_to"`
	Title              string     `json:"title"`
	Description        string     `json:"description,omitempty"`
	Type               string     `json:"type"` // call, whatsapp, meeting, reminder
	DueAt              *time.Time `json:"due_at,omitempty"`
	DueEndAt           *time.Time `json:"due_end_at,omitempty"`
	Priority           string     `json:"priority"` // low, medium, high, urgent
	Status             string     `json:"status"`   // pending, completed, overdue, cancelled
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	CompletedBy        *uuid.UUID `json:"completed_by,omitempty"`
	LeadID             *uuid.UUID `json:"lead_id,omitempty"`
	EventID            *uuid.UUID `json:"event_id,omitempty"`
	ProgramID          *uuid.UUID `json:"program_id,omitempty"`
	ContactID          *uuid.UUID `json:"contact_id,omitempty"`
	ListID             *uuid.UUID `json:"list_id,omitempty"`
	Starred            bool       `json:"starred"`
	SortOrder          int        `json:"sort_order"`
	RecurrenceRule     string     `json:"recurrence_rule,omitempty"`
	RecurrenceParentID *uuid.UUID `json:"recurrence_parent_id,omitempty"`
	ReminderMinutes    *int       `json:"reminder_minutes,omitempty"`
	Notes              string     `json:"notes,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Populated on demand (JOINs)
	AssignedToName string `json:"assigned_to_name,omitempty"`
	CreatedByName  string `json:"created_by_name,omitempty"`
	LeadName       string `json:"lead_name,omitempty"`
	EventName      string `json:"event_name,omitempty"`
	ProgramName    string `json:"program_name,omitempty"`
	ContactName    string `json:"contact_name,omitempty"`
	ListName       string `json:"list_name,omitempty"`

	// Subtask counts (populated via subqueries)
	SubtaskCount int `json:"subtask_count"`
	SubtaskDone  int `json:"subtask_done"`
}

// TaskReminder represents a scheduled reminder for a task
type TaskReminder struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	AccountID   uuid.UUID  `json:"account_id"`
	AssignedTo  uuid.UUID  `json:"assigned_to"`
	ReminderAt  time.Time  `json:"reminder_at"`
	Delivered   bool       `json:"delivered"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

// Subtask represents a sub-item within a task
type Subtask struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	AccountID   uuid.UUID  `json:"account_id"`
	Title       string     `json:"title"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	SortOrder   int        `json:"sort_order"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Task type constants
const (
	TaskTypeCall     = "call"
	TaskTypeWhatsApp = "whatsapp"
	TaskTypeMeeting  = "meeting"
	TaskTypeReminder = "reminder"
)

// Task priority constants
const (
	TaskPriorityLow    = "low"
	TaskPriorityMedium = "medium"
	TaskPriorityHigh   = "high"
	TaskPriorityUrgent = "urgent"
)

// Task status constants
const (
	TaskStatusPending   = "pending"
	TaskStatusCompleted = "completed"
	TaskStatusOverdue   = "overdue"
	TaskStatusCancelled = "cancelled"
)

// EventFilter defines filter options for listing events
type EventFilter struct {
	Search       string
	Status       string
	FolderFilter string // "": all events, "root": folder_id IS NULL, "<uuid>": specific folder
	DateFrom     *time.Time
	DateTo       *time.Time
	Limit        int
	Offset       int
}

// InteractionFilter defines filter options for listing interactions
type InteractionFilter struct {
	ContactID     *uuid.UUID
	EventID       *uuid.UUID
	ParticipantID *uuid.UUID
	Type          string
	Limit         int
	Offset        int
}

// QuickReply represents a canned/predefined response
type QuickReply struct {
	ID            uuid.UUID              `json:"id"`
	AccountID     uuid.UUID              `json:"account_id"`
	Shortcut      string                 `json:"shortcut"`
	Title         string                 `json:"title"`
	Body          string                 `json:"body"`
	MediaURL      string                 `json:"media_url"`
	MediaType     string                 `json:"media_type"`
	MediaFilename string                 `json:"media_filename"`
	Attachments   []QuickReplyAttachment `json:"attachments"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// QuickReplyAttachment represents a media attachment for a quick reply (up to 5)
type QuickReplyAttachment struct {
	ID            uuid.UUID `json:"id"`
	QuickReplyID  uuid.UUID `json:"quick_reply_id"`
	MediaURL      string    `json:"media_url"`
	MediaType     string    `json:"media_type"`
	MediaFilename string    `json:"media_filename"`
	Caption       string    `json:"caption"`
	Position      int       `json:"position"`
}

// Default campaign settings (anti-ban)
func DefaultCampaignSettings() map[string]interface{} {
	return map[string]interface{}{
		"min_delay_seconds":   8,
		"max_delay_seconds":   15,
		"batch_size":          25,
		"batch_pause_minutes": 2,
		"daily_limit":         1000,
		"active_hours_start":  "07:00",
		"active_hours_end":    "22:00",
		"randomize_message":   true,
		"simulate_typing":     true,
	}
}

// --- Programs (Courses, Workshops, etc.) ---

// ProgramFolder represents a folder for organising programs
type ProgramFolder struct {
	ID        uuid.UUID  `json:"id"`
	AccountID uuid.UUID  `json:"account_id"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	Name      string     `json:"name"`
	Color     string     `json:"color"`
	Icon      string     `json:"icon"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// Populated on demand
	ProgramCount int `json:"program_count,omitempty"`
}

// Program represents an educational program, course, or workshop
type Program struct {
	ID          uuid.UUID  `json:"id"`
	AccountID   uuid.UUID  `json:"account_id"`
	FolderID    *uuid.UUID `json:"folder_id,omitempty"`
	Type        string     `json:"type"` // course (default) | event
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Status      string     `json:"status"` // active, completed, archived
	Color       string     `json:"color"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// Schedule fields for recurring sessions (course type)
	ScheduleStartDate *time.Time `json:"schedule_start_date,omitempty"`
	ScheduleEndDate   *time.Time `json:"schedule_end_date,omitempty"`
	ScheduleDays      []int      `json:"schedule_days,omitempty"`       // 0=Sun, 1=Mon, ..., 6=Sat
	ScheduleStartTime *string    `json:"schedule_start_time,omitempty"` // "HH:MM" format
	ScheduleEndTime   *string    `json:"schedule_end_time,omitempty"`   // "HH:MM" format

	// Event-type fields (only used when Type == "event")
	PipelineID     *uuid.UUID `json:"pipeline_id,omitempty"`
	TagFormula     string     `json:"tag_formula,omitempty"`
	TagFormulaMode string     `json:"tag_formula_mode,omitempty"` // OR, AND
	TagFormulaType string     `json:"tag_formula_type,omitempty"` // simple, advanced
	EventDate      *time.Time `json:"event_date,omitempty"`
	EventEnd       *time.Time `json:"event_end,omitempty"`
	Location       *string    `json:"location,omitempty"`

	// Populated on demand
	ParticipantCount int            `json:"participant_count"`
	SessionCount     int            `json:"session_count"`
	PipelineName     *string        `json:"pipeline_name,omitempty"`
	StageCounts      map[string]int `json:"stage_counts,omitempty"`
}

// ProgramParticipant represents a contact enrolled in a program
type ProgramParticipant struct {
	ID          uuid.UUID  `json:"id"`
	ProgramID   uuid.UUID  `json:"program_id"`
	ContactID   uuid.UUID  `json:"contact_id"`
	LeadID      *uuid.UUID `json:"lead_id,omitempty"`
	StageID     *uuid.UUID `json:"stage_id,omitempty"`
	Status      string     `json:"status"` // active, dropped, completed
	EnrolledAt  time.Time  `json:"enrolled_at"`
	AutoTagSync bool       `json:"auto_tag_sync"`

	// Populated on demand
	ContactName  string  `json:"contact_name,omitempty"`
	ContactPhone *string `json:"contact_phone,omitempty"`
	StageName    *string `json:"stage_name,omitempty"`
	StageColor   *string `json:"stage_color,omitempty"`
}

// ProgramSession represents a single class or session within a program
type ProgramSession struct {
	ID        uuid.UUID `json:"id"`
	ProgramID uuid.UUID `json:"program_id"`
	Date      time.Time `json:"date"`
	Topic     *string   `json:"topic,omitempty"`
	StartTime *string   `json:"start_time,omitempty"` // "HH:MM" format
	EndTime   *string   `json:"end_time,omitempty"`   // "HH:MM" format
	Location  *string   `json:"location,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Populated on demand
	AttendanceStats map[string]int `json:"attendance_stats,omitempty"`
}

// ProgramAttendance represents a participant's attendance record for a session
type ProgramAttendance struct {
	ID            uuid.UUID `json:"id"`
	SessionID     uuid.UUID `json:"session_id"`
	ParticipantID uuid.UUID `json:"participant_id"`
	Status        string    `json:"status"` // present, absent, late, excused
	Notes         *string   `json:"notes,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Populated on demand
	ParticipantName  string  `json:"participant_name,omitempty"`
	ParticipantPhone *string `json:"participant_phone,omitempty"`
}

// Attendance status constants
const (
	AttendanceStatusPresent = "present"
	AttendanceStatusAbsent  = "absent"
	AttendanceStatusLate    = "late"
	AttendanceStatusExcused = "excused"
)

// WhatsAppCheckResult represents the result of checking if a phone is on WhatsApp
type WhatsAppCheckResult struct {
	Phone        string `json:"phone"`
	IsOnWhatsApp bool   `json:"is_on_whatsapp"`
	JID          string `json:"jid,omitempty"`
}

// ── Event Logbooks (Bitácora) ──────────────────────────────────────────

// EventLogbook represents a snapshot of an event's state on a specific date
type EventLogbook struct {
	ID                uuid.UUID              `json:"id"`
	EventID           uuid.UUID              `json:"event_id"`
	AccountID         uuid.UUID              `json:"account_id"`
	Date              time.Time              `json:"date"`
	Title             string                 `json:"title"`
	Status            string                 `json:"status"` // pending, completed
	GeneralNotes      string                 `json:"general_notes"`
	StageSnapshot     map[string]interface{} `json:"stage_snapshot"`
	TotalParticipants int                    `json:"total_participants"`
	CapturedAt        *time.Time             `json:"captured_at,omitempty"`
	CreatedBy         *uuid.UUID             `json:"created_by,omitempty"`
	SavedFilter       json.RawMessage        `json:"saved_filter,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`

	// Populated on demand
	Entries       []*EventLogbookEntry `json:"entries,omitempty"`
	CreatedByName *string              `json:"created_by_name,omitempty"`
}

// EventLogbookEntry represents a participant's state snapshot in a logbook
type EventLogbookEntry struct {
	ID            uuid.UUID  `json:"id"`
	LogbookID     uuid.UUID  `json:"logbook_id"`
	ParticipantID uuid.UUID  `json:"participant_id"`
	StageID       *uuid.UUID `json:"stage_id,omitempty"`
	StageName     string     `json:"stage_name"`
	StageColor    string     `json:"stage_color"`
	Notes         string     `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`

	// Populated on demand
	ParticipantName  string  `json:"participant_name,omitempty"`
	ParticipantPhone *string `json:"participant_phone,omitempty"`
}

// Logbook status constants
const (
	LogbookStatusPending   = "pending"
	LogbookStatusCompleted = "completed"
)

// APIKey represents an API key for MCP / external integrations
type APIKey struct {
	ID          uuid.UUID  `json:"id"`
	AccountID   uuid.UUID  `json:"account_id"`
	Name        string     `json:"name"`
	KeyHash     string     `json:"-"`
	KeyPrefix   string     `json:"key_prefix"`
	Permissions string     `json:"permissions"`
	IsActive    bool       `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ErosConversation represents a persistent chat conversation with Eros AI
type ErosConversation struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	UserID    uuid.UUID `json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Populated on demand
	Messages []ErosMessage `json:"messages,omitempty"`
}

// ErosMessage represents a single message in an Eros conversation
type ErosMessage struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	Role           string    `json:"role"` // "user" or "assistant"
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// AITokenLog represents a log of AI tokens consumed by a user
type AITokenLog struct {
	ID            uuid.UUID `json:"id"`
	AccountID     uuid.UUID `json:"account_id"`
	UserID        uuid.UUID `json:"user_id"`
	APIKeyPreview string    `json:"api_key_preview"`
	Model         string    `json:"model"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	TotalTokens   int       `json:"total_tokens"`
	CreatedAt     time.Time `json:"created_at"`
}

// ── Automations ───────────────────────────────────────────────────────────

// Automation trigger type constants
const (
	AutoTriggerLeadCreated      = "lead_created"
	AutoTriggerLeadStageChanged = "lead_stage_changed"
	AutoTriggerTagAssigned      = "tag_assigned"
	AutoTriggerTagRemoved       = "tag_removed"
	AutoTriggerMessageReceived  = "message_received"
	AutoTriggerManual           = "manual"
)

// Automation node type constants
const (
	AutoNodeSendWhatsApp = "send_whatsapp"
	AutoNodeChangeStage  = "change_stage"
	AutoNodeAssignTag    = "assign_tag"
	AutoNodeRemoveTag    = "remove_tag"
	AutoNodeDelay        = "delay"
	AutoNodeCondition    = "condition"
)

// Automation execution status constants
const (
	AutoExecPending   = "pending"
	AutoExecRunning   = "running"
	AutoExecPaused    = "paused"
	AutoExecCompleted = "completed"
	AutoExecFailed    = "failed"
)

// AutomationGraph is the ReactFlow-compatible graph stored as JSONB
type AutomationGraph struct {
	Nodes []AutomationNode `json:"nodes"`
	Edges []AutomationEdge `json:"edges"`
}

// AutomationNode represents a single node in the automation graph (ReactFlow format)
type AutomationNode struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`     // trigger, action, condition, delay
	Position map[string]float64     `json:"position"` // {x, y}
	Data     map[string]interface{} `json:"data"`     // node config: {nodeType, label, config}
}

// AutomationEdge represents a connection between nodes (ReactFlow format)
type AutomationEdge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle,omitempty"` // "true" or "false" for condition nodes
}

// Automation represents a workflow automation definition
type Automation struct {
	ID              uuid.UUID              `json:"id"`
	AccountID       uuid.UUID              `json:"account_id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	TriggerType     string                 `json:"trigger"`
	TriggerConfig   map[string]interface{} `json:"trigger_config"`
	Config          AutomationGraph        `json:"graph"`
	IsActive        bool                   `json:"is_active"`
	ExecutionCount  int                    `json:"execution_count"`
	LastTriggeredAt *time.Time             `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// AutomationExecution represents a single run of an automation for a lead
type AutomationExecution struct {
	ID             uuid.UUID              `json:"id"`
	AutomationID   uuid.UUID              `json:"automation_id"`
	AccountID      uuid.UUID              `json:"account_id"`
	LeadID         *uuid.UUID             `json:"lead_id,omitempty"`
	Status         string                 `json:"status"`
	CurrentNodeID  string                 `json:"current_node_id"`
	NextNodeID     string                 `json:"next_node_id,omitempty"`
	ResumeAt       *time.Time             `json:"resume_at,omitempty"`
	ConfigSnapshot *AutomationGraph       `json:"config_snapshot,omitempty"`
	ContextData    map[string]interface{} `json:"context_data"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	StartedAt      time.Time              `json:"started_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`

	// Populated on demand
	AutomationName string `json:"automation_name,omitempty"`
}

// AutomationExecutionLog records the result of each node execution
type AutomationExecutionLog struct {
	ID          uuid.UUID `json:"id"`
	ExecutionID uuid.UUID `json:"execution_id"`
	NodeID      string    `json:"node_id"`
	NodeType    string    `json:"node_type"`
	Status      string    `json:"status"` // success, failed, skipped
	DurationMs  int       `json:"duration_ms"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// AutomationStats summarizes an automation's execution history
type AutomationStats struct {
	TotalExecutions     int `json:"total_executions"`
	CompletedExecutions int `json:"completed"`
	FailedExecutions    int `json:"failed"`
	ActiveExecutions    int `json:"active"`
}

// ─── Surveys / Forms ──────────────────────────────────────────────────────────

// SurveyBranding holds visual customization for the public form
type SurveyBranding struct {
	LogoURL       string `json:"logo_url,omitempty"`
	BgColor       string `json:"bg_color,omitempty"`
	AccentColor   string `json:"accent_color,omitempty"`
	BgImageURL    string `json:"bg_image_url,omitempty"`
	FontFamily    string `json:"font_family,omitempty"`    // Inter, Poppins, Playfair Display, etc.
	TitleSize     string `json:"title_size,omitempty"`     // sm, md, lg, xl
	TextColor     string `json:"text_color,omitempty"`     // custom text color for titles
	ButtonStyle   string `json:"button_style,omitempty"`   // rounded, pill, square
	BgOverlay     string `json:"bg_overlay,omitempty"`     // overlay opacity: 0, 0.2, 0.4, 0.6
	QuestionAlign string `json:"question_align,omitempty"` // left, center
}

// Survey represents a form/survey that can be shared via a public link
type Survey struct {
	ID                  uuid.UUID      `json:"id"`
	AccountID           uuid.UUID      `json:"account_id"`
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	Slug                string         `json:"slug"`
	Status              string         `json:"status"` // draft, active, closed
	WelcomeTitle        string         `json:"welcome_title"`
	WelcomeDescription  string         `json:"welcome_description"`
	ThankYouTitle       string         `json:"thank_you_title"`
	ThankYouMessage     string         `json:"thank_you_message"`
	ThankYouRedirectURL string         `json:"thank_you_redirect_url"`
	Branding            SurveyBranding `json:"branding"`
	IsTemplate          bool           `json:"is_template"`
	CreatedBy           *uuid.UUID     `json:"created_by,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	// Populated on demand
	QuestionCount int `json:"question_count,omitempty"`
	ResponseCount int `json:"response_count,omitempty"`
}

// SurveyQuestionConfig holds type-specific configuration for a question
type SurveyQuestionConfig struct {
	Options      []string `json:"options,omitempty"`       // single_choice, multiple_choice
	MaxRating    int      `json:"max_rating,omitempty"`    // rating (default 5)
	LikertScale  int      `json:"likert_scale,omitempty"`  // likert scale points (default 5)
	LikertMin    string   `json:"likert_min,omitempty"`    // likert min label
	LikertMax    string   `json:"likert_max,omitempty"`    // likert max label
	AllowedTypes []string `json:"allowed_types,omitempty"` // file_upload mime types
	MaxSizeMB    int      `json:"max_size_mb,omitempty"`   // file_upload max size
	Placeholder  string   `json:"placeholder,omitempty"`   // text input placeholder
}

// SurveyLogicRule defines a conditional jump based on an answer value
type SurveyLogicRule struct {
	Value    string    `json:"value"`              // answer value to match
	Operator string    `json:"operator,omitempty"` // eq, neq, contains, gt, lt (default: eq)
	JumpTo   uuid.UUID `json:"jump_to"`            // question ID to jump to
}

// SurveyQuestion represents a single question in a survey
type SurveyQuestion struct {
	ID          uuid.UUID            `json:"id"`
	SurveyID    uuid.UUID            `json:"survey_id"`
	OrderIndex  int                  `json:"order_index"`
	Type        string               `json:"type"` // short_text, long_text, single_choice, multiple_choice, rating, likert, date, email, phone, file_upload
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Required    bool                 `json:"required"`
	Config      SurveyQuestionConfig `json:"config"`
	LogicRules  []SurveyLogicRule    `json:"logic_rules"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

// SurveyResponse represents a complete submission by one respondent
type SurveyResponse struct {
	ID              uuid.UUID  `json:"id"`
	SurveyID        uuid.UUID  `json:"survey_id"`
	AccountID       uuid.UUID  `json:"account_id"`
	RespondentToken string     `json:"respondent_token"`
	LeadID          *uuid.UUID `json:"lead_id,omitempty"`
	Source          string     `json:"source,omitempty"` // direct, ws, ig, email, qr
	IPAddress       string     `json:"-"`
	UserAgent       string     `json:"-"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	// Populated on demand
	Answers []SurveyAnswer `json:"answers,omitempty"`
}

// SurveyAnswer represents the answer to a single question within a response
type SurveyAnswer struct {
	ID         uuid.UUID `json:"id"`
	ResponseID uuid.UUID `json:"response_id"`
	QuestionID uuid.UUID `json:"question_id"`
	Value      string    `json:"value"`
	FileURL    string    `json:"file_url,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SurveyAnalytics holds aggregated data for a survey's results
type SurveyAnalytics struct {
	TotalResponses   int                   `json:"total_responses"`
	CompletionRate   float64               `json:"completion_rate"`
	AvgCompletionSec float64               `json:"avg_completion_seconds"`
	QuestionStats    []SurveyQuestionStats `json:"question_stats"`
}

// SurveyQuestionStats holds per-question aggregated stats
type SurveyQuestionStats struct {
	QuestionID   uuid.UUID `json:"question_id"`
	QuestionType string    `json:"question_type"`
	Title        string    `json:"title"`
	TotalAnswers int       `json:"total_answers"`
	// For choice-based questions
	OptionCounts map[string]int `json:"option_counts,omitempty"`
	// For numeric questions (rating, likert)
	Average      *float64       `json:"average,omitempty"`
	Distribution map[string]int `json:"distribution,omitempty"`
}

// ─── Dynamics (Interactive Activities) ────────────────────────────────────────

// DynamicConfig holds visual configuration for a dynamic activity
type DynamicConfig struct {
	Title            string `json:"title"`
	ScratchColor     string `json:"scratch_color"`
	ScratchThreshold int    `json:"scratch_threshold"`
	ScratchSound     bool   `json:"scratch_sound"`
	ShowConfetti     bool   `json:"show_confetti"`
	VictorySound     bool   `json:"victory_sound"`
	OverlayImageURL  string `json:"overlay_image_url"`
	BgColor          string `json:"bg_color"`
}

// Dynamic represents an interactive activity (e.g. scratch card)
type Dynamic struct {
	ID          uuid.UUID     `json:"id"`
	AccountID   uuid.UUID     `json:"account_id"`
	Type        string        `json:"type"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Description string        `json:"description"`
	Config      DynamicConfig `json:"config"`
	IsActive    bool          `json:"is_active"`
	ItemCount   int           `json:"item_count"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// DynamicItem represents a single item within a dynamic activity
type DynamicItem struct {
	ID          uuid.UUID   `json:"id"`
	DynamicID   uuid.UUID   `json:"dynamic_id"`
	OptionIDs   []uuid.UUID `json:"option_ids"`
	ImageURL    string      `json:"image_url"`
	ThoughtText string      `json:"thought_text"`
	Author      string      `json:"author"`
	Tipo        string      `json:"tipo"`
	FileSize    int64       `json:"file_size"`
	SortOrder   int         `json:"sort_order"`
	IsActive    bool        `json:"is_active"`
	CreatedAt   time.Time   `json:"created_at"`
}

// DynamicOption represents a selectable category for items within a dynamic
type DynamicOption struct {
	ID        uuid.UUID `json:"id"`
	DynamicID uuid.UUID `json:"dynamic_id"`
	Name      string    `json:"name"`
	Emoji     string    `json:"emoji"`
	SortOrder int       `json:"sort_order"`
	ItemCount int       `json:"item_count"`
	CreatedAt time.Time `json:"created_at"`
}

// DynamicLink represents a public link for a dynamic with its own WhatsApp config
type DynamicLink struct {
	ID                    uuid.UUID                `json:"id"`
	DynamicID             uuid.UUID                `json:"dynamic_id"`
	Slug                  string                   `json:"slug"`
	WhatsAppEnabled       bool                     `json:"whatsapp_enabled"`
	WhatsAppMessage       string                   `json:"whatsapp_message"`
	ExtraMessageText      string                   `json:"extra_message_text"`
	ExtraMessageMediaURL  string                   `json:"extra_message_media_url"`
	ExtraMessageMediaType string                   `json:"extra_message_media_type"`
	StartsAt              *time.Time               `json:"starts_at"`
	EndsAt                *time.Time               `json:"ends_at"`
	IsActive              bool                     `json:"is_active"`
	CreatedAt             time.Time                `json:"created_at"`
	ExtraMedia            []*DynamicLinkExtraMedia `json:"extra_media,omitempty"`
}

// DynamicLinkExtraMedia represents an additional image/video that is sent
// to the user after the main scratched-image message. Each item supports
// an optional caption with WhatsApp formatting. Up to 10 per link.
type DynamicLinkExtraMedia struct {
	ID        uuid.UUID `json:"id"`
	LinkID    uuid.UUID `json:"link_id"`
	URL       string    `json:"url"`
	MediaType string    `json:"media_type"`
	Caption   string    `json:"caption"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// DynamicLinkRegistration represents a participant registration on a public link
type DynamicLinkRegistration struct {
	ID                     uuid.UUID  `json:"id"`
	LinkID                 uuid.UUID  `json:"link_id"`
	FullName               string     `json:"full_name"`
	Phone                  string     `json:"phone"`
	Age                    *int       `json:"age,omitempty"`
	ContactID              *uuid.UUID `json:"contact_id,omitempty"`
	LeadID                 *uuid.UUID `json:"lead_id,omitempty"`
	WhatsAppStatus         string     `json:"whatsapp_status"`
	WhatsAppError          string     `json:"whatsapp_error,omitempty"`
	SessionToken           string     `json:"session_token,omitempty"`
	SharedByRegistrationID *uuid.UUID `json:"shared_by_registration_id,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
}

// DynamicWhatsAppQueue represents a queued WhatsApp message for a dynamic
type DynamicWhatsAppQueue struct {
	ID             uuid.UUID  `json:"id"`
	DynamicID      uuid.UUID  `json:"dynamic_id"`
	AccountID      uuid.UUID  `json:"account_id"`
	LinkID         uuid.UUID  `json:"link_id"`
	Phone          string     `json:"phone"`
	ItemID         uuid.UUID  `json:"item_id"`
	ImageURL       string     `json:"image_url"`
	Caption        string     `json:"caption"`
	ExtraText      string     `json:"extra_text"`
	ExtraMediaURL  string     `json:"extra_media_url"`
	ExtraMediaType string     `json:"extra_media_type"`
	Status         string     `json:"status"`
	ErrorMsg       string     `json:"error_msg"`
	CreatedAt      time.Time  `json:"created_at"`
	SentAt         *time.Time `json:"sent_at"`
}

// DocumentTemplate represents a reusable document design template
type DocumentTemplate struct {
	ID              uuid.UUID       `json:"id"`
	AccountID       uuid.UUID       `json:"account_id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	CanvasJSON      json.RawMessage `json:"canvas_json"`
	ThumbnailURL    string          `json:"thumbnail_url"`
	PageWidth       float64         `json:"page_width"`
	PageHeight      float64         `json:"page_height"`
	PageOrientation string          `json:"page_orientation"`
	FieldsUsed      []string        `json:"fields_used"`
	CreatedBy       *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// CustomFieldDefinition represents the schema/configuration of a custom field at account level
type CustomFieldDefinition struct {
	ID           uuid.UUID       `json:"id"`
	AccountID    uuid.UUID       `json:"account_id"`
	Name         string          `json:"name"`
	Slug         string          `json:"slug"`
	FieldType    string          `json:"field_type"`
	Config       json.RawMessage `json:"config"`
	IsRequired   bool            `json:"is_required"`
	DefaultValue *string         `json:"default_value"`
	SortOrder    int             `json:"sort_order"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// CustomFieldValue represents a stored value of a custom field for a specific contact
type CustomFieldValue struct {
	ID          uuid.UUID       `json:"id"`
	FieldID     uuid.UUID       `json:"field_id"`
	ContactID   uuid.UUID       `json:"contact_id"`
	ValueText   *string         `json:"value_text,omitempty"`
	ValueNumber *float64        `json:"value_number,omitempty"`
	ValueDate   *time.Time      `json:"value_date,omitempty"`
	ValueBool   *bool           `json:"value_bool,omitempty"`
	ValueJSON   json.RawMessage `json:"value_json,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`

	// Populated on demand (from JOIN with definition)
	FieldName string `json:"field_name,omitempty"`
	FieldSlug string `json:"field_slug,omitempty"`
	FieldType string `json:"field_type,omitempty"`
}
