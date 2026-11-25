// Тесты отличия падения задачи от штатной остановки (FR-12).
package streams

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"
)

// fakeTasks — состояния задач по ID; отсутствующая задача — ошибка.
type fakeTasks map[string]swarm.TaskState

func (f fakeTasks) TaskState(_ context.Context, id string) (swarm.TaskState, error) {
	st, ok := f[id]
	if !ok {
		return "", errors.New("task not found")
	}
	return st, nil
}

// die — событие смерти контейнера с заданными атрибутами.
func die(attrs map[string]string) events.Message {
	return events.Message{
		Type: events.ContainerEventType, Action: events.ActionDie,
		Actor: events.Actor{ID: "c1", Attributes: attrs},
	}
}

// TestParseDeath: берутся только выходы контейнера с ненулевым кодом.
func TestParseDeath(t *testing.T) {
	d, ok := ParseDeath(die(map[string]string{
		"exitCode": "137", "name": "c-name",
		"com.docker.swarm.task.name": "app_web.1.abc", "com.docker.swarm.task.id": "t1",
	}))
	if !ok || d.Name != "app_web.1.abc" || d.ExitCode != "137" || d.TaskID != "t1" {
		t.Fatalf("swarm task death: %+v ok=%v", d, ok)
	}

	// Обычный контейнер: имя берётся из name, задачи нет.
	d, ok = ParseDeath(die(map[string]string{"exitCode": "1", "name": "plain"}))
	if !ok || d.Name != "plain" || d.TaskID != "" {
		t.Fatalf("plain container death: %+v ok=%v", d, ok)
	}

	// Код 0 — штатное завершение, не падение.
	if _, ok := ParseDeath(die(map[string]string{"exitCode": "0"})); ok {
		t.Fatal("exit code 0 must not be a death")
	}
	// Другое событие контейнера.
	start := die(map[string]string{"exitCode": "1"})
	start.Action = events.ActionStart
	if _, ok := ParseDeath(start); ok {
		t.Fatal("non-die event must be ignored")
	}
	// Событие не про контейнер.
	svc := die(map[string]string{"exitCode": "1"})
	svc.Type = events.ServiceEventType
	if _, ok := ParseDeath(svc); ok {
		t.Fatal("non-container event must be ignored")
	}
}

// TestIsRealFailure: алерт только когда Swarm считает задачу упавшей.
func TestIsRealFailure(t *testing.T) {
	tasks := fakeTasks{
		"crashed":  swarm.TaskStateFailed,
		"rejected": swarm.TaskStateRejected,
		"stopped":  swarm.TaskStateShutdown, // Stop стека / деплой / scale-down
		"removing": swarm.TaskStateRemove,
	}
	cases := []struct {
		name string
		task string
		want bool
	}{
		{"задача упала сама", "crashed", true},
		{"задача отклонена нодой", "rejected", true},
		{"задачу остановили (SIGKILL → 137)", "stopped", false},
		{"задача удаляется", "removing", false},
		{"задачи уже нет — сервис удалён", "gone", false},
		{"обычный контейнер, не задача Swarm", "", true},
	}
	for _, c := range cases {
		got := IsRealFailure(context.Background(), tasks, ContainerDeath{TaskID: c.task, ExitCode: "137"}, 0)
		if got != c.want {
			t.Errorf("%s: want %v, got %v", c.name, c.want, got)
		}
	}
}

// TestIsRealFailureCanceled: панель завершается — не ждём и не алертим.
func TestIsRealFailureCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tasks := fakeTasks{"crashed": swarm.TaskStateFailed}
	if IsRealFailure(ctx, tasks, ContainerDeath{TaskID: "crashed"}, TaskSettleDelay) {
		t.Fatal("canceled context must not produce an alert")
	}
}
