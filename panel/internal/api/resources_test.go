// Тесты secrets/configs/networks (FR-09, FR-10): guards использования,
// отсутствие значений secrets в audit.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// clusterWithSecret — фикстура: secret "db_pass" используется сервисом app_db.
func clusterWithSecret() *fakeDocker {
	fd := clusterWithServices()
	fd.secrets = []swarm.Secret{{
		ID:   "sec-db_pass",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db_pass"}},
	}}
	// Сервис app_db (s2) ссылается на secret.
	fd.services[1].Spec.TaskTemplate.ContainerSpec.Secrets = []*swarm.SecretReference{
		{SecretName: "db_pass"},
	}
	return fd
}

// TestSecretUsageGuard: удаление используемого secret — только с force (3.9.2).
func TestSecretUsageGuard(t *testing.T) {
	fd := clusterWithSecret()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// список показывает использование
	resp, _ := client.Get(srv.URL + "/api/v1/secrets")
	var list []struct {
		Name   string   `json:"name"`
		UsedBy []string `json:"used_by"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 || len(list[0].UsedBy) != 1 || list[0].UsedBy[0] != "app_db" {
		t.Fatalf("used_by: %+v", list)
	}

	// без force → 409
	resp = del(t, client, srv.URL+"/api/v1/secrets/sec-db_pass", hdr)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("remove used secret: want 409, got %d", resp.StatusCode)
	}
	// с force → ok
	resp = del(t, client, srv.URL+"/api/v1/secrets/sec-db_pass?force=true", hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force remove: want 200, got %d", resp.StatusCode)
	}
}

// TestSecretValueNotLeaked: значение secret не попадает в audit (3.9.4).
func TestSecretValueNotLeaked(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	resp := postJSON(t, client, srv.URL+"/api/v1/secrets",
		map[string]string{"name": "api_key", "data": "TOP-SECRET-VALUE"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("secret create: got %d", resp.StatusCode)
	}

	// Журнал не содержит значения
	resp, _ = client.Get(srv.URL + "/api/v1/audit?limit=100")
	var audit []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&audit)
	raw, _ := json.Marshal(audit)
	if strings.Contains(string(raw), "TOP-SECRET-VALUE") {
		t.Fatal("secret value leaked into audit log")
	}
	// А в Docker значение ушло (иначе secret бесполезен)
	if len(fd.secretData) != 1 || fd.secretData[0] != "TOP-SECRET-VALUE" {
		t.Fatalf("secret data not passed to docker: %+v", fd.secretData)
	}
}

// TestConfigsAndNetworks: config читаем, сеть создаётся с дефолтным overlay.
func TestConfigsAndNetworks(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// config: create → content
	resp := postJSON(t, client, srv.URL+"/api/v1/configs",
		map[string]string{"name": "nginx_conf", "data": "server {}"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config create: got %d", resp.StatusCode)
	}
	resp, _ = client.Get(srv.URL + "/api/v1/configs/cfg-nginx_conf")
	var cfg struct {
		Data string `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&cfg)
	if cfg.Data != "server {}" {
		t.Fatalf("config content: %q", cfg.Data)
	}

	// network: driver по умолчанию overlay
	resp = postJSON(t, client, srv.URL+"/api/v1/networks",
		map[string]any{"name": "backend", "attachable": true}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("network create: got %d", resp.StatusCode)
	}
	if len(fd.networks) != 1 || fd.networks[0].Driver != "overlay" || !fd.networks[0].Attachable {
		t.Fatalf("network: %+v", fd.networks)
	}
}
