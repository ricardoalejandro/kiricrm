package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/repository"
)

type SubscriptionAccessDecision struct {
	Allowed  bool
	Reason   string
	Message  string
	Overview *domain.SubscriptionOverview
}

// SubscriptionService coordinates SaaS plan state and enforcement rules.
type SubscriptionService struct {
	repos *repository.Repositories
}

func NewSubscriptionService(repos *repository.Repositories) *SubscriptionService {
	return &SubscriptionService{repos: repos}
}

func (s *SubscriptionService) ListPlans(ctx context.Context, includePrivate bool) ([]*domain.Plan, error) {
	return s.repos.Subscription.ListPlans(ctx, includePrivate)
}

func (s *SubscriptionService) GetOverview(ctx context.Context, accountID uuid.UUID) (*domain.SubscriptionOverview, error) {
	sub, err := s.repos.Subscription.GetByAccountID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		account, err := s.repos.Account.GetByID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if account == nil {
			return nil, fmt.Errorf("account not found")
		}
		if err := s.CreateForAccount(ctx, accountID, account.Plan, domain.SubscriptionStatusActive, 0); err != nil {
			return nil, err
		}
		sub, err = s.repos.Subscription.GetByAccountID(ctx, accountID)
		if err != nil {
			return nil, err
		}
	}

	plan, err := s.repos.Subscription.GetPlan(ctx, sub.PlanCode)
	if err != nil {
		return nil, err
	}
	usage, err := s.repos.Subscription.GetUsage(ctx, accountID)
	if err != nil {
		return nil, err
	}

	var daysLeft *int
	if deadline := subscriptionDeadline(sub); deadline != nil {
		days := daysUntil(*deadline)
		daysLeft = &days
	}

	overview := &domain.SubscriptionOverview{
		Subscription: sub,
		Plan:         plan,
		Usage:        usage,
		DaysLeft:     daysLeft,
		IsTrial:      sub.Status == domain.SubscriptionStatusTrialing,
		IsSuspended:  sub.Status == domain.SubscriptionStatusSuspended,
		IsActive:     subscriptionAllowsAccess(sub, time.Now()),
	}
	if plan != nil {
		overview.Entitlements = entitlementValues(plan.Entitlements)
	}
	account, err := s.repos.Account.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if overview.Entitlements == nil {
		overview.Entitlements = map[string]any{}
	}
	if account != nil && account.MaxUsersOverride != nil {
		overview.Entitlements["max_users"] = *account.MaxUsersOverride
	}
	return overview, nil
}

func (s *SubscriptionService) CheckAccess(ctx context.Context, accountID uuid.UUID) (*SubscriptionAccessDecision, error) {
	overview, err := s.GetOverview(ctx, accountID)
	if err != nil {
		return nil, err
	}
	decision := &SubscriptionAccessDecision{
		Allowed:  true,
		Reason:   "active",
		Message:  "Suscripción activa",
		Overview: overview,
	}
	if overview == nil || overview.Subscription == nil {
		decision.Allowed = false
		decision.Reason = "missing_subscription"
		decision.Message = "La cuenta no tiene una suscripción configurada"
		return decision, nil
	}

	subscription := overview.Subscription
	now := time.Now()
	switch subscription.Status {
	case domain.SubscriptionStatusActive:
		if subscription.CurrentPeriodEnd != nil && subscription.CurrentPeriodEnd.Before(now) {
			decision.Allowed = false
			decision.Reason = "subscription_expired"
			decision.Message = "El periodo de suscripción terminó"
		}
	case domain.SubscriptionStatusTrialing:
		if subscription.TrialEndsAt != nil && subscription.TrialEndsAt.Before(now) {
			decision.Allowed = false
			decision.Reason = "trial_expired"
			decision.Message = "El periodo de prueba terminó"
		}
	case domain.SubscriptionStatusGrace:
		if subscription.GraceEndsAt != nil && subscription.GraceEndsAt.Before(now) {
			decision.Allowed = false
			decision.Reason = "grace_expired"
			decision.Message = "El periodo de gracia terminó"
		}
	case domain.SubscriptionStatusPastDue:
		decision.Allowed = false
		decision.Reason = "past_due"
		decision.Message = "La suscripción tiene un pago pendiente"
	case domain.SubscriptionStatusSuspended:
		decision.Allowed = false
		decision.Reason = "suspended"
		decision.Message = "La suscripción está suspendida"
	case domain.SubscriptionStatusCanceled:
		decision.Allowed = false
		decision.Reason = "canceled"
		decision.Message = "La suscripción fue cancelada"
	case domain.SubscriptionStatusIncomplete:
		decision.Allowed = false
		decision.Reason = "incomplete"
		decision.Message = "La suscripción está incompleta"
	default:
		decision.Allowed = false
		decision.Reason = "invalid_status"
		decision.Message = "El estado de suscripción no es válido"
	}
	if overview != nil {
		overview.IsActive = decision.Allowed
	}
	return decision, nil
}

