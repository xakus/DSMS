// Package api — REST-маршруты PANEL (раздел 4 ТЗ).
//
// Базовый префикс /api/v1. Все эндпоинты кроме /login, /setup и /healthz
// требуют сессию; /ingest авторизуется токеном агента.
package api

import (
	"context"
	"net/http"

	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/xakus/DSMS/panel/internal/auth"
	"github.com/xakus/DSMS/panel/internal/config"
	"github.com/xakus/DSMS/panel/internal/crypto"
	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
	"github.com/xakus/DSMS/panel/internal/ws"
	"github.com/xakus/DSMS/panel/web"
)

// DockerAPI — операции Docker Engine, нужные API-слою.
// Интерфейс на стороне потребителя: продакшен — dockerapi.Client,
// тесты — fake без Docker daemon (guards проверяются офлайн).
type DockerAPI interface {
	Ping(ctx context.Context) error
	Nodes(ctx context.Context) ([]swarm.Node, error)
	Services(ctx context.Context) ([]swarm.Service, error)
	NodeInspect(ctx context.Context, id string) (swarm.Node, error)
	NodeUpdate(ctx context.Context, id string, version swarm.Version, spec swarm.NodeSpec) error
	NodeRemove(ctx context.Context, id string, force bool) error
	SwarmInspect(ctx context.Context) (swarm.Swarm, error)
	RotateJoinTokens(ctx context.Context) error
	NodeTasks(ctx context.Context, nodeID string) ([]swarm.Task, error)
	ServiceInspect(ctx context.Context, id string) (swarm.Service, error)
	ServiceUpdate(ctx context.Context, id string, version swarm.Version,
		spec swarm.ServiceSpec, registryAuth, rollback string) ([]string, error)
	ServiceRemove(ctx context.Context, id string) error
	ServiceTasks(ctx context.Context, serviceID string) ([]swarm.Task, error)
	Tasks(ctx context.Context) ([]swarm.Task, error)
}

// Deps — зависимости API-слоя, собираются в main.
type Deps struct {
	Cfg      *config.Config
	Store    *store.Store
	Docker   DockerAPI
	Buffer   *metrics.ClusterBuffer
	Hub      *ws.Hub
	Sessions *auth.Manager
	// Crypto — шифрование паролей реестров (FR-13).
	// nil — ключ не задан, registries API отвечает 503.
	Crypto *crypto.Box
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
			r.Get("/ws", h.websocket)

			// --- ноды (FR-02, FR-03) ---
			r.Get("/nodes", h.nodes)
			r.Get("/nodes/{id}", h.nodeDetail)
			r.Post("/nodes/{id}/role", h.nodeRole)
			r.Post("/nodes/{id}/availability", h.nodeAvailability)
			r.Put("/nodes/{id}/labels", h.nodeLabels)
			r.Delete("/nodes/{id}", h.nodeRemove)
			r.Get("/swarm/join-tokens", h.joinTokens)
			r.Post("/swarm/join-tokens/rotate", h.rotateJoinTokens)
			r.Get("/metrics/nodes/{id}", h.nodeMetrics)

			// --- сервисы (FR-04) ---
			r.Get("/services", h.services)
			r.Get("/services/{id}", h.serviceDetail)
			r.Post("/services/{id}/scale", h.serviceScale)
			r.Post("/services/{id}/start", h.serviceStart)
			r.Post("/services/{id}/redeploy", h.serviceRedeploy)
			r.Post("/services/{id}/image", h.serviceImage)
			r.Post("/services/{id}/rollback", h.serviceRollback)
			r.Delete("/services/{id}", h.serviceRemove)

			// --- стеки (FR-08) ---
			r.Get("/stacks", h.stacks)
			r.Get("/stacks/{name}", h.stackDetail)
			r.Post("/stacks/{name}/redeploy", h.stackRedeploy)
			r.Delete("/stacks/{name}", h.stackRemove)

			// --- реестры (FR-13) ---
			r.Get("/registries", h.registries)
			r.Post("/registries", h.registryCreate)
			r.Put("/registries/{id}", h.registryUpdate)
			r.Delete("/registries/{id}", h.registryDelete)
		})
	})

	// SPA: статика из go:embed + fallback на index.html (client-side routing).
	r.NotFound(web.SPAHandler())

	return r
}
