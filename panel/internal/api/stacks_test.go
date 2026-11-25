// Тесты управления стеком целиком и потребления сервисов (этап 14):
// FR-08 3.8.2, 3.8.3, 3.8.6; FR-04 3.4.1, 3.4.7; FR-14 3.14.13.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/xakus/DSMS/panel/internal/metrics"
)

// opResult — сводка групповой операции (3.8.6), как её видит клиент.
type opResult struct {
	Done    []string `json:"done"`
	Skipped []struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Failed []struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	} `json:"failed"`
	Resources []string `json:"resources"`
}

// skipReasons сворачивает «пропущено» в карту имя → код причины.
func (r opResult) skipReasons() map[string]string {
	out := map[string]string{}
	for _, s := range r.Skipped {
		out[s.Name] = s.Reason
	}
	return out
}

// stackPost выполняет POST-действие над стеком и разбирает сводку.
func stackPost(t *testing.T, client *http.Client, url, csrf string) opResult {
	t.Helper()
	resp := postJSON(t, client, url, nil, map[string]string{"X-CSRF-Token": csrf})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: got %d", url, resp.StatusCode)
	}
	var res opResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return res
}

// mkGlobal — конструктор global-сервиса (реплик у него нет).
func mkGlobal(id, name, stack string) swarm.Service {
	return swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: name, Labels: map[string]string{"com.docker.stack.namespace": stack},
			},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "agent:1"}},
			Mode:         swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	}
}

// replicasOf возвращает текущее число реплик сервиса в слепке кластера.
func replicasOf(t *testing.T, fd *fakeDocker, id string) uint64 {
	t.Helper()
	for _, s := range fd.services {
		if s.ID == id {
			return *s.Spec.Mode.Replicated.Replicas
		}
	}
	t.Fatalf("service %s not found", id)
	return 0
}

// stackRow — строка /stacks нужного стека.
type stackRow struct {
	Name      string `json:"name"`
	Services  int    `json:"services"`
	Stopped   int    `json:"stopped"`
	Stoppable int    `json:"stoppable"`
	Managed   bool   `json:"managed"`
	Usage     *struct {
		CPUCores float64 `json:"cpu_cores"`
		MemUsed  uint64  `json:"mem_used"`
		Tasks    int     `json:"tasks"`
	} `json:"usage"`
}

// findStack ищет стек в ответе /stacks; ok=false — стека в списке нет.
func findStack(t *testing.T, client *http.Client, base, name string) (stackRow, bool) {
	t.Helper()
	var rows []stackRow
	getJSON(t, client, base+"/api/v1/stacks", &rows)
	for _, r := range rows {
		if r.Name == name {
			return r, true
		}
	}
	return stackRow{}, false
}

// TestStackStopStart: Stop гасит все replicated-сервисы стека и запоминает
// реплики, Start возвращает их; global пропускается; чужие сервисы не
// трогаются (3.8.3).
func TestStackStopStart(t *testing.T) {
	fd := clusterWithServices()
	fd.services = append(fd.services, mkGlobal("g1", "app_agent", "app"))
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	base := srv.URL + "/api/v1/stacks/app"

	// До остановки: два сервиса можно остановить, остановленных нет.
	if row, _ := findStack(t, client, srv.URL, "app"); row.Stoppable != 2 || row.Stopped != 0 {
		t.Fatalf("before stop: %+v", row)
	}

	res := stackPost(t, client, base+"/stop", csrf)
	if len(res.Done) != 2 || len(res.Failed) != 0 {
		t.Fatalf("stop summary: %+v", res)
	}
	if got := res.skipReasons()["app_agent"]; got != "global" {
		t.Fatalf("global service must be skipped with reason global, got %q", got)
	}
	if replicasOf(t, fd, "s1") != 0 || replicasOf(t, fd, "s2") != 0 {
		t.Fatal("stack services must be scaled to 0")
	}
	if replicasOf(t, fd, "s3") != 2 {
		t.Fatal("service outside the stack must not be touched")
	}
	if row, _ := findStack(t, client, srv.URL, "app"); row.Stopped != 2 || row.Stoppable != 0 {
		t.Fatalf("after stop: %+v", row)
	}

	// Повторный Stop: оба уже остановлены, реплики не затёрты нулём.
	res = stackPost(t, client, base+"/stop", csrf)
	if len(res.Done) != 0 || res.skipReasons()["app_web"] != "already_stopped" {
		t.Fatalf("second stop: %+v", res)
	}

	res = stackPost(t, client, base+"/start", csrf)
	if len(res.Done) != 2 || len(res.Failed) != 0 {
		t.Fatalf("start summary: %+v", res)
	}
	if replicasOf(t, fd, "s1") != 3 || replicasOf(t, fd, "s2") != 1 {
		t.Fatalf("replicas must be restored: web=%d db=%d",
			replicasOf(t, fd, "s1"), replicasOf(t, fd, "s2"))
	}

	// Повторный Start: запускать нечего.
	res = stackPost(t, client, base+"/start", csrf)
	if len(res.Done) != 0 || res.skipReasons()["app_web"] != "not_stopped" {
		t.Fatalf("second start: %+v", res)
	}

	// Стека нет — 404, а не «успех с нулём сервисов».
	resp := postJSON(t, client, srv.URL+"/api/v1/stacks/nope/stop", nil,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown stack: want 404, got %d", resp.StatusCode)
	}
}

