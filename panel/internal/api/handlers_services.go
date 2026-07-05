// Handlers сервисов (FR-04): список, детали, scale/stop/start,
// redeploy, update image (с registry auth), rollback, remove.
//
// Правила 3.4.3: Stop = scale 0 (реплики запоминаются в service_state),
// Start = scale обратно, Redeploy = ForceUpdate++.
package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
)

// stackLabel — label группировки по стекам (3.4.2, FR-08).
const stackLabel = "com.docker.stack.namespace"

// serviceView — строка таблицы сервисов (3.4.1).
type serviceView struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Mode     string            `json:"mode"` // replicated | global
	Running  int               `json:"running"`
	Desired  int               `json:"desired"`
	Stack    string            `json:"stack"`
	Ports    []string          `json:"ports"`
	Labels   map[string]string `json:"labels"`
	UpdState string            `json:"update_state,omitempty"` // 3.4.5
	Stopped  bool              `json:"stopped"`                // scale 0 через панель
}

// buildServiceViews агрегирует сервисы + задачи в представление таблицы.
func (h *handlers) buildServiceViews(r *http.Request) ([]serviceView, error) {
	ctx := r.Context()
	services, err := h.Docker.Services(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := h.Docker.Tasks(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := h.Docker.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	// running-задачи по сервисам.
	running := map[string]int{}
	for _, t := range tasks {
		if t.Status.State == swarm.TaskStateRunning && t.DesiredState == swarm.TaskStateRunning {
			running[t.ServiceID]++
		}
	}
	// Для global-сервисов desired = количество активных ready-нод.
	activeNodes := 0
	for _, n := range nodes {
		if n.Status.State == swarm.NodeStateReady && n.Spec.Availability == swarm.NodeAvailabilityActive {
			activeNodes++
		}
	}

	out := make([]serviceView, 0, len(services))
	for _, s := range services {
		v := serviceView{
			ID:      s.ID,
			Name:    s.Spec.Name,
			Image:   shortImage(s.Spec.TaskTemplate.ContainerSpec.Image),
			Running: running[s.ID],
			Stack:   s.Spec.Labels[stackLabel],
			Labels:  s.Spec.Labels,
		}
		if s.Spec.Mode.Replicated != nil {
			v.Mode = "replicated"
			if s.Spec.Mode.Replicated.Replicas != nil {
				v.Desired = int(*s.Spec.Mode.Replicated.Replicas)
			}
		} else {
			v.Mode = "global"
			v.Desired = activeNodes
		}
		if s.UpdateStatus != nil {
			v.UpdState = string(s.UpdateStatus.State)
		}
		for _, p := range s.Endpoint.Ports {
			v.Ports = append(v.Ports, fmt.Sprintf("%d:%d/%s", p.PublishedPort, p.TargetPort, p.Protocol))
		}
		// Сервис «остановлен панелью»: 0 desired + запись в service_state.
		if v.Mode == "replicated" && v.Desired == 0 {
			if _, ok, _ := h.Store.SavedServiceReplicas(s.ID); ok {
				v.Stopped = true
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// shortImage отрезает digest от image-ссылки (читаемость таблицы).
func shortImage(img string) string {
	if i := strings.Index(img, "@"); i > 0 {
		return img[:i]
	}
	return img
}

// services — GET /services (3.4.1).
func (h *handlers) services(w http.ResponseWriter, r *http.Request) {
	views, err := h.buildServiceViews(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	writeJSON(w, views)
}

// serviceDetail — GET /services/{id}: спецификация + задачи с историей (3.4.4).
func (h *handlers) serviceDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	tasks, err := h.Docker.ServiceTasks(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	// Имена нод для задач.
	nodeName := map[string]string{}
	if nodes, err := h.Docker.Nodes(r.Context()); err == nil {
		for _, n := range nodes {
			nodeName[n.ID] = n.Description.Hostname
		}
	}

	type taskView struct {
		ID        string `json:"id"`
		Slot      int    `json:"slot"`
		Node      string `json:"node"`
		State     string `json:"state"`
		Desired   string `json:"desired"`
		Message   string `json:"message"`
		ExitCode  *int   `json:"exit_code,omitempty"`
		CreatedAt string `json:"created_at"`
	}
	tv := make([]taskView, 0, len(tasks))
	for _, t := range tasks {
		item := taskView{
			ID:        t.ID,
			Slot:      t.Slot,
			Node:      nodeName[t.NodeID],
			State:     string(t.Status.State),
			Desired:   string(t.DesiredState),
			Message:   t.Status.Message,
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		}
		if t.Status.ContainerStatus != nil && t.Status.State == swarm.TaskStateFailed {
			ec := t.Status.ContainerStatus.ExitCode
			item.ExitCode = &ec
		}
		tv = append(tv, item)
	}
	// Свежие сверху (история обновлений, 3.4.4).
	sort.Slice(tv, func(i, j int) bool { return tv[i].CreatedAt > tv[j].CreatedAt })

	spec := map[string]any{
		"image":       s2(svc.Spec.TaskTemplate.ContainerSpec.Image),
		"env":         svc.Spec.TaskTemplate.ContainerSpec.Env,
		"constraints": svc.Spec.TaskTemplate.Placement.Constraints,
		"labels":      svc.Spec.Labels,
	}
	if svc.Spec.TaskTemplate.Resources != nil && svc.Spec.TaskTemplate.Resources.Limits != nil {
		spec["limits"] = map[string]any{
			"cpus":   float64(svc.Spec.TaskTemplate.Resources.Limits.NanoCPUs) / 1e9,
			"memory": svc.Spec.TaskTemplate.Resources.Limits.MemoryBytes,
		}
	}
	mounts := make([]string, 0)
	for _, m := range svc.Spec.TaskTemplate.ContainerSpec.Mounts {
		mounts = append(mounts, fmt.Sprintf("%s:%s (%s)", m.Source, m.Target, m.Type))
	}
	spec["mounts"] = mounts

	view := map[string]any{
		"id":     svc.ID,
		"name":   svc.Spec.Name,
		"stack":  svc.Spec.Labels[stackLabel],
		"spec":   spec,
		"tasks":  tv,
		"mode":   map[bool]string{true: "replicated", false: "global"}[svc.Spec.Mode.Replicated != nil],
		"update": nil,
	}
	if svc.UpdateStatus != nil {
		view["update"] = map[string]any{
			"state":   string(svc.UpdateStatus.State),
			"message": svc.UpdateStatus.Message,
		}
	}
	writeJSON(w, view)
}

// s2 — image без digest для спецификации.
func s2(img string) string { return shortImage(img) }

// serviceScale — POST /services/{id}/scale {replicas} (3.4.3 Scale/Stop).
// replicas == 0 запоминает прежнее значение для последующего Start.
func (h *handlers) serviceScale(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Replicas *uint64 `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Replicas == nil {
		writeErr(w, http.StatusBadRequest, "replicas required")
		return
	}
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeErr(w, http.StatusConflict, "cannot scale a global service")
		return
	}

	// Stop: запомнить текущее кол-во реплик (3.4.3).
	if *req.Replicas == 0 && svc.Spec.Mode.Replicated.Replicas != nil && *svc.Spec.Mode.Replicated.Replicas > 0 {
		if err := h.Store.SaveServiceReplicas(id, *svc.Spec.Mode.Replicated.Replicas); err != nil {
			writeErr(w, http.StatusInternalServerError, "storage error")
			return
		}
	}
	spec := svc.Spec
	spec.Mode.Replicated.Replicas = req.Replicas
	if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version, spec, "", ""); err != nil {
		writeErr(w, http.StatusBadGateway, "service update failed")
		return
	}
	if *req.Replicas > 0 {
		_ = h.Store.DeleteServiceState(id) // сервис снова живой
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.scale", "service", id,
		fmt.Sprintf(`{"replicas":%d}`, *req.Replicas))
	writeJSON(w, map[string]string{"status": "ok"})
}

// serviceStart — POST /services/{id}/start: scale обратно к запомненному (3.4.3).
func (h *handlers) serviceStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	saved, ok, err := h.Store.SavedServiceReplicas(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	if !ok {
		writeErr(w, http.StatusConflict, "service was not stopped via panel")
		return
	}
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeErr(w, http.StatusConflict, "cannot scale a global service")
		return
	}
	spec := svc.Spec
	spec.Mode.Replicated.Replicas = &saved
	if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version, spec, "", ""); err != nil {
		writeErr(w, http.StatusBadGateway, "service update failed")
		return
	}
	_ = h.Store.DeleteServiceState(id)
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.start", "service", id,
		fmt.Sprintf(`{"replicas":%d}`, saved))
	writeJSON(w, map[string]string{"status": "ok"})
}

// serviceRedeploy — POST /services/{id}/redeploy: ForceUpdate++ (3.4.3).
func (h *handlers) serviceRedeploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.forceUpdate(r, id); err != nil {
		writeErr(w, http.StatusBadGateway, "redeploy failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.redeploy", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// forceUpdate инкрементирует ForceUpdate спецификации — аналог
// docker service update --force.
func (h *handlers) forceUpdate(r *http.Request, id string) error {
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		return err
	}
	spec := svc.Spec
	spec.TaskTemplate.ForceUpdate++
	_, err = h.Docker.ServiceUpdate(r.Context(), id, svc.Version, spec, "", "")
	return err
}

// serviceImage — POST /services/{id}/image {image, registry_id?} (3.4.3, FR-13).
func (h *handlers) serviceImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Image      string `json:"image"`
		RegistryID *int64 `json:"registry_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Image == "" {
		writeErr(w, http.StatusBadRequest, "image required")
		return
	}
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}

	// Собрать X-Registry-Auth из сохранённого реестра (3.13.3).
	regAuth := ""
	if req.RegistryID != nil {
		regAuth, err = h.registryAuthHeader(*req.RegistryID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "registry auth failed")
			return
		}
	}

	spec := svc.Spec
	spec.TaskTemplate.ContainerSpec.Image = req.Image
	if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version, spec, regAuth, ""); err != nil {
		writeErr(w, http.StatusBadGateway, "image update failed")
		return
	}
	s := sessionFrom(r)
	// В audit — только имя образа; учётные данные не пишутся (3.13.4).
	_ = h.Store.AppendAudit(s.UserID, "service.image", "service", id,
		fmt.Sprintf(`{"image":%q}`, req.Image))
	writeJSON(w, map[string]string{"status": "ok"})
}

// registryAuthHeader строит base64 X-Registry-Auth из реестра FR-13.
func (h *handlers) registryAuthHeader(registryID int64) (string, error) {
	if h.Crypto == nil {
		return "", fmt.Errorf("encryption key not configured")
	}
	reg, err := h.Store.RegistryByID(registryID)
	if err != nil {
		return "", err
	}
	password, err := h.Crypto.Open(reg.PasswordEnc)
	if err != nil {
		return "", err
	}
	authJSON, err := json.Marshal(registry.AuthConfig{
		Username:      reg.Username,
		Password:      string(password),
		ServerAddress: reg.Address,
	})
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(authJSON), nil
}

// serviceRollback — POST /services/{id}/rollback (3.4.3).
func (h *handlers) serviceRollback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	svc, err := h.Docker.ServiceInspect(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "service not found")
		return
	}
	if _, err := h.Docker.ServiceUpdate(r.Context(), id, svc.Version, svc.Spec, "", "previous"); err != nil {
		writeErr(w, http.StatusBadGateway, "rollback failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.rollback", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// serviceRemove — DELETE /services/{id} (3.4.3 Remove; подтверждение — в UI).
func (h *handlers) serviceRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Docker.ServiceRemove(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadGateway, "remove failed")
		return
	}
	_ = h.Store.DeleteServiceState(id)
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "service.remove", "service", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}
