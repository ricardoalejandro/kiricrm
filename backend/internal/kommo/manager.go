package kommo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/ws"
)

type ManagerConfig struct {
	PublicURL           string
	ProxyURL            string
	OutboxEnabled       bool
	OutboxBatchSize     int
	OutboxFlushInterval time.Duration
}

type Manager struct {
	db      *pgxpool.Pool
	hub     *ws.Hub
	cfg     ManagerConfig
	mu      sync.RWMutex
	byID    map[uuid.UUID]*SyncService
	secret  map[string]*SyncService
	account map[uuid.UUID]*SyncService
	primary *SyncService

	OnLeadTagsChanged func(ctx context.Context, accountID uuid.UUID)
}

func NewManager(db *pgxpool.Pool, hub *ws.Hub, cfg ManagerConfig) *Manager {
	return &Manager{
		db:      db,
		hub:     hub,
		cfg:     cfg,
		byID:    make(map[uuid.UUID]*SyncService),
		secret:  make(map[string]*SyncService),
		account: make(map[uuid.UUID]*SyncService),
	}
}

func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, svc := range m.byID {
		svc.Stop()
	}
	m.byID = make(map[uuid.UUID]*SyncService)
	m.secret = make(map[string]*SyncService)
	m.account = make(map[uuid.UUID]*SyncService)
	m.primary = nil

	rows, err := m.db.Query(ctx, `
		SELECT id, name, subdomain, access_token, webhook_secret
		FROM integration_instances
		WHERE provider = $1 AND is_active = TRUE AND status = $2
		ORDER BY created_at ASC
	`, domain.IntegrationProviderKommo, domain.IntegrationStatusActive)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var name, subdomain, accessToken, webhookSecret string
		if err := rows.Scan(&id, &name, &subdomain, &accessToken, &webhookSecret); err != nil {
			return err
		}
		if subdomain == "" || accessToken == "" {
			log.Printf("[KOMMO_MANAGER] Instance %s skipped: missing subdomain or token", name)
			continue
		}

		client := NewClientWithProxy(subdomain, accessToken, m.cfg.ProxyURL)
		instanceID := id
		svc := NewSyncServiceForInstance(client, m.db, m.hub, &instanceID, name)
		svc.WebhookSecret = webhookSecret
		svc.PublicURL = m.cfg.PublicURL
		svc.OnLeadTagsChanged = m.OnLeadTagsChanged
		if m.cfg.OutboxEnabled {
			svc.Outbox = NewOutboxForInstance(m.db, client, svc.Monitor, &instanceID, m.cfg.OutboxBatchSize, m.cfg.OutboxFlushInterval)
		}
		svc.Start()

		m.byID[id] = svc
		if m.primary == nil {
			m.primary = svc
		}
		if webhookSecret != "" {
			m.secret[webhookSecret] = svc
		}

		accountRows, err := m.db.Query(ctx, `
			SELECT ia.account_id
			FROM integration_instance_accounts ia
			JOIN accounts a ON a.id = ia.account_id
			WHERE ia.integration_instance_id = $1
			  AND ia.enabled = TRUE
			  AND COALESCE(a.kommo_enabled, false) = TRUE
		`, id)
		if err != nil {
			return err
		}
		for accountRows.Next() {
			var accountID uuid.UUID
			if err := accountRows.Scan(&accountID); err == nil {
				m.account[accountID] = svc
			}
		}
		accountRows.Close()

		log.Printf("[KOMMO_MANAGER] Started instance %s (%s.kommo.com)", name, subdomain)
	}
	return rows.Err()
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, svc := range m.byID {
		svc.Stop()
	}
	m.byID = make(map[uuid.UUID]*SyncService)
	m.secret = make(map[string]*SyncService)
	m.account = make(map[uuid.UUID]*SyncService)
	m.primary = nil
}

func (m *Manager) Primary() *SyncService {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.primary
}

func (m *Manager) ForAccount(ctx context.Context, accountID uuid.UUID) *SyncService {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	svc := m.account[accountID]
	m.mu.RUnlock()
	if svc != nil {
		return svc
	}

	var instanceID uuid.UUID
	err := m.db.QueryRow(ctx, `
		SELECT i.id
		FROM integration_instances i
		JOIN integration_instance_accounts ia ON ia.integration_instance_id = i.id
		JOIN accounts a ON a.id = ia.account_id
		WHERE i.provider = $1 AND i.is_active = TRUE AND i.status = $2 AND ia.account_id = $3 AND ia.enabled = TRUE
		  AND COALESCE(a.kommo_enabled, false) = TRUE
		ORDER BY ia.created_at ASC
		LIMIT 1
	`, domain.IntegrationProviderKommo, domain.IntegrationStatusActive, accountID).Scan(&instanceID)
	if err != nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[instanceID]
}

func (m *Manager) ForWebhook(secret string) *SyncService {
	if m == nil || secret == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.secret[secret]
}

func (m *Manager) ForInstance(id uuid.UUID) *SyncService {
	if m == nil || id == uuid.Nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[id]
}

func (m *Manager) RuntimeStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instances := make([]map[string]interface{}, 0, len(m.byID))
	for id, svc := range m.byID {
		instances = append(instances, map[string]interface{}{
			"id":     id,
			"name":   svc.InstanceName,
			"status": svc.GetStatus(),
		})
	}
	return map[string]interface{}{
		"running_instances": len(instances),
		"instances":         instances,
	}
}

func (m *Manager) RequireForAccount(ctx context.Context, accountID uuid.UUID) (*SyncService, error) {
	svc := m.ForAccount(ctx, accountID)
	if svc == nil {
		return nil, fmt.Errorf("Kommo no está configurado para esta cuenta")
	}
	return svc, nil
}
