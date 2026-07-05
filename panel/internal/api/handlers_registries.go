// Handlers реестров (FR-13): CRUD учётных данных приватных реестров.
// Пароль шифруется AES-GCM и НИКОГДА не возвращается клиенту (3.13.4).
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// registryView — реестр в ответах API (без пароля!).
type registryView struct {
	ID       int64  `json:"id"`
	Address  string `json:"address"`
	Username string `json:"username"`
	Label    string `json:"label"`
}

// requireCrypto проверяет наличие ключа шифрования (DSMS_ENCRYPTION_KEY).
func (h *handlers) requireCrypto(w http.ResponseWriter) bool {
	if h.Crypto == nil {
		writeErr(w, http.StatusServiceUnavailable,
			"registry credentials require DSMS_ENCRYPTION_KEY to be configured")
		return false
	}
	return true
}

// registries — GET /registries (3.13.1).
func (h *handlers) registries(w http.ResponseWriter, r *http.Request) {
	regs, err := h.Store.Registries()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	out := make([]registryView, 0, len(regs))
	for _, reg := range regs {
		out = append(out, registryView{ID: reg.ID, Address: reg.Address, Username: reg.Username, Label: reg.Label})
	}
	writeJSON(w, out)
}

// registryCreate — POST /registries {address, username, password, label} (3.13.2).
func (h *handlers) registryCreate(w http.ResponseWriter, r *http.Request) {
	if !h.requireCrypto(w) {
		return
	}
	var req struct {
		Address  string `json:"address"`
		Username string `json:"username"`
		Password string `json:"password"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Address == "" || req.Username == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "address, username and password required")
		return
	}
	enc, err := h.Crypto.Seal([]byte(req.Password))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encryption failed")
		return
	}
	id, err := h.Store.CreateRegistry(req.Address, req.Username, enc, req.Label)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	s := sessionFrom(r)
	// В audit — только адрес; пароль не попадает никуда (3.13.4).
	_ = h.Store.AppendAudit(s.UserID, "registry.create", "registry", req.Address, "")
	writeJSON(w, registryView{ID: id, Address: req.Address, Username: req.Username, Label: req.Label})
}

// registryUpdate — PUT /registries/{id}; пустой password = не менять (3.13.2).
func (h *handlers) registryUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.requireCrypto(w) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Address  string `json:"address"`
		Username string `json:"username"`
		Password string `json:"password"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Address == "" || req.Username == "" {
		writeErr(w, http.StatusBadRequest, "address and username required")
		return
	}
	var enc []byte // nil — пароль остаётся прежним
	if req.Password != "" {
		enc, err = h.Crypto.Seal([]byte(req.Password))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "encryption failed")
			return
		}
	}
	if err := h.Store.UpdateRegistry(id, req.Address, req.Username, enc, req.Label); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "registry.update", "registry", req.Address, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// registryDelete — DELETE /registries/{id} (3.13.2).
func (h *handlers) registryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.Store.DeleteRegistry(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "registry.delete", "registry", strconv.FormatInt(id, 10), "")
	writeJSON(w, map[string]string{"status": "ok"})
}
