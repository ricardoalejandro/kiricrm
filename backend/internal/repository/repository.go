package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/naperu/clarin/internal/domain"
)

type Repositories struct {
	db                 *pgxpool.Pool
	User               *UserRepository
	UserAccount        *UserAccountRepository
	Account            *AccountRepository
	Subscription       *SubscriptionRepository
	Device             *DeviceRepository
	Chat               *ChatRepository
	Message            *MessageRepository
	Contact            *ContactRepository
	ContactDeviceName  *ContactDeviceNameRepository
	Lead               *LeadRepository
	Pipeline           *PipelineRepository
	Tag                *TagRepository
	Campaign           *CampaignRepository
	Event              *EventRepository
	EventFolder        *EventFolderRepository
	EventPipeline      *EventPipelineRepository
	Participant        *ParticipantRepository
	Interaction        *InteractionRepository
	SavedSticker       *SavedStickerRepository
	Reaction           *ReactionRepository
	Poll               *PollRepository
	CampaignAttachment *CampaignAttachmentRepository
	QuickReply         *QuickReplyRepository
	Program            *ProgramRepository
	ProgramFolder      *ProgramFolderRepository
	Role               *RoleRepository
	Logbook            *LogbookRepository
	APIKey             *APIKeyRepository
	ErosConversation   *ErosConversationRepository
	AIToken            *AITokenRepository
	Automation         *AutomationRepository
	Survey             *SurveyRepository
	Dynamic            *DynamicRepository
	Task               *TaskRepository
	DocumentTemplate   *DocumentTemplateRepository
	CustomField        *CustomFieldRepository
	WhatsAppAPI        *WhatsAppAPIRepository
	Bot                *BotRepository
	Integration        *IntegrationRepository
	MediaAsset         *MediaAssetRepository
}

func NewRepositories(db *pgxpool.Pool) *Repositories {
	return &Repositories{
		db:                 db,
		User:               &UserRepository{db: db},
		UserAccount:        &UserAccountRepository{db: db},
		Account:            &AccountRepository{db: db},
		Subscription:       &SubscriptionRepository{db: db},
		Device:             &DeviceRepository{db: db},
		Chat:               &ChatRepository{db: db},
		Message:            &MessageRepository{db: db},
		Contact:            &ContactRepository{db: db},
		ContactDeviceName:  &ContactDeviceNameRepository{db: db},
		Lead:               &LeadRepository{db: db},
		Pipeline:           &PipelineRepository{db: db},
		Tag:                &TagRepository{db: db},
		Campaign:           &CampaignRepository{db: db},
		Event:              &EventRepository{db: db},
		EventFolder:        &EventFolderRepository{db: db},
		EventPipeline:      &EventPipelineRepository{db: db},
		Participant:        &ParticipantRepository{db: db},
		Interaction:        &InteractionRepository{db: db},
		SavedSticker:       &SavedStickerRepository{db: db},
		Reaction:           &ReactionRepository{db: db},
		Poll:               &PollRepository{db: db},
		CampaignAttachment: &CampaignAttachmentRepository{db: db},
		QuickReply:         &QuickReplyRepository{db: db},
		Program:            &ProgramRepository{db: db},
		ProgramFolder:      &ProgramFolderRepository{db: db},
		Role:               &RoleRepository{db: db},
		Logbook:            &LogbookRepository{db: db},
		APIKey:             &APIKeyRepository{db: db},
		ErosConversation:   &ErosConversationRepository{db: db},
		AIToken:            &AITokenRepository{db: db},
		Automation:         &AutomationRepository{db: db},
		Survey:             &SurveyRepository{db: db},
		Dynamic:            &DynamicRepository{db: db},
		Task:               &TaskRepository{db: db},
		DocumentTemplate:   &DocumentTemplateRepository{db: db},
		CustomField:        &CustomFieldRepository{db: db},
		WhatsAppAPI:        &WhatsAppAPIRepository{db: db},
		Bot:                &BotRepository{db: db},
		Integration:        &IntegrationRepository{db: db},
		MediaAsset:         &MediaAssetRepository{db: db},
	}
}

// DB returns the underlying database pool.
func (r *Repositories) DB() *pgxpool.Pool {
	return r.db
}

// UserRepository handles user data access
type UserRepository struct {
	db *pgxpool.Pool
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	user := &domain.User{}
	err := r.db.QueryRow(ctx, `
		SELECT u.id, u.account_id, u.username, u.email, u.password_hash, u.display_name, u.is_admin, u.is_active, u.is_super_admin, u.role, u.created_at, u.updated_at, a.name
		FROM users u JOIN accounts a ON a.id = u.account_id
		WHERE u.username = $1 AND u.is_active = TRUE
	`, username).Scan(
		&user.ID, &user.AccountID, &user.Username, &user.Email, &user.PasswordHash,
		&user.DisplayName, &user.IsAdmin, &user.IsActive, &user.IsSuperAdmin, &user.Role, &user.CreatedAt, &user.UpdatedAt, &user.AccountName,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	user := &domain.User{}
	err := r.db.QueryRow(ctx, `
		SELECT u.id, u.account_id, u.username, u.email, u.password_hash, u.display_name, u.is_admin, u.is_active, u.is_super_admin, u.role, u.created_at, u.updated_at, a.name
		FROM users u JOIN accounts a ON a.id = u.account_id
		WHERE u.id = $1
	`, id).Scan(
		&user.ID, &user.AccountID, &user.Username, &user.Email, &user.PasswordHash,
		&user.DisplayName, &user.IsAdmin, &user.IsActive, &user.IsSuperAdmin, &user.Role, &user.CreatedAt, &user.UpdatedAt, &user.AccountName,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (r *UserRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.User, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.account_id, u.username, u.email, u.password_hash, u.display_name, u.is_admin, u.is_active, u.is_super_admin,
		       COALESCE(ua.role, u.role), u.created_at, u.updated_at, a.name
		FROM user_accounts ua
		JOIN users u ON u.id = ua.user_id
		JOIN accounts a ON a.id = ua.account_id
		WHERE ua.account_id = $1
		ORDER BY u.created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		user := &domain.User{}
		if err := rows.Scan(
			&user.ID, &user.AccountID, &user.Username, &user.Email, &user.PasswordHash,
			&user.DisplayName, &user.IsAdmin, &user.IsActive, &user.IsSuperAdmin, &user.Role, &user.CreatedAt, &user.UpdatedAt, &user.AccountName,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (r *UserRepository) GetAll(ctx context.Context) ([]*domain.User, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.account_id, u.username, u.email, u.password_hash, u.display_name, u.is_admin, u.is_active, u.is_super_admin, u.role, u.created_at, u.updated_at, a.name
		FROM users u JOIN accounts a ON a.id = u.account_id
		ORDER BY u.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		user := &domain.User{}
		if err := rows.Scan(
			&user.ID, &user.AccountID, &user.Username, &user.Email, &user.PasswordHash,
			&user.DisplayName, &user.IsAdmin, &user.IsActive, &user.IsSuperAdmin, &user.Role, &user.CreatedAt, &user.UpdatedAt, &user.AccountName,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO users (account_id, username, email, password_hash, display_name, is_admin, is_super_admin, role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, is_active, created_at, updated_at
	`, user.AccountID, user.Username, user.Email, user.PasswordHash, user.DisplayName, user.IsAdmin, user.IsSuperAdmin, user.Role).Scan(
		&user.ID, &user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET username = $2, email = $3, display_name = $4, is_admin = $5, role = $6, updated_at = NOW()
		WHERE id = $1
	`, user.ID, user.Username, user.Email, user.DisplayName, user.IsAdmin, user.Role)
	return err
}

func (r *UserRepository) UpdatePassword(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`, userID, passwordHash)
	return err
}

func (r *UserRepository) ToggleActive(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET is_active = NOT is_active, updated_at = NOW() WHERE id = $1`, userID)
	return err
}

func (r *UserRepository) Delete(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

func (r *UserRepository) GetGroqAPIKey(ctx context.Context, userID uuid.UUID) (string, error) {
	var key string
	err := r.db.QueryRow(ctx, `SELECT COALESCE(groq_api_key, '') FROM users WHERE id = $1`, userID).Scan(&key)
	return key, err
}

func (r *UserRepository) SetGroqAPIKey(ctx context.Context, userID uuid.UUID, key string) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET groq_api_key = $2, updated_at = NOW() WHERE id = $1`, userID, key)
	return err
}

func (r *UserRepository) GetErosConfig(ctx context.Context, userID uuid.UUID) (model, role, instructions string, err error) {
	err = r.db.QueryRow(ctx, `SELECT COALESCE(eros_model, ''), COALESCE(eros_role, ''), COALESCE(eros_instructions, '') FROM users WHERE id = $1`, userID).Scan(&model, &role, &instructions)
	return
}

func (r *UserRepository) SetErosConfig(ctx context.Context, userID uuid.UUID, model, role, instructions string) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET eros_model = $2, eros_role = $3, eros_instructions = $4, updated_at = NOW() WHERE id = $1`, userID, model, role, instructions)
	return err
}

// UserAccountRepository handles user-account assignments (many-to-many)
type UserAccountRepository struct {
	db *pgxpool.Pool
}

func (r *UserAccountRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.UserAccount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT ua.id, ua.user_id, ua.account_id, ua.role, ua.is_default, ua.created_at,
		       a.name, COALESCE(a.slug, ''), COALESCE(a.mcp_enabled, false),
		       ua.role_id, COALESCE(ro.name, ''), COALESCE(ro.permissions, '{}')
		FROM user_accounts ua
		JOIN accounts a ON a.id = ua.account_id
		LEFT JOIN roles ro ON ro.id = ua.role_id
		WHERE ua.user_id = $1
		ORDER BY ua.is_default DESC, a.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*domain.UserAccount
	for rows.Next() {
		ua := &domain.UserAccount{}
		if err := rows.Scan(&ua.ID, &ua.UserID, &ua.AccountID, &ua.Role, &ua.IsDefault, &ua.CreatedAt,
			&ua.AccountName, &ua.AccountSlug, &ua.AccountMCPEnabled, &ua.RoleID, &ua.RoleName, &ua.Permissions); err != nil {
			return nil, err
		}
		accounts = append(accounts, ua)
	}
	return accounts, nil
}

func (r *UserAccountRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.UserAccount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT ua.id, ua.user_id, ua.account_id, ua.role, ua.is_default, ua.created_at,
		       a.name, COALESCE(a.slug, ''), COALESCE(a.mcp_enabled, false),
		       ua.role_id, COALESCE(ro.name, ''), COALESCE(ro.permissions, '{}')
		FROM user_accounts ua
		JOIN accounts a ON a.id = ua.account_id
		LEFT JOIN roles ro ON ro.id = ua.role_id
		WHERE ua.account_id = $1
		ORDER BY ua.created_at ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*domain.UserAccount
	for rows.Next() {
		ua := &domain.UserAccount{}
		if err := rows.Scan(&ua.ID, &ua.UserID, &ua.AccountID, &ua.Role, &ua.IsDefault, &ua.CreatedAt,
			&ua.AccountName, &ua.AccountSlug, &ua.AccountMCPEnabled, &ua.RoleID, &ua.RoleName, &ua.Permissions); err != nil {
			return nil, err
		}
		accounts = append(accounts, ua)
	}
	return accounts, nil
}

func (r *UserAccountRepository) Exists(ctx context.Context, userID, accountID uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM user_accounts WHERE user_id = $1 AND account_id = $2`, userID, accountID).Scan(&count)
	return count > 0, err
}

func (r *UserAccountRepository) CountByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM user_accounts WHERE user_id = $1`, userID).Scan(&count)
	return count, err
}

// NormalizeForUser keeps the legacy users.account_id in sync with the user's
// default assignment and guarantees exactly one default account when possible.
func (r *UserAccountRepository) NormalizeForUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_accounts (user_id, account_id, role, is_default)
		SELECT id, account_id, COALESCE(NULLIF(role, ''), 'agent'), TRUE
		FROM users
		WHERE id = $1
		  AND account_id IS NOT NULL
		  AND NOT EXISTS (
		  	SELECT 1 FROM user_accounts existing WHERE existing.user_id = users.id
		  )
		ON CONFLICT (user_id, account_id) DO NOTHING
	`, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		WITH preferred AS (
			SELECT ua.id
			FROM user_accounts ua
			JOIN users u ON u.id = ua.user_id
			WHERE ua.user_id = $1
			ORDER BY ua.is_default DESC, (ua.account_id = u.account_id) DESC, ua.created_at ASC, ua.id ASC
			LIMIT 1
		)
		UPDATE user_accounts ua
		SET is_default = (ua.id = (SELECT id FROM preferred))
		WHERE ua.user_id = $1
	`, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		WITH chosen AS (
			SELECT ua.account_id, COALESCE(NULLIF(ua.role, ''), 'agent') AS role
			FROM user_accounts ua
			WHERE ua.user_id = $1 AND ua.is_default = TRUE
			ORDER BY ua.created_at ASC, ua.id ASC
			LIMIT 1
		)
		UPDATE users u
		SET account_id = chosen.account_id,
			role = chosen.role,
			is_admin = CASE
				WHEN u.is_super_admin THEN TRUE
				ELSE chosen.role IN ('admin', 'super_admin')
			END,
			is_super_admin = CASE
				WHEN chosen.role = 'super_admin' THEN TRUE
				ELSE u.is_super_admin
			END,
			updated_at = NOW()
		FROM chosen
		WHERE u.id = $1
	`, userID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *UserAccountRepository) Assign(ctx context.Context, ua *domain.UserAccount) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if ua.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE user_accounts SET is_default = FALSE WHERE user_id = $1`, ua.UserID); err != nil {
			return err
		}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, account_id, role, role_id, is_default)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, account_id) DO UPDATE SET
			role = EXCLUDED.role,
			role_id = EXCLUDED.role_id,
			is_default = CASE WHEN EXCLUDED.is_default THEN TRUE ELSE user_accounts.is_default END
		RETURNING id, created_at
	`, ua.UserID, ua.AccountID, ua.Role, ua.RoleID, ua.IsDefault).Scan(&ua.ID, &ua.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *UserAccountRepository) UpdateRoleID(ctx context.Context, userID, accountID uuid.UUID, roleID *uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE user_accounts SET role_id = $3 WHERE user_id = $1 AND account_id = $2`, userID, accountID, roleID)
	return err
}

// GetUserPermissions returns the permissions slice for a user in a given account
// Returns empty slice if no role is assigned
func (r *UserAccountRepository) GetUserPermissions(ctx context.Context, userID, accountID uuid.UUID) ([]string, error) {
	var permissions []string
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(ro.permissions, '{}')
		FROM user_accounts ua
		LEFT JOIN roles ro ON ro.id = ua.role_id
		WHERE ua.user_id = $1 AND ua.account_id = $2
	`, userID, accountID).Scan(&permissions)
	if err != nil {
		return []string{}, nil
	}
	if permissions == nil {
		permissions = []string{}
	}
	return permissions, nil
}

func (r *UserAccountRepository) UpdateRole(ctx context.Context, userID, accountID uuid.UUID, role string) error {
	_, err := r.db.Exec(ctx, `UPDATE user_accounts SET role = $3 WHERE user_id = $1 AND account_id = $2`, userID, accountID, role)
	return err
}

func (r *UserAccountRepository) Remove(ctx context.Context, userID, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM user_accounts WHERE user_id = $1 AND account_id = $2`, userID, accountID)
	return err
}

func (r *UserAccountRepository) SetDefault(ctx context.Context, userID, accountID uuid.UUID) error {
	// Unset all defaults for this user, then set the new one
	_, err := r.db.Exec(ctx, `UPDATE user_accounts SET is_default = FALSE WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `UPDATE user_accounts SET is_default = TRUE WHERE user_id = $1 AND account_id = $2`, userID, accountID)
	return err
}

// AccountRepository handles account data access
type AccountRepository struct {
	db *pgxpool.Pool
}

func (r *AccountRepository) GetAll(ctx context.Context) ([]*domain.Account, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.name, COALESCE(a.slug, ''), COALESCE(s.plan_code, a.plan), a.max_devices, COALESCE(a.storage_limit_bytes, 0), COALESCE(a.is_active, true), COALESCE(a.mcp_enabled, false), COALESCE(a.kommo_enabled, false), a.created_at, a.updated_at,
			COALESCE(s.status, 'active'), s.trial_ends_at, s.current_period_end, s.grace_ends_at,
			(SELECT COUNT(*) FROM user_accounts WHERE account_id = a.id) as user_count,
			(SELECT COUNT(*) FROM devices WHERE account_id = a.id) as device_count,
			(SELECT COUNT(*) FROM chats WHERE account_id = a.id) as chat_count
		FROM accounts a
		LEFT JOIN subscriptions s ON s.account_id = a.id
		ORDER BY a.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*domain.Account
	for rows.Next() {
		a := &domain.Account{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Slug, &a.Plan, &a.MaxDevices, &a.StorageLimitBytes, &a.IsActive, &a.MCPEnabled, &a.KommoEnabled, &a.CreatedAt, &a.UpdatedAt,
			&a.SubscriptionStatus, &a.TrialEndsAt, &a.CurrentPeriodEnd, &a.GraceEndsAt,
			&a.UserCount, &a.DeviceCount, &a.ChatCount); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (r *AccountRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	a := &domain.Account{}
	err := r.db.QueryRow(ctx, `
		SELECT a.id, a.name, COALESCE(a.slug, ''), COALESCE(s.plan_code, a.plan), a.max_devices, COALESCE(a.storage_limit_bytes, 0), COALESCE(a.is_active, true), COALESCE(a.mcp_enabled, false), COALESCE(a.kommo_enabled, false), a.default_incoming_stage_id, a.created_at, a.updated_at,
			COALESCE(s.status, 'active'), s.trial_ends_at, s.current_period_end, s.grace_ends_at,
			(SELECT COUNT(*) FROM user_accounts WHERE account_id = a.id) as user_count,
			(SELECT COUNT(*) FROM devices WHERE account_id = a.id) as device_count,
			(SELECT COUNT(*) FROM chats WHERE account_id = a.id) as chat_count,
			a.google_email, a.google_contact_group_id, a.google_connected_at, COALESCE(a.google_sync_limit, 20000)
		FROM accounts a
		LEFT JOIN subscriptions s ON s.account_id = a.id
		WHERE a.id = $1
	`, id).Scan(&a.ID, &a.Name, &a.Slug, &a.Plan, &a.MaxDevices, &a.StorageLimitBytes, &a.IsActive, &a.MCPEnabled, &a.KommoEnabled, &a.DefaultIncomingStageID, &a.CreatedAt, &a.UpdatedAt,
		&a.SubscriptionStatus, &a.TrialEndsAt, &a.CurrentPeriodEnd, &a.GraceEndsAt,
		&a.UserCount, &a.DeviceCount, &a.ChatCount,
		&a.GoogleEmail, &a.GoogleContactGroupID, &a.GoogleConnectedAt, &a.GoogleSyncLimit)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (r *AccountRepository) Create(ctx context.Context, a *domain.Account) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO accounts (name, slug, plan, max_devices, storage_limit_bytes, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, a.Name, a.Slug, a.Plan, a.MaxDevices, a.StorageLimitBytes, a.IsActive).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

func (r *AccountRepository) Update(ctx context.Context, a *domain.Account) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET name = $2, slug = $3, plan = $4, max_devices = $5, storage_limit_bytes = $6, mcp_enabled = $7, kommo_enabled = $8, updated_at = NOW()
		WHERE id = $1
	`, a.ID, a.Name, a.Slug, a.Plan, a.MaxDevices, a.StorageLimitBytes, a.MCPEnabled, a.KommoEnabled)
	return err
}

func (r *AccountRepository) ToggleActive(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE accounts SET is_active = NOT COALESCE(is_active, true), updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *AccountRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id)
	return err
}

// DeviceRepository handles device data access
type DeviceRepository struct {
	db *pgxpool.Pool
}

func deviceCapabilities(device *domain.Device) string {
	if device == nil || len(device.Capabilities) == 0 {
		return "[]"
	}
	return string(device.Capabilities)
}

func (r *DeviceRepository) Create(ctx context.Context, device *domain.Device) error {
	capabilities := deviceCapabilities(device)
	return r.db.QueryRow(ctx, `
		INSERT INTO devices (
			account_id, name, status, provider, phone, waba_id, phone_number_id, api_display_phone,
			api_webhook_status, api_billing_status, api_sending_enabled, api_templates_enabled, capabilities
		)
		VALUES (
			$1, $2, COALESCE($3::text, 'disconnected'), COALESCE(NULLIF($4::text, ''), 'whatsapp_web'), $5, $6, $7, $8,
			COALESCE(NULLIF($9::text, ''), 'not_configured'), COALESCE(NULLIF($10::text, ''), 'not_configured'), $11, $12, $13::jsonb
		)
		RETURNING id, account_id, name, phone, jid, status, qr_code, receive_messages, provider, waba_id,
			phone_number_id, api_display_phone, api_webhook_status, api_billing_status, api_sending_enabled,
			api_templates_enabled, capabilities, last_seen_at, created_at, updated_at
	`, device.AccountID, device.Name, device.Status, device.Provider, device.Phone, device.WABAID, device.PhoneNumberID,
		device.APIDisplayPhone, device.APIWebhookStatus, device.APIBillingStatus, device.APISendingEnabled,
		device.APITemplatesEnabled, capabilities).Scan(
		&device.ID, &device.AccountID, &device.Name, &device.Phone, &device.JID,
		&device.Status, &device.QRCode, &device.ReceiveMessages, &device.Provider, &device.WABAID,
		&device.PhoneNumberID, &device.APIDisplayPhone, &device.APIWebhookStatus, &device.APIBillingStatus,
		&device.APISendingEnabled, &device.APITemplatesEnabled, &device.Capabilities, &device.LastSeenAt,
		&device.CreatedAt, &device.UpdatedAt,
	)
}

