package server

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jamie/drkate/internal/auth"
	"github.com/jamie/drkate/internal/server/handlers"
)

//go:embed all:static
var staticFS embed.FS

type Server struct {
	handler  *handlers.Handler
	sessions *auth.SessionManager
}

func New(handler *handlers.Handler, sessions *auth.SessionManager) *Server {
	return &Server{handler: handler, sessions: sessions}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/api/auth/login", s.handler.Login)

	r.Group(func(r chi.Router) {
		r.Use(s.sessions.RequireAuth)

		r.Post("/api/auth/logout", s.handler.Logout)
		r.Get("/api/auth/me", s.handler.Me)

		r.Get("/api/dr/status", s.handler.DRStatus)
		r.Get("/api/dr/status/{ns}", s.handler.DRStatusNamespace)
		r.Get("/api/namespaces", s.handler.Namespaces)
		r.Get("/api/scrape/status", s.handler.ScrapeStatus)
		r.Get("/api/resources", s.handler.ListResources)
		r.Get("/api/resources/{ns}/{kind}/{name}", s.handler.GetResource)

		r.Group(func(r chi.Router) {
			r.Use(s.requireOperator)
			r.Put("/api/resources/{ns}/{kind}/{name}", s.handler.UpdateResource)
			r.Delete("/api/resources/{ns}/{kind}/{name}/edit", s.handler.DiscardEdit)
			r.Post("/api/scrape", s.handler.Scrape)
			r.Post("/api/deploy", s.handler.Deploy)
			r.Post("/api/dr/refresh", s.handler.DRRefresh)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Get("/api/users", s.handler.ListUsers)
			r.Post("/api/users", s.handler.CreateUser)
			r.Delete("/api/users/{username}", s.handler.DeleteUser)
		})
	})

	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		fileServer := http.FileServer(http.FS(sub))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/" {
				fileServer.ServeHTTP(w, r)
				return
			}
			if path == "/favicon.ico" {
				if _, err := sub.Open("favicon.png"); err == nil {
					r.URL.Path = "/favicon.png"
					fileServer.ServeHTTP(w, r)
					return
				}
			}
			if _, err := sub.Open(path[1:]); err != nil {
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return r
}

func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok || !auth.RoleCanWrite(session.Role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := auth.SessionFromContext(r.Context())
		if !ok || !auth.RoleCanManageUsers(session.Role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
