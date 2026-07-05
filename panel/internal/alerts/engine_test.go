// Тесты движка алертов (FR-12): open/resolve, отсутствие дублей.
package alerts

import (
	"sync"
	"testing"
	"time"

	"github.com/xakus/DSMS/panel/internal/metrics"
)

// fakeStore — журнал алертов в памяти.
type fakeStore struct {
	mu       sync.Mutex
	inserted []string
	resolved []int64
	nextID   int64
}

func (f *fakeStore) InsertAlert(rule, severity, objectType, objectID, message string, openedAt int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.inserted = append(f.inserted, rule+":"+objectID)
	return f.nextID, nil
}

func (f *fakeStore) ResolveAlert(id, resolvedAt int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, id)
	return nil
}

// fakeNotifier — собирает доставленные алерты.
type fakeNotifier struct {
	mu    sync.Mutex
	sent  []Alert
}

func (f *fakeNotifier) Notify(a Alert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, a)
}

// snap — синтетический батч с заданным использованием диска.
func snap(nodeID string, usedPct float64) metrics.Snapshot {
	return metrics.Snapshot{
		NodeID:   nodeID,
		Hostname: "host-" + nodeID,
		TS:       time.Now().Unix(),
		Disk: []metrics.Disk{
			{Mount: "/", Total: 1000, Used: uint64(usedPct * 10)},
		},
	}
}

// TestDiskAlertLifecycle: 95% открывает алерт, повтор не дублирует,
// 50% резолвит (3.12.1, 3.12.3).
func TestDiskAlertLifecycle(t *testing.T) {
	st := &fakeStore{}
	nt := &fakeNotifier{}
	buf := metrics.NewClusterBuffer(time.Minute, time.Second)
	e := NewEngine(st, nt, buf, nil)

	// диск 95% → алерт открыт
	e.Evaluate(snap("n1", 95))
	if len(e.Active()) != 1 {
		t.Fatalf("want 1 active alert, got %d", len(e.Active()))
	}
	if len(nt.sent) != 1 || nt.sent[0].Rule != "disk_high" || nt.sent[0].State != "active" {
		t.Fatalf("notify: %+v", nt.sent)
	}

	// повторный батч с той же проблемой — БЕЗ дубликата (критерий этапа)
	e.Evaluate(snap("n1", 96))
	if len(e.Active()) != 1 || len(st.inserted) != 1 {
		t.Fatalf("duplicate alert: active=%d inserted=%d", len(e.Active()), len(st.inserted))
	}

	// диск вернулся к 50% → resolved
	e.Evaluate(snap("n1", 50))
	if len(e.Active()) != 0 {
		t.Fatalf("alert not resolved: %+v", e.Active())
	}
	if len(st.resolved) != 1 {
		t.Fatalf("resolve not persisted: %+v", st.resolved)
	}
	last := nt.sent[len(nt.sent)-1]
	if last.State != "resolved" {
		t.Fatalf("resolved not notified: %+v", last)
	}
}

// TestSilentAgent: старая точка в буфере → алерт; свежий батч резолвит.
func TestSilentAgent(t *testing.T) {
	st := &fakeStore{}
	nt := &fakeNotifier{}
	buf := metrics.NewClusterBuffer(time.Minute, time.Second)
	e := NewEngine(st, nt, buf, nil)

	// точка от агента 2 минуты назад
	old := snap("n1", 10)
	old.TS = time.Now().Add(-2 * time.Minute).Unix()
	buf.Put(old)

	e.checkSilentAgents()
	if len(e.Active()) != 1 || e.Active()[0].Rule != "agent_silent" {
		t.Fatalf("want agent_silent alert, got %+v", e.Active())
	}

	// свежий батч резолвит
	e.Evaluate(snap("n1", 10))
	if len(e.Active()) != 0 {
		t.Fatalf("agent_silent not resolved: %+v", e.Active())
	}
}

// TestEvent: одноразовое событие сразу закрыто и доставлено.
func TestEvent(t *testing.T) {
	st := &fakeStore{}
	nt := &fakeNotifier{}
	e := NewEngine(st, nt, metrics.NewClusterBuffer(time.Minute, time.Second), nil)

	e.Event("task_failed", "warning", "task", "web.1", "task web.1 exited with code 137")
	if len(e.Active()) != 0 {
		t.Fatal("event must not stay active")
	}
	if len(st.inserted) != 1 || len(st.resolved) != 1 {
		t.Fatalf("event persistence: inserted=%d resolved=%d", len(st.inserted), len(st.resolved))
	}
	if len(nt.sent) != 1 || nt.sent[0].State != "event" {
		t.Fatalf("event notify: %+v", nt.sent)
	}
}
