package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apeming/go-proxy-server/internal/activity"
	"github.com/apeming/go-proxy-server/internal/auth"
	"github.com/apeming/go-proxy-server/internal/config"
	applogger "github.com/apeming/go-proxy-server/internal/logger"
)

const (
	adminSessionCookieName = "gps_admin_session"
	adminSessionTTL        = 12 * time.Hour
	sessionCleanupInterval = 1 * time.Hour
	adminBootstrapTokenEnv = "GPS_ADMIN_BOOTSTRAP_TOKEN"
)

var (
	errAdminPasswordConfigured = errors.New("admin password is already configured")
	errInvalidBootstrapToken   = errors.New("invalid bootstrap token")
)

type adminSession struct {
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type adminAuthManager struct {
	mu       sync.Mutex
	sessions map[string]adminSession
	stopChan chan struct{}
}

func newAdminAuthManager() *adminAuthManager {
	m := &adminAuthManager{
		sessions: make(map[string]adminSession),
		stopChan: make(chan struct{}),
	}
	go m.cleanupExpiredSessions()
	return m
}

func (m *adminAuthManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for token, session := range m.sessions {
				if now.After(session.ExpiresAt) {
					delete(m.sessions, token)
				}
			}
			m.mu.Unlock()
		case <-m.stopChan:
			return
		}
	}
}

func (m *adminAuthManager) stop() {
	close(m.stopChan)
}

func (wm *Manager) getAdminAuthManager() *adminAuthManager {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if wm.adminAuth == nil {
		wm.adminAuth = newAdminAuthManager()
	}

	return wm.adminAuth
}

func (m *adminAuthManager) createSession() (adminSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return adminSession{}, fmt.Errorf("generate session token: %w", err)
	}
	now := time.Now()
	session := adminSession{
		Token:     hex.EncodeToString(buf),
		CreatedAt: now,
		ExpiresAt: now.Add(adminSessionTTL),
	}
	m.sessions[session.Token] = session
	return session, nil
}

func (m *adminAuthManager) validateSession(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(session.ExpiresAt) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *adminAuthManager) deleteSession(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (wm *Manager) adminPasswordConfigured() (bool, error) {
	value, err := config.GetSystemConfig(wm.db, config.KeyWebAdminPassword)
	if err != nil {
		return false, err
	}
	return value != "", nil
}

func (wm *Manager) setAdminPassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("admin password must be at least 8 characters")
	}
	hash, err := auth.HashPassword([]byte(password))
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	if err := config.SetSystemConfig(wm.db, config.KeyWebAdminPassword, string(hash)); err != nil {
		return err
	}
	wm.clearAdminBootstrapToken()
	return nil
}

func (wm *Manager) bootstrapAdminPassword(password, bootstrapToken string) error {
	if len(password) < 8 {
		return fmt.Errorf("admin password must be at least 8 characters")
	}
	configured, err := wm.adminPasswordConfigured()
	if err != nil {
		return err
	}
	if configured {
		return errAdminPasswordConfigured
	}
	valid, err := wm.validateAdminBootstrapToken(bootstrapToken)
	if err != nil {
		return err
	}
	if !valid {
		return errInvalidBootstrapToken
	}

	hash, err := auth.HashPassword([]byte(password))
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	inserted, err := config.SetSystemConfigIfAbsent(wm.db, config.KeyWebAdminPassword, string(hash))
	if err != nil {
		return err
	}
	if !inserted {
		return errAdminPasswordConfigured
	}
	wm.clearAdminBootstrapToken()
	return nil
}

func (wm *Manager) verifyAdminPassword(password string) (bool, error) {
	storedHash, err := config.GetSystemConfig(wm.db, config.KeyWebAdminPassword)
	if err != nil {
		return false, err
	}
	if storedHash == "" {
		return false, errors.New("admin password is not configured")
	}
	return auth.VerifyPasswordHash([]byte(storedHash), []byte(password)), nil
}

func (wm *Manager) issueAdminSession(w http.ResponseWriter) error {
	session, err := wm.getAdminAuthManager().createSession()
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(adminSessionTTL.Seconds()),
		Expires:  session.ExpiresAt,
	})
	return nil
}

func (wm *Manager) clearAdminSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminSessionCookieName); err == nil {
		wm.getAdminAuthManager().deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (wm *Manager) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		return false
	}
	return wm.getAdminAuthManager().validateSession(cookie.Value)
}

func (wm *Manager) requireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wm.isAuthenticated(r) {
			next(w, r)
			return
		}
		wm.recordEvent("auth", "admin_auth_required", activity.SeverityWarn, "web_admin", "Unauthorized admin API access blocked", map[string]any{
			"path":       r.URL.Path,
			"method":     r.Method,
			"source_ip":  requestSourceIP(r),
			"user_agent": r.UserAgent(),
		})
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "authentication required",
		})
	}
}

func (wm *Manager) ensureAdminBootstrapToken() (string, error) {
	configured, err := wm.adminPasswordConfigured()
	if err != nil {
		return "", err
	}
	if configured {
		wm.clearAdminBootstrapToken()
		return "", nil
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()
	if wm.adminBootstrapToken != "" {
		return wm.adminBootstrapToken, nil
	}

	token := strings.TrimSpace(os.Getenv(adminBootstrapTokenEnv))
	if token == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate bootstrap token: %w", err)
		}
		token = hex.EncodeToString(buf)
		applogger.Warn("Web admin bootstrap token generated. Initialize the password with token: %s", token)
	} else {
		applogger.Warn("Using web admin bootstrap token from %s for first-time initialization", adminBootstrapTokenEnv)
	}

	wm.adminBootstrapToken = token
	return token, nil
}

func (wm *Manager) clearAdminBootstrapToken() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.adminBootstrapToken = ""
}

func (wm *Manager) validateAdminBootstrapToken(provided string) (bool, error) {
	expected, err := wm.ensureAdminBootstrapToken()
	if err != nil {
		return false, err
	}
	if expected == "" || provided == "" {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1, nil
}
