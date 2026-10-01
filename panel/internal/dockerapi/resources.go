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
// не читается — secrets неизвлекаемы (3.9.2). labels нужны стекам (FR-14):
// без label namespace объект не опознаётся как часть стека.
func (cl *Client) SecretCreate(ctx context.Context, name string, data []byte, labels map[string]string) (string, error) {
	resp, err := cl.c.SecretCreate(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
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

// ConfigCreate создаёт config (3.9.3); labels — как у SecretCreate.
func (cl *Client) ConfigCreate(ctx context.Context, name string, data []byte, labels map[string]string) (string, error) {
	resp, err := cl.c.ConfigCreate(ctx, swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
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

// NetworkOptions — параметры создания сети. Отдельный тип, потому что кроме
// ручного создания из UI (3.10.3) сети создаёт деплой стека (FR-14), которому
// нужны labels, driver_opts и флаги internal/ipv6.
type NetworkOptions struct {
	Driver     string
	Attachable bool
	Internal   bool
	EnableIPv6 bool
	Subnet     string
	Options    map[string]string // driver_opts
	Labels     map[string]string
}

// NetworkCreate создаёт сеть (3.10.3, FR-14).
func (cl *Client) NetworkCreate(ctx context.Context, name string, o NetworkOptions) (string, error) {
	opts := network.CreateOptions{
		Driver:     o.Driver,
		Attachable: o.Attachable,
		Internal:   o.Internal,
		Options:    o.Options,
		Labels:     o.Labels,
	}
	if o.EnableIPv6 {
		ipv6 := true
		opts.EnableIPv6 = &ipv6
	}
	if o.Subnet != "" {
		opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: o.Subnet}}}
	}
	resp, err := cl.c.NetworkCreate(ctx, name, opts)
	return resp.ID, err
}

// NetworkRemove удаляет сеть (3.10.3; Docker сам откажет, если используется).
func (cl *Client) NetworkRemove(ctx context.Context, id string) error {
	return cl.c.NetworkRemove(ctx, id)
}
