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
	s.Put("n1", []Task{{Task: "t1", CPUCores: 0.5}, {Task: "t2", CPUCores: 0.1}}, TasksOK)
	s.Put("n1", []Task{{Task: "t1", CPUCores: 0.7}}, TasksOK)

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
	s.Put("n1", []Task{{Task: "t1"}}, TasksOK)
	s.Put("n2", []Task{{Task: "t2"}}, TasksOK)
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

	s.Put("n1", []Task{{Task: "t1"}}, TasksOK)
	clock = clock.Add(90 * time.Second) // ровно на границе — ещё свежий
	if got := s.ByTask(); len(got) != 1 {
		t.Fatalf("at ttl boundary: want 1, got %d", len(got))
	}
	clock = clock.Add(time.Second) // 91 с — протух
	if got := s.ByTask(); len(got) != 0 {
		t.Fatalf("after ttl: want 0, got %d", len(got))
	}

	// Запись от другой ноды убирает протухший батч из памяти.
	s.Put("n2", nil, TasksOK)
	if _, ok := s.nodes["n1"]; ok {
		t.Fatal("expired node batch must be evicted on Put")
	}
}

// newClockedStats — хранилище с управляемыми часами.
func newClockedStats() (*TaskStats, *time.Time) {
	clock := time.Unix(1_700_000_000, 0)
	s := NewTaskStats(90 * time.Second)
	s.now = func() time.Time { return clock }
	return s, &clock
}

// TestTaskStatsFailedBatchKeepsData: агент не смог опросить Docker — прошлые
// цифры остаются. Раньше один такой батч превращал все сервисы ноды в
// прочерки, и таблица «прыгала».
func TestTaskStatsFailedBatchKeepsData(t *testing.T) {
	s, clock := newClockedStats()
	s.Put("n1", []Task{{Task: "t1", CPUCores: 0.4}}, TasksOK)
	*clock = clock.Add(3 * time.Second)
	s.Put("n1", nil, TasksFailed)

	if got := s.ByTask(); got["t1"].CPUCores != 0.4 {
		t.Fatalf("после сбойного батча цифры должны остаться, получено %+v", got)
	}
	if st := s.Agents()["n1"]; st != AgentOK {
		t.Fatalf("state = %q, ожидалось ok: свежие метрики ещё есть", st)
	}

	// Сбой длится дольше TTL — цифры уходят, причина — сбой агента.
	*clock = clock.Add(91 * time.Second)
	s.Put("n1", nil, TasksFailed)
	if got := s.ByTask(); len(got) != 0 {
		t.Fatalf("после TTL цифр быть не должно, получено %+v", got)
	}
	if st := s.Agents()["n1"]; st != AgentFailed {
		t.Fatalf("state = %q, ожидалось failed", st)
	}
}

// TestTaskStatsOldAgentBatchKeepsData: на ноде работают агенты двух версий
// (обновление не завершилось). Батч старого агента — без метрик задач —
// не затирает цифры нового.
func TestTaskStatsOldAgentBatchKeepsData(t *testing.T) {
	s, _ := newClockedStats()
	s.Put("n1", []Task{{Task: "t1", CPUCores: 0.2}}, TasksOK)
	s.Put("n1", nil, "") // старый агент: ни задач, ни признака

	if got := s.ByTask(); len(got) != 1 {
		t.Fatalf("батч старого агента стёр цифры: %+v", got)
	}
	if st := s.Agents()["n1"]; st != AgentOK {
		t.Fatalf("state = %q, ожидалось ok", st)
	}
}

// TestTaskStatsEmptyOKClears: агент сообщил «опросил, задач нет» — прошлые
// цифры убираются сразу (задачи уехали с ноды).
func TestTaskStatsEmptyOKClears(t *testing.T) {
	s, _ := newClockedStats()
	s.Put("n1", []Task{{Task: "t1"}}, TasksOK)
	s.Put("n1", []Task{}, TasksOK)
	if got := s.ByTask(); len(got) != 0 {
		t.Fatalf("после пустого полного батча задач быть не должно: %+v", got)
	}
}

// TestTaskStatsAgentStates: причина прочерка — по виду последних батчей.
func TestTaskStatsAgentStates(t *testing.T) {
	s, clock := newClockedStats()
	s.Put("old", nil, "")                     // старый агент
	s.Put("legacy", []Task{{Task: "t9"}}, "") // агент 2.2: задачи есть, признака нет
	s.Put("broken", nil, TasksFailed)         // Docker не ответил
	s.Put("fine", []Task{{Task: "t1"}}, TasksOK)

	got := s.Agents()
	want := map[string]AgentState{
		"old": AgentOutdated, "legacy": AgentOK, "broken": AgentFailed, "fine": AgentOK,
	}
	for node, st := range want {
		if got[node] != st {
			t.Errorf("нода %s: state = %q, ожидалось %q", node, got[node], st)
		}
	}
	if _, ok := got["never"]; ok {
		t.Error("нода без батчей не должна быть в карте (она «молчит»)")
	}

	// Все замолчали: через TTL карта пуста.
	*clock = clock.Add(91 * time.Second)
	if got := s.Agents(); len(got) != 0 {
		t.Fatalf("после TTL состояний быть не должно: %+v", got)
	}
}
