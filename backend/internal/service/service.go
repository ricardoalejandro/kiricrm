package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/formula"
	"github.com/naperu/clarin/internal/kommo"
	"github.com/naperu/clarin/internal/repository"
	"github.com/naperu/clarin/internal/whatsapp"
	"github.com/naperu/clarin/internal/ws"
	"github.com/naperu/clarin/pkg/cache"
	"golang.org/x/crypto/bcrypt"
)

type Services struct {
	Auth             *AuthService
	Account          *AccountService
	Subscription     *SubscriptionService
	Device           *DeviceService
	Chat             *ChatService
	Contact          *ContactService
	Lead             *LeadService
	Pipeline         *PipelineService
	Tag              *TagService
	Campaign         *CampaignService
	Event            *EventService
	Interaction      *InteractionService
	QuickReply       *QuickReplyService
	Program          *ProgramService
	Role             *RoleService
	Automation       *AutomationService
	Survey           *SurveyService
	Task             *TaskService
	DocumentTemplate *DocumentTemplateService
}

func NewServices(repos *repository.Repositories, pool *whatsapp.DevicePool, hub *ws.Hub) *Services {
	return &Services{
		Auth:             &AuthService{repos: repos},
		Account:          &AccountService{repos: repos},
		Subscription:     NewSubscriptionService(repos),
		Device:           &DeviceService{repos: repos, pool: pool, hub: hub},
		Chat:             &ChatService{repos: repos, pool: pool},
		Contact:          &ContactService{repos: repos, pool: pool},
		Lead:             &LeadService{repos: repos},
		Pipeline:         &PipelineService{repos: repos},
		Tag:              &TagService{repos: repos},
		Campaign:         &CampaignService{repos: repos, pool: pool, hub: hub},
		Event:            &EventService{repos: repos, hub: hub},
		Interaction:      &InteractionService{repos: repos, hub: hub},
		QuickReply:       &QuickReplyService{repos: repos},
		Program:          NewProgramService(repos),
		Role:             &RoleService{repos: repos},
		Automation:       NewAutomationService(repos, pool, hub, nil), // cache injected after Init
		Survey:           NewSurveyService(repos),
		Task:             NewTaskService(repos, hub),
		DocumentTemplate: NewDocumentTemplateService(repos),
	}
}

// AuthService handles authentication
type AuthService struct {
	repos *repository.Repositories
	cache *cache.Cache
}

// SetCache injects the Redis cache into AuthService (for refresh tokens, blacklist, rate limiting)
func (s *AuthService) SetCache(c *cache.Cache) {
	s.cache = c
}

const (
	jwtAccessTTL           = 1 * time.Hour      // Access token lives 1 hour
	refreshTokenTTL        = 7 * 24 * time.Hour // Refresh token lives 7 days
	sessionIdleTTL         = 30 * time.Minute   // Session expires after 30 minutes of inactivity
	loginLockoutTTL        = 15 * time.Minute   // Lockout after max failed attempts
	maxLoginAttempts       = 5                  // Failed attempts before lockout
	refreshTokenKeyPrefix  = "refresh:"         // Redis key prefix for refresh tokens
	jwtBlacklistKeyPrefix  = "jwtblk:"          // Redis key prefix for JWT blacklist
	loginFailuresKeyPrefix = "loginfail:"       // Redis key prefix for login failures
	userInvalidatedPrefix  = "userinv:"         // Redis key prefix for invalidated users
	sessionKeyPrefix       = "session:"         // Redis key prefix for active login sessions
)

type JWTClaims struct {
	UserID       uuid.UUID `json:"user_id"`
	AccountID    uuid.UUID `json:"account_id"`
	SessionID    string    `json:"session_id"`
	Username     string    `json:"username"`
	IsAdmin      bool      `json:"is_admin"`
	IsSuperAdmin bool      `json:"is_super_admin"`
	Role         string    `json:"role"`
	Permissions  []string  `json:"permissions"`
	jwt.RegisteredClaims
}

type authSessionData struct {
	UserID    string `json:"user_id"`
	AccountID string `json:"account_id"`
	Username  string `json:"username"`
	CreatedAt int64  `json:"created_at"`
	LastSeen  int64  `json:"last_seen"`
}

type refreshTokenData struct {
	UserID    string `json:"user_id"`
	AccountID string `json:"account_id"`
	Username  string `json:"username"`
	SessionID string `json:"session_id"`
	CreatedAt int64  `json:"created_at"`
}

func (s *AuthService) Login(ctx context.Context, username, password, jwtSecret string) (string, string, *domain.User, []*domain.UserAccount, error) {
	if s.cache == nil {
		return "", "", nil, nil, fmt.Errorf("session service unavailable")
	}

	// Check login rate limiting
	if s.cache != nil {
		failKey := loginFailuresKeyPrefix + strings.ToLower(username)
		data, _ := s.cache.Get(ctx, failKey)
		if data != nil {
			var failures int
			if err := json.Unmarshal(data, &failures); err == nil && failures >= maxLoginAttempts {
				return "", "", nil, nil, fmt.Errorf("cuenta bloqueada temporalmente, intente en 15 minutos")
			}
		}
	}

	user, err := s.repos.User.GetByUsername(ctx, username)
	if err != nil {
		s.recordLoginFailure(ctx, username)
		return "", "", nil, nil, fmt.Errorf("invalid credentials")
	}
	if user == nil {
		s.recordLoginFailure(ctx, username)
		return "", "", nil, nil, fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.recordLoginFailure(ctx, username)
		return "", "", nil, nil, fmt.Errorf("invalid credentials")
	}

	// Clear login failures on success
	if s.cache != nil {
		failKey := loginFailuresKeyPrefix + strings.ToLower(username)
		_ = s.cache.Del(ctx, failKey)
	}

	// Get user's account assignments
	if err := s.repos.UserAccount.NormalizeForUser(ctx, user.ID); err != nil {
		return "", "", nil, nil, fmt.Errorf("failed to normalize user accounts: %w", err)
	}
	userAccounts, _ := s.repos.UserAccount.GetByUserID(ctx, user.ID)

	// Determine first/default account
	activeAccountID := user.AccountID
	activeRole := user.Role
	if len(userAccounts) > 0 {
		for _, ua := range userAccounts {
			if ua.IsDefault {
				activeAccountID = ua.AccountID
				activeRole = ua.Role
				break
			}
		}
	}

	// Generate JWT with default account
	// Admins/super_admins get wildcard; agents get their role's permissions
	isAdmin := user.IsAdmin || user.IsSuperAdmin || activeRole == domain.RoleAdmin || activeRole == domain.RoleSuperAdmin
	var permissions []string
	if isAdmin {
		permissions = []string{domain.PermAll}
	} else {
		permissions, _ = s.repos.UserAccount.GetUserPermissions(ctx, user.ID, activeAccountID)
	}

	sessionID, sessionCreatedAt, err := s.createSession(ctx, user.ID, activeAccountID, user.Username)
	if err != nil {
		return "", "", nil, nil, err
	}

	jti := uuid.New().String()
	claims := &JWTClaims{
		UserID:       user.ID,
		AccountID:    activeAccountID,
		SessionID:    sessionID,
		Username:     user.Username,
		IsAdmin:      isAdmin,
		IsSuperAdmin: user.IsSuperAdmin,
		Role:         activeRole,
		Permissions:  permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtAccessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "clarin",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("failed to sign token: %w", err)
	}

	// Generate refresh token and store in Redis
	refreshToken := uuid.New().String()
	rtData := refreshTokenData{
		UserID:    user.ID.String(),
		AccountID: activeAccountID.String(),
		Username:  user.Username,
		SessionID: sessionID,
		CreatedAt: sessionCreatedAt,
	}
	rtJSON, _ := json.Marshal(rtData)
	_ = s.cache.Set(ctx, refreshTokenKeyPrefix+refreshToken, rtJSON, refreshTokenTTL)

	// Update user fields to match active account
	user.AccountID = activeAccountID
	user.Role = activeRole
	for _, ua := range userAccounts {
		if ua.AccountID == activeAccountID {
			user.AccountName = ua.AccountName
			break
		}
	}

	return tokenString, refreshToken, user, userAccounts, nil
}

func (s *AuthService) SwitchAccount(ctx context.Context, userID, targetAccountID uuid.UUID, sessionID, jwtSecret string) (string, string, *domain.User, error) {
	if sessionID == "" {
		return "", "", nil, fmt.Errorf("session expired")
	}
	sessionData, err := s.TouchSession(ctx, sessionID)
	if err != nil {
		return "", "", nil, err
	}

	// Verify user exists
	user, err := s.repos.User.GetByID(ctx, userID)
	if err != nil || user == nil {
		return "", "", nil, fmt.Errorf("user not found")
	}

	// Verify user has access to the target account
	exists, err := s.repos.UserAccount.Exists(ctx, userID, targetAccountID)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to check access: %w", err)
	}
	if !exists {
		return "", "", nil, fmt.Errorf("no tiene acceso a esta cuenta")
	}

	// Get the role for this specific account
	userAccounts, _ := s.repos.UserAccount.GetByUserID(ctx, userID)
	accountRole := user.Role
	accountName := ""
	for _, ua := range userAccounts {
		if ua.AccountID == targetAccountID {
			accountRole = ua.Role
			accountName = ua.AccountName
			break
		}
	}

	// Generate new JWT for the target account
	isAdmin := user.IsAdmin || user.IsSuperAdmin || accountRole == domain.RoleAdmin || accountRole == domain.RoleSuperAdmin
	var permissions []string
	if isAdmin {
		permissions = []string{domain.PermAll}
	} else {
		permissions, _ = s.repos.UserAccount.GetUserPermissions(ctx, userID, targetAccountID)
	}

	jti := uuid.New().String()
	claims := &JWTClaims{
		UserID:       user.ID,
		AccountID:    targetAccountID,
		SessionID:    sessionID,
		Username:     user.Username,
		IsAdmin:      isAdmin,
		IsSuperAdmin: user.IsSuperAdmin,
		Role:         accountRole,
		Permissions:  permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtAccessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "clarin",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to sign token: %w", err)
	}

	// Generate refresh token
	refreshToken := uuid.New().String()
	rtData := refreshTokenData{
		UserID:    user.ID.String(),
		AccountID: targetAccountID.String(),
		Username:  user.Username,
		SessionID: sessionID,
		CreatedAt: sessionData.CreatedAt,
	}
	rtJSON, _ := json.Marshal(rtData)
	_ = s.cache.Set(ctx, refreshTokenKeyPrefix+refreshToken, rtJSON, refreshTokenTTL)

	// Update user object to reflect active account
	user.AccountID = targetAccountID
	user.Role = accountRole
	user.AccountName = accountName

	return tokenString, refreshToken, user, nil
}

func (s *AuthService) GetUserAccounts(ctx context.Context, userID uuid.UUID) ([]*domain.UserAccount, error) {
	return s.repos.UserAccount.GetByUserID(ctx, userID)
}

func (s *AuthService) ValidateToken(tokenString, jwtSecret string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method to prevent algorithm confusion attacks
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	if claims.SessionID == "" {
		return nil, fmt.Errorf("session expired")
	}

	// Check JWT blacklist (revoked tokens)
	if s.cache != nil && claims.ID != "" {
		ctx := context.Background()
		data, _ := s.cache.Get(ctx, jwtBlacklistKeyPrefix+claims.ID)
		if data != nil {
			return nil, fmt.Errorf("token has been revoked")
		}
	}
	if _, err := s.TouchSession(context.Background(), claims.SessionID); err != nil {
		return nil, err
	}

	return claims, nil
}

func (s *AuthService) GetUser(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	return s.repos.User.GetByID(ctx, userID)
}

