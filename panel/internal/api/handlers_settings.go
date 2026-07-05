// Handlers Settings (разд. 6.2 экран 12): смена пароля, пороги алертов.
// Ротация agent-token невозможна из панели (Docker secret управляется
// снаружи) — UI показывает CLI-инструкцию.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/xakus/DSMS/panel/internal/auth"
)

// settingAlertDiskPct — ключ порога диска в таблице settings (FR-12).
const settingAlertDiskPct = "alert.disk_pct"

// changePassword — POST /settings/password {old, new} (6.2 экран 12).
// Смена инвалидирует ВСЕ сессии пользователя, кроме текущей? В v1 — все:
// проще и безопаснее; фронт после смены делает relogin.
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.New) < 8 {
		writeErr(w, http.StatusBadRequest, "new password min 8 chars")
		return
	}
	s := sessionFrom(r)
	u, err := h.Store.UserByID(s.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Old) {
		writeErr(w, http.StatusForbidden, "old password is wrong")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash error")
		return
	}
	if err := h.Store.UpdatePassword(u.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	_ = h.Store.AppendAudit(u.ID, "user.password", "user", u.Username, "")
	writeJSON(w, map[string]string{"status": "ok"})
}

// getSettings — GET /settings: текущие настройки панели.
func (h *handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	diskPct := 90.0
	if v, _ := h.Store.GetSetting(settingAlertDiskPct); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			diskPct = f
		}
	}
	writeJSON(w, map[string]any{"alert_disk_pct": diskPct})
}

// putSettings — PUT /settings {alert_disk_pct}: пороги алертов (3.12.1).
func (h *handlers) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AlertDiskPct float64 `json:"alert_disk_pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.AlertDiskPct < 50 || req.AlertDiskPct > 99 {
		writeErr(w, http.StatusBadRequest, "alert_disk_pct must be 50..99")
		return
	}
	if err := h.Store.SetSetting(settingAlertDiskPct,
		strconv.FormatFloat(req.AlertDiskPct, 'f', 1, 64)); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage error")
		return
	}
	if h.Alerts != nil {
		h.Alerts.SetDiskPct(req.AlertDiskPct) // применяется сразу, без рестарта
	}
	s := sessionFrom(r)
	_ = h.Store.AppendAudit(s.UserID, "settings.update", "settings", settingAlertDiskPct, "")
	writeJSON(w, map[string]string{"status": "ok"})
}
