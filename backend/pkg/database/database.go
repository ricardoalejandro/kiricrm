package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/naperu/clarin/pkg/config"
	"golang.org/x/crypto/bcrypt"
)

func Connect(databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	poolConfig.MaxConns = 50
	poolConfig.MinConns = 10
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

func Migrate(db *pgxpool.Pool) error {
	ctx := context.Background()

	migrations := []string{
		// Accounts table (multi-tenant)
		`CREATE TABLE IF NOT EXISTS accounts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(255) NOT NULL,
			plan VARCHAR(50) DEFAULT 'free',
			max_devices INT DEFAULT 5,
			max_users_override INT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Users table
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			username VARCHAR(255) UNIQUE NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			display_name VARCHAR(255),
			is_admin BOOLEAN DEFAULT FALSE,
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Devices table (WhatsApp connections)
		`CREATE TABLE IF NOT EXISTS devices (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255),
			phone VARCHAR(50),
			jid VARCHAR(255),
			status VARCHAR(50) DEFAULT 'disconnected',
			qr_code TEXT,
			last_seen_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Contacts table
		`CREATE TABLE IF NOT EXISTS contacts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
			jid VARCHAR(255) NOT NULL,
			phone VARCHAR(50),
			name VARCHAR(255),
			push_name VARCHAR(255),
			avatar_url TEXT,
			avatar_checked_at TIMESTAMPTZ,
			is_group BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(account_id, jid)
		)`,

		// Chats table
		`CREATE TABLE IF NOT EXISTS chats (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			jid VARCHAR(255) NOT NULL,
			name VARCHAR(255),
			last_message TEXT,
			last_message_at TIMESTAMPTZ,
			unread_count INT DEFAULT 0,
			is_archived BOOLEAN DEFAULT FALSE,
			is_pinned BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(account_id, jid)
		)`,

		// Messages table
		`CREATE TABLE IF NOT EXISTS messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
			chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
			message_id VARCHAR(255) NOT NULL,
			from_jid VARCHAR(255),
			from_name VARCHAR(255),
			body TEXT,
			message_type VARCHAR(50) DEFAULT 'text',
			media_url TEXT,
			media_mimetype VARCHAR(100),
			media_filename VARCHAR(255),
			media_size BIGINT,
			is_from_me BOOLEAN DEFAULT FALSE,
			is_read BOOLEAN DEFAULT FALSE,
			status VARCHAR(50) DEFAULT 'sent',
			timestamp TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(account_id, device_id, message_id)
		)`,

		// Leads table
		`CREATE TABLE IF NOT EXISTS leads (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			jid VARCHAR(255) NOT NULL,
			name VARCHAR(255),
			phone VARCHAR(50),
			email VARCHAR(255),
			status VARCHAR(50) DEFAULT 'new',
			source VARCHAR(100),
			notes TEXT,
			tags TEXT[],
			custom_fields JSONB DEFAULT '{}',
			assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(account_id, jid)
		)`,

		// Pipelines table
		`CREATE TABLE IF NOT EXISTS pipelines (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			is_default BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Pipeline stages table
		`CREATE TABLE IF NOT EXISTS pipeline_stages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			pipeline_id UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			color VARCHAR(50) DEFAULT '#6366f1',
			position INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Lead pipeline assignments
		`CREATE TABLE IF NOT EXISTS lead_stages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			lead_id UUID NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
			stage_id UUID NOT NULL REFERENCES pipeline_stages(id) ON DELETE CASCADE,
			entered_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(lead_id, stage_id)
		)`,

		// Contact device names table (per-device contact names)
		`CREATE TABLE IF NOT EXISTS contact_device_names (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
			device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			name VARCHAR(255),
			push_name VARCHAR(255),
			business_name VARCHAR(255),
			synced_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(contact_id, device_id)
		)`,

		// Add new columns to contacts table
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS custom_name VARCHAR(255)`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS email VARCHAR(255)`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS company VARCHAR(255)`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS tags TEXT[]`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS notes TEXT`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS source VARCHAR(100) DEFAULT 'whatsapp'`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS last_name VARCHAR(255)`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS short_name VARCHAR(100)`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS age INTEGER`,

		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_users_account ON users(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_account ON devices(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_account ON contacts(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_phone ON contacts(phone)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(name)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_device_names_contact ON contact_device_names(contact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_device_names_device ON contact_device_names(device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chats_account ON chats(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(chat_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_leads_account ON leads(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_leads_status ON leads(status)`,

		// Tags system
		`CREATE TABLE IF NOT EXISTS tags (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			color VARCHAR(20) DEFAULT '#6366f1',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(account_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS contact_tags (
			contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
			tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (contact_id, tag_id)
		)`,
		`CREATE TABLE IF NOT EXISTS chat_tags (
			chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
			tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (chat_id, tag_id)
		)`,

		// Campaigns (mass messaging)
		`CREATE TABLE IF NOT EXISTS campaigns (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			message_template TEXT NOT NULL DEFAULT '',
			media_url TEXT,
			media_type VARCHAR(50),
			status VARCHAR(50) DEFAULT 'draft',
			scheduled_at TIMESTAMPTZ,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			total_recipients INT DEFAULT 0,
			sent_count INT DEFAULT 0,
			failed_count INT DEFAULT 0,
			settings JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS campaign_recipients (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			jid VARCHAR(255) NOT NULL,
			name VARCHAR(255),
			phone VARCHAR(50),
			status VARCHAR(50) DEFAULT 'pending',
			sent_at TIMESTAMPTZ,
			error_message TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tags_account ON tags(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_campaigns_account ON campaigns(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_campaigns_status ON campaigns(status)`,
		`CREATE INDEX IF NOT EXISTS idx_campaign_recipients_campaign ON campaign_recipients(campaign_id)`,
		`CREATE INDEX IF NOT EXISTS idx_campaign_recipients_status ON campaign_recipients(status)`,

		// Events system (contact interaction tracking)
		`CREATE TABLE IF NOT EXISTS events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			event_date TIMESTAMPTZ,
			event_end TIMESTAMPTZ,
			location VARCHAR(500),
			status VARCHAR(50) DEFAULT 'active',
			color VARCHAR(20) DEFAULT '#3b82f6',
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS event_participants (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			name VARCHAR(255) NOT NULL,
			last_name VARCHAR(255),
			phone VARCHAR(50),
			email VARCHAR(255),
			age INT,
			status VARCHAR(50) DEFAULT 'invited',
			notes TEXT,
			next_action TEXT,
			next_action_date TIMESTAMPTZ,
			invited_at TIMESTAMPTZ DEFAULT NOW(),
			confirmed_at TIMESTAMPTZ,
			attended_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS interactions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			event_id UUID REFERENCES events(id) ON DELETE SET NULL,
			participant_id UUID REFERENCES event_participants(id) ON DELETE SET NULL,
			type VARCHAR(50) NOT NULL,
			direction VARCHAR(20),
			outcome VARCHAR(50),
			notes TEXT,
			next_action TEXT,
			next_action_date TIMESTAMPTZ,
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_account ON events(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_status ON events(status)`,
		`CREATE INDEX IF NOT EXISTS idx_events_date ON events(event_date)`,
		`CREATE INDEX IF NOT EXISTS idx_event_participants_event ON event_participants(event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_participants_contact ON event_participants(contact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_participants_status ON event_participants(status)`,
		`CREATE INDEX IF NOT EXISTS idx_event_participants_next_action ON event_participants(next_action_date)`,
		`ALTER TABLE interactions ADD COLUMN IF NOT EXISTS lead_id UUID REFERENCES leads(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_interactions_account ON interactions(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_interactions_contact ON interactions(contact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_interactions_event ON interactions(event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_interactions_participant ON interactions(participant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_interactions_lead ON interactions(lead_id)`,
		`CREATE INDEX IF NOT EXISTS idx_interactions_created ON interactions(created_at DESC)`,

		// Participant tags
		`CREATE TABLE IF NOT EXISTS participant_tags (
			participant_id UUID NOT NULL REFERENCES event_participants(id) ON DELETE CASCADE,
			tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (participant_id, tag_id)
		)`,

		// Campaign source tracking
		// Quoted/reply message fields
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS quoted_message_id VARCHAR(255)`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS quoted_body TEXT`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS quoted_sender VARCHAR(255)`,

		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS event_id UUID REFERENCES events(id) ON DELETE SET NULL`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS source VARCHAR(50)`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS short_name VARCHAR(100)`,
		`DROP INDEX IF EXISTS idx_event_participants_unique_phone`,
		`DROP INDEX IF EXISTS idx_event_participants_unique_email`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_event_participants_unique_contact ON event_participants(event_id, contact_id) WHERE contact_id IS NOT NULL`,

		// Saved stickers
		`CREATE TABLE IF NOT EXISTS saved_stickers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			media_url TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_stickers_unique ON saved_stickers(account_id, media_url)`,

		// Multi-tenant management columns
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_super_admin BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(50) DEFAULT 'admin'`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS slug VARCHAR(255)`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS max_users_override INT NULL`,
		`UPDATE users SET is_super_admin = TRUE, role = 'super_admin' WHERE is_admin = TRUE AND account_id = (SELECT id FROM accounts ORDER BY created_at LIMIT 1) AND is_super_admin = FALSE`,

		// Multi-account user assignments (user can belong to many accounts)
		`CREATE TABLE IF NOT EXISTS user_accounts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			role VARCHAR(50) DEFAULT 'agent',
			is_default BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_accounts_unique ON user_accounts(user_id, account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_accounts_user ON user_accounts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_accounts_account ON user_accounts(account_id)`,

		// Programs (Courses, Workshops, etc.)
		`CREATE TABLE IF NOT EXISTS programs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			status VARCHAR(50) DEFAULT 'active',
			color VARCHAR(20) DEFAULT '#10b981',
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_programs_account ON programs(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_programs_status ON programs(status)`,

		// Program Participants
		`CREATE TABLE IF NOT EXISTS program_participants (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			program_id UUID NOT NULL REFERENCES programs(id) ON DELETE CASCADE,
			contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
			status VARCHAR(50) DEFAULT 'enrolled',
			enrolled_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(program_id, contact_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_program_participants_program ON program_participants(program_id)`,
		`CREATE INDEX IF NOT EXISTS idx_program_participants_contact ON program_participants(contact_id)`,

		// Program Sessions (Classes)
		`CREATE TABLE IF NOT EXISTS program_sessions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			program_id UUID NOT NULL REFERENCES programs(id) ON DELETE CASCADE,
			date TIMESTAMPTZ NOT NULL,
			topic VARCHAR(255),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_program_sessions_program ON program_sessions(program_id)`,
		`CREATE INDEX IF NOT EXISTS idx_program_sessions_date ON program_sessions(date)`,

		// Program Attendance
		`CREATE TABLE IF NOT EXISTS program_attendance (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			session_id UUID NOT NULL REFERENCES program_sessions(id) ON DELETE CASCADE,
			participant_id UUID NOT NULL REFERENCES program_participants(id) ON DELETE CASCADE,
			status VARCHAR(50) NOT NULL, -- present, absent, late, excused
			notes TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(session_id, participant_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_program_attendance_session ON program_attendance(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_program_attendance_participant ON program_attendance(participant_id)`,
		// Seed existing user-account relationships into junction table
		`INSERT INTO user_accounts (user_id, account_id, role, is_default)
		 SELECT id, account_id, role, TRUE FROM users
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM user_accounts existing WHERE existing.user_id = users.id
		 )
		 ON CONFLICT (user_id, account_id) DO NOTHING`,
		// Normalize user-account assignments so every user has one default and
		// the legacy users.account_id mirrors that default assignment.
		`INSERT INTO user_accounts (user_id, account_id, role, is_default)
		 SELECT id, account_id, COALESCE(NULLIF(role, ''), 'agent'), TRUE
		 FROM users
		 WHERE account_id IS NOT NULL
		   AND NOT EXISTS (
		   	SELECT 1 FROM user_accounts existing WHERE existing.user_id = users.id
		   )
		 ON CONFLICT (user_id, account_id) DO NOTHING`,
		`WITH ranked AS (
			SELECT ua.id,
			       ROW_NUMBER() OVER (
			         PARTITION BY ua.user_id
			         ORDER BY ua.is_default DESC, (ua.account_id = u.account_id) DESC, ua.created_at ASC, ua.id ASC
			       ) AS rn
			FROM user_accounts ua
			JOIN users u ON u.id = ua.user_id
		)
		UPDATE user_accounts ua
		SET is_default = (ranked.rn = 1)
		FROM ranked
		WHERE ua.id = ranked.id`,
		`WITH chosen AS (
			SELECT DISTINCT ON (ua.user_id)
			       ua.user_id,
			       ua.account_id,
			       COALESCE(NULLIF(ua.role, ''), 'agent') AS role
			FROM user_accounts ua
			WHERE ua.is_default = TRUE
			ORDER BY ua.user_id, ua.created_at ASC, ua.id ASC
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
		WHERE u.id = chosen.user_id`,

		// Allow saving attendance notes without a status
		`ALTER TABLE program_attendance ALTER COLUMN status DROP NOT NULL`,

		// Campaign recipient timing tracking
		`ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS wait_time_ms INT`,

		// Message reactions table
		`CREATE TABLE IF NOT EXISTS message_reactions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
			target_message_id VARCHAR(255) NOT NULL,
			sender_jid VARCHAR(255) NOT NULL,
			sender_name VARCHAR(255),
			emoji VARCHAR(50) NOT NULL,
			is_from_me BOOLEAN DEFAULT FALSE,
			timestamp TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(chat_id, target_message_id, sender_jid)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_message_reactions_chat ON message_reactions(chat_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_reactions_target ON message_reactions(target_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_reactions_filter ON message_reactions(account_id, chat_id, is_from_me, timestamp DESC)`,

		// Poll options table
		`CREATE TABLE IF NOT EXISTS poll_options (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			vote_count INT DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_poll_options_message ON poll_options(message_id)`,

		// Poll votes table
		`CREATE TABLE IF NOT EXISTS poll_votes (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			voter_jid VARCHAR(255) NOT NULL,
			selected_names TEXT[] NOT NULL DEFAULT '{}',
			timestamp TIMESTAMPTZ NOT NULL,
			UNIQUE(message_id, voter_jid)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_poll_votes_message ON poll_votes(message_id)`,

		// Poll metadata on messages
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS poll_question TEXT`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS poll_max_selections INT DEFAULT 1`,

		// Campaign recipient metadata for custom variables
		`ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}'`,

		// Lead contact fields sync
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS last_name VARCHAR(255)`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS short_name VARCHAR(100)`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS company VARCHAR(255)`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS age INTEGER`,

		// Campaign attachments (multi-file support)
		`CREATE TABLE IF NOT EXISTS campaign_attachments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			media_url TEXT NOT NULL,
			media_type VARCHAR(50) NOT NULL,
			caption TEXT DEFAULT '',
			file_name VARCHAR(255) DEFAULT '',
			file_size BIGINT DEFAULT 0,
			position INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_campaign_attachments_campaign ON campaign_attachments(campaign_id)`,

		// Quick replies (canned responses)
		`CREATE TABLE IF NOT EXISTS quick_replies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			shortcut VARCHAR(100) NOT NULL,
			title VARCHAR(255) NOT NULL,
			body TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_quick_replies_account ON quick_replies(account_id)`,

		// Quick replies media support
		`ALTER TABLE quick_replies ADD COLUMN IF NOT EXISTS media_url TEXT DEFAULT ''`,
		`ALTER TABLE quick_replies ADD COLUMN IF NOT EXISTS media_type VARCHAR(50) DEFAULT ''`,
		`ALTER TABLE quick_replies ADD COLUMN IF NOT EXISTS media_filename VARCHAR(255) DEFAULT ''`,

		// Quick reply attachments (multi-attachment support)
		`CREATE TABLE IF NOT EXISTS quick_reply_attachments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			quick_reply_id UUID NOT NULL REFERENCES quick_replies(id) ON DELETE CASCADE,
			media_url TEXT NOT NULL,
			media_type VARCHAR(50) NOT NULL DEFAULT 'document',
			media_filename VARCHAR(255) NOT NULL DEFAULT '',
			caption TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			CONSTRAINT quick_reply_attachments_max_5 CHECK (position >= 0 AND position < 5)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_quick_reply_attachments_qr ON quick_reply_attachments(quick_reply_id)`,

		// Lead pipeline linkage
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS pipeline_id UUID REFERENCES pipelines(id) ON DELETE SET NULL`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS stage_id UUID REFERENCES pipeline_stages(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_leads_pipeline ON leads(pipeline_id)`,
		`CREATE INDEX IF NOT EXISTS idx_leads_stage ON leads(stage_id)`,

		// Kommo CRM integration
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS kommo_id BIGINT`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS kommo_id BIGINT`,
		`ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS kommo_id BIGINT`,
		`ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS kommo_id BIGINT`,
		`ALTER TABLE tags ADD COLUMN IF NOT EXISTS kommo_id BIGINT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_leads_kommo_id ON leads(account_id, kommo_id) WHERE kommo_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_kommo_id ON contacts(account_id, kommo_id) WHERE kommo_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pipelines_kommo_id ON pipelines(account_id, kommo_id) WHERE kommo_id IS NOT NULL`,
		`DROP INDEX IF EXISTS idx_pipeline_stages_kommo_id`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pipeline_stages_pipeline_kommo_id ON pipeline_stages(pipeline_id, kommo_id) WHERE kommo_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_kommo_id ON tags(account_id, kommo_id) WHERE kommo_id IS NOT NULL`,

		// Kommo connected pipelines (real-time sync tracking)
		`CREATE TABLE IF NOT EXISTS kommo_connected_pipelines (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			kommo_pipeline_id BIGINT NOT NULL,
			pipeline_id UUID REFERENCES pipelines(id) ON DELETE SET NULL,
			enabled BOOLEAN DEFAULT TRUE,
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(account_id, kommo_pipeline_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kommo_connected_pipelines_account ON kommo_connected_pipelines(account_id)`,

		// Integration framework: global, multi-account and per-account instances.
		`CREATE TABLE IF NOT EXISTS integration_instances (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			provider VARCHAR(50) NOT NULL,
			scope VARCHAR(50) NOT NULL DEFAULT 'account',
			name VARCHAR(255) NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			subdomain VARCHAR(255) NOT NULL DEFAULT '',
			client_id TEXT NOT NULL DEFAULT '',
			client_secret TEXT NOT NULL DEFAULT '',
			access_token TEXT NOT NULL DEFAULT '',
			refresh_token TEXT NOT NULL DEFAULT '',
			redirect_uri TEXT NOT NULL DEFAULT '',
			webhook_secret TEXT NOT NULL DEFAULT '',
			config JSONB NOT NULL DEFAULT '{}'::jsonb,
			last_sync_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(provider, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_integration_instances_provider_name ON integration_instances(provider, name)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_instances_provider_active ON integration_instances(provider, is_active)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_integration_instances_webhook_secret ON integration_instances(provider, webhook_secret) WHERE webhook_secret <> ''`,
		`CREATE TABLE IF NOT EXISTS integration_instance_accounts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			integration_instance_id UUID NOT NULL REFERENCES integration_instances(id) ON DELETE CASCADE,
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(integration_instance_id, account_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_instance_accounts_account ON integration_instance_accounts(account_id, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_instance_accounts_instance ON integration_instance_accounts(integration_instance_id, enabled)`,
		`ALTER TABLE kommo_connected_pipelines ADD COLUMN IF NOT EXISTS integration_instance_id UUID REFERENCES integration_instances(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_kommo_connected_pipelines_instance ON kommo_connected_pipelines(integration_instance_id) WHERE integration_instance_id IS NOT NULL`,
		`ALTER TABLE kommo_connected_pipelines DROP CONSTRAINT IF EXISTS kommo_connected_pipelines_account_id_kommo_pipeline_id_key`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_kommo_connected_pipelines_instance_account_pipeline ON kommo_connected_pipelines(COALESCE(integration_instance_id, '00000000-0000-0000-0000-000000000000'::uuid), account_id, kommo_pipeline_id)`,

		// Anti-loop: track last push timestamp to detect echoes from Kommo poller
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS kommo_last_pushed_at BIGINT DEFAULT 0`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS kommo_last_pushed_at BIGINT DEFAULT 0`,

		// Kommo call slot tracking on interactions (for dedup during sync)
		`ALTER TABLE interactions ADD COLUMN IF NOT EXISTS kommo_call_slot INT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_interactions_kommo_call_slot ON interactions(lead_id, kommo_call_slot) WHERE kommo_call_slot IS NOT NULL`,

		// Account settings: default incoming stage for new leads
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS default_incoming_stage_id UUID REFERENCES pipeline_stages(id) ON DELETE SET NULL`,

		// Performance indexes for chat listing
		`CREATE INDEX IF NOT EXISTS idx_chats_account_lastmsg ON chats(account_id, last_message_at DESC NULLS LAST) WHERE jid NOT LIKE '%@g.us' AND jid NOT LIKE '%@newsletter' AND jid NOT LIKE '%@broadcast' AND jid NOT LIKE '%@lid'`,
		`CREATE INDEX IF NOT EXISTS idx_chats_device_id ON chats(device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_jid ON contacts(jid)`,
		`CREATE INDEX IF NOT EXISTS idx_leads_jid ON leads(account_id, jid)`,

		// Composite index for leads ordered listing (covers GetByAccountID ORDER BY)
		`CREATE INDEX IF NOT EXISTS idx_leads_account_created ON leads(account_id, created_at DESC)`,
		// Composite index for stage-based pagination (infinite scroll per column)
		`CREATE INDEX IF NOT EXISTS idx_leads_stage_created ON leads(stage_id, created_at DESC)`,
		// Composite index for pipeline + account filtering
		`CREATE INDEX IF NOT EXISTS idx_leads_pipeline_account ON leads(account_id, pipeline_id)`,

		// Backfill participant_tags from contact_tags for existing participants
		`INSERT INTO participant_tags (participant_id, tag_id)
		 SELECT ep.id, ct.tag_id FROM event_participants ep
		 JOIN contact_tags ct ON ct.contact_id = ep.contact_id
		 WHERE ep.contact_id IS NOT NULL
		 ON CONFLICT DO NOTHING`,

		// Program schedule fields
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS schedule_start_date TIMESTAMPTZ`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS schedule_end_date TIMESTAMPTZ`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS schedule_days INT[] DEFAULT '{}'`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS schedule_start_time VARCHAR(10)`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS schedule_end_time VARCHAR(10)`,

		// Program session time/location fields
		`ALTER TABLE program_sessions ADD COLUMN IF NOT EXISTS start_time VARCHAR(10)`,
		`ALTER TABLE program_sessions ADD COLUMN IF NOT EXISTS end_time VARCHAR(10)`,
		`ALTER TABLE program_sessions ADD COLUMN IF NOT EXISTS location TEXT`,

		// Program participants: track which lead was used to add the participant
		`ALTER TABLE program_participants ADD COLUMN IF NOT EXISTS lead_id UUID REFERENCES leads(id) ON DELETE SET NULL`,

		// RBAC: Custom roles with granular module permissions
		`CREATE TABLE IF NOT EXISTS roles (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(255) UNIQUE NOT NULL,
			description TEXT DEFAULT '',
			is_system BOOLEAN DEFAULT FALSE,
			permissions TEXT[] DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Link user_accounts to a role
		`ALTER TABLE user_accounts ADD COLUMN IF NOT EXISTS role_id UUID REFERENCES roles(id) ON DELETE SET NULL`,

		// Seed system roles (idempotent)
		`INSERT INTO roles (name, description, is_system, permissions) VALUES
			('Administrador', 'Acceso total a todos los módulos', TRUE, ARRAY['chats','contacts','programs','devices','leads','events','broadcasts','tags','settings','integrations'])
		 ON CONFLICT (name) DO NOTHING`,
		// Ensure existing 'Administrador' role gets the new 'integrations' permission
		`UPDATE roles SET permissions = array_append(permissions, 'integrations') WHERE name = 'Administrador' AND NOT ('integrations' = ANY(permissions))`,
		`INSERT INTO roles (name, description, is_system, permissions) VALUES
			('Supervisor', 'Acceso a chats, leads, contactos y eventos', TRUE, ARRAY['chats','contacts','leads','events','tags'])
		 ON CONFLICT (name) DO NOTHING`,
		`INSERT INTO roles (name, description, is_system, permissions) VALUES
			('Agente Básico', 'Acceso solo a chats y contactos', TRUE, ARRAY['chats','contacts','tags'])
		 ON CONFLICT (name) DO NOTHING`,
		// Backfill newly split module permissions only for the platform admin.
		// Custom roles must preserve the exact permissions saved by admins.
		`UPDATE roles SET permissions = array_append(permissions, 'automations') WHERE name = 'Administrador' AND NOT ('automations' = ANY(permissions))`,
		`UPDATE roles SET permissions = array_append(permissions, 'bots') WHERE name = 'Administrador' AND NOT ('bots' = ANY(permissions))`,
		`CREATE TABLE IF NOT EXISTS migration_flags (
			key TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// One-time repair for roles affected by the previous Leads backfill.
		// After this marker is set, admins can grant these permissions normally.
		`DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM migration_flags WHERE key = 'repair_role_permissions_leads_backfill_20260510') THEN
				UPDATE roles
				SET permissions = array_remove(array_remove(permissions, 'automations'), 'bots')
				WHERE name <> 'Administrador'
				  AND 'leads' = ANY(permissions)
				  AND ('automations' = ANY(permissions) OR 'bots' = ANY(permissions));

				INSERT INTO migration_flags (key)
				VALUES ('repair_role_permissions_leads_backfill_20260510')
				ON CONFLICT (key) DO NOTHING;
			END IF;
		END $$`,

		// Event folders – Windows Explorer style folder organisation
		`CREATE TABLE IF NOT EXISTS event_folders (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			parent_id UUID REFERENCES event_folders(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			color VARCHAR(20) DEFAULT '#3b82f6',
			icon VARCHAR(50) DEFAULT '📁',
			position INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS folder_id UUID REFERENCES event_folders(id) ON DELETE SET NULL`,

		// WhatsApp extended message fields
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_revoked BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_view_once BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_deleted BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_deleted_at TIMESTAMPTZ`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS latitude DOUBLE PRECISION`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS longitude DOUBLE PRECISION`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS contact_name VARCHAR(255)`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS contact_phone VARCHAR(100)`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS contact_vcard TEXT`,

		// Message editing support
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_edited BOOLEAN DEFAULT FALSE`,

		// DNI and BirthDate on leads (mirrors contacts)
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS dni VARCHAR(50)`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS birth_date TIMESTAMPTZ`,

		// Event pipelines (dynamic stages for events, replacing hardcoded statuses)
		`CREATE TABLE IF NOT EXISTS event_pipelines (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			is_default BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_event_pipelines_account ON event_pipelines(account_id)`,

		// Event pipeline stages
		`CREATE TABLE IF NOT EXISTS event_pipeline_stages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			pipeline_id UUID NOT NULL REFERENCES event_pipelines(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			color VARCHAR(50) DEFAULT '#6366f1',
			position INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_event_pipeline_stages_pipeline ON event_pipeline_stages(pipeline_id)`,

		// Link events to event pipelines
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS pipeline_id UUID REFERENCES event_pipelines(id) ON DELETE SET NULL`,

		// Link event participants to pipeline stages and leads
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS stage_id UUID REFERENCES event_pipeline_stages(id) ON DELETE SET NULL`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS lead_id UUID REFERENCES leads(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_event_participants_stage ON event_participants(stage_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_participants_lead ON event_participants(lead_id)`,

		// Option 3: allow multiple leads per phone (JID) per account.
		// Each Kommo lead is unique by kommo_id (partial unique index already exists).
		// The (account_id, jid) unique constraint is dropped so leads sharing a phone
		// (e.g. archived + active) can both sync correctly.
		`ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_account_id_jid_key`,

		// Event tag auto-sync: junction table linking events to tags
		`CREATE TABLE IF NOT EXISTS event_tags (
			event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (event_id, tag_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_event_tags_tag_id ON event_tags(tag_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_tags_event_id ON event_tags(event_id)`,

		// Mark participants created by tag auto-sync (manual participants won't be auto-removed)
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS auto_tag_sync BOOLEAN DEFAULT FALSE`,
		// Index for fast lookup of auto-synced participants by lead_id
		`CREATE INDEX IF NOT EXISTS idx_event_participants_auto_sync ON event_participants(event_id, lead_id) WHERE auto_tag_sync = TRUE`,

		// Formula mode for event tag matching (AND = all tags required, OR = any tag matches)
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS tag_formula_mode TEXT NOT NULL DEFAULT 'OR'`,

		// Negate flag for event_tags (TRUE = exclude leads with this tag)
		`ALTER TABLE event_tags ADD COLUMN IF NOT EXISTS negate BOOLEAN NOT NULL DEFAULT FALSE`,

		// Advanced formula: text-based boolean expression and type selector
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS tag_formula TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS tag_formula_type TEXT NOT NULL DEFAULT 'simple'`,

		// Campaign tracking: who created / who started
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id)`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS started_by UUID REFERENCES users(id)`,

		// ── Event Logbooks (Bitácora) ──────────────────────────────────
		`CREATE TABLE IF NOT EXISTS event_logbooks (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			date DATE NOT NULL,
			title VARCHAR(255) NOT NULL DEFAULT '',
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			general_notes TEXT NOT NULL DEFAULT '',
			stage_snapshot JSONB DEFAULT '{}',
			total_participants INT NOT NULL DEFAULT 0,
			captured_at TIMESTAMPTZ,
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(event_id, date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_event_logbooks_event ON event_logbooks(event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_logbooks_account ON event_logbooks(account_id)`,

		// Event Logbook Entries (per-participant snapshot)
		`CREATE TABLE IF NOT EXISTS event_logbook_entries (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			logbook_id UUID NOT NULL REFERENCES event_logbooks(id) ON DELETE CASCADE,
			participant_id UUID NOT NULL REFERENCES event_participants(id) ON DELETE CASCADE,
			stage_id UUID REFERENCES event_pipeline_stages(id) ON DELETE SET NULL,
			stage_name VARCHAR(255) NOT NULL DEFAULT '',
			stage_color VARCHAR(50) NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(logbook_id, participant_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_event_logbook_entries_logbook ON event_logbook_entries(logbook_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_logbook_entries_participant ON event_logbook_entries(participant_id)`,

		// MCP access control per account
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS mcp_enabled BOOLEAN DEFAULT FALSE`,

		// Eros AI conversation persistence
		`CREATE TABLE IF NOT EXISTS eros_conversations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_eros_conversations_user ON eros_conversations (user_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS eros_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID NOT NULL REFERENCES eros_conversations(id) ON DELETE CASCADE,
			role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
			content TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_eros_messages_conv ON eros_messages (conversation_id, created_at ASC)`,

		// ─── Surveys / Forms ──────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS surveys (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			slug TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft',
			welcome_title TEXT NOT NULL DEFAULT '',
			welcome_description TEXT NOT NULL DEFAULT '',
			thank_you_title TEXT NOT NULL DEFAULT '',
			thank_you_message TEXT NOT NULL DEFAULT '',
			thank_you_redirect_url TEXT NOT NULL DEFAULT '',
			branding JSONB NOT NULL DEFAULT '{}',
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uq_surveys_slug UNIQUE (slug)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_surveys_account ON surveys(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_surveys_slug ON surveys(slug)`,
		`CREATE TABLE IF NOT EXISTS survey_questions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			survey_id UUID NOT NULL REFERENCES surveys(id) ON DELETE CASCADE,
			order_index INTEGER NOT NULL DEFAULT 0,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			required BOOLEAN NOT NULL DEFAULT FALSE,
			config JSONB NOT NULL DEFAULT '{}',
			logic_rules JSONB NOT NULL DEFAULT '[]',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_questions_survey ON survey_questions(survey_id, order_index)`,
		`CREATE TABLE IF NOT EXISTS survey_responses (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			survey_id UUID NOT NULL REFERENCES surveys(id) ON DELETE CASCADE,
			account_id UUID NOT NULL,
			respondent_token TEXT NOT NULL DEFAULT '',
			lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
			source TEXT NOT NULL DEFAULT 'direct',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_responses_survey ON survey_responses(survey_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_responses_account ON survey_responses(account_id)`,
		`CREATE TABLE IF NOT EXISTS survey_answers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			response_id UUID NOT NULL REFERENCES survey_responses(id) ON DELETE CASCADE,
			question_id UUID NOT NULL REFERENCES survey_questions(id) ON DELETE CASCADE,
			value TEXT NOT NULL DEFAULT '',
			file_url TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_answers_response ON survey_answers(response_id)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_answers_question ON survey_answers(question_id)`,

		// Add surveys permission to Administrador role
		`UPDATE roles SET permissions = array_append(permissions, 'surveys') WHERE name = 'Administrador' AND NOT ('surveys' = ANY(permissions))`,

		// Add is_template column to surveys
		`ALTER TABLE surveys ADD COLUMN IF NOT EXISTS is_template BOOLEAN NOT NULL DEFAULT FALSE`,

		// Lead archive & block system
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS is_archived BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS is_blocked BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS blocked_at TIMESTAMPTZ`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS block_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS archive_reason TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_leads_archived ON leads(account_id) WHERE is_archived = true`,
		`CREATE INDEX IF NOT EXISTS idx_leads_blocked ON leads(account_id) WHERE is_blocked = true`,

		// Kommo deletion tracking
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS kommo_deleted_at TIMESTAMPTZ`,

		// Address field on contacts and leads (must be before data unification)
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS address TEXT`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS address TEXT`,

		// Distrito and Ocupacion fields on contacts and leads
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS distrito VARCHAR(255) DEFAULT ''`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS ocupacion VARCHAR(255) DEFAULT ''`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS avatar_checked_at TIMESTAMPTZ`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS distrito VARCHAR(255) DEFAULT ''`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS ocupacion VARCHAR(255) DEFAULT ''`,

		// Per-account Kommo integration flag
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS kommo_enabled BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS storage_limit_bytes BIGINT NOT NULL DEFAULT 0`,

		// Storage inventory/audit. V1 uses this as a deletion/audit ledger while
		// live usage is measured from MinIO to avoid drift on legacy objects.
		`CREATE TABLE IF NOT EXISTS storage_objects (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			object_key TEXT NOT NULL,
			media_type VARCHAR(50) NOT NULL DEFAULT 'other',
			content_type VARCHAR(255) NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			source VARCHAR(50) NOT NULL DEFAULT 'unknown',
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			deleted_at TIMESTAMPTZ,
			deleted_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(account_id, object_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_objects_account_status ON storage_objects(account_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_objects_account_type ON storage_objects(account_id, media_type)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_account_media_url ON messages(account_id, media_url) WHERE media_url IS NOT NULL AND media_url <> ''`,
		`CREATE TABLE IF NOT EXISTS media_assets (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			content_hash TEXT NOT NULL,
			object_key TEXT NOT NULL,
			media_type VARCHAR(50) NOT NULL DEFAULT 'other',
			content_type VARCHAR(255) NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMPTZ,
			UNIQUE(account_id, content_hash)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_media_assets_account_status ON media_assets(account_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_media_assets_object_key ON media_assets(object_key)`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_asset_id UUID REFERENCES media_assets(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_media_asset ON messages(media_asset_id) WHERE media_asset_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS storage_dedupe_jobs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			status VARCHAR(50) NOT NULL DEFAULT 'queued',
			total_objects BIGINT NOT NULL DEFAULT 0,
			processed_objects BIGINT NOT NULL DEFAULT 0,
			duplicates_found BIGINT NOT NULL DEFAULT 0,
			duplicates_deleted BIGINT NOT NULL DEFAULT 0,
			bytes_freed BIGINT NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_dedupe_jobs_account_created ON storage_dedupe_jobs(account_id, created_at DESC)`,

		// ─── Data Unification: Contact = source of truth ───────────────────

		// Index on leads.contact_id for JOIN performance
		`CREATE INDEX IF NOT EXISTS idx_leads_contact_id ON leads(contact_id) WHERE contact_id IS NOT NULL`,

		// Auto-create contacts for manual leads (no phone) that lack a contact_id
		`INSERT INTO contacts (id, account_id, jid, name, last_name, short_name, phone, email, company, age, dni, birth_date, address, distrito, ocupacion, notes, source, is_group, created_at, updated_at)
		 SELECT gen_random_uuid(), l.account_id, l.jid, l.name, l.last_name, l.short_name, l.phone, l.email, l.company, l.age, l.dni, l.birth_date, l.address, l.distrito, l.ocupacion, l.notes, l.source, false, l.created_at, NOW()
		 FROM leads l
		 WHERE l.contact_id IS NULL
		   AND NOT EXISTS (SELECT 1 FROM contacts c WHERE c.account_id = l.account_id AND c.jid = l.jid)
		 ON CONFLICT DO NOTHING`,

		// Link leads to their contacts (for newly created and pre-existing contacts)
		`UPDATE leads SET contact_id = c.id
		 FROM contacts c
		 WHERE leads.contact_id IS NULL
		   AND leads.account_id = c.account_id
		   AND leads.jid = c.jid`,

		// Merge personal data from leads into contacts (fill missing contact fields from lead)
		`UPDATE contacts SET
		   name = COALESCE(contacts.name, l.name),
		   last_name = COALESCE(contacts.last_name, l.last_name),
		   short_name = COALESCE(contacts.short_name, l.short_name),
		   phone = COALESCE(contacts.phone, l.phone),
		   email = COALESCE(contacts.email, l.email),
		   company = COALESCE(contacts.company, l.company),
		   age = COALESCE(contacts.age, l.age),
		   dni = COALESCE(contacts.dni, l.dni),
		   birth_date = COALESCE(contacts.birth_date, l.birth_date),
		   address = COALESCE(contacts.address, l.address),
		   distrito = COALESCE(NULLIF(contacts.distrito, ''), NULLIF(l.distrito, '')),
		   ocupacion = COALESCE(NULLIF(contacts.ocupacion, ''), NULLIF(l.ocupacion, '')),
		   notes = COALESCE(contacts.notes, l.notes),
		   updated_at = NOW()
		 FROM leads l
		 WHERE contacts.id = l.contact_id
		   AND l.contact_id IS NOT NULL`,

		// (lead_tags migration removed — table dropped, data already in contact_tags)

		// Migrate participant_tags → contact_tags (for participants that have a contact_id)
		`INSERT INTO contact_tags (contact_id, tag_id)
		 SELECT DISTINCT p.contact_id, pt.tag_id
		 FROM participant_tags pt
		 JOIN event_participants p ON p.id = pt.participant_id
		 WHERE p.contact_id IS NOT NULL
		 ON CONFLICT DO NOTHING`,

		// Index on contact_tags for frequent JOINs
		`CREATE INDEX IF NOT EXISTS idx_contact_tags_contact ON contact_tags(contact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_tags_tag ON contact_tags(tag_id)`,

		// Backfill contact_id on campaign_recipients via JID match
		`UPDATE campaign_recipients cr
		 SET contact_id = c.id
		 FROM contacts c
		 WHERE cr.contact_id IS NULL
		   AND c.jid = cr.jid`,

		// Drop obsolete lead_tags table (all tags now in contact_tags)
		`DROP TABLE IF EXISTS lead_tags`,

		// NULL out personal data in leads (Contact is source of truth via COALESCE)
		`UPDATE leads SET
		   name = NULL, last_name = NULL, short_name = NULL,
		   phone = NULL, email = NULL, company = NULL,
		   age = NULL, dni = NULL, birth_date = NULL, notes = NULL
		 WHERE contact_id IS NOT NULL
		   AND name IS NOT NULL`,

		// Program folders – organise programs in folders
		`CREATE TABLE IF NOT EXISTS program_folders (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			parent_id UUID REFERENCES program_folders(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			color VARCHAR(20) DEFAULT '#10b981',
			icon VARCHAR(50) DEFAULT '📁',
			position INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS folder_id UUID REFERENCES program_folders(id) ON DELETE SET NULL`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS receive_messages BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS provider VARCHAR(50) NOT NULL DEFAULT 'whatsapp_web'`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS waba_id VARCHAR(100)`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS phone_number_id VARCHAR(100)`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS api_display_phone VARCHAR(50)`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS api_webhook_status VARCHAR(50) NOT NULL DEFAULT 'not_configured'`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS api_billing_status VARCHAR(50) NOT NULL DEFAULT 'not_configured'`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS api_sending_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS api_templates_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`UPDATE devices SET provider = 'whatsapp_web' WHERE provider IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_devices_account_provider ON devices(account_id, provider)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_phone_number_id ON devices(phone_number_id) WHERE phone_number_id IS NOT NULL`,

		// Backfill contact_id for event_participants that have lead_id but missing contact_id
		`UPDATE event_participants SET contact_id = l.contact_id FROM leads l WHERE l.id = event_participants.lead_id AND event_participants.contact_id IS NULL AND l.contact_id IS NOT NULL`,

		// ─── Dynamics (Interactive Activities) ─────────────────────────────

		`CREATE TABLE IF NOT EXISTS dynamics (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			type TEXT NOT NULL DEFAULT 'scratch_card',
			name TEXT NOT NULL,
			slug TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			config JSONB NOT NULL DEFAULT '{}',
			is_active BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uq_dynamics_slug UNIQUE (slug)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamics_account ON dynamics(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamics_slug ON dynamics(slug)`,

		`CREATE TABLE IF NOT EXISTS dynamic_items (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			dynamic_id UUID NOT NULL REFERENCES dynamics(id) ON DELETE CASCADE,
			image_url TEXT NOT NULL DEFAULT '',
			thought_text TEXT NOT NULL DEFAULT '',
			author TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_items_dynamic ON dynamic_items(dynamic_id, sort_order)`,

		// Migrate v1/v2 columns
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='dynamic_items' AND column_name='content') THEN
				ALTER TABLE dynamic_items DROP COLUMN content;
			END IF;
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='dynamic_items' AND column_name='caption') THEN
				ALTER TABLE dynamic_items RENAME COLUMN caption TO thought_text;
			END IF;
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='dynamic_items' AND column_name='thought_text') THEN
				ALTER TABLE dynamic_items ADD COLUMN thought_text TEXT NOT NULL DEFAULT '';
			END IF;
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='dynamic_items' AND column_name='author') THEN
				ALTER TABLE dynamic_items ADD COLUMN author TEXT NOT NULL DEFAULT '';
			END IF;
		END $$`,

		// Add file_size column to dynamic_items
		`ALTER TABLE dynamic_items ADD COLUMN IF NOT EXISTS file_size BIGINT NOT NULL DEFAULT 0`,

		// Add dynamics permission to Administrador role
		`UPDATE roles SET permissions = array_append(permissions, 'dynamics') WHERE name = 'Administrador' AND NOT ('dynamics' = ANY(permissions))`,

		// ─── Dynamic Options (categories for scratch card items) ──────────
		`CREATE TABLE IF NOT EXISTS dynamic_options (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			dynamic_id UUID NOT NULL REFERENCES dynamics(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT '',
			emoji TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_options_dynamic ON dynamic_options(dynamic_id, sort_order)`,

		// ─── Dynamic Links (multiple public URLs per dynamic) ─────────────
		`CREATE TABLE IF NOT EXISTS dynamic_links (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			dynamic_id UUID NOT NULL REFERENCES dynamics(id) ON DELETE CASCADE,
			slug TEXT NOT NULL,
			whatsapp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			whatsapp_message TEXT NOT NULL DEFAULT '',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uq_dynamic_links_slug UNIQUE (slug)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_links_dynamic ON dynamic_links(dynamic_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_links_slug ON dynamic_links(slug)`,

		// ─── Dynamic WhatsApp Queue (persistent queue for sending images) ─
		`CREATE TABLE IF NOT EXISTS dynamic_whatsapp_queue (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			dynamic_id UUID NOT NULL REFERENCES dynamics(id) ON DELETE CASCADE,
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			link_id UUID NOT NULL REFERENCES dynamic_links(id) ON DELETE CASCADE,
			phone TEXT NOT NULL,
			item_id UUID NOT NULL REFERENCES dynamic_items(id) ON DELETE CASCADE,
			image_url TEXT NOT NULL DEFAULT '',
			caption TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			error_msg TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			sent_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_wa_queue_status ON dynamic_whatsapp_queue(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_wa_queue_account ON dynamic_whatsapp_queue(account_id, status)`,

		// Add option_id column to dynamic_items (nullable FK) — legacy, kept for migration
		`ALTER TABLE dynamic_items ADD COLUMN IF NOT EXISTS option_id UUID REFERENCES dynamic_options(id) ON DELETE SET NULL`,

		// ─── Many-to-many: items ↔ options junction table ───────────────
		`CREATE TABLE IF NOT EXISTS dynamic_item_options (
			item_id UUID NOT NULL REFERENCES dynamic_items(id) ON DELETE CASCADE,
			option_id UUID NOT NULL REFERENCES dynamic_options(id) ON DELETE CASCADE,
			PRIMARY KEY (item_id, option_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_item_options_option ON dynamic_item_options(option_id)`,

		// Migrate data from legacy option_id column into junction table
		`INSERT INTO dynamic_item_options (item_id, option_id)
		 SELECT id, option_id FROM dynamic_items WHERE option_id IS NOT NULL
		 ON CONFLICT DO NOTHING`,

		// Add tipo column to dynamic_items
		`ALTER TABLE dynamic_items ADD COLUMN IF NOT EXISTS tipo TEXT NOT NULL DEFAULT ''`,

		// Add extra message fields to dynamic_links
		`ALTER TABLE dynamic_links ADD COLUMN IF NOT EXISTS extra_message_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_links ADD COLUMN IF NOT EXISTS extra_message_media_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_links ADD COLUMN IF NOT EXISTS extra_message_media_type TEXT NOT NULL DEFAULT ''`,

		// Add extra message fields to dynamic_whatsapp_queue
		`ALTER TABLE dynamic_whatsapp_queue ADD COLUMN IF NOT EXISTS extra_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_whatsapp_queue ADD COLUMN IF NOT EXISTS extra_media_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_whatsapp_queue ADD COLUMN IF NOT EXISTS extra_media_type TEXT NOT NULL DEFAULT ''`,

		// Add start/end dates to dynamic_links
		`ALTER TABLE dynamic_links ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ`,
		`ALTER TABLE dynamic_links ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ`,

		// ─── Dynamic Link Registrations ─────────────────────────────────
		`CREATE TABLE IF NOT EXISTS dynamic_link_registrations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			link_id UUID NOT NULL REFERENCES dynamic_links(id) ON DELETE CASCADE,
			full_name TEXT NOT NULL,
			phone TEXT NOT NULL,
			age INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uq_link_phone UNIQUE (link_id, phone)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_link_registrations_link ON dynamic_link_registrations(link_id, created_at)`,

		// ─── Dynamic Link: multi-image extras + registration tracking (2026-04) ─
		// Make age nullable (no longer required)
		`ALTER TABLE dynamic_link_registrations ALTER COLUMN age DROP NOT NULL`,
		`ALTER TABLE dynamic_link_registrations ALTER COLUMN age DROP DEFAULT`,
		// Link registrations to global contact/lead + track WA send status
		`ALTER TABLE dynamic_link_registrations ADD COLUMN IF NOT EXISTS contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL`,
		`ALTER TABLE dynamic_link_registrations ADD COLUMN IF NOT EXISTS lead_id UUID REFERENCES leads(id) ON DELETE SET NULL`,
		`ALTER TABLE dynamic_link_registrations ADD COLUMN IF NOT EXISTS whatsapp_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_link_registrations ADD COLUMN IF NOT EXISTS whatsapp_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_link_registrations ADD COLUMN IF NOT EXISTS session_token TEXT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_dynamic_link_reg_session_token ON dynamic_link_registrations(session_token) WHERE session_token IS NOT NULL`,

		// ─── Share feature (2026-04) ───────────────────────────────────
		// When set, the registration was created as a "share" by another
		// registration (the sharer). NULL = self-registration.
		`ALTER TABLE dynamic_link_registrations ADD COLUMN IF NOT EXISTS shared_by_registration_id UUID REFERENCES dynamic_link_registrations(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_link_reg_shared_by ON dynamic_link_registrations(shared_by_registration_id) WHERE shared_by_registration_id IS NOT NULL`,
		// Replace the strict (link_id, phone) unique constraint with a partial
		// one that only applies to self-registrations. This allows the same
		// phone to receive multiple shared messages.
		`ALTER TABLE dynamic_link_registrations DROP CONSTRAINT IF EXISTS uq_link_phone`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_link_phone_self ON dynamic_link_registrations(link_id, phone) WHERE shared_by_registration_id IS NULL`,

		// Multi-image extras per link (up to 10, each with optional caption)
		`CREATE TABLE IF NOT EXISTS dynamic_link_extra_media (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			link_id UUID NOT NULL REFERENCES dynamic_links(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			media_type TEXT NOT NULL DEFAULT 'image',
			caption TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_link_extra_media_link ON dynamic_link_extra_media(link_id, sort_order)`,

		// One-shot migration of legacy single-slot extra to the new table
		`INSERT INTO dynamic_link_extra_media (link_id, url, media_type, caption, sort_order)
		 SELECT dl.id, dl.extra_message_media_url,
		        COALESCE(NULLIF(dl.extra_message_media_type,''), 'image'),
		        COALESCE(dl.extra_message_text,''), 0
		 FROM dynamic_links dl
		 WHERE dl.extra_message_media_url <> ''
		   AND NOT EXISTS (
		     SELECT 1 FROM dynamic_link_extra_media dlem WHERE dlem.link_id = dl.id
		   )`,

		// Migrate existing dynamics: create a dynamic_link for each dynamic using its slug
		`INSERT INTO dynamic_links (dynamic_id, slug, is_active)
		 SELECT d.id, d.slug, d.is_active FROM dynamics d
		 WHERE NOT EXISTS (SELECT 1 FROM dynamic_links dl WHERE dl.dynamic_id = d.id)`,

		// ─── Backfill: link orphaned interactions to their lead's contact ──
		`UPDATE interactions i SET contact_id = l.contact_id
		 FROM leads l
		 WHERE i.lead_id = l.id
		   AND i.contact_id IS NULL
		   AND l.contact_id IS NOT NULL`,

		// ─── Google Contacts integration ──
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS google_email TEXT`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS google_access_token TEXT`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS google_refresh_token TEXT`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS google_contact_group_id TEXT`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS google_connected_at TIMESTAMPTZ`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS google_sync_limit INT DEFAULT 20000`,

		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS google_sync BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS google_resource_name TEXT`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS google_synced_at TIMESTAMPTZ`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS google_sync_error TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_google_sync ON contacts(account_id) WHERE google_sync = TRUE`,

		`CREATE TABLE IF NOT EXISTS contact_phones (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
			phone TEXT NOT NULL,
			label TEXT DEFAULT 'mobile',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_phones_contact_id ON contact_phones(contact_id)`,

		// ─── Tasks & Reminders ──
		`CREATE TABLE IF NOT EXISTS tasks (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			assigned_to UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			type TEXT NOT NULL DEFAULT 'reminder',
			due_at TIMESTAMPTZ NOT NULL,
			due_end_at TIMESTAMPTZ,
			priority TEXT NOT NULL DEFAULT 'medium',
			status TEXT NOT NULL DEFAULT 'pending',
			completed_at TIMESTAMPTZ,
			completed_by UUID REFERENCES users(id),
			lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
			event_id UUID REFERENCES events(id) ON DELETE SET NULL,
			program_id UUID REFERENCES programs(id) ON DELETE SET NULL,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			recurrence_rule TEXT DEFAULT '',
			recurrence_parent_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
			reminder_minutes INT,
			notes TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_account_assigned_status ON tasks(account_id, assigned_to, status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_account_due_at ON tasks(account_id, due_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_lead_id ON tasks(lead_id) WHERE lead_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_event_id ON tasks(event_id) WHERE event_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_program_id ON tasks(program_id) WHERE program_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_contact_id ON tasks(contact_id) WHERE contact_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_recurrence_parent ON tasks(recurrence_parent_id) WHERE recurrence_parent_id IS NOT NULL`,

		`CREATE TABLE IF NOT EXISTS task_reminders (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			assigned_to UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			reminder_at TIMESTAMPTZ NOT NULL,
			delivered BOOLEAN DEFAULT FALSE,
			delivered_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_reminders_pending ON task_reminders(reminder_at) WHERE delivered = FALSE`,

		// ─── Make due_at optional ──
		`ALTER TABLE tasks ALTER COLUMN due_at DROP NOT NULL`,

		// ─── Subtasks ──
		`CREATE TABLE IF NOT EXISTS subtasks (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			title TEXT NOT NULL DEFAULT '',
			completed BOOLEAN DEFAULT FALSE,
			completed_at TIMESTAMPTZ,
			sort_order INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subtasks_task_id ON subtasks(task_id)`,

		// ─── Performance indexes ──
		`CREATE INDEX IF NOT EXISTS idx_tasks_account ON tasks(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_lead ON tasks(lead_id) WHERE lead_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_contact ON tasks(contact_id) WHERE contact_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_events_account ON events(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_event_tags_event ON event_tags(event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contact_tags_contact ON contact_tags(contact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pipeline_stages_pipeline ON pipeline_stages(pipeline_id)`,

		// ─── Backfill: auto-link contact_id for tasks with lead_id ──
		`UPDATE tasks SET contact_id = l.contact_id FROM leads l WHERE tasks.lead_id = l.id AND tasks.contact_id IS NULL AND l.contact_id IS NOT NULL`,

		// ─── Document Templates ──
		`CREATE TABLE IF NOT EXISTS document_templates (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			canvas_json JSONB NOT NULL DEFAULT '{}',
			thumbnail_url TEXT NOT NULL DEFAULT '',
			page_width DOUBLE PRECISION NOT NULL DEFAULT 210,
			page_height DOUBLE PRECISION NOT NULL DEFAULT 297,
			page_orientation TEXT NOT NULL DEFAULT 'portrait',
			fields_used TEXT[] NOT NULL DEFAULT '{}',
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_document_templates_account ON document_templates(account_id)`,
		// Sync monitor: persistent log entries (24h retention, auto-cleanup)
		`CREATE TABLE IF NOT EXISTS sync_monitor_entries (
			id BIGSERIAL PRIMARY KEY,
			source TEXT NOT NULL,
			message TEXT NOT NULL,
			level TEXT NOT NULL DEFAULT 'info',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS integration_instance_id UUID REFERENCES integration_instances(id) ON DELETE SET NULL`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS account_id UUID REFERENCES accounts(id) ON DELETE SET NULL`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS entity_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS entity_id UUID`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS kommo_entity_id BIGINT`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS operation TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS duration_ms BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS request_count INT NOT NULL DEFAULT 0`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS batch_size INT NOT NULL DEFAULT 0`,
		`ALTER TABLE sync_monitor_entries ADD COLUMN IF NOT EXISTS details JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_created_at ON sync_monitor_entries(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_source ON sync_monitor_entries(source)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_instance_created ON sync_monitor_entries(integration_instance_id, created_at DESC) WHERE integration_instance_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_instance_source_created ON sync_monitor_entries(integration_instance_id, source, created_at DESC) WHERE integration_instance_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_account_created ON sync_monitor_entries(account_id, created_at DESC) WHERE account_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_status_created ON sync_monitor_entries(status, created_at DESC) WHERE status <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_sync_monitor_details_gin ON sync_monitor_entries USING GIN(details)`,

		`CREATE TABLE IF NOT EXISTS csv_import_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			uploaded_by UUID,
			import_type VARCHAR(32) NOT NULL DEFAULT 'leads',
			source VARCHAR(50) NOT NULL DEFAULT 'csv',
			file_name TEXT NOT NULL DEFAULT '',
			total_rows INT NOT NULL DEFAULT 0,
			created_count INT NOT NULL DEFAULT 0,
			updated_count INT NOT NULL DEFAULT 0,
			existing_count INT NOT NULL DEFAULT 0,
			skipped_count INT NOT NULL DEFAULT 0,
			duplicate_count INT NOT NULL DEFAULT 0,
			error_count INT NOT NULL DEFAULT 0,
			new_contacts_count INT NOT NULL DEFAULT 0,
			details JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_csv_import_logs_account_created ON csv_import_logs(account_id, created_at DESC)`,

		// Three-way merge baseline for tag sync (Clarin ↔ Kommo)
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS kommo_synced_tags TEXT[] DEFAULT '{}'`,
		// Bootstrap: initialize baseline from current tags for existing Kommo-linked leads
		`UPDATE leads SET kommo_synced_tags = COALESCE(tags, '{}') WHERE kommo_id IS NOT NULL AND (kommo_synced_tags IS NULL OR kommo_synced_tags = '{}')`,

		// ── Event participant personal data fields ─────────────────────
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS company VARCHAR(255)`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS dni VARCHAR(50)`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS birth_date DATE`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS address TEXT`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS distrito VARCHAR(255)`,
		`ALTER TABLE event_participants ADD COLUMN IF NOT EXISTS ocupacion VARCHAR(255)`,

		// ── Custom field definitions ─────────────────────
		`CREATE TABLE IF NOT EXISTS custom_field_definitions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			slug VARCHAR(255) NOT NULL,
			field_type VARCHAR(50) NOT NULL CHECK(field_type IN ('text','number','date','select','multi_select','checkbox','email','phone','url','currency')),
			config JSONB NOT NULL DEFAULT '{}',
			is_required BOOLEAN NOT NULL DEFAULT FALSE,
			default_value TEXT,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(account_id, slug)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cfd_account ON custom_field_definitions(account_id, sort_order)`,

		// ── Custom field values ─────────────────────
		`CREATE TABLE IF NOT EXISTS custom_field_values (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			field_id UUID NOT NULL REFERENCES custom_field_definitions(id) ON DELETE CASCADE,
			contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
			value_text TEXT,
			value_number NUMERIC(18,4),
			value_date TIMESTAMPTZ,
			value_bool BOOLEAN,
			value_json JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(field_id, contact_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cfv_contact ON custom_field_values(contact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cfv_field ON custom_field_values(field_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cfv_text ON custom_field_values(field_id, value_text) WHERE value_text IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_cfv_number ON custom_field_values(field_id, value_number) WHERE value_number IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_cfv_date ON custom_field_values(field_id, value_date) WHERE value_date IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_cfv_bool ON custom_field_values(field_id, value_bool) WHERE value_bool IS NOT NULL`,

		// ─── Programs: dual type (course | event) ──────────────────────────
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS type VARCHAR(20) NOT NULL DEFAULT 'course'`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS pipeline_id UUID REFERENCES event_pipelines(id) ON DELETE SET NULL`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS tag_formula TEXT DEFAULT ''`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS tag_formula_mode VARCHAR(10) DEFAULT 'OR'`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS tag_formula_type VARCHAR(20) DEFAULT 'simple'`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS event_date TIMESTAMPTZ`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS event_end TIMESTAMPTZ`,
		`ALTER TABLE programs ADD COLUMN IF NOT EXISTS location TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_programs_type ON programs(type)`,
		`ALTER TABLE program_participants ADD COLUMN IF NOT EXISTS stage_id UUID REFERENCES event_pipeline_stages(id) ON DELETE SET NULL`,
		`ALTER TABLE program_participants ADD COLUMN IF NOT EXISTS auto_tag_sync BOOLEAN DEFAULT FALSE`,
		`CREATE INDEX IF NOT EXISTS idx_program_participants_stage ON program_participants(stage_id)`,

		// ─── Kommo Push Outbox: batched, coalesced push worker ─────────────
		// Enables bulk PATCH to Kommo (up to 250 items/req) with coalescing
		// by (entity_id, operation) via unique partial index.
		`CREATE TABLE IF NOT EXISTS kommo_push_outbox (
			id UUID PRIMARY KEY,
			integration_instance_id UUID REFERENCES integration_instances(id) ON DELETE SET NULL,
			account_id UUID NOT NULL,
			operation TEXT NOT NULL,
			entity_id UUID NOT NULL,
			kommo_entity_id BIGINT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			enqueued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			processing_started_at TIMESTAMPTZ,
			attempts INT NOT NULL DEFAULT 0,
			last_error TEXT
		)`,
		`ALTER TABLE kommo_push_outbox ADD COLUMN IF NOT EXISTS integration_instance_id UUID REFERENCES integration_instances(id) ON DELETE SET NULL`,
		`DROP INDEX IF EXISTS uq_kommo_outbox_pending`,
		`CREATE INDEX IF NOT EXISTS idx_kommo_outbox_instance_pending ON kommo_push_outbox(integration_instance_id, operation, enqueued_at) WHERE processing_started_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_kommo_outbox_pending ON kommo_push_outbox(operation, enqueued_at) WHERE processing_started_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_kommo_outbox_processing ON kommo_push_outbox(processing_started_at) WHERE processing_started_at IS NOT NULL`,

		// ─── WhatsApp Cloud API: safe infrastructure, no paid sends by default ──
		`ALTER TABLE chats ADD COLUMN IF NOT EXISTS last_inbound_at TIMESTAMPTZ`,
		`ALTER TABLE chats ADD COLUMN IF NOT EXISTS last_outbound_at TIMESTAMPTZ`,
		`ALTER TABLE chats ADD COLUMN IF NOT EXISTS customer_service_window_expires_at TIMESTAMPTZ`,
		`ALTER TABLE chats ADD COLUMN IF NOT EXISTS last_message_provider VARCHAR(50) NOT NULL DEFAULT 'whatsapp_web'`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS provider VARCHAR(50) NOT NULL DEFAULT 'whatsapp_web'`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS template_name VARCHAR(255)`,
		`CREATE INDEX IF NOT EXISTS idx_chats_service_window ON chats(account_id, customer_service_window_expires_at) WHERE customer_service_window_expires_at IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_provider ON messages(account_id, provider)`,

		`CREATE TABLE IF NOT EXISTS whatsapp_message_templates (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
			name VARCHAR(255) NOT NULL,
			language VARCHAR(20) NOT NULL DEFAULT 'es',
			category VARCHAR(50) NOT NULL DEFAULT 'UTILITY',
			status VARCHAR(50) NOT NULL DEFAULT 'draft',
			components JSONB NOT NULL DEFAULT '[]'::jsonb,
			meta_template_id VARCHAR(255),
			rejection_reason TEXT,
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(account_id, name, language)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_templates_account_status ON whatsapp_message_templates(account_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_templates_device ON whatsapp_message_templates(device_id) WHERE device_id IS NOT NULL`,

		`CREATE TABLE IF NOT EXISTS whatsapp_webhook_events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID REFERENCES accounts(id) ON DELETE SET NULL,
			device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
			phone_number_id VARCHAR(100) NOT NULL DEFAULT '',
			event_id VARCHAR(255) NOT NULL,
			event_type VARCHAR(100) NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			processed BOOLEAN NOT NULL DEFAULT FALSE,
			error_message TEXT,
			received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_webhook_events_account ON whatsapp_webhook_events(account_id, received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_webhook_events_device ON whatsapp_webhook_events(device_id, received_at DESC) WHERE device_id IS NOT NULL`,

		`CREATE TABLE IF NOT EXISTS bot_flows (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			channel VARCHAR(50) NOT NULL DEFAULT 'whatsapp',
			trigger_type VARCHAR(100) NOT NULL DEFAULT 'message_received',
			trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb,
			graph JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}'::jsonb,
			is_active BOOLEAN NOT NULL DEFAULT FALSE,
			is_published BOOLEAN NOT NULL DEFAULT FALSE,
			draft_version INT NOT NULL DEFAULT 1,
			published_version INT NOT NULL DEFAULT 0,
			execution_count INT NOT NULL DEFAULT 0,
			last_triggered_at TIMESTAMPTZ,
			published_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_flows_account ON bot_flows(account_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_flows_active ON bot_flows(account_id, channel, trigger_type) WHERE is_active = TRUE AND is_published = TRUE`,

		`CREATE TABLE IF NOT EXISTS bot_flow_versions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			flow_id UUID NOT NULL REFERENCES bot_flows(id) ON DELETE CASCADE,
			version INT NOT NULL,
			graph JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(flow_id, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_flow_versions_flow ON bot_flow_versions(flow_id, version DESC)`,

		`CREATE TABLE IF NOT EXISTS bot_sessions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			flow_id UUID NOT NULL REFERENCES bot_flows(id) ON DELETE CASCADE,
			chat_id UUID REFERENCES chats(id) ON DELETE SET NULL,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			current_node_id VARCHAR(255) NOT NULL DEFAULT '',
			context_data JSONB NOT NULL DEFAULT '{}'::jsonb,
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ended_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_sessions_account_status ON bot_sessions(account_id, status, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_sessions_chat ON bot_sessions(chat_id) WHERE chat_id IS NOT NULL`,

		`CREATE TABLE IF NOT EXISTS bot_execution_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			flow_id UUID NOT NULL REFERENCES bot_flows(id) ON DELETE CASCADE,
			session_id UUID REFERENCES bot_sessions(id) ON DELETE SET NULL,
			node_id VARCHAR(255) NOT NULL DEFAULT '',
			node_type VARCHAR(100) NOT NULL DEFAULT '',
			status VARCHAR(50) NOT NULL DEFAULT 'success',
			input JSONB NOT NULL DEFAULT '{}'::jsonb,
			output JSONB NOT NULL DEFAULT '{}'::jsonb,
			error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_execution_logs_flow ON bot_execution_logs(flow_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_execution_logs_account ON bot_execution_logs(account_id, created_at DESC)`,

		`CREATE TABLE IF NOT EXISTS plans (
			code VARCHAR(50) PRIMARY KEY,
			name VARCHAR(120) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			trial_days INT NOT NULL DEFAULT 0,
			is_public BOOLEAN NOT NULL DEFAULT TRUE,
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS plan_entitlements (
			plan_code VARCHAR(50) NOT NULL REFERENCES plans(code) ON DELETE CASCADE,
			key VARCHAR(100) NOT NULL,
			value_json JSONB NOT NULL DEFAULT 'null'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (plan_code, key)
		)`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			plan_code VARCHAR(50) NOT NULL REFERENCES plans(code),
			status VARCHAR(40) NOT NULL DEFAULT 'active',
			trial_started_at TIMESTAMPTZ,
			trial_ends_at TIMESTAMPTZ,
			current_period_start TIMESTAMPTZ,
			current_period_end TIMESTAMPTZ,
			grace_ends_at TIMESTAMPTZ,
			canceled_at TIMESTAMPTZ,
			suspended_at TIMESTAMPTZ,
			billing_provider VARCHAR(50) NOT NULL DEFAULT '',
			provider_customer_id VARCHAR(255) NOT NULL DEFAULT '',
			provider_subscription_id VARCHAR(255) NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(account_id),
			CHECK (status IN ('trialing', 'active', 'past_due', 'grace', 'suspended', 'canceled', 'incomplete'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_subscriptions_period_end ON subscriptions(current_period_end) WHERE current_period_end IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS security_events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			type VARCHAR(80) NOT NULL,
			account_id UUID REFERENCES accounts(id) ON DELETE SET NULL,
			user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			subject_hash VARCHAR(64) NOT NULL DEFAULT '',
			ip_hash VARCHAR(64) NOT NULL DEFAULT '',
			user_agent_hash VARCHAR(64) NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_security_events_account_created ON security_events(account_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_security_events_user_created ON security_events(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_security_events_type_created ON security_events(type, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_security_events_ip_created ON security_events(ip_hash, created_at DESC)`,
		`INSERT INTO plans (code, name, description, trial_days, is_public, sort_order)
		VALUES
			('free', 'Free', 'Plan gratuito interno para pruebas controladas.', 0, FALSE, 5),
			('trial', 'Trial', 'Prueba comercial para cuentas nuevas.', 14, TRUE, 10),
			('basic', 'Basic', 'Operación inicial con límites controlados.', 0, TRUE, 20),
			('starter', 'Starter', 'Plan de entrada para equipos pequeños.', 14, TRUE, 30),
			('pro', 'Pro', 'Plan para equipos en crecimiento.', 14, TRUE, 40),
			('business', 'Business', 'Plan avanzado con más capacidad operativa.', 14, TRUE, 50),
			('enterprise', 'Enterprise', 'Plan corporativo con límites amplios y soporte prioritario.', 0, TRUE, 60),
			('internal', 'Internal', 'Plan administrativo para cuentas existentes o especiales.', 0, FALSE, 70)
		ON CONFLICT (code) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			trial_days = EXCLUDED.trial_days,
			is_public = EXCLUDED.is_public,
			sort_order = EXCLUDED.sort_order,
			updated_at = NOW()`,
		`INSERT INTO plan_entitlements (plan_code, key, value_json)
		VALUES
			('free', 'max_users', '1'::jsonb), ('free', 'max_devices', '1'::jsonb), ('free', 'max_contacts', '500'::jsonb), ('free', 'kommo_sync', 'false'::jsonb), ('free', 'google_contacts', 'false'::jsonb),
			('trial', 'max_users', '3'::jsonb), ('trial', 'max_devices', '2'::jsonb), ('trial', 'max_contacts', '2000'::jsonb), ('trial', 'kommo_sync', 'true'::jsonb), ('trial', 'google_contacts', 'true'::jsonb),
			('basic', 'max_users', '3'::jsonb), ('basic', 'max_devices', '2'::jsonb), ('basic', 'max_contacts', '5000'::jsonb), ('basic', 'kommo_sync', 'true'::jsonb), ('basic', 'google_contacts', 'true'::jsonb),
			('starter', 'max_users', '5'::jsonb), ('starter', 'max_devices', '3'::jsonb), ('starter', 'max_contacts', '10000'::jsonb), ('starter', 'kommo_sync', 'true'::jsonb), ('starter', 'google_contacts', 'true'::jsonb),
			('pro', 'max_users', '12'::jsonb), ('pro', 'max_devices', '8'::jsonb), ('pro', 'max_contacts', '50000'::jsonb), ('pro', 'kommo_sync', 'true'::jsonb), ('pro', 'google_contacts', 'true'::jsonb), ('pro', 'broadcasts', 'true'::jsonb),
			('business', 'max_users', '30'::jsonb), ('business', 'max_devices', '20'::jsonb), ('business', 'max_contacts', '150000'::jsonb), ('business', 'kommo_sync', 'true'::jsonb), ('business', 'google_contacts', 'true'::jsonb), ('business', 'broadcasts', 'true'::jsonb), ('business', 'automations', 'true'::jsonb),
			('enterprise', 'max_users', '250'::jsonb), ('enterprise', 'max_devices', '100'::jsonb), ('enterprise', 'max_contacts', '1000000'::jsonb), ('enterprise', 'kommo_sync', 'true'::jsonb), ('enterprise', 'google_contacts', 'true'::jsonb), ('enterprise', 'broadcasts', 'true'::jsonb), ('enterprise', 'automations', 'true'::jsonb), ('enterprise', 'priority_support', 'true'::jsonb),
			('internal', 'max_users', '1000'::jsonb), ('internal', 'max_devices', '1000'::jsonb), ('internal', 'max_contacts', '10000000'::jsonb), ('internal', 'kommo_sync', 'true'::jsonb), ('internal', 'google_contacts', 'true'::jsonb), ('internal', 'broadcasts', 'true'::jsonb), ('internal', 'automations', 'true'::jsonb)
		ON CONFLICT (plan_code, key) DO UPDATE SET value_json = EXCLUDED.value_json, updated_at = NOW()`,
		`INSERT INTO subscriptions (account_id, plan_code, status, current_period_start, current_period_end, metadata)
		SELECT
			a.id,
			CASE
				WHEN COALESCE(NULLIF(a.plan, ''), 'enterprise') IN ('free', 'trial', 'basic', 'starter', 'pro', 'business', 'enterprise', 'internal') THEN COALESCE(NULLIF(a.plan, ''), 'enterprise')
				ELSE 'enterprise'
			END,
			'active',
			COALESCE(a.created_at, NOW()),
			NOW() + INTERVAL '100 years',
			jsonb_build_object('source', 'legacy_backfill', 'preserve_existing_integrations', true)
		FROM accounts a
		ON CONFLICT (account_id) DO NOTHING`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(ctx, migration); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, migration)
		}
	}

	return nil
}

func SeedAdmin(db *pgxpool.Pool, cfg *config.Config) error {
	ctx := context.Background()

	// Check if admin exists
	var count int
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE username = $1", cfg.AdminUser).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check admin existence: %w", err)
	}

	if count > 0 {
		return nil // Admin already exists
	}

	// Create default account
	var accountID string
	err = db.QueryRow(ctx, `
		INSERT INTO accounts (name, plan, max_devices)
		VALUES ('Default Account', 'enterprise', 200)
		ON CONFLICT DO NOTHING
		RETURNING id
	`).Scan(&accountID)
	if err != nil {
		// Try to get existing account
		err = db.QueryRow(ctx, "SELECT id FROM accounts WHERE name = 'Default Account'").Scan(&accountID)
		if err != nil {
			return fmt.Errorf("failed to create/get default account: %w", err)
		}
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Create or update admin user (super_admin)
	_, err = db.Exec(ctx, `
		INSERT INTO users (account_id, username, email, password_hash, display_name, is_admin, is_super_admin, role)
		VALUES ($1, $2, $3, $4, 'Administrador', TRUE, TRUE, 'super_admin')
		ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash, account_id = EXCLUDED.account_id, is_super_admin = TRUE, role = 'super_admin'
	`, accountID, cfg.AdminUser, cfg.AdminEmail, string(hashedPassword))
	if err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	// Create default pipeline (idempotent)
	var pipelineID string
	err = db.QueryRow(ctx, `
		SELECT id FROM pipelines WHERE account_id = $1 AND is_default = TRUE LIMIT 1
	`, accountID).Scan(&pipelineID)
	if err != nil {
		// No default pipeline exists, create one
		err = db.QueryRow(ctx, `
			INSERT INTO pipelines (account_id, name, description, is_default)
			VALUES ($1, 'Pipeline Principal', 'Pipeline por defecto para leads', TRUE)
			RETURNING id
		`, accountID).Scan(&pipelineID)
		if err != nil {
			return fmt.Errorf("failed to create default pipeline: %w", err)
		}

		// Create default stages
		stages := []struct {
			name  string
			color string
		}{
			{"Nuevo", "#6366f1"},
			{"Contactado", "#f59e0b"},
			{"En Negociación", "#3b82f6"},
			{"Propuesta", "#8b5cf6"},
			{"Cerrado", "#10b981"},
			{"Perdido", "#ef4444"},
		}

		for i, stage := range stages {
			_, err = db.Exec(ctx, `
				INSERT INTO pipeline_stages (pipeline_id, name, color, position)
				VALUES ($1, $2, $3, $4)
			`, pipelineID, stage.name, stage.color, i)
			if err != nil {
				return fmt.Errorf("failed to create stage %s: %w", stage.name, err)
			}
		}
	}

	// Assign existing leads without pipeline to the default pipeline's first stage
	var firstStageID string
	err = db.QueryRow(ctx, `
		SELECT id FROM pipeline_stages WHERE pipeline_id = $1 ORDER BY position LIMIT 1
	`, pipelineID).Scan(&firstStageID)
	if err == nil {
		_, _ = db.Exec(ctx, `
			UPDATE leads SET pipeline_id = $1, stage_id = $2
			WHERE account_id = $3 AND pipeline_id IS NULL
		`, pipelineID, firstStageID, accountID)
	}

	// ─── API Keys table for MCP / external integrations ───
	_, _ = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS api_keys (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT '',
			key_hash TEXT NOT NULL,
			key_prefix TEXT NOT NULL DEFAULT '',
			permissions TEXT NOT NULL DEFAULT 'read',
			is_active BOOLEAN NOT NULL DEFAULT true,
			last_used_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_api_keys_account_id ON api_keys(account_id)`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash)`)

	// ─── Automations ──────────────────────────────────────────────────────────
	_, _ = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS automations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			trigger_type TEXT NOT NULL,
			trigger_config JSONB NOT NULL DEFAULT '{}',
			config JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			execution_count INTEGER NOT NULL DEFAULT 0,
			last_triggered_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_automations_account ON automations(account_id)`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_automations_trigger ON automations(account_id, trigger_type) WHERE is_active = TRUE`)

	_, _ = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS automation_executions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
			account_id UUID NOT NULL,
			lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			current_node_id TEXT NOT NULL DEFAULT '',
			next_node_id TEXT NOT NULL DEFAULT '',
			resume_at TIMESTAMPTZ,
			config_snapshot JSONB,
			context_data JSONB NOT NULL DEFAULT '{}',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_auto_exec_paused ON automation_executions(resume_at) WHERE status = 'paused'`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_auto_exec_dedup ON automation_executions(automation_id, lead_id, created_at) WHERE status IN ('pending','running','paused')`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_auto_exec_account ON automation_executions(account_id, created_at DESC)`)

	_, _ = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS automation_execution_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			execution_id UUID NOT NULL REFERENCES automation_executions(id) ON DELETE CASCADE,
			node_id TEXT NOT NULL,
			node_type TEXT NOT NULL,
			status TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_auto_exec_logs ON automation_execution_logs(execution_id, created_at)`)

	// ── Task Lists ──
	_, _ = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS task_lists (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			color TEXT DEFAULT '',
			sort_order INT DEFAULT 0,
			created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_task_lists_account_id ON task_lists(account_id)`)
	_, _ = db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS list_id UUID REFERENCES task_lists(id) ON DELETE SET NULL`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_list_id ON tasks(list_id) WHERE list_id IS NOT NULL`)

	// ── Task starred + sort_order ──
	_, _ = db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS starred BOOLEAN DEFAULT FALSE`)
	_, _ = db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS sort_order INT DEFAULT 0`)
	_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_starred ON tasks(account_id, starred) WHERE starred = TRUE`)

	return nil
}

// MigrateEventPipelines ensures every account has a default event pipeline with
// the canonical stages, then assigns existing events that still have no pipeline.
func MigrateEventPipelines(db *pgxpool.Pool) error {
	ctx := context.Background()

	rows, err := db.Query(ctx, `SELECT id FROM accounts`)
	if err != nil {
		return fmt.Errorf("failed to list accounts for event pipelines: %w", err)
	}
	defer rows.Close()

	var accountIDs []string
	for rows.Next() {
		var aid string
		if err := rows.Scan(&aid); err != nil {
			return err
		}
		accountIDs = append(accountIDs, aid)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	defaultStages := []struct {
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
	statusStageNames := map[string]string{
		"invited":   "Registrados",
		"confirmed": "Confirmados",
		"attended":  "Asistentes",
		"contacted": "Contactados",
		"declined":  "Declinados",
	}

	for _, aid := range accountIDs {
		var pipelineID string
		err := db.QueryRow(ctx, `SELECT id FROM event_pipelines WHERE account_id = $1 AND is_default = TRUE LIMIT 1`, aid).Scan(&pipelineID)
		if err != nil {
			err = db.QueryRow(ctx, `
				INSERT INTO event_pipelines (account_id, name, description, is_default)
				VALUES ($1, 'Pipeline por Defecto', 'Pipeline por defecto para eventos', TRUE)
				RETURNING id
			`, aid).Scan(&pipelineID)
			if err != nil {
				log.Printf("[MIGRATE] Warning: failed to create event pipeline for account %s: %v", aid, err)
				continue
			}
		}

		_, _ = db.Exec(ctx, `UPDATE event_pipeline_stages SET name = 'Pre inscritos' WHERE pipeline_id = $1 AND name = 'Pre inscrito'`, pipelineID)

		stageIDs := make(map[string]string)
		var maxPos int
		_ = db.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM event_pipeline_stages WHERE pipeline_id = $1`, pipelineID).Scan(&maxPos)
		for i, stage := range defaultStages {
			var stageID string
			err := db.QueryRow(ctx, `
				SELECT id FROM event_pipeline_stages
				WHERE pipeline_id = $1 AND LOWER(name) = LOWER($2)
				LIMIT 1
			`, pipelineID, stage.name).Scan(&stageID)
			if err != nil {
				position := i
				if maxPos >= i {
					maxPos++
					position = maxPos
				}
				err = db.QueryRow(ctx, `
					INSERT INTO event_pipeline_stages (pipeline_id, name, color, position)
					VALUES ($1, $2, $3, $4)
					RETURNING id
				`, pipelineID, stage.name, stage.color, position).Scan(&stageID)
				if err != nil {
					log.Printf("[MIGRATE] Warning: failed to create event stage %s for account %s: %v", stage.name, aid, err)
					continue
				}
			}
			stageIDs[stage.name] = stageID
		}

		_, _ = db.Exec(ctx, `UPDATE events SET pipeline_id = $1 WHERE account_id = $2 AND pipeline_id IS NULL`, pipelineID, aid)

		for status, stageName := range statusStageNames {
			stageID := stageIDs[stageName]
			if stageID == "" {
				continue
			}
			_, _ = db.Exec(ctx, `
				UPDATE event_participants SET stage_id = $1
				WHERE event_id IN (SELECT id FROM events WHERE account_id = $2)
				AND status = $3 AND stage_id IS NULL
			`, stageID, aid, status)
		}
	}

	// Migrate messages unique constraint from (account_id, device_id, message_id) to (chat_id, message_id)
	// This prevents cross-device duplicates and fixes NULL device_id dedup issues for history sync
	// Drop old unique constraint (account_id, device_id, message_id) — replaced by (chat_id, message_id)
	// Try as constraint first (most common), then as standalone index
	_, _ = db.Exec(ctx, `
		DO $$ BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'messages_account_id_device_id_message_id_key'
			) THEN
				ALTER TABLE messages DROP CONSTRAINT messages_account_id_device_id_message_id_key;
			END IF;
		END $$
	`)
	_, _ = db.Exec(ctx, `DROP INDEX IF EXISTS messages_account_id_device_id_message_id_key`)
	_, _ = db.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS messages_chat_id_message_id_key ON messages (chat_id, message_id)`)

	// Per-user Groq API key for Eros AI assistant
	_, _ = db.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS groq_api_key TEXT DEFAULT ''`)

	// Eros AI customization per user
	_, _ = db.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS eros_model TEXT DEFAULT ''`)
	_, _ = db.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS eros_role TEXT DEFAULT ''`)
	_, _ = db.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS eros_instructions TEXT DEFAULT ''`)

	// Saved filter for pending logbooks (bitácoras)
	_, _ = db.Exec(ctx, `ALTER TABLE event_logbooks ADD COLUMN IF NOT EXISTS saved_filter JSONB DEFAULT NULL`)

	// AI Token Usage logs
	_, _ = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ai_token_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			api_key_preview TEXT NOT NULL,
			model TEXT NOT NULL,
			input_tokens INT NOT NULL DEFAULT 0,
			output_tokens INT NOT NULL DEFAULT 0,
			total_tokens INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`)

	// ── Add missing event pipeline stages to ALL existing default pipelines ──
	// Adds: Interesados, Pre inscritos, Inscrito (if not present)
	// Renames: "Pre inscrito" → "Pre inscritos" (plural consistency)
	{
		_, _ = db.Exec(ctx, `UPDATE event_pipeline_stages SET name = 'Pre inscritos' WHERE name = 'Pre inscrito'`)
		_, _ = db.Exec(ctx, `DELETE FROM event_pipeline_stages WHERE name = 'No asistieron'`)

		newStages := []struct {
			name  string
			color string
		}{
			{"Interesados", "#8b5cf6"},
			{"Contactados", "#eab308"},
			{"Declinados", "#ef4444"},
			{"Pre inscritos", "#f59e0b"},
			{"Inscrito", "#6366f1"},
		}

		pipelineRows, err := db.Query(ctx, `SELECT id FROM event_pipelines WHERE is_default = TRUE`)
		if err == nil {
			var pipelineIDs []string
			for pipelineRows.Next() {
				var pid string
				if err := pipelineRows.Scan(&pid); err == nil {
					pipelineIDs = append(pipelineIDs, pid)
				}
			}
			pipelineRows.Close()

			for _, pid := range pipelineIDs {
				var maxPos int
				_ = db.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM event_pipeline_stages WHERE pipeline_id = $1`, pid).Scan(&maxPos)

				for _, stage := range newStages {
					var exists bool
					_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM event_pipeline_stages WHERE pipeline_id = $1 AND name = $2)`, pid, stage.name).Scan(&exists)
					if !exists {
						maxPos++
						_, _ = db.Exec(ctx, `INSERT INTO event_pipeline_stages (pipeline_id, name, color, position) VALUES ($1, $2, $3, $4)`, pid, stage.name, stage.color, maxPos)
						log.Printf("[MIGRATE] Added stage '%s' to pipeline %s at position %d", stage.name, pid, maxPos)
					}
				}
			}
		}
	}

	// ── Task Lists ──
	// (Moved to Migrate function)

	return nil
}

// SeedTemplateSurveys ensures all accounts have the 3 template surveys.
// Safe to call on every startup — idempotent via slug checks.
func SeedTemplateSurveys(db *pgxpool.Pool) error {
	ctx := context.Background()

	rows, err := db.Query(ctx, `SELECT id FROM accounts`)
	if err != nil {
		return fmt.Errorf("[SEED] failed to list accounts: %w", err)
	}
	defer rows.Close()

	var accountIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		accountIDs = append(accountIDs, id)
	}

	for _, accountID := range accountIDs {
		if err := seedTemplateSurveysForAccount(ctx, db, accountID); err != nil {
			log.Printf("[SEED] Warning: failed to seed templates for account %s: %v", accountID, err)
		}
	}
	return nil
}

// SeedTemplateSurveysForAccount seeds the 3 template surveys for a single account.
func SeedTemplateSurveysForAccount(db *pgxpool.Pool, accountID string) error {
	return seedTemplateSurveysForAccount(context.Background(), db, accountID)
}

func seedTemplateSurveysForAccount(ctx context.Context, db *pgxpool.Pool, accountID string) error {
	short := accountID[:8]

	type tplQuestion struct {
		orderIdx int
		qtype    string
		title    string
		desc     string
		required bool
		config   string
	}
	type tplSurvey struct {
		slugSuffix  string
		name        string
		description string
		welcomeT    string
		welcomeD    string
		thankT      string
		thankM      string
		branding    string
		questions   []tplQuestion
	}

	templates := []tplSurvey{
		{
			slugSuffix:  "motivaciones",
			name:        "Motivaciones para Estudiar en Nueva Acrópolis",
			description: "Descubre qué inspira a nuestros estudiantes a iniciar y continuar su camino en la filosofía práctica.",
			welcomeT:    "¿Qué te inspiró a estudiar filosofía?",
			welcomeD:    "Queremos conocer tu historia. Esta breve encuesta nos ayuda a entender qué te motivó a dar el primer paso y qué te impulsa a seguir creciendo con nosotros.",
			thankT:      "¡Gracias por compartir tu experiencia!",
			thankM:      "Tu historia nos inspira a seguir creando espacios de crecimiento. Cada respuesta nos ayuda a mejorar y llegar a más personas que buscan lo mismo que tú encontraste aquí.",
			branding:    `{"bg_color":"#0f172a","accent_color":"#f59e0b","font_family":"Playfair Display","title_size":"lg","text_color":"#f8fafc","button_style":"pill","bg_overlay":"0","question_align":"center"}`,
			questions: []tplQuestion{
				{0, "single_choice", "¿Cómo conociste Nueva Acrópolis?", "Selecciona la opción que mejor describa cómo llegaste a nosotros.", true, `{"options":["Recomendación de un amigo o familiar","Redes sociales (Facebook, Instagram, TikTok)","Búsqueda en internet","Pasé por la sede y me llamó la atención","Un evento o charla abierta","Publicidad (volantes, afiches, anuncios)","Otro"]}`},
				{1, "multiple_choice", "¿Qué te motivó a inscribirte por primera vez?", "Puedes seleccionar más de una opción.", true, `{"options":["Interés por la filosofía y el autoconocimiento","Buscar un propósito o sentido de vida","Desarrollar habilidades de liderazgo","Conocer personas con intereses similares","Superar una etapa difícil en mi vida","Curiosidad por las culturas antiguas","Crecimiento personal y espiritual","El voluntariado y la acción social"]}`},
				{2, "likert", "¿Qué tan importante fue cada factor en tu decisión de inscribirte?", "Valora del 1 (nada importante) al 5 (muy importante).", true, `{"likert_scale":5,"likert_min":"Nada importante","likert_max":"Muy importante"}`},
				{3, "single_choice", "¿Cuánto tiempo llevas estudiando en Nueva Acrópolis?", "", true, `{"options":["Menos de 3 meses","3 a 6 meses","6 meses a 1 año","1 a 2 años","2 a 5 años","Más de 5 años"]}`},
				{4, "multiple_choice", "¿Qué es lo que más valoras de tu experiencia en Nueva Acrópolis?", "Selecciona todas las que apliquen.", true, `{"options":["Las enseñanzas filosóficas y su aplicación práctica","La comunidad y los lazos de amistad","Las actividades de voluntariado","El desarrollo de disciplina y constancia","Los retiros y actividades especiales","Los profesores y mentores","El ambiente de respeto y crecimiento","Las artes marciales o actividades físicas"]}`},
				{5, "rating", "¿Qué tan probable es que recomiendes Nueva Acrópolis a un amigo o familiar?", `Donde 1 es "Nada probable" y 10 es "Totalmente probable".`, true, `{"max_rating":10}`},
				{6, "single_choice", "¿Qué te impulsa a continuar estudiando?", "", true, `{"options":["Siento que sigo creciendo como persona","La comunidad se ha vuelto parte de mi vida","Quiero profundizar más en las enseñanzas","Me motiva contribuir al mundo a través del voluntariado","Encuentro respuestas a mis preguntas de vida","El compromiso con mi propio desarrollo"]}`},
				{7, "long_text", "¿Hay alguna experiencia o momento en Nueva Acrópolis que haya sido especialmente significativo para ti?", "Comparte libremente. Tu historia puede inspirar a otros.", false, `{"placeholder":"Cuéntanos ese momento que marcó la diferencia..."}`},
			},
		},
		{
			slugSuffix:  "perfil",
			name:        "Conoce a Nuestros Estudiantes",
			description: "Perfil de intereses, edades y preocupaciones de los estudiantes para diseñar mejores programas.",
			welcomeT:    "Queremos conocerte mejor",
			welcomeD:    "Ayúdanos a entender quiénes somos como comunidad. Tus respuestas nos permiten crear programas que realmente respondan a lo que necesitas.",
			thankT:      "¡Tu voz cuenta!",
			thankM:      "Gracias por tomarte el tiempo de responder. Con esta información diseñaremos experiencias más significativas para ti y para quienes vendrán después.",
			branding:    `{"bg_color":"#1e293b","accent_color":"#10b981","font_family":"Poppins","title_size":"lg","text_color":"#e2e8f0","button_style":"rounded","bg_overlay":"0","question_align":"center"}`,
			questions: []tplQuestion{
				{0, "single_choice", "¿En qué rango de edad te encuentras?", "", true, `{"options":["15 a 20 años","21 a 25 años","26 a 30 años","31 a 40 años","41 a 50 años","51 a 60 años","Más de 60 años"]}`},
				{1, "single_choice", "¿Cuál es tu ocupación principal?", "", true, `{"options":["Estudiante universitario","Profesional independiente","Empleado en empresa privada","Funcionario público","Emprendedor / empresario","Jubilado / retirado","Ama de casa","Artista / creativo","Otro"]}`},
				{2, "multiple_choice", "¿Cuáles de estos temas te interesan más?", "Selecciona todos los que despierten tu curiosidad.", true, `{"options":["Filosofía práctica y ética","Psicología y autoconocimiento","Historia de las civilizaciones antiguas","Liderazgo y trabajo en equipo","Meditación y vida interior","Ecología y cuidado del planeta","Arte y expresión creativa","Ciencia y cosmovisión","Oratoria y comunicación","Artes marciales y disciplina corporal"]}`},
				{3, "multiple_choice", "¿Cuáles son tus principales preocupaciones en la vida actualmente?", "Selecciona hasta 3 opciones que resuenen contigo.", true, `{"options":["Encontrar mi vocación o propósito de vida","Estrés laboral o académico","Relaciones personales y comunicación","Estabilidad económica","Salud física y mental","Sentirme parte de una comunidad con valores","Incertidumbre sobre el futuro","Falta de motivación o inspiración","El estado del mundo y la sociedad","Quiero contribuir más a la sociedad"]}`},
				{4, "single_choice", "¿Qué buscas principalmente al estudiar en Nueva Acrópolis?", "", true, `{"options":["Herramientas para enfrentar la vida con más claridad","Una comunidad con la que comparta valores","Conocimiento que no encuentro en la educación tradicional","Desarrollar mi carácter y disciplina","Aportar algo al mundo desde el voluntariado","Un espacio de paz y reflexión"]}`},
				{5, "rating", "¿Qué tan satisfecho estás con tu experiencia hasta ahora?", `Donde 1 es "Nada satisfecho" y 5 es "Muy satisfecho".`, true, `{"max_rating":5}`},
				{6, "likert", "¿Qué tan de acuerdo estás con la siguiente afirmación?", `"Nueva Acrópolis me ha ayudado a crecer como persona."`, true, `{"likert_scale":5,"likert_min":"Totalmente en desacuerdo","likert_max":"Totalmente de acuerdo"}`},
				{7, "single_choice", "¿Con qué frecuencia participas en actividades de Nueva Acrópolis?", "", true, `{"options":["Varias veces por semana","Una vez por semana","Cada dos semanas","Una vez al mes","Esporádicamente"]}`},
				{8, "long_text", "¿Qué programa, taller o actividad te gustaría que ofreciéramos?", "Tus sugerencias son valiosas para nosotros.", false, `{"placeholder":"Comparte tus ideas..."}`},
			},
		},
		{
			slugSuffix:  "habitos",
			name:        "Hábitos y Medios de Comunicación",
			description: "Análisis de consumo de medios y hábitos digitales para optimizar la difusión de nuestras actividades.",
			welcomeT:    "¿Cómo te conectas con el mundo?",
			welcomeD:    "Queremos saber cómo consumes información y en qué inviertes tu tiempo libre. Esto nos ayuda a llegar a más personas que, como tú, buscan algo diferente.",
			thankT:      "¡Gracias por ayudarnos a crecer!",
			thankM:      "Con tus respuestas podremos llevar nuestro mensaje a más personas que buscan filosofía, crecimiento y comunidad. Eres parte fundamental de este esfuerzo.",
			branding:    `{"bg_color":"#0c0a09","accent_color":"#8b5cf6","font_family":"Space Grotesk","title_size":"lg","text_color":"#fafaf9","button_style":"pill","bg_overlay":"0","question_align":"center"}`,
			questions: []tplQuestion{
				{0, "multiple_choice", "¿Qué redes sociales usas con más frecuencia?", "Selecciona todas las que uses al menos una vez por semana.", true, `{"options":["Instagram","Facebook","TikTok","YouTube","X (Twitter)","LinkedIn","WhatsApp (grupos y canales)","Telegram","Pinterest","Reddit","Ninguna"]}`},
				{1, "single_choice", "¿Cuántas horas al día pasas en redes sociales?", "", true, `{"options":["Menos de 1 hora","1 a 2 horas","2 a 3 horas","3 a 5 horas","Más de 5 horas"]}`},
				{2, "multiple_choice", "¿Qué tipo de contenido consumes principalmente en internet?", "Selecciona todos los que apliquen.", true, `{"options":["Noticias y actualidad","Entretenimiento y humor","Desarrollo personal y motivación","Educación y cursos online","Filosofía y espiritualidad","Ciencia y tecnología","Podcasts y entrevistas","Música y arte","Fitness y salud","Viajes y cultura"]}`},
				{3, "single_choice", "¿Cuál es tu formato de contenido preferido?", "", true, `{"options":["Videos cortos (Reels, TikTok, Shorts)","Videos largos (YouTube, documentales)","Artículos y blogs","Podcasts y audio","Imágenes e infografías","Historias efímeras (Stories)","Transmisiones en vivo"]}`},
				{4, "multiple_choice", "¿En qué actividades inviertes más tu tiempo libre?", "Selecciona las 3 principales.", true, `{"options":["Navegar redes sociales","Leer libros o artículos","Hacer ejercicio o deporte","Ver series o películas","Estar con familia o amigos","Meditar o actividades introspectivas","Videojuegos","Aprender algo nuevo (cursos, tutoriales)","Voluntariado o actividades comunitarias","Escuchar música o podcasts","Actividades artísticas o creativas"]}`},
				{5, "single_choice", "¿En qué horario sueles estar más activo en redes sociales?", "", true, `{"options":["Mañana (6am - 12pm)","Tarde (12pm - 6pm)","Noche (6pm - 10pm)","Madrugada (10pm - 6am)","Todo el día por igual"]}`},
				{6, "single_choice", "¿Cómo prefieres enterarte de eventos y actividades?", "", true, `{"options":["WhatsApp (mensaje directo)","Instagram (publicaciones o historias)","Facebook (eventos o publicaciones)","Correo electrónico","Boca a boca (amigos, familia)","Afiches o volantes físicos","Página web"]}`},
				{7, "single_choice", "¿Escuchas podcasts regularmente?", "", true, `{"options":["Sí, todos los días","Sí, varias veces por semana","Ocasionalmente (1-2 veces al mes)","Casi nunca","No escucho podcasts"]}`},
				{8, "rating", "¿Qué tanto influyen las redes sociales en tus decisiones de asistir a eventos o actividades?", `Donde 1 es "No influyen nada" y 5 es "Influyen mucho".`, true, `{"max_rating":5}`},
				{9, "long_text", "¿Hay algún creador de contenido, canal o podcast sobre filosofía, desarrollo personal o espiritualidad que nos recomiendes?", "Nos encantaría conocer tus referencias favoritas.", false, `{"placeholder":"Comparte aquí tus recomendaciones..."}`},
			},
		},
	}

	for _, tpl := range templates {
		slug := fmt.Sprintf("tpl-%s-%s", tpl.slugSuffix, short)

		// Check if this template already exists for this account
		var exists bool
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM surveys WHERE slug = $1)`, slug).Scan(&exists)
		if exists {
			continue
		}

		var surveyID string
		err := db.QueryRow(ctx, `
			INSERT INTO surveys (account_id, name, description, slug, status, welcome_title, welcome_description, thank_you_title, thank_you_message, branding, is_template)
			VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, $9::jsonb, TRUE)
			RETURNING id
		`, accountID, tpl.name, tpl.description, slug, tpl.welcomeT, tpl.welcomeD, tpl.thankT, tpl.thankM, tpl.branding).Scan(&surveyID)
		if err != nil {
			return fmt.Errorf("failed to insert survey %s: %w", slug, err)
		}

		for _, q := range tpl.questions {
			_, err := db.Exec(ctx, `
				INSERT INTO survey_questions (survey_id, order_index, type, title, description, required, config)
				VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
			`, surveyID, q.orderIdx, q.qtype, q.title, q.desc, q.required, q.config)
			if err != nil {
				return fmt.Errorf("failed to insert question for %s: %w", slug, err)
			}
		}
		log.Printf("[SEED] Created template survey '%s' for account %s", tpl.name, short)
	}
	return nil
}