// Logout revokes the current JWT and deletes the refresh token from Redis
func (s *AuthService) Logout(ctx context.Context, claims *JWTClaims, refreshToken string) {
	if s.cache == nil {
		return
	}
	// Blacklist the JWT by its jti for its remaining lifetime
	if claims.ID != "" && claims.ExpiresAt != nil {
		remaining := time.Until(claims.ExpiresAt.Time)
		if remaining > 0 {
			_ = s.cache.Set(ctx, jwtBlacklistKeyPrefix+claims.ID, []byte("1"), remaining)
		}
	}
	// Delete the refresh token
	if refreshToken != "" {
		_ = s.cache.Del(ctx, refreshTokenKeyPrefix+refreshToken)
	}
	if claims != nil && claims.SessionID != "" {
		_ = s.cache.Del(ctx, sessionKeyPrefix+claims.SessionID)
	}
}

func (s *AuthService) LogoutByRefreshToken(ctx context.Context, refreshToken string) {
	if s.cache == nil || refreshToken == "" {
		return
	}
	data, _ := s.cache.Get(ctx, refreshTokenKeyPrefix+refreshToken)
	if data != nil {
		var rt refreshTokenData
		if err := json.Unmarshal(data, &rt); err == nil && rt.SessionID != "" {
			_ = s.cache.Del(ctx, sessionKeyPrefix+rt.SessionID)
		}
	}
	_ = s.cache.Del(ctx, refreshTokenKeyPrefix+refreshToken)
}

// BlacklistJTI blacklists a JWT's JTI for its remaining lifetime (used on token refresh)
func (s *AuthService) BlacklistJTI(claims *JWTClaims) {
	if s.cache == nil || claims == nil || claims.ID == "" || claims.ExpiresAt == nil {
		return
	}
	remaining := time.Until(claims.ExpiresAt.Time)
	if remaining > 0 {
		_ = s.cache.Set(context.Background(), jwtBlacklistKeyPrefix+claims.ID, []byte("1"), remaining)
	}
}

// InvalidateUserSessions marks a user as invalidated so all their existing tokens are rejected.
func (s *AuthService) InvalidateUserSessions(userID uuid.UUID) {
	if s.cache == nil {
		return
	}
	// Store invalidation timestamp; any JWT issued before this is rejected
	_ = s.cache.Set(context.Background(), userInvalidatedPrefix+userID.String(), []byte(fmt.Sprintf("%d", time.Now().Unix())), jwtAccessTTL)
}

// IsUserSessionInvalidated checks if a user's sessions have been invalidated after their token was issued.
func (s *AuthService) IsUserSessionInvalidated(claims *JWTClaims) bool {
	if s.cache == nil || claims == nil {
		return false
	}
	data, _ := s.cache.Get(context.Background(), userInvalidatedPrefix+claims.UserID.String())
	if data == nil {
		return false
	}
	// If the invalidation happened after the token was issued, reject
	invalidatedAt, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return true // corrupted data = reject
	}
	if claims.IssuedAt != nil && claims.IssuedAt.Time.Unix() < invalidatedAt {
		return true
	}
	return false
}

// RefreshToken validates a refresh token and issues a new JWT + rotated refresh token
func (s *AuthService) RefreshToken(ctx context.Context, oldRefreshToken, jwtSecret string) (string, string, error) {
	if s.cache == nil {
		return "", "", fmt.Errorf("session service unavailable")
	}

	// Look up refresh token in Redis
	data, err := s.cache.Get(ctx, refreshTokenKeyPrefix+oldRefreshToken)
	if err != nil || data == nil {
		return "", "", fmt.Errorf("invalid or expired refresh token")
	}

	// Parse stored data
	var rtData refreshTokenData
	if err := json.Unmarshal(data, &rtData); err != nil {
		return "", "", fmt.Errorf("corrupted refresh token data")
	}

	if rtData.SessionID == "" {
		_ = s.cache.Del(ctx, refreshTokenKeyPrefix+oldRefreshToken)
		return "", "", fmt.Errorf("session expired")
	}
	sessionData, err := s.TouchSession(ctx, rtData.SessionID)
	if err != nil {
		_ = s.cache.Del(ctx, refreshTokenKeyPrefix+oldRefreshToken)
		return "", "", err
	}

	userID, err := uuid.Parse(rtData.UserID)
	if err != nil {
		return "", "", fmt.Errorf("invalid user in refresh token")
	}
	accountID, err := uuid.Parse(rtData.AccountID)
	if err != nil {
		return "", "", fmt.Errorf("invalid account in refresh token")
	}

	// Verify user still exists and is active
	user, err := s.repos.User.GetByID(ctx, userID)
	if err != nil || user == nil || !user.IsActive {
		_ = s.cache.Del(ctx, refreshTokenKeyPrefix+oldRefreshToken)
		return "", "", fmt.Errorf("user not found")
	}

	// Verify user still has access to account
	exists, _ := s.repos.UserAccount.Exists(ctx, userID, accountID)
	if !exists {
		_ = s.cache.Del(ctx, refreshTokenKeyPrefix+oldRefreshToken)
		return "", "", fmt.Errorf("account access revoked")
	}

	// Get current permissions
	// Get role for this account
	userAccounts, _ := s.repos.UserAccount.GetByUserID(ctx, userID)
	accountRole := user.Role
	for _, ua := range userAccounts {
		if ua.AccountID == accountID {
			accountRole = ua.Role
			break
		}
	}

	// Get current permissions — per-account admin gets full access
	isAdmin := user.IsAdmin || user.IsSuperAdmin || accountRole == domain.RoleAdmin || accountRole == domain.RoleSuperAdmin
	var permissions []string
	if isAdmin {
		permissions = []string{domain.PermAll}
	} else {
		permissions, _ = s.repos.UserAccount.GetUserPermissions(ctx, userID, accountID)
	}

	// Generate new JWT
	jti := uuid.New().String()
	claims := &JWTClaims{
		UserID:       user.ID,
		AccountID:    accountID,
		SessionID:    rtData.SessionID,
		Username:     user.Username,
		IsAdmin:      isAdmin,
		IsSuperAdmin: user.IsSuperAdmin,
		Role:         accountRole,
		Permissions:  permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtAccessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "clarin",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", "", fmt.Errorf("failed to sign token: %w", err)
	}

	// Rotate refresh token: delete old, create new
	_ = s.cache.Del(ctx, refreshTokenKeyPrefix+oldRefreshToken)
	newRefreshToken := uuid.New().String()
	newRTData := refreshTokenData{
		UserID:    user.ID.String(),
		AccountID: accountID.String(),
		Username:  user.Username,
		SessionID: rtData.SessionID,
		CreatedAt: sessionData.CreatedAt,
	}
	rtJSON, _ := json.Marshal(newRTData)
	_ = s.cache.Set(ctx, refreshTokenKeyPrefix+newRefreshToken, rtJSON, refreshTokenTTL)

	return tokenString, newRefreshToken, nil
}

func (s *AuthService) createSession(ctx context.Context, userID, accountID uuid.UUID, username string) (string, int64, error) {
	if s.cache == nil {
		return "", 0, fmt.Errorf("session service unavailable")
	}
	now := time.Now().Unix()
	sessionID := uuid.New().String()
	data := authSessionData{
		UserID:    userID.String(),
		AccountID: accountID.String(),
		Username:  username,
		CreatedAt: now,
		LastSeen:  now,
	}
	raw, _ := json.Marshal(data)
	if err := s.cache.Set(ctx, sessionKeyPrefix+sessionID, raw, sessionIdleTTL); err != nil {
		return "", 0, fmt.Errorf("failed to create session: %w", err)
	}
	return sessionID, now, nil
}

func (s *AuthService) TouchSession(ctx context.Context, sessionID string) (*authSessionData, error) {
	if s.cache == nil {
		return nil, fmt.Errorf("session service unavailable")
	}
	data, err := s.cache.Get(ctx, sessionKeyPrefix+sessionID)
	if err != nil || data == nil {
		return nil, fmt.Errorf("session expired")
	}
	var session authSessionData
	if err := json.Unmarshal(data, &session); err != nil {
		_ = s.cache.Del(ctx, sessionKeyPrefix+sessionID)
		return nil, fmt.Errorf("corrupted session")
	}
	if time.Since(time.Unix(session.CreatedAt, 0)) > refreshTokenTTL {
		_ = s.cache.Del(ctx, sessionKeyPrefix+sessionID)
		return nil, fmt.Errorf("session expired")
	}
	session.LastSeen = time.Now().Unix()
	raw, _ := json.Marshal(session)
	if err := s.cache.Set(ctx, sessionKeyPrefix+sessionID, raw, sessionIdleTTL); err != nil {
		return nil, fmt.Errorf("failed to refresh session: %w", err)
	}
	return &session, nil
}

