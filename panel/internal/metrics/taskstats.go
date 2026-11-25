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

// taskBatch — метрики задач одной ноды и момент их приёма.
type taskBatch struct {
	at    time.Time // время приёма по часам панели (часам агента не доверяем)
	tasks []Task
}

// TaskStats — потокобезопасное хранилище «последнее по каждой ноде».
// Пишет ingest-handler, читают REST-обработчики сервисов и стеков.
type TaskStats struct {
	mu    sync.RWMutex
	nodes map[string]taskBatch // node_id → последний батч
	ttl   time.Duration
	now   func() time.Time // подменяется в тестах
}

// NewTaskStats создаёт хранилище; ttl — срок свежести батча ноды.
func NewTaskStats(ttl time.Duration) *TaskStats {
	return &TaskStats{
		nodes: make(map[string]taskBatch),
		ttl:   ttl,
		now:   time.Now,
	}
}

// Put заменяет метрики задач ноды свежим батчем. Пустой список — тоже
// данные: на ноде сейчас нет задач (или агент старой версии).
// Заодно выбрасывает протухшие батчи других нод, чтобы удалённые из
// кластера ноды не копились в памяти.
func (s *TaskStats) Put(nodeID string, tasks []Task) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[nodeID] = taskBatch{at: now, tasks: tasks}
	for id, b := range s.nodes {
		if now.Sub(b.at) > s.ttl {
			delete(s.nodes, id)
		}
	}
}

// ByTask возвращает свежие метрики всех задач кластера: ID задачи → метрики.
// Задачи с нод, которые молчат дольше ttl, в ответ не попадают.
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
