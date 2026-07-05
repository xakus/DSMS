// Package dockerapi — обёртка над официальным Docker SDK.
//
// Панель — прокси-слой над Docker Engine API (разд. 2.2 ТЗ): nodes, services,
// tasks, logs, swarm. Клиент общается через /var/run/docker.sock.
package dockerapi

import (
	"context"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// Client — тонкая обёртка над docker client с доменными методами.
type Client struct {
	c *client.Client
}

// New создаёт клиента с автосогласованием версии API —
// обязательное требование (разд. 10 ТЗ, риск несовместимости версий).
func New() (*Client, error) {
	c, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &Client{c: c}, nil
}

// Close освобождает ресурсы клиента.
func (cl *Client) Close() error { return cl.c.Close() }

// Ping проверяет доступность Docker daemon (для /healthz-диагностики).
func (cl *Client) Ping(ctx context.Context) error {
	_, err := cl.c.Ping(ctx)
	return err
}

// Nodes возвращает список нод кластера (FR-01, FR-02).
func (cl *Client) Nodes(ctx context.Context) ([]swarm.Node, error) {
	return cl.c.NodeList(ctx, swarm.NodeListOptions{})
}

// Services возвращает список сервисов с их спецификациями (FR-04).
func (cl *Client) Services(ctx context.Context) ([]swarm.Service, error) {
	return cl.c.ServiceList(ctx, swarm.ServiceListOptions{})
}