// ChangePassword validates the current password and updates to the new one
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	user, err := s.repos.User.GetByID(ctx, userID)
	if err != nil || user == nil {
		return fmt.Errorf("user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return fmt.Errorf("contraseña actual incorrecta")
	}

	if err := ValidateStrongPassword(newPassword); err != nil {
		return err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	return s.repos.User.UpdatePassword(ctx, userID, string(hashedPassword))
}

// recordLoginFailure increments the failed login counter for a username
func (s *AuthService) recordLoginFailure(ctx context.Context, username string) {
	if s.cache == nil {
		return
	}
	failKey := loginFailuresKeyPrefix + strings.ToLower(username)
	data, _ := s.cache.Get(ctx, failKey)
	failures := 0
	if data != nil {
		_ = json.Unmarshal(data, &failures)
	}
	failures++
	countJSON, _ := json.Marshal(failures)
	_ = s.cache.Set(ctx, failKey, countJSON, loginLockoutTTL)
}

// AccountService handles account management (super admin)
type AccountService struct {
	repos *repository.Repositories
}

func (s *AccountService) GetAll(ctx context.Context) ([]*domain.Account, error) {
	return s.repos.Account.GetAll(ctx)
}

func (s *AccountService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return s.repos.Account.GetByID(ctx, id)
}

func (s *AccountService) Create(ctx context.Context, a *domain.Account) error {
	return s.repos.Account.Create(ctx, a)
}

func (s *AccountService) Update(ctx context.Context, a *domain.Account) error {
	return s.repos.Account.Update(ctx, a)
}

func (s *AccountService) ToggleActive(ctx context.Context, id uuid.UUID) error {
	return s.repos.Account.ToggleActive(ctx, id)
}

func (s *AccountService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Account.Delete(ctx, id)
}

func (s *AccountService) GetUsers(ctx context.Context, accountID *uuid.UUID) ([]*domain.User, error) {
	if accountID != nil {
		return s.repos.User.GetByAccountID(ctx, *accountID)
	}
	return s.repos.User.GetAll(ctx)
}

func (s *AccountService) CreateUser(ctx context.Context, user *domain.User, password string) error {
	if err := ValidateStrongPassword(password); err != nil {
		return err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	user.PasswordHash = string(hashedPassword)
	if err := s.repos.User.Create(ctx, user); err != nil {
		return err
	}
	// Auto-assign user to their primary account in user_accounts
	ua := &domain.UserAccount{
		UserID:    user.ID,
		AccountID: user.AccountID,
		Role:      user.Role,
		IsDefault: true,
	}
	return s.repos.UserAccount.Assign(ctx, ua)
}

func (s *AccountService) UpdateUser(ctx context.Context, user *domain.User) error {
	return s.repos.User.Update(ctx, user)
}

func (s *AccountService) ResetPassword(ctx context.Context, userID uuid.UUID, password string) error {
	if err := ValidateStrongPassword(password); err != nil {
		return err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	return s.repos.User.UpdatePassword(ctx, userID, string(hashedPassword))
}

func (s *AccountService) ToggleUserActive(ctx context.Context, userID uuid.UUID) error {
	return s.repos.User.ToggleActive(ctx, userID)
}

func (s *AccountService) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	return s.repos.User.Delete(ctx, userID)
}

func (s *AccountService) AssignUserAccount(ctx context.Context, ua *domain.UserAccount) error {
	if err := s.repos.UserAccount.Assign(ctx, ua); err != nil {
		return err
	}
	return s.repos.UserAccount.NormalizeForUser(ctx, ua.UserID)
}

func (s *AccountService) RemoveUserAccount(ctx context.Context, userID, accountID uuid.UUID) error {
	count, err := s.repos.UserAccount.CountByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if count <= 1 {
		return fmt.Errorf("el usuario debe conservar al menos una cuenta asignada")
	}
	if err := s.repos.UserAccount.Remove(ctx, userID, accountID); err != nil {
		return err
	}
	return s.repos.UserAccount.NormalizeForUser(ctx, userID)
}

func (s *AccountService) GetUserAccountAssignments(ctx context.Context, userID uuid.UUID) ([]*domain.UserAccount, error) {
	if err := s.repos.UserAccount.NormalizeForUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.repos.UserAccount.GetByUserID(ctx, userID)
}

// DeviceService handles WhatsApp devices
type DeviceService struct {
	repos *repository.Repositories
	pool  *whatsapp.DevicePool
	hub   *ws.Hub
}

func (s *DeviceService) Create(ctx context.Context, accountID uuid.UUID, name string) (*domain.Device, error) {
	return s.pool.CreateDevice(ctx, accountID, name)
}

func (s *DeviceService) Connect(ctx context.Context, deviceID uuid.UUID) error {
	return s.pool.ConnectDevice(ctx, deviceID)
}

func (s *DeviceService) Disconnect(ctx context.Context, deviceID uuid.UUID) error {
	return s.pool.DisconnectDevice(ctx, deviceID)
}

func (s *DeviceService) Reset(ctx context.Context, deviceID uuid.UUID) error {
	return s.pool.ResetDevice(ctx, deviceID)
}

func (s *DeviceService) Delete(ctx context.Context, deviceID uuid.UUID) error {
	return s.pool.DeleteDevice(ctx, deviceID)
}

func (s *DeviceService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Device, error) {
	devices, err := s.repos.Device.GetByAccountID(ctx, accountID)
	if err != nil {
		return nil, err
	}

	// Add live status from pool
	for _, device := range devices {
		if device.Provider != nil && *device.Provider == domain.DeviceProviderWhatsAppCloudAPI {
			continue
		}
		status := s.pool.GetDeviceStatus(device.ID)
		device.Status = &status
		qr := s.pool.GetQRCode(device.ID)
		if qr != "" {
			device.QRCode = &qr
		}
	}

	return devices, nil
}

func (s *DeviceService) GetByID(ctx context.Context, deviceID uuid.UUID) (*domain.Device, error) {
	device, err := s.repos.Device.GetByID(ctx, deviceID)
	if err != nil || device == nil {
		return nil, err
	}
	if device.Provider != nil && *device.Provider == domain.DeviceProviderWhatsAppCloudAPI {
		return device, nil
	}

	status := s.pool.GetDeviceStatus(device.ID)
	device.Status = &status
	qr := s.pool.GetQRCode(device.ID)
	if qr != "" {
		device.QRCode = &qr
	}

	return device, nil
}

// ChatService handles chat operations
type ChatService struct {
	repos *repository.Repositories
	pool  *whatsapp.DevicePool
}

func (s *ChatService) ensureWhatsAppWebOutbound(ctx context.Context, deviceID uuid.UUID) error {
	device, err := s.repos.Device.GetByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if device == nil {
		return fmt.Errorf("device not found")
	}
	if device.Provider != nil && *device.Provider == domain.DeviceProviderWhatsAppCloudAPI {
		return fmt.Errorf("canal API Oficial en modo configuracion: envio bloqueado hasta activar facturacion y reglas de plantillas")
	}
	return nil
}

func (s *ChatService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Chat, error) {
	return s.repos.Chat.GetByAccountID(ctx, accountID)
}

func (s *ChatService) GetByAccountIDWithFilters(ctx context.Context, accountID uuid.UUID, filter domain.ChatFilter) ([]*domain.Chat, int, error) {
	return s.repos.Chat.GetByAccountIDWithFilters(ctx, accountID, filter)
}

func (s *ChatService) GetByID(ctx context.Context, chatID uuid.UUID) (*domain.Chat, error) {
	return s.repos.Chat.GetByID(ctx, chatID)
}

func (s *ChatService) GetChatDetails(ctx context.Context, chatID uuid.UUID) (*domain.ChatDetails, error) {
	chat, err := s.repos.Chat.GetByID(ctx, chatID)
	if err != nil || chat == nil {
		return nil, err
	}

	details := &domain.ChatDetails{
		Chat: chat,
	}

	// Get contact if exists
	if chat.ContactID != nil {
		// We need to get contact by ID, but for now get by JID
	}
	contact, _ := s.repos.Contact.GetByJID(ctx, chat.AccountID, chat.JID)
	if contact != nil {
		details.Contact = contact
	}

	// Get lead
	lead, _ := s.repos.Lead.GetByJID(ctx, chat.AccountID, chat.JID)
	if lead != nil {
		details.Lead = lead
	}

	return details, nil
}

func (s *ChatService) FindByJID(ctx context.Context, accountID uuid.UUID, jid string) (*domain.Chat, error) {
	return s.repos.Chat.FindByJID(ctx, accountID, jid)
}

func (s *ChatService) CreateNewChat(ctx context.Context, accountID, deviceID uuid.UUID, phone string) (*domain.Chat, error) {
	// Normalize phone number to JID
	jid := phone
	if !strings.Contains(phone, "@") {
		phone = kommo.NormalizePhone(phone)
		jid = phone + "@s.whatsapp.net"
	}

	// Create or get existing chat
	chat, err := s.repos.Chat.GetOrCreate(ctx, accountID, deviceID, jid, "")
	if err != nil {
		return nil, err
	}

	return chat, nil
}

func (s *ChatService) GetMessages(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*domain.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.repos.Message.GetByChatID(ctx, chatID, limit, offset)
}

func (s *ChatService) RequestHistorySync(ctx context.Context, accountID, deviceID, chatID uuid.UUID, chatJID string) error {
	return s.pool.RequestHistorySync(ctx, accountID, deviceID, chatID, chatJID)
}

func (s *ChatService) SendMessage(ctx context.Context, deviceID uuid.UUID, to, body string) (*domain.Message, error) {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return nil, err
	}
	return s.pool.SendMessage(ctx, deviceID, to, body)
}

func (s *ChatService) SendMediaMessage(ctx context.Context, deviceID uuid.UUID, to, caption, mediaURL, mediaType string) (*domain.Message, error) {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return nil, err
	}
	return s.pool.SendMediaMessage(ctx, deviceID, to, caption, mediaURL, mediaType)
}

func (s *ChatService) SendReplyMessage(ctx context.Context, deviceID uuid.UUID, to, body, quotedID, quotedBody, quotedSender string, quotedIsFromMe bool) (*domain.Message, error) {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return nil, err
	}
	return s.pool.SendReplyMessage(ctx, deviceID, to, body, quotedID, quotedBody, quotedSender, quotedIsFromMe)
}

func (s *ChatService) ForwardMessage(ctx context.Context, deviceID uuid.UUID, to string, originalMsg *domain.Message) (*domain.Message, error) {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return nil, err
	}
	return s.pool.ForwardMessage(ctx, deviceID, to, originalMsg)
}

func (s *ChatService) SendReaction(ctx context.Context, deviceID uuid.UUID, to, targetMessageID, emoji string, targetFromMe bool) error {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return err
	}
	return s.pool.SendReaction(ctx, deviceID, to, targetMessageID, emoji, targetFromMe)
}

func (s *ChatService) SendPoll(ctx context.Context, deviceID uuid.UUID, to, question string, options []string, maxSelections int) (*domain.Message, error) {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return nil, err
	}
	return s.pool.SendPoll(ctx, deviceID, to, question, options, maxSelections)
}

func (s *ChatService) SendContactMessage(ctx context.Context, deviceID uuid.UUID, to, contactName, contactPhone string) (*domain.Message, error) {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return nil, err
	}
	return s.pool.SendContactMessage(ctx, deviceID, to, contactName, contactPhone)
}

func (s *ChatService) GetReactions(ctx context.Context, chatID uuid.UUID) ([]*domain.MessageReaction, error) {
	return s.repos.Reaction.GetByChatID(ctx, chatID)
}

func (s *ChatService) GetPollData(ctx context.Context, messageID uuid.UUID) ([]*domain.PollOption, []*domain.PollVote, error) {
	options, err := s.repos.Poll.GetOptions(ctx, messageID)
	if err != nil {
		return nil, nil, err
	}
	votes, err := s.repos.Poll.GetVotes(ctx, messageID)
	if err != nil {
		return nil, nil, err
	}
	return options, votes, nil
}

func (s *ChatService) GetMessageByID(ctx context.Context, chatID uuid.UUID, messageID string) (*domain.Message, error) {
	return s.repos.Message.GetByMessageID(ctx, chatID, messageID)
}

func (s *ChatService) MarkAsRead(ctx context.Context, chatID uuid.UUID) error {
	return s.repos.Chat.MarkAsRead(ctx, chatID)
}

func (s *ChatService) SendChatPresence(ctx context.Context, deviceID uuid.UUID, to string, composing bool, media string) error {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return err
	}
	return s.pool.SendChatPresence(ctx, deviceID, to, composing, media)
}

func (s *ChatService) SendReadReceipt(ctx context.Context, deviceID uuid.UUID, chatJID, senderJID string, messageIDs []string) error {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return err
	}
	return s.pool.SendReadReceipt(ctx, deviceID, chatJID, senderJID, messageIDs)
}

func (s *ChatService) IsOnWhatsApp(ctx context.Context, deviceID uuid.UUID, phones []string) ([]domain.WhatsAppCheckResult, error) {
	return s.pool.IsOnWhatsApp(ctx, deviceID, phones)
}

func (s *ChatService) RevokeMessage(ctx context.Context, deviceID uuid.UUID, chatJID, senderJID, messageID string, isFromMe bool) error {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return err
	}
	return s.pool.RevokeMessage(ctx, deviceID, chatJID, senderJID, messageID, isFromMe)
}

func (s *ChatService) EditMessage(ctx context.Context, deviceID uuid.UUID, chatJID, messageID, newBody string) error {
	if err := s.ensureWhatsAppWebOutbound(ctx, deviceID); err != nil {
		return err
	}
	return s.pool.EditMessage(ctx, deviceID, chatJID, messageID, newBody)
}

func (s *ChatService) Delete(ctx context.Context, accountID, chatID uuid.UUID) error {
	return s.repos.Chat.Delete(ctx, accountID, chatID)
}

func (s *ChatService) DeleteBatch(ctx context.Context, accountID uuid.UUID, ids []uuid.UUID) error {
	return s.repos.Chat.DeleteBatch(ctx, accountID, ids)
}

func (s *ChatService) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	return s.repos.Chat.DeleteAll(ctx, accountID)
}

func (s *ChatService) GetContacts(ctx context.Context, accountID uuid.UUID) ([]*domain.Contact, error) {
	return s.repos.Contact.GetByAccountID(ctx, accountID)
}

func (s *ChatService) GetRecentStickers(ctx context.Context, accountID uuid.UUID) ([]string, error) {
	return s.repos.Message.GetRecentStickers(ctx, accountID, 50)
}

func (s *ChatService) GetSavedStickers(ctx context.Context, accountID uuid.UUID) ([]string, error) {
	return s.repos.SavedSticker.GetAll(ctx, accountID)
}

func (s *ChatService) SaveSticker(ctx context.Context, accountID uuid.UUID, mediaURL string) error {
	return s.repos.SavedSticker.Save(ctx, accountID, mediaURL)
}

func (s *ChatService) DeleteSavedSticker(ctx context.Context, accountID uuid.UUID, mediaURL string) error {
	return s.repos.SavedSticker.Delete(ctx, accountID, mediaURL)
}

// ContactService handles contact operations
type ContactService struct {
	repos *repository.Repositories
	pool  *whatsapp.DevicePool
}