func (r *DeviceRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Device, error) {
	device := &domain.Device{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, name, phone, jid, status, qr_code, receive_messages, provider, waba_id,
			phone_number_id, api_display_phone, api_webhook_status, api_billing_status, api_sending_enabled,
			api_templates_enabled, capabilities, last_seen_at, created_at, updated_at
		FROM devices WHERE id = $1
	`, id).Scan(
		&device.ID, &device.AccountID, &device.Name, &device.Phone, &device.JID,
		&device.Status, &device.QRCode, &device.ReceiveMessages, &device.Provider, &device.WABAID,
		&device.PhoneNumberID, &device.APIDisplayPhone, &device.APIWebhookStatus, &device.APIBillingStatus,
		&device.APISendingEnabled, &device.APITemplatesEnabled, &device.Capabilities, &device.LastSeenAt,
		&device.CreatedAt, &device.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return device, err
}

func (r *DeviceRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Device, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, name, phone, jid, status, qr_code, receive_messages, provider, waba_id,
			phone_number_id, api_display_phone, api_webhook_status, api_billing_status, api_sending_enabled,
			api_templates_enabled, capabilities, last_seen_at, created_at, updated_at
		FROM devices WHERE account_id = $1 ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*domain.Device
	for rows.Next() {
		device := &domain.Device{}
		if err := rows.Scan(
			&device.ID, &device.AccountID, &device.Name, &device.Phone, &device.JID,
			&device.Status, &device.QRCode, &device.ReceiveMessages, &device.Provider, &device.WABAID,
			&device.PhoneNumberID, &device.APIDisplayPhone, &device.APIWebhookStatus, &device.APIBillingStatus,
			&device.APISendingEnabled, &device.APITemplatesEnabled, &device.Capabilities, &device.LastSeenAt,
			&device.CreatedAt, &device.UpdatedAt,
		); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func (r *DeviceRepository) GetAll(ctx context.Context) ([]*domain.Device, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, name, phone, jid, status, qr_code, receive_messages, provider, waba_id,
			phone_number_id, api_display_phone, api_webhook_status, api_billing_status, api_sending_enabled,
			api_templates_enabled, capabilities, last_seen_at, created_at, updated_at
		FROM devices ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*domain.Device
	for rows.Next() {
		device := &domain.Device{}
		if err := rows.Scan(
			&device.ID, &device.AccountID, &device.Name, &device.Phone, &device.JID,
			&device.Status, &device.QRCode, &device.ReceiveMessages, &device.Provider, &device.WABAID,
			&device.PhoneNumberID, &device.APIDisplayPhone, &device.APIWebhookStatus, &device.APIBillingStatus,
			&device.APISendingEnabled, &device.APITemplatesEnabled, &device.Capabilities, &device.LastSeenAt,
			&device.CreatedAt, &device.UpdatedAt,
		); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func (r *DeviceRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE devices SET status = $1, updated_at = NOW() WHERE id = $2
	`, status, id)
	return err
}

func (r *DeviceRepository) UpdateJID(ctx context.Context, id uuid.UUID, jid, phone string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE devices SET jid = $1, phone = $2, status = $3, last_seen_at = NOW(), updated_at = NOW() WHERE id = $4
	`, jid, phone, domain.DeviceStatusConnected, id)
	return err
}

func (r *DeviceRepository) UpdateName(ctx context.Context, id uuid.UUID, name string) error {
	_, err := r.db.Exec(ctx, `UPDATE devices SET name = $1, updated_at = NOW() WHERE id = $2`, name, id)
	return err
}

func (r *DeviceRepository) UpdateReceiveMessages(ctx context.Context, id uuid.UUID, receive bool) error {
	_, err := r.db.Exec(ctx, `UPDATE devices SET receive_messages = $1, updated_at = NOW() WHERE id = $2`, receive, id)
	return err
}

func (r *DeviceRepository) UpdateCloudAPIConfig(ctx context.Context, id uuid.UUID, device *domain.Device) error {
	capabilities := deviceCapabilities(device)
	_, err := r.db.Exec(ctx, `
		UPDATE devices SET
			phone = $2,
			waba_id = $3,
			phone_number_id = $4,
			api_display_phone = $5,
			api_webhook_status = COALESCE(NULLIF($6::text, ''), 'not_configured'),
			api_billing_status = COALESCE(NULLIF($7::text, ''), 'not_configured'),
			api_sending_enabled = $8,
			api_templates_enabled = $9,
			capabilities = $10::jsonb,
			updated_at = NOW()
		WHERE id = $1
	`, id, device.Phone, device.WABAID, device.PhoneNumberID, device.APIDisplayPhone,
		device.APIWebhookStatus, device.APIBillingStatus, device.APISendingEnabled,
		device.APITemplatesEnabled, capabilities)
	return err
}

func (r *DeviceRepository) UpdateQRCode(ctx context.Context, id uuid.UUID, qrCode string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE devices SET qr_code = $1, status = $2, updated_at = NOW() WHERE id = $3
	`, qrCode, domain.DeviceStatusConnecting, id)
	return err
}

func (r *DeviceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id)
	return err
}

// ChatRepository handles chat data access
type ChatRepository struct {
	db *pgxpool.Pool
}

