// Тесты причин «нет данных» у потребления сервисов (ТЗ 3.4.7).
//
// Прочерк в таблице сам по себе ничего не объясняет. Панель обязана
// сказать, с какой ноды нет метрик и почему, — и не терять цифры из-за
// одного сбойного батча (иначе строки таблицы «прыгают»).
package api

import (
	"net/http"
	"testing"

	"github.com/xakus/DSMS/panel/internal/metrics"
)

// noDataJSON — пара «нода — причина» из ответа API.
type noDataJSON struct {
	Node   string `json:"node"`
	Reason string `json:"reason"`
}

// svcNoData — поля сервиса, нужные тестам причин.
type svcNoData struct {
	ID     string       `json:"id"`
	Usage  *usageJSON   `json:"usage"`
	NoData []noDataJSON `json:"no_data"`
}

// servicesByID читает список сервисов и раскладывает по ID.
func servicesByID(t *testing.T, client *http.Client, base string) map[string]svcNoData {
	t.Helper()
	var list []svcNoData
	getJSON(t, client, base+"/api/v1/services", &list)
	out := make(map[string]svcNoData, len(list))
	for _, s := range list {
		out[s.ID] = s
	}
	return out
}

// sameNoData сравнивает список причин с ожидаемым (порядок важен: по нодам).
func sameNoData(got, want []noDataJSON) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestServiceNoDataReasons: причина прочерка зависит от того, что прислал
// агент ноды: ничего, батч старой версии, признак сбоя или батч без задачи.
func TestServiceNoDataReasons(t *testing.T) {
	fd := usageCluster() // web (s1): задачи на m1 и w1; db (s2): задача на m1
	srv, _ := newTestServer(t, fd)
	client, _ := loginClient(t, srv)

	// Агенты ещё не отчитались: обе ноды «молчат».
	got := servicesByID(t, client, srv.URL)
	want := []noDataJSON{{"host-m1", noDataSilent}, {"host-w1", noDataSilent}}
	if !sameNoData(got["s1"].NoData, want) {
		t.Fatalf("без батчей: want %+v, got %+v", want, got["s1"].NoData)
	}

	// w1 — агент старой версии: батч есть, метрик задач и признака нет.
	ingest(t, srv.URL, metrics.Snapshot{NodeID: "w1", TS: 1})
	got = servicesByID(t, client, srv.URL)
	want = []noDataJSON{{"host-m1", noDataSilent}, {"host-w1", noDataOutdated}}
	if !sameNoData(got["s1"].NoData, want) {
		t.Fatalf("старый агент: want %+v, got %+v", want, got["s1"].NoData)
	}

	// m1 — агент работает, но задачу db не прислал; w1 — Docker не ответил.
	ingest(t, srv.URL, metrics.Snapshot{NodeID: "m1", TS: 2, TasksState: metrics.TasksOK,
		Tasks: []metrics.Task{{Task: "t-web-1", CPUCores: 0.2}}})
	ingest(t, srv.URL, metrics.Snapshot{NodeID: "w1", TS: 2, TasksState: metrics.TasksFailed})
	got = servicesByID(t, client, srv.URL)
	if want := []noDataJSON{{"host-w1", noDataFailed}}; !sameNoData(got["s1"].NoData, want) {
		t.Fatalf("сбой агента: want %+v, got %+v", want, got["s1"].NoData)
	}
	if want := []noDataJSON{{"host-m1", noDataUnseen}}; !sameNoData(got["s2"].NoData, want) {
		t.Fatalf("задача не прислана: want %+v, got %+v", want, got["s2"].NoData)
	}
	if got["s1"].Usage == nil || got["s1"].Usage.Tasks != 1 {
		t.Fatalf("web: метрики одной задачи должны быть, got %+v", got["s1"].Usage)
	}

	// Та же причина — у задачи на странице сервиса.
	var detail struct {
		NoData []noDataJSON `json:"no_data"`
		Tasks  []struct {
			ID     string `json:"id"`
			NoData string `json:"no_data"`
		} `json:"tasks"`
	}
	getJSON(t, client, srv.URL+"/api/v1/services/s1", &detail)
	if want := []noDataJSON{{"host-w1", noDataFailed}}; !sameNoData(detail.NoData, want) {
		t.Fatalf("страница сервиса: want %+v, got %+v", want, detail.NoData)
	}
	for _, task := range detail.Tasks {
		wantReason := map[string]string{"t-web-1": "", "t-web-2": noDataFailed, "t-web-old": ""}[task.ID]
		if task.NoData != wantReason {
			t.Errorf("задача %s: причина %q, ожидалась %q", task.ID, task.NoData, wantReason)
		}
	}
}

// TestFailedBatchDoesNotWipeUsage: один батч «Docker не ответил» не должен
// превращать цифры сервисов ноды в прочерки — это и было мигание таблицы.
func TestFailedBatchDoesNotWipeUsage(t *testing.T) {
	fd := usageCluster()
	srv, _ := newTestServer(t, fd)
	client, _ := loginClient(t, srv)

	ingest(t, srv.URL, metrics.Snapshot{NodeID: "m1", TS: 1, TasksState: metrics.TasksOK,
		Tasks: []metrics.Task{{Task: "t-db-1", CPUCores: 1.5}}})
	// Следующий тик: сбой опроса Docker, затем батч старого агента с той же ноды.
	ingest(t, srv.URL, metrics.Snapshot{NodeID: "m1", TS: 2, TasksState: metrics.TasksFailed})
	ingest(t, srv.URL, metrics.Snapshot{NodeID: "m1", TS: 3})

	db := servicesByID(t, client, srv.URL)["s2"]
	if db.Usage == nil || db.Usage.CPUCores != 1.5 {
		t.Fatalf("цифры должны остаться после сбойных батчей, got %+v", db.Usage)
	}
	if len(db.NoData) != 0 {
		t.Fatalf("причин «нет данных» быть не должно, got %+v", db.NoData)
	}
}
