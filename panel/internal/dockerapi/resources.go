// Операции над secrets/configs/networks (FR-09, FR-10).
// Это swarm-объекты — доступны через docker.sock manager-ноды.
package dockerapi

import (
	"context"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

// Secrets возвращает список secrets кластера (3.9.1).
func (cl *Client) Secrets(ctx context.Context) ([]swarm.Secret, error) {
	return cl.c.SecretList(ctx, swarm.SecretListOptions{})
}

// SecretCreate создаёт secret; значение уходит в Docker и обратно
// не читается — secrets неизвлекаемы (3.9.2).
func (cl *Client) SecretCreate(ctx context.Context, name string, data []byte) (string, error) {
	resp, err := cl.c.SecretCreate(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name},
		Data:        data,
	})
	return resp.ID, err
}

// SecretRemove удаляет secret (3.9.2).
func (cl *Client) SecretRemove(ctx context.Context, id string) error {
	return cl.c.SecretRemove(ctx, id)
}

// Configs возвращает список configs (3.9.3).
func (cl *Client) Configs(ctx context.Context) ([]swarm.Config, error) {
	return cl.c.ConfigList(ctx, swarm.ConfigListOptions{})
}

// ConfigInspect возвращает config с содержимым (configs читаемы, 3.9.3).
func (cl *Client) ConfigInspect(ctx context.Context, id string) (swarm.Config, error) {
	cfg, _, err := cl.c.ConfigInspectWithRaw(ctx, id)
	return cfg, err
}

// ConfigCreate создаёт config (3.9.3).
func (cl *Client) ConfigCreate(ctx context.Context, name string, data []byte) (string, error) {
	resp, err := cl.c.ConfigCreate(ctx, swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name},
		Data:        data,
	})
	return resp.ID, err
}

// ConfigRemove удаляет config (3.9.3).
func (cl *Client) ConfigRemove(ctx context.Context, id string) error {
	return cl.c.ConfigRemove(ctx, id)
}

// Networks возвращает список сетей (3.10.1).
func (cl *Client) Networks(ctx context.Context) ([]network.Summary, error) {
	return cl.c.NetworkList(ctx, network.ListOptions{})
}

// NetworkInspect — детали сети с подключёнными контейнерами (3.10.2).
func (cl *Client) NetworkInspect(ctx context.Context, id string) (network.Inspect, error) {
	return cl.c.NetworkInspect(ctx, id, network.InspectOptions{Verbose: true})
}

// NetworkCreate создаёт overlay-сеть (3.10.3).
func (cl *Client) NetworkCreate(ctx context.Context, name, driver string, attachable bool, subnet string) (string, error) {
	opts := network.CreateOptions{
		Driver:     driver,
		Attachable: attachable,
	}
	if subnet != "" {
		opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: subnet}}}
	}
	resp, err := cl.c.NetworkCreate(ctx, name, opts)
	return resp.ID, err
}

// NetworkRemove удаляет сеть (3.10.3; Docker сам откажет, если используется).
func (cl *Client) NetworkRemove(ctx context.Context, id string) error {
	return cl.c.NetworkRemove(ctx, id)
}
