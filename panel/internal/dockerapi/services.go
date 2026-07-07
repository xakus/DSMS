// Операции над сервисами (FR-04) и их задачами.
package dockerapi

import (
	"context"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

// ServiceInspect возвращает сервис со вставленными Defaults.
func (cl *Client) ServiceInspect(ctx context.Context, id string) (swarm.Service, error) {
	s, _, err := cl.c.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	return s, err
}

// ServiceUpdate применяет спецификацию сервиса.
// registryAuth — base64 X-Registry-Auth для приватных образов (FR-13);
// rollback — "previous" для отката (3.4.3 Rollback).
func (cl *Client) ServiceUpdate(ctx context.Context, id string, version swarm.Version,
	spec swarm.ServiceSpec, registryAuth, rollback string) ([]string, error) {
	resp, err := cl.c.ServiceUpdate(ctx, id, version, spec, swarm.ServiceUpdateOptions{
		EncodedRegistryAuth: registryAuth,
		RegistryAuthFrom:    swarm.RegistryAuthFromSpec,
		Rollback:            rollback,
	})
	return resp.Warnings, err
}

// ServiceDeploy тянет свежую версию образа по тегу и катит обновление.
// В отличие от ServiceUpdate (redeploy = ForceUpdate++, тот же digest),
// QueryRegistry=true заставляет Swarm заново разрезолвить тег образа в
// реестре — аналог `docker service update --image repo:tag`. Старые
// задачи заменяются согласно UpdateConfig сервиса (start-first — новый
// поднимается раньше, чем убивается старый).
func (cl *Client) ServiceDeploy(ctx context.Context, id string, version swarm.Version,
	spec swarm.ServiceSpec, registryAuth string) ([]string, error) {
	resp, err := cl.c.ServiceUpdate(ctx, id, version, spec, swarm.ServiceUpdateOptions{
		EncodedRegistryAuth: registryAuth,
		RegistryAuthFrom:    swarm.RegistryAuthFromSpec,
		QueryRegistry:       true,
	})
	return resp.Warnings, err
}

// ServiceRemove удаляет сервис (3.4.3 Remove).
func (cl *Client) ServiceRemove(ctx context.Context, id string) error {
	return cl.c.ServiceRemove(ctx, id)
}

// ServiceTasks возвращает задачи сервиса, включая историю (3.4.4).
func (cl *Client) ServiceTasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	return cl.c.TaskList(ctx, swarm.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("service", serviceID)),
	})
}

// Tasks возвращает все задачи кластера (для агрегации running/desired).
func (cl *Client) Tasks(ctx context.Context) ([]swarm.Task, error) {
	return cl.c.TaskList(ctx, swarm.TaskListOptions{})
}
