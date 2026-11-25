// Отличает настоящее падение задачи от штатной остановки (FR-12).
//
// Событие Docker «container die» с ненулевым кодом приходит в обоих случаях:
// при Stop сервиса, деплое или scale-down Swarm шлёт контейнеру SIGTERM, а
// через паузу — SIGKILL, и процесс выходит с кодом 143 или 137. Раньше каждый
// такой выход давал алерт task_failed: остановка стека из 10 сервисов —
// 10 ложных тревог, деплой Java-сервиса (выход 143) — тревога на каждый деплой.
//
// Правду знает Swarm: у задачи, упавшей самой, состояние «failed», у
// остановленной намеренно — «shutdown».
package streams

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"
)

// TaskSettleDelay — пауза между смертью контейнера и проверкой состояния
// задачи. Агент Swarm сообщает manager'у итог задачи не мгновенно: сразу
// после события задача ещё числится «running».
const TaskSettleDelay = 2 * time.Second

// Атрибуты события, которыми Swarm помечает контейнеры задач.
const (
	attrTaskID   = "com.docker.swarm.task.id"
	attrTaskName = "com.docker.swarm.task.name"
)

// TaskStater — состояние задачи Swarm по её ID (реализуется dockerapi.Client).
type TaskStater interface {
	TaskState(ctx context.Context, taskID string) (swarm.TaskState, error)
}

// ContainerDeath — выход контейнера с ненулевым кодом.
type ContainerDeath struct {
	Name     string // имя задачи Swarm либо имя контейнера
	ExitCode string // код выхода как в событии («137»)
	TaskID   string // ID задачи Swarm; пусто — обычный контейнер, не задача
}

// ParseDeath разбирает событие Docker. ok=false — это не выход контейнера
// с ненулевым кодом (другое событие или штатное завершение с кодом 0).
func ParseDeath(m events.Message) (ContainerDeath, bool) {
	if m.Type != events.ContainerEventType || m.Action != events.ActionDie {
		return ContainerDeath{}, false
	}
	attrs := m.Actor.Attributes
	code := attrs["exitCode"]
	if code == "" || code == "0" {
		return ContainerDeath{}, false
	}
	name := attrs[attrTaskName]
	if name == "" {
		name = attrs["name"]
	}
	return ContainerDeath{Name: name, ExitCode: code, TaskID: attrs[attrTaskID]}, true
}

// IsRealFailure решает, заслуживает ли выход контейнера алерта task_failed.
//
//   - обычный контейнер (не задача Swarm) — да: спросить о намерениях некого;
//   - задача в состоянии failed/rejected — да: упала сама;
//   - задача в любом другом состоянии (shutdown, remove…) — нет: её остановили;
//   - задачу не найти (сервис уже удалён) — нет: удаление было намеренным.
//
// Ждёт delay перед запросом состояния и блокирует на это время — вызывать
// в отдельной горутине. Отмена ctx прерывает ожидание с ответом false.
func IsRealFailure(ctx context.Context, src TaskStater, d ContainerDeath, delay time.Duration) bool {
	if d.TaskID == "" {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
	}
	state, err := src.TaskState(ctx, d.TaskID)
	if err != nil {
		return false
	}
	return state == swarm.TaskStateFailed || state == swarm.TaskStateRejected
}
