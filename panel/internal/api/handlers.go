// Handlers REST API этапа 1: auth/setup, healthz, ingest, cluster, nodes, ws.
package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/xakus/DSMS/panel/internal/auth"
	"github.com/xakus/DSMS/panel/internal/metrics"
)

// handlers — приёмник всех HTTP-обработчиков с общими зависимостями.
type handlers struct {
	Deps
}

// writeJSON сериализует ответ в JSON со статусом 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr отдаёт единый формат ошибки {"error": "..."}.
func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// healthz — liveness-проба без аутентификации (разд. 4.1).
func (h *handlers) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

// setupStatus сообщает фронтенду, нужен ли экран первичной настройки (3.7.1).
func (h *handlers) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := h.Store.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, map[string]bool{"needs_setup": n == 0})
}

// setup создаёт admin-аккаунт при первом запуске. Доступен только пока
// пользователей нет — после первого вызова навсегда отвечает 409.
func (h *handlers) setup(w http.ResponseWriter, r *http.Request) {
	n, err := h.Store.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "already configured")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "username and password (min 8 chars) required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash error")
		return
	}
	userID, err := h.Store.CreateUser(req.Username, hash, "admin")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create user failed")
		return
	}
	_ = h.Store.AppendAudit(userID, "setup", "user", req.Username, "")
	writeJSON(w, map[string]bool{"ok": true})
}

// login — вход с rate-limit 5 попыток / 15 мин с IP (3.7.3).
func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !h.Sessions.Allowed(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try later")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, err := h.Store.UserByUsername(req.Username)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !auth.CheckPassword(u.PasswordHash, req.Password)) {
		h.Sessions.RecordFailure(ip)
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	s, err := h.Sessions.Create(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	auth.SetCookie(w, s, h.Cfg.CookieSecure)
	_ = h.Store.AppendAudit(u.ID, "login", "session", "", "")
	// CSRF-токен отдаётся один раз при входе; фронт шлёт его в X-CSRF-Token.
	writeJSON(w, map[string]string{"username": u.Username, "role": u.Role, "csrf": s.CSRF})
}

// logout уничтожает сессию и чистит cookie.
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		h.Sessions.Destroy(c.Value)
	}
	auth.ClearCookie(w, h.Cfg.CookieSecure)
	writeJSON(w, map[string]bool{"ok": true})
}

// me возвращает текущего пользователя (для восстановления состояния SPA).
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	s := sessionFrom(r)
	writeJSON(w, map[string]string{"username": s.Username, "role": s.Role, "csrf": s.CSRF})
}

// ingest принимает батч метрик от агента (разд. 4.3).
// Авторизация — сравнение X-Agent-Token в constant time (3.7.4).
func (h *handlers) ingest(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Agent-Token")
	if h.Cfg.AgentToken == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(h.Cfg.AgentToken)) != 1 {
		writeErr(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	var snap metrics.Snapshot
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&snap); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if snap.NodeID == "" {
		writeErr(w, http.StatusBadRequest, "node_id required")
		return
	}
	h.Buffer.Put(snap)
	// Живая рассылка подписчикам topic=metrics (разд. 4.2).
	h.Hub.Broadcast("metrics", "", map[string]any{
		"topic": "metrics",
		"node":  snap.NodeID,
		"ts":    snap.TS,
		"data":  snap,
	})
	w.WriteHeader(http.StatusNoContent)
}

// cluster — сводка кластера (FR-01 3.1.3): ноды, сервисы, статусы.
func (h *handlers) cluster(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := h.Docker.Nodes(ctx)
	if err != nil {
		slog.Error("docker nodes failed", "err", err)
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	services, err := h.Docker.Services(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	ready := 0
	for _, n := range nodes {
		if string(n.Status.State) == "ready" {
			ready++
		}
	}
	writeJSON(w, map[string]any{
		"nodes":       len(nodes),
		"nodes_ready": ready,
		"services":    len(services),
		"ts":          time.Now().Unix(),
	})
}

// nodes — список нод + последние live-метрики каждой (FR-01).
// Метрики старше 15 сек фронт помечает как «агент молчит» (3.1.4).
func (h *handlers) nodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.Docker.Nodes(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	type nodeView struct {
		ID           string            `json:"id"`
		Hostname     string            `json:"hostname"`
		Role         string            `json:"role"`
		Leader       bool              `json:"leader"`
		State        string            `json:"state"`
		Availability string            `json:"availability"`
		Addr         string            `json:"addr"`
		Labels       map[string]string `json:"labels"`
		Metrics      *metrics.Snapshot `json:"metrics,omitempty"`
	}
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		v := nodeView{
			ID:           n.ID,
			Hostname:     n.Description.Hostname,
			Role:         string(n.Spec.Role),
			State:        string(n.Status.State),
			Availability: string(n.Spec.Availability),
			Addr:         n.Status.Addr,
			Labels:       n.Spec.Labels,
		}
		if n.ManagerStatus != nil {
			v.Leader = n.ManagerStatus.Leader
		}
		if snap, ok := h.Buffer.Latest(n.ID); ok {
			v.Metrics = &snap
		}
		out = append(out, v)
	}
	writeJSON(w, out)
}

// websocket — апгрейд соединения в hub (только под сессией).
func (h *handlers) websocket(w http.ResponseWriter, r *http.Request) {
	h.Hub.Handle(w, r)
}

// audit — GET /audit?limit=&offset=: журнал действий (3.6.2).
func (h *handlers) audit(w http.ResponseWriter, r *http.Request) {
	limit, offset := 100, 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	entries, err := h.Store.AuditEntries(limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, entries)
}
