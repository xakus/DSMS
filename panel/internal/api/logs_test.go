// Тест REST-истории логов (FR-05): точный tail, сортировка по времени,
// курсор has_more/oldest для подгрузки старого по скроллу.
package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// TestServiceLogsHistory: сервер отдаёт РОВНО tail строк (последние по времени)
// и сигналит has_more, когда истории больше страницы.
func TestServiceLogsHistory(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{
			mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		},
		services: []swarm.Service{mkService("s1", "app_web", "nginx:1.27", 3, "app")},
		// Три строки в формате docker --details --timestamps, вразнобой по времени.
		logLines: []string{
			"2026-07-05T12:00:03Z com.docker.swarm.node.id=n1,com.docker.swarm.task.name=app_web.2.aaa line-C",
			"2026-07-05T12:00:01Z com.docker.swarm.node.id=n1,com.docker.swarm.task.name=app_web.1.bbb line-A",
			"2026-07-05T12:00:02Z com.docker.swarm.node.id=n2,com.docker.swarm.task.name=app_web.3.ccc line-B",
		},
	}
	srv, _ := newTestServer(t, fd)
	client, _ := loginClient(t, srv)

	resp, err := client.Get(srv.URL + "/api/v1/services/s1/logs?tail=2")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("logs request: err=%v status=%v", err, resp.StatusCode)
	}
	var page struct {
		Lines []struct {
			Line     string `json:"line"`
			TaskName string `json:"task_name"`
			TS       string `json:"ts"`
		} `json:"lines"`
		Oldest  string `json:"oldest"`
		HasMore bool   `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Ровно tail=2 строки, и это ДВЕ САМЫЕ СВЕЖИЕ (B@12:00:02, C@12:00:03).
	if len(page.Lines) != 2 {
		t.Fatalf("want exactly 2 lines, got %d: %+v", len(page.Lines), page.Lines)
	}
	if page.Lines[0].Line != "line-B" || page.Lines[1].Line != "line-C" {
		t.Fatalf("wrong order/trim: %q, %q", page.Lines[0].Line, page.Lines[1].Line)
	}
	// task_name распарсен (для номера реплики в UI).
	if page.Lines[1].TaskName != "app_web.2.aaa" {
		t.Fatalf("task_name not parsed: %q", page.Lines[1].TaskName)
	}
	// Есть ещё старое (line-A отсеклась) → курсор для догрузки.
	if !page.HasMore {
		t.Fatalf("has_more: want true")
	}
	if page.Oldest != page.Lines[0].TS {
		t.Fatalf("oldest cursor mismatch: %q vs %q", page.Oldest, page.Lines[0].TS)
	}
}

// TestServiceLogsFallback: если большой tail отдаёт пусто (баг docker),
// хендлер откатывается на безопасный tail и всё же возвращает строки.
func TestServiceLogsFallback(t *testing.T) {
	fd := &fakeDocker{
		nodes: []swarm.Node{
			mkNode("m1", swarm.NodeRoleManager, swarm.NodeAvailabilityActive, swarm.NodeStateReady, true),
		},
		services: []swarm.Service{mkService("s1", "app_web", "nginx:1.27", 3, "app")},
		logLines: []string{
			"2026-07-05T12:00:01Z com.docker.swarm.node.id=n1 line-A",
			"2026-07-05T12:00:02Z com.docker.swarm.node.id=n1 line-B",
		},
		emptyAboveTail: 100, // всё, что больше 100, вернётся пустым
	}
	srv, _ := newTestServer(t, fd)
	client, _ := loginClient(t, srv)

	// tail=500 → docker (fake) отдаёт пусто → откат на 100 → строки есть.
	resp, err := client.Get(srv.URL + "/api/v1/services/s1/logs?tail=500")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("logs request: err=%v status=%v", err, resp.StatusCode)
	}
	var page struct {
		Lines []struct {
			Line string `json:"line"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Lines) != 2 {
		t.Fatalf("fallback: want 2 lines from retry, got %d", len(page.Lines))
	}
}