func (s *SubscriptionService) HasFeature(ctx context.Context, accountID uuid.UUID, key string) (bool, error) {
	overview, err := s.GetOverview(ctx, accountID)
	if err != nil {
		return false, err
	}
	if overview == nil || overview.Entitlements == nil {
		return false, nil
	}
	value, ok := overview.Entitlements[key]
	if !ok {
		return false, nil
	}
	switch typedValue := value.(type) {
	case bool:
		return typedValue, nil
	case string:
		return typedValue == "true", nil
	default:
		return false, nil
	}
}

func (s *SubscriptionService) EnforceLimit(ctx context.Context, accountID uuid.UUID, key string, increment int) error {
	if increment < 0 {
		increment = 0
	}
	overview, err := s.GetOverview(ctx, accountID)
	if err != nil {
		return err
	}
	limit, ok := entitlementInt(overview.Entitlements, key)
	if !ok || limit <= 0 {
		return nil
	}
	current := usageForEntitlement(overview.Usage, key)
	if current+increment > limit {
		return fmt.Errorf("tu plan permite hasta %d %s; actualmente tienes %d", limit, entitlementLabel(key), current)
	}
	return nil
}

func (s *SubscriptionService) CreateForAccount(ctx context.Context, accountID uuid.UUID, planCode, status string, trialDays int) error {
	planCode, err := s.validPlanCode(ctx, planCode)
	if err != nil {
		return err
	}
	if status == "" {
		status = domain.SubscriptionStatusActive
	}
	now := time.Now()
	sub := &domain.Subscription{
		AccountID:          accountID,
		PlanCode:           planCode,
		Status:             status,
		CurrentPeriodStart: &now,
		Metadata:           json.RawMessage(`{"source":"account_create"}`),
	}
	if status == domain.SubscriptionStatusTrialing && trialDays > 0 {
		trialEnd := now.AddDate(0, 0, trialDays)
		sub.TrialStartedAt = &now
		sub.TrialEndsAt = &trialEnd
	} else if status == domain.SubscriptionStatusActive {
		periodEnd := now.AddDate(1, 0, 0)
		sub.CurrentPeriodEnd = &periodEnd
	}
	return s.Upsert(ctx, sub)
}

func (s *SubscriptionService) Upsert(ctx context.Context, sub *domain.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription is required")
	}
	planCode, err := s.validPlanCode(ctx, sub.PlanCode)
	if err != nil {
		return err
	}
	sub.PlanCode = planCode
	if sub.Status == "" {
		sub.Status = domain.SubscriptionStatusActive
	}
	if !validSubscriptionStatus(sub.Status) {
		return fmt.Errorf("invalid subscription status")
	}
	if len(sub.Metadata) == 0 {
		sub.Metadata = json.RawMessage(`{}`)
	}
	if err := s.repos.Subscription.Upsert(ctx, sub); err != nil {
		return err
	}
	return s.repos.Subscription.SetAccountPlan(ctx, sub.AccountID, sub.PlanCode)
}