func (s *ContactService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Contact, error) {
	return s.repos.Contact.GetByAccountID(ctx, accountID)
}

func (s *ContactService) GetOrCreate(ctx context.Context, accountID uuid.UUID, deviceID *uuid.UUID, jid, phone, name, pushName string, isGroup bool) (*domain.Contact, error) {
	return s.repos.Contact.GetOrCreate(ctx, accountID, deviceID, jid, phone, name, pushName, isGroup)
}

func (s *ContactService) GetByAccountIDWithFilters(ctx context.Context, accountID uuid.UUID, filter domain.ContactFilter) ([]*domain.Contact, int, error) {
	return s.repos.Contact.GetByAccountIDWithFilters(ctx, accountID, filter)
}

func (s *ContactService) GetByID(ctx context.Context, contactID uuid.UUID) (*domain.Contact, error) {
	contact, err := s.repos.Contact.GetByID(ctx, contactID)
	if err != nil || contact == nil {
		return nil, err
	}

	// Load device names
	deviceNames, err := s.repos.ContactDeviceName.GetByContactID(ctx, contactID)
	if err == nil {
		contact.DeviceNames = deviceNames
	}

	return contact, nil
}

func (s *ContactService) Update(ctx context.Context, contact *domain.Contact) error {
	return s.repos.Contact.Update(ctx, contact)
}

func (s *ContactService) SyncToParticipants(ctx context.Context, contact *domain.Contact) error {
	return s.repos.Contact.SyncToParticipants(ctx, contact)
}

func (s *ContactService) SyncToLead(ctx context.Context, contact *domain.Contact) error {
	return s.repos.Contact.SyncToLead(ctx, contact)
}

func (s *ContactService) Delete(ctx context.Context, accountID, id uuid.UUID) error {
	return s.repos.Contact.Delete(ctx, accountID, id)
}

func (s *ContactService) DeleteBatch(ctx context.Context, accountID uuid.UUID, ids []uuid.UUID) error {
	return s.repos.Contact.DeleteBatch(ctx, accountID, ids)
}

func (s *ContactService) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	return s.repos.Contact.DeleteAll(ctx, accountID)
}

func (s *ContactService) FindDuplicates(ctx context.Context, accountID uuid.UUID) ([][]*domain.Contact, error) {
	return s.repos.Contact.FindDuplicates(ctx, accountID)
}

func (s *ContactService) GetDuplicateLeadsCount(ctx context.Context, accountID uuid.UUID) (int, error) {
	return s.repos.Contact.GetContactsWithDuplicateLeads(ctx, accountID)
}

func (s *ContactService) MergeContacts(ctx context.Context, keepID uuid.UUID, mergeIDs []uuid.UUID) error {
	return s.repos.Contact.MergeContacts(ctx, keepID, mergeIDs)
}

func (s *ContactService) ResetFromDevice(ctx context.Context, contactID uuid.UUID) error {
	contact, err := s.repos.Contact.GetByID(ctx, contactID)
	if err != nil || contact == nil {
		return fmt.Errorf("contact not found")
	}

	// Get latest device name
	deviceNames, err := s.repos.ContactDeviceName.GetByContactID(ctx, contactID)
	if err != nil || len(deviceNames) == 0 {
		return fmt.Errorf("no device names available to reset from")
	}

	// Use the first (most recent) device name
	latest := deviceNames[0]
	contact.CustomName = nil // Clear custom name
	if latest.Name != nil {
		contact.Name = latest.Name
	}
	if latest.PushName != nil {
		contact.PushName = latest.PushName
	}

	return s.repos.Contact.Update(ctx, contact)
}

func (s *ContactService) SyncDevice(ctx context.Context, deviceID uuid.UUID) error {
	return s.pool.SyncDeviceContacts(ctx, deviceID)
}

// LeadService handles lead operations
type LeadService struct {
	repos *repository.Repositories
}

func (s *LeadService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Lead, error) {
	return s.repos.Lead.GetByAccountID(ctx, accountID)
}

func (s *LeadService) GetByID(ctx context.Context, leadID uuid.UUID) (*domain.Lead, error) {
	return s.repos.Lead.GetByID(ctx, leadID)
}

func (s *LeadService) Create(ctx context.Context, lead *domain.Lead) error {
	return s.repos.Lead.Create(ctx, lead)
}

func (s *LeadService) Update(ctx context.Context, lead *domain.Lead) error {
	return s.repos.Lead.Update(ctx, lead)
}

func (s *LeadService) SyncToContact(ctx context.Context, lead *domain.Lead) error {
	return s.repos.Lead.SyncToContact(ctx, lead)
}

func (s *LeadService) UpdateStatus(ctx context.Context, leadID uuid.UUID, status string) error {
	return s.repos.Lead.UpdateStatus(ctx, leadID, status)
}

func (s *LeadService) UpdateStage(ctx context.Context, leadID uuid.UUID, stageID uuid.UUID) error {
	return s.repos.Lead.UpdateStage(ctx, leadID, stageID)
}

func (s *LeadService) GetByJID(ctx context.Context, accountID uuid.UUID, jid string) (*domain.Lead, error) {
	return s.repos.Lead.GetByJID(ctx, accountID, jid)
}

func (s *LeadService) Delete(ctx context.Context, accountID, id uuid.UUID) error {
	return s.repos.Lead.Delete(ctx, accountID, id)
}

func (s *LeadService) DeleteBatch(ctx context.Context, accountID uuid.UUID, ids []uuid.UUID) error {
	return s.repos.Lead.DeleteBatch(ctx, accountID, ids)
}

func (s *LeadService) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	return s.repos.Lead.DeleteAll(ctx, accountID)
}

func (s *LeadService) ArchiveLead(ctx context.Context, id uuid.UUID, archive bool, reason string) error {
	if err := s.repos.Lead.ArchiveLead(ctx, id, archive, reason); err != nil {
		return err
	}

	// After archiving, clean up tags and event participants
	if archive {
		// Get the contact linked to this lead
		contactID, err := s.repos.Lead.GetContactIDForLead(ctx, id)
		if err == nil && contactID != nil {
			// Recalculate contact tags — remove tags that only came from this lead
			if err := s.repos.Tag.RecalculateContactTags(ctx, *contactID); err != nil {
				log.Printf("[LEAD] Error recalculating contact tags after archive: %v", err)
			}
		}
		// Remove auto-synced event participants for this lead across all events
		removed, err := s.repos.Event.RemoveAutoSyncParticipantsByLeadID(ctx, id)
		if err != nil {
			log.Printf("[LEAD] Error removing event participants after archive: %v", err)
		} else if removed > 0 {
			log.Printf("[LEAD] Removed %d auto-sync event participants for archived lead %s", removed, id)
		}
	}
	return nil
}

func (s *LeadService) ArchiveLeadsBatch(ctx context.Context, ids []uuid.UUID, archive bool, reason string) error {
	if err := s.repos.Lead.ArchiveLeadsBatch(ctx, ids, archive, reason); err != nil {
		return err
	}

	// After batch archive, clean up tags and event participants
	if archive {
		for _, id := range ids {
			contactID, err := s.repos.Lead.GetContactIDForLead(ctx, id)
			if err == nil && contactID != nil {
				if err := s.repos.Tag.RecalculateContactTags(ctx, *contactID); err != nil {
					log.Printf("[LEAD] Error recalculating contact tags after batch archive: %v", err)
				}
			}
			if removed, err := s.repos.Event.RemoveAutoSyncParticipantsByLeadID(ctx, id); err != nil {
				log.Printf("[LEAD] Error removing event participants after batch archive: %v", err)
			} else if removed > 0 {
				log.Printf("[LEAD] Removed %d auto-sync event participants for archived lead %s", removed, id)
			}
		}
	}
	return nil
}

func (s *LeadService) BlockLead(ctx context.Context, id uuid.UUID, block bool, reason string) error {
	return s.repos.Lead.BlockLead(ctx, id, block, reason)
}

func (s *LeadService) BlockLeadsBatch(ctx context.Context, ids []uuid.UUID, block bool, reason string) error {
	return s.repos.Lead.BlockLeadsBatch(ctx, ids, block, reason)
}

func (s *LeadService) GetArchivedBlockedCounts(ctx context.Context, accountID uuid.UUID) (int, int, int, error) {
	return s.repos.Lead.GetArchivedBlockedCounts(ctx, accountID)
}

// PipelineService handles pipeline operations
type PipelineService struct {
	repos *repository.Repositories
}

func (s *PipelineService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Pipeline, error) {
	return s.repos.Pipeline.GetByAccountID(ctx, accountID)
}

func (s *PipelineService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Pipeline, error) {
	return s.repos.Pipeline.GetByID(ctx, id)
}

func (s *PipelineService) GetDefaultPipeline(ctx context.Context, accountID uuid.UUID) (*domain.Pipeline, error) {
	return s.repos.Pipeline.GetDefaultPipeline(ctx, accountID)
}

func (s *PipelineService) Create(ctx context.Context, pipeline *domain.Pipeline) error {
	return s.repos.Pipeline.Create(ctx, pipeline)
}

func (s *PipelineService) Update(ctx context.Context, pipeline *domain.Pipeline) error {
	return s.repos.Pipeline.Update(ctx, pipeline)
}

func (s *PipelineService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Pipeline.Delete(ctx, id)
}

func (s *PipelineService) DeleteForAccount(ctx context.Context, id, accountID uuid.UUID) error {
	return s.repos.Pipeline.DeleteForAccount(ctx, id, accountID)
}

func (s *PipelineService) CreateStage(ctx context.Context, stage *domain.PipelineStage) error {
	return s.repos.Pipeline.CreateStage(ctx, stage)
}

func (s *PipelineService) UpdateStage(ctx context.Context, stage *domain.PipelineStage) error {
	return s.repos.Pipeline.UpdateStage(ctx, stage)
}

func (s *PipelineService) DeleteStage(ctx context.Context, id uuid.UUID) error {
	return s.repos.Pipeline.DeleteStage(ctx, id)
}

func (s *PipelineService) ReorderStages(ctx context.Context, pipelineID uuid.UUID, stageIDs []uuid.UUID) error {
	return s.repos.Pipeline.ReorderStages(ctx, pipelineID, stageIDs)
}

// TagService handles tag operations
type TagService struct {
	repos *repository.Repositories
}

func (s *TagService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Tag, error) {
	return s.repos.Tag.GetByAccountID(ctx, accountID)
}

func (s *TagService) ListPaginated(ctx context.Context, accountID uuid.UUID, search string, limit, offset int) ([]*domain.Tag, int, error) {
	return s.repos.Tag.ListPaginated(ctx, accountID, search, limit, offset)
}

func (s *TagService) Create(ctx context.Context, tag *domain.Tag) error {
	return s.repos.Tag.Create(ctx, tag)
}

func (s *TagService) Update(ctx context.Context, tag *domain.Tag) error {
	return s.repos.Tag.Update(ctx, tag)
}

func (s *TagService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Tag.Delete(ctx, id)
}

func (s *TagService) DeleteAll(ctx context.Context, accountID uuid.UUID) error {
	return s.repos.Tag.DeleteAll(ctx, accountID)
}

func (s *TagService) Assign(ctx context.Context, entityType string, entityID, tagID uuid.UUID) error {
	switch entityType {
	case "contact":
		return s.repos.Tag.AssignToContact(ctx, entityID, tagID)
	case "lead":
		return s.repos.Tag.AssignToLead(ctx, entityID, tagID)
	case "chat":
		return s.repos.Tag.AssignToChat(ctx, entityID, tagID)
	case "participant":
		return s.repos.Tag.AssignToParticipant(ctx, entityID, tagID)
	default:
		return fmt.Errorf("invalid entity type: %s", entityType)
	}
}

