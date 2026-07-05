// Кольцевой буфер live-метрик (разд. 2.2 ТЗ):
// последние 15 минут при шаге 3с держатся в памяти, в БД не пишутся.
package metrics

import (
	"sync"
	"time"
)

// ClusterBuffer — live-метрики всех нод кластера.
// Потокобезопасен: пишет ingest-handler, читают REST и WebSocket-hub.
type ClusterBuffer struct {
	mu    sync.RWMutex
	nodes map[string]*ring // node_id → кольцо снапшотов
	size  int              // ёмкость кольца = окно / шаг
}

// ring — кольцо снапшотов одной ноды.
type ring struct {
	buf  []Snapshot
	head int // индекс следующей записи
	n    int // фактически занято
}

// NewClusterBuffer рассчитывает ёмкость кольца из окна и шага сбора.
// Пример: 15 мин / 3 с = 300 точек на ноду.
func NewClusterBuffer(window, step time.Duration) *ClusterBuffer {
	size := int(window / step)
	if size < 1 {
		size = 1
	}
	return &ClusterBuffer{
		nodes: make(map[string]*ring),
		size:  size,
	}
}

// Put добавляет снапшот ноды, вытесняя самую старую точку при переполнении.
func (b *ClusterBuffer) Put(s Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.nodes[s.NodeID]
	if !ok {
		r = &ring{buf: make([]Snapshot, b.size)}
		b.nodes[s.NodeID] = r
	}
	r.buf[r.head] = s
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// Latest возвращает последний снапшот ноды; ok=false — метрик ещё не было.
func (b *ClusterBuffer) Latest(nodeID string) (Snapshot, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := b.nodes[nodeID]
	if !ok || r.n == 0 {
		return Snapshot{}, false
	}
	idx := (r.head - 1 + len(r.buf)) % len(r.buf)
	return r.buf[idx], true
}

// Latests возвращает последние снапшоты всех известных нод
// (для периодических проверок алертов, FR-12).
func (b *ClusterBuffer) Latests() map[string]Snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]Snapshot, len(b.nodes))
	for id, r := range b.nodes {
		if r.n == 0 {
			continue
		}
		idx := (r.head - 1 + len(r.buf)) % len(r.buf)
		out[id] = r.buf[idx]
	}
	return out
}

// Window возвращает снапшоты ноды от старых к новым (для графиков live-окна).
func (b *ClusterBuffer) Window(nodeID string) []Snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := b.nodes[nodeID]
	if !ok || r.n == 0 {
		return nil
	}
	out := make([]Snapshot, 0, r.n)
	start := (r.head - r.n + len(r.buf)) % len(r.buf)
	for i := 0; i < r.n; i++ {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	return out
}
