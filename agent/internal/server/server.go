// Package server — локальный HTTP API агента (разд. 2.3 ТЗ):
// df / volumes / prune по командам панели. Слушает только внутри
// overlay-сети, авторизация shared-token (X-Agent-Token).
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/xakus/DSMS/agent/internal/config"
	"github.com/xakus/DSMS/agent/internal/dockerops"
)

// Server — HTTP API агента.
type Server struct {
	cfg *config.Config
	ops *dockerops.Ops
}

// New создаёт сервер; ops может быть nil (docker.sock не смонтирован) —
// тогда все endpoints отвечают 503.
func New(cfg *config.Config, ops *dockerops.Ops) *Server {
	return &Server{cfg: cfg, ops: ops}
}

// Run блокирует до отмены ctx.
func (s *Server) Run(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/df", s.auth(s.df))
	mux.HandleFunc("GET /api/v1/volumes", s.auth(s.volumes))
	mux.HandleFunc("POST /api/v1/volumes/remove", s.auth(s.volumeRemove))
	mux.HandleFunc("POST /api/v1/prune", s.auth(s.prune))

	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	slog.Info("agent api listening", "addr", s.cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("agent api failed", "err", err)
	}
}

// auth — проверка X-Agent-Token в constant time + наличие docker.sock.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Agent-Token")
		if s.cfg.AgentToken == "" ||
			subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AgentToken)) != 1 {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		if s.ops == nil {
			http.Error(w, `{"error":"docker.sock is not mounted"}`, http.StatusServiceUnavailable)
			return
		}
		next(w, r)
	}
}

// writeJSON сериализует ответ.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr отдаёт реальную ошибку Docker в JSON — чтобы в UI была видна
// причина (например «rw layer snapshot not found …»), а не общий текст.
func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// df — GET /api/v1/df: локальный docker system df (3.11.1).
func (s *Server) df(w http.ResponseWriter, r *http.Request) {
	out, err := s.ops.DiskUsage(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, out)
}

// volumes — GET /api/v1/volumes: локальные тома (3.10.4).
func (s *Server) volumes(w http.ResponseWriter, r *http.Request) {
	out, err := s.ops.Volumes(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, out)
}

// volumeRemove — POST /api/v1/volumes/remove {name, force} (3.10.5).
func (s *Server) volumeRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	if err := s.ops.VolumeRemove(r.Context(), req.Name, req.Force); err != nil {
		// Docker сам отклонит удаление используемого тома без force.
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// prune — POST /api/v1/prune {targets, all_images} (3.11.2).
// «Включая используемые» невозможно по построению: Docker prune
// трогает только неиспользуемые объекты (3.11.4).
func (s *Server) prune(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Targets   []string `json:"targets"`
		AllImages bool     `json:"all_images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Targets) == 0 {
		http.Error(w, `{"error":"targets required"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, s.ops.Prune(r.Context(), req.Targets, req.AllImages))
}
