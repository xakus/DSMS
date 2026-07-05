// Тесты сервисов/стеков/реестров (FR-04, FR-08, FR-13).
package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// clusterWithServices — фикстура: 1 manager, 2 сервиса в стеке "app", 1 вне стека.
func clusterWithServices() *fakeDocker {
	return &fakeDocker{
		nodes: []swarm.Node{
			mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		},
		services: []swarm.Service{
			mkService("s1", "app_web", "nginx:1.27", 3, "app"),
			mkService("s2", "app_db", "postgres:17", 1, "app"),
			mkService("s3", "standalone", "redis:7", 2, ""),
		},
	}
}

// TestServiceStopStart: Stop=scale 0 запоминает реплики, Start возвращает (3.4.3).
func TestServiceStopStart(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// Stop: scale 0
	resp := postJSON(t, client, srv.URL+"/api/v1/services/s1/scale",
		map[string]int{"replicas": 0}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scale 0: got %d", resp.StatusCode)
	}
	if got := *fd.services[0].Spec.Mode.Replicated.Replicas; got != 0 {
		t.Fatalf("replicas after stop: want 0, got %d", got)
	}

	// Start: восстановление 3 реплик из service_state
	resp = postJSON(t, client, srv.URL+"/api/v1/services/s1/start", nil, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start: got %d", resp.StatusCode)
	}
	if got := *fd.services[0].Spec.Mode.Replicated.Replicas; got != 3 {
		t.Fatalf("replicas after start: want 3, got %d", got)
	}

	// Повторный start без stop — 409 (записи больше нет)
	resp = postJSON(t, client, srv.URL+"/api/v1/services/s1/start", nil, hdr)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("double start: want 409, got %d", resp.StatusCode)
	}
}

// TestServiceRedeploy: ForceUpdate инкрементируется (3.4.3).
func TestServiceRedeploy(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	before := fd.services[0].Spec.TaskTemplate.ForceUpdate
	resp := postJSON(t, client, srv.URL+"/api/v1/services/s1/redeploy", nil,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("redeploy: got %d", resp.StatusCode)
	}
	if got := fd.services[0].Spec.TaskTemplate.ForceUpdate; got != before+1 {
		t.Fatalf("ForceUpdate: want %d, got %d", before+1, got)
	}
}

// TestServiceRollback: rollback уходит в Docker с флагом previous.
func TestServiceRollback(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	resp := postJSON(t, client, srv.URL+"/api/v1/services/s1/rollback", nil,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rollback: got %d", resp.StatusCode)
	}
	last := fd.updates[len(fd.updates)-1]
	if last.Rollback != "previous" {
		t.Fatalf("rollback flag: want previous, got %q", last.Rollback)
	}
}

// TestStacks: группировка по label и redeploy всего стека (FR-08).
func TestStacks(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	// список стеков: app (2 сервиса) + (no stack) (1 сервис)
	resp, _ := client.Get(srv.URL + "/api/v1/stacks")
	var stacks []struct {
		Name     string `json:"name"`
		Services int    `json:"services"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&stacks)
	if len(stacks) != 2 {
		t.Fatalf("stacks: want 2 groups, got %+v", stacks)
	}
	byName := map[string]int{}
	for _, s := range stacks {
		byName[s.Name] = s.Services
	}
	if byName["app"] != 2 || byName["(no stack)"] != 1 {
		t.Fatalf("grouping wrong: %+v", byName)
	}

	// redeploy стека: ForceUpdate++ у обоих сервисов app
	resp = postJSON(t, client, srv.URL+"/api/v1/stacks/app/redeploy", nil,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stack redeploy: got %d", resp.StatusCode)
	}
	if fd.services[0].Spec.TaskTemplate.ForceUpdate != 1 ||
		fd.services[1].Spec.TaskTemplate.ForceUpdate != 1 {
		t.Fatal("stack redeploy must bump ForceUpdate on all stack services")
	}
	if fd.services[2].Spec.TaskTemplate.ForceUpdate != 0 {
		t.Fatal("stack redeploy must not touch services outside the stack")
	}

	// удаление стека: остаётся только standalone
	resp = del(t, client, srv.URL+"/api/v1/stacks/app", map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stack remove: got %d", resp.StatusCode)
	}
	if len(fd.services) != 1 || fd.services[0].ID != "s3" {
		t.Fatalf("after stack remove: %+v", fd.services)
	}
}

// TestRegistries: CRUD, пароль шифруется и не отдаётся клиенту (FR-13).
func TestRegistries(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// создание
	resp := postJSON(t, client, srv.URL+"/api/v1/registries",
		map[string]string{"address": "ghcr.io", "username": "bot", "password": "s3cret", "label": "GitHub"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registry create: got %d", resp.StatusCode)
	}
	raw, _ := json.Marshal(readBody(t, resp))
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("password leaked in create response")
	}

	// список — пароль отсутствует
	resp, _ = client.Get(srv.URL + "/api/v1/registries")
	var regs []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&regs)
	if len(regs) != 1 {
		t.Fatalf("registries: want 1, got %d", len(regs))
	}
	raw, _ = json.Marshal(regs)
	if strings.Contains(string(raw), "s3cret") || strings.Contains(string(raw), "password") {
		t.Fatal("password (or field) leaked in list response")
	}

	// update image с registry auth: X-Registry-Auth содержит пароль (для Docker)
	resp = postJSON(t, client, srv.URL+"/api/v1/services/s1/image",
		map[string]any{"image": "ghcr.io/x/app:2.0", "registry_id": 1}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("image update: got %d", resp.StatusCode)
	}
	last := fd.updates[len(fd.updates)-1]
	if last.Spec.TaskTemplate.ContainerSpec.Image != "ghcr.io/x/app:2.0" {
		t.Fatalf("image not applied: %s", last.Spec.TaskTemplate.ContainerSpec.Image)
	}
	decoded, err := base64.URLEncoding.DecodeString(last.RegistryAuth)
	if err != nil || !strings.Contains(string(decoded), "s3cret") {
		t.Fatal("X-Registry-Auth must carry decrypted password to Docker API")
	}

	// удаление
	resp = del(t, client, srv.URL+"/api/v1/registries/1", hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registry delete: got %d", resp.StatusCode)
	}
}

// readBody декодирует JSON-ответ в map.
func readBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m
}
