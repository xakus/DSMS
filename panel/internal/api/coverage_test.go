// Тесты «читающих» эндпоинтов и смены пароля (NFR-12).
//
// Эти маршруты рисуют почти весь интерфейс, а раньше проверялись только
// глазами. Здесь важна не полнота полей, а контракт: доступ только под
// сессией, корректная агрегация и отсутствие утечек секретов в ответах.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

// testPassword — пароль, который заводит loginClient при setup.
const testPassword = "secret123"

// loginStatus пробует войти заданным паролем и возвращает HTTP-статус.
func loginStatus(t *testing.T, srv *httptest.Server, password string) int {
	t.Helper()
	client := newCookieClient(t)
	resp := postJSON(t, client, srv.URL+"/api/v1/login",
		map[string]string{"username": "admin", "password": password}, nil)
	defer resp.Body.Close()
	return resp.StatusCode
}

// clusterFixture — слепок кластера: две ноды, сервис стека, сеть, secret, config.
func clusterFixture() *fakeDocker {
	replicas := uint64(2)
	return &fakeDocker{
		nodes: []swarm.Node{
			mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
			mkNode("w1", swarm.NodeRoleWorker, swarm.NodeAvailabilityActive, swarm.NodeStateReady, false),
		},
		services: []swarm.Service{{
			ID: "svc-1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name:   "app_web",
					Labels: map[string]string{"com.docker.stack.namespace": "app"},
				},
				Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
				},
			},
		}},
		tasks: []swarm.Task{{
			ID: "task-1", ServiceID: "svc-1", NodeID: "m1",
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
			DesiredState: swarm.TaskStateRunning,
		}},
		networks: []network.Summary{{ID: "net-1", Name: "app_net1", Driver: "overlay"}},
		secrets: []swarm.Secret{{
			ID:   "sec-1",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db_pass"}},
		}},
		configs: []swarm.Config{{
			ID:   "cfg-1",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx_conf"}, Data: []byte("server {}")},
		}},
	}
}

// getJSON выполняет GET под сессией и разбирает ответ.
func getJSON(t *testing.T, client *http.Client, url string, out any) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: статус %d", url, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

// TestReadEndpoints: основные списки отдают данные кластера.
func TestReadEndpoints(t *testing.T) {
	srv, _ := newTestServer(t, clusterFixture())
	client, _ := loginClient(t, srv)

	var cluster struct {
		Nodes    int `json:"nodes"`
		Services int `json:"services"`
	}
	getJSON(t, client, srv.URL+"/api/v1/cluster", &cluster)
	if cluster.Nodes != 2 {
		t.Fatalf("сводка кластера: нод %d, ожидалось 2", cluster.Nodes)
	}

	var nodes []map[string]any
	getJSON(t, client, srv.URL+"/api/v1/nodes", &nodes)
	if len(nodes) != 2 {
		t.Fatalf("список нод: %d", len(nodes))
	}

	var node map[string]any
	getJSON(t, client, srv.URL+"/api/v1/nodes/m1", &node)
	if node["id"] != "m1" {
		t.Fatalf("детали ноды: %+v", node)
	}

	var services []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Desired int    `json:"desired"`
		Running int    `json:"running"`
	}
	getJSON(t, client, srv.URL+"/api/v1/services", &services)
	if len(services) != 1 || services[0].Name != "app_web" {
		t.Fatalf("список сервисов: %+v", services)
	}
	if services[0].Desired != 2 || services[0].Running != 1 {
		t.Fatalf("счётчики реплик неверны: %+v", services[0])
	}

	var detail map[string]any
	getJSON(t, client, srv.URL+"/api/v1/services/svc-1", &detail)
	if detail["name"] != "app_web" {
		t.Fatalf("детали сервиса: %+v", detail)
	}

	var stack map[string]any
	getJSON(t, client, srv.URL+"/api/v1/stacks/app", &stack)
	if stack["name"] != "app" {
		t.Fatalf("детали стека: %+v", stack)
	}

	var networks []map[string]any
	getJSON(t, client, srv.URL+"/api/v1/networks", &networks)
	if len(networks) != 1 {
		t.Fatalf("список сетей: %+v", networks)
	}

	var configs []map[string]any
	getJSON(t, client, srv.URL+"/api/v1/configs", &configs)
	if len(configs) != 1 {
		t.Fatalf("список конфигов: %+v", configs)
	}

	var alerts map[string]any
	getJSON(t, client, srv.URL+"/api/v1/alerts", &alerts)

	var metrics map[string]any
	getJSON(t, client, srv.URL+"/api/v1/metrics/nodes/m1", &metrics)
}

// TestReadEndpointsRequireSession: без входа наружу не отдаётся ничего —
// доступ к панели равен доступу к кластеру.
func TestReadEndpointsRequireSession(t *testing.T) {
	srv, _ := newTestServer(t, clusterFixture())

	for _, path := range []string{
		"/api/v1/cluster", "/api/v1/nodes", "/api/v1/nodes/m1",
		"/api/v1/services", "/api/v1/services/svc-1", "/api/v1/stacks",
		"/api/v1/stacks/app", "/api/v1/networks", "/api/v1/secrets",
		"/api/v1/configs", "/api/v1/audit", "/api/v1/alerts", "/api/v1/registries",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s без сессии: want 401, got %d", path, resp.StatusCode)
		}
	}
}

// TestConfigContentReadable: configs читаемы (3.9.3) — в отличие от secrets.
func TestConfigContentReadable(t *testing.T) {
	srv, _ := newTestServer(t, clusterFixture())
	client, _ := loginClient(t, srv)

	var cfg struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	getJSON(t, client, srv.URL+"/api/v1/configs/cfg-1", &cfg)
	if cfg.Data != "server {}" {
		t.Fatalf("содержимое config должно читаться: %+v", cfg)
	}
}

// TestChangePassword: пароль меняется только со знанием старого, новая
// сессия работает, короткий пароль отклоняется.
func TestChangePassword(t *testing.T) {
	srv, _ := newTestServer(t, clusterFixture())
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// Неверный текущий пароль.
	resp := postJSON(t, client, srv.URL+"/api/v1/settings/password",
		map[string]any{"old": "не-тот", "new": "новый-пароль-123"}, hdr)
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("смена пароля без верного старого: got %d", resp.StatusCode)
	}

	// Слишком короткий новый пароль.
	resp = postJSON(t, client, srv.URL+"/api/v1/settings/password",
		map[string]any{"old": testPassword, "new": "123"}, hdr)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("короткий пароль: want 400, got %d", resp.StatusCode)
	}

	// Успешная смена.
	resp = postJSON(t, client, srv.URL+"/api/v1/settings/password",
		map[string]any{"old": testPassword, "new": "новый-пароль-123"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("смена пароля: got %d", resp.StatusCode)
	}

	// Старый пароль больше не работает, новый — работает.
	if code := loginStatus(t, srv, testPassword); code != http.StatusUnauthorized {
		t.Fatalf("вход со старым паролем: want 401, got %d", code)
	}
	if code := loginStatus(t, srv, "новый-пароль-123"); code != http.StatusOK {
		t.Fatalf("вход с новым паролем: want 200, got %d", code)
	}
}
