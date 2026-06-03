package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const turnstileSiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type abuseLimit struct {
	Key    string
	Max    int64
	Window time.Duration
}

type inMemoryAbuseLimiter struct {
	mu      sync.Mutex
	buckets map[string]inMemoryAbuseBucket
}

type inMemoryAbuseBucket struct {
	Count     int64
	ExpiresAt time.Time
}

type turnstileVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
}

func newInMemoryAbuseLimiter() *inMemoryAbuseLimiter {
	return &inMemoryAbuseLimiter{buckets: make(map[string]inMemoryAbuseBucket)}
}

func (s *Server) handlePublicSecurityConfig(c *fiber.Ctx) error {
	configured := s.turnstileConfigured()
	required := s.turnstileRequired()
	return c.JSON(fiber.Map{
		"success":                  true,
		"login_enabled":            !required || configured,
		"turnstile_site_key":       strings.TrimSpace(s.cfg.TurnstileSiteKey),
		"login_turnstile_required": required,
		"has_turnstile_secret":     strings.TrimSpace(s.cfg.TurnstileSecretKey) != "",
	})
}

func (s *Server) handleRegisterDisabled(c *fiber.Ctx) error {
	s.recordSecurityEvent(c.Context(), "public_register_blocked", "", c, nil)
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"success": false,
		"error":   "El registro publico no esta habilitado",
	})
}

func (s *Server) turnstileConfigured() bool {
	return strings.TrimSpace(s.cfg.TurnstileSiteKey) != "" && strings.TrimSpace(s.cfg.TurnstileSecretKey) != ""
}

func (s *Server) turnstileRequired() bool {
	return s.cfg.IsProduction() || s.turnstileConfigured()
}

func (s *Server) validateTurnstileLogin(c *fiber.Ctx, username, token string) error {
	if !s.turnstileRequired() {
		return nil
	}
	if !s.turnstileConfigured() {
		s.recordSecurityEvent(c.Context(), "turnstile_not_configured", username, c, nil)
		return fiber.NewError(fiber.StatusServiceUnavailable, "Inicio de sesión temporalmente no disponible")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		s.recordSecurityEvent(c.Context(), "turnstile_missing", username, c, nil)
		return fiber.NewError(fiber.StatusBadRequest, "Completa la verificación de seguridad")
	}
	if len(token) > 2048 {
		s.recordSecurityEvent(c.Context(), "turnstile_token_too_long", username, c, map[string]interface{}{"length": len(token)})
		return fiber.NewError(fiber.StatusBadRequest, "Verificación inválida")
	}

	body, _ := json.Marshal(map[string]string{
		"secret":          s.cfg.TurnstileSecretKey,
		"response":        token,
		"remoteip":        clientIP(c),
		"idempotency_key": uuid.NewString(),
	})
	req, err := http.NewRequestWithContext(c.Context(), http.MethodPost, turnstileSiteVerifyURL, bytes.NewReader(body))
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "No se pudo validar la verificación")
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.recordSecurityEvent(c.Context(), "turnstile_request_failed", username, c, map[string]interface{}{"error": err.Error()})
		return fiber.NewError(fiber.StatusServiceUnavailable, "No se pudo validar la verificación")
	}
	defer resp.Body.Close()

	var result turnstileVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.recordSecurityEvent(c.Context(), "turnstile_decode_failed", username, c, map[string]interface{}{"status": resp.StatusCode})
		return fiber.NewError(fiber.StatusServiceUnavailable, "No se pudo validar la verificación")
	}
	if !result.Success {
		s.recordSecurityEvent(c.Context(), "turnstile_rejected", username, c, map[string]interface{}{
			"status":      resp.StatusCode,
			"error_codes": result.ErrorCodes,
			"hostname":    result.Hostname,
			"action":      result.Action,
		})
		return fiber.NewError(fiber.StatusBadRequest, "Verificación de seguridad inválida. Intenta nuevamente.")
	}
	return nil
}

func (s *Server) checkLoginAbuseLimit(c *fiber.Ctx, username string) error {
	if username == "" {
		username = "unknown"
	}
	ipKey := hashForLog(clientIP(c))
	userKey := hashForLog(username)
	fingerprintKey := hashForLog(clientFingerprint(c))
	limits := []abuseLimit{
		{Key: "abuse:login:ip:minute:" + ipKey, Max: 15, Window: time.Minute},
		{Key: "abuse:login:ip:hour:" + ipKey, Max: 120, Window: time.Hour},
		{Key: "abuse:login:user:minute:" + userKey, Max: 8, Window: time.Minute},
		{Key: "abuse:login:user:hour:" + userKey, Max: 40, Window: time.Hour},
		{Key: "abuse:login:fp:minute:" + fingerprintKey, Max: 20, Window: time.Minute},
	}
	return s.checkAbuseLimits(c, "login_rate_limited", username, limits)
}

