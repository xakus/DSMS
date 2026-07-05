// Тесты управления нодами (FR-03): guards 3.3.5, join-tokens, labels.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// putJSON — PUT с JSON-телом (для labels).
func putJSON(t *testing.T, client *http.Client, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	return resp
}

// del — DELETE-запрос с заголовками.
func del(t *testing.T, client *http.Client, url string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	return resp
}

// TestDemoteLastManager: запрет разжалования последнего manager'а (3.3.5).
func TestDemoteLastManager(t *testing.T) {
	fd := &fakeDocker{nodes: []swarm.Node{
		mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		mkNode("w1", swarm.NodeRoleWorker, swarm.NodeAvailabilityActive, swarm.NodeStateReady, false),
	}}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	resp := postJSON(t, client, srv.URL+"/api/v1/nodes/m1/role",
		map[string]string{"role": "worker"}, hdr)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("demote last manager: want 409, got %d", resp.StatusCode)
	}

	// А promote worker'а — можно; после него demote m1 тоже можно.
	resp = postJSON(t, client, srv.URL+"/api/v1/nodes/w1/role",
		map[string]string{"role": "manager"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promote: want 200, got %d", resp.StatusCode)
	}
	resp = postJSON(t, client, srv.URL+"/api/v1/nodes/m1/role",
		map[string]string{"role": "worker"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("demote with 2 managers: want 200, got %d", resp.StatusCode)
	}
}

// TestQuorumWarning: при чётном числе manager'ов приходит предупреждение.
func TestQuorumWarning(t *testing.T) {
	fd := &fakeDocker{nodes: []swarm.Node{
		mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		mkNode("w1", swarm.NodeRoleWorker, swarm.NodeAvailabilityActive, swarm.NodeStateReady, false),
	}}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)

	// promote w1 → 2 manager'а (чётно) → warning в ответе
	resp := postJSON(t, client, srv.URL+"/api/v1/nodes/w1/role",
		map[string]string{"role": "manager"}, map[string]string{"X-CSRF-Token": csrf})
	var body struct {
		Warning string `json:"warning"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Warning == "" {
		t.Fatal("expected quorum warning for even number of managers")
	}
}

// TestDrainLastActiveManager: запрет drain последнего активного manager'а.
func TestDrainLastActiveManager(t *testing.T) {
	fd := &fakeDocker{nodes: []swarm.Node{
		mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		mkNode("m2", swarm.NodeRoleManager, swarm.NodeAvailabilityDrain, swarm.NodeStateReady, false),
	}}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// m2 уже в drain → m1 — последний активный manager
	resp := postJSON(t, client, srv.URL+"/api/v1/nodes/m1/availability",
		map[string]string{"availability": "drain"}, hdr)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("drain last active manager: want 409, got %d", resp.StatusCode)
	}

	// вернуть m2 в active → теперь drain m1 разрешён
	resp = postJSON(t, client, srv.URL+"/api/v1/nodes/m2/availability",
		map[string]string{"availability": "active"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activate m2: want 200, got %d", resp.StatusCode)
	}
	resp = postJSON(t, client, srv.URL+"/api/v1/nodes/m1/availability",
		map[string]string{"availability": "drain"}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain with backup manager: want 200, got %d", resp.StatusCode)
	}
}

// TestNodeRemoveGuards: удаление только down-нод, force обходит (3.3.3).
func TestNodeRemoveGuards(t *testing.T) {
	fd := &fakeDocker{nodes: []swarm.Node{
		mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		mkNode("w1", swarm.NodeRoleWorker, swarm.NodeAvailabilityActive, swarm.NodeStateReady, false),
		mkNode("w2", swarm.NodeRoleWorker, swarm.NodeAvailabilityActive, swarm.NodeStateDown, false),
	}}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	// ready-нода без force → 409
	resp := del(t, client, srv.URL+"/api/v1/nodes/w1", hdr)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("remove ready node: want 409, got %d", resp.StatusCode)
	}
	// down-нода → ok
	resp = del(t, client, srv.URL+"/api/v1/nodes/w2", hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove down node: want 200, got %d", resp.StatusCode)
	}
	// ready-нода с force → ok
	resp = del(t, client, srv.URL+"/api/v1/nodes/w1?force=true", hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force remove: want 200, got %d", resp.StatusCode)
	}
	if len(fd.removed) != 2 {
		t.Fatalf("expected 2 removals, got %v", fd.removed)
	}
}

// TestJoinTokensAndLabels: выдача токенов, ротация, замена labels.
func TestJoinTokensAndLabels(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{
			mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		},
	}
	fd.swarm.JoinTokens = swarm.JoinTokens{Worker: "SWMTKN-w", Manager: "SWMTKN-m"}
	srv, _ := newTestServer(t, fd)
	client, csrf := loginClient(t, srv)
	hdr := map[string]string{"X-CSRF-Token": csrf}

	resp, _ := client.Get(srv.URL + "/api/v1/swarm/join-tokens")
	var tokens struct {
		Worker      string `json:"worker"`
		Manager     string `json:"manager"`
		ManagerAddr string `json:"manager_addr"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tokens)
	if tokens.Worker != "SWMTKN-w" || tokens.ManagerAddr != "10.0.0.1:2377" {
		t.Fatalf("join tokens: %+v", tokens)
	}

	resp = postJSON(t, client, srv.URL+"/api/v1/swarm/join-tokens/rotate", nil, hdr)
	if resp.StatusCode != http.StatusOK || !fd.rotated {
		t.Fatalf("rotate: status %d rotated=%v", resp.StatusCode, fd.rotated)
	}

	// labels: полная замена
	resp = putJSON(t, client, srv.URL+"/api/v1/nodes/m1/labels",
		map[string]any{"labels": map[string]string{"zone": "eu"}}, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("labels: want 200, got %d", resp.StatusCode)
	}
	if fd.nodes[0].Spec.Labels["zone"] != "eu" {
		t.Fatalf("labels not applied: %v", fd.nodes[0].Spec.Labels)
	}
}
