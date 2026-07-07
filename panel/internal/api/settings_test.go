// Тесты серверной настройки интервала метрик и эндпоинта конфига агента.
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// getWithHeaders — GET с произвольными заголовками (для X-Agent-Token).
func getWithHeaders(t *testing.T, client *http.Client, url string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return resp
}

// TestMetricsIntervalSetting: PUT /settings меняет интервал, агент читает его
// через /agent/config; валидация диапазона и авторизация токеном.
func TestMetricsIntervalSetting(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}
	agentHdr := map[string]string{"X-Agent-Token": "test-token"}

	// дефолт до настройки — 3с
	resp := getWithHeaders(t, client, srv.URL+"/api/v1/agent/config", agentHdr)
	var cfg struct {
		IntervalSec int `json:"interval_sec"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&cfg)
	if resp.StatusCode != http.StatusOK || cfg.IntervalSec != 3 {
		t.Fatalf("default config: status %d, interval %d", resp.StatusCode, cfg.IntervalSec)
	}

	// сохранить интервал 10с (обязательны и прочие поля putSettings)
	resp = putJSON(t, client, srv.URL+"/api/v1/settings", map[string]any{
		"alert_disk_pct":         90,
		"metrics_retention_days": 7,
		"metrics_interval_sec":   10,
	}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put settings: got %d", resp.StatusCode)
	}

	// getSettings под сессией отдаёт новый интервал
	resp = getWithHeaders(t, client, srv.URL+"/api/v1/settings", nil)
	var s struct {
		IntervalSec int `json:"metrics_interval_sec"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&s)
	if s.IntervalSec != 10 {
		t.Fatalf("getSettings interval: want 10, got %d", s.IntervalSec)
	}

	// агент видит новый интервал
	resp = getWithHeaders(t, client, srv.URL+"/api/v1/agent/config", agentHdr)
	_ = json.NewDecoder(resp.Body).Decode(&cfg)
	if cfg.IntervalSec != 10 {
		t.Fatalf("agent config: want 10, got %d", cfg.IntervalSec)
	}

	// вне диапазона → 400
	resp = putJSON(t, client, srv.URL+"/api/v1/settings", map[string]any{
		"alert_disk_pct":         90,
		"metrics_retention_days": 7,
		"metrics_interval_sec":   0,
	}, hdr)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("interval 0: want 400, got %d", resp.StatusCode)
	}

	// /agent/config без токена → 401
	resp = getWithHeaders(t, client, srv.URL+"/api/v1/agent/config", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("agent config w/o token: want 401, got %d", resp.StatusCode)
	}
}