// TestStackStartIgnoresStaleState: сервис остановили панелью, потом подняли
// в обход неё на 5 реплик. Start стека не должен «вернуть» ему старые 3.
func TestStackStartIgnoresStaleState(t *testing.T) {
	fd := clusterWithServices()
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	base := srv.URL + "/api/v1/stacks/app"

	stackPost(t, client, base+"/stop", csrf)
	five := uint64(5)
	fd.services[0].Spec.Mode.Replicated.Replicas = &five // как `docker service scale`

	res := stackPost(t, client, base+"/start", csrf)
	if res.skipReasons()["app_web"] != "not_stopped" {
		t.Fatalf("running service must be skipped: %+v", res)
	}
	if got := replicasOf(t, fd, "s1"); got != 5 {
		t.Fatalf("replicas must stay 5, got %d", got)
	}
	if got := replicasOf(t, fd, "s2"); got != 1 {
		t.Fatalf("stopped service must be started, got %d", got)
	}
}

// TestStackOpPartialFailure: ошибка одного сервиса не обрывает операцию и
// видна в сводке с именем (3.8.6); упавший Stop не оставляет запись
// «остановлен панелью».
func TestStackOpPartialFailure(t *testing.T) {
	fd := clusterWithServices()
	fd.failUpdate = map[string]bool{"s1": true}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	base := srv.URL + "/api/v1/stacks/app"

	res := stackPost(t, client, base+"/redeploy", csrf)
	if len(res.Done) != 1 || res.Done[0] != "app_db" {
		t.Fatalf("healthy service must be redeployed: %+v", res)
	}
	if len(res.Failed) != 1 || res.Failed[0].Name != "app_web" || res.Failed[0].Error == "" {
		t.Fatalf("failed service must be reported by name: %+v", res)
	}

	res = stackPost(t, client, base+"/stop", csrf)
	if len(res.Failed) != 1 || res.Failed[0].Name != "app_web" {
		t.Fatalf("stop failure must be reported: %+v", res)
	}
	// app_web не остановился — запись о нём должна быть откатана.
	fd.failUpdate = nil
	res = stackPost(t, client, base+"/start", csrf)
	if res.skipReasons()["app_web"] != "not_stopped" {
		t.Fatalf("failed stop must not leave saved state: %+v", res)
	}
	if got := replicasOf(t, fd, "s1"); got != 3 {
		t.Fatalf("app_web replicas must be untouched, got %d", got)
	}
}

// yamlFull — стек с сетью, secret, config и томом (все — свои, с label стека).
const yamlFull = `
services:
  web:
    image: nginx:1.27
    networks: [ net1, shared ]
    secrets: [ token ]
    configs: [ site ]
    volumes: [ "data:/var/lib/data" ]
networks:
  net1:
    driver: overlay
  shared:
    external: true
secrets:
  token:
    environment: TOKEN
configs:
  site:
    content: "server {}"
volumes:
  data: {}
`

