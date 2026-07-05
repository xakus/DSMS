// Package alerts — движок in-app алертов (FR-12).
//
// Правила v1 (3.12.1): диск > порога, агент молчит > 60с,
// desired > running дольше порога, task с non-zero exit.
// Состояния: active → resolved (3.12.3). Активные — в памяти,
// журнал — в SQLite. Доставка — через интерфейс Notifier (3.12.5):
// v1 — WebSocket topic "alerts"; v2 подключит Telegram/email тем же интерфейсом.
package alerts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/xakus/DSMS/panel/internal/metrics"
)

// Пороги и интервалы по умолчанию (настройка порогов — Settings, этап 7).
const (
	defaultDiskPct     = 90.0             // 3.12.1: диск > 90%
	agentSilentAfter   = 60 * time.Second // 3.12.1: агент молчит > 60с
	degradedAfter      = 60 * time.Second // 3.12.1: desired > running дольше N
	checkInterval      = 15 * time.Second // периодические проверки
)

// Alert — один алерт (активный или из истории).
type Alert struct {
	ID         int64  `json:"id"`
	Rule       string `json:"rule"`     // disk_high | agent_silent | service_degraded | task_failed
	Severity   string `json:"severity"` // warning | critical
	State      string `json:"state"`    // active | resolved | event
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	Message    string `json:"message"`
	OpenedAt   int64  `json:"opened_at"`
	ResolvedAt int64  `json:"resolved_at,omitempty"`
}

// Notifier — канал доставки (3.12.5): v1 — WS, v2 — Telegram/email.
type Notifier interface {
	Notify(a Alert)
}

// AlertStore — персистенция журнала (реализуется store.Store).
type AlertStore interface {
	InsertAlert(rule, severity, objectType, objectID, message string, openedAt int64) (int64, error)
	ResolveAlert(id, resolvedAt int64) error
}

// ClusterSource — данные для правила service_degraded.
type ClusterSource interface {
	Services(ctx context.Context) ([]swarm.Service, error)
	Tasks(ctx context.Context) ([]swarm.Task, error)
	Nodes(ctx context.Context) ([]swarm.Node, error)
}

// key — уникальный ключ активного алерта: правило + объект.
type key struct {
	rule string
	obj  string
}

// Engine — вычисление правил и учёт состояний.
type Engine struct {
	store    AlertStore
	notifier Notifier
	buffer   *metrics.ClusterBuffer
	cluster  ClusterSource

	diskPct float64

	mu       sync.Mutex
	active   map[key]*Alert
	degraded map[string]time.Time // serviceID → когда впервые замечен degraded
}

// NewEngine создаёт движок с дефолтными порогами.
func NewEngine(st AlertStore, n Notifier, buf *metrics.ClusterBuffer, cluster ClusterSource) *Engine {
	return &Engine{
		store:    st,
		notifier: n,
		buffer:   buf,
		cluster:  cluster,
		diskPct:  defaultDiskPct,
		active:   make(map[key]*Alert),
		degraded: make(map[string]time.Time),
	}
}

// SetDiskPct меняет порог disk_high на лету (Settings, этап 7).
func (e *Engine) SetDiskPct(pct float64) {
	e.mu.Lock()
	e.diskPct = pct
	e.mu.Unlock()
}

// diskThreshold возвращает текущий порог (потокобезопасно).
func (e *Engine) diskThreshold() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.diskPct
}

// Active возвращает копию активных алертов (для GET /alerts).
func (e *Engine) Active() []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Alert, 0, len(e.active))
	for _, a := range e.active {
		out = append(out, *a)
	}
	return out
}

// open заводит активный алерт, если его ещё нет (без дублей, критерий этапа).
func (e *Engine) open(k key, severity, objectType, message string) {
	e.mu.Lock()
	if _, exists := e.active[k]; exists {
		e.mu.Unlock()
		return
	}
	now := time.Now().Unix()
	a := &Alert{
		Rule: k.rule, Severity: severity, State: "active",
		ObjectType: objectType, ObjectID: k.obj, Message: message, OpenedAt: now,
	}
	if id, err := e.store.InsertAlert(k.rule, severity, objectType, k.obj, message, now); err == nil {
		a.ID = id
	}
	e.active[k] = a
	e.mu.Unlock()
	e.notifier.Notify(*a)
}