func (r *ChatRepository) GetOrCreate(ctx context.Context, accountID, deviceID uuid.UUID, jid, name string) (*domain.Chat, error) {
	chat := &domain.Chat{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO chats (account_id, device_id, jid, name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, jid) DO UPDATE SET
			device_id = EXCLUDED.device_id,
			name = CASE WHEN EXCLUDED.name != '' AND EXCLUDED.name IS NOT NULL THEN EXCLUDED.name ELSE chats.name END
		RETURNING id, account_id, device_id, contact_id, jid, name, last_message, last_message_at,
		          unread_count, is_archived, is_pinned, created_at, updated_at
	`, accountID, deviceID, jid, name).Scan(
		&chat.ID, &chat.AccountID, &chat.DeviceID, &chat.ContactID, &chat.JID, &chat.Name,
		&chat.LastMessage, &chat.LastMessageAt, &chat.UnreadCount, &chat.IsArchived,
		&chat.IsPinned, &chat.CreatedAt, &chat.UpdatedAt,
	)
	return chat, err
}

func (r *ChatRepository) FindByJID(ctx context.Context, accountID uuid.UUID, jid string) (*domain.Chat, error) {
	chat := &domain.Chat{}
	err := r.db.QueryRow(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.contact_id, c.jid, c.name, c.last_message, c.last_message_at,
		       c.unread_count, c.is_archived, c.is_pinned, c.created_at, c.updated_at,
		       d.name, d.phone, d.status
		FROM chats c
		LEFT JOIN devices d ON c.device_id = d.id
		WHERE c.account_id = $1 AND c.jid = $2
	`, accountID, jid).Scan(
		&chat.ID, &chat.AccountID, &chat.DeviceID, &chat.ContactID, &chat.JID, &chat.Name,
		&chat.LastMessage, &chat.LastMessageAt, &chat.UnreadCount, &chat.IsArchived,
		&chat.IsPinned, &chat.CreatedAt, &chat.UpdatedAt,
		&chat.DeviceName, &chat.DevicePhone, &chat.DeviceStatus,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return chat, nil
}

func (r *ChatRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Chat, error) {
	chat := &domain.Chat{}
	err := r.db.QueryRow(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.contact_id, c.jid, c.name, c.last_message, c.last_message_at,
		       c.unread_count, c.is_archived, c.is_pinned, c.created_at, c.updated_at,
		       d.name, d.phone,
		       ctc.phone, ctc.avatar_url, ctc.custom_name, ctc.name
		FROM chats c
		LEFT JOIN devices d ON c.device_id = d.id
		LEFT JOIN contacts ctc ON ctc.account_id = c.account_id AND ctc.jid = c.jid
		WHERE c.id = $1
	`, id).Scan(
		&chat.ID, &chat.AccountID, &chat.DeviceID, &chat.ContactID, &chat.JID, &chat.Name,
		&chat.LastMessage, &chat.LastMessageAt, &chat.UnreadCount, &chat.IsArchived,
		&chat.IsPinned, &chat.CreatedAt, &chat.UpdatedAt,
		&chat.DeviceName, &chat.DevicePhone,
		&chat.ContactPhone, &chat.ContactAvatarURL, &chat.ContactCustomName, &chat.ContactName,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return chat, err
}

func (r *ChatRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Chat, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.contact_id, c.jid, c.name, c.last_message, c.last_message_at,
		       c.unread_count, c.is_archived, c.is_pinned, c.created_at, c.updated_at,
		       d.name, d.phone,
		       ctc.phone, ctc.avatar_url, ctc.custom_name, ctc.name
		FROM chats c
		LEFT JOIN devices d ON c.device_id = d.id
		LEFT JOIN contacts ctc ON ctc.account_id = c.account_id AND ctc.jid = c.jid
		WHERE c.account_id = $1 AND c.jid NOT LIKE '%@g.us' AND c.jid NOT LIKE '%@newsletter' AND c.jid NOT LIKE '%@broadcast' AND c.jid NOT LIKE '%@lid'
		ORDER BY c.is_pinned DESC, c.last_message_at DESC NULLS LAST
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []*domain.Chat
	for rows.Next() {
		chat := &domain.Chat{}
		if err := rows.Scan(
			&chat.ID, &chat.AccountID, &chat.DeviceID, &chat.ContactID, &chat.JID, &chat.Name,
			&chat.LastMessage, &chat.LastMessageAt, &chat.UnreadCount, &chat.IsArchived,
			&chat.IsPinned, &chat.CreatedAt, &chat.UpdatedAt,
			&chat.DeviceName, &chat.DevicePhone,
			&chat.ContactPhone, &chat.ContactAvatarURL, &chat.ContactCustomName, &chat.ContactName,
		); err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	return chats, nil
}

func (r *ChatRepository) GetByAccountIDWithFilters(ctx context.Context, accountID uuid.UUID, filter domain.ChatFilter) ([]*domain.Chat, int, error) {
	// Build dynamic query
	baseQuery := `
		FROM chats c
		LEFT JOIN devices d ON c.device_id = d.id
		LEFT JOIN contacts ctc ON ctc.account_id = c.account_id AND ctc.jid = c.jid
		LEFT JOIN leads l ON l.account_id = c.account_id AND l.jid = c.jid
		WHERE c.account_id = $1 AND c.jid NOT LIKE '%@g.us' AND c.jid NOT LIKE '%@newsletter' AND c.jid NOT LIKE '%@broadcast' AND c.jid NOT LIKE '%@lid'
	`
	args := []interface{}{accountID}
	argNum := 2

	// Device filter
	if len(filter.DeviceIDs) > 0 {
		baseQuery += fmt.Sprintf(" AND c.device_id = ANY($%d)", argNum)
		args = append(args, filter.DeviceIDs)
		argNum++
	}

	// Tag filter (filter by contact tags)
	if len(filter.TagIDs) > 0 {
		baseQuery += fmt.Sprintf(" AND ctc.id IN (SELECT contact_id FROM contact_tags WHERE tag_id = ANY($%d))", argNum)
		args = append(args, filter.TagIDs)
		argNum++
	}

	// Unread filter
	if filter.UnreadOnly {
		baseQuery += " AND c.unread_count > 0"
	}

	// Archived filter
	if !filter.Archived {
		baseQuery += " AND c.is_archived = FALSE"
	}

	// Search filter
	if filter.Search != "" {
		baseQuery += fmt.Sprintf(" AND (c.name ILIKE $%d OR c.jid ILIKE $%d OR ctc.custom_name ILIKE $%d OR ctc.name ILIKE $%d OR ctc.push_name ILIKE $%d OR ctc.phone ILIKE $%d)", argNum, argNum, argNum, argNum, argNum, argNum)
		args = append(args, "%"+filter.Search+"%")
		argNum++
	}

	// Reaction filter — only chats that have at least one reaction matching the criteria
	if filter.HasReaction {
		reactionClause := " AND EXISTS (SELECT 1 FROM message_reactions mr WHERE mr.chat_id = c.id AND mr.account_id = c.account_id"
		if filter.ReactionFromMe != nil {
			reactionClause += fmt.Sprintf(" AND mr.is_from_me = $%d", argNum)
			args = append(args, *filter.ReactionFromMe)
			argNum++
		}
		if len(filter.ReactionEmojis) > 0 {
			reactionClause += fmt.Sprintf(" AND mr.emoji = ANY($%d)", argNum)
			args = append(args, filter.ReactionEmojis)
			argNum++
		}
		if filter.ReactionSince != nil {
			reactionClause += fmt.Sprintf(" AND mr.timestamp >= $%d", argNum)
			args = append(args, *filter.ReactionSince)
			argNum++
		}
		if filter.ReactionUntil != nil {
			reactionClause += fmt.Sprintf(" AND mr.timestamp <= $%d", argNum)
			args = append(args, *filter.ReactionUntil)
			argNum++
		}
		reactionClause += ")"
		baseQuery += reactionClause
	}

	// Count total
	var total int
	countQuery := "SELECT COUNT(DISTINCT c.id) " + baseQuery
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get data — DISTINCT ON prevents duplicate rows when multiple leads share the same JID
	selectQuery := `
		SELECT DISTINCT ON (c.is_pinned, c.last_message_at, c.id)
		       c.id, c.account_id, c.device_id, c.contact_id, c.jid, c.name, c.last_message, c.last_message_at,
		       c.unread_count, c.is_archived, c.is_pinned, c.created_at, c.updated_at,
		       d.name, d.phone,
		       ctc.phone, ctc.avatar_url, ctc.custom_name, ctc.name,
		       COALESCE(l.is_blocked, false)
	` + baseQuery + " ORDER BY c.is_pinned DESC, c.last_message_at DESC NULLS LAST, c.id"

	// Apply pagination
	if filter.Limit > 0 {
		selectQuery += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			selectQuery += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := r.db.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var chats []*domain.Chat
	for rows.Next() {
		chat := &domain.Chat{}
		if err := rows.Scan(
			&chat.ID, &chat.AccountID, &chat.DeviceID, &chat.ContactID, &chat.JID, &chat.Name,
			&chat.LastMessage, &chat.LastMessageAt, &chat.UnreadCount, &chat.IsArchived,
			&chat.IsPinned, &chat.CreatedAt, &chat.UpdatedAt,
			&chat.DeviceName, &chat.DevicePhone,
			&chat.ContactPhone, &chat.ContactAvatarURL, &chat.ContactCustomName, &chat.ContactName,
			&chat.LeadIsBlocked,
		); err != nil {
			return nil, 0, err
		}
		chats = append(chats, chat)
	}

	return chats, total, nil
}

func (r *ChatRepository) UpdateLastMessage(ctx context.Context, chatID uuid.UUID, message string, timestamp time.Time, incrementUnread bool) error {
	query := `
		UPDATE chats SET last_message = $1, last_message_at = $2, updated_at = NOW()
	`
	if incrementUnread {
		query += `, unread_count = unread_count + 1`
	}
	query += ` WHERE id = $3`
	_, err := r.db.Exec(ctx, query, message, timestamp, chatID)
	return err
}

func (r *ChatRepository) MarkAsRead(ctx context.Context, chatID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE chats SET unread_count = 0, updated_at = NOW() WHERE id = $1`, chatID)
	return err
}

func (r *ChatRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// First delete all messages in the chat
	_, err := r.db.Exec(ctx, `DELETE FROM messages WHERE chat_id = $1`, id)
	if err != nil {
		return err
	}
	// Then delete the chat
	_, err = r.db.Exec(ctx, `DELETE FROM chats WHERE id = $1`, id)
	return err
}

func (r *ChatRepository) DeleteBatch(ctx context.Context, ids []uuid.UUID) error {
	// First delete all messages in the chats
	_, err := r.db.Exec(ctx, `DELETE FROM messages WHERE chat_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	// Then delete the chats
	_, err = r.db.Exec(ctx, `DELETE FROM chats WHERE id = ANY($1)`, ids)
	return err
}

func (r *ChatRepository) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	// First delete all messages for the account
	_, err := r.db.Exec(ctx, `DELETE FROM messages WHERE account_id = $1`, accountID)
	if err != nil {
		return err
	}
	// Then delete all chats
	_, err = r.db.Exec(ctx, `DELETE FROM chats WHERE account_id = $1`, accountID)
	return err
}

// MessageRepository handles message data access
type MessageRepository struct {
	db *pgxpool.Pool
}

type MediaAssetUpsert struct {
	AccountID   uuid.UUID
	ContentHash string
	ObjectKey   string
	MediaType   string
	ContentType string
	Filename    string
	SizeBytes   int64
}

type MediaAssetRepository struct {
	db *pgxpool.Pool
}

func (r *MediaAssetRepository) GetByHash(ctx context.Context, accountID uuid.UUID, contentHash string) (*domain.MediaAsset, error) {
	asset := &domain.MediaAsset{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, content_hash, object_key, media_type, content_type, filename, size_bytes, status, created_at, updated_at, deleted_at
		FROM media_assets
		WHERE account_id = $1 AND content_hash = $2 AND status = 'active'
		LIMIT 1
	`, accountID, contentHash).Scan(
		&asset.ID, &asset.AccountID, &asset.ContentHash, &asset.ObjectKey, &asset.MediaType,
		&asset.ContentType, &asset.Filename, &asset.SizeBytes, &asset.Status, &asset.CreatedAt, &asset.UpdatedAt, &asset.DeletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func (r *MediaAssetRepository) Upsert(ctx context.Context, input MediaAssetUpsert) (*domain.MediaAsset, error) {
	asset := &domain.MediaAsset{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO media_assets (account_id, content_hash, object_key, media_type, content_type, filename, size_bytes, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', NOW())
		ON CONFLICT (account_id, content_hash) DO UPDATE
		SET status = 'active',
		    deleted_at = NULL,
		    updated_at = NOW()
		RETURNING id, account_id, content_hash, object_key, media_type, content_type, filename, size_bytes, status, created_at, updated_at, deleted_at
	`, input.AccountID, input.ContentHash, input.ObjectKey, input.MediaType, input.ContentType, input.Filename, input.SizeBytes).Scan(
		&asset.ID, &asset.AccountID, &asset.ContentHash, &asset.ObjectKey, &asset.MediaType,
		&asset.ContentType, &asset.Filename, &asset.SizeBytes, &asset.Status, &asset.CreatedAt, &asset.UpdatedAt, &asset.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func (r *MessageRepository) Create(ctx context.Context, msg *domain.Message) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO messages (account_id, device_id, chat_id, message_id, from_jid, from_name, body,
		                      message_type, media_url, media_mimetype, media_filename, media_size, media_asset_id,
		                      is_from_me, is_read, status, timestamp,
		                      quoted_message_id, quoted_body, quoted_sender,
		                      poll_question, poll_max_selections,
		                      is_revoked, is_view_once, latitude, longitude,
		                      contact_name, contact_phone, contact_vcard, provider, template_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
		        $22, $23, $24, $25, $26, $27, $28, $29, COALESCE(NULLIF($30::text, ''), 'whatsapp_web'), $31)
		ON CONFLICT (chat_id, message_id) DO NOTHING
		RETURNING id, created_at
	`, msg.AccountID, msg.DeviceID, msg.ChatID, msg.MessageID, msg.FromJID, msg.FromName, msg.Body,
		msg.MessageType, msg.MediaURL, msg.MediaMimetype, msg.MediaFilename, msg.MediaSize, msg.MediaAssetID,
		msg.IsFromMe, msg.IsRead, msg.Status, msg.Timestamp,
		msg.QuotedMessageID, msg.QuotedBody, msg.QuotedSender,
		msg.PollQuestion, msg.PollMaxSelections,
		msg.IsRevoked, msg.IsViewOnce, msg.Latitude, msg.Longitude,
		msg.ContactName, msg.ContactPhone, msg.ContactVCard, msg.Provider, msg.TemplateName,
	).Scan(&msg.ID, &msg.CreatedAt)
}

func (r *MessageRepository) GetByChatID(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*domain.Message, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, device_id, chat_id, message_id, from_jid, from_name, body,
		       message_type, media_url, media_mimetype, media_filename, media_size, media_asset_id,
		       is_from_me, is_read, status, provider, template_name, timestamp, created_at,
		       quoted_message_id, quoted_body, quoted_sender,
		       COALESCE(is_revoked, false), COALESCE(is_view_once, false), COALESCE(media_deleted, false),
		       latitude, longitude, contact_name, contact_phone, contact_vcard
		FROM (
			SELECT * FROM messages WHERE chat_id = $1
			ORDER BY timestamp DESC
			LIMIT $2 OFFSET $3
		) sub ORDER BY timestamp ASC
	`, chatID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*domain.Message
	for rows.Next() {
		msg := &domain.Message{}
		if err := rows.Scan(
			&msg.ID, &msg.AccountID, &msg.DeviceID, &msg.ChatID, &msg.MessageID, &msg.FromJID,
			&msg.FromName, &msg.Body, &msg.MessageType, &msg.MediaURL, &msg.MediaMimetype,
			&msg.MediaFilename, &msg.MediaSize, &msg.MediaAssetID, &msg.IsFromMe, &msg.IsRead, &msg.Status,
			&msg.Provider, &msg.TemplateName, &msg.Timestamp, &msg.CreatedAt,
			&msg.QuotedMessageID, &msg.QuotedBody, &msg.QuotedSender,
			&msg.IsRevoked, &msg.IsViewOnce, &msg.MediaDeleted,
			&msg.Latitude, &msg.Longitude, &msg.ContactName, &msg.ContactPhone, &msg.ContactVCard,
		); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// GetByMessageID finds a message by its WhatsApp message_id within a chat
func (r *MessageRepository) GetByMessageID(ctx context.Context, chatID uuid.UUID, messageID string) (*domain.Message, error) {
	msg := &domain.Message{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, device_id, chat_id, message_id, from_jid, from_name, body,
		       message_type, media_url, media_mimetype, media_filename, media_size, media_asset_id,
		       is_from_me, is_read, status, provider, template_name, timestamp, created_at,
		       quoted_message_id, quoted_body, quoted_sender,
		       COALESCE(is_revoked, false), COALESCE(is_view_once, false), COALESCE(media_deleted, false),
		       latitude, longitude, contact_name, contact_phone, contact_vcard
		FROM messages WHERE chat_id = $1 AND message_id = $2
		LIMIT 1
	`, chatID, messageID).Scan(
		&msg.ID, &msg.AccountID, &msg.DeviceID, &msg.ChatID, &msg.MessageID, &msg.FromJID,
		&msg.FromName, &msg.Body, &msg.MessageType, &msg.MediaURL, &msg.MediaMimetype,
		&msg.MediaFilename, &msg.MediaSize, &msg.MediaAssetID, &msg.IsFromMe, &msg.IsRead, &msg.Status,
		&msg.Provider, &msg.TemplateName, &msg.Timestamp, &msg.CreatedAt,
		&msg.QuotedMessageID, &msg.QuotedBody, &msg.QuotedSender,
		&msg.IsRevoked, &msg.IsViewOnce, &msg.MediaDeleted,
		&msg.Latitude, &msg.Longitude, &msg.ContactName, &msg.ContactPhone, &msg.ContactVCard,
	)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// GetOldestByChatID returns the oldest message in a chat (for history sync pagination)
func (r *MessageRepository) GetOldestByChatID(ctx context.Context, chatID uuid.UUID) (*domain.Message, error) {
	msg := &domain.Message{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, device_id, chat_id, message_id, from_jid, from_name, body,
		       message_type, is_from_me, timestamp
		FROM messages WHERE chat_id = $1
		ORDER BY timestamp ASC LIMIT 1
	`, chatID).Scan(
		&msg.ID, &msg.AccountID, &msg.DeviceID, &msg.ChatID, &msg.MessageID, &msg.FromJID,
		&msg.FromName, &msg.Body, &msg.MessageType, &msg.IsFromMe, &msg.Timestamp,
	)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// ContactRepository handles contact data access
type ContactRepository struct {
	db *pgxpool.Pool
}

// UpdateStatus updates the delivery status of a message by its WhatsApp message_id
func (r *MessageRepository) UpdateStatus(ctx context.Context, accountID uuid.UUID, chatJID string, messageID string, status string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE messages SET status = $1
		WHERE account_id = $2 AND message_id = $3 AND is_from_me = true
		AND chat_id IN (SELECT id FROM chats WHERE account_id = $2 AND jid = $4)
	`, status, accountID, messageID, chatJID)
	return err
}

// UpdateStatusUpgrade updates message status only if it's an upgrade (sent→delivered→read)
// This prevents race conditions where a late "delivered" receipt overwrites "read"
func (r *MessageRepository) UpdateStatusUpgrade(ctx context.Context, accountID uuid.UUID, chatJID string, messageID string, status string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE messages SET status = $1
		WHERE account_id = $2 AND message_id = $3 AND is_from_me = true
		AND chat_id IN (SELECT id FROM chats WHERE account_id = $2 AND jid = $4)
		AND (
			($1 = 'read') OR
			($1 = 'delivered' AND status IN ('sent', 'sending')) OR
			($1 = 'sent' AND status = 'sending')
		)
	`, status, accountID, messageID, chatJID)
	return err
}

// MarkAsRevoked marks a message as revoked (deleted for everyone)
func (r *MessageRepository) MarkAsRevoked(ctx context.Context, accountID uuid.UUID, chatJID string, messageID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE messages SET is_revoked = true, body = NULL
		WHERE account_id = $1 AND message_id = $2
		AND chat_id IN (SELECT id FROM chats WHERE account_id = $1 AND jid = $3)
	`, accountID, messageID, chatJID)
	return err
}

// UpdateBody updates the body text of an edited message
func (r *MessageRepository) UpdateBody(ctx context.Context, accountID uuid.UUID, chatJID string, messageID string, newBody string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE messages SET body = $4, is_edited = true
		WHERE account_id = $1 AND message_id = $2
		AND chat_id IN (SELECT id FROM chats WHERE account_id = $1 AND jid = $3)
	`, accountID, messageID, chatJID, newBody)
	return err
}

func (r *MessageRepository) GetRecentStickers(ctx context.Context, accountID uuid.UUID, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT media_url FROM messages
		WHERE account_id = $1 AND message_type = 'sticker' AND media_url IS NOT NULL AND media_url != ''
		ORDER BY media_url DESC
		LIMIT $2
	`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	return urls, nil
}

func (r *ContactRepository) GetOrCreate(ctx context.Context, accountID uuid.UUID, deviceID *uuid.UUID, jid, phone, name, pushName string, isGroup bool) (*domain.Contact, error) {
	contact := &domain.Contact{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO contacts (account_id, device_id, jid, phone, name, push_name, is_group)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (account_id, jid) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), contacts.name),
			push_name = COALESCE(NULLIF(EXCLUDED.push_name, ''), contacts.push_name),
			phone = COALESCE(NULLIF(EXCLUDED.phone, ''), contacts.phone),
			updated_at = NOW()
		RETURNING id, account_id, device_id, jid, phone, name, last_name, short_name, custom_name, push_name, avatar_url, avatar_checked_at,
		          email, company, age, dni, birth_date, address, distrito, ocupacion, tags, notes, source, is_group, created_at, updated_at
	`, accountID, deviceID, jid, phone, name, pushName, isGroup).Scan(
		&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
		&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL, &contact.AvatarCheckedAt,
		&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
		&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
	)
	return contact, err
}

func (r *ContactRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Contact, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, device_id, jid, phone, name, last_name, short_name, custom_name, push_name, avatar_url,
		       email, company, age, dni, birth_date, address, distrito, ocupacion, tags, notes, source, is_group, created_at, updated_at
		FROM contacts WHERE account_id = $1 ORDER BY COALESCE(custom_name, name, push_name, phone) ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []*domain.Contact
	for rows.Next() {
		contact := &domain.Contact{}
		if err := rows.Scan(
			&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
			&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL,
			&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
			&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
		); err != nil {
			return nil, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (r *ContactRepository) GetByAccountIDWithFilters(ctx context.Context, accountID uuid.UUID, filter domain.ContactFilter) ([]*domain.Contact, int, error) {
	baseQuery := `
		FROM contacts
		WHERE account_id = $1 AND is_group = $2
	`
	args := []interface{}{accountID, filter.IsGroup}
	argNum := 3

	if filter.Search != "" {
		baseQuery += fmt.Sprintf(` AND (
			name ILIKE $%d OR last_name ILIKE $%d OR short_name ILIKE $%d OR custom_name ILIKE $%d OR push_name ILIKE $%d OR
			phone ILIKE $%d OR jid ILIKE $%d OR email ILIKE $%d OR company ILIKE $%d
		)`, argNum, argNum, argNum, argNum, argNum, argNum, argNum, argNum, argNum)
		args = append(args, "%"+filter.Search+"%")
		argNum++
	}

	if filter.DeviceID != nil {
		baseQuery += fmt.Sprintf(" AND device_id = $%d", argNum)
		args = append(args, *filter.DeviceID)
		argNum++
	}

	if filter.HasPhone {
		baseQuery += " AND phone IS NOT NULL AND phone != ''"
	}

	if len(filter.Tags) > 0 {
		baseQuery += fmt.Sprintf(" AND tags && $%d", argNum)
		args = append(args, filter.Tags)
		argNum++
	}

	if len(filter.TagIDs) > 0 {
		baseQuery += fmt.Sprintf(" AND id IN (SELECT contact_id FROM contact_tags WHERE tag_id = ANY($%d))", argNum)
		args = append(args, filter.TagIDs)
		argNum++
	}

	if len(filter.MatchingContactIDs) > 0 {
		baseQuery += fmt.Sprintf(" AND id = ANY($%d)", argNum)
		args = append(args, filter.MatchingContactIDs)
		argNum++
	}

	if len(filter.TagNames) > 0 {
		if filter.TagMode == "AND" {
			baseQuery += fmt.Sprintf(
				" AND id IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d) GROUP BY ct.contact_id HAVING COUNT(DISTINCT t.name) = $%d)",
				argNum, argNum+1,
			)
			args = append(args, filter.TagNames, len(filter.TagNames))
			argNum += 2
		} else {
			baseQuery += fmt.Sprintf(
				" AND id IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d))",
				argNum,
			)
			args = append(args, filter.TagNames)
			argNum++
		}
	}

	if len(filter.ExcludeTagNames) > 0 {
		baseQuery += fmt.Sprintf(
			" AND id NOT IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d))",
			argNum,
		)
		args = append(args, filter.ExcludeTagNames)
		argNum++
	}

	if len(filter.CfFilterContactIDs) > 0 {
		baseQuery += fmt.Sprintf(" AND id = ANY($%d)", argNum)
		args = append(args, filter.CfFilterContactIDs)
		argNum++
	}

	if filter.DateField == "created_at" || filter.DateField == "updated_at" {
		if filter.DateFrom != "" {
			baseQuery += fmt.Sprintf(" AND %s >= $%d", filter.DateField, argNum)
			args = append(args, filter.DateFrom)
			argNum++
		}
		if filter.DateTo != "" {
			baseQuery += fmt.Sprintf(" AND %s < $%d", filter.DateField, argNum)
			args = append(args, filter.DateTo)
			argNum++
		}
	}

	// Count
	var total int
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) "+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Select with last activity from chats + lead count
	selectQuery := `
		SELECT c.id, c.account_id, c.device_id, c.jid, c.phone, c.name, c.last_name, c.short_name, c.custom_name, c.push_name, c.avatar_url,
		       c.email, c.company, c.age, c.dni, c.birth_date, c.address, c.distrito, c.ocupacion, c.tags, c.notes, c.source, c.is_group, c.created_at, c.updated_at,
		       c.google_sync, c.google_synced_at, c.google_sync_error,
		       ch_agg.last_activity,
		       COALESCE(lc.cnt, 0) AS lead_count
		FROM contacts c
		LEFT JOIN (
			SELECT ch.contact_id, MAX(ch.last_message_at) AS last_activity
			FROM chats ch
			WHERE ch.account_id = $1
			GROUP BY ch.contact_id
		) ch_agg ON ch_agg.contact_id = c.id
		LEFT JOIN (
			SELECT contact_id, COUNT(*) AS cnt
			FROM leads
			WHERE account_id = $1 AND contact_id IS NOT NULL AND is_archived = false AND is_blocked = false
			GROUP BY contact_id
		) lc ON lc.contact_id = c.id
		WHERE c.account_id = $1 AND c.is_group = $2
	`

	// Re-apply filters with c. prefix
	selectArgs := []interface{}{accountID, filter.IsGroup}
	selectArgNum := 3

	if filter.Search != "" {
		selectQuery += fmt.Sprintf(` AND (
			c.name ILIKE $%d OR c.last_name ILIKE $%d OR c.short_name ILIKE $%d OR c.custom_name ILIKE $%d OR c.push_name ILIKE $%d OR
			c.phone ILIKE $%d OR c.jid ILIKE $%d OR c.email ILIKE $%d OR c.company ILIKE $%d
		)`, selectArgNum, selectArgNum, selectArgNum, selectArgNum, selectArgNum, selectArgNum, selectArgNum, selectArgNum, selectArgNum)
		selectArgs = append(selectArgs, "%"+filter.Search+"%")
		selectArgNum++
	}
	if filter.DeviceID != nil {
		selectQuery += fmt.Sprintf(" AND c.device_id = $%d", selectArgNum)
		selectArgs = append(selectArgs, *filter.DeviceID)
		selectArgNum++
	}
	if filter.HasPhone {
		selectQuery += " AND c.phone IS NOT NULL AND c.phone != ''"
	}
	if len(filter.Tags) > 0 {
		selectQuery += fmt.Sprintf(" AND c.tags && $%d", selectArgNum)
		selectArgs = append(selectArgs, filter.Tags)
		selectArgNum++
	}
	if len(filter.TagIDs) > 0 {
		selectQuery += fmt.Sprintf(" AND c.id IN (SELECT contact_id FROM contact_tags WHERE tag_id = ANY($%d))", selectArgNum)
		selectArgs = append(selectArgs, filter.TagIDs)
		selectArgNum++
	}

	if len(filter.MatchingContactIDs) > 0 {
		selectQuery += fmt.Sprintf(" AND c.id = ANY($%d)", selectArgNum)
		selectArgs = append(selectArgs, filter.MatchingContactIDs)
		selectArgNum++
	}

	if len(filter.TagNames) > 0 {
		if filter.TagMode == "AND" {
			selectQuery += fmt.Sprintf(
				" AND c.id IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d) GROUP BY ct.contact_id HAVING COUNT(DISTINCT t.name) = $%d)",
				selectArgNum, selectArgNum+1,
			)
			selectArgs = append(selectArgs, filter.TagNames, len(filter.TagNames))
			selectArgNum += 2
		} else {
			selectQuery += fmt.Sprintf(
				" AND c.id IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d))",
				selectArgNum,
			)
			selectArgs = append(selectArgs, filter.TagNames)
			selectArgNum++
		}
	}

	if len(filter.ExcludeTagNames) > 0 {
		selectQuery += fmt.Sprintf(
			" AND c.id NOT IN (SELECT ct.contact_id FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id WHERE t.name = ANY($%d))",
			selectArgNum,
		)
		selectArgs = append(selectArgs, filter.ExcludeTagNames)
		selectArgNum++
	}

	if len(filter.CfFilterContactIDs) > 0 {
		selectQuery += fmt.Sprintf(" AND c.id = ANY($%d)", selectArgNum)
		selectArgs = append(selectArgs, filter.CfFilterContactIDs)
		selectArgNum++
	}

	if filter.DateField == "created_at" || filter.DateField == "updated_at" {
		if filter.DateFrom != "" {
			selectQuery += fmt.Sprintf(" AND c.%s >= $%d", filter.DateField, selectArgNum)
			selectArgs = append(selectArgs, filter.DateFrom)
			selectArgNum++
		}
		if filter.DateTo != "" {
			selectQuery += fmt.Sprintf(" AND c.%s < $%d", filter.DateField, selectArgNum)
			selectArgs = append(selectArgs, filter.DateTo)
			selectArgNum++
		}
	}
	_ = selectArgNum

	// Dynamic sort
	switch filter.SortBy {
	case "name":
		ord := "ASC"
		if filter.SortOrder == "desc" {
			ord = "DESC"
		}
		selectQuery += " ORDER BY COALESCE(NULLIF(c.push_name,''), c.jid) " + ord + ", c.updated_at DESC"
	case "lead_count":
		ord := "DESC"
		if filter.SortOrder == "asc" {
			ord = "ASC"
		}
		selectQuery += " ORDER BY lead_count " + ord + " NULLS LAST, c.updated_at DESC"
	case "created_at":
		ord := "DESC"
		if filter.SortOrder == "asc" {
			ord = "ASC"
		}
		selectQuery += " ORDER BY c.created_at " + ord + ", c.updated_at DESC"
	default:
		selectQuery += " ORDER BY ch_agg.last_activity DESC NULLS LAST, c.updated_at DESC"
	}

	if filter.Limit > 0 {
		selectQuery += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			selectQuery += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := r.db.Query(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var contacts []*domain.Contact
	for rows.Next() {
		contact := &domain.Contact{}
		if err := rows.Scan(
			&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
			&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL,
			&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
			&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
			&contact.GoogleSync, &contact.GoogleSyncedAt, &contact.GoogleSyncError,
			&contact.LastActivity,
			&contact.LeadCount,
		); err != nil {
			return nil, 0, err
		}
		contacts = append(contacts, contact)
	}
	return contacts, total, nil
}

func (r *ContactRepository) GetByJID(ctx context.Context, accountID uuid.UUID, jid string) (*domain.Contact, error) {
	contact := &domain.Contact{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, device_id, jid, phone, name, last_name, short_name, custom_name, push_name, avatar_url,
		       email, company, age, dni, birth_date, address, distrito, ocupacion, tags, notes, source, is_group, created_at, updated_at
		FROM contacts WHERE account_id = $1 AND jid = $2
	`, accountID, jid).Scan(
		&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
		&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL,
		&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
		&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return contact, err
}

func (r *ContactRepository) GetByPhone(ctx context.Context, accountID uuid.UUID, phone string) (*domain.Contact, error) {
	contact := &domain.Contact{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, device_id, jid, phone, name, last_name, short_name, custom_name, push_name, avatar_url,
		       email, company, age, dni, birth_date, address, distrito, ocupacion, tags, notes, source, is_group, created_at, updated_at
		FROM contacts WHERE account_id = $1 AND phone = $2
		LIMIT 1
	`, accountID, phone).Scan(
		&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
		&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL,
		&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
		&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return contact, err
}

func (r *ContactRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Contact, error) {
	contact := &domain.Contact{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, device_id, jid, phone, name, last_name, short_name, custom_name, push_name, avatar_url,
		       email, company, age, dni, birth_date, address, distrito, ocupacion, tags, notes, source, is_group, created_at, updated_at,
		       google_sync, google_resource_name, google_synced_at, google_sync_error
		FROM contacts WHERE id = $1
	`, id).Scan(
		&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
		&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL,
		&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
		&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
		&contact.GoogleSync, &contact.GoogleResourceName, &contact.GoogleSyncedAt, &contact.GoogleSyncError,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return contact, err
}

func (r *ContactRepository) Update(ctx context.Context, contact *domain.Contact) error {
	_, err := r.db.Exec(ctx, `
		UPDATE contacts SET
			name = $1, last_name = $2, short_name = $3, custom_name = $4, push_name = $5,
			email = $6, company = $7, age = $8,
			tags = $9, notes = $10, phone = $11, dni = $12, birth_date = $13, address = $14, distrito = $15, ocupacion = $16, source = $17, updated_at = NOW()
		WHERE id = $18
	`, contact.Name, contact.LastName, contact.ShortName, contact.CustomName, contact.PushName, contact.Email, contact.Company,
		contact.Age, contact.Tags, contact.Notes, contact.Phone, contact.DNI, contact.BirthDate, contact.Address, contact.Distrito, contact.Ocupacion, contact.Source, contact.ID)
	return err
}

// SyncToParticipants propagates contact fields to all linked event_participants and campaign_recipients
func (r *ContactRepository) SyncToParticipants(ctx context.Context, contact *domain.Contact) error {
	name := contact.DisplayName()
	_, err := r.db.Exec(ctx, `
		UPDATE event_participants SET
			name = $2,
			last_name = COALESCE($3, last_name),
			short_name = COALESCE($4, short_name),
			phone = COALESCE($5, phone),
			email = COALESCE($6, email),
			age = COALESCE($7, age),
			updated_at = NOW()
		WHERE contact_id = $1
	`, contact.ID, name, contact.LastName, contact.ShortName, contact.Phone, contact.Email, contact.Age)
	if err != nil {
		log.Printf("[SYNC] Error syncing contact %s to event_participants: %v", contact.ID, err)
	}
	_, err = r.db.Exec(ctx, `
		UPDATE campaign_recipients SET name = $2, phone = COALESCE($3, phone)
		WHERE contact_id = $1
	`, contact.ID, name, contact.Phone)
	if err != nil {
		log.Printf("[SYNC] Error syncing contact %s to campaign_recipients: %v", contact.ID, err)
	}
	return nil
}

// SyncToLead propagates contact name to linked lead
func (r *ContactRepository) SyncToLead(ctx context.Context, contact *domain.Contact) error {
	name := contact.DisplayName()
	_, err := r.db.Exec(ctx, `
		UPDATE leads SET
			name = $2,
			last_name = COALESCE($3, last_name),
			short_name = COALESCE($4, short_name),
			phone = COALESCE($5, phone),
			email = COALESCE($6, email),
			age = COALESCE($7, age),
			dni = COALESCE($8, dni),
			birth_date = COALESCE($9, birth_date),
			address = COALESCE($10, address),
			updated_at = NOW()
		WHERE contact_id = $1
	`, contact.ID, name, contact.LastName, contact.ShortName, contact.Phone, contact.Email, contact.Age, contact.DNI, contact.BirthDate, contact.Address)
	if err != nil {
		log.Printf("[SYNC] Error syncing contact %s to leads: %v", contact.ID, err)
	}
	return nil
}

func (r *ContactRepository) ClaimAvatarRefresh(ctx context.Context, accountID uuid.UUID, jid string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	var claimed bool
	err := r.db.QueryRow(ctx, `
		UPDATE contacts
		SET avatar_checked_at = NOW()
		WHERE account_id = $1
		  AND jid = $2
		  AND (avatar_checked_at IS NULL OR avatar_checked_at < NOW() - $3::interval)
		RETURNING TRUE
	`, accountID, jid, fmt.Sprintf("%d seconds", int(ttl.Seconds()))).Scan(&claimed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return claimed, err
}

func (r *ContactRepository) UpdateAvatarURL(ctx context.Context, accountID uuid.UUID, jid, avatarURL string) (bool, error) {
	var changed bool
	err := r.db.QueryRow(ctx, `
		WITH previous AS (
			SELECT avatar_url FROM contacts WHERE account_id = $2 AND jid = $3
		), updated AS (
			UPDATE contacts
			SET avatar_url = $1,
			    avatar_checked_at = NOW(),
			    updated_at = CASE WHEN avatar_url IS DISTINCT FROM $1 THEN NOW() ELSE updated_at END
			WHERE account_id = $2 AND jid = $3
			RETURNING TRUE
		)
		SELECT COALESCE((SELECT avatar_url FROM previous) IS DISTINCT FROM $1, FALSE)
	`, avatarURL, accountID, jid).Scan(&changed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return changed, err
}

func (r *ContactRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM contacts WHERE id = $1`, id)
	return err
}

func (r *ContactRepository) DeleteBatch(ctx context.Context, ids []uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM contacts WHERE id = ANY($1)`, ids)
	return err
}

func (r *ContactRepository) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM contacts WHERE account_id = $1`, accountID)
	return err
}

func (r *ContactRepository) FindDuplicates(ctx context.Context, accountID uuid.UUID) ([][]*domain.Contact, error) {
	// Find contacts with the same phone number
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.jid, c.phone, c.name, c.last_name, c.short_name, c.custom_name, c.push_name,
		       c.avatar_url, c.email, c.company, c.age, c.dni, c.birth_date, c.address, c.distrito, c.ocupacion, c.tags, c.notes, c.source, c.is_group, c.created_at, c.updated_at
		FROM contacts c
		INNER JOIN (
			SELECT phone FROM contacts
			WHERE account_id = $1 AND phone IS NOT NULL AND phone != '' AND is_group = FALSE
			GROUP BY phone HAVING COUNT(*) > 1
		) dup ON c.phone = dup.phone
		WHERE c.account_id = $1 AND c.is_group = FALSE
		ORDER BY c.phone, c.updated_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grouped := make(map[string][]*domain.Contact)
	var order []string
	for rows.Next() {
		contact := &domain.Contact{}
		if err := rows.Scan(
			&contact.ID, &contact.AccountID, &contact.DeviceID, &contact.JID, &contact.Phone,
			&contact.Name, &contact.LastName, &contact.ShortName, &contact.CustomName, &contact.PushName, &contact.AvatarURL,
			&contact.Email, &contact.Company, &contact.Age, &contact.DNI, &contact.BirthDate, &contact.Address, &contact.Distrito, &contact.Ocupacion, &contact.Tags, &contact.Notes, &contact.Source,
			&contact.IsGroup, &contact.CreatedAt, &contact.UpdatedAt,
		); err != nil {
			return nil, err
		}
		phone := ""
		if contact.Phone != nil {
			phone = *contact.Phone
		}
		if _, exists := grouped[phone]; !exists {
			order = append(order, phone)
		}
		grouped[phone] = append(grouped[phone], contact)
	}

	var result [][]*domain.Contact
	for _, phone := range order {
		result = append(result, grouped[phone])
	}
	return result, nil
}

// GetContactsWithDuplicateLeads returns the count of contacts that have 2+ active (non-archived, non-blocked) leads.
func (r *ContactRepository) GetContactsWithDuplicateLeads(ctx context.Context, accountID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT contact_id
			FROM leads
			WHERE account_id = $1 AND contact_id IS NOT NULL AND is_archived = false AND is_blocked = false
			GROUP BY contact_id
			HAVING COUNT(*) > 1
		) dup
	`, accountID).Scan(&count)
	return count, err
}

func (r *ContactRepository) MergeContacts(ctx context.Context, keepID uuid.UUID, mergeIDs []uuid.UUID) error {
	// Update chats to point to the kept contact's JID
	keepContact, err := r.GetByID(ctx, keepID)
	if err != nil || keepContact == nil {
		return fmt.Errorf("contact to keep not found")
	}

	for _, mergeID := range mergeIDs {
		mergeContact, err := r.GetByID(ctx, mergeID)
		if err != nil || mergeContact == nil {
			continue
		}
		// Update chats JID references
		_, _ = r.db.Exec(ctx, `
			UPDATE chats SET jid = $1, updated_at = NOW()
			WHERE account_id = $2 AND jid = $3
		`, keepContact.JID, keepContact.AccountID, mergeContact.JID)

		// Update leads JID references
		_, _ = r.db.Exec(ctx, `
			UPDATE leads SET jid = $1, updated_at = NOW()
			WHERE account_id = $2 AND jid = $3
		`, keepContact.JID, keepContact.AccountID, mergeContact.JID)

		// Move device names to the kept contact
		// First delete any rows that would conflict (same device already has a name for keepID)
		_, _ = r.db.Exec(ctx, `
			DELETE FROM contact_device_names WHERE contact_id = $1
			AND device_id IN (SELECT device_id FROM contact_device_names WHERE contact_id = $2)
		`, mergeID, keepID)
		// Then move remaining device names
		_, _ = r.db.Exec(ctx, `
			UPDATE contact_device_names SET contact_id = $1 WHERE contact_id = $2
		`, keepID, mergeID)

		// Delete merged contact
		_, _ = r.db.Exec(ctx, `DELETE FROM contacts WHERE id = $1`, mergeID)
	}
	return nil
}

// ContactDeviceNameRepository handles per-device contact names
type ContactDeviceNameRepository struct {
	db *pgxpool.Pool
}

func (r *ContactDeviceNameRepository) Upsert(ctx context.Context, cdn *domain.ContactDeviceName) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO contact_device_names (contact_id, device_id, name, push_name, business_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (contact_id, device_id) DO UPDATE SET
			name = COALESCE(EXCLUDED.name, contact_device_names.name),
			push_name = COALESCE(EXCLUDED.push_name, contact_device_names.push_name),
			business_name = COALESCE(EXCLUDED.business_name, contact_device_names.business_name),
			synced_at = NOW()
		RETURNING id, synced_at
	`, cdn.ContactID, cdn.DeviceID, cdn.Name, cdn.PushName, cdn.BusinessName).Scan(&cdn.ID, &cdn.SyncedAt)
}

func (r *ContactDeviceNameRepository) GetByContactID(ctx context.Context, contactID uuid.UUID) ([]domain.ContactDeviceName, error) {
	rows, err := r.db.Query(ctx, `
		SELECT cdn.id, cdn.contact_id, cdn.device_id, cdn.name, cdn.push_name, cdn.business_name, cdn.synced_at,
		       d.name as device_name
		FROM contact_device_names cdn
		LEFT JOIN devices d ON d.id = cdn.device_id
		WHERE cdn.contact_id = $1
		ORDER BY cdn.synced_at DESC
	`, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []domain.ContactDeviceName
	for rows.Next() {
		cdn := domain.ContactDeviceName{}
		if err := rows.Scan(
			&cdn.ID, &cdn.ContactID, &cdn.DeviceID, &cdn.Name, &cdn.PushName,
			&cdn.BusinessName, &cdn.SyncedAt, &cdn.DeviceName,
		); err != nil {
			return nil, err
		}
		names = append(names, cdn)
	}
	return names, nil
}

// LeadRepository handles lead data access
type LeadRepository struct {
	db *pgxpool.Pool
}

func (r *LeadRepository) Create(ctx context.Context, lead *domain.Lead) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO leads (account_id, contact_id, jid, name, phone, email, notes, dni, birth_date, status, source, pipeline_id, stage_id, tags, custom_fields, assigned_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, created_at, updated_at
	`, lead.AccountID, lead.ContactID, lead.JID, lead.Name, lead.Phone, lead.Email, lead.Notes, lead.DNI, lead.BirthDate, lead.Status, lead.Source, lead.PipelineID, lead.StageID, lead.Tags, lead.CustomFields, lead.AssignedTo,
	).Scan(&lead.ID, &lead.CreatedAt, &lead.UpdatedAt)
}

func (r *LeadRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Lead, error) {
	rows, err := r.db.Query(ctx, `
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
		WHERE l.account_id = $1 ORDER BY l.created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var leads []*domain.Lead
	for rows.Next() {
		lead := &domain.Lead{}
		if err := rows.Scan(
			&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName, &lead.Phone,
			&lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes, &lead.Tags,
			&lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID, &lead.CreatedAt, &lead.UpdatedAt,
			&lead.StageName, &lead.StageColor, &lead.StagePosition, &lead.KommoID,
			&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
		); err != nil {
			return nil, err
		}
		leads = append(leads, lead)
	}
	return leads, nil
}

func (r *LeadRepository) GetByJID(ctx context.Context, accountID uuid.UUID, jid string) (*domain.Lead, error) {
	lead := &domain.Lead{}
	err := r.db.QueryRow(ctx, `
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
		WHERE l.account_id = $1 AND l.jid = $2
		ORDER BY l.updated_at DESC LIMIT 1
	`, accountID, jid).Scan(
		&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName, &lead.Phone,
		&lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes, &lead.Tags,
		&lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID, &lead.CreatedAt, &lead.UpdatedAt,
		&lead.StageName, &lead.StageColor, &lead.StagePosition, &lead.KommoID,
		&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return lead, err
}

func (r *LeadRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.Exec(ctx, `UPDATE leads SET status = $1, updated_at = NOW() WHERE id = $2`, status, id)
	return err
}

func (r *LeadRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Lead, error) {
	lead := &domain.Lead{}
	err := r.db.QueryRow(ctx, `
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
		WHERE l.id = $1
	`, id).Scan(
		&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName, &lead.Phone,
		&lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes, &lead.Tags,
		&lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID, &lead.CreatedAt, &lead.UpdatedAt,
		&lead.StageName, &lead.StageColor, &lead.StagePosition, &lead.KommoID,
		&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return lead, err
}

func (r *LeadRepository) Update(ctx context.Context, lead *domain.Lead) error {
	// CRM fields stay on leads table; personal data is written to contacts separately
	_, err := r.db.Exec(ctx, `
		UPDATE leads SET
			name = $1, last_name = $2, short_name = $3, phone = $4, email = $5,
			company = $6, age = $7, dni = $8, birth_date = $9, address = $10, distrito = $11, ocupacion = $12, notes = $13,
			status = $14, source = $15, tags = $16, custom_fields = $17, assigned_to = $18,
			pipeline_id = $19, stage_id = $20, updated_at = NOW()
		WHERE id = $21
	`, lead.Name, lead.LastName, lead.ShortName, lead.Phone, lead.Email,
		lead.Company, lead.Age, lead.DNI, lead.BirthDate, lead.Address, lead.Distrito, lead.Ocupacion, lead.Notes,
		lead.Status, lead.Source, lead.Tags, lead.CustomFields, lead.AssignedTo,
		lead.PipelineID, lead.StageID, lead.ID)
	if err != nil {
		return err
	}
	// Also write personal data to linked contact (source of truth)
	if lead.ContactID != nil {
		_, err = r.db.Exec(ctx, `
			UPDATE contacts SET
				custom_name = $1, last_name = $2, short_name = $3, phone = $4, email = $5,
				company = $6, age = $7, dni = $8, birth_date = $9, address = $10, distrito = $11, ocupacion = $12, notes = $13,
				updated_at = NOW()
			WHERE id = $14
		`, lead.Name, lead.LastName, lead.ShortName, lead.Phone, lead.Email,
			lead.Company, lead.Age, lead.DNI, lead.BirthDate, lead.Address, lead.Distrito, lead.Ocupacion, lead.Notes, *lead.ContactID)
	}
	return err
}

// UpdateStage moves a lead to a different pipeline stage
func (r *LeadRepository) UpdateStage(ctx context.Context, id uuid.UUID, stageID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE leads SET stage_id = $1, updated_at = NOW() WHERE id = $2`, stageID, id)
	return err
}

// SyncToContact is now a no-op — Contact is the source of truth and Lead.Update writes to contacts directly.
func (r *LeadRepository) SyncToContact(ctx context.Context, lead *domain.Lead) error {
	if lead.ContactID == nil {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		UPDATE contacts SET
			custom_name = COALESCE($2, custom_name),
			last_name = COALESCE($3, last_name),
			short_name = COALESCE($4, short_name),
			phone = COALESCE($5, phone),
			email = COALESCE($6, email),
			age = COALESCE($7, age),
			dni = COALESCE($8, dni),
			birth_date = COALESCE($9, birth_date),
			address = COALESCE($10, address),
			distrito = COALESCE(NULLIF($11, ''), distrito),
			ocupacion = COALESCE(NULLIF($12, ''), ocupacion),
			updated_at = NOW()
		WHERE id = $1
	`, *lead.ContactID, lead.Name, lead.LastName, lead.ShortName, lead.Phone, lead.Email, lead.Age, lead.DNI, lead.BirthDate, lead.Address, lead.Distrito, lead.Ocupacion)
	if err != nil {
		log.Printf("[SYNC] Error syncing lead %s to contact: %v", lead.ID, err)
	}
	return nil
}

// GetByContactID finds a lead linked to a specific contact
func (r *LeadRepository) GetByContactID(ctx context.Context, contactID uuid.UUID) (*domain.Lead, error) {
	lead := &domain.Lead{}
	err := r.db.QueryRow(ctx, `
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
		WHERE l.contact_id = $1
	`, contactID).Scan(
		&lead.ID, &lead.AccountID, &lead.ContactID, &lead.JID, &lead.Name, &lead.LastName, &lead.ShortName, &lead.Phone,
		&lead.Email, &lead.Company, &lead.Age, &lead.DNI, &lead.BirthDate, &lead.Address, &lead.Distrito, &lead.Ocupacion, &lead.Status, &lead.Source, &lead.Notes, &lead.Tags,
		&lead.CustomFields, &lead.AssignedTo, &lead.PipelineID, &lead.StageID, &lead.CreatedAt, &lead.UpdatedAt,
		&lead.StageName, &lead.StageColor, &lead.StagePosition, &lead.KommoID,
		&lead.IsArchived, &lead.ArchivedAt, &lead.IsBlocked, &lead.BlockedAt, &lead.BlockReason, &lead.KommoDeletedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return lead, err
}

func (r *LeadRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM leads WHERE id = $1`, id)
	return err
}

func (r *LeadRepository) DeleteBatch(ctx context.Context, ids []uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM leads WHERE id = ANY($1)`, ids)
	return err
}

// GetContactIDForLead returns the contact_id linked to a lead, or nil.
func (r *LeadRepository) GetContactIDForLead(ctx context.Context, leadID uuid.UUID) (*uuid.UUID, error) {
	var contactID *uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT contact_id FROM leads WHERE id = $1`, leadID).Scan(&contactID)
	if err != nil {
		return nil, err
	}
	return contactID, nil
}