// deployFull разворачивает yamlFull как стек full; сеть shared — внешняя.
func deployFull(t *testing.T, fd *fakeDocker, client *http.Client, base, csrf string) {
	t.Helper()
	fd.networks = append(fd.networks, network.Summary{ID: "net-shared", Name: "shared", Driver: "overlay"})
	resp := postJSON(t, client, base+"/api/v1/stacks",
		map[string]any{"name": "full", "compose_yaml": yamlFull, "env": "TOKEN=s3cr3t"},
		map[string]string{"X-CSRF-Token": csrf})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deploy full: got %d", resp.StatusCode)
	}
	var res struct {
		Failed []struct{ Name, Error string } `json:"failed"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Failed) != 0 {
		t.Fatalf("deploy full failed: %+v", res.Failed)
	}
}

// TestStackDetailResources: страница стека показывает сервисы и
// задействованные сети/тома/секреты/конфиги, отличая свои от внешних (3.8.2).
func TestStackDetailResources(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	deployFull(t, fd, client, srv.URL, csrf)

	type res struct {
		Name     string `json:"name"`
		External bool   `json:"external"`
	}
	var detail struct {
		Name     string `json:"name"`
		Managed  bool   `json:"managed"`
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
		Networks []res    `json:"networks"`
		Secrets  []res    `json:"secrets"`
		Configs  []res    `json:"configs"`
		Volumes  []string `json:"volumes"`
	}
	getJSON(t, client, srv.URL+"/api/v1/stacks/full", &detail)

	if !detail.Managed || len(detail.Services) != 1 || detail.Services[0].Name != "full_web" {
		t.Fatalf("detail header: %+v", detail)
	}
	ext := map[string]bool{}
	for _, n := range detail.Networks {
		ext[n.Name] = n.External
	}
	if e, ok := ext["full_net1"]; !ok || e {
		t.Fatalf("own network must be listed as not external: %+v", detail.Networks)
	}
	if e, ok := ext["shared"]; !ok || !e {
		t.Fatalf("external network must be marked external: %+v", detail.Networks)
	}
	if len(detail.Secrets) != 1 || detail.Secrets[0].Name != "full_token" {
		t.Fatalf("secrets: %+v", detail.Secrets)
	}
	if len(detail.Configs) != 1 || detail.Configs[0].Name != "full_site" {
		t.Fatalf("configs: %+v", detail.Configs)
	}
	if len(detail.Volumes) != 1 || detail.Volumes[0] != "full_data" {
		t.Fatalf("volumes: %+v", detail.Volumes)
	}
}

// TestStackRemoveResources: удаление стека сносит сервисы и объекты с label
// стека (сети, secrets, configs), но не трогает внешнюю сеть (3.8.3).
func TestStackRemoveResources(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	deployFull(t, fd, client, srv.URL, csrf)

	resp := del(t, client, srv.URL+"/api/v1/stacks/full", map[string]string{"X-CSRF-Token": csrf})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove: got %d", resp.StatusCode)
	}
	var res opResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Done) != 1 || len(res.Failed) != 0 {
		t.Fatalf("remove summary: %+v", res)
	}
	removed := strings.Join(res.Resources, ",")
	for _, want := range []string{"network full_net1", "secret full_token", "config full_site"} {
		if !strings.Contains(removed, want) {
			t.Fatalf("%q must be removed, got %q", want, removed)
		}
	}
	if len(fd.services) != 0 || len(fd.secrets) != 0 || len(fd.configs) != 0 {
		t.Fatalf("leftovers: services=%d secrets=%d configs=%d",
			len(fd.services), len(fd.secrets), len(fd.configs))
	}
	if len(fd.networks) != 1 || fd.networks[0].Name != "shared" {
		t.Fatalf("external network must survive: %+v", fd.networks)
	}
	// Исходник убран — стек исчез из списка.
	if _, ok := findStack(t, client, srv.URL, "full"); ok {
		t.Fatal("removed stack must disappear from the list")
	}
}

// TestStackRemoveKeepsSourceOnFailure: сервис не удалился → его сеть не
// трогаем и запись стека оставляем, чтобы удаление можно было повторить.
func TestStackRemoveKeepsSourceOnFailure(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	deployFull(t, fd, client, srv.URL, csrf)
	fd.failRemove = map[string]bool{"svc-full_web": true}

	resp := del(t, client, srv.URL+"/api/v1/stacks/full", map[string]string{"X-CSRF-Token": csrf})
	defer resp.Body.Close()
	var res opResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Failed) != 1 || res.Failed[0].Name != "full_web" {
		t.Fatalf("failure must be reported: %+v", res)
	}
	if len(res.Resources) != 0 || len(fd.secrets) != 1 {
		t.Fatal("resources of a still-alive service must not be removed")
	}
	if row, ok := findStack(t, client, srv.URL, "full"); !ok || !row.Managed {
		t.Fatal("stack must stay in the list until removal succeeds")
	}
}

// TestStackRemoveManagedWithoutServices: managed-стек без живых сервисов
// (деплой упал целиком) раньше отвечал 404 и оставался в списке навсегда.
func TestStackRemoveManagedWithoutServices(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	resp := postJSON(t, client, srv.URL+"/api/v1/stacks",
		map[string]any{"name": "ghost", "compose_yaml": yamlApp}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	fd.services = nil // сервисы пропали в обход панели
	fd.networks = nil

	if row, ok := findStack(t, client, srv.URL, "ghost"); !ok || row.Services != 0 {
		t.Fatalf("empty managed stack must be listed: %+v ok=%v", row, ok)
	}
	// Страница такого стека открывается, а не 404.
	var detail map[string]any
	getJSON(t, client, srv.URL+"/api/v1/stacks/ghost", &detail)
	if detail["managed"] != true {
		t.Fatalf("detail of empty managed stack: %+v", detail)
	}

	if resp := del(t, client, srv.URL+"/api/v1/stacks/ghost", hdr); resp.StatusCode != http.StatusOK {
		t.Fatalf("remove empty managed stack: want 200, got %d", resp.StatusCode)
	}
	if _, ok := findStack(t, client, srv.URL, "ghost"); ok {
		t.Fatal("stack must be gone after removal")
	}
	// Совсем неизвестный стек — по-прежнему 404.
	if resp := del(t, client, srv.URL+"/api/v1/stacks/ghost", hdr); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown stack: want 404, got %d", resp.StatusCode)
	}
}

// TestStackNameValidation: плохое имя отклоняется ДО сохранения исходника —
// иначе в БД остаётся стек, который нельзя развернуть (3.14.13).
func TestStackNameValidation(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	bad := []string{"", "a?b", "стек", "-app", "_app", "my.stack", "a b", "a/b", strings.Repeat("a", 51)}
	for _, name := range bad {
		resp := postJSON(t, client, srv.URL+"/api/v1/stacks",
			map[string]any{"name": name, "compose_yaml": yamlApp}, hdr)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("name %q: want 400, got %d", name, resp.StatusCode)
		}
	}
	var rows []stackRow
	getJSON(t, client, srv.URL+"/api/v1/stacks", &rows)
	if len(rows) != 0 || len(fd.services) != 0 {
		t.Fatalf("nothing must be saved or created: rows=%+v services=%d", rows, len(fd.services))
	}

	// Граница: ровно 50 символов и допустимые «_», «-» — проходит.
	for _, name := range []string{strings.Repeat("a", 50), "my-app_2"} {
		resp := postJSON(t, client, srv.URL+"/api/v1/stacks",
			map[string]any{"name": name, "compose_yaml": yamlApp}, hdr)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("name %q: want 200, got %d", name, resp.StatusCode)
		}
	}
}

// TestStackFileDeployClearsStoppedState: стек остановили, потом передеплоили
// из файла — сервисы снова работают, и запись «остановлен панелью» обязана
// исчезнуть (иначе позже сервис с 0 реплик примут за остановленный).
func TestStackFileDeployClearsStoppedState(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}
	body := map[string]any{"name": "app", "compose_yaml": yamlApp}

	if resp := postJSON(t, client, srv.URL+"/api/v1/stacks", body, hdr); resp.StatusCode != http.StatusOK {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	stackPost(t, client, srv.URL+"/api/v1/stacks/app/stop", csrf)
	if row, _ := findStack(t, client, srv.URL, "app"); row.Stopped != 2 {
		t.Fatalf("after stop: %+v", row)
	}
	if resp := putJSON(t, client, srv.URL+"/api/v1/stacks/app", body, hdr); resp.StatusCode != http.StatusOK {
		t.Fatalf("redeploy from file: got %d", resp.StatusCode)
	}
	// Гасим сервисы в обход панели — «остановленными панелью» они быть не должны.
	zero := uint64(0)
	for i := range fd.services {
		fd.services[i].Spec.Mode.Replicated.Replicas = &zero
	}
	if row, _ := findStack(t, client, srv.URL, "app"); row.Stopped != 0 {
		t.Fatalf("stale stopped state must be cleared: %+v", row)
	}
}

// usageJSON — потребление в ответах /services и /services/{id}.
type usageJSON struct {
	CPUCores float64 `json:"cpu_cores"`
	MemUsed  uint64  `json:"mem_used"`
	MemLimit uint64  `json:"mem_limit"`
	RxBps    uint64  `json:"rx_bps"`
	TxBps    uint64  `json:"tx_bps"`
	Tasks    int     `json:"tasks"`
}

// usageCluster — 2 ноды; app_web: по running-задаче на каждой ноде + одна
// упавшая; app_db: одна running-задача на m1.
func usageCluster() *fakeDocker {
	fd := clusterWithServices()
	fd.nodes = append(fd.nodes,
		mkNode("w1", swarm.NodeRoleWorker, swarm.NodeAvailabilityActive, swarm.NodeStateReady, false))
	run := func(id, svc, node string) swarm.Task {
		return swarm.Task{
			ID: id, ServiceID: svc, NodeID: node,
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
			DesiredState: swarm.TaskStateRunning,
		}
	}
	fd.tasks = []swarm.Task{
		run("t-web-1", "s1", "m1"),
		run("t-web-2", "s1", "w1"),
		{ID: "t-web-old", ServiceID: "s1", NodeID: "m1",
			Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
			DesiredState: swarm.TaskStateShutdown},
		run("t-db-1", "s2", "m1"),
	}
	return fd
}

// ingest шлёт батч агента с метриками задач.
func ingest(t *testing.T, base string, snap metrics.Snapshot) {
	t.Helper()
	resp := postJSON(t, http.DefaultClient, base+"/api/v1/ingest", snap,
		map[string]string{"X-Agent-Token": "test-token"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ingest: got %d", resp.StatusCode)
	}
}

// TestServiceUsage: потребление сервиса = сумма по его running-задачам со
// всех нод; ноды размещения — hostname нод с running-задачами (3.4.1, 3.4.7).
func TestServiceUsage(t *testing.T) {
	const mb = 1 << 20
	fd := usageCluster()
	srv, buf := newTestServer(t, fd)
	client, _ := loginClient(t, srv)

	// До первого батча метрик нет: usage отсутствует (в UI — прочерк, не ноль).
	var before []struct {
		Name  string     `json:"name"`
		Usage *usageJSON `json:"usage"`
	}
	getJSON(t, client, srv.URL+"/api/v1/services", &before)
	for _, s := range before {
		if s.Usage != nil {
			t.Fatalf("%s: usage must be absent without metrics, got %+v", s.Name, s.Usage)
		}
	}

	ingest(t, srv.URL, metrics.Snapshot{NodeID: "m1", TS: 1, Tasks: []metrics.Task{
		{Task: "t-web-1", CPUCores: 0.25, MemUsed: 100 * mb, MemLimit: 512 * mb, RxBps: 1000, TxBps: 200},
		{Task: "t-db-1", CPUCores: 1.5, MemUsed: 800 * mb, MemLimit: 0, RxBps: 50, TxBps: 60},
		// Метрики мёртвой задачи в сумму попасть не должны.
		{Task: "t-web-old", CPUCores: 9, MemUsed: 9000 * mb},
	}})
	ingest(t, srv.URL, metrics.Snapshot{NodeID: "w1", TS: 1, Tasks: []metrics.Task{
		{Task: "t-web-2", CPUCores: 0.1, MemUsed: 50 * mb, MemLimit: 512 * mb, RxBps: 3000, TxBps: 400},
	}})

	// tasks вырезаются из снапшота: в кольцевом буфере их быть не должно (NFR-2a).
	if snap, ok := buf.Latest("m1"); !ok || snap.Tasks != nil {
		t.Fatalf("tasks must be stripped from the ring buffer: ok=%v tasks=%v", ok, snap.Tasks)
	}

	var list []struct {
		ID      string     `json:"id"`
		Running int        `json:"running"`
		Nodes   []string   `json:"nodes"`
		Usage   *usageJSON `json:"usage"`
	}
	getJSON(t, client, srv.URL+"/api/v1/services", &list)
	byID := map[string]int{}
	for i, s := range list {
		byID[s.ID] = i
	}

	web := list[byID["s1"]]
	if web.Running != 2 || len(web.Nodes) != 2 || web.Nodes[0] != "host-m1" || web.Nodes[1] != "host-w1" {
		t.Fatalf("web placement: %+v", web)
	}
	// 0.25 + 0.1 ядра; 100 + 50 МБ; лимит 512 + 512 МБ; сеть 1000+3000 / 200+400.
	want := usageJSON{CPUCores: 0.35, MemUsed: 150 * mb, MemLimit: 1024 * mb, RxBps: 4000, TxBps: 600, Tasks: 2}
	if web.Usage == nil || *web.Usage != want {
		t.Fatalf("web usage: want %+v, got %+v", want, web.Usage)
	}

	// У db лимита нет — лимит сервиса не показываем (0).
	db := list[byID["s2"]]
	if db.Usage == nil || db.Usage.MemLimit != 0 || db.Usage.CPUCores != 1.5 || db.Usage.Tasks != 1 {
		t.Fatalf("db usage: %+v", db.Usage)
	}
	// Сервис без задач — без потребления и без нод.
	if st := list[byID["s3"]]; st.Usage != nil || len(st.Nodes) != 0 {
		t.Fatalf("standalone: %+v", st)
	}

	// Страница сервиса: потребление у каждой running-задачи и сумма.
	var detail struct {
		Running int        `json:"running"`
		Desired int        `json:"desired"`
		Nodes   []string   `json:"nodes"`
		Usage   *usageJSON `json:"usage"`
		Tasks   []struct {
			ID     string     `json:"id"`
			Node   string     `json:"node"`
			NodeID string     `json:"node_id"`
			Usage  *usageJSON `json:"usage"`
		} `json:"tasks"`
	}
	getJSON(t, client, srv.URL+"/api/v1/services/s1", &detail)
	if detail.Running != 2 || detail.Desired != 3 || detail.Usage == nil || *detail.Usage != want {
		t.Fatalf("detail summary: %+v usage=%+v", detail, detail.Usage)
	}
	for _, task := range detail.Tasks {
		switch task.ID {
		case "t-web-2":
			if task.Usage == nil || task.Usage.RxBps != 3000 || task.Node != "host-w1" || task.NodeID != "w1" {
				t.Fatalf("task t-web-2: %+v usage=%+v", task, task.Usage)
			}
		case "t-web-old":
			if task.Usage != nil {
				t.Fatalf("dead task must have no usage: %+v", task.Usage)
			}
		}
	}

	// Стек = сумма сервисов: 0.35 + 1.5 ядра, 150 + 800 МБ, задач 3.
	row, ok := findStack(t, client, srv.URL, "app")
	if !ok || row.Usage == nil || row.Usage.CPUCores != 1.85 || row.Usage.MemUsed != 950*mb || row.Usage.Tasks != 3 {
		t.Fatalf("stack usage: %+v usage=%+v", row, row.Usage)
	}
}

// TestServiceDetailSpec: спецификация отдаёт сети по именам, тома, порты,
// лимиты в ядрах и байтах, digest отдельно от тега (3.4.4).
func TestServiceDetailSpec(t *testing.T) {
	fd := clusterWithServices()
	fd.networks = []network.Summary{{ID: "net-id-1", Name: "app_backend"}}
	maxAttempts := uint64(3)
	svc := &fd.services[0]
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Image:   "nginx:1.27@sha256:abc123",
		Env:     []string{"MODE=prod"},
		Mounts:  []mount.Mount{{Type: mount.TypeVolume, Source: "app_data", Target: "/data", ReadOnly: true}},
		Secrets: []*swarm.SecretReference{{SecretName: "app_token"}},
	}
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "net-id-1"}}
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{
		Limits: &swarm.Limit{NanoCPUs: 500_000_000, MemoryBytes: 512 << 20},
	}
	svc.Spec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{
		Condition: swarm.RestartPolicyConditionOnFailure, MaxAttempts: &maxAttempts,
	}
	svc.Endpoint.Ports = []swarm.PortConfig{{PublishedPort: 8080, TargetPort: 80, Protocol: "tcp"}}

	srv, _ := newTestServer(t, fd)
	client, _ := loginClient(t, srv)

	var detail struct {
		Spec struct {
			Image    string   `json:"image"`
			Digest   string   `json:"digest"`
			Networks []string `json:"networks"`
			Mounts   []string `json:"mounts"`
			Ports    []string `json:"ports"`
			Secrets  []string `json:"secrets"`
			Limits   *struct {
				CPUs   float64 `json:"cpus"`
				Memory int64   `json:"memory"`
			} `json:"limits"`
			Restart *struct {
				Condition   string `json:"condition"`
				MaxAttempts uint64 `json:"max_attempts"`
			} `json:"restart"`
		} `json:"spec"`
	}
	getJSON(t, client, srv.URL+"/api/v1/services/s1", &detail)
	sp := detail.Spec
	if sp.Image != "nginx:1.27" || sp.Digest != "sha256:abc123" {
		t.Fatalf("image/digest: %q / %q", sp.Image, sp.Digest)
	}
	if len(sp.Networks) != 1 || sp.Networks[0] != "app_backend" {
		t.Fatalf("network must be resolved to its name: %+v", sp.Networks)
	}
	if len(sp.Mounts) != 1 || sp.Mounts[0] != "app_data:/data (volume, ro)" {
		t.Fatalf("mounts: %+v", sp.Mounts)
	}
	if len(sp.Ports) != 1 || sp.Ports[0] != "8080:80/tcp" {
		t.Fatalf("ports: %+v", sp.Ports)
	}
	if len(sp.Secrets) != 1 || sp.Secrets[0] != "app_token" {
		t.Fatalf("secrets: %+v", sp.Secrets)
	}
	// 500 000 000 наноядер = 0.5 ядра.
	if sp.Limits == nil || sp.Limits.CPUs != 0.5 || sp.Limits.Memory != 512<<20 {
		t.Fatalf("limits: %+v", sp.Limits)
	}
	if sp.Restart == nil || sp.Restart.Condition != "on-failure" || sp.Restart.MaxAttempts != 3 {
		t.Fatalf("restart: %+v", sp.Restart)
	}
}

// TestStackRemoveToleratesGoneNetwork: сеть видна в списке, но Docker уже
// отвечает «не найдено» (найдено живой проверкой 25.11.2026: overlay-сети
// Swarm убирает с задержкой). Это не сбой — стек должен удалиться чисто.
func TestStackRemoveToleratesGoneNetwork(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true)},
	}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	deployFull(t, fd, client, srv.URL, csrf)
	fd.goneNetworks = map[string]bool{"net-full_net1": true}

	resp := del(t, client, srv.URL+"/api/v1/stacks/full", map[string]string{"X-CSRF-Token": csrf})
	defer resp.Body.Close()
	var res opResult
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Failed) != 0 {
		t.Fatalf("not-found on removal must not be a failure: %+v", res.Failed)
	}
	if _, ok := findStack(t, client, srv.URL, "full"); ok {
		t.Fatal("stack source must be removed")
	}
}
