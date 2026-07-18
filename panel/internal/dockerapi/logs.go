// Стриминг логов и событий Docker (FR-05, FR-06).
package dockerapi

import (
	"context"
	"io"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
)

// ServiceLogs открывает поток логов сервиса (все реплики, 3.5.1).
// Details=true добавляет swarm-метаданные (task/node) в каждую строку,
// Timestamps=true — честное время из daemon.
func (cl *Client) ServiceLogs(ctx context.Context, serviceID string, tail int, follow bool) (io.ReadCloser, error) {
	return cl.c.ServiceLogs(ctx, serviceID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       strconv.Itoa(tail),
		Timestamps: true,
		Details:    true,
	})
}

// ServiceLogsSnapshot — одноразовый дамп логов сервиса без follow, для
// постраничной подгрузки истории. until (RFC3339Nano, опц.) ограничивает
// выборку моментом ДО until — так листаем историю назад по времени.
func (cl *Client) ServiceLogsSnapshot(ctx context.Context, serviceID string, tail int, until string) (io.ReadCloser, error) {
	return cl.c.ServiceLogs(ctx, serviceID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
		Tail:       strconv.Itoa(tail),
		Timestamps: true,
		Details:    true,
		Until:      until,
	})
}

// TaskLogs открывает поток логов одной задачи (3.5.5).
func (cl *Client) TaskLogs(ctx context.Context, taskID string, tail int, follow bool) (io.ReadCloser, error) {
	return cl.c.TaskLogs(ctx, taskID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       strconv.Itoa(tail),
		Timestamps: true,
		Details:    true,
	})
}

// Events подписывается на события Docker daemon (FR-06 3.6.1).
func (cl *Client) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	return cl.c.Events(ctx, events.ListOptions{})
}