func (r *LeadRepository) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM leads WHERE account_id = $1`, accountID)
	return err
}

// ArchiveLead sets or clears the archived flag on a lead.
func (r *LeadRepository) ArchiveLead(ctx context.Context, id uuid.UUID, archive bool, reason string) error {
	if archive {
		_, err := r.db.Exec(ctx, `UPDATE leads SET is_archived = true, archived_at = NOW(), archive_reason = $2, updated_at = NOW() WHERE id = $1`, id, reason)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE leads SET is_archived = false, archived_at = NULL, archive_reason = '', updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ArchiveLeadsBatch sets or clears the archived flag on multiple leads.
func (r *LeadRepository) ArchiveLeadsBatch(ctx context.Context, ids []uuid.UUID, archive bool, reason string) error {
	if archive {
		_, err := r.db.Exec(ctx, `UPDATE leads SET is_archived = true, archived_at = NOW(), archive_reason = $2, updated_at = NOW() WHERE id = ANY($1)`, ids, reason)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE leads SET is_archived = false, archived_at = NULL, archive_reason = '', updated_at = NOW() WHERE id = ANY($1)`, ids)
	return err
}

// BlockLead sets or clears the blocked flag on a lead.
func (r *LeadRepository) BlockLead(ctx context.Context, id uuid.UUID, block bool, reason string) error {
	if block {
		_, err := r.db.Exec(ctx, `UPDATE leads SET is_blocked = true, blocked_at = NOW(), block_reason = $2, updated_at = NOW() WHERE id = $1`, id, reason)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE leads SET is_blocked = false, blocked_at = NULL, block_reason = '', updated_at = NOW() WHERE id = $1`, id)
	return err
}

// BlockLeadsBatch sets or clears the blocked flag on multiple leads.
func (r *LeadRepository) BlockLeadsBatch(ctx context.Context, ids []uuid.UUID, block bool, reason string) error {
	if block {
		_, err := r.db.Exec(ctx, `UPDATE leads SET is_blocked = true, blocked_at = NOW(), block_reason = $2, updated_at = NOW() WHERE id = ANY($1)`, ids, reason)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE leads SET is_blocked = false, blocked_at = NULL, block_reason = '', updated_at = NOW() WHERE id = ANY($1)`, ids)
	return err
}

// GetArchivedBlockedCounts returns the count of archived and blocked leads for an account.
func (r *LeadRepository) GetArchivedBlockedCounts(ctx context.Context, accountID uuid.UUID) (active, archived, blocked int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE NOT is_archived AND NOT is_blocked),
			COUNT(*) FILTER (WHERE is_archived AND NOT is_blocked),
			COUNT(*) FILTER (WHERE is_blocked)
		FROM leads WHERE account_id = $1
	`, accountID).Scan(&active, &archived, &blocked)
	return
}

// PipelineRepository handles pipeline data access
type PipelineRepository struct {
	db *pgxpool.Pool
}

func (r *PipelineRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Pipeline, error) {
	rows, err := r.db.Query(ctx, `
		SELECT p.id, p.account_id, p.name, p.description, p.is_default, p.kommo_id, p.created_at, p.updated_at
		FROM pipelines p WHERE p.account_id = $1 ORDER BY p.created_at
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipelines []*domain.Pipeline
	for rows.Next() {
		pipeline := &domain.Pipeline{}
		if err := rows.Scan(
			&pipeline.ID, &pipeline.AccountID, &pipeline.Name, &pipeline.Description,
			&pipeline.IsDefault, &pipeline.KommoID, &pipeline.CreatedAt, &pipeline.UpdatedAt,
		); err != nil {
			return nil, err
		}
		pipelines = append(pipelines, pipeline)
	}

	// Batch load all stages for all pipelines in one query
	if len(pipelines) > 0 {
		pipelineIDs := make([]uuid.UUID, len(pipelines))
		pipelineMap := make(map[uuid.UUID]*domain.Pipeline)
		for i, p := range pipelines {
			pipelineIDs[i] = p.ID
			pipelineMap[p.ID] = p
		}
		stageRows, err := r.db.Query(ctx, `
			SELECT ps.id, ps.pipeline_id, ps.name, ps.color, ps.position, ps.created_at,
			       (SELECT COUNT(*) FROM leads WHERE stage_id = ps.id) as lead_count
			FROM pipeline_stages ps WHERE ps.pipeline_id = ANY($1) ORDER BY ps.pipeline_id, ps.position
		`, pipelineIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to batch load stages: %w", err)
		}
		defer stageRows.Close()
		for stageRows.Next() {
			stage := &domain.PipelineStage{}
			if err := stageRows.Scan(
				&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
				&stage.Position, &stage.CreatedAt, &stage.LeadCount,
			); err != nil {
				return nil, err
			}
			if p, ok := pipelineMap[stage.PipelineID]; ok {
				p.Stages = append(p.Stages, stage)
			}
		}
	}

	return pipelines, nil
}