func (s *TagService) Remove(ctx context.Context, entityType string, entityID, tagID uuid.UUID) error {
	switch entityType {
	case "contact":
		return s.repos.Tag.RemoveFromContact(ctx, entityID, tagID)
	case "lead":
		return s.repos.Tag.RemoveFromLead(ctx, entityID, tagID)
	case "chat":
		return s.repos.Tag.RemoveFromChat(ctx, entityID, tagID)
	case "participant":
		return s.repos.Tag.RemoveFromParticipant(ctx, entityID, tagID)
	default:
		return fmt.Errorf("invalid entity type: %s", entityType)
	}
}

func (s *TagService) GetByEntity(ctx context.Context, entityType string, entityID uuid.UUID) ([]*domain.Tag, error) {
	switch entityType {
	case "contact":
		return s.repos.Tag.GetByContact(ctx, entityID)
	case "lead":
		return s.repos.Tag.GetByLead(ctx, entityID)
	case "chat":
		return s.repos.Tag.GetByChat(ctx, entityID)
	case "participant":
		return s.repos.Tag.GetByParticipant(ctx, entityID)
	default:
		return nil, fmt.Errorf("invalid entity type: %s", entityType)
	}
}

// CampaignService handles campaign operations
type CampaignService struct {
	repos      *repository.Repositories
	pool       *whatsapp.DevicePool
	hub        *ws.Hub
	mediaCache sync.Map // map[string]*whatsapp.PreUploadedMedia — keyed by mediaURL
}

func (s *CampaignService) Create(ctx context.Context, campaign *domain.Campaign) error {
	return s.repos.Campaign.Create(ctx, campaign)
}

func (s *CampaignService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.Campaign, error) {
	return s.repos.Campaign.GetByAccountID(ctx, accountID)
}

func (s *CampaignService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Campaign, error) {
	return s.repos.Campaign.GetByID(ctx, id)
}

func (s *CampaignService) Update(ctx context.Context, campaign *domain.Campaign) error {
	return s.repos.Campaign.Update(ctx, campaign)
}

func (s *CampaignService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Campaign.Delete(ctx, id)
}

func (s *CampaignService) AddRecipients(ctx context.Context, recipients []*domain.CampaignRecipient) error {
	return s.repos.Campaign.AddRecipients(ctx, recipients)
}

func (s *CampaignService) GetRecipients(ctx context.Context, campaignID uuid.UUID) ([]*domain.CampaignRecipient, error) {
	return s.repos.Campaign.GetRecipients(ctx, campaignID)
}

func (s *CampaignService) DeleteRecipient(ctx context.Context, campaignID, recipientID uuid.UUID) error {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("campaign not found")
	}
	if campaign.Status != domain.CampaignStatusDraft && campaign.Status != domain.CampaignStatusScheduled {
		return fmt.Errorf("can only remove recipients from draft or scheduled campaigns")
	}
	return s.repos.Campaign.DeleteRecipient(ctx, campaignID, recipientID)
}

func (s *CampaignService) UpdateRecipientData(ctx context.Context, campaignID, recipientID uuid.UUID, name *string, phone *string, metadata map[string]interface{}) (*domain.CampaignRecipient, error) {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return nil, fmt.Errorf("campaign not found")
	}
	if campaign.Status != domain.CampaignStatusDraft && campaign.Status != domain.CampaignStatusScheduled {
		return nil, fmt.Errorf("can only edit recipients in draft or scheduled campaigns")
	}
	return s.repos.Campaign.UpdateRecipientData(ctx, campaignID, recipientID, name, phone, metadata)
}

func (s *CampaignService) Start(ctx context.Context, campaignID uuid.UUID, startedBy *uuid.UUID) error {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return err
	}
	if campaign.Status != domain.CampaignStatusDraft && campaign.Status != domain.CampaignStatusPaused && campaign.Status != domain.CampaignStatusScheduled {
		return fmt.Errorf("campaign cannot be started from status: %s", campaign.Status)
	}
	now := time.Now()
	campaign.Status = domain.CampaignStatusRunning
	campaign.StartedAt = &now
	campaign.StartedBy = startedBy
	return s.repos.Campaign.Update(ctx, campaign)
}

func (s *CampaignService) Pause(ctx context.Context, campaignID uuid.UUID) error {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return err
	}
	if campaign.Status != domain.CampaignStatusRunning {
		return fmt.Errorf("campaign is not running")
	}
	campaign.Status = domain.CampaignStatusPaused
	return s.repos.Campaign.Update(ctx, campaign)
}

func (s *CampaignService) Cancel(ctx context.Context, campaignID uuid.UUID) error {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return err
	}
	switch campaign.Status {
	case domain.CampaignStatusDraft, domain.CampaignStatusScheduled, domain.CampaignStatusRunning, domain.CampaignStatusPaused:
		campaign.Status = domain.CampaignStatusCancelled
		now := time.Now()
		campaign.CompletedAt = &now
		return s.repos.Campaign.Update(ctx, campaign)
	default:
		return fmt.Errorf("campaign cannot be cancelled in status: %s", campaign.Status)
	}
}

func (s *CampaignService) GetRunningCampaigns(ctx context.Context) ([]*domain.Campaign, error) {
	return s.repos.Campaign.GetRunningCampaigns(ctx)
}

func (s *CampaignService) RetryRecipient(ctx context.Context, campaignID uuid.UUID, recipientID uuid.UUID) error {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("campaign not found: %w", err)
	}

	rec, err := s.repos.Campaign.GetRecipientByID(ctx, recipientID)
	if err != nil {
		return fmt.Errorf("recipient not found: %w", err)
	}
	if rec.CampaignID != campaignID {
		return fmt.Errorf("recipient does not belong to this campaign")
	}
	if rec.Status != "failed" {
		return fmt.Errorf("recipient status is '%s', only 'failed' can be retried", rec.Status)
	}

	// Look up contact and lead for template personalization
	var contact *domain.Contact
	if rec.ContactID != nil {
		contact, _ = s.repos.Contact.GetByID(ctx, *rec.ContactID)
	}
	var lead *domain.Lead
	if rec.JID != "" {
		lead, _ = s.repos.Lead.GetByJID(ctx, campaign.AccountID, rec.JID)
	}

	msg := personalizeText(campaign.MessageTemplate, rec, contact, lead)

	var sendErr error
	attachments, _ := s.repos.CampaignAttachment.GetByCampaignID(ctx, campaignID)

	if len(attachments) > 0 {
		if msg != "" {
			if len(attachments) == 1 && attachments[0].Caption == "" {
				_, sendErr = s.pool.SendMediaMessage(ctx, campaign.DeviceID, rec.JID, msg, attachments[0].MediaURL, attachments[0].MediaType)
			} else {
				_, sendErr = s.pool.SendMessage(ctx, campaign.DeviceID, rec.JID, msg)
				if sendErr == nil {
					for _, att := range attachments {
						time.Sleep(1500 * time.Millisecond)
						caption := personalizeText(att.Caption, rec, contact, lead)
						_, err := s.pool.SendMediaMessage(ctx, campaign.DeviceID, rec.JID, caption, att.MediaURL, att.MediaType)
						if err != nil {
							sendErr = err
							break
						}
					}
				}
			}
		} else {
			for i, att := range attachments {
				if i > 0 {
					time.Sleep(1500 * time.Millisecond)
				}
				caption := personalizeText(att.Caption, rec, contact, lead)
				_, err := s.pool.SendMediaMessage(ctx, campaign.DeviceID, rec.JID, caption, att.MediaURL, att.MediaType)
				if err != nil {
					sendErr = err
					break
				}
			}
		}
	} else if campaign.MediaURL != nil && *campaign.MediaURL != "" && campaign.MediaType != nil {
		_, sendErr = s.pool.SendMediaMessage(ctx, campaign.DeviceID, rec.JID, msg, *campaign.MediaURL, *campaign.MediaType)
	} else {
		_, sendErr = s.pool.SendMessage(ctx, campaign.DeviceID, rec.JID, msg)
	}

	if sendErr != nil {
		errMsg := sendErr.Error()
		s.repos.Campaign.UpdateRecipientStatus(ctx, rec.ID, "failed", &errMsg, nil)
		return fmt.Errorf("envío fallido: %s", errMsg)
	}

	// Mark as sent and update counters
	s.repos.Campaign.UpdateRecipientStatus(ctx, rec.ID, "sent", nil, nil)
	s.repos.Campaign.IncrementSentCount(ctx, campaignID)
	// Decrement failed count
	s.repos.Campaign.DecrementFailedCount(ctx, campaignID)

	return nil
}

func (s *CampaignService) Duplicate(ctx context.Context, campaignID uuid.UUID, newMessage *string) (*domain.Campaign, error) {
	original, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return nil, fmt.Errorf("campaign not found: %w", err)
	}

	newCampaign := &domain.Campaign{
		AccountID:       original.AccountID,
		DeviceID:        original.DeviceID,
		Name:            original.Name + " (copia)",
		MessageTemplate: original.MessageTemplate,
		MediaURL:        original.MediaURL,
		MediaType:       original.MediaType,
		Settings:        original.Settings,
		EventID:         original.EventID,
		Source:          original.Source,
	}
	if newMessage != nil && *newMessage != "" {
		newCampaign.MessageTemplate = *newMessage
	}

	if err := s.repos.Campaign.Create(ctx, newCampaign); err != nil {
		return nil, err
	}

	// Copy recipients with pending status
	origRecipients, err := s.repos.Campaign.GetRecipients(ctx, campaignID)
	if err != nil {
		return newCampaign, nil // campaign created but recipients failed to copy
	}

	var newRecipients []*domain.CampaignRecipient
	for _, r := range origRecipients {
		newRecipients = append(newRecipients, &domain.CampaignRecipient{
			CampaignID: newCampaign.ID,
			ContactID:  r.ContactID,
			JID:        r.JID,
			Name:       r.Name,
			Phone:      r.Phone,
			Status:     "pending",
			Metadata:   r.Metadata,
		})
	}
	if len(newRecipients) > 0 {
		s.repos.Campaign.AddRecipients(ctx, newRecipients)
	}

	// Copy attachments
	origAttachments, _ := s.repos.CampaignAttachment.GetByCampaignID(ctx, campaignID)
	if len(origAttachments) > 0 {
		var newAttachments []*domain.CampaignAttachment
		for _, a := range origAttachments {
			newAttachments = append(newAttachments, &domain.CampaignAttachment{
				MediaURL:  a.MediaURL,
				MediaType: a.MediaType,
				Caption:   a.Caption,
				FileName:  a.FileName,
				FileSize:  a.FileSize,
				Position:  a.Position,
			})
		}
		s.repos.CampaignAttachment.CreateBatch(ctx, newCampaign.ID, newAttachments)
	}

	// Re-fetch to get updated total_recipients
	newCampaign, _ = s.repos.Campaign.GetByID(ctx, newCampaign.ID)
	return newCampaign, nil
}

