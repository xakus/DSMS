// Последнее потребление задач Swarm (FR-04 3.4.7).
//
// Агент каждой ноды присылает метрики всех своих задач в каждом батче.
// Истории здесь нет: хранится только свежий батч по каждой ноде, в память.
// В кольцевой буфер и БД эти данные не попадают (NFR-2a).
package metrics

import (
	"sync"
	"time"
)

// TaskStatsTTL — сколько батч ноды считается свежим.
// Агент шлёт метрики раз в 1–30 с (настройка metrics.interval_sec); 90 с —
// три пропущенных батча при самом редком интервале. Старше — нода молчит,
// и показывать её цифры как текущие нельзя.
const TaskStatsTTL = 90 * time.Second

// Значения Snapshot.TasksState — как агент оценил сбор метрик задач.
const (
	TasksOK     = "ok"     // Docker опрошен, список задач полный (может быть пустым)
	TasksFailed = "failed" // агент не смог опросить Docker
)

// AgentState — что известно о метриках задач с ноды. Это причина прочерка
// в UI: по ней пользователь понимает, что именно чинить (ТЗ 3.4.7).
type AgentState string

// Состояния агента ноды.
const (
	// AgentOK — метрики задач приходят.
	AgentOK AgentState = "ok"
	// AgentSilent — батчей нет дольше TTL: агент не запущен или не
	// достучался до панели.
	AgentSilent AgentState = "silent"
	// AgentOutdated — батчи идут, но метрик задач в них нет и признака
	// TasksState нет: агент старой версии.
	AgentOutdated AgentState = "outdated"
	// AgentFailed — агент сообщил, что не смог опросить Docker.
	AgentFailed AgentState = "failed"
)

// taskBatch — метрики задач одной ноды и момент их приёма.
type taskBatch struct {
	at    time.Time // время приёма по часам панели (часам агента не доверяем)
	tasks []Task
}

// agentSeen — когда от ноды в последний раз пришёл батч каждого вида.
// Виды хранятся раздельно: при незавершённом обновлении на ноде работают
// агенты разных версий, и их батчи чередуются.
type agentSeen struct {
	okAt     time.Time // с метриками задач
	failedAt time.Time // агент не смог опросить Docker
	bareAt   time.Time // без метрик задач и без признака (старый агент)
}

// last — самый поздний из моментов.
func (a agentSeen) last() time.Time {
	out := a.okAt
	for _, t := range []time.Time{a.failedAt, a.bareAt} {
		if t.After(out) {
			out = t
		}
	}
	return out
}

// TaskStats — потокобезопасное хранилище «последнее по каждой ноде».
// Пишет ingest-handler, читают REST-обработчики сервисов и стеков.
type TaskStats struct {
	mu    sync.RWMutex
	nodes map[string]taskBatch // node_id → последний батч с метриками задач
	seen  map[string]agentSeen // node_id → когда какие батчи приходили
	ttl   time.Duration
	now   func() time.Time // подменяется в тестах
}

// NewTaskStats создаёт хранилище; ttl — срок свежести батча ноды.
func NewTaskStats(ttl time.Duration) *TaskStats {
	return &TaskStats{
		nodes: make(map[string]taskBatch),
		seen:  make(map[string]agentSeen),
		ttl:   ttl,
		now:   time.Now,
	}
}

// Put принимает метрики задач из батча ноды. state — Snapshot.TasksState.
//
// Прошлые цифры заменяются, только если батч точно содержит полную картину:
// агент сообщил TasksOK (пустой список тогда значит «задач на ноде нет») или
// задачи в батче есть. Иначе цифры остаются до истечения TTL: один сбой
// опроса Docker или батч от старого агента не должен превращать все сервисы
// ноды в прочерки.
//
// Заодно выбрасывает протухшие записи других нод, чтобы удалённые из
// кластера ноды не копились в памяти.
func (s *TaskStats) Put(nodeID string, tasks []Task, state string) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := s.seen[nodeID]
	switch {
	case state == TasksOK || (state == "" && len(tasks) > 0):
		seen.okAt = now
		s.nodes[nodeID] = taskBatch{at: now, tasks: tasks}
	case state == TasksFailed:
		seen.failedAt = now
	default:
		seen.bareAt = now
	}
	s.seen[nodeID] = seen

	for id, b := range s.nodes {
		if now.Sub(b.at) > s.ttl {
			delete(s.nodes, id)
		}
	}
	for id, a := range s.seen {
		if now.Sub(a.last()) > s.ttl {
			delete(s.seen, id)
		}
	}
}

// ByTask возвращает свежие метрики всех задач кластера: ID задачи → метрики.
// Задачи с нод, от которых метрик нет дольше ttl, в ответ не попадают.
func (s *TaskStats) ByTask() map[string]Task {
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Task)
	for _, b := range s.nodes {
		if now.Sub(b.at) > s.ttl {
			continue
		}
		for _, t := range b.tasks {
			out[t.Task] = t
		}
	}
	return out
}

// Agents возвращает состояние агента каждой ноды, от которой за ttl
// приходил хоть один батч. Ноды нет в карте — агент молчит (AgentSilent).
//
// Свежие метрики важнее сбоя, сбой важнее «старой версии»: если хоть один
// агент ноды присылает метрики, нода в порядке.
func (s *TaskStats) Agents() map[string]AgentState {
	now := s.now()
	fresh := func(t time.Time) bool { return !t.IsZero() && now.Sub(t) <= s.ttl }
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]AgentState, len(s.seen))
	for id, a := range s.seen {
		switch {
		case fresh(a.okAt):
			out[id] = AgentOK
		case fresh(a.failedAt):
			out[id] = AgentFailed
		case fresh(a.bareAt):
			out[id] = AgentOutdated
		}
	}
	return out
}