// resolve закрывает активный алерт, если он был (3.12.3).
func (e *Engine) resolve(k key) {
	e.mu.Lock()
	a, exists := e.active[k]
	if !exists {
		e.mu.Unlock()
		return
	}
	delete(e.active, k)
	a.State = "resolved"
	a.ResolvedAt = time.Now().Unix()
	e.mu.Unlock()
	_ = e.store.ResolveAlert(a.ID, a.ResolvedAt)
	e.notifier.Notify(*a)
}

// Event фиксирует одноразовый алерт-событие (task failed):
// без состояния active — сразу в журнал + доставка.
func (e *Engine) Event(rule, severity, objectType, objectID, message string) {
	now := time.Now().Unix()
	a := Alert{
		Rule: rule, Severity: severity, State: "event",
		ObjectType: objectType, ObjectID: objectID, Message: message, OpenedAt: now,
	}
	if id, err := e.store.InsertAlert(rule, severity, objectType, objectID, message, now); err == nil {
		a.ID = id
		_ = e.store.ResolveAlert(id, now) // событие закрыто по определению
	}
	e.notifier.Notify(a)
}

// Evaluate вычисляет метрические правила на свежем батче (3.12.1).
func (e *Engine) Evaluate(snap metrics.Snapshot) {
	threshold := e.diskThreshold()
	// disk_high: по каждой точке монтирования.
	for _, d := range snap.Disk {
		if d.Total == 0 {
			continue
		}
		pct := float64(d.Used) / float64(d.Total) * 100
		k := key{rule: "disk_high", obj: snap.NodeID + ":" + d.Mount}
		if pct > threshold {
			e.open(k, "critical", "node",
				fmt.Sprintf("disk %s on %s used %.1f%%", d.Mount, snap.Hostname, pct))
		} else {
			e.resolve(k)
		}
	}
	// Свежий батч резолвит "агент молчит".
	e.resolve(key{rule: "agent_silent", obj: snap.NodeID})
}

// Run — периодические проверки: агент молчит, сервис degraded.
// Блокирует до отмены ctx; запускать в горутине.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(checkInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.checkSilentAgents()
			e.checkDegradedServices(ctx)
		}
	}
}

// checkSilentAgents: latest-точка ноды старше 60с → алерт (3.12.1).
func (e *Engine) checkSilentAgents() {
	cutoff := time.Now().Add(-agentSilentAfter).Unix()
	for nodeID, snap := range e.buffer.Latests() {
		k := key{rule: "agent_silent", obj: nodeID}
		if snap.TS < cutoff {
			e.open(k, "warning", "node",
				fmt.Sprintf("agent on %s is silent for over %s", snap.Hostname, agentSilentAfter))
		}
	}
}

// checkDegradedServices: desired > running дольше degradedAfter (3.12.1).
func (e *Engine) checkDegradedServices(ctx context.Context) {
	if e.cluster == nil {
		return
	}
	services, err := e.cluster.Services(ctx)
	if err != nil {
		return
	}
	tasks, err := e.cluster.Tasks(ctx)
	if err != nil {
		return
	}
	running := map[string]int{}
	for _, t := range tasks {
		if t.Status.State == swarm.TaskStateRunning && t.DesiredState == swarm.TaskStateRunning {
			running[t.ServiceID]++
		}
	}
	now := time.Now()
	for _, s := range services {
		if s.Spec.Mode.Replicated == nil || s.Spec.Mode.Replicated.Replicas == nil {
			continue // global-сервисы: покрываются agent_silent/node down
		}
		desired := int(*s.Spec.Mode.Replicated.Replicas)
		k := key{rule: "service_degraded", obj: s.ID}
		if running[s.ID] < desired {
			since, seen := e.degraded[s.ID]
			if !seen {
				e.degraded[s.ID] = now // впервые замечен — ждём degradedAfter
				continue
			}
			if now.Sub(since) >= degradedAfter {
				e.open(k, "critical", "service",
					fmt.Sprintf("service %s: %d/%d replicas running", s.Spec.Name, running[s.ID], desired))
			}
		} else {
			delete(e.degraded, s.ID)
			e.resolve(k)
		}
	}
}
