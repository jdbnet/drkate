package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jamie/drkate/internal/auth"
	"github.com/jamie/drkate/internal/k8s"
	"github.com/jamie/drkate/internal/storage"
)

type Handler struct {
	Users       *auth.UserStore
	Sessions    *auth.SessionManager
	Store       *storage.Store
	Scraper     *k8s.Scraper
	Coordinator *k8s.ScrapeCoordinator
	Comparator  *k8s.Comparator
	DrCache     *k8s.DrStatusCache
	Deployer    *k8s.Deployer
	Sanitizer   *k8s.Sanitizer

	loginAttempts map[string][]time.Time
	loginMu       sync.Mutex
}

func NewHandler(users *auth.UserStore, sessions *auth.SessionManager, store *storage.Store, scraper *k8s.Scraper, coordinator *k8s.ScrapeCoordinator, comparator *k8s.Comparator, drCache *k8s.DrStatusCache, deployer *k8s.Deployer, sanitizer *k8s.Sanitizer) *Handler {
	return &Handler{
		Users:         users,
		Sessions:      sessions,
		Store:         store,
		Scraper:       scraper,
		Coordinator:   coordinator,
		Comparator:    comparator,
		DrCache:       drCache,
		Deployer:      deployer,
		Sanitizer:     sanitizer,
		loginAttempts: make(map[string][]time.Time),
	}
}

func (h *Handler) JSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if h.isRateLimited(req.Username) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}

	user, err := h.Users.Authenticate(req.Username, req.Password)
	if err != nil {
		h.recordLoginAttempt(req.Username)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	sid, err := h.Sessions.Create(user)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	h.Sessions.SetCookie(w, sid)
	h.JSON(w, http.StatusOK, map[string]string{
		"username": user.Username,
		"role":     string(user.Role),
	})
}

func (h *Handler) isRateLimited(username string) bool {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()
	now := time.Now()
	attempts := h.loginAttempts[username]
	var recent []time.Time
	for _, t := range attempts {
		if now.Sub(t) < 15*time.Minute {
			recent = append(recent, t)
		}
	}
	h.loginAttempts[username] = recent
	return len(recent) >= 5
}

func (h *Handler) recordLoginAttempt(username string) {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()
	h.loginAttempts[username] = append(h.loginAttempts[username], time.Now())
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if _, sid, ok := h.Sessions.SessionFromRequest(r); ok {
		h.Sessions.Delete(sid)
	}
	h.Sessions.ClearCookie(w)
	h.JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	s, ok := auth.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	h.JSON(w, http.StatusOK, map[string]string{
		"username": s.Username,
		"role":     string(s.Role),
	})
}

func (h *Handler) DRStatus(w http.ResponseWriter, r *http.Request) {
	h.JSON(w, http.StatusOK, h.DrCache.Overview())
}

func (h *Handler) DRStatusNamespace(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	if h.Comparator.EnsureWarnings(ns) {
		h.DrCache.HydrateFromStore()
	}
	h.JSON(w, http.StatusOK, h.DrCache.NamespaceDetail(ns))
}

func (h *Handler) DRRefresh(w http.ResponseWriter, r *http.Request) {
	h.DrCache.RefreshAsync()
	h.JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *Handler) Namespaces(w http.ResponseWriter, r *http.Request) {
	stats := h.Store.ScrapeStats()
	h.JSON(w, http.StatusOK, map[string]interface{}{
		"namespaces": stats.Namespaces,
		"stats":      stats,
	})
}

func (h *Handler) ListResources(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	kind := r.URL.Query().Get("kind")
	h.JSON(w, http.StatusOK, h.Store.List(ns, kind))
}

func (h *Handler) GetResource(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")

	files, err := h.Store.GetFiles(ns, kind, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	meta := files.Meta

	status, drift, ok := h.DrCache.ResourceStatus(meta.Namespace, meta.Kind, meta.Name)
	if !ok {
		status, drift, _ = h.Comparator.CompareOne(r.Context(), meta)
	}

	h.JSON(w, http.StatusOK, map[string]interface{}{
		"yaml":        string(files.YAML),
		"scrapedYaml": string(files.Scraped),
		"edited":      files.HasEdit,
		"meta":        meta,
		"drStatus":    status,
		"drift":       drift,
		"warnings":    k8s.DetectWarningsFromYAML(files.YAML),
	})
}

func (h *Handler) UpdateResource(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")

	var req struct {
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	cleaned, err := h.Sanitizer.SanitizeYAML([]byte(req.YAML))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, _, err := h.Store.Get(ns, kind, name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := h.Store.SaveEdit(ns, kind, name, cleaned); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.Store.ApplyWarnings([]storage.WarningUpdate{{
		Namespace: ns,
		Kind:      kind,
		Name:      name,
		Warnings:  k8s.DetectWarningsFromYAML(cleaned),
	}}); err != nil {
		log.Printf("persist resource warnings: %v", err)
	}
	h.DrCache.HydrateFromStore()
	h.JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *Handler) DiscardEdit(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")
	if err := h.Store.ClearEdit(ns, kind, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.DrCache.HydrateFromStore()
	h.JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *Handler) ScrapeStatus(w http.ResponseWriter, r *http.Request) {
	scraping := h.Coordinator.IsScraping()
	stats := h.Store.ScrapeStats()
	if scraping {
		stats = h.Store.LiveStats()
		if stats.LastScrapeAt.IsZero() {
			if started := h.Coordinator.StartedAt(); !started.IsZero() {
				stats.LastScrapeAt = started
			}
		}
	}
	last := h.Coordinator.LastResult()
	var lastResult interface{} = nil
	if last != nil {
		lastResult = last
	}
	var lastScrapeAt interface{} = nil
	if !stats.LastScrapeAt.IsZero() {
		lastScrapeAt = stats.LastScrapeAt
	}
	h.JSON(w, http.StatusOK, map[string]interface{}{
		"scraping":       scraping,
		"lastScrapeAt":   lastScrapeAt,
		"namespaceCount": stats.NamespaceCount,
		"resourceCount":  stats.ResourceCount,
		"namespaces":     stats.Namespaces,
		"lastResult":     lastResult,
	})
}

func (h *Handler) Scrape(w http.ResponseWriter, r *http.Request) {
	if h.Coordinator.IsScraping() {
		http.Error(w, "scrape in progress", http.StatusConflict)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		result, err := h.Coordinator.Run(ctx, h.Scraper)
		if err != nil {
			if !errors.Is(err, k8s.ErrScrapeInProgress) {
				log.Printf("scrape error: %v", err)
			}
			if result != nil {
				h.DrCache.RefreshAsync()
			}
			return
		}
		h.DrCache.RefreshAsync()
	}()

	h.JSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	var req k8s.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Namespace == "" {
		http.Error(w, "namespace required", http.StatusBadRequest)
		return
	}

	result, err := h.Deployer.Deploy(r.Context(), req, h.Comparator)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(result.Failed) > 0 {
		log.Printf("deploy: %d succeeded, %d failed in %s", len(result.Success), len(result.Failed), req.Namespace)
		for _, f := range result.Failed {
			log.Printf("deploy failed: %s", f)
		}
	}
	h.JSON(w, http.StatusOK, result)
	h.DrCache.RefreshAsync()
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	h.JSON(w, http.StatusOK, h.Users.List())
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	role := auth.Role(req.Role)
	if role != auth.RoleAdmin && role != auth.RoleOperator && role != auth.RoleViewer {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}
	if err := h.Users.Create(req.Username, req.Password, role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.JSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if err := h.Users.Delete(username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