func (r *PipelineRepository) GetStages(ctx context.Context, pipelineID uuid.UUID) ([]*domain.PipelineStage, error) {
	rows, err := r.db.Query(ctx, `
		SELECT ps.id, ps.pipeline_id, ps.name, ps.color, ps.position, ps.created_at,
		       (SELECT COUNT(*) FROM leads WHERE stage_id = ps.id) as lead_count
		FROM pipeline_stages ps WHERE ps.pipeline_id = $1 ORDER BY ps.position
	`, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stages []*domain.PipelineStage
	for rows.Next() {
		stage := &domain.PipelineStage{}
		if err := rows.Scan(
			&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
			&stage.Position, &stage.CreatedAt, &stage.LeadCount,
		); err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}
	return stages, nil
}

func (r *PipelineRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Pipeline, error) {
	pipeline := &domain.Pipeline{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, name, description, is_default, created_at, updated_at
		FROM pipelines WHERE id = $1
	`, id).Scan(
		&pipeline.ID, &pipeline.AccountID, &pipeline.Name, &pipeline.Description,
		&pipeline.IsDefault, &pipeline.CreatedAt, &pipeline.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stages, err := r.GetStages(ctx, pipeline.ID)
	if err != nil {
		return nil, err
	}
	pipeline.Stages = stages
	return pipeline, nil
}

func (r *PipelineRepository) Create(ctx context.Context, pipeline *domain.Pipeline) error {
	pipeline.ID = uuid.New()
	now := time.Now()
	pipeline.CreatedAt = now
	pipeline.UpdatedAt = now
	_, err := r.db.Exec(ctx, `
		INSERT INTO pipelines (id, account_id, name, description, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, pipeline.ID, pipeline.AccountID, pipeline.Name, pipeline.Description, pipeline.IsDefault, pipeline.CreatedAt, pipeline.UpdatedAt)
	return err
}

func (r *PipelineRepository) Update(ctx context.Context, pipeline *domain.Pipeline) error {
	pipeline.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE pipelines SET name = $1, description = $2, updated_at = $3 WHERE id = $4
	`, pipeline.Name, pipeline.Description, pipeline.UpdatedAt, pipeline.ID)
	return err
}

func (r *PipelineRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// Unlink leads from this pipeline's stages first
	_, _ = r.db.Exec(ctx, `UPDATE leads SET pipeline_id = NULL, stage_id = NULL WHERE pipeline_id = $1`, id)
	// Delete stages (FK cascade would also do it)
	_, _ = r.db.Exec(ctx, `DELETE FROM pipeline_stages WHERE pipeline_id = $1`, id)
	_, err := r.db.Exec(ctx, `DELETE FROM pipelines WHERE id = $1`, id)
	return err
}

func (r *PipelineRepository) CreateStage(ctx context.Context, stage *domain.PipelineStage) error {
	stage.ID = uuid.New()
	stage.CreatedAt = time.Now()
	// Auto-set position to the end
	if stage.Position == 0 {
		var maxPos *int
		r.db.QueryRow(ctx, `SELECT MAX(position) FROM pipeline_stages WHERE pipeline_id = $1`, stage.PipelineID).Scan(&maxPos)
		if maxPos != nil {
			stage.Position = *maxPos + 1
		}
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO pipeline_stages (id, pipeline_id, name, color, position, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, stage.ID, stage.PipelineID, stage.Name, stage.Color, stage.Position, stage.CreatedAt)
	return err
}

func (r *PipelineRepository) UpdateStage(ctx context.Context, stage *domain.PipelineStage) error {
	_, err := r.db.Exec(ctx, `
		UPDATE pipeline_stages SET name = $1, color = $2, position = $3 WHERE id = $4
	`, stage.Name, stage.Color, stage.Position, stage.ID)
	return err
}

func (r *PipelineRepository) DeleteStage(ctx context.Context, id uuid.UUID) error {
	// Move leads in this stage to no stage
	_, _ = r.db.Exec(ctx, `UPDATE leads SET stage_id = NULL WHERE stage_id = $1`, id)
	_, err := r.db.Exec(ctx, `DELETE FROM pipeline_stages WHERE id = $1`, id)
	return err
}

func (r *PipelineRepository) ReorderStages(ctx context.Context, pipelineID uuid.UUID, stageIDs []uuid.UUID) error {
	for i, stageID := range stageIDs {
		_, err := r.db.Exec(ctx, `UPDATE pipeline_stages SET position = $1 WHERE id = $2 AND pipeline_id = $3`, i, stageID, pipelineID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *PipelineRepository) GetDefaultPipeline(ctx context.Context, accountID uuid.UUID) (*domain.Pipeline, error) {
	pipeline := &domain.Pipeline{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, name, description, is_default, created_at, updated_at
		FROM pipelines WHERE account_id = $1 AND is_default = TRUE LIMIT 1
	`, accountID).Scan(
		&pipeline.ID, &pipeline.AccountID, &pipeline.Name, &pipeline.Description,
		&pipeline.IsDefault, &pipeline.CreatedAt, &pipeline.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stages, err := r.GetStages(ctx, pipeline.ID)
	if err != nil {
		return nil, err
	}
	pipeline.Stages = stages
	return pipeline, nil
}

// GetActivePipeline returns the pipeline connected to Kommo (enabled=TRUE), falling back to is_default, then any pipeline.
func (r *PipelineRepository) GetActivePipeline(ctx context.Context, accountID uuid.UUID) (*domain.Pipeline, error) {
	// 1. Try the Kommo-connected pipeline
	var pipelineID uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT pipeline_id FROM kommo_connected_pipelines
		WHERE account_id = $1 AND enabled = TRUE AND pipeline_id IS NOT NULL LIMIT 1
	`, accountID).Scan(&pipelineID)
	if err == nil {
		return r.GetByID(ctx, pipelineID)
	}
	// 2. Fallback to default pipeline
	return r.GetDefaultPipeline(ctx, accountID)
}

func (r *PipelineRepository) ResolveStageDestination(ctx context.Context, accountID, stageID uuid.UUID) (*uuid.UUID, *uuid.UUID, error) {
	var pipelineID uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT ps.pipeline_id
		FROM pipeline_stages ps
		JOIN pipelines p ON p.id = ps.pipeline_id
		WHERE ps.id = $1 AND p.account_id = $2
	`, stageID, accountID).Scan(&pipelineID)
	if err == pgx.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	pid := pipelineID
	sid := stageID
	return &pid, &sid, nil
}

func (r *PipelineRepository) ResolveIncomingLeadDestination(ctx context.Context, accountID uuid.UUID) (*uuid.UUID, *uuid.UUID, error) {
	var configuredPipelineID, configuredStageID uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT ps.pipeline_id, ps.id
		FROM accounts a
		JOIN pipeline_stages ps ON ps.id = a.default_incoming_stage_id
		JOIN pipelines p ON p.id = ps.pipeline_id
		WHERE a.id = $1 AND p.account_id = $1
	`, accountID).Scan(&configuredPipelineID, &configuredStageID)
	if err == nil {
		pid := configuredPipelineID
		sid := configuredStageID
		return &pid, &sid, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, nil, err
	}

	pickDestination := func(pipeline *domain.Pipeline) (*uuid.UUID, *uuid.UUID) {
		if pipeline == nil {
			return nil, nil
		}
		pid := pipeline.ID
		var stageID *uuid.UUID
		for _, st := range pipeline.Stages {
			if strings.EqualFold(st.Name, "Leads Entrantes") {
				sid := st.ID
				stageID = &sid
				break
			}
		}
		if stageID == nil && len(pipeline.Stages) > 0 {
			sid := pipeline.Stages[0].ID
			stageID = &sid
		}
		return &pid, stageID
	}

	pipeline, err := r.GetActivePipeline(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	pipelineID, stageID := pickDestination(pipeline)
	if stageID != nil {
		return pipelineID, stageID, nil
	}

	pipelines, err := r.GetByAccountID(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	for _, candidate := range pipelines {
		if pipeline != nil && candidate.ID == pipeline.ID {
			continue
		}
		candidatePipelineID, candidateStageID := pickDestination(candidate)
		if candidateStageID != nil {
			return candidatePipelineID, candidateStageID, nil
		}
	}
	return pipelineID, stageID, nil
}

// TagRepository handles tag data access
type TagRepository struct {
	db *pgxpool.Pool
}

func (r *TagRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, name, color, created_at, updated_at
		FROM tags WHERE account_id = $1 ORDER BY name
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (r *TagRepository) ListPaginated(ctx context.Context, accountID uuid.UUID, search string, limit, offset int) ([]*domain.Tag, int, error) {
	args := []interface{}{accountID}
	where := "WHERE account_id = $1"
	idx := 2
	if search != "" {
		where += fmt.Sprintf(" AND name ILIKE $%d", idx)
		args = append(args, "%"+search+"%")
		idx++
	}

	var total int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM tags "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	query := fmt.Sprintf("SELECT id, account_id, name, color, created_at, updated_at FROM tags %s ORDER BY name LIMIT $%d OFFSET $%d", where, idx, idx+1)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		tags = append(tags, t)
	}
	return tags, total, nil
}

func (r *TagRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tag, error) {
	t := &domain.Tag{}
	err := r.db.QueryRow(ctx, `SELECT id, account_id, name, color, created_at, updated_at FROM tags WHERE id = $1`, id).
		Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *TagRepository) Create(ctx context.Context, tag *domain.Tag) error {
	tag.ID = uuid.New()
	now := time.Now()
	tag.CreatedAt = now
	tag.UpdatedAt = now
	_, err := r.db.Exec(ctx, `
		INSERT INTO tags (id, account_id, name, color, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tag.ID, tag.AccountID, tag.Name, tag.Color, tag.CreatedAt, tag.UpdatedAt)
	return err
}

func (r *TagRepository) Update(ctx context.Context, tag *domain.Tag) error {
	tag.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE tags SET name = $1, color = $2, updated_at = $3 WHERE id = $4
	`, tag.Name, tag.Color, tag.UpdatedAt, tag.ID)
	return err
}

func (r *TagRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// Delete associations first
	r.db.Exec(ctx, `DELETE FROM contact_tags WHERE tag_id = $1`, id)
	r.db.Exec(ctx, `DELETE FROM chat_tags WHERE tag_id = $1`, id)
	_, err := r.db.Exec(ctx, `DELETE FROM tags WHERE id = $1`, id)
	return err
}

func (r *TagRepository) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	// Delete all tag associations for this account's tags
	r.db.Exec(ctx, `DELETE FROM contact_tags WHERE tag_id IN (SELECT id FROM tags WHERE account_id = $1)`, accountID)
	r.db.Exec(ctx, `DELETE FROM chat_tags WHERE tag_id IN (SELECT id FROM tags WHERE account_id = $1)`, accountID)
	_, err := r.db.Exec(ctx, `DELETE FROM tags WHERE account_id = $1`, accountID)
	return err
}

// SyncLeadTagsByNames populates contact_tags for a lead's linked contact from tag names.
// Creates tags that don't exist yet. Used by CSV import.
func (r *TagRepository) SyncLeadTagsByNames(ctx context.Context, accountID, leadID uuid.UUID, tagNames []string) error {
	// Resolve contact_id for this lead
	var contactID *uuid.UUID
	_ = r.db.QueryRow(ctx, `SELECT contact_id FROM leads WHERE id = $1`, leadID).Scan(&contactID)
	if contactID == nil {
		return nil // cannot assign tags without a linked contact
	}
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var tagID uuid.UUID
		err := r.db.QueryRow(ctx, `SELECT id FROM tags WHERE account_id = $1 AND name = $2`, accountID, name).Scan(&tagID)
		if err != nil {
			tagID = uuid.New()
			_, err = r.db.Exec(ctx, `
				INSERT INTO tags (id, account_id, name, color, created_at, updated_at)
				VALUES ($1, $2, $3, '#6366f1', NOW(), NOW())
				ON CONFLICT (account_id, name) DO NOTHING
			`, tagID, accountID, name)
			if err != nil {
				_ = r.db.QueryRow(ctx, `SELECT id FROM tags WHERE account_id = $1 AND name = $2`, accountID, name).Scan(&tagID)
			}
		}
		_, _ = r.db.Exec(ctx, `INSERT INTO contact_tags (contact_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, *contactID, tagID)
	}
	return nil
}

// SyncContactTagsByNames replaces all contact_tags for a contact with the given tag names.
// Creates tags that don't exist yet. Used by contact create/update handlers.
func (r *TagRepository) SyncContactTagsByNames(ctx context.Context, accountID, contactID uuid.UUID, tagNames []string) error {
	// Collect resolved tag IDs
	var tagIDs []uuid.UUID
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var tagID uuid.UUID
		err := r.db.QueryRow(ctx, `SELECT id FROM tags WHERE account_id = $1 AND name = $2`, accountID, name).Scan(&tagID)
		if err != nil {
			tagID = uuid.New()
			_, err = r.db.Exec(ctx, `
				INSERT INTO tags (id, account_id, name, color, created_at, updated_at)
				VALUES ($1, $2, $3, '#6366f1', NOW(), NOW())
				ON CONFLICT (account_id, name) DO NOTHING
			`, tagID, accountID, name)
			if err != nil {
				_ = r.db.QueryRow(ctx, `SELECT id FROM tags WHERE account_id = $1 AND name = $2`, accountID, name).Scan(&tagID)
			}
		}
		tagIDs = append(tagIDs, tagID)
	}
	// Remove old contact_tags and insert new ones
	_, _ = r.db.Exec(ctx, `DELETE FROM contact_tags WHERE contact_id = $1`, contactID)
	for _, tagID := range tagIDs {
		_, _ = r.db.Exec(ctx, `INSERT INTO contact_tags (contact_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, contactID, tagID)
	}
	return nil
}

func (r *TagRepository) AssignToContact(ctx context.Context, contactID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, contactID, tagID)
	return err
}

func (r *TagRepository) RemoveFromContact(ctx context.Context, contactID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM contact_tags WHERE contact_id = $1 AND tag_id = $2`, contactID, tagID)
	return err
}

// RecalculateContactTags replaces all contact_tags for a contact with the union of tags
// from their active (non-archived, non-blocked) leads. If no active leads remain, all tags are removed.
func (r *TagRepository) RecalculateContactTags(ctx context.Context, contactID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		WITH active_lead_tags AS (
			SELECT DISTINCT ct.tag_id
			FROM contact_tags ct
			JOIN leads l ON l.contact_id = ct.contact_id
			WHERE ct.contact_id = $1
			  AND l.contact_id = $1
			  AND l.is_archived = false
			  AND l.is_blocked = false
		)
		DELETE FROM contact_tags
		WHERE contact_id = $1
		  AND tag_id NOT IN (SELECT tag_id FROM active_lead_tags)
	`, contactID)
	return err
}

func (r *TagRepository) AssignToLead(ctx context.Context, leadID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO contact_tags (contact_id, tag_id)
		SELECT l.contact_id, $2 FROM leads l WHERE l.id = $1 AND l.contact_id IS NOT NULL
		ON CONFLICT DO NOTHING
	`, leadID, tagID)
	return err
}

func (r *TagRepository) RemoveFromLead(ctx context.Context, leadID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM contact_tags WHERE contact_id = (SELECT contact_id FROM leads WHERE id = $1) AND tag_id = $2
	`, leadID, tagID)
	return err
}

func (r *TagRepository) AssignToChat(ctx context.Context, chatID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO chat_tags (chat_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, chatID, tagID)
	return err
}

func (r *TagRepository) RemoveFromChat(ctx context.Context, chatID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM chat_tags WHERE chat_id = $1 AND tag_id = $2`, chatID, tagID)
	return err
}

func (r *TagRepository) GetByContact(ctx context.Context, contactID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.account_id, t.name, t.color, t.created_at, t.updated_at
		FROM tags t JOIN contact_tags ct ON ct.tag_id = t.id
		WHERE ct.contact_id = $1 ORDER BY t.name
	`, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// GetByContactsBatch loads tags for multiple contacts in a single query
func (r *TagRepository) GetByContactsBatch(ctx context.Context, contactIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	result := make(map[uuid.UUID][]*domain.Tag)
	if len(contactIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT ct.contact_id, t.id, t.account_id, t.name, t.color, t.created_at, t.updated_at
		FROM tags t JOIN contact_tags ct ON ct.tag_id = t.id
		WHERE ct.contact_id = ANY($1)
		ORDER BY ct.contact_id, t.name
	`, contactIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid uuid.UUID
		t := &domain.Tag{}
		if err := rows.Scan(&cid, &t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		result[cid] = append(result[cid], t)
	}
	return result, nil
}

func (r *TagRepository) GetByLead(ctx context.Context, leadID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.account_id, t.name, t.color, t.created_at, t.updated_at
		FROM tags t
		JOIN contact_tags ct ON ct.tag_id = t.id
		JOIN leads l ON l.contact_id = ct.contact_id
		WHERE l.id = $1 ORDER BY t.name
	`, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (r *TagRepository) GetByChat(ctx context.Context, chatID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.account_id, t.name, t.color, t.created_at, t.updated_at
		FROM tags t JOIN chat_tags cht ON cht.tag_id = t.id
		WHERE cht.chat_id = $1 ORDER BY t.name
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

func (r *TagRepository) AssignToParticipant(ctx context.Context, participantID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO participant_tags (participant_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, participantID, tagID)
	return err
}

func (r *TagRepository) RemoveFromParticipant(ctx context.Context, participantID, tagID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM participant_tags WHERE participant_id = $1 AND tag_id = $2`, participantID, tagID)
	return err
}

func (r *TagRepository) GetByParticipant(ctx context.Context, participantID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.account_id, t.name, t.color, t.created_at, t.updated_at
		FROM tags t JOIN participant_tags pt ON pt.tag_id = t.id
		WHERE pt.participant_id = $1 ORDER BY t.name
	`, participantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// CampaignRepository handles campaign data access
type CampaignRepository struct {
	db *pgxpool.Pool
}

func (r *CampaignRepository) Create(ctx context.Context, c *domain.Campaign) error {
	c.ID = uuid.New()
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = domain.CampaignStatusDraft
	}
	if c.Settings == nil {
		c.Settings = domain.DefaultCampaignSettings()
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO campaigns (id, account_id, device_id, name, message_template, media_url, media_type, status, scheduled_at, settings, total_recipients, sent_count, failed_count, event_id, source, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
	`, c.ID, c.AccountID, c.DeviceID, c.Name, c.MessageTemplate, c.MediaURL, c.MediaType,
		c.Status, c.ScheduledAt, c.Settings, c.TotalRecipients, c.SentCount, c.FailedCount, c.EventID, c.Source, c.CreatedBy, c.CreatedAt, c.UpdatedAt)
	return err
}

func (r *CampaignRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Campaign, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.name, c.message_template, c.media_url, c.media_type,
			c.status, c.scheduled_at, c.started_at, c.completed_at, c.total_recipients, c.sent_count, c.failed_count,
			c.settings, c.event_id, c.source, c.created_by, c.started_by, c.created_at, c.updated_at,
			d.name as device_name, uc.email as created_by_name, us.email as started_by_name
		FROM campaigns c
		LEFT JOIN devices d ON d.id = c.device_id
		LEFT JOIN users uc ON uc.id = c.created_by
		LEFT JOIN users us ON us.id = c.started_by
		WHERE c.account_id = $1
		ORDER BY c.created_at DESC
		LIMIT 100
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []*domain.Campaign
	for rows.Next() {
		camp := &domain.Campaign{}
		if err := rows.Scan(
			&camp.ID, &camp.AccountID, &camp.DeviceID, &camp.Name, &camp.MessageTemplate,
			&camp.MediaURL, &camp.MediaType, &camp.Status, &camp.ScheduledAt, &camp.StartedAt,
			&camp.CompletedAt, &camp.TotalRecipients, &camp.SentCount, &camp.FailedCount,
			&camp.Settings, &camp.EventID, &camp.Source, &camp.CreatedBy, &camp.StartedBy,
			&camp.CreatedAt, &camp.UpdatedAt, &camp.DeviceName, &camp.CreatedByName, &camp.StartedByName,
		); err != nil {
			return nil, err
		}
		campaigns = append(campaigns, camp)
	}
	return campaigns, nil
}

func (r *CampaignRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error) {
	camp := &domain.Campaign{}
	err := r.db.QueryRow(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.name, c.message_template, c.media_url, c.media_type,
			c.status, c.scheduled_at, c.started_at, c.completed_at, c.total_recipients, c.sent_count, c.failed_count,
			c.settings, c.event_id, c.source, c.created_by, c.started_by, c.created_at, c.updated_at,
			d.name as device_name, uc.email as created_by_name, us.email as started_by_name
		FROM campaigns c
		LEFT JOIN devices d ON d.id = c.device_id
		LEFT JOIN users uc ON uc.id = c.created_by
		LEFT JOIN users us ON us.id = c.started_by
		WHERE c.id = $1
	`, id).Scan(
		&camp.ID, &camp.AccountID, &camp.DeviceID, &camp.Name, &camp.MessageTemplate,
		&camp.MediaURL, &camp.MediaType, &camp.Status, &camp.ScheduledAt, &camp.StartedAt,
		&camp.CompletedAt, &camp.TotalRecipients, &camp.SentCount, &camp.FailedCount,
		&camp.Settings, &camp.EventID, &camp.Source, &camp.CreatedBy, &camp.StartedBy,
		&camp.CreatedAt, &camp.UpdatedAt, &camp.DeviceName, &camp.CreatedByName, &camp.StartedByName,
	)
	if err != nil {
		return nil, err
	}
	return camp, nil
}

func (r *CampaignRepository) Update(ctx context.Context, c *domain.Campaign) error {
	c.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE campaigns SET name=$1, message_template=$2, media_url=$3, media_type=$4, status=$5,
			scheduled_at=$6, started_at=$7, completed_at=$8, total_recipients=$9, sent_count=$10,
			failed_count=$11, settings=$12, device_id=$13, started_by=$14, updated_at=$15
		WHERE id=$16
	`, c.Name, c.MessageTemplate, c.MediaURL, c.MediaType, c.Status,
		c.ScheduledAt, c.StartedAt, c.CompletedAt, c.TotalRecipients, c.SentCount,
		c.FailedCount, c.Settings, c.DeviceID, c.StartedBy, c.UpdatedAt, c.ID)
	return err
}

func (r *CampaignRepository) Delete(ctx context.Context, id uuid.UUID) error {
	r.db.Exec(ctx, `DELETE FROM campaign_recipients WHERE campaign_id = $1`, id)
	_, err := r.db.Exec(ctx, `DELETE FROM campaigns WHERE id = $1`, id)
	return err
}

func (r *CampaignRepository) AddRecipients(ctx context.Context, recipients []*domain.CampaignRecipient) error {
	if len(recipients) == 0 {
		return nil
	}
	for _, rec := range recipients {
		rec.ID = uuid.New()
		if rec.Status == "" {
			rec.Status = "pending"
		}
		metaJSON := []byte("{}")
		if rec.Metadata != nil {
			if b, err := json.Marshal(rec.Metadata); err == nil {
				metaJSON = b
			}
		}
		_, err := r.db.Exec(ctx, `
			INSERT INTO campaign_recipients (id, campaign_id, contact_id, jid, name, phone, status, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT DO NOTHING
		`, rec.ID, rec.CampaignID, rec.ContactID, rec.JID, rec.Name, rec.Phone, rec.Status, metaJSON)
		if err != nil {
			return err
		}
	}
	// Update total count
	_, err := r.db.Exec(ctx, `
		UPDATE campaigns SET total_recipients = (SELECT count(*) FROM campaign_recipients WHERE campaign_id = $1), updated_at = NOW()
		WHERE id = $1
	`, recipients[0].CampaignID)
	return err
}

func (r *CampaignRepository) GetRecipients(ctx context.Context, campaignID uuid.UUID) ([]*domain.CampaignRecipient, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, campaign_id, contact_id, jid, name, phone, status, sent_at, error_message, wait_time_ms, COALESCE(metadata, '{}')
		FROM campaign_recipients WHERE campaign_id = $1 ORDER BY sent_at ASC NULLS LAST, id
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipients []*domain.CampaignRecipient
	for rows.Next() {
		rec := &domain.CampaignRecipient{}
		var metaJSON []byte
		if err := rows.Scan(&rec.ID, &rec.CampaignID, &rec.ContactID, &rec.JID, &rec.Name, &rec.Phone, &rec.Status, &rec.SentAt, &rec.ErrorMessage, &rec.WaitTimeMs, &metaJSON); err != nil {
			return nil, err
		}
		if len(metaJSON) > 2 {
			json.Unmarshal(metaJSON, &rec.Metadata)
		}
		recipients = append(recipients, rec)
	}
	return recipients, nil
}

func (r *CampaignRepository) GetRecipientByID(ctx context.Context, recipientID uuid.UUID) (*domain.CampaignRecipient, error) {
	rec := &domain.CampaignRecipient{}
	var metaJSON []byte
	err := r.db.QueryRow(ctx, `
		SELECT id, campaign_id, contact_id, jid, name, phone, status, sent_at, error_message, wait_time_ms, COALESCE(metadata, '{}')
		FROM campaign_recipients WHERE id = $1
	`, recipientID).Scan(&rec.ID, &rec.CampaignID, &rec.ContactID, &rec.JID, &rec.Name, &rec.Phone, &rec.Status, &rec.SentAt, &rec.ErrorMessage, &rec.WaitTimeMs, &metaJSON)
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 2 {
		json.Unmarshal(metaJSON, &rec.Metadata)
	}
	return rec, nil
}

func (r *CampaignRepository) GetNextPendingRecipient(ctx context.Context, campaignID uuid.UUID) (*domain.CampaignRecipient, error) {
	rec := &domain.CampaignRecipient{}
	var metaJSON []byte
	err := r.db.QueryRow(ctx, `
		SELECT id, campaign_id, contact_id, jid, name, phone, status, sent_at, error_message, wait_time_ms, COALESCE(metadata, '{}')
		FROM campaign_recipients WHERE campaign_id = $1 AND status = 'pending'
		ORDER BY id LIMIT 1
	`, campaignID).Scan(&rec.ID, &rec.CampaignID, &rec.ContactID, &rec.JID, &rec.Name, &rec.Phone, &rec.Status, &rec.SentAt, &rec.ErrorMessage, &rec.WaitTimeMs, &metaJSON)
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 2 {
		json.Unmarshal(metaJSON, &rec.Metadata)
	}
	return rec, nil
}

func (r *CampaignRepository) UpdateRecipientStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string, waitTimeMs *int) error {
	if status == "sent" {
		now := time.Now()
		_, err := r.db.Exec(ctx, `
			UPDATE campaign_recipients SET status = $1, sent_at = $2, error_message = $3, wait_time_ms = $4 WHERE id = $5
		`, status, now, errMsg, waitTimeMs, id)
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE campaign_recipients SET status = $1, error_message = $2, wait_time_ms = $3 WHERE id = $4
	`, status, errMsg, waitTimeMs, id)
	return err
}

func (r *CampaignRepository) IncrementSentCount(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE campaigns SET sent_count = sent_count + 1, updated_at = NOW() WHERE id = $1`, campaignID)
	return err
}

func (r *CampaignRepository) IncrementFailedCount(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE campaigns SET failed_count = failed_count + 1, updated_at = NOW() WHERE id = $1`, campaignID)
	return err
}

func (r *CampaignRepository) DecrementFailedCount(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE campaigns SET failed_count = GREATEST(failed_count - 1, 0), updated_at = NOW() WHERE id = $1`, campaignID)
	return err
}

func (r *CampaignRepository) DeleteRecipient(ctx context.Context, campaignID, recipientID uuid.UUID) error {
	result, err := r.db.Exec(ctx, `
		DELETE FROM campaign_recipients WHERE id = $1 AND campaign_id = $2 AND status = 'pending'
	`, recipientID, campaignID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("recipient not found or already processed")
	}
	_, err = r.db.Exec(ctx, `
		UPDATE campaigns SET total_recipients = (SELECT count(*) FROM campaign_recipients WHERE campaign_id = $1), updated_at = NOW()
		WHERE id = $1
	`, campaignID)
	return err
}