func (s *Server) checkAbuseLimits(c *fiber.Ctx, eventType, subject string, limits []abuseLimit) error {
	for _, limit := range limits {
		count, err := s.incrementAbuseCounter(c.Context(), limit.Key, limit.Window)
		if err != nil {
			log.Printf("[SECURITY] abuse limiter fallback error: %v", err)
			count = s.abuseLimiter.incr(limit.Key, limit.Window)
		}
		if count > limit.Max {
			s.recordSecurityEvent(c.Context(), eventType, subject, c, map[string]interface{}{
				"key":    limit.Key,
				"count":  count,
				"limit":  limit.Max,
				"window": limit.Window.String(),
			})
			return fiber.NewError(fiber.StatusTooManyRequests, "Demasiados intentos. Intenta nuevamente más tarde.")
		}
	}
	return nil
}

func (s *Server) incrementAbuseCounter(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if s.cache == nil {
		return s.abuseLimiter.incr(key, ttl), nil
	}
	return s.cache.IncrWithTTL(ctx, key, ttl)
}

func (l *inMemoryAbuseLimiter) incr(key string, window time.Duration) int64 {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[key]
	if bucket.ExpiresAt.IsZero() || now.After(bucket.ExpiresAt) {
		bucket = inMemoryAbuseBucket{ExpiresAt: now.Add(window)}
	}
	bucket.Count++
	l.buckets[key] = bucket

	if len(l.buckets) > 2000 {
		for k, b := range l.buckets {
			if now.After(b.ExpiresAt) {
				delete(l.buckets, k)
			}
		}
	}
	return bucket.Count
}

func (s *Server) validateBrowserOrigin(c *fiber.Ctx) error {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return c.Next()
	}
	origin := strings.TrimSpace(c.Get("Origin"))
	if origin == "" {
		return c.Next()
	}
	if !s.isAllowedRequestOriginForRequest(c, origin) {
		s.recordSecurityEvent(c.Context(), "origin_rejected", "", c, map[string]interface{}{"origin": origin})
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"success": false,
			"error":   "Origin not allowed",
		})
	}
	return c.Next()
}

func (s *Server) isAllowedRequestOriginForRequest(c *fiber.Ctx, origin string) bool {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return true
	}
	if originURL, err := url.Parse(origin); err == nil {
		requestHost := strings.ToLower(strings.TrimSpace(c.Hostname()))
		originHost := strings.ToLower(strings.TrimSpace(originURL.Hostname()))
		if requestHost != "" && originHost == strings.Split(requestHost, ":")[0] {
			return true
		}
	}
	for _, allowed := range s.cfg.CORSOrigins {
		allowed = strings.TrimRight(strings.TrimSpace(allowed), "/")
		if allowed != "" && origin == allowed {
			return true
		}
	}
	if s.cfg.PublicURL != "" && origin == strings.TrimRight(strings.TrimSpace(s.cfg.PublicURL), "/") {
		return true
	}
	return s.cfg.IsDevelopment() && (strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:"))
}

func (s *Server) recordSecurityEvent(ctx context.Context, eventType, subject string, c *fiber.Ctx, metadata map[string]interface{}) {
	s.recordSecurityEventWithRefs(ctx, eventType, subject, c, nil, nil, metadata)
}

func (s *Server) recordSecurityEventWithRefs(ctx context.Context, eventType, subject string, c *fiber.Ctx, accountID, userID *uuid.UUID, metadata map[string]interface{}) {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["path"] = c.Path()
	raw, _ := json.Marshal(metadata)
	if _, err := s.repos.DB().Exec(ctx, `
		INSERT INTO security_events (type, account_id, user_id, subject_hash, ip_hash, user_agent_hash, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`, eventType, accountID, userID, hashForLog(subject), hashForLog(clientIP(c)), hashForLog(c.Get("User-Agent")), string(raw)); err != nil {
		log.Printf("[SECURITY] failed to record event %s: %v", eventType, err)
	}
}

func clientIP(c *fiber.Ctx) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if ip := strings.TrimSpace(c.Get(header)); ip != "" {
			return ip
		}
	}
	if forwarded := strings.TrimSpace(c.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}
	if ip := strings.TrimSpace(c.IP()); ip != "" {
		if host, _, err := net.SplitHostPort(ip); err == nil {
			return host
		}
		return ip
	}
	return "unknown"
}

func clientFingerprint(c *fiber.Ctx) string {
	return strings.Join([]string{
		clientIP(c),
		c.Get("User-Agent"),
		c.Get("Accept-Language"),
		c.Get("Sec-CH-UA-Platform"),
	}, "|")
}

func hashForLog(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		value = "unknown"
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (l abuseLimit) String() string {
	return fmt.Sprintf("%s max=%d window=%s", l.Key, l.Max, l.Window)
}
