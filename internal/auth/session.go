package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "drkate_session"
const sessionTTL = 24 * time.Hour

type Session struct {
	Username string
	Role     Role
	Expires  time.Time
}

type SessionManager struct {
	secret []byte
	mu     sync.RWMutex
	sessions map[string]Session
}

func NewSessionManager(secret []byte) *SessionManager {
	sm := &SessionManager{
		secret:   secret,
		sessions: make(map[string]Session),
	}
	go sm.cleanupLoop()
	return sm
}

func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		for id, s := range sm.sessions {
			if s.Expires.Before(now) {
				delete(sm.sessions, id)
			}
		}
		sm.mu.Unlock()
	}
}

func (sm *SessionManager) Create(user *User) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	sm.mu.Lock()
	sm.sessions[id] = Session{
		Username: user.Username,
		Role:     user.Role,
		Expires:  time.Now().Add(sessionTTL),
	}
	sm.mu.Unlock()
	return id, nil
}

func (sm *SessionManager) Get(id string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[id]
	if !ok || s.Expires.Before(time.Now()) {
		return nil, false
	}
	return &s, true
}

func (sm *SessionManager) Delete(id string) {
	sm.mu.Lock()
	delete(sm.sessions, id)
	sm.mu.Unlock()
}

func (sm *SessionManager) SetCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (sm *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (sm *SessionManager) SessionFromRequest(r *http.Request) (*Session, string, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, "", false
	}
	s, ok := sm.Get(c.Value)
	return s, c.Value, ok
}

type contextKey string

const sessionContextKey contextKey = "session"

func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}

func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(*Session)
	return s, ok
}

func (sm *SessionManager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _, ok := sm.SessionFromRequest(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), s)))
	})
}

func (sm *SessionManager) RequireRole(minRole Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !roleSufficient(s.Role, minRole) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func roleSufficient(actual, required Role) bool {
	order := map[Role]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}
	return order[actual] >= order[required]
}