func (r *CampaignRepository) UpdateRecipientData(ctx context.Context, campaignID, recipientID uuid.UUID, name *string, phone *string, metadata map[string]interface{}) (*domain.CampaignRecipient, error) {
	metaJSON, _ := json.Marshal(metadata)
	_, err := r.db.Exec(ctx, `
		UPDATE campaign_recipients SET name = $1, phone = $2, metadata = $3
		WHERE id = $4 AND campaign_id = $5 AND status = 'pending'
	`, name, phone, metaJSON, recipientID, campaignID)
	if err != nil {
		return nil, err
	}
	rec := &domain.CampaignRecipient{}
	err = r.db.QueryRow(ctx, `
		SELECT id, campaign_id, contact_id, jid, name, phone, status, sent_at, error_message, wait_time_ms, metadata
		FROM campaign_recipients WHERE id = $1
	`, recipientID).Scan(
		&rec.ID, &rec.CampaignID, &rec.ContactID, &rec.JID, &rec.Name, &rec.Phone,
		&rec.Status, &rec.SentAt, &rec.ErrorMessage, &rec.WaitTimeMs, &rec.Metadata,
	)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (r *CampaignRepository) GetRunningCampaigns(ctx context.Context) ([]*domain.Campaign, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.account_id, c.device_id, c.name, c.message_template, c.media_url, c.media_type,
			c.status, c.scheduled_at, c.started_at, c.completed_at, c.total_recipients, c.sent_count, c.failed_count,
			c.settings, c.created_at, c.updated_at
		FROM campaigns c
		WHERE c.status IN ('running', 'scheduled')
		ORDER BY c.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []*domain.Campaign
	for rows.Next() {
		camp := &domain.Campaign{}
		var deviceName *string
		if err := rows.Scan(
			&camp.ID, &camp.AccountID, &camp.DeviceID, &camp.Name, &camp.MessageTemplate,
			&camp.MediaURL, &camp.MediaType, &camp.Status, &camp.ScheduledAt, &camp.StartedAt,
			&camp.CompletedAt, &camp.TotalRecipients, &camp.SentCount, &camp.FailedCount,
			&camp.Settings, &camp.CreatedAt, &camp.UpdatedAt,
		); err != nil {
			return nil, err
		}
		camp.DeviceName = deviceName
		campaigns = append(campaigns, camp)
	}
	return campaigns, nil
}

// ============================================================
// EventRepository handles event data access
// ============================================================

type EventRepository struct {
	db *pgxpool.Pool
}

func (r *EventRepository) Create(ctx context.Context, e *domain.Event) error {
	e.ID = uuid.New()
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now
	if e.Status == "" {
		e.Status = domain.EventStatusActive
	}
	if e.Color == "" {
		e.Color = "#3b82f6"
	}
	if e.TagFormulaMode == "" {
		e.TagFormulaMode = "OR"
	}
	if e.TagFormulaType == "" {
		e.TagFormulaType = "simple"
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO events (id, account_id, folder_id, pipeline_id, name, description, event_date, event_end, location, status, color, tag_formula_mode, tag_formula, tag_formula_type, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	`, e.ID, e.AccountID, e.FolderID, e.PipelineID, e.Name, e.Description, e.EventDate, e.EventEnd, e.Location, e.Status, e.Color, e.TagFormulaMode, e.TagFormula, e.TagFormulaType, e.CreatedBy, e.CreatedAt, e.UpdatedAt)
	return err
}

func (r *EventRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID, filter domain.EventFilter) ([]*domain.Event, int, error) {
	baseQuery := ` FROM events WHERE account_id = $1`
	args := []interface{}{accountID}
	argNum := 2

	if filter.Status != "" {
		baseQuery += fmt.Sprintf(" AND status = $%d", argNum)
		args = append(args, filter.Status)
		argNum++
	}
	if filter.Search != "" {
		baseQuery += fmt.Sprintf(" AND (name ILIKE $%d OR description ILIKE $%d OR location ILIKE $%d)", argNum, argNum, argNum)
		args = append(args, "%"+filter.Search+"%")
		argNum++
	}
	if filter.DateFrom != nil {
		baseQuery += fmt.Sprintf(" AND event_date >= $%d", argNum)
		args = append(args, *filter.DateFrom)
		argNum++
	}
	if filter.DateTo != nil {
		baseQuery += fmt.Sprintf(" AND event_date <= $%d", argNum)
		args = append(args, *filter.DateTo)
		argNum++
	}
	switch filter.FolderFilter {
	case "root":
		baseQuery += " AND folder_id IS NULL"
	case "":
		// no filter – return all
	default:
		// assume it's a UUID
		baseQuery += fmt.Sprintf(" AND folder_id = $%d", argNum)
		args = append(args, filter.FolderFilter)
		argNum++
	}
	_ = argNum // suppress unused warning

	var total int
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) "+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectQuery := `SELECT id, account_id, folder_id, pipeline_id, name, description, event_date, event_end, location, status, color, tag_formula_mode, tag_formula, tag_formula_type, created_by, created_at, updated_at` + baseQuery + ` ORDER BY COALESCE(event_date, created_at) DESC`
	if filter.Limit > 0 {
		selectQuery += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			selectQuery += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := r.db.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []*domain.Event
	for rows.Next() {
		ev := &domain.Event{}
		if err := rows.Scan(&ev.ID, &ev.AccountID, &ev.FolderID, &ev.PipelineID, &ev.Name, &ev.Description, &ev.EventDate, &ev.EventEnd, &ev.Location, &ev.Status, &ev.Color, &ev.TagFormulaMode, &ev.TagFormula, &ev.TagFormulaType, &ev.CreatedBy, &ev.CreatedAt, &ev.UpdatedAt); err != nil {
			return nil, 0, err
		}
		events = append(events, ev)
	}

	// Load participant counts for each event
	for _, ev := range events {
		counts, total, _ := r.GetParticipantCounts(ctx, ev.ID)
		ev.ParticipantCounts = counts
		ev.TotalParticipants = total
		ev.StageCounts, _ = r.GetStageNameCounts(ctx, ev.ID)
	}

	return events, total, nil
}

func (r *EventRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	ev := &domain.Event{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, folder_id, pipeline_id, name, description, event_date, event_end, location, status, color, tag_formula_mode, tag_formula, tag_formula_type, created_by, created_at, updated_at
		FROM events WHERE id = $1
	`, id).Scan(&ev.ID, &ev.AccountID, &ev.FolderID, &ev.PipelineID, &ev.Name, &ev.Description, &ev.EventDate, &ev.EventEnd, &ev.Location, &ev.Status, &ev.Color, &ev.TagFormulaMode, &ev.TagFormula, &ev.TagFormulaType, &ev.CreatedBy, &ev.CreatedAt, &ev.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	counts, total, _ := r.GetParticipantCounts(ctx, ev.ID)
	ev.ParticipantCounts = counts
	ev.TotalParticipants = total
	ev.StageCounts, _ = r.GetStageNameCounts(ctx, ev.ID)
	return ev, nil
}

func (r *EventRepository) Update(ctx context.Context, e *domain.Event) error {
	e.UpdatedAt = time.Now()
	if e.TagFormulaMode == "" {
		e.TagFormulaMode = "OR"
	}
	if e.TagFormulaType == "" {
		e.TagFormulaType = "simple"
	}
	_, err := r.db.Exec(ctx, `
		UPDATE events SET name=$1, description=$2, event_date=$3, event_end=$4, location=$5, status=$6, color=$7, pipeline_id=$8, tag_formula_mode=$9, tag_formula=$10, tag_formula_type=$11, updated_at=$12
		WHERE id=$13
	`, e.Name, e.Description, e.EventDate, e.EventEnd, e.Location, e.Status, e.Color, e.PipelineID, e.TagFormulaMode, e.TagFormula, e.TagFormulaType, e.UpdatedAt, e.ID)
	return err
}

func (r *EventRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// Cascade deletes participants and interactions via FK
	_, err := r.db.Exec(ctx, `DELETE FROM interactions WHERE event_id = $1`, id)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `DELETE FROM event_participants WHERE event_id = $1`, id)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `DELETE FROM events WHERE id = $1`, id)
	return err
}

func (r *EventRepository) GetParticipantCounts(ctx context.Context, eventID uuid.UUID) (map[string]int, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT status, COUNT(*) FROM event_participants WHERE event_id = $1 GROUP BY status
	`, eventID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	total := 0
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, 0, err
		}
		counts[status] = count
		total += count
	}
	return counts, total, nil
}

func (r *EventRepository) GetStageNameCounts(ctx context.Context, eventID uuid.UUID) (map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT eps.name, COUNT(*) FROM event_participants ep
		JOIN event_pipeline_stages eps ON eps.id = ep.stage_id
		WHERE ep.event_id = $1 AND ep.stage_id IS NOT NULL
		GROUP BY eps.name
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		counts[name] = count
	}
	return counts, nil
}

func (r *EventRepository) GetByContactID(ctx context.Context, accountID, contactID uuid.UUID) ([]*domain.Event, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT e.id, e.account_id, e.folder_id, e.pipeline_id, e.name, e.description, e.event_date, e.event_end, e.location, e.status, e.color, e.tag_formula_mode, e.tag_formula, e.tag_formula_type, e.created_by, e.created_at, e.updated_at
		FROM events e
		JOIN event_participants ep ON ep.event_id = e.id
		WHERE e.account_id = $1 AND ep.contact_id = $2
		ORDER BY COALESCE(e.event_date, e.created_at) DESC
	`, accountID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*domain.Event
	for rows.Next() {
		ev := &domain.Event{}
		if err := rows.Scan(&ev.ID, &ev.AccountID, &ev.FolderID, &ev.PipelineID, &ev.Name, &ev.Description, &ev.EventDate, &ev.EventEnd, &ev.Location, &ev.Status, &ev.Color, &ev.TagFormulaMode, &ev.TagFormula, &ev.TagFormulaType, &ev.CreatedBy, &ev.CreatedAt, &ev.UpdatedAt); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

// ============================================================
// EventTagSync — event_tags CRUD + reconciliation queries
// ============================================================

// SetEventTags replaces all tags for an event (transactional delete-all + insert).
// Each entry has a UUID and a negate flag (TRUE = exclude).
func (r *EventRepository) SetEventTags(ctx context.Context, eventID uuid.UUID, includes []uuid.UUID, excludes []uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM event_tags WHERE event_id = $1`, eventID); err != nil {
		return err
	}
	for _, tid := range includes {
		if _, err := tx.Exec(ctx, `INSERT INTO event_tags (event_id, tag_id, negate) VALUES ($1, $2, FALSE) ON CONFLICT DO NOTHING`, eventID, tid); err != nil {
			return err
		}
	}
	for _, tid := range excludes {
		if _, err := tx.Exec(ctx, `INSERT INTO event_tags (event_id, tag_id, negate) VALUES ($1, $2, TRUE) ON CONFLICT DO NOTHING`, eventID, tid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// EventTagEntry represents a tag configured on an event with its negate flag.
type EventTagEntry struct {
	Tag    *domain.Tag
	Negate bool
}

// GetEventTags returns the tags configured on an event with their negate flags.
func (r *EventRepository) GetEventTags(ctx context.Context, eventID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.id, t.account_id, t.name, t.color, t.kommo_id, t.created_at, t.updated_at, et.negate
		FROM tags t JOIN event_tags et ON et.tag_id = t.id
		WHERE et.event_id = $1
		ORDER BY et.negate ASC, t.name
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []*domain.Tag
	for rows.Next() {
		t := &domain.Tag{}
		var negate bool
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Name, &t.Color, &t.KommoID, &t.CreatedAt, &t.UpdatedAt, &negate); err != nil {
			return nil, err
		}
		if negate {
			t.Negate = true
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// GetEventTagsBatch loads tags for multiple events in a single query
func (r *EventRepository) GetEventTagsBatch(ctx context.Context, eventIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	result := make(map[uuid.UUID][]*domain.Tag)
	if len(eventIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT et.event_id, t.id, t.account_id, t.name, t.color, t.kommo_id, t.created_at, t.updated_at, et.negate
		FROM tags t JOIN event_tags et ON et.tag_id = t.id
		WHERE et.event_id = ANY($1)
		ORDER BY et.event_id, et.negate ASC, t.name
	`, eventIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var eid uuid.UUID
		t := &domain.Tag{}
		var negate bool
		if err := rows.Scan(&eid, &t.ID, &t.AccountID, &t.Name, &t.Color, &t.KommoID, &t.CreatedAt, &t.UpdatedAt, &negate); err != nil {
			return nil, err
		}
		if negate {
			t.Negate = true
		}
		result[eid] = append(result[eid], t)
	}
	return result, nil
}

// GetEventTagEntries returns include/exclude tag ID lists for an event.
func (r *EventRepository) GetEventTagEntries(ctx context.Context, eventID uuid.UUID) (includes []uuid.UUID, excludes []uuid.UUID, err error) {
	rows, err := r.db.Query(ctx, `SELECT tag_id, negate FROM event_tags WHERE event_id = $1`, eventID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid uuid.UUID
		var neg bool
		if err := rows.Scan(&tid, &neg); err != nil {
			return nil, nil, err
		}
		if neg {
			excludes = append(excludes, tid)
		} else {
			includes = append(includes, tid)
		}
	}
	return includes, excludes, nil
}

// FindActiveEventsByTagID returns active events that have a specific tag configured (include or exclude).
func (r *EventRepository) FindActiveEventsByTagID(ctx context.Context, tagID uuid.UUID) ([]*domain.Event, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.id, e.account_id, e.pipeline_id, e.name, e.status, e.tag_formula_mode, e.tag_formula, e.tag_formula_type
		FROM events e
		JOIN event_tags et ON et.event_id = e.id
		WHERE et.tag_id = $1 AND e.status = 'active'
	`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*domain.Event
	for rows.Next() {
		ev := &domain.Event{}
		if err := rows.Scan(&ev.ID, &ev.AccountID, &ev.PipelineID, &ev.Name, &ev.Status, &ev.TagFormulaMode, &ev.TagFormula, &ev.TagFormulaType); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

// GetActiveEventsWithTags returns all active events that have tags or an advanced formula configured.
func (r *EventRepository) GetActiveEventsWithTags(ctx context.Context) ([]struct {
	Event    *domain.Event
	Includes []uuid.UUID
	Excludes []uuid.UUID
}, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.id, e.account_id, e.pipeline_id, e.name, e.status, e.tag_formula_mode, e.tag_formula, e.tag_formula_type,
		       array_agg(et.tag_id) FILTER (WHERE et.negate = FALSE) AS include_ids,
		       array_agg(et.tag_id) FILTER (WHERE et.negate = TRUE) AS exclude_ids
		FROM events e
		LEFT JOIN event_tags et ON et.event_id = e.id
		WHERE e.status = 'active'
		  AND (et.event_id IS NOT NULL OR (e.tag_formula_type = 'advanced' AND e.tag_formula != ''))
		GROUP BY e.id, e.account_id, e.pipeline_id, e.name, e.status, e.tag_formula_mode, e.tag_formula, e.tag_formula_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type eventWithTags = struct {
		Event    *domain.Event
		Includes []uuid.UUID
		Excludes []uuid.UUID
	}
	var results []eventWithTags
	for rows.Next() {
		ev := &domain.Event{}
		var includes, excludes []uuid.UUID
		if err := rows.Scan(&ev.ID, &ev.AccountID, &ev.PipelineID, &ev.Name, &ev.Status, &ev.TagFormulaMode, &ev.TagFormula, &ev.TagFormulaType, &includes, &excludes); err != nil {
			return nil, err
		}
		results = append(results, eventWithTags{Event: ev, Includes: includes, Excludes: excludes})
	}
	return results, nil
}

// GetActiveAdvancedFormulaEvents returns all active events using advanced tag formulas.
// Used by the real-time hooks to check if a lead tag change affects any advanced-formula event.
func (r *EventRepository) GetActiveAdvancedFormulaEvents(ctx context.Context, accountID uuid.UUID) ([]*domain.Event, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, pipeline_id, name, status, tag_formula_mode, tag_formula, tag_formula_type
		FROM events
		WHERE account_id = $1 AND status = 'active' AND tag_formula_type = 'advanced' AND tag_formula != ''
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*domain.Event
	for rows.Next() {
		ev := &domain.Event{}
		if err := rows.Scan(&ev.ID, &ev.AccountID, &ev.PipelineID, &ev.Name, &ev.Status, &ev.TagFormulaMode, &ev.TagFormula, &ev.TagFormulaType); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

// GetLeadIDsByFormulaText executes a formula AST SQL query and returns matching lead IDs.
func (r *EventRepository) GetLeadIDsByFormulaText(ctx context.Context, sql string, args []interface{}) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetLeadTagNames returns the lowercase tag names for a lead (via its linked contact).
func (r *EventRepository) GetLeadTagNames(ctx context.Context, leadID uuid.UUID) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT LOWER(t.name) FROM contact_tags ct JOIN tags t ON t.id = ct.tag_id JOIN leads l ON l.contact_id = ct.contact_id WHERE l.id = $1
	`, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// GetLeadIDsByTagFormula returns lead IDs matching a formula: (includes with mode AND/OR) minus (excludes).
func (r *EventRepository) GetLeadIDsByTagFormula(ctx context.Context, accountID uuid.UUID, mode string, includes []uuid.UUID, excludes []uuid.UUID) ([]uuid.UUID, error) {
	if len(includes) == 0 {
		return nil, nil
	}

	var query string
	args := []interface{}{accountID, includes}

	if mode == "AND" {
		// Leads must have ALL include tags
		query = `
			SELECT l.id AS lead_id
			FROM leads l
			JOIN contact_tags ct ON ct.contact_id = l.contact_id
			WHERE l.account_id = $1 AND l.is_archived = false AND l.is_blocked = false AND ct.tag_id = ANY($2)
			GROUP BY l.id
			HAVING COUNT(DISTINCT ct.tag_id) = $3
		`
		args = append(args, len(includes))
	} else {
		// OR mode: leads with ANY include tag
		query = `
			SELECT DISTINCT l.id AS lead_id
			FROM leads l
			JOIN contact_tags ct ON ct.contact_id = l.contact_id
			WHERE l.account_id = $1 AND l.is_archived = false AND l.is_blocked = false AND ct.tag_id = ANY($2)
		`
	}

	// Subtract excludes
	if len(excludes) > 0 {
		excArgNum := len(args) + 1
		query = `SELECT lead_id FROM (` + query + `) inc WHERE lead_id NOT IN (
			SELECT l2.id FROM leads l2 JOIN contact_tags ct2 ON ct2.contact_id = l2.contact_id WHERE ct2.tag_id = ANY($` + fmt.Sprintf("%d", excArgNum) + `)
		)`
		args = append(args, excludes)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetLeadIDsWithAnyTag returns lead IDs in an account that have at least one of the given tags.
// Kept for backward compatibility with HandleLeadTagAssigned/Removed.
func (r *EventRepository) GetLeadIDsWithAnyTag(ctx context.Context, accountID uuid.UUID, tagIDs []uuid.UUID) ([]uuid.UUID, error) {
	return r.GetLeadIDsByTagFormula(ctx, accountID, "OR", tagIDs, nil)
}

// GetAutoSyncParticipantLeadIDs returns lead_ids of participants created by auto_tag_sync.
func (r *EventRepository) GetAutoSyncParticipantLeadIDs(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT lead_id FROM event_participants
		WHERE event_id = $1 AND auto_tag_sync = TRUE AND lead_id IS NOT NULL
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// BulkAddParticipantsFromLeads creates participants from lead data in batch.
// Returns the count of actually inserted participants.
func (r *EventRepository) BulkAddParticipantsFromLeads(ctx context.Context, eventID uuid.UUID, stageID *uuid.UUID, leadIDs []uuid.UUID) (int, error) {
	if len(leadIDs) == 0 {
		return 0, nil
	}
	tag, err := r.db.Exec(ctx, `
		INSERT INTO event_participants (id, event_id, lead_id, contact_id, stage_id, name, last_name, short_name, phone, email, age, status, auto_tag_sync, invited_at, created_at, updated_at)
		SELECT gen_random_uuid(), $1, l.id, l.contact_id, $3,
		       COALESCE(c.custom_name, c.name, l.name, ''), COALESCE(c.last_name, l.last_name, ''), COALESCE(c.short_name, l.short_name, ''),
		       COALESCE(c.phone, l.phone, ''), COALESCE(c.email, l.email, ''), COALESCE(c.age, l.age, 0),
		       'invited', TRUE, NOW(), NOW(), NOW()
		FROM leads l
		LEFT JOIN contacts c ON c.id = l.contact_id
		WHERE l.id = ANY($2)
		AND NOT EXISTS (
			SELECT 1 FROM event_participants ep
			WHERE ep.event_id = $1 AND ep.lead_id = l.id
		)
		ON CONFLICT DO NOTHING
	`, eventID, leadIDs, stageID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RemoveAutoSyncParticipantsByLeadID removes auto_tag_sync participants for a single lead across ALL events.
func (r *EventRepository) RemoveAutoSyncParticipantsByLeadID(ctx context.Context, leadID uuid.UUID) (int, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM event_participants
		WHERE lead_id = $1 AND auto_tag_sync = TRUE
	`, leadID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RemoveAutoSyncParticipantsByLeadIDs removes auto_tag_sync participants by lead IDs.
func (r *EventRepository) RemoveAutoSyncParticipantsByLeadIDs(ctx context.Context, eventID uuid.UUID, leadIDs []uuid.UUID) (int, error) {
	if len(leadIDs) == 0 {
		return 0, nil
	}
	tag, err := r.db.Exec(ctx, `
		DELETE FROM event_participants
		WHERE event_id = $1 AND lead_id = ANY($2) AND auto_tag_sync = TRUE
	`, eventID, leadIDs)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// LeadHasAnyTag checks if a lead still has at least one of the given tags (via contact_tags).
func (r *EventRepository) LeadHasAnyTag(ctx context.Context, leadID uuid.UUID, tagIDs []uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM contact_tags ct JOIN leads l ON l.contact_id = ct.contact_id WHERE l.id = $1 AND ct.tag_id = ANY($2))
	`, leadID, tagIDs).Scan(&exists)
	return exists, err
}

// LeadMatchesFormula checks if a lead matches a formula (mode AND/OR, includes, excludes).
func (r *EventRepository) LeadMatchesFormula(ctx context.Context, leadID uuid.UUID, mode string, includes []uuid.UUID, excludes []uuid.UUID) (bool, error) {
	// Check excludes first — if lead has any exclude tag, it doesn't match
	if len(excludes) > 0 {
		var hasExclude bool
		err := r.db.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM contact_tags ct JOIN leads l ON l.contact_id = ct.contact_id WHERE l.id = $1 AND ct.tag_id = ANY($2))
		`, leadID, excludes).Scan(&hasExclude)
		if err != nil {
			return false, err
		}
		if hasExclude {
			return false, nil
		}
	}

	if len(includes) == 0 {
		return true, nil
	}

	if mode == "AND" {
		// Lead must have ALL include tags
		var cnt int
		err := r.db.QueryRow(ctx, `
			SELECT COUNT(DISTINCT ct.tag_id) FROM contact_tags ct JOIN leads l ON l.contact_id = ct.contact_id WHERE l.id = $1 AND ct.tag_id = ANY($2)
		`, leadID, includes).Scan(&cnt)
		if err != nil {
			return false, err
		}
		return cnt == len(includes), nil
	}
	// OR mode: lead has at least one include tag
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM contact_tags ct JOIN leads l ON l.contact_id = ct.contact_id WHERE l.id = $1 AND ct.tag_id = ANY($2))
	`, leadID, includes).Scan(&exists)
	return exists, err
}

// ParticipantExistsForLead checks if a lead already has a participant row in a given event.
func (r *EventRepository) ParticipantExistsForLead(ctx context.Context, eventID, leadID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM event_participants WHERE event_id = $1 AND lead_id = $2)
	`, eventID, leadID).Scan(&exists)
	return exists, err
}

// ============================================================
// EventFolderRepository handles event folder data access
// ============================================================

type EventFolderRepository struct {
	db *pgxpool.Pool
}

func (r *EventFolderRepository) Create(ctx context.Context, f *domain.EventFolder) error {
	f.ID = uuid.New()
	now := time.Now()
	f.CreatedAt = now
	f.UpdatedAt = now
	if f.Color == "" {
		f.Color = "#3b82f6"
	}
	if f.Icon == "" {
		f.Icon = "📁"
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO event_folders (id, account_id, parent_id, name, color, icon, position, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, f.ID, f.AccountID, f.ParentID, f.Name, f.Color, f.Icon, f.Position, f.CreatedAt, f.UpdatedAt)
	return err
}

func (r *EventFolderRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.EventFolder, error) {
	rows, err := r.db.Query(ctx, `
		SELECT ef.id, ef.account_id, ef.parent_id, ef.name, ef.color, ef.icon, ef.position, ef.created_at, ef.updated_at,
		       COUNT(e.id) AS event_count
		FROM event_folders ef
		LEFT JOIN events e ON e.folder_id = ef.id
		WHERE ef.account_id = $1
		GROUP BY ef.id
		ORDER BY ef.position, ef.name
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []*domain.EventFolder
	for rows.Next() {
		f := &domain.EventFolder{}
		if err := rows.Scan(&f.ID, &f.AccountID, &f.ParentID, &f.Name, &f.Color, &f.Icon, &f.Position, &f.CreatedAt, &f.UpdatedAt, &f.EventCount); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, nil
}

func (r *EventFolderRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.EventFolder, error) {
	f := &domain.EventFolder{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, parent_id, name, color, icon, position, created_at, updated_at
		FROM event_folders WHERE id = $1
	`, id).Scan(&f.ID, &f.AccountID, &f.ParentID, &f.Name, &f.Color, &f.Icon, &f.Position, &f.CreatedAt, &f.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (r *EventFolderRepository) Update(ctx context.Context, f *domain.EventFolder) error {
	f.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE event_folders SET name=$1, color=$2, icon=$3, position=$4, updated_at=$5 WHERE id=$6
	`, f.Name, f.Color, f.Icon, f.Position, f.UpdatedAt, f.ID)
	return err
}

func (r *EventFolderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// Determine parent so we can re-home children and events
	var parentID *uuid.UUID
	_ = r.db.QueryRow(ctx, `SELECT parent_id FROM event_folders WHERE id = $1`, id).Scan(&parentID)
	// Move events in this folder to parent (or root)
	_, _ = r.db.Exec(ctx, `UPDATE events SET folder_id = $1 WHERE folder_id = $2`, parentID, id)
	// Move sub-folders to parent
	_, _ = r.db.Exec(ctx, `UPDATE event_folders SET parent_id = $1 WHERE parent_id = $2`, parentID, id)
	_, err := r.db.Exec(ctx, `DELETE FROM event_folders WHERE id = $1`, id)
	return err
}

func (r *EventFolderRepository) MoveEvent(ctx context.Context, eventID uuid.UUID, folderID *uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE events SET folder_id = $1, updated_at = NOW() WHERE id = $2`, folderID, eventID)
	return err
}

// ============================================================
// EventPipelineRepository handles event pipeline data access
// ============================================================

type EventPipelineRepository struct {
	db *pgxpool.Pool
}

var defaultEventPipelineStages = []struct {
	name  string
	color string
}{
	{"Registrados", "#3b82f6"},
	{"Confirmados", "#10b981"},
	{"Asistentes", "#059669"},
	{"Interesados", "#8b5cf6"},
	{"Contactados", "#eab308"},
	{"Declinados", "#ef4444"},
	{"Pre inscritos", "#f59e0b"},
	{"Inscrito", "#6366f1"},
}

func (r *EventPipelineRepository) Create(ctx context.Context, p *domain.EventPipeline) error {
	p.ID = uuid.New()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	_, err := r.db.Exec(ctx, `
		INSERT INTO event_pipelines (id, account_id, name, description, is_default, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, p.ID, p.AccountID, p.Name, p.Description, p.IsDefault, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *EventPipelineRepository) EnsureDefaultByAccountID(ctx context.Context, accountID uuid.UUID) (*domain.EventPipeline, error) {
	pipeline, err := r.GetDefaultByAccountID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if pipeline == nil {
		desc := "Pipeline por defecto para eventos"
		pipeline = &domain.EventPipeline{
			AccountID:   accountID,
			Name:        "Pipeline por Defecto",
			Description: &desc,
			IsDefault:   true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := r.Create(ctx, pipeline); err != nil {
			return nil, err
		}
	}

	_, _ = r.db.Exec(ctx, `
		UPDATE event_pipeline_stages
		SET name = 'Pre inscritos'
		WHERE pipeline_id = $1 AND name = 'Pre inscrito'
	`, pipeline.ID)

	var maxPos int
	_ = r.db.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM event_pipeline_stages WHERE pipeline_id = $1`, pipeline.ID).Scan(&maxPos)
	for _, stage := range defaultEventPipelineStages {
		var exists bool
		err := r.db.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM event_pipeline_stages
				WHERE pipeline_id = $1 AND LOWER(name) = LOWER($2)
			)
		`, pipeline.ID, stage.name).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		maxPos++
		_, err = r.db.Exec(ctx, `
			INSERT INTO event_pipeline_stages (pipeline_id, name, color, position)
			VALUES ($1, $2, $3, $4)
		`, pipeline.ID, stage.name, stage.color, maxPos)
		if err != nil {
			return nil, err
		}
	}
	return r.GetByID(ctx, pipeline.ID)
}

func (r *EventPipelineRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.EventPipeline, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, name, description, is_default, created_at, updated_at
		FROM event_pipelines WHERE account_id = $1
		ORDER BY is_default DESC, name
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipelines []*domain.EventPipeline
	for rows.Next() {
		p := &domain.EventPipeline{}
		if err := rows.Scan(&p.ID, &p.AccountID, &p.Name, &p.Description, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		// Load stages
		stageRows, err := r.db.Query(ctx, `
			SELECT id, pipeline_id, name, color, position, created_at
			FROM event_pipeline_stages WHERE pipeline_id = $1
			ORDER BY position
		`, p.ID)
		if err == nil {
			for stageRows.Next() {
				s := &domain.EventPipelineStage{}
				if err := stageRows.Scan(&s.ID, &s.PipelineID, &s.Name, &s.Color, &s.Position, &s.CreatedAt); err == nil {
					p.Stages = append(p.Stages, s)
				}
			}
			stageRows.Close()
		}
		if p.Stages == nil {
			p.Stages = make([]*domain.EventPipelineStage, 0)
		}
		pipelines = append(pipelines, p)
	}
	return pipelines, nil
}

func (r *EventPipelineRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.EventPipeline, error) {
	p := &domain.EventPipeline{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, name, description, is_default, created_at, updated_at
		FROM event_pipelines WHERE id = $1
	`, id).Scan(&p.ID, &p.AccountID, &p.Name, &p.Description, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Load stages
	stageRows, err := r.db.Query(ctx, `
		SELECT id, pipeline_id, name, color, position, created_at
		FROM event_pipeline_stages WHERE pipeline_id = $1
		ORDER BY position
	`, p.ID)
	if err == nil {
		for stageRows.Next() {
			s := &domain.EventPipelineStage{}
			if err := stageRows.Scan(&s.ID, &s.PipelineID, &s.Name, &s.Color, &s.Position, &s.CreatedAt); err == nil {
				p.Stages = append(p.Stages, s)
			}
		}
		stageRows.Close()
	}
	if p.Stages == nil {
		p.Stages = make([]*domain.EventPipelineStage, 0)
	}
	return p, nil
}

func (r *EventPipelineRepository) GetDefaultByAccountID(ctx context.Context, accountID uuid.UUID) (*domain.EventPipeline, error) {
	p := &domain.EventPipeline{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, name, description, is_default, created_at, updated_at
		FROM event_pipelines WHERE account_id = $1 AND is_default = TRUE LIMIT 1
	`, accountID).Scan(&p.ID, &p.AccountID, &p.Name, &p.Description, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Load stages
	stageRows, err := r.db.Query(ctx, `
		SELECT id, pipeline_id, name, color, position, created_at
		FROM event_pipeline_stages WHERE pipeline_id = $1
		ORDER BY position
	`, p.ID)
	if err == nil {
		for stageRows.Next() {
			s := &domain.EventPipelineStage{}
			if err := stageRows.Scan(&s.ID, &s.PipelineID, &s.Name, &s.Color, &s.Position, &s.CreatedAt); err == nil {
				p.Stages = append(p.Stages, s)
			}
		}
		stageRows.Close()
	}
	if p.Stages == nil {
		p.Stages = make([]*domain.EventPipelineStage, 0)
	}
	return p, nil
}

func (r *EventPipelineRepository) Update(ctx context.Context, p *domain.EventPipeline) error {
	p.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE event_pipelines SET name=$1, description=$2, updated_at=$3 WHERE id=$4
	`, p.Name, p.Description, p.UpdatedAt, p.ID)
	return err
}

func (r *EventPipelineRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM event_pipelines WHERE id = $1`, id)
	return err
}

// ReplaceStages deletes all stages for a pipeline and inserts new ones
func (r *EventPipelineRepository) ReplaceStages(ctx context.Context, pipelineID uuid.UUID, stages []*domain.EventPipelineStage) error {
	_, err := r.db.Exec(ctx, `DELETE FROM event_pipeline_stages WHERE pipeline_id = $1`, pipelineID)
	if err != nil {
		return err
	}
	for i, s := range stages {
		s.ID = uuid.New()
		s.PipelineID = pipelineID
		s.Position = i
		s.CreatedAt = time.Now()
		_, err := r.db.Exec(ctx, `
			INSERT INTO event_pipeline_stages (id, pipeline_id, name, color, position, created_at)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, s.ID, s.PipelineID, s.Name, s.Color, s.Position, s.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetStagesByPipelineID returns stages for a pipeline
func (r *EventPipelineRepository) GetStagesByPipelineID(ctx context.Context, pipelineID uuid.UUID) ([]*domain.EventPipelineStage, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, pipeline_id, name, color, position, created_at
		FROM event_pipeline_stages WHERE pipeline_id = $1
		ORDER BY position
	`, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stages []*domain.EventPipelineStage
	for rows.Next() {
		s := &domain.EventPipelineStage{}
		if err := rows.Scan(&s.ID, &s.PipelineID, &s.Name, &s.Color, &s.Position, &s.CreatedAt); err != nil {
			return nil, err
		}
		stages = append(stages, s)
	}
	return stages, nil
}

// GetParticipantCountsByStage returns counts per stage_id for an event
func (r *EventPipelineRepository) GetParticipantCountsByStage(ctx context.Context, eventID uuid.UUID) (map[uuid.UUID]int, int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT stage_id, COUNT(*) FROM event_participants WHERE event_id = $1 AND stage_id IS NOT NULL GROUP BY stage_id
	`, eventID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	total := 0
	for rows.Next() {
		var stageID uuid.UUID
		var count int
		if err := rows.Scan(&stageID, &count); err != nil {
			return nil, 0, err
		}
		counts[stageID] = count
		total += count
	}
	var noStageCount int
	_ = r.db.QueryRow(ctx, `SELECT COUNT(*) FROM event_participants WHERE event_id = $1 AND stage_id IS NULL`, eventID).Scan(&noStageCount)
	total += noStageCount
	return counts, total, nil
}

// ============================================================
// ParticipantRepository handles event participant data access
// ============================================================

type ParticipantRepository struct {
	db *pgxpool.Pool
}

func (r *ParticipantRepository) Add(ctx context.Context, p *domain.EventParticipant) error {
	p.ID = uuid.New()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Status == "" {
		p.Status = domain.ParticipantStatusInvited
	}
	p.InvitedAt = &now
	if err := r.db.QueryRow(ctx, `
		INSERT INTO event_participants (id, event_id, contact_id, lead_id, stage_id, name, last_name, short_name, phone, email, age, status, notes, next_action, next_action_date, invited_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id
	`, p.ID, p.EventID, p.ContactID, p.LeadID, p.StageID, p.Name, p.LastName, p.ShortName, p.Phone, p.Email, p.Age, p.Status, p.Notes, p.NextAction, p.NextActionDate, p.InvitedAt, p.CreatedAt, p.UpdatedAt).Scan(&p.ID); err != nil {
		return err
	}
	// Tags are now derived from contact_tags via contact_id
	return nil
}

func (r *ParticipantRepository) BulkAdd(ctx context.Context, eventID uuid.UUID, participants []*domain.EventParticipant) error {
	for _, p := range participants {
		p.EventID = eventID
		if err := r.Add(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (r *ParticipantRepository) GetByEventID(ctx context.Context, eventID uuid.UUID, search, statusFilter string, tagIDs []uuid.UUID, hasPhone *bool) ([]*domain.EventParticipant, error) {
	useDistinct := len(tagIDs) > 0
	selectClause := `SELECT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id, p.name, p.last_name, p.short_name, p.phone, p.email, p.age, p.status, p.notes, p.next_action, p.next_action_date, p.invited_at, p.confirmed_at, p.attended_at, p.created_at, p.updated_at, eps.name AS stage_name, eps.color AS stage_color, l.pipeline_id AS lead_pipeline_id, l.stage_id AS lead_stage_id, ps.name AS lead_stage_name, ps.color AS lead_stage_color, COALESCE(l.is_archived, false) AS is_archived, COALESCE(l.is_blocked, false) AS is_blocked`
	if useDistinct {
		selectClause = `SELECT DISTINCT p.id, p.event_id, p.contact_id, p.lead_id, p.stage_id, p.name, p.last_name, p.short_name, p.phone, p.email, p.age, p.status, p.notes, p.next_action, p.next_action_date, p.invited_at, p.confirmed_at, p.attended_at, p.created_at, p.updated_at, eps.name AS stage_name, eps.color AS stage_color, l.pipeline_id AS lead_pipeline_id, l.stage_id AS lead_stage_id, ps.name AS lead_stage_name, ps.color AS lead_stage_color, COALESCE(l.is_archived, false) AS is_archived, COALESCE(l.is_blocked, false) AS is_blocked`
	}
	query := selectClause + ` FROM event_participants p LEFT JOIN event_pipeline_stages eps ON eps.id = p.stage_id LEFT JOIN leads l ON l.id = p.lead_id LEFT JOIN pipeline_stages ps ON ps.id = l.stage_id`
	args := []interface{}{eventID}
	argNum := 2

	if useDistinct {
		query += ` JOIN contact_tags ct ON ct.contact_id = p.contact_id`
	}
	query += ` WHERE p.event_id = $1`

	if statusFilter != "" {
		// Check if it's a UUID (stage_id filter) or a status string
		if _, err := uuid.Parse(statusFilter); err == nil {
			query += fmt.Sprintf(" AND p.stage_id = $%d", argNum)
		} else {
			query += fmt.Sprintf(" AND p.status = $%d", argNum)
		}
		args = append(args, statusFilter)
		argNum++
	}
	if search != "" {
		query += fmt.Sprintf(" AND (p.name ILIKE $%d OR p.last_name ILIKE $%d OR p.phone ILIKE $%d OR p.email ILIKE $%d)", argNum, argNum, argNum, argNum)
		args = append(args, "%"+search+"%")
		argNum++
	}
	if useDistinct {
		placeholders := ""
		for i, tid := range tagIDs {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", argNum)
			args = append(args, tid)
			argNum++
		}
		query += fmt.Sprintf(" AND ct.tag_id IN (%s)", placeholders)
	}
	if hasPhone != nil && *hasPhone {
		query += " AND p.phone IS NOT NULL AND p.phone != ''"
	}
	query += " ORDER BY p.next_action_date ASC NULLS LAST, p.name ASC"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []*domain.EventParticipant
	for rows.Next() {
		p := &domain.EventParticipant{}
		if err := rows.Scan(&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID, &p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age, &p.Status, &p.Notes, &p.NextAction, &p.NextActionDate, &p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt, &p.CreatedAt, &p.UpdatedAt, &p.StageName, &p.StageColor, &p.LeadPipelineID, &p.LeadStageID, &p.LeadStageName, &p.LeadStageColor, &p.IsArchived, &p.IsBlocked); err != nil {
			return nil, err
		}
		participants = append(participants, p)
	}

	// Load tags for each participant from contact_tags (via contact_id)
	for _, p := range participants {
		if p.ContactID == nil {
			p.Tags = make([]*domain.Tag, 0)
			continue
		}
		tags, err := r.db.Query(ctx, `
			SELECT t.id, t.account_id, t.name, t.color, t.created_at
			FROM tags t
			JOIN contact_tags ct ON ct.tag_id = t.id
			WHERE ct.contact_id = $1
		`, *p.ContactID)
		if err == nil {
			defer tags.Close()
			for tags.Next() {
				tag := &domain.Tag{}
				if err := tags.Scan(&tag.ID, &tag.AccountID, &tag.Name, &tag.Color, &tag.CreatedAt); err == nil {
					p.Tags = append(p.Tags, tag)
				}
			}
		}
		if p.Tags == nil {
			p.Tags = make([]*domain.Tag, 0)
		}
	}

	return participants, nil
}

func (r *ParticipantRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.EventParticipant, error) {
	p := &domain.EventParticipant{}
	err := r.db.QueryRow(ctx, `
		SELECT id, event_id, contact_id, lead_id, stage_id, name, last_name, short_name, phone, email, age, company, dni, birth_date, address, distrito, ocupacion, status, notes, next_action, next_action_date, invited_at, confirmed_at, attended_at, created_at, updated_at
		FROM event_participants WHERE id = $1
	`, id).Scan(&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID, &p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age, &p.Company, &p.DNI, &p.BirthDate, &p.Address, &p.Distrito, &p.Ocupacion, &p.Status, &p.Notes, &p.NextAction, &p.NextActionDate, &p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (r *ParticipantRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	now := time.Now()
	query := `UPDATE event_participants SET status = $1, updated_at = $2`
	args := []interface{}{status, now}
	argNum := 3

	switch status {
	case domain.ParticipantStatusConfirmed:
		query += fmt.Sprintf(", confirmed_at = $%d", argNum)
		args = append(args, now)
		argNum++
	case domain.ParticipantStatusAttended:
		query += fmt.Sprintf(", attended_at = $%d", argNum)
		args = append(args, now)
		argNum++
	}
	query += fmt.Sprintf(" WHERE id = $%d", argNum)
	args = append(args, id)

	_, err := r.db.Exec(ctx, query, args...)
	return err
}

// UpdateStage updates a participant's stage_id (used when dragging in kanban)
func (r *ParticipantRepository) UpdateStage(ctx context.Context, id, stageID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE event_participants SET stage_id = $1, updated_at = NOW() WHERE id = $2`, stageID, id)
	return err
}

// BulkUpdateStage updates stage_id for multiple participants
func (r *ParticipantRepository) BulkUpdateStage(ctx context.Context, ids []uuid.UUID, stageID uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `UPDATE event_participants SET stage_id = $1, updated_at = NOW() WHERE id = ANY($2::uuid[])`, stageID, ids)
	return err
}

func (r *ParticipantRepository) BulkUpdateStatus(ctx context.Context, ids []uuid.UUID, status string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	query := `UPDATE event_participants SET status = $1, updated_at = $2`
	args := []interface{}{status, now}
	argNum := 3

	switch status {
	case domain.ParticipantStatusConfirmed:
		query += fmt.Sprintf(", confirmed_at = $%d", argNum)
		args = append(args, now)
		argNum++
	case domain.ParticipantStatusAttended:
		query += fmt.Sprintf(", attended_at = $%d", argNum)
		args = append(args, now)
		argNum++
	}
	query += fmt.Sprintf(" WHERE id = ANY($%d::uuid[])", argNum)
	args = append(args, ids)

	_, err := r.db.Exec(ctx, query, args...)
	return err
}

func (r *ParticipantRepository) Update(ctx context.Context, p *domain.EventParticipant) error {
	p.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE event_participants SET name=$1, last_name=$2, short_name=$3, phone=$4, email=$5, age=$6, company=$7, dni=$8, birth_date=$9, address=$10, distrito=$11, ocupacion=$12, notes=$13, next_action=$14, next_action_date=$15, updated_at=$16
		WHERE id=$17
	`, p.Name, p.LastName, p.ShortName, p.Phone, p.Email, p.Age, p.Company, p.DNI, p.BirthDate, p.Address, p.Distrito, p.Ocupacion, p.Notes, p.NextAction, p.NextActionDate, p.UpdatedAt, p.ID)
	if err != nil {
		return err
	}
	// Sync personal data back to Contact (source of truth)
	if p.ContactID != nil {
		_, _ = r.db.Exec(ctx, `
			UPDATE contacts SET
				name = COALESCE($1, name), last_name = $2, short_name = $3,
				phone = COALESCE($4, phone), email = $5, age = $6,
				company = $7, dni = $8, birth_date = $9, address = $10, distrito = $11, ocupacion = $12,
				updated_at = NOW()
			WHERE id = $13
		`, p.Name, p.LastName, p.ShortName, p.Phone, p.Email, p.Age, p.Company, p.DNI, p.BirthDate, p.Address, p.Distrito, p.Ocupacion, *p.ContactID)
	}
	return nil
}

// SyncToContact propagates shared participant fields back to the linked contact
func (r *ParticipantRepository) SyncToContact(ctx context.Context, p *domain.EventParticipant) error {
	if p.ContactID == nil {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		UPDATE contacts SET
			custom_name = COALESCE($1, custom_name), name = COALESCE($1, name), last_name = $2, short_name = $3, phone = COALESCE($4, phone), email = $5, age = $6,
			company = $7, dni = $8, birth_date = $9, address = $10, distrito = $11, ocupacion = $12,
			updated_at = NOW()
		WHERE id = $13
	`, p.Name, p.LastName, p.ShortName, p.Phone, p.Email, p.Age, p.Company, p.DNI, p.BirthDate, p.Address, p.Distrito, p.Ocupacion, *p.ContactID)
	return err
}

func (r *ParticipantRepository) LinkContact(ctx context.Context, participantID, contactID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE event_participants SET contact_id = $2 WHERE id = $1`, participantID, contactID)
	return err
}

func (r *ParticipantRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, _ = r.db.Exec(ctx, `DELETE FROM interactions WHERE participant_id = $1`, id)
	_, err := r.db.Exec(ctx, `DELETE FROM event_participants WHERE id = $1`, id)
	return err
}

func (r *ParticipantRepository) GetUpcomingActions(ctx context.Context, accountID uuid.UUID, limit int) ([]*domain.EventParticipant, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.Query(ctx, `
		SELECT ep.id, ep.event_id, ep.contact_id, ep.lead_id, ep.stage_id, ep.name, ep.last_name, ep.short_name, ep.phone, ep.email, ep.age, ep.status, ep.notes, ep.next_action, ep.next_action_date, ep.invited_at, ep.confirmed_at, ep.attended_at, ep.created_at, ep.updated_at
		FROM event_participants ep
		JOIN events e ON e.id = ep.event_id
		WHERE e.account_id = $1 AND ep.next_action_date IS NOT NULL AND ep.status NOT IN ('attended','no_show','declined')
		ORDER BY ep.next_action_date ASC
		LIMIT $2
	`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []*domain.EventParticipant
	for rows.Next() {
		p := &domain.EventParticipant{}
		if err := rows.Scan(&p.ID, &p.EventID, &p.ContactID, &p.LeadID, &p.StageID, &p.Name, &p.LastName, &p.ShortName, &p.Phone, &p.Email, &p.Age, &p.Status, &p.Notes, &p.NextAction, &p.NextActionDate, &p.InvitedAt, &p.ConfirmedAt, &p.AttendedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		participants = append(participants, p)
	}
	return participants, nil
}

// ============================================================
// InteractionRepository handles interaction data access
// ============================================================

type InteractionRepository struct {
	db *pgxpool.Pool
}

func (r *InteractionRepository) Create(ctx context.Context, i *domain.Interaction) error {
	i.ID = uuid.New()
	i.CreatedAt = time.Now()
	return r.db.QueryRow(ctx, `
		INSERT INTO interactions (id, account_id, contact_id, lead_id, event_id, participant_id, type, direction, outcome, notes, next_action, next_action_date, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id
	`, i.ID, i.AccountID, i.ContactID, i.LeadID, i.EventID, i.ParticipantID, i.Type, i.Direction, i.Outcome, i.Notes, i.NextAction, i.NextActionDate, i.CreatedBy, i.CreatedAt).Scan(&i.ID)
}

func (r *InteractionRepository) GetByParticipantID(ctx context.Context, participantID uuid.UUID) ([]*domain.Interaction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT i.id, i.account_id, i.contact_id, i.lead_id, i.event_id, i.participant_id, i.type, i.direction, i.outcome, i.notes, i.next_action, i.next_action_date, i.created_by, i.created_at,
		       u.display_name as created_by_name
		FROM interactions i
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.participant_id = $1
		ORDER BY i.created_at DESC
	`, participantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interactions []*domain.Interaction
	for rows.Next() {
		it := &domain.Interaction{}
		if err := rows.Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID, &it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate, &it.CreatedBy, &it.CreatedAt, &it.CreatedByName); err != nil {
			return nil, err
		}
		interactions = append(interactions, it)
	}
	return interactions, nil
}

func (r *InteractionRepository) GetByContactID(ctx context.Context, contactID uuid.UUID, limit, offset int) ([]*domain.Interaction, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT i.id, i.account_id, i.contact_id, i.lead_id, i.event_id, i.participant_id, i.type, i.direction, i.outcome, i.notes, i.next_action, i.next_action_date, i.created_by, i.created_at,
		       u.display_name as created_by_name, e.name as event_name
		FROM interactions i
		LEFT JOIN users u ON u.id = i.created_by
		LEFT JOIN events e ON e.id = i.event_id
		WHERE i.contact_id = $1
		ORDER BY i.created_at DESC
		LIMIT $2 OFFSET $3
	`, contactID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interactions []*domain.Interaction
	for rows.Next() {
		it := &domain.Interaction{}
		if err := rows.Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID, &it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate, &it.CreatedBy, &it.CreatedAt, &it.CreatedByName, &it.EventName); err != nil {
			return nil, err
		}
		interactions = append(interactions, it)
	}
	return interactions, nil
}

func (r *InteractionRepository) GetByEventID(ctx context.Context, eventID uuid.UUID, limit, offset int) ([]*domain.Interaction, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT i.id, i.account_id, i.contact_id, i.lead_id, i.event_id, i.participant_id, i.type, i.direction, i.outcome, i.notes, i.next_action, i.next_action_date, i.created_by, i.created_at,
		       u.display_name as created_by_name
		FROM interactions i
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.event_id = $1
		ORDER BY i.created_at DESC
		LIMIT $2 OFFSET $3
	`, eventID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interactions []*domain.Interaction
	for rows.Next() {
		it := &domain.Interaction{}
		if err := rows.Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID, &it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate, &it.CreatedBy, &it.CreatedAt, &it.CreatedByName); err != nil {
			return nil, err
		}
		interactions = append(interactions, it)
	}
	return interactions, nil
}

func (r *InteractionRepository) GetLastByParticipantID(ctx context.Context, participantID uuid.UUID) (*domain.Interaction, error) {
	it := &domain.Interaction{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, contact_id, lead_id, event_id, participant_id, type, direction, outcome, notes, next_action, next_action_date, created_by, created_at
		FROM interactions WHERE participant_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, participantID).Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID, &it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate, &it.CreatedBy, &it.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (r *InteractionRepository) GetByLeadID(ctx context.Context, leadID uuid.UUID, limit, offset int) ([]*domain.Interaction, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT i.id, i.account_id, i.contact_id, i.lead_id, i.event_id, i.participant_id, i.type, i.direction, i.outcome, i.notes, i.next_action, i.next_action_date, i.created_by, i.created_at,
		       u.display_name as created_by_name
		FROM interactions i
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.lead_id = $1
		ORDER BY i.created_at DESC
		LIMIT $2 OFFSET $3
	`, leadID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interactions []*domain.Interaction
	for rows.Next() {
		it := &domain.Interaction{}
		if err := rows.Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID, &it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate, &it.CreatedBy, &it.CreatedAt, &it.CreatedByName); err != nil {
			return nil, err
		}
		interactions = append(interactions, it)
	}
	return interactions, nil
}

func (r *InteractionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM interactions WHERE id = $1`, id)
	return err
}

// GetCallsByLeadID returns all call-type interactions for a lead, ordered by created_at ASC.
func (r *InteractionRepository) GetCallsByLeadID(ctx context.Context, leadID uuid.UUID) ([]*domain.Interaction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, contact_id, lead_id, event_id, participant_id, type, direction, outcome, notes,
		       next_action, next_action_date, created_by, created_at, kommo_call_slot
		FROM interactions
		WHERE lead_id = $1 AND type = 'call'
		ORDER BY created_at ASC
	`, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interactions []*domain.Interaction
	for rows.Next() {
		it := &domain.Interaction{}
		if err := rows.Scan(&it.ID, &it.AccountID, &it.ContactID, &it.LeadID, &it.EventID, &it.ParticipantID,
			&it.Type, &it.Direction, &it.Outcome, &it.Notes, &it.NextAction, &it.NextActionDate,
			&it.CreatedBy, &it.CreatedAt, &it.KommoCallSlot); err != nil {
			return nil, err
		}
		interactions = append(interactions, it)
	}
	return interactions, nil
}

// SavedStickerRepository handles saved sticker data access
type SavedStickerRepository struct {
	db *pgxpool.Pool
}

func (r *SavedStickerRepository) GetAll(ctx context.Context, accountID uuid.UUID) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT media_url FROM saved_stickers
		WHERE account_id = $1
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	return urls, nil
}

func (r *SavedStickerRepository) Save(ctx context.Context, accountID uuid.UUID, mediaURL string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO saved_stickers (account_id, media_url)
		VALUES ($1, $2)
		ON CONFLICT (account_id, media_url) DO NOTHING
	`, accountID, mediaURL)
	return err
}

func (r *SavedStickerRepository) Delete(ctx context.Context, accountID uuid.UUID, mediaURL string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM saved_stickers
		WHERE account_id = $1 AND media_url = $2
	`, accountID, mediaURL)
	return err
}

// ReactionRepository handles message reaction data access
type ReactionRepository struct {
	db *pgxpool.Pool
}

func (r *ReactionRepository) Upsert(ctx context.Context, reaction *domain.MessageReaction) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO message_reactions (account_id, chat_id, target_message_id, sender_jid, sender_name, emoji, is_from_me, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (chat_id, target_message_id, sender_jid) DO UPDATE SET
			emoji = EXCLUDED.emoji, sender_name = EXCLUDED.sender_name, timestamp = EXCLUDED.timestamp
		RETURNING id, created_at
	`, reaction.AccountID, reaction.ChatID, reaction.TargetMessageID, reaction.SenderJID, reaction.SenderName, reaction.Emoji, reaction.IsFromMe, reaction.Timestamp,
	).Scan(&reaction.ID, &reaction.CreatedAt)
}

func (r *ReactionRepository) Delete(ctx context.Context, chatID uuid.UUID, targetMessageID, senderJID string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM message_reactions WHERE chat_id = $1 AND target_message_id = $2 AND sender_jid = $3
	`, chatID, targetMessageID, senderJID)
	return err
}

func (r *ReactionRepository) GetByChatID(ctx context.Context, chatID uuid.UUID) ([]*domain.MessageReaction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, chat_id, target_message_id, sender_jid, sender_name, emoji, is_from_me, timestamp, created_at
		FROM message_reactions WHERE chat_id = $1
		ORDER BY timestamp ASC
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reactions []*domain.MessageReaction
	for rows.Next() {
		r := &domain.MessageReaction{}
		if err := rows.Scan(&r.ID, &r.AccountID, &r.ChatID, &r.TargetMessageID, &r.SenderJID, &r.SenderName, &r.Emoji, &r.IsFromMe, &r.Timestamp, &r.CreatedAt); err != nil {
			return nil, err
		}
		reactions = append(reactions, r)
	}
	return reactions, nil
}

// PollRepository handles poll data access
type PollRepository struct {
	db *pgxpool.Pool
}

func (r *PollRepository) CreateOptions(ctx context.Context, messageID uuid.UUID, options []string) error {
	for _, opt := range options {
		_, err := r.db.Exec(ctx, `
			INSERT INTO poll_options (message_id, name) VALUES ($1, $2)
		`, messageID, opt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *PollRepository) GetOptions(ctx context.Context, messageID uuid.UUID) ([]*domain.PollOption, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, message_id, name, vote_count FROM poll_options WHERE message_id = $1 ORDER BY id
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var options []*domain.PollOption
	for rows.Next() {
		o := &domain.PollOption{}
		if err := rows.Scan(&o.ID, &o.MessageID, &o.Name, &o.VoteCount); err != nil {
			return nil, err
		}
		options = append(options, o)
	}
	return options, nil
}

func (r *PollRepository) UpsertVote(ctx context.Context, vote *domain.PollVote) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO poll_votes (message_id, voter_jid, selected_names, timestamp)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (message_id, voter_jid) DO UPDATE SET
			selected_names = EXCLUDED.selected_names, timestamp = EXCLUDED.timestamp
		RETURNING id
	`, vote.MessageID, vote.VoterJID, vote.SelectedNames, vote.Timestamp).Scan(&vote.ID)
}

func (r *PollRepository) RecalculateVoteCounts(ctx context.Context, messageID uuid.UUID) error {
	// Reset all counts to 0, then recalculate from votes
	_, err := r.db.Exec(ctx, `UPDATE poll_options SET vote_count = 0 WHERE message_id = $1`, messageID)
	if err != nil {
		return err
	}

	// For each vote, increment the count of selected options
	rows, err := r.db.Query(ctx, `SELECT selected_names FROM poll_votes WHERE message_id = $1`, messageID)
	if err != nil {
		return err
	}
	defer rows.Close()

	countMap := make(map[string]int)
	for rows.Next() {
		var names []string
		if err := rows.Scan(&names); err != nil {
			return err
		}
		for _, n := range names {
			countMap[n]++
		}
	}
	for name, count := range countMap {
		_, _ = r.db.Exec(ctx, `UPDATE poll_options SET vote_count = $1 WHERE message_id = $2 AND name = $3`, count, messageID, name)
	}
	return nil
}

func (r *PollRepository) GetVotes(ctx context.Context, messageID uuid.UUID) ([]*domain.PollVote, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, message_id, voter_jid, selected_names, timestamp FROM poll_votes WHERE message_id = $1 ORDER BY timestamp
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var votes []*domain.PollVote
	for rows.Next() {
		v := &domain.PollVote{}
		if err := rows.Scan(&v.ID, &v.MessageID, &v.VoterJID, &v.SelectedNames, &v.Timestamp); err != nil {
			return nil, err
		}
		votes = append(votes, v)
	}
	return votes, nil
}

// CampaignAttachmentRepository handles campaign attachment operations
type CampaignAttachmentRepository struct {
	db *pgxpool.Pool
}

func (r *CampaignAttachmentRepository) CreateBatch(ctx context.Context, campaignID uuid.UUID, attachments []*domain.CampaignAttachment) error {
	if len(attachments) == 0 {
		return nil
	}
	for i, a := range attachments {
		a.ID = uuid.New()
		a.CampaignID = campaignID
		if a.Position == 0 {
			a.Position = i
		}
		a.CreatedAt = time.Now()
		_, err := r.db.Exec(ctx, `
			INSERT INTO campaign_attachments (id, campaign_id, media_url, media_type, caption, file_name, file_size, position, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, a.ID, a.CampaignID, a.MediaURL, a.MediaType, a.Caption, a.FileName, a.FileSize, a.Position, a.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *CampaignAttachmentRepository) GetByCampaignID(ctx context.Context, campaignID uuid.UUID) ([]*domain.CampaignAttachment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, campaign_id, media_url, media_type, caption, file_name, file_size, position, created_at
		FROM campaign_attachments WHERE campaign_id = $1 ORDER BY position
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []*domain.CampaignAttachment
	for rows.Next() {
		a := &domain.CampaignAttachment{}
		if err := rows.Scan(&a.ID, &a.CampaignID, &a.MediaURL, &a.MediaType, &a.Caption, &a.FileName, &a.FileSize, &a.Position, &a.CreatedAt); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, nil
}

func (r *CampaignAttachmentRepository) DeleteByCampaignID(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM campaign_attachments WHERE campaign_id = $1`, campaignID)
	return err
}

// ============================================================
// QuickReplyRepository handles quick reply (canned response) data access
// ============================================================

type QuickReplyRepository struct {
	db *pgxpool.Pool
}

func (r *QuickReplyRepository) loadAttachments(ctx context.Context, qrIDs []uuid.UUID) (map[uuid.UUID][]domain.QuickReplyAttachment, error) {
	if len(qrIDs) == 0 {
		return map[uuid.UUID][]domain.QuickReplyAttachment{}, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, quick_reply_id, media_url, media_type, media_filename, caption, position
		FROM quick_reply_attachments WHERE quick_reply_id = ANY($1) ORDER BY position
	`, qrIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[uuid.UUID][]domain.QuickReplyAttachment)
	for rows.Next() {
		var a domain.QuickReplyAttachment
		if err := rows.Scan(&a.ID, &a.QuickReplyID, &a.MediaURL, &a.MediaType, &a.MediaFilename, &a.Caption, &a.Position); err != nil {
			return nil, err
		}
		m[a.QuickReplyID] = append(m[a.QuickReplyID], a)
	}
	return m, nil
}

func (r *QuickReplyRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.QuickReply, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, shortcut, title, body, COALESCE(media_url,''), COALESCE(media_type,''), COALESCE(media_filename,''), created_at, updated_at
		FROM quick_replies WHERE account_id = $1 ORDER BY shortcut
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var replies []*domain.QuickReply
	var ids []uuid.UUID
	for rows.Next() {
		qr := &domain.QuickReply{}
		if err := rows.Scan(&qr.ID, &qr.AccountID, &qr.Shortcut, &qr.Title, &qr.Body, &qr.MediaURL, &qr.MediaType, &qr.MediaFilename, &qr.CreatedAt, &qr.UpdatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, qr)
		ids = append(ids, qr.ID)
	}

	// Load attachments for all quick replies in one query
	attMap, err := r.loadAttachments(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, qr := range replies {
		qr.Attachments = attMap[qr.ID]
		if qr.Attachments == nil {
			qr.Attachments = []domain.QuickReplyAttachment{}
		}
	}
	return replies, nil
}

func (r *QuickReplyRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.QuickReply, error) {
	qr := &domain.QuickReply{}
	err := r.db.QueryRow(ctx, `
		SELECT id, account_id, shortcut, title, body, COALESCE(media_url,''), COALESCE(media_type,''), COALESCE(media_filename,''), created_at, updated_at
		FROM quick_replies WHERE id = $1
	`, id).Scan(&qr.ID, &qr.AccountID, &qr.Shortcut, &qr.Title, &qr.Body, &qr.MediaURL, &qr.MediaType, &qr.MediaFilename, &qr.CreatedAt, &qr.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	attMap, err := r.loadAttachments(ctx, []uuid.UUID{qr.ID})
	if err != nil {
		return nil, err
	}
	qr.Attachments = attMap[qr.ID]
	if qr.Attachments == nil {
		qr.Attachments = []domain.QuickReplyAttachment{}
	}
	return qr, err
}

func (r *QuickReplyRepository) Create(ctx context.Context, qr *domain.QuickReply) error {
	qr.ID = uuid.New()
	now := time.Now()
	qr.CreatedAt = now
	qr.UpdatedAt = now
	_, err := r.db.Exec(ctx, `
		INSERT INTO quick_replies (id, account_id, shortcut, title, body, media_url, media_type, media_filename, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, qr.ID, qr.AccountID, qr.Shortcut, qr.Title, qr.Body, qr.MediaURL, qr.MediaType, qr.MediaFilename, qr.CreatedAt, qr.UpdatedAt)
	if err != nil {
		return err
	}
	return r.ReplaceAttachments(ctx, qr.ID, qr.Attachments)
}

func (r *QuickReplyRepository) Update(ctx context.Context, qr *domain.QuickReply) error {
	qr.UpdatedAt = time.Now()
	_, err := r.db.Exec(ctx, `
		UPDATE quick_replies SET shortcut = $1, title = $2, body = $3, media_url = $4, media_type = $5, media_filename = $6, updated_at = $7
		WHERE id = $8
	`, qr.Shortcut, qr.Title, qr.Body, qr.MediaURL, qr.MediaType, qr.MediaFilename, qr.UpdatedAt, qr.ID)
	if err != nil {
		return err
	}
	return r.ReplaceAttachments(ctx, qr.ID, qr.Attachments)
}

func (r *QuickReplyRepository) ReplaceAttachments(ctx context.Context, quickReplyID uuid.UUID, attachments []domain.QuickReplyAttachment) error {
	// Delete existing
	_, err := r.db.Exec(ctx, `DELETE FROM quick_reply_attachments WHERE quick_reply_id = $1`, quickReplyID)
	if err != nil {
		return err
	}
	// Insert new (max 5)
	for i, a := range attachments {
		if i >= 5 {
			break
		}
		if a.ID == uuid.Nil {
			a.ID = uuid.New()
		}
		_, err := r.db.Exec(ctx, `
			INSERT INTO quick_reply_attachments (id, quick_reply_id, media_url, media_type, media_filename, caption, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, a.ID, quickReplyID, a.MediaURL, a.MediaType, a.MediaFilename, a.Caption, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *QuickReplyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM quick_replies WHERE id = $1`, id)
	return err
}

// RoleRepository handles RBAC role and permission management
type RoleRepository struct {
	db *pgxpool.Pool
}

func (r *RoleRepository) GetAll(ctx context.Context) ([]*domain.Role, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, description, is_system, COALESCE(permissions, '{}'), created_at, updated_at
		FROM roles ORDER BY is_system DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []*domain.Role
	for rows.Next() {
		role := &domain.Role{}
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem,
			&role.Permissions, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, err
		}
		if role.Permissions == nil {
			role.Permissions = []string{}
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (r *RoleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	role := &domain.Role{}
	err := r.db.QueryRow(ctx, `
		SELECT id, name, description, is_system, COALESCE(permissions, '{}'), created_at, updated_at
		FROM roles WHERE id = $1
	`, id).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem,
		&role.Permissions, &role.CreatedAt, &role.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	return role, nil
}

func (r *RoleRepository) Create(ctx context.Context, role *domain.Role) error {
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO roles (name, description, is_system, permissions)
		VALUES ($1, $2, FALSE, $3)
		RETURNING id, created_at, updated_at
	`, role.Name, role.Description, role.Permissions).Scan(&role.ID, &role.CreatedAt, &role.UpdatedAt)
}

func (r *RoleRepository) Update(ctx context.Context, role *domain.Role) error {
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	result, err := r.db.Exec(ctx, `
		UPDATE roles SET name = $2, description = $3, permissions = $4, updated_at = NOW()
		WHERE id = $1
	`, role.ID, role.Name, role.Description, role.Permissions)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("role not found")
	}
	return nil
}

func (r *RoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.Exec(ctx, `DELETE FROM roles WHERE id = $1 AND is_system = FALSE`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("role not found or cannot delete system role")
	}
	return nil
}
