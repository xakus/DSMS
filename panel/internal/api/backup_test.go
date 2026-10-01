// Тесты этапа 11: резервная копия БД (NFR-10) и версия сборки в /healthz (NFR-11).
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// TestBackupDownload: копия отдаётся файлом, это настоящая база SQLite,
// и факт снятия попадает в audit (выгрузка всех данных панели наружу).
func TestBackupDownload(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	resp := postJSON(t, client, srv.URL+"/api/v1/backup", nil,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backup: got %d", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "dsms-backup-") {
		t.Fatalf("нет имени файла в Content-Disposition: %q", cd)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("чтение тела: %v", err)
	}
	// Файл SQLite начинается с сигнатуры — проверяем, что это база, а не JSON-ошибка.
	if len(body) < 16 || !strings.HasPrefix(string(body[:15]), "SQLite format 3") {
		t.Fatalf("ответ не похож на файл SQLite: %d байт, начало %q", len(body), body[:min(20, len(body))])
	}

	// Audit проверяем через публичный эндпоинт — так же, как его увидит человек.
	aResp, err := client.Get(srv.URL + "/api/v1/audit")
	if err != nil {
		t.Fatalf("GET audit: %v", err)
	}
	defer aResp.Body.Close()
	var entries []struct {
		Action   string `json:"action"`
		ObjectID string `json:"object_id"`
	}
	if err := json.NewDecoder(aResp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Action == "backup.create" && strings.HasPrefix(e.ObjectID, "dsms-backup-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("снятие копии не записано в audit: %+v", entries)
	}
}

// TestBackupRequiresAuth: без сессии копию не получить — иначе это открытая
// выгрузка всей базы.
func TestBackupRequiresAuth(t *testing.T) {
	fd := &fakeDocker{}
	srv, _ := newTestServer(t, fd)

	resp, err := http.Post(srv.URL+"/api/v1/backup", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("без сессии: want 401, got %d", resp.StatusCode)
	}
}

// TestHealthzReportsVersion: /healthz отдаёт версию сборки и версию схемы —
// по ним видно, какой бинарник работает (NFR-11).
func TestHealthzReportsVersion(t *testing.T) {
	fd := &fakeDocker{}
	srv, _ := newTestServer(t, fd)

	resp, err := http.Get(srv.URL + "/api/v1/healthz")
	if err != nil {
		t.Fatalf("GET healthz: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Schema  int    `json:"schema"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != "ok" || out.Version == "" || out.Commit == "" {
		t.Fatalf("healthz неполон: %+v", out)
	}
	if out.Schema < 1 {
		t.Fatalf("версия схемы не отдана: %+v", out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
