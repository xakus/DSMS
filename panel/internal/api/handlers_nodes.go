// Handlers управления нодами (FR-02, FR-03).
//
// Все мутирующие действия пишутся в audit (3.7.7). Правило 3.3.5
// («защита от выстрела в ногу»): нельзя demote/drain последнего manager'а;
// при чётном числе manager'ов возвращается предупреждение о кворуме.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
)

// managerStats считает manager'ов кластера и сколько из них «живые».
func (h *handlers) managerStats(ctx context.Context) (total int, err error) {
	nodes, err := h.Docker.Nodes(ctx)
	if err != nil {
		return 0, err
	}
	for _, n := range nodes {
		if n.Spec.Role == swarm.NodeRoleManager {
			total++
		}
	}
	return total, nil
}

// quorumWarning — текст предупреждения при чётном числе manager'ов (3.3.5).
func quorumWarning(managers int) string {
	if managers > 0 && managers%2 == 0 {
		return fmt.Sprintf("even number of managers (%d): quorum is fragile, add or remove one", managers)
	}
	return ""
}

// nodeDetail — GET /nodes/{id}: спецификация + задачи на ноде (3.2.4).
func (h *handlers) nodeDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	node, err := h.Docker.NodeInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	tasks, err := h.Docker.NodeTasks(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	// Имена сервисов для задач — одним списком, чтобы не дёргать inspect по каждой.
	services, _ := h.Docker.Services(r.Context())
	svcName := make(map[string]string, len(services))
	for _, s := range services {
		svcName[s.ID] = s.Spec.Name
	}

	type taskView struct {
		ID        string `json:"id"`
		ServiceID string `json:"service_id"`
		Service   string `json:"service"`
		Slot      int    `json:"slot"`
		State     string `json:"state"`
		Message   string `json:"message"`
		CreatedAt string `json:"created_at"`
	}
	tv := make([]taskView, 0, len(tasks))
	for _, t := range tasks {
		tv = append(tv, taskView{
			ID:        t.ID,
			ServiceID: t.ServiceID,
			Service:   svcName[t.ServiceID],
			Slot:      t.Slot,
			State:     string(t.Status.State),
			Message:   t.Status.Message,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	view := map[string]any{
		"id":           node.ID,
		"hostname":     node.Description.Hostname,
		"role":         string(node.Spec.Role),
		"availability": string(node.Spec.Availability),
		"state":        string(node.Status.State),
		"addr":         node.Status.Addr,
		"labels":       node.Spec.Labels,
		"engine":       node.Description.Engine.EngineVersion,
		"os":           node.Description.Platform.OS,
		"arch":         node.Description.Platform.Architecture,
		"tasks":        tv,
	}
	if node.ManagerStatus != nil {
		view["leader"] = node.ManagerStatus.Leader
	}
	if snap, ok := h.Buffer.Latest(node.ID); ok {
		view["metrics"] = snap
	}
	writeJSON(w, view)
}

// nodeRole — POST /nodes/{id}/role {role: manager|worker} (3.3.1).
func (h *handlers) nodeRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Role != "manager" && req.Role != "worker") {
		writeErr(w, http.StatusBadRequest, "role must be manager or worker")
		return
	}
	node, err := h.Docker.NodeInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}

	// Guard 3.3.5: нельзя разжаловать последнего manager'а.
	if req.Role == "worker" && node.Spec.Role == swarm.NodeRoleManager {
		managers, err := h.managerStats(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, "docker api error")
			return
		}
		if managers <= 1 {
			writeErr(w, http.StatusConflict, "cannot demote the last manager")
			return
		}
	}

	spec := node.Spec
	spec.Role = swarm.NodeRole(req.Role)
	if err := h.Docker.NodeUpdate(r.Context(), id, node.Version, spec); err != nil {
		writeErr(w, http.StatusBadGateway, "node update failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "node.role."+req.Role, "node", id, "")

	managers, _ := h.managerStats(r.Context())
	writeJSON(w, map[string]string{"status": "ok", "warning": quorumWarning(managers)})
}

// nodeAvailability — POST /nodes/{id}/availability {availability} (3.3.2).
func (h *handlers) nodeAvailability(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Availability string `json:"availability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	av := swarm.NodeAvailability(req.Availability)
	if av != swarm.NodeAvailabilityActive && av != swarm.NodeAvailabilityPause && av != swarm.NodeAvailabilityDrain {
		writeErr(w, http.StatusBadRequest, "availability must be active, pause or drain")
		return
	}
	node, err := h.Docker.NodeInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}

	// Guard 3.3.5: drain/pause последнего активного manager'а кладёт панель и API.
	if av != swarm.NodeAvailabilityActive && node.Spec.Role == swarm.NodeRoleManager {
		nodes, err := h.Docker.Nodes(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, "docker api error")
			return
		}
		activeManagers := 0
		for _, n := range nodes {
			if n.Spec.Role == swarm.NodeRoleManager &&
				n.Spec.Availability == swarm.NodeAvailabilityActive && n.ID != id {
				activeManagers++
			}
		}
		if activeManagers == 0 {
			writeErr(w, http.StatusConflict, "cannot drain/pause the last active manager")
			return
		}
	}

	spec := node.Spec
	spec.Availability = av
	if err := h.Docker.NodeUpdate(r.Context(), id, node.Version, spec); err != nil {
		writeErr(w, http.StatusBadGateway, "node update failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "node.availability."+req.Availability, "node", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// nodeLabels — PUT /nodes/{id}/labels: полная замена labels (3.2.5).
func (h *handlers) nodeLabels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Labels == nil {
		writeErr(w, http.StatusBadRequest, "labels object required")
		return
	}
	node, err := h.Docker.NodeInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	spec := node.Spec
	spec.Labels = req.Labels
	if err := h.Docker.NodeUpdate(r.Context(), id, node.Version, spec); err != nil {
		writeErr(w, http.StatusBadGateway, "node update failed")
		return
	}
	s := sessionFrom(r)
	details, _ := json.Marshal(req.Labels)
	_ = h.Store.AppendAudit(s.UserID, "node.labels", "node", id, string(details))
	writeJSON(w, map[string]string{"status": "ok"})
}

// nodeRemove — DELETE /nodes/{id}?force= (3.3.3):
// без force удаляется только нода в состоянии down.
func (h *handlers) nodeRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	force := r.URL.Query().Get("force") == "true"

	node, err := h.Docker.NodeInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	if node.Status.State != swarm.NodeStateDown && !force {
		writeErr(w, http.StatusConflict, "node is not down: drain it first or use force")
		return
	}
	if err := h.Docker.NodeRemove(r.Context(), id, force); err != nil {
		writeErr(w, http.StatusBadGateway, "node remove failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "node.remove", "node", id,
		fmt.Sprintf(`{"force":%t}`, force))
	writeJSON(w, map[string]string{"status": "ok"})
}

// joinTokens — GET /swarm/join-tokens: токены + адрес manager'а (3.3.4).
func (h *handlers) joinTokens(w http.ResponseWriter, r *http.Request) {
	sw, err := h.Docker.SwarmInspect(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	// Адрес для команды join — адрес любого живого manager'а.
	managerAddr := ""
	if nodes, err := h.Docker.Nodes(r.Context()); err == nil {
		for _, n := range nodes {
			if n.ManagerStatus != nil && n.ManagerStatus.Leader {
				managerAddr = n.ManagerStatus.Addr
				break
			}
		}
	}
	writeJSON(w, map[string]string{
		"worker":       sw.JoinTokens.Worker,
		"manager":      sw.JoinTokens.Manager,
		"manager_addr": managerAddr,
	})
}

// rotateJoinTokens — POST /swarm/join-tokens/rotate (3.3.4).
func (h *handlers) rotateJoinTokens(w http.ResponseWriter, r *http.Request) {
	if err := h.Docker.RotateJoinTokens(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, "rotate failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "swarm.join-tokens.rotate", "swarm", "", "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// nodeMetrics — GET /metrics/nodes/{id}: live-окно из кольцевого буфера.
// Исторические окна (1ч–7дн из SQLite) — этап 7.
func (h *handlers) nodeMetrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	writeJSON(w, h.Buffer.Window(id))
}
