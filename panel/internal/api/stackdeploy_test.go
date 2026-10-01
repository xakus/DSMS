// Тесты деплоя стека из файла (FR-14): validate (dry-run), create (подъём),
// update+prune (diff), source (round-trip зашифрованного env).
package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
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

// yamlWithSecrets — стек с секретом из env, конфигом и томом.
const yamlWithSecrets = `
services:
  api:
    image: alpine
    volumes:
      - data:/var/lib/data
    secrets: [ jwt ]
    configs: [ app_conf ]
    networks: [ net1 ]
volumes:
  data: {}
networks:
  net1:
    driver: overlay
secrets:
  jwt:
    environment: JWT_SECRET
configs:
  app_conf:
    content: "debug=false"
`

// TestStackDeploySecretsAndLabels (A-02, A-04): секрет и конфиг создаются,
// ID подставляется в ссылки сервиса, на сети стоит label стека.
func TestStackDeploySecretsAndLabels(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	resp := postJSON(t, client, srv.URL+"/api/v1/stacks", map[string]any{
		"name": "app", "compose_yaml": yamlWithSecrets,
		"env": "JWT_SECRET=topsecret\n",
	}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var res struct {
		Created, Resources []string
		Failed             []struct{ Name, Error string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Failed) != 0 {
		t.Fatalf("деплой с секретами не должен падать: %+v", res.Failed)
	}
	if !contains(res.Resources, "secret app_jwt") || !contains(res.Resources, "config app_app_conf") {
		t.Fatalf("secret/config не созданы: %+v", res.Resources)
	}

	// Секрет создан с содержимым из env и label стека.
	if len(fd.secrets) != 1 || fd.secretData[0] != "topsecret" {
		t.Fatalf("содержимое секрета неверно: %+v", fd.secretData)
	}
	if fd.secrets[0].Spec.Labels["com.docker.stack.namespace"] != "app" {
		t.Fatalf("label стека на секрете потерян: %+v", fd.secrets[0].Spec.Labels)
	}

	// Label стека на созданной сети (без него сеть не опознаётся как часть стека).
	var net1 *network.Summary
	for i := range fd.networks {
		if fd.networks[i].Name == "app_net1" {
			net1 = &fd.networks[i]
		}
	}
	if net1 == nil || net1.Labels["com.docker.stack.namespace"] != "app" {
		t.Fatalf("label стека на сети потерян: %+v", net1)
	}

	// В спеке сервиса: ID секрета подставлен, том смонтирован.
	if len(fd.services) != 1 {
		t.Fatalf("services: want 1, got %d", len(fd.services))
	}
	cs := fd.services[0].Spec.TaskTemplate.ContainerSpec
	if len(cs.Secrets) != 1 || cs.Secrets[0].SecretID == "" {
		t.Fatalf("ID секрета не подставлен: %+v", cs.Secrets)
	}
	if len(cs.Configs) != 1 || cs.Configs[0].ConfigID == "" {
		t.Fatalf("ID конфига не подставлен: %+v", cs.Configs)
	}
	if len(cs.Mounts) != 1 || cs.Mounts[0].Source != "app_data" {
		t.Fatalf("том сервиса потерян: %+v", cs.Mounts)
	}
}

// TestStackValidateWarningsAndMissing (A-03, A-05): превью показывает
// непереносимые поля и отсутствующие external-ресурсы.
func TestStackValidateWarningsAndMissing(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	yaml := `
services:
  web:
    image: nginx
    depends_on: [ db ]
    networks: [ ext ]
  db:
    image: postgres:17
networks:
  ext:
    external: true
    name: missing-net
`
	resp := postJSON(t, client, srv.URL+"/api/v1/stacks/validate",
		map[string]any{"name": "app", "compose_yaml": yaml},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate: got %d", resp.StatusCode)
	}
	var pv struct {
		Warnings []struct{ Service, Field, Message string }
		Missing  []string
		Changes  struct{ Create, Update, Remove []string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&pv)

	foundDepends := false
	for _, w := range pv.Warnings {
		if w.Field == "depends_on" {
			foundDepends = true
		}
	}
	if !foundDepends {
		t.Fatalf("нет предупреждения о depends_on: %+v", pv.Warnings)
	}
	if !contains(pv.Missing, "network missing-net") {
		t.Fatalf("отсутствующая external-сеть не найдена: %+v", pv.Missing)
	}
	// Стека ещё нет — оба сервиса на создание.
	if len(pv.Changes.Create) != 2 || len(pv.Changes.Update) != 0 {
		t.Fatalf("diff неверен: %+v", pv.Changes)
	}
}

// TestStackBodyLimit (A-06, ТЗ 3.14.11): гигантский файл отбивается 413,
// а не съедает память панели.
func TestStackBodyLimit(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	huge := strings.Repeat("x", 2<<20) // 2 МБ при лимите 1 МБ
	resp := postJSON(t, client, srv.URL+"/api/v1/stacks/validate",
		map[string]any{"name": "app", "compose_yaml": huge},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("большое тело: want 413, got %d", resp.StatusCode)
	}
}

// TestStackDeployPrivateRegistry: стек с registry_id тянет приватные образы с
// X-Registry-Auth при create/update, запоминает реестр и использует его в
// «Деплое стека»; несуществующий реестр — 400 до сохранения (FR-13 × FR-14).
func TestStackDeployPrivateRegistry(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	resp := postJSON(t, client, srv.URL+"/api/v1/registries",
		map[string]string{"address": "docker.io", "username": "bot", "password": "s3cret", "label": "Hub"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registry create: got %d", resp.StatusCode)
	}
	const yamlPrivate = `
services:
  api:
    image: bot/private-api:1.0
`
	// Несуществующий реестр — 400, стек не сохраняется и не разворачивается.
	resp = postJSON(t, client, srv.URL+"/api/v1/stacks",
		map[string]any{"name": "pay", "compose_yaml": yamlPrivate, "registry_id": 99}, hdr)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown registry: want 400, got %d", resp.StatusCode)
	}
	if len(fd.services) != 0 {
		t.Fatalf("при ошибке реестра сервисы не создаются: %d", len(fd.services))
	}

	// Создание с реестром: ServiceCreate получает расшифрованную учётку.
	resp = postJSON(t, client, srv.URL+"/api/v1/stacks",
		map[string]any{"name": "pay", "compose_yaml": yamlPrivate, "registry_id": 1}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	assertAuth := func(step string) {
		t.Helper()
		last := fd.updates[len(fd.updates)-1]
		decoded, err := base64.URLEncoding.DecodeString(last.RegistryAuth)
		if err != nil || !strings.Contains(string(decoded), "s3cret") {
			t.Fatalf("%s: X-Registry-Auth должен нести учётку реестра", step)
		}
	}
	assertAuth("create")

	// Реестр запомнен у стека — его видно в исходнике.
	resp, _ = client.Get(srv.URL + "/api/v1/stacks/pay/source")
	src := readBody(t, resp)
	if id, ok := src["registry_id"].(float64); !ok || id != 1 {
		t.Fatalf("source.registry_id: %v", src["registry_id"])
	}

	// Обновление из файла — ServiceUpdate тоже с учёткой.
	resp = putJSON(t, client, srv.URL+"/api/v1/stacks/pay",
		map[string]any{"compose_yaml": strings.Replace(yamlPrivate, "1.0", "1.1", 1), "registry_id": 1}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: got %d", resp.StatusCode)
	}
	assertAuth("update")

	// «Деплой стека» (свежие образы по тегу) берёт реестр из сохранённого стека.
	resp = postJSON(t, client, srv.URL+"/api/v1/stacks/pay/deploy", map[string]any{}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stack deploy: got %d", resp.StatusCode)
	}
	assertAuth("stack deploy")
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
