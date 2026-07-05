// Операции над нодами и swarm (FR-02, FR-03).
package dockerapi

import (
	"context"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

// NodeInspect возвращает полную спецификацию ноды.
func (cl *Client) NodeInspect(ctx context.Context, id string) (swarm.Node, error) {
	n, _, err := cl.c.NodeInspectWithRaw(ctx, id)
	return n, err
}

// NodeUpdate применяет новую спецификацию ноды.
// version берётся из свежего inspect — оптимистическая блокировка Swarm.
func (cl *Client) NodeUpdate(ctx context.Context, id string, version swarm.Version, spec swarm.NodeSpec) error {
	return cl.c.NodeUpdate(ctx, id, version, spec)
}

// NodeRemove удаляет ноду из кластера (force — для не-down нод, 3.3.3).
func (cl *Client) NodeRemove(ctx context.Context, id string, force bool) error {
	return cl.c.NodeRemove(ctx, id, swarm.NodeRemoveOptions{Force: force})
}

// SwarmInspect возвращает состояние swarm — нужен для join-tokens (3.3.4).
func (cl *Client) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	return cl.c.SwarmInspect(ctx)
}

// RotateJoinTokens ротирует оба join-токена (3.3.4).
func (cl *Client) RotateJoinTokens(ctx context.Context) error {
	sw, err := cl.c.SwarmInspect(ctx)
	if err != nil {
		return err
	}
	return cl.c.SwarmUpdate(ctx, sw.Version, sw.Spec, swarm.UpdateFlags{
		RotateWorkerToken:  true,
		RotateManagerToken: true,
	})
}

// NodeTasks возвращает задачи, размещённые на ноде (3.2.4).
func (cl *Client) NodeTasks(ctx context.Context, nodeID string) ([]swarm.Task, error) {
	return cl.c.TaskList(ctx, swarm.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("node", nodeID)),
	})
}
