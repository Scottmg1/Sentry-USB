package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Scottmg1/Sentry-USB/server/config"
)

const (
	sessionCookieName = "sentryusb_session"
	sessionTTL        = 24 * time.Hour
	cleanupInterval   = 1 * time.Hour
)

// webCredentials holds the configured web username/password from sentryusb.conf.
var webCredentials struct {
	username string
	password string
}

// sessions is the session store (persisted to disk).
var sessions = newSessionStore()

// sessionsFile is the path to the on-disk session store.
var sessionsFile string

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time // token → expiry
}

func newSessionStore() *sessionStore {
	s := &sessionStore{sessions: make(map[string]time.Time)}
	go s.cleanupLoop()
	return s
}

func (s *sessionStore) create() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[auth] crypto/rand failed: %v", err)
		return ""
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	s.saveToDisk()
	return token
}

func (s *sessionStore) validate(token string) bool {
	s.mu.RLock()
	expiry, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().Before(expiry)
}

func (s *sessionStore) remove(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	s.saveToDisk()
}

func (s *sessionStore) cleanupLoop() {
	for {
		time.Sleep(cleanupInterval)
		s.mu.Lock()
		now := time.Now()
		removed := 0
		for token, expiry := range s.sessions {
			if now.After(expiry) {
				delete(s.sessions, token)
				removed++
			}
		}
		s.mu.Unlock()
		if removed > 0 {
			s.saveToDisk()
		}
	}
}

func (s *sessionStore) loadFromDisk() {
	if sessionsFile == "" {
		return
	}
	data, err := os.ReadFile(sessionsFile)
	if err != nil {
		return
	}
	var stored map[string]int64
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}
	s.mu.Lock()
	now := time.Now()
	loaded := 0
	for token, unix := range stored {
		expiry := time.Unix(unix, 0)
		if now.Before(expiry) {
			s.sessions[token] = expiry
			loaded++
		}
	}
	s.mu.Unlock()
	if loaded > 0 {
		log.Printf("[auth] Restored %d active sessions from disk", loaded)
	}
}

func (s *sessionStore) saveToDisk() {
	if sessionsFile == "" {
		return
	}
	s.mu.RLock()
	stored := make(map[string]int64, len(s.sessions))
	for token, expiry := range s.sessions {
		stored[token] = expiry.Unix()
	}
	s.mu.RUnlock()
	data, err := json.Marshal(stored)
	if err != nil {
		return
	}
	os.WriteFile(sessionsFile, data, 0600)
}

// InitAuth loads web credentials from the config file. Call at startup.
func InitAuth() {
	path := config.FindConfigPath()
	sessionsFile = filepath.Join(filepath.Dir(path), ".sentryusb-sessions.json")
	sessions.loadFromDisk()

	active, _, err := config.ParseFile(path)
	if err != nil {
		log.Printf("[auth] Could not read config for web auth: %v", err)
		return
	}
	webCredentials.username = active["WEB_USERNAME"]
	webCredentials.password = active["WEB_PASSWORD"]
	if webCredentials.username != "" {
		log.Printf("[auth] Web authentication enabled for user %q", webCredentials.username)
	}
}

// AuthRequired returns true if web auth is configured.
func AuthRequired() bool {
	return webCredentials.username != ""
}

// POST /api/auth/login
func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !AuthRequired() {
		writeError(w, http.StatusBadRequest, "Authentication is not configured")
		return
	}

	usernameMatch := subtle.ConstantTimeCompare([]byte(req.Username), []byte(webCredentials.username)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(req.Password), []byte(webCredentials.password)) == 1

	if !usernameMatch || !passwordMatch {
		log.Printf("[auth] Failed login attempt for user %q", req.Username)
		writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	token := sessions.create()
	if token == "" {
		writeError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// POST /api/auth/logout
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		sessions.remove(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GET /api/auth/check
func (h *handlers) authCheck(w http.ResponseWriter, r *http.Request) {
	authRequired := AuthRequired()
	authenticated := false

	if !authRequired {
		authenticated = true
	} else {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil {
			authenticated = sessions.validate(cookie.Value)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": authenticated,
		"auth_required": authRequired,
	})
}

// NewAuthMiddleware returns an http.Handler that enforces web authentication
// on /api/* routes (except exempt paths). If no credentials are configured,
// all requests pass through.
func NewAuthMiddleware(next http.Handler) http.Handler {
	exemptExact := map[string]bool{
		// "/api/status":             true,
		// "/api/auth/login":         true,
		// "/api/auth/logout":        true,
		// "/api/auth/check":         true,
		// "/api/setup/status":       true,
		// "/api/setup/config":       true,
		// "/api/setup/run":          true,
		// "/api/setup/test-archive": true,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if not configured
		if !AuthRequired() {
			next.ServeHTTP(w, r)
			return
		}

		// Allow localhost requests (internal scripts like post-archive-process.sh,
		// archiveloop snapshot processing, etc. call the API from 127.0.0.1).
		host := r.RemoteAddr
		if strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "[::1]:") {
			next.ServeHTTP(w, r)
			return
		}

		// Only protect /api/* paths (not static files, TeslaCam, etc.)
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow exempt paths
		if exemptExact[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Allow setup sub-paths (PUT /api/setup/config etc.)
		if strings.HasPrefix(r.URL.Path, "/api/setup/") {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !sessions.validate(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		next.ServeHTTP(w, r)
	})
}
