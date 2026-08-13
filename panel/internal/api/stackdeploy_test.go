// Тесты деплоя стека из файла (FR-14): validate (dry-run), create (подъём),
// update+prune (diff), source (round-trip зашифрованного env).
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// yamlApp — стек app: 2 сервиса + overlay-сеть.
const yamlApp = `
services:
  web:
    image: nginx:1.27
    networks: [ net1 ]
    deploy:
      replicas: 2
  db:
    image: postgres:17
networks:
  net1:
    driver: overlay
`

// TestStackValidate: dry-run парсит и отдаёт превью, ничего не создаёт.
func TestStackValidate(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	resp := postJSON(t, client, srv.URL+"/api/v1/stacks/validate",
		map[string]any{"name": "app", "compose_yaml": yamlApp},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate: got %d", resp.StatusCode)
	}
	var pv struct {
		Name     string `json:"name"`
		Networks []string
		Services []struct {
			Name     string
			Image    string
			Replicas uint64
		}
	}
	_ = json.NewDecoder(resp.Body).Decode(&pv)
	// 2 сервиса; сеть app_net1 + автоматическая app_default (db без networks
	// вешается на default — как и настоящий docker stack deploy).
	if len(pv.Services) != 2 {
		t.Fatalf("preview services: %+v", pv)
	}
	if !contains(pv.Networks, "app_net1") {
		t.Fatalf("network app_net1 missing: %+v", pv.Networks)
	}
	// Ничего не создано в Docker.
	if len(fd.services) != 0 {
		t.Fatalf("validate must not create services, got %d", len(fd.services))
	}

	// Битый YAML → 400.
	bad := postJSON(t, client, srv.URL+"/api/v1/stacks/validate",
		map[string]any{"compose_yaml": "services: [this is: broken"},
		map[string]string{"X-CSRF-Token": csrf})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("broken yaml: want 400, got %d", bad.StatusCode)
	}
}

// TestStackDeployFromFile: create поднимает сервисы+сеть; update+prune делает
// diff (обновить/создать/удалить).
func TestStackDeployFromFile(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// --- create ---
	resp := postJSON(t, client, srv.URL+"/api/v1/stacks",
		map[string]any{"name": "app", "compose_yaml": yamlApp}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var res struct {
		Created, Updated, Removed []string
		Failed                    []struct{ Name, Error string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Created) != 2 || len(res.Failed) != 0 {
		t.Fatalf("create result: %+v", res)
	}
	if len(fd.services) != 2 {
		t.Fatalf("services created: want 2, got %d", len(fd.services))
	}
	// Сеть app_net1 создана.
	foundNet := false
	for _, n := range fd.networks {
		if n.Name == "app_net1" {
			foundNet = true
		}
	}
	if !foundNet {
		t.Fatalf("network app_net1 not created: %+v", fd.networks)
	}

	// --- update + prune: web(изменён) + cache(новый), db убран ---
	yaml2 := `
services:
  web:
    image: nginx:1.28
  cache:
    image: redis:7
`
	resp = putJSON(t, client, srv.URL+"/api/v1/stacks/app",
		map[string]any{"compose_yaml": yaml2, "prune": true}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: got %d", resp.StatusCode)
	}
	res.Created, res.Updated, res.Removed = nil, nil, nil
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if !contains(res.Updated, "app_web") {
		t.Fatalf("web must be updated: %+v", res.Updated)
	}
	if !contains(res.Created, "app_cache") {
		t.Fatalf("cache must be created: %+v", res.Created)
	}
	if !contains(res.Removed, "app_db") {
		t.Fatalf("db must be pruned: %+v", res.Removed)
	}
}

// TestStackSourceRoundtrip: env шифруется и корректно возвращается для правки.
func TestStackSourceRoundtrip(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	yaml := "services:\n  x:\n    image: alpine\n"
	resp := postJSON(t, client, srv.URL+"/api/v1/stacks",
		map[string]any{
			"name": "sec", "compose_yaml": yaml,
			"env":      "FOO=bar\n",
			"env_vars": map[string]string{"BAZ": "qux"},
		}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}

	resp, _ = client.Get(srv.URL + "/api/v1/stacks/sec/source")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("source: got %d", resp.StatusCode)
	}
	var src struct {
		Name        string            `json:"name"`
		ComposeYAML string            `json:"compose_yaml"`
		Env         string            `json:"env"`
		EnvVars     map[string]string `json:"env_vars"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&src)
	if src.Name != "sec" || !strings.Contains(src.ComposeYAML, "alpine") {
		t.Fatalf("source yaml wrong: %+v", src)
	}
	if !strings.Contains(src.Env, "FOO=bar") {
		t.Fatalf("env .env not restored: %q", src.Env)
	}
	if src.EnvVars["BAZ"] != "qux" {
		t.Fatalf("env_vars not restored: %+v", src.EnvVars)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
