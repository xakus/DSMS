// Handlers ресурсов кластера (FR-09 secrets/configs, FR-10 networks/volumes,
// FR-11 disk usage/prune). Swarm-объекты — через manager docker.sock;
// volumes/df/prune — через HTTP API агентов конкретных нод.
package api

import (
	"encoding/json"
	"net"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
)

// usedBySecrets строит map: имя secret/config → имена использующих сервисов.
func (h *handlers) usedBy(r *http.Request) (secrets, configs map[string][]string, err error) {
	services, err := h.Docker.Services(r.Context())
	if err != nil {
		return nil, nil, err
	}
	secrets = map[string][]string{}
	configs = map[string][]string{}
	for _, s := range services {
		cs := s.Spec.TaskTemplate.ContainerSpec
		if cs == nil {
			continue
		}
		for _, ref := range cs.Secrets {
			secrets[ref.SecretName] = append(secrets[ref.SecretName], s.Spec.Name)
		}
		for _, ref := range cs.Configs {
			configs[ref.ConfigName] = append(configs[ref.ConfigName], s.Spec.Name)
		}
	}
	return secrets, configs, nil
}

// secretsList — GET /secrets (3.9.1): список + использующие сервисы.
func (h *handlers) secretsList(w http.ResponseWriter, r *http.Request) {
	items, err := h.Docker.Secrets(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	used, _, err := h.usedBy(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	type view struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Created string   `json:"created"`
		UsedBy  []string `json:"used_by"`
	}
	out := make([]view, 0, len(items))
	for _, s := range items {
		out = append(out, view{
			ID:      s.ID,
			Name:    s.Spec.Name,
			Created: s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UsedBy:  used[s.Spec.Name],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

// secretCreate — POST /secrets {name, data} (3.9.2).
// Значение уходит в Docker один раз; в audit — только имя (3.9.4).
func (h *handlers) secretCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Data == "" {
		writeErr(w, http.StatusBadRequest, "name and data required")
		return
	}
	id, err := h.Docker.SecretCreate(r.Context(), req.Name, []byte(req.Data))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "secret create failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "secret.create", "secret", req.Name, "")
	writeJSON(w, map[string]string{"id": id})
}

// secretRemove — DELETE /secrets/{id}?force= (3.9.2):
// без force удаление используемого secret запрещено.
func (h *handlers) secretRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	force := r.URL.Query().Get("force") == "true"

	// Найти имя secret'а и проверить использование.
	items, err := h.Docker.Secrets(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	name := ""
	for _, s := range items {
		if s.ID == id {
			name = s.Spec.Name
			break
		}
	}
	if name == "" {
		writeErr(w, http.StatusNotFound, "secret not found")
		return
	}
	used, _, err := h.usedBy(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	if len(used[name]) > 0 && !force {
		writeErr(w, http.StatusConflict, "secret is used by services: use force")
		return
	}
	if err := h.Docker.SecretRemove(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadGateway, "secret remove failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "secret.remove", "secret", name, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// configsList — GET /configs (3.9.3).
func (h *handlers) configsList(w http.ResponseWriter, r *http.Request) {
	items, err := h.Docker.Configs(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	_, used, err := h.usedBy(r)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	type view struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Size    int      `json:"size"`
		Created string   `json:"created"`
		UsedBy  []string `json:"used_by"`
	}
	out := make([]view, 0, len(items))
	for _, c := range items {
		out = append(out, view{
			ID:      c.ID,
			Name:    c.Spec.Name,
			Size:    len(c.Spec.Data),
			Created: c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UsedBy:  used[c.Spec.Name],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

// configContent — GET /configs/{id}: содержимое (configs читаемы, 3.9.3).
func (h *handlers) configContent(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Docker.ConfigInspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "config not found")
		return
	}
	writeJSON(w, map[string]string{"name": cfg.Spec.Name, "data": string(cfg.Spec.Data)})
}

// configCreate — POST /configs {name, data} (3.9.3).
func (h *handlers) configCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name and data required")
		return
	}
	id, err := h.Docker.ConfigCreate(r.Context(), req.Name, []byte(req.Data))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "config create failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "config.create", "config", req.Name, "")
	writeJSON(w, map[string]string{"id": id})
}

// configRemove — DELETE /configs/{id} (3.9.3).
func (h *handlers) configRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Docker.ConfigRemove(r.Context(), id); err != nil {
		writeErr(w, http.StatusConflict, "config remove failed (in use?)")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "config.remove", "config", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// networksList — GET /networks (3.10.1).
func (h *handlers) networksList(w http.ResponseWriter, r *http.Request) {
	nets, err := h.Docker.Networks(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker api error")
		return
	}
	type view struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Driver     string `json:"driver"`
		Scope      string `json:"scope"`
		Attachable bool   `json:"attachable"`
		Subnet     string `json:"subnet"`
	}
	out := make([]view, 0, len(nets))
	for _, n := range nets {
		v := view{ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Attachable: n.Attachable}
		if len(n.IPAM.Config) > 0 {
			v.Subnet = n.IPAM.Config[0].Subnet
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

// networkCreate — POST /networks {name, driver?, attachable?, subnet?} (3.10.3).
func (h *handlers) networkCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Driver     string `json:"driver"`
		Attachable bool   `json:"attachable"`
		Subnet     string `json:"subnet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	if req.Driver == "" {
		req.Driver = "overlay"
	}
	id, err := h.Docker.NetworkCreate(r.Context(), req.Name, req.Driver, req.Attachable, req.Subnet)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "network create failed")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "network.create", "network", req.Name, "")
	writeJSON(w, map[string]string{"id": id})
}

// networkRemove — DELETE /networks/{id} (3.10.3).
func (h *handlers) networkRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Docker.NetworkRemove(r.Context(), id); err != nil {
		writeErr(w, http.StatusConflict, "network remove failed (in use?)")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "network.remove", "network", id, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// --- volumes / df / prune: через агентов нод ---

// nodeAgents возвращает node_id → (hostname, agentIP) для fan-out.
func (h *handlers) nodeHostnames(r *http.Request) map[string]string {
	names := map[string]string{}
	if nodes, err := h.Docker.Nodes(r.Context()); err == nil {
		for _, n := range nodes {
			names[n.ID] = n.Description.Hostname
		}
	}
	return names
}

// volumesList — GET /volumes: fan-out по агентам всех нод (3.10.4).
func (h *handlers) volumesList(w http.ResponseWriter, r *http.Request) {
	type volView struct {
		Node       string `json:"node"`
		NodeID     string `json:"node_id"`
		Name       string `json:"name"`
		Driver     string `json:"driver"`
		Mountpoint string `json:"mountpoint"`
		Size       int64  `json:"size"`
		InUse      bool   `json:"in_use"`
	}
	names := h.nodeHostnames(r)
	out := []volView{}
	// Последовательный опрос — без параллельного шторма (разд. 10 ТЗ).
	for nodeID, ip := range h.Agents.All() {
		var vols []struct {
			Name       string `json:"name"`
			Driver     string `json:"driver"`
			Mountpoint string `json:"mountpoint"`
			Size       int64  `json:"size"`
			InUse      bool   `json:"in_use"`
		}
		if err := h.AgentClient.Volumes(r.Context(), ip, &vols); err != nil {
			continue // нода недоступна — пропускаем, остальные покажем
		}
		for _, v := range vols {
			out = append(out, volView{
				Node: names[nodeID], NodeID: nodeID,
				Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint,
				Size: v.Size, InUse: v.InUse,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, out)
}

// volumeRemove — DELETE /volumes/{name}?node=&force= (3.10.5).
func (h *handlers) volumeRemove(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	nodeID := r.URL.Query().Get("node")
	force := r.URL.Query().Get("force") == "true"
	if nodeID == "" {
		writeErr(w, http.StatusBadRequest, "node query param required")
		return
	}
	ip, ok := h.Agents.Get(nodeID)
	if !ok {
		writeErr(w, http.StatusNotFound, "agent for node is unknown")
		return
	}
	if err := h.AgentClient.VolumeRemove(r.Context(), ip, name, force); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "volume.remove", "volume", name, `{"node":"`+nodeID+`"}`)
	writeJSON(w, map[string]string{"status": "ok"})
}

// systemDF — GET /system/df: fan-out docker system df по нодам (3.11.1).
func (h *handlers) systemDF(w http.ResponseWriter, r *http.Request) {
	type dfView struct {
		Node   string          `json:"node"`
		NodeID string          `json:"node_id"`
		DF     json.RawMessage `json:"df"`
		Err    string          `json:"err,omitempty"`
	}
	names := h.nodeHostnames(r)
	out := []dfView{}
	for nodeID, ip := range h.Agents.All() {
		item := dfView{Node: names[nodeID], NodeID: nodeID}
		var raw json.RawMessage
		if err := h.AgentClient.DF(r.Context(), ip, &raw); err != nil {
			item.Err = err.Error()
		} else {
			item.DF = raw
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	writeJSON(w, out)
}

// systemPrune — POST /system/prune {node_id, targets, all_images?} (3.11.2–3.11.3).
func (h *handlers) systemPrune(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID    string   `json:"node_id"`
		Targets   []string `json:"targets"`
		AllImages bool     `json:"all_images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeID == "" || len(req.Targets) == 0 {
		writeErr(w, http.StatusBadRequest, "node_id and targets required")
		return
	}
	ip, ok := h.Agents.Get(req.NodeID)
	if !ok {
		writeErr(w, http.StatusNotFound, "agent for node is unknown")
		return
	}
	var results json.RawMessage
	if err := h.AgentClient.Prune(r.Context(), ip, req.Targets, req.AllImages, &results); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s := sessionFrom(r)
	details, _ := json.Marshal(map[string]any{"node": req.NodeID, "targets": req.Targets, "results": results})
	_ = h.Store.AppendAudit(s.UserID, "system.prune", "node", req.NodeID, string(details))
	writeJSON(w, results)
}

// ipFromRemoteAddr выделяет IP из RemoteAddr ("10.0.1.5:43210" → "10.0.1.5").
func ipFromRemoteAddr(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