func (s *SubscriptionService) ExtendTrial(ctx context.Context, accountID uuid.UUID, days int) (*domain.SubscriptionOverview, error) {
	if days <= 0 {
		return nil, fmt.Errorf("days must be greater than zero")
	}
	overview, err := s.GetOverview(ctx, accountID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sub := overview.Subscription
	base := now
	if sub.TrialEndsAt != nil && sub.TrialEndsAt.After(now) {
		base = *sub.TrialEndsAt
	}
	trialEnd := base.AddDate(0, 0, days)
	if sub.TrialStartedAt == nil {
		sub.TrialStartedAt = &now
	}
	sub.TrialEndsAt = &trialEnd
	sub.Status = domain.SubscriptionStatusTrialing
	sub.SuspendedAt = nil
	sub.CanceledAt = nil
	sub.Metadata = mergeSubscriptionMetadata(sub.Metadata, map[string]any{
		"last_action": "extend_trial",
		"days":        days,
	})
	if err := s.Upsert(ctx, sub); err != nil {
		return nil, err
	}
	return s.GetOverview(ctx, accountID)
}

func (s *SubscriptionService) Suspend(ctx context.Context, accountID uuid.UUID) (*domain.SubscriptionOverview, error) {
	overview, err := s.GetOverview(ctx, accountID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sub := overview.Subscription
	sub.Status = domain.SubscriptionStatusSuspended
	sub.SuspendedAt = &now
	sub.Metadata = mergeSubscriptionMetadata(sub.Metadata, map[string]any{"last_action": "suspend"})
	if err := s.Upsert(ctx, sub); err != nil {
		return nil, err
	}
	return s.GetOverview(ctx, accountID)
}

func (s *SubscriptionService) Reactivate(ctx context.Context, accountID uuid.UUID) (*domain.SubscriptionOverview, error) {
	overview, err := s.GetOverview(ctx, accountID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sub := overview.Subscription
	sub.Status = domain.SubscriptionStatusActive
	sub.SuspendedAt = nil
	sub.CanceledAt = nil
	if sub.CurrentPeriodStart == nil {
		sub.CurrentPeriodStart = &now
	}
	if sub.CurrentPeriodEnd == nil || sub.CurrentPeriodEnd.Before(now) {
		periodEnd := now.AddDate(1, 0, 0)
		sub.CurrentPeriodEnd = &periodEnd
	}
	sub.Metadata = mergeSubscriptionMetadata(sub.Metadata, map[string]any{"last_action": "reactivate"})
	if err := s.Upsert(ctx, sub); err != nil {
		return nil, err
	}
	return s.GetOverview(ctx, accountID)
}

func (s *SubscriptionService) validPlanCode(ctx context.Context, planCode string) (string, error) {
	if planCode == "" {
		planCode = "basic"
	}
	plan, err := s.repos.Subscription.GetPlan(ctx, planCode)
	if err != nil {
		return "", err
	}
	if plan != nil {
		return planCode, nil
	}
	fallback, err := s.repos.Subscription.GetPlan(ctx, "enterprise")
	if err != nil {
		return "", err
	}
	if fallback == nil {
		return "", fmt.Errorf("plan not found")
	}
	return "enterprise", nil
}

func validSubscriptionStatus(status string) bool {
	switch status {
	case domain.SubscriptionStatusTrialing,
		domain.SubscriptionStatusActive,
		domain.SubscriptionStatusPastDue,
		domain.SubscriptionStatusGrace,
		domain.SubscriptionStatusSuspended,
		domain.SubscriptionStatusCanceled,
		domain.SubscriptionStatusIncomplete:
		return true
	default:
		return false
	}
}

func subscriptionAllowsAccess(sub *domain.Subscription, now time.Time) bool {
	if sub == nil {
		return false
	}
	switch sub.Status {
	case domain.SubscriptionStatusActive:
		return sub.CurrentPeriodEnd == nil || !sub.CurrentPeriodEnd.Before(now)
	case domain.SubscriptionStatusTrialing:
		return sub.TrialEndsAt == nil || !sub.TrialEndsAt.Before(now)
	case domain.SubscriptionStatusGrace:
		return sub.GraceEndsAt == nil || !sub.GraceEndsAt.Before(now)
	default:
		return false
	}
}

func subscriptionDeadline(sub *domain.Subscription) *time.Time {
	if sub == nil {
		return nil
	}
	if sub.Status == domain.SubscriptionStatusTrialing && sub.TrialEndsAt != nil {
		return sub.TrialEndsAt
	}
	if sub.Status == domain.SubscriptionStatusGrace && sub.GraceEndsAt != nil {
		return sub.GraceEndsAt
	}
	if sub.CurrentPeriodEnd != nil {
		return sub.CurrentPeriodEnd
	}
	return nil
}

func daysUntil(deadline time.Time) int {
	duration := deadline.Sub(time.Now())
	if duration <= 0 {
		return 0
	}
	day := 24 * time.Hour
	return int((duration + day - time.Nanosecond) / day)
}

func entitlementValues(raw map[string]json.RawMessage) map[string]any {
	values := make(map[string]any, len(raw))
	for key, value := range raw {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err == nil {
			values[key] = decoded
		}
	}
	return values
}

func entitlementInt(values map[string]any, key string) (int, bool) {
	if values == nil {
		return 0, false
	}
	value, ok := values[key]
	if !ok {
		return 0, false
	}
	switch typedValue := value.(type) {
	case float64:
		return int(typedValue), true
	case int:
		return typedValue, true
	case int64:
		return int(typedValue), true
	case json.Number:
		parsed, err := typedValue.Int64()
		if err == nil {
			return int(parsed), true
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(typedValue, "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func usageForEntitlement(usage domain.SubscriptionUsage, key string) int {
	switch key {
	case "max_users":
		return usage.Users
	case "max_devices":
		return usage.Devices
	case "max_contacts":
		return usage.Contacts
	case "max_leads":
		return usage.Leads
	case "max_chats":
		return usage.Chats
	default:
		return 0
	}
}

func entitlementLabel(key string) string {
	switch key {
	case "max_users":
		return "usuarios"
	case "max_devices":
		return "dispositivos"
	case "max_contacts":
		return "contactos"
	case "max_leads":
		return "leads"
	case "max_chats":
		return "chats"
	default:
		return "elementos"
	}
}

func mergeSubscriptionMetadata(raw json.RawMessage, updates map[string]any) json.RawMessage {
	metadata := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &metadata)
	}
	for key, value := range updates {
		metadata[key] = value
	}
	metadata["updated_by"] = "system"
	data, err := json.Marshal(metadata)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}