func personalizeText(text string, rec *domain.CampaignRecipient, contact *domain.Contact, lead *domain.Lead) string {
	if text == "" {
		return text
	}
	if rec.Name != nil && *rec.Name != "" {
		text = strings.Replace(text, "{{nombre}}", *rec.Name, -1)
		text = strings.Replace(text, "{{name}}", *rec.Name, -1)
	}
	if rec.Phone != nil {
		text = strings.Replace(text, "{{telefono}}", *rec.Phone, -1)
		text = strings.Replace(text, "{{phone}}", *rec.Phone, -1)
		text = strings.Replace(text, "{{celular}}", *rec.Phone, -1)
	}

	// Resolve nombre_corto: check recipient metadata first (event participant override),
	// then contact, then lead
	shortName := ""
	if rec.Metadata != nil {
		if v, ok := rec.Metadata["nombre_corto"]; ok {
			if s, ok := v.(string); ok && s != "" {
				shortName = s
			}
		}
	}
	if shortName == "" && contact != nil && contact.ShortName != nil && *contact.ShortName != "" {
		shortName = *contact.ShortName
	}
	if shortName == "" && lead != nil && lead.ShortName != nil && *lead.ShortName != "" {
		shortName = *lead.ShortName
	}
	if shortName != "" {
		text = strings.Replace(text, "{{nombre_corto}}", shortName, -1)
	}

	// Resolve nombre_completo: try contact first, then lead
	fullName := ""
	if contact != nil {
		if contact.CustomName != nil && *contact.CustomName != "" {
			fullName = *contact.CustomName
		} else {
			parts := []string{}
			if contact.Name != nil && *contact.Name != "" {
				parts = append(parts, *contact.Name)
			}
			if contact.LastName != nil && *contact.LastName != "" {
				parts = append(parts, *contact.LastName)
			}
			if len(parts) > 0 {
				fullName = strings.Join(parts, " ")
			}
		}
	}
	if fullName == "" && lead != nil {
		parts := []string{}
		if lead.Name != nil && *lead.Name != "" {
			parts = append(parts, *lead.Name)
		}
		if lead.LastName != nil && *lead.LastName != "" {
			parts = append(parts, *lead.LastName)
		}
		if len(parts) > 0 {
			fullName = strings.Join(parts, " ")
		}
	}
	if fullName != "" {
		text = strings.Replace(text, "{{nombre_completo}}", fullName, -1)
	}

	// Resolve custom metadata variables (e.g. {{empresa}}, {{ciudad}})
	if rec.Metadata != nil {
		for key, val := range rec.Metadata {
			if str, ok := val.(string); ok && str != "" {
				text = strings.Replace(text, "{{"+key+"}}", str, -1)
			}
		}
	}

	// Clean up any remaining unresolved placeholders (e.g. {{nombre_corto}} when no data exists)
	re := regexp.MustCompile(`\{\{[a-zA-Z0-9_]+\}\}`)
	text = re.ReplaceAllString(text, "")

	return text
}

// getOrUploadMedia returns cached pre-uploaded media or uploads it once.
func (s *CampaignService) getOrUploadMedia(ctx context.Context, deviceID uuid.UUID, mediaURL, mediaType string) (*whatsapp.PreUploadedMedia, error) {
	if val, ok := s.mediaCache.Load(mediaURL); ok {
		return val.(*whatsapp.PreUploadedMedia), nil
	}
	media, err := s.pool.UploadMedia(ctx, deviceID, mediaURL, mediaType)
	if err != nil {
		return nil, err
	}
	s.mediaCache.Store(mediaURL, media)
	log.Printf("[Campaign] Cached media upload: %s (%s)", mediaURL, mediaType)
	return media, nil
}

// sendWithRetry wraps a send function with retry logic for WhatsApp error 475 (anti-spam).
// Retries up to 3 times with exponential backoff: 10s, 20s, 40s.
func sendWithRetry(campaignID uuid.UUID, recipientJID string, sendFunc func() error) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ { // 1 initial + 3 retries
		err = sendFunc()
		if err == nil {
			return nil
		}
		// Only retry on error 475 (WhatsApp anti-spam rate limit)
		if !strings.Contains(err.Error(), "475") {
			return err
		}
		if attempt < 3 {
			backoff := time.Duration(10*(1<<attempt)) * time.Second // 10s, 20s, 40s
			log.Printf("[Campaign %s] Error 475 for %s, retrying in %v (attempt %d/3)", campaignID, recipientJID, backoff, attempt+1)
			time.Sleep(backoff)
		}
	}
	return err
}

func (s *CampaignService) ProcessNextRecipient(ctx context.Context, campaignID uuid.UUID, waitTimeMs *int) (bool, error) {
	campaign, err := s.repos.Campaign.GetByID(ctx, campaignID)
	if err != nil {
		return false, err
	}
	if campaign.Status != domain.CampaignStatusRunning {
		return false, nil
	}

	rec, err := s.repos.Campaign.GetNextPendingRecipient(ctx, campaignID)
	if err != nil {
		// No more recipients
		now := time.Now()
		campaign.Status = domain.CampaignStatusCompleted
		campaign.CompletedAt = &now
		s.repos.Campaign.Update(ctx, campaign)
		return false, nil
	}

	// Verify WhatsApp number before sending
	if rec.JID != "" && s.pool != nil {
		// Extract phone from JID (format: 51999999999@s.whatsapp.net)
		phone := strings.Split(rec.JID, "@")[0]
		results, verifyErr := s.pool.IsOnWhatsApp(ctx, campaign.DeviceID, []string{"+" + phone})
		if verifyErr != nil {
			log.Printf("[Campaign %s] WA verify error for %s: %v (proceeding with send)", campaignID, rec.JID, verifyErr)
		} else if len(results) > 0 && !results[0].IsOnWhatsApp {
			errMsg := "Número no disponible en WhatsApp"
			log.Printf("[Campaign %s] SKIPPED %s: %s", campaignID, rec.JID, errMsg)
			s.repos.Campaign.UpdateRecipientStatus(ctx, rec.ID, "failed", &errMsg, waitTimeMs)
			s.repos.Campaign.IncrementFailedCount(ctx, campaignID)
			return true, nil
		}
	}

	// Look up the full contact for more template variables
	var contact *domain.Contact
	if rec.ContactID != nil {
		contact, _ = s.repos.Contact.GetByID(context.Background(), *rec.ContactID)
	}

	// Also look up lead for additional data (nombre_corto, nombre_completo fallback)
	var lead *domain.Lead
	if rec.JID != "" {
		lead, _ = s.repos.Lead.GetByJID(ctx, campaign.AccountID, rec.JID)
	}

	// Personalize message
	msg := personalizeText(campaign.MessageTemplate, rec, contact, lead)

	// Send message with retry on error 475 and pre-uploaded media cache
	var sendErr error

	// Load attachments for this campaign
	attachments, _ := s.repos.CampaignAttachment.GetByCampaignID(ctx, campaignID)

	if len(attachments) > 0 {
		if msg != "" {
			if len(attachments) == 1 && attachments[0].Caption == "" {
				// Single attachment with text as caption — use pre-uploaded media
				media, uploadErr := s.getOrUploadMedia(ctx, campaign.DeviceID, attachments[0].MediaURL, attachments[0].MediaType)
				if uploadErr != nil {
					sendErr = uploadErr
				} else {
					sendErr = sendWithRetry(campaignID, rec.JID, func() error {
						_, err := s.pool.SendPreUploadedMediaMessage(ctx, campaign.DeviceID, rec.JID, msg, media)
						return err
					})
				}
			} else {
				// Text + multiple attachments: send text first, then each attachment
				sendErr = sendWithRetry(campaignID, rec.JID, func() error {
					_, err := s.pool.SendMessage(ctx, campaign.DeviceID, rec.JID, msg)
					return err
				})
				if sendErr == nil {
					for _, att := range attachments {
						time.Sleep(1500 * time.Millisecond)
						caption := personalizeText(att.Caption, rec, contact, lead)
						media, uploadErr := s.getOrUploadMedia(ctx, campaign.DeviceID, att.MediaURL, att.MediaType)
						if uploadErr != nil {
							sendErr = uploadErr
							break
						}
						sendErr = sendWithRetry(campaignID, rec.JID, func() error {
							_, err := s.pool.SendPreUploadedMediaMessage(ctx, campaign.DeviceID, rec.JID, caption, media)
							return err
						})
						if sendErr != nil {
							break
						}
					}
				}
			}
		} else {
			// No text, only attachments
			for i, att := range attachments {
				if i > 0 {
					time.Sleep(1500 * time.Millisecond)
				}
				caption := personalizeText(att.Caption, rec, contact, lead)
				media, uploadErr := s.getOrUploadMedia(ctx, campaign.DeviceID, att.MediaURL, att.MediaType)
				if uploadErr != nil {
					sendErr = uploadErr
					break
				}
				sendErr = sendWithRetry(campaignID, rec.JID, func() error {
					_, err := s.pool.SendPreUploadedMediaMessage(ctx, campaign.DeviceID, rec.JID, caption, media)
					return err
				})
				if sendErr != nil {
					break
				}
			}
		}
	} else if campaign.MediaURL != nil && *campaign.MediaURL != "" && campaign.MediaType != nil {
		// Legacy media field — also use cached upload
		media, uploadErr := s.getOrUploadMedia(ctx, campaign.DeviceID, *campaign.MediaURL, *campaign.MediaType)
		if uploadErr != nil {
			sendErr = uploadErr
		} else {
			sendErr = sendWithRetry(campaignID, rec.JID, func() error {
				_, err := s.pool.SendPreUploadedMediaMessage(ctx, campaign.DeviceID, rec.JID, msg, media)
				return err
			})
		}
	} else {
		// Text-only message
		sendErr = sendWithRetry(campaignID, rec.JID, func() error {
			_, err := s.pool.SendMessage(ctx, campaign.DeviceID, rec.JID, msg)
			return err
		})
	}

	if sendErr != nil {
		errMsg := sendErr.Error()
		log.Printf("[Campaign %s] FAILED %s: %s", campaignID, rec.JID, errMsg)
		s.repos.Campaign.UpdateRecipientStatus(ctx, rec.ID, "failed", &errMsg, waitTimeMs)
		s.repos.Campaign.IncrementFailedCount(ctx, campaignID)
	} else {
		log.Printf("[Campaign %s] SENT to %s", campaignID, rec.JID)
		s.repos.Campaign.UpdateRecipientStatus(ctx, rec.ID, "sent", nil, waitTimeMs)
		s.repos.Campaign.IncrementSentCount(ctx, campaignID)
	}

	return true, sendErr
}

// EventService handles event operations
type EventService struct {
	repos *repository.Repositories
	hub   *ws.Hub
}

func (s *EventService) Create(ctx context.Context, event *domain.Event) error {
	return s.repos.Event.Create(ctx, event)
}

func (s *EventService) DuplicateWithStageConfig(ctx context.Context, sourceID, accountID uuid.UUID, createdBy *uuid.UUID) (*domain.Event, error) {
	return s.repos.Event.DuplicateWithStageConfig(ctx, sourceID, accountID, createdBy)
}

func (s *EventService) GetByAccountID(ctx context.Context, accountID uuid.UUID, filter domain.EventFilter) ([]*domain.Event, int, error) {
	return s.repos.Event.GetByAccountID(ctx, accountID, filter)
}

func (s *EventService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	return s.repos.Event.GetByID(ctx, id)
}

func (s *EventService) Update(ctx context.Context, event *domain.Event) error {
	return s.repos.Event.Update(ctx, event)
}

func (s *EventService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Event.Delete(ctx, id)
}

func (s *EventService) GetByContactID(ctx context.Context, accountID, contactID uuid.UUID) ([]*domain.Event, error) {
	return s.repos.Event.GetByContactID(ctx, accountID, contactID)
}

func (s *EventService) GetParticipants(ctx context.Context, eventID uuid.UUID, search, status string, tagIDs []uuid.UUID, hasPhone *bool) ([]*domain.EventParticipant, error) {
	return s.repos.Participant.GetByEventID(ctx, eventID, search, status, tagIDs, hasPhone)
}

func (s *EventService) AddParticipant(ctx context.Context, p *domain.EventParticipant) error {
	return s.repos.Participant.Add(ctx, p)
}

func (s *EventService) BulkAddParticipants(ctx context.Context, eventID uuid.UUID, participants []*domain.EventParticipant) error {
	return s.repos.Participant.BulkAdd(ctx, eventID, participants)
}

func (s *EventService) GetParticipant(ctx context.Context, id uuid.UUID) (*domain.EventParticipant, error) {
	return s.repos.Participant.GetByID(ctx, id)
}

func (s *EventService) UpdateParticipant(ctx context.Context, p *domain.EventParticipant) error {
	return s.repos.Participant.Update(ctx, p)
}

func (s *EventService) SyncParticipantToContact(ctx context.Context, p *domain.EventParticipant) error {
	return s.repos.Participant.SyncToContact(ctx, p)
}

func (s *EventService) UpdateParticipantStatus(ctx context.Context, id uuid.UUID, status string) error {
	return s.repos.Participant.UpdateStatus(ctx, id, status)
}

