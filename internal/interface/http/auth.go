package httpapi

// Web console authentication: one operator account from http.auth config,
// opaque session cookies, and a middleware gate on /v1 (except login/status).

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	platformconfig "github.com/noroadzh/gb28181-simulator/internal/platform/config"
)

const sessionCookieName = "gbsim_sid"

type sessionEntry struct {
	user string
	expires  time.Time
}

// sessionStore holds in-memory login sessions for one process.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]sessionEntry
	ttl      time.Duration
}

func newSessionStore(ttl time.Duration) *sessionStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &sessionStore{sessions: make(map[string]sessionEntry), ttl: ttl}
}

func (s *sessionStore) create(username string) (string, time.Time, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, err
	}
	id := hex.EncodeToString(raw[:])
	exp := time.Now().Add(s.ttl)
	s.mu.Lock()
	s.sessions[id] = sessionEntry{user: username, expires: exp}
	s.mu.Unlock()
	return id, exp, nil
}

func (s *sessionStore) get(id string) (sessionEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ent, ok := s.sessions[id]
	if !ok {
		return sessionEntry{}, false
	}
	if time.Now().After(ent.expires) {
		delete(s.sessions, id)
		return sessionEntry{}, false
	}
	return ent, true
}

func (s *sessionStore) revoke(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func (s *Server) authEnabled() bool {
	return s.cfg.HTTP.Auth.Enabled
}

func (s *Server) authCredentialsOK(user, pass string) bool {
	cfg := s.cfg.HTTP.Auth
	return secretEqual(user, cfg.Username) && secretEqual(pass, cfg.Password)
}

func secretEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func (s *Server) sessionIDFromRequest(c echo.Context) string {
	if cookie, err := c.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	auth := c.Request().Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if q := strings.TrimSpace(c.QueryParam("access_token")); q != "" {
		return q
	}
	return ""
}

func (s *Server) currentSession(c echo.Context) (sessionEntry, bool) {
	if s.sessions == nil {
		return sessionEntry{}, false
	}
	id := s.sessionIDFromRequest(c)
	if id == "" {
		return sessionEntry{}, false
	}
	return s.sessions.get(id)
}

func (s *Server) setSessionCookie(c echo.Context, id string, exp time.Time) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
		MaxAge:   int(time.Until(exp).Seconds()),
	}
	c.SetCookie(cookie)
}

func (s *Server) clearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthStatus(c echo.Context) error {
	_, authed := s.currentSession(c)
	return c.JSON(http.StatusOK, map[string]any{
		"enabled":       s.authEnabled(),
		"authenticated": !s.authEnabled() || authed,
		"username": func() string {
			if ent, ok := s.currentSession(c); ok {
				return ent.user
			}
			return ""
		}(),
	})
}

func (s *Server) handleAuthLogin(c echo.Context) error {
	if !s.authEnabled() {
		return c.JSON(http.StatusOK, map[string]any{"enabled": false, "authenticated": true})
	}
	var in loginRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON: " + err.Error()})
	}
	user := strings.TrimSpace(in.Username)
	if user == "" || in.Password == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "username and password are required"})
	}
	if !s.authCredentialsOK(user, in.Password) {
		return c.JSON(http.StatusUnauthorized, errorBody{Error: "invalid username or password"})
	}
	id, exp, err := s.sessions.create(user)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "could not create session"})
	}
	s.setSessionCookie(c, id, exp)
	return c.JSON(http.StatusOK, map[string]any{
		"enabled":       true,
		"authenticated": true,
		"username":      user,
		"expires_at":    exp.UTC().Format(time.RFC3339),
		"token":         id, // for non-cookie clients / media query fallback
	})
}

func (s *Server) handleAuthLogout(c echo.Context) error {
	if id := s.sessionIDFromRequest(c); id != "" && s.sessions != nil {
		s.sessions.revoke(id)
	}
	s.clearSessionCookie(c)
	return c.JSON(http.StatusOK, map[string]any{"authenticated": false})
}

// authMiddleware gates /v1 when http.auth.enabled is true.
func (s *Server) authMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !s.authEnabled() {
			return next(c)
		}
		path := c.Request().URL.Path
		if isPublicHTTPPath(path) {
			return next(c)
		}
		if _, ok := s.currentSession(c); ok {
			return next(c)
		}
		return c.JSON(http.StatusUnauthorized, errorBody{Error: "authentication required"})
	}
}

func isPublicHTTPPath(path string) bool {
	switch path {
	case "/healthz", "/metrics", "/v1/health",
		"/v1/auth/login", "/v1/auth/logout", "/v1/auth/status":
		return true
	}
	// SPA assets and index — login page must load without a session.
	if path == "/" || path == "/index.html" || strings.HasPrefix(path, "/assets/") {
		return true
	}
	return false
}

// ensureAuthStore initialises the session map when auth is enabled.
func ensureAuthStore(cfg platformconfig.HTTPAuthConfig) *sessionStore {
	if !cfg.Enabled {
		return nil
	}
	return newSessionStore(cfg.SessionTTL())
}
