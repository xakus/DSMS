// Handlers стеков (FR-08): стек — не объект Docker, а агрегация сервисов
// по label com.docker.stack.namespace (3.8.1).
package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
)

// stacks — GET /stacks: агрегированный список (3.8.1).
func (h *handlers) stacks(w http.ResponseWriter, r *http.Request) {
	views, err := h.buildServiceViews(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	type stackView struct {
		Name     string `json:"name"`
		Services int    `json:"services"`
		Running  int    `json:"running"`
		Desired  int    `json:"desired"`
	}
	agg := map[string]*stackView{}
	for _, v := range views {
		name := v.Stack
		if name == "" {
			name = "(no stack)" // сервисы вне стеков — отдельная группа
		}
		sv, ok := agg[name]
		if !ok {
			sv = &stackView{Name: name}
			agg[name] = sv
		}
		sv.Services++
		sv.Running += v.Running
		sv.Desired += v.Desired
	}
	out := make([]stackView, 0, len(agg))
	for _, sv := range agg {
		out = append(out, *sv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

// stackServices возвращает сервисы стека по имени.
func (h *handlers) stackServices(r *http.Request, name string) ([]serviceView, error) {
	views, err := h.buildServiceViews(r)
	if err != nil {
		return nil, err
	}
	out := make([]serviceView, 0)
	for _, v := range views {
		if v.Stack == name {
			out = append(out, v)
		}
	}
	return out, nil
}

// stackDetail — GET /stacks/{name}: состав стека (3.8.2).
func (h *handlers) stackDetail(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	services, err := h.stackServices(r, name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	if len(services) == 0 {
		writeErr(w, http.StatusNotFound, "stack not found")
		return
	}
	writeJSON(w, map[string]any{"name": name, "services": services})
}

// stackRedeploy — POST /stacks/{name}/redeploy: ForceUpdate++ каждому (3.8.3).
func (h *handlers) stackRedeploy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	services, err := h.stackServices(r, name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	if len(services) == 0 {
		writeErr(w, http.StatusNotFound, "stack not found")
		return
	}
	failed := 0
	for _, svc := range services {
		if err := h.forceUpdate(r, svc.ID); err != nil {
			failed++
		}
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "stack.redeploy", "stack", name, "")
	if failed > 0 {
		writeErr(w, http.StatusBadGateway, "some services failed to redeploy")
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "services": len(services)})
}

// stackDeploy — POST /stacks/{name}/deploy: каждому сервису стека тянет
// свежий образ по тегу и катит без простоя (start-first). Отличие от
// stackRedeploy: сбрасывается pinned digest, Swarm заново резолвит теги.
func (h *handlers) stackDeploy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	services, err := h.stackServices(r, name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	if len(services) == 0 {
		writeErr(w, http.StatusNotFound, "stack not found")
		return
	}
	failed := 0
	for _, sv := range services {
		svc, err := h.Docker.ServiceInspect(r.Context(), sv.ID)
		if err != nil {
			failed++
			continue
		}
		if err := h.deployService(r, svc, ""); err != nil {
			failed++
		}
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "stack.deploy", "stack", name, "")
	if failed > 0 {
		writeErr(w, http.StatusBadGateway, "some services failed to deploy")
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "services": len(services)})
}

// stackRemove — DELETE /stacks/{name}: удаление всех сервисов стека (3.8.3).
// Двойное подтверждение с вводом имени — на стороне UI.
func (h *handlers) stackRemove(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	services, err := h.stackServices(r, name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	if len(services) == 0 {
		writeErr(w, http.StatusNotFound, "stack not found")
		return
	}
	for _, svc := range services {
		if err := h.Docker.ServiceRemove(r.Context(), svc.ID); err != nil {
			writeErr(w, http.StatusBadGateway, "remove failed on "+svc.Name)
			return
		}
		_ = h.Store.DeleteServiceState(svc.ID)
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "stack.remove", "stack", name, "")
	writeJSON(w, map[string]any{"status": "ok", "removed": len(services)})
}