func (s *EventService) BulkUpdateParticipantStatus(ctx context.Context, ids []uuid.UUID, status string) error {
	return s.repos.Participant.BulkUpdateStatus(ctx, ids, status)
}

func (s *EventService) DeleteParticipant(ctx context.Context, id uuid.UUID) error {
	return s.repos.Participant.Delete(ctx, id)
}

func (s *EventService) GetUpcomingActions(ctx context.Context, accountID uuid.UUID, limit int) ([]*domain.EventParticipant, error) {
	return s.repos.Participant.GetUpcomingActions(ctx, accountID, limit)
}

func (s *EventService) GetFolders(ctx context.Context, accountID uuid.UUID) ([]*domain.EventFolder, error) {
	return s.repos.EventFolder.GetByAccountID(ctx, accountID)
}

func (s *EventService) GetFolderByID(ctx context.Context, id uuid.UUID) (*domain.EventFolder, error) {
	return s.repos.EventFolder.GetByID(ctx, id)
}

func (s *EventService) CreateFolder(ctx context.Context, folder *domain.EventFolder) error {
	return s.repos.EventFolder.Create(ctx, folder)
}

func (s *EventService) UpdateFolder(ctx context.Context, folder *domain.EventFolder) error {
	return s.repos.EventFolder.Update(ctx, folder)
}

func (s *EventService) DeleteFolder(ctx context.Context, id uuid.UUID) error {
	return s.repos.EventFolder.Delete(ctx, id)
}

func (s *EventService) MoveEventToFolder(ctx context.Context, eventID uuid.UUID, folderID *uuid.UUID) error {
	return s.repos.EventFolder.MoveEvent(ctx, eventID, folderID)
}

// Pipeline methods
func (s *EventService) GetPipelines(ctx context.Context, accountID uuid.UUID) ([]*domain.EventPipeline, error) {
	return s.repos.EventPipeline.GetByAccountID(ctx, accountID)
}

func (s *EventService) GetPipeline(ctx context.Context, id uuid.UUID) (*domain.EventPipeline, error) {
	return s.repos.EventPipeline.GetByID(ctx, id)
}

func (s *EventService) GetDefaultPipeline(ctx context.Context, accountID uuid.UUID) (*domain.EventPipeline, error) {
	return s.repos.EventPipeline.GetDefaultByAccountID(ctx, accountID)
}

func (s *EventService) CreatePipeline(ctx context.Context, p *domain.EventPipeline) error {
	return s.repos.EventPipeline.Create(ctx, p)
}

func (s *EventService) UpdatePipeline(ctx context.Context, p *domain.EventPipeline) error {
	return s.repos.EventPipeline.Update(ctx, p)
}

func (s *EventService) DeletePipeline(ctx context.Context, id uuid.UUID) error {
	return s.repos.EventPipeline.Delete(ctx, id)
}

func (s *EventService) EnsureDedicatedPipelineForEvent(ctx context.Context, eventID, accountID, pipelineID uuid.UUID, eventName string) (uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	return s.repos.EventPipeline.EnsureDedicatedForEvent(ctx, eventID, accountID, pipelineID, eventName)
}

func (s *EventService) CreatePipelineStage(ctx context.Context, stage *domain.EventPipelineStage) error {
	return s.repos.EventPipeline.CreateStage(ctx, stage)
}

func (s *EventService) UpdatePipelineStage(ctx context.Context, pipelineID, stageID uuid.UUID, name, color *string) (*domain.EventPipelineStage, error) {
	return s.repos.EventPipeline.UpdateStage(ctx, pipelineID, stageID, name, color)
}

func (s *EventService) DeletePipelineStageForEvent(ctx context.Context, eventID, pipelineID, stageID uuid.UUID) error {
	return s.repos.EventPipeline.DeleteStageForEvent(ctx, eventID, pipelineID, stageID)
}

func (s *EventService) SavePipelineStageLayoutForEvent(ctx context.Context, eventID, pipelineID uuid.UUID, stages []repository.EventStageLayoutStage, deletions []repository.EventStageLayoutDeletion) error {
	return s.repos.EventPipeline.SaveStageLayoutForEvent(ctx, eventID, pipelineID, stages, deletions)
}

func (s *EventService) ReplaceStages(ctx context.Context, pipelineID uuid.UUID, stages []*domain.EventPipelineStage) error {
	return s.repos.EventPipeline.ReplaceStages(ctx, pipelineID, stages)
}

func (s *EventService) GetPipelineStages(ctx context.Context, pipelineID uuid.UUID) ([]*domain.EventPipelineStage, error) {
	return s.repos.EventPipeline.GetStagesByPipelineID(ctx, pipelineID)
}

func (s *EventService) GetParticipantCountsByStage(ctx context.Context, eventID uuid.UUID) (map[uuid.UUID]int, int, error) {
	return s.repos.EventPipeline.GetParticipantCountsByStage(ctx, eventID)
}

func (s *EventService) UpdateParticipantStage(ctx context.Context, id, stageID uuid.UUID) error {
	return s.repos.Participant.UpdateStage(ctx, id, stageID)
}

func (s *EventService) BulkUpdateParticipantStage(ctx context.Context, ids []uuid.UUID, stageID uuid.UUID) error {
	return s.repos.Participant.BulkUpdateStage(ctx, ids, stageID)
}

// ── Event Tag Auto-Sync Methods ──────────────────────────────────────────────

// SetEventTags sets the tags for automatic participant sync on an event (with formula support).
func (s *EventService) SetEventTags(ctx context.Context, eventID uuid.UUID, includes []uuid.UUID, excludes []uuid.UUID) error {
	return s.repos.Event.SetEventTags(ctx, eventID, includes, excludes)
}

// GetEventTags returns the tags configured for auto-sync on an event (with negate flag).
func (s *EventService) GetEventTags(ctx context.Context, eventID uuid.UUID) ([]*domain.Tag, error) {
	return s.repos.Event.GetEventTags(ctx, eventID)
}

// GetEventTagEntries returns include/exclude tag ID lists for an event.
func (s *EventService) GetEventTagEntries(ctx context.Context, eventID uuid.UUID) (includes []uuid.UUID, excludes []uuid.UUID, err error) {
	return s.repos.Event.GetEventTagEntries(ctx, eventID)
}

// ReconcileEventParticipants synchronizes participants for an event based on its configured tags and formula mode.
// Supports AND (lead must have ALL include tags), OR (lead has ANY include tag), plus exclude tags.
// Returns (added, removed, error).
func (s *EventService) ReconcileEventParticipants(ctx context.Context, eventID uuid.UUID, accountID uuid.UUID, mode string, includes []uuid.UUID, excludes []uuid.UUID, defaultStageID *uuid.UUID) (int, int, error) {
	if len(includes) == 0 {
		return 0, 0, nil
	}

	if mode == "" {
		mode = "OR"
	}

	// 1. Get lead IDs that match the formula
	matchedLeadIDs, err := s.repos.Event.GetLeadIDsByTagFormula(ctx, accountID, mode, includes, excludes)
	if err != nil {
		return 0, 0, fmt.Errorf("get leads by formula: %w", err)
	}

	return s.reconcileWithMatchedLeads(ctx, eventID, defaultStageID, matchedLeadIDs)
}

// ReconcileEventParticipantsAdvanced synchronizes participants using a text-based formula.
func (s *EventService) ReconcileEventParticipantsAdvanced(ctx context.Context, eventID uuid.UUID, accountID uuid.UUID, formulaText string, defaultStageID *uuid.UUID) (int, int, error) {
	if formulaText == "" {
		return 0, 0, nil
	}

	ast, err := formula.Parse(formulaText)
	if err != nil {
		return 0, 0, fmt.Errorf("parse formula: %w", err)
	}

	sql, args, err := formula.BuildSQL(ast, accountID)
	if err != nil {
		return 0, 0, fmt.Errorf("build formula SQL: %w", err)
	}

	matchedLeadIDs, err := s.repos.Event.GetLeadIDsByFormulaText(ctx, sql, args)
	if err != nil {
		return 0, 0, fmt.Errorf("execute formula query: %w", err)
	}

	log.Printf("[EVENT-SYNC] Advanced formula matched %d leads for event %s", len(matchedLeadIDs), eventID)

	return s.reconcileWithMatchedLeads(ctx, eventID, defaultStageID, matchedLeadIDs)
}

// reconcileWithMatchedLeads is the shared reconciliation logic for both simple and advanced formulas.
func (s *EventService) reconcileWithMatchedLeads(ctx context.Context, eventID uuid.UUID, defaultStageID *uuid.UUID, matchedLeadIDs []uuid.UUID) (int, int, error) {
	// Add missing leads as participants
	added, err := s.repos.Event.BulkAddParticipantsFromLeads(ctx, eventID, defaultStageID, matchedLeadIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("bulk add participants: %w", err)
	}

	// Get current auto_tag_sync participants
	currentAutoLeadIDs, err := s.repos.Event.GetAutoSyncParticipantLeadIDs(ctx, eventID)
	if err != nil {
		return added, 0, fmt.Errorf("get auto sync participants: %w", err)
	}

	// Find auto-sync participants whose leads no longer match
	matchedSet := make(map[uuid.UUID]bool, len(matchedLeadIDs))
	for _, id := range matchedLeadIDs {
		matchedSet[id] = true
	}
	var toRemove []uuid.UUID
	for _, lid := range currentAutoLeadIDs {
		if !matchedSet[lid] {
			toRemove = append(toRemove, lid)
		}
	}

	// Remove stale auto-sync participants
	removed, err := s.repos.Event.RemoveAutoSyncParticipantsByLeadIDs(ctx, eventID, toRemove)
	if err != nil {
		return added, 0, fmt.Errorf("remove stale participants: %w", err)
	}

	return added, removed, nil
}

// HandleLeadTagAssigned is called when a tag is assigned to a lead.
// It checks if any active events have this tag configured and, depending on formula mode,
// adds the lead as participant only if it matches the full formula.
func (s *EventService) HandleLeadTagAssigned(ctx context.Context, accountID, leadID, tagID uuid.UUID) {
	// Skip event sync for archived or blocked leads
	lead, err := s.repos.Lead.GetByID(ctx, leadID)
	if err == nil && lead != nil && (lead.IsArchived || lead.IsBlocked) {
		return
	}

	// 1. Handle simple-formula events that reference this tag
	events, err := s.repos.Event.FindActiveEventsByTagID(ctx, tagID)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error finding events for tag %s: %v", tagID, err)
		return
	}
	for _, ev := range events {
		if ev.TagFormulaType == "advanced" {
			continue // handled below
		}
		s.tryAddLeadToSimpleEvent(ctx, ev, leadID)
	}

	// 2. Handle advanced-formula events for the same account
	advEvents, err := s.repos.Event.GetActiveAdvancedFormulaEvents(ctx, accountID)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error finding advanced formula events: %v", err)
		return
	}
	for _, ev := range advEvents {
		s.tryAddLeadToAdvancedEvent(ctx, ev, leadID)
	}
}

// tryAddLeadToSimpleEvent checks the simple formula and adds the lead if it matches.
func (s *EventService) tryAddLeadToSimpleEvent(ctx context.Context, ev *domain.Event, leadID uuid.UUID) {
	exists, _ := s.repos.Event.ParticipantExistsForLead(ctx, ev.ID, leadID)
	if exists {
		return
	}

	includes, excludes, err := s.repos.Event.GetEventTagEntries(ctx, ev.ID)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error getting event tag entries for event %s: %v", ev.ID, err)
		return
	}

	matches, err := s.repos.Event.LeadMatchesFormula(ctx, leadID, ev.TagFormulaMode, includes, excludes)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error checking formula for lead %s event %s: %v", leadID, ev.ID, err)
		return
	}
	if !matches {
		return
	}

	stageID := s.getDefaultStageID(ctx, ev)
	added, err := s.repos.Event.BulkAddParticipantsFromLeads(ctx, ev.ID, stageID, []uuid.UUID{leadID})
	if err != nil {
		log.Printf("[EVENT-SYNC] Error adding lead %s to event %s: %v", leadID, ev.ID, err)
		return
	}
	if added > 0 {
		log.Printf("[EVENT-SYNC] ✅ Added lead %s to event '%s' (tag assigned, mode=%s)", leadID, ev.Name, ev.TagFormulaMode)
		if s.hub != nil {
			s.hub.BroadcastToAccount(ev.AccountID, "event_participant_update", map[string]interface{}{
				"event_id": ev.ID,
				"action":   "tag_sync_add",
			})
		}
	}
}

