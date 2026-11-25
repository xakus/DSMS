// Тесты хранилища метрик задач (FR-04 3.4.7).
package metrics

import (
	"testing"
	"time"
)

// TestTaskStatsLatestWins: новый батч ноды полностью заменяет старый —
// задача, уехавшая с ноды, не должна «висеть» со старыми цифрами.
func TestTaskStatsLatestWins(t *testing.T) {
	s := NewTaskStats(TaskStatsTTL)
	s.Put("n1", []Task{{Task: "t1", CPUCores: 0.5}, {Task: "t2", CPUCores: 0.1}})
	s.Put("n1", []Task{{Task: "t1", CPUCores: 0.7}})

	got := s.ByTask()
	if len(got) != 1 {
		t.Fatalf("want 1 task, got %d: %+v", len(got), got)
	}
	if got["t1"].CPUCores != 0.7 {
		t.Fatalf("want latest value 0.7, got %v", got["t1"].CPUCores)
	}
}

// TestTaskStatsMergesNodes: задачи разных нод складываются в одну карту.
func TestTaskStatsMergesNodes(t *testing.T) {
	s := NewTaskStats(TaskStatsTTL)
	s.Put("n1", []Task{{Task: "t1"}})
	s.Put("n2", []Task{{Task: "t2"}})
	if got := s.ByTask(); len(got) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(got))
	}
}

// TestTaskStatsExpires: батч старше TTL не отдаётся (агент замолчал) и
// вычищается при следующей записи.
func TestTaskStatsExpires(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	s := NewTaskStats(90 * time.Second)
	s.now = func() time.Time { return clock }

	s.Put("n1", []Task{{Task: "t1"}})
	clock = clock.Add(90 * time.Second) // ровно на границе — ещё свежий
	if got := s.ByTask(); len(got) != 1 {
		t.Fatalf("at ttl boundary: want 1, got %d", len(got))
	}
	clock = clock.Add(time.Second) // 91 с — протух
	if got := s.ByTask(); len(got) != 0 {
		t.Fatalf("after ttl: want 0, got %d", len(got))
	}

	// Запись от другой ноды убирает протухший батч из памяти.
	s.Put("n2", nil)
	if _, ok := s.nodes["n1"]; ok {
		t.Fatal("expired node batch must be evicted on Put")
	}
}
