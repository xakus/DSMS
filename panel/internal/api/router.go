// Package api — REST-маршруты PANEL (раздел 4 ТЗ).
//
// Базовый префикс /api/v1. Все эндпоинты кроме /login, /setup и /healthz
// требуют сессию; /ingest авторизуется токеном агента.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/xakus/DSMS/panel/internal/auth"
	"github.com/xakus/DSMS/panel/internal/config"
	"github.com/xakus/DSMS/panel/internal/dockerapi"
	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
	"github.com/xakus/DSMS/panel/internal/ws"
	"github.com/xakus/DSMS/panel/web"
)

// Deps — зависимости API-слоя, собираются в main.
type Deps struct {
	Cfg      *config.Config
	Store    *store.Store
	Docker   *dockerapi.Client
	Buffer   *metrics.ClusterBuffer
	Hub      *ws.Hub
	Sessions *auth.Manager
}

// NewRouter собирает chi-роутер: API + встроенная SPA.
func NewRouter(d Deps) http.Handler {
	h := &handlers{Deps: d}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		// --- без аутентификации ---
		r.Get("/healthz", h.healthz)
		r.Post("/login", h.login)
		r.Get("/setup", h.setupStatus)
		r.Post("/setup", h.setup)
		// приём метрик от агентов — авторизация по X-Agent-Token (3.7.4)
		r.Post("/ingest", h.ingest)

		// --- под сессией ---
		r.Group(func(r chi.Router) {
			r.Use(h.requireSession)
			r.Use(h.requireCSRF) // CSRF на мутирующие запросы (3.7.6)

			r.Post("/logout", h.logout)
			r.Get("/me", h.me)
			r.Get("/cluster", h.cluster)
			r.Get("/nodes", h.nodes)
			r.Get("/ws", h.websocket)
		})
	})

	// SPA: статика из go:embed + fallback на index.html (client-side routing).
	r.NotFound(web.SPAHandler())

	return r
}