// tryAddLeadToAdvancedEvent checks the text formula and adds the lead if it matches.
func (s *EventService) tryAddLeadToAdvancedEvent(ctx context.Context, ev *domain.Event, leadID uuid.UUID) {
	exists, _ := s.repos.Event.ParticipantExistsForLead(ctx, ev.ID, leadID)
	if exists {
		return
	}

	tagNames, err := s.repos.Event.GetLeadTagNames(ctx, leadID)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error getting lead tags for %s: %v", leadID, err)
		return
	}

	ast, err := formula.Parse(ev.TagFormula)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error parsing formula for event %s: %v", ev.ID, err)
		return
	}

	if !formula.Evaluate(ast, tagNames) {
		return
	}

	stageID := s.getDefaultStageID(ctx, ev)
	added, err := s.repos.Event.BulkAddParticipantsFromLeads(ctx, ev.ID, stageID, []uuid.UUID{leadID})
	if err != nil {
		log.Printf("[EVENT-SYNC] Error adding lead %s to event %s: %v", leadID, ev.ID, err)
		return
	}
	if added > 0 {
		log.Printf("[EVENT-SYNC] ✅ Added lead %s to event '%s' (advanced formula)", leadID, ev.Name)
		if s.hub != nil {
			s.hub.BroadcastToAccount(ev.AccountID, "event_participant_update", map[string]interface{}{
				"event_id": ev.ID,
				"action":   "tag_sync_add",
			})
		}
	}
}

// getDefaultStageID returns the first stage ID for the event's pipeline.
func (s *EventService) getDefaultStageID(ctx context.Context, ev *domain.Event) *uuid.UUID {
	if ev.PipelineID != nil {
		stages, _ := s.repos.EventPipeline.GetStagesByPipelineID(ctx, *ev.PipelineID)
		if len(stages) > 0 {
			return &stages[0].ID
		}
	}
	return nil
}

// HandleLeadTagRemoved is called when a tag is removed from a lead.
// Checks if the lead should be removed from any event that uses this tag (formula-aware).
func (s *EventService) HandleLeadTagRemoved(ctx context.Context, accountID, leadID, tagID uuid.UUID) {
	// 1. Handle simple-formula events that reference this tag
	events, err := s.repos.Event.FindActiveEventsByTagID(ctx, tagID)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error finding events for tag %s: %v", tagID, err)
		return
	}
	for _, ev := range events {
		if ev.TagFormulaType == "advanced" {
			continue
		}
		s.tryRemoveLeadFromSimpleEvent(ctx, ev, leadID)
	}

	// 2. Handle advanced-formula events for the same account
	advEvents, err := s.repos.Event.GetActiveAdvancedFormulaEvents(ctx, accountID)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error finding advanced formula events: %v", err)
		return
	}
	for _, ev := range advEvents {
		s.tryRemoveLeadFromAdvancedEvent(ctx, ev, leadID)
	}
}

// tryRemoveLeadFromSimpleEvent checks if the lead still matches the simple formula.
func (s *EventService) tryRemoveLeadFromSimpleEvent(ctx context.Context, ev *domain.Event, leadID uuid.UUID) {
	includes, excludes, err := s.repos.Event.GetEventTagEntries(ctx, ev.ID)
	if err != nil {
		return
	}

	stillMatches, err := s.repos.Event.LeadMatchesFormula(ctx, leadID, ev.TagFormulaMode, includes, excludes)
	if err != nil || stillMatches {
		return
	}

	removed, err := s.repos.Event.RemoveAutoSyncParticipantsByLeadIDs(ctx, ev.ID, []uuid.UUID{leadID})
	if err != nil {
		log.Printf("[EVENT-SYNC] Error removing lead %s from event %s: %v", leadID, ev.ID, err)
		return
	}
	if removed > 0 {
		log.Printf("[EVENT-SYNC] ❌ Removed lead %s from event '%s' (tag removed, mode=%s)", leadID, ev.Name, ev.TagFormulaMode)
		if s.hub != nil {
			s.hub.BroadcastToAccount(ev.AccountID, "event_participant_update", map[string]interface{}{
				"event_id": ev.ID,
				"action":   "tag_sync_remove",
			})
		}
	}
}

// tryRemoveLeadFromAdvancedEvent checks if the lead still matches the text formula.
func (s *EventService) tryRemoveLeadFromAdvancedEvent(ctx context.Context, ev *domain.Event, leadID uuid.UUID) {
	tagNames, err := s.repos.Event.GetLeadTagNames(ctx, leadID)
	if err != nil {
		return
	}

	ast, err := formula.Parse(ev.TagFormula)
	if err != nil {
		return
	}

	if formula.Evaluate(ast, tagNames) {
		return // still matches
	}

	removed, err := s.repos.Event.RemoveAutoSyncParticipantsByLeadIDs(ctx, ev.ID, []uuid.UUID{leadID})
	if err != nil {
		log.Printf("[EVENT-SYNC] Error removing lead %s from event %s: %v", leadID, ev.ID, err)
		return
	}
	if removed > 0 {
		log.Printf("[EVENT-SYNC] ❌ Removed lead %s from event '%s' (advanced formula)", leadID, ev.Name)
		if s.hub != nil {
			s.hub.BroadcastToAccount(ev.AccountID, "event_participant_update", map[string]interface{}{
				"event_id": ev.ID,
				"action":   "tag_sync_remove",
			})
		}
	}
}

// InteractionService handles interaction operations
// ReconcileAllAccountEvents reconciles participants for ALL active events in an account.
// Called after bulk tag changes (Kommo sync, CSV import, tag deletion).
func (s *EventService) ReconcileAllAccountEvents(ctx context.Context, accountID uuid.UUID) {
	eventsWithTags, err := s.repos.Event.GetActiveEventsWithTags(ctx)
	if err != nil {
		log.Printf("[EVENT-SYNC] Error fetching events for account %s: %v", accountID, err)
		return
	}
	for _, ewt := range eventsWithTags {
		if ewt.Event.AccountID != accountID {
			continue
		}
		var stageID *uuid.UUID
		if ewt.Event.PipelineID != nil {
			stages, _ := s.GetPipelineStages(ctx, *ewt.Event.PipelineID)
			if len(stages) > 0 {
				stageID = &stages[0].ID
			}
		}
		var added, removed int
		var reconcileErr error
		if ewt.Event.TagFormulaType == "advanced" && ewt.Event.TagFormula != "" {
			added, removed, reconcileErr = s.ReconcileEventParticipantsAdvanced(ctx, ewt.Event.ID, ewt.Event.AccountID, ewt.Event.TagFormula, stageID)
		} else if len(ewt.Includes) > 0 {
			added, removed, reconcileErr = s.ReconcileEventParticipants(ctx, ewt.Event.ID, ewt.Event.AccountID, ewt.Event.TagFormulaMode, ewt.Includes, ewt.Excludes, stageID)
		}
		if reconcileErr != nil {
			log.Printf("[EVENT-SYNC] Error reconciling event '%s': %v", ewt.Event.Name, reconcileErr)
			continue
		}
		if added > 0 || removed > 0 {
			log.Printf("[EVENT-SYNC] Account %s event '%s': +%d added, -%d removed", accountID, ewt.Event.Name, added, removed)
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

type InteractionService struct {
	repos *repository.Repositories
	hub   *ws.Hub
}

func (s *InteractionService) LogInteraction(ctx context.Context, interaction *domain.Interaction) error {
	if err := s.repos.Interaction.Create(ctx, interaction); err != nil {
		return err
	}

	// Auto-update participant status based on outcome
	if interaction.ParticipantID != nil && interaction.Outcome != nil {
		switch *interaction.Outcome {
		case domain.InteractionOutcomeConfirmed:
			s.repos.Participant.UpdateStatus(ctx, *interaction.ParticipantID, domain.ParticipantStatusConfirmed)
		case domain.InteractionOutcomeDeclined:
			s.repos.Participant.UpdateStatus(ctx, *interaction.ParticipantID, domain.ParticipantStatusDeclined)
		case domain.InteractionOutcomeAnswered, domain.InteractionOutcomeCallback, domain.InteractionOutcomeRescheduled:
			// Move to contacted if still invited
			p, _ := s.repos.Participant.GetByID(ctx, *interaction.ParticipantID)
			if p != nil && p.Status == domain.ParticipantStatusInvited {
				s.repos.Participant.UpdateStatus(ctx, *interaction.ParticipantID, domain.ParticipantStatusContacted)
			}
		}
	}

	// Update next_action on participant if provided
	if interaction.ParticipantID != nil && (interaction.NextAction != nil || interaction.NextActionDate != nil) {
		p, _ := s.repos.Participant.GetByID(ctx, *interaction.ParticipantID)
		if p != nil {
			p.NextAction = interaction.NextAction
			p.NextActionDate = interaction.NextActionDate
			s.repos.Participant.Update(ctx, p)
		}
	}

	return nil
}

func (s *InteractionService) GetByParticipantID(ctx context.Context, participantID uuid.UUID) ([]*domain.Interaction, error) {
	return s.repos.Interaction.GetByParticipantID(ctx, participantID)
}

func (s *InteractionService) GetByContactID(ctx context.Context, contactID uuid.UUID, limit, offset int) ([]*domain.Interaction, error) {
	return s.repos.Interaction.GetByContactID(ctx, contactID, limit, offset)
}

func (s *InteractionService) GetByEventID(ctx context.Context, eventID uuid.UUID, limit, offset int) ([]*domain.Interaction, error) {
	return s.repos.Interaction.GetByEventID(ctx, eventID, limit, offset)
}

func (s *InteractionService) GetByLeadID(ctx context.Context, leadID uuid.UUID, limit, offset int) ([]*domain.Interaction, error) {
	return s.repos.Interaction.GetByLeadID(ctx, leadID, limit, offset)
}

func (s *InteractionService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Interaction.Delete(ctx, id)
}

// QuickReplyService handles quick reply / canned response operations
type QuickReplyService struct {
	repos *repository.Repositories
}

func (s *QuickReplyService) GetByAccountID(ctx context.Context, accountID uuid.UUID) ([]*domain.QuickReply, error) {
	return s.repos.QuickReply.GetByAccountID(ctx, accountID)
}

func (s *QuickReplyService) GetByID(ctx context.Context, id uuid.UUID) (*domain.QuickReply, error) {
	return s.repos.QuickReply.GetByID(ctx, id)
}

func (s *QuickReplyService) Create(ctx context.Context, qr *domain.QuickReply) error {
	return s.repos.QuickReply.Create(ctx, qr)
}

func (s *QuickReplyService) Update(ctx context.Context, qr *domain.QuickReply) error {
	return s.repos.QuickReply.Update(ctx, qr)
}

func (s *QuickReplyService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.QuickReply.Delete(ctx, id)
}

// RoleService handles RBAC role management
type RoleService struct {
	repos *repository.Repositories
}

func (s *RoleService) GetAll(ctx context.Context) ([]*domain.Role, error) {
	return s.repos.Role.GetAll(ctx)
}

func (s *RoleService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	return s.repos.Role.GetByID(ctx, id)
}

func (s *RoleService) Create(ctx context.Context, role *domain.Role) error {
	return s.repos.Role.Create(ctx, role)
}

func (s *RoleService) Update(ctx context.Context, role *domain.Role) error {
	return s.repos.Role.Update(ctx, role)
}

func (s *RoleService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repos.Role.Delete(ctx, id)
}
