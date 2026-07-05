// Fake-реализация DockerAPI для тестов: кластер в памяти, без daemon.
package api

import (
	"context"
	"errors"

	"github.com/docker/docker/api/types/swarm"
)

// fakeDocker — управляемый из тестов слепок кластера.
type fakeDocker struct {
	nodes    []swarm.Node
	services []swarm.Service
	tasks    []swarm.Task
	swarm    swarm.Swarm
	rotated  bool // был ли вызван RotateJoinTokens
	removed  []string
	updates  []lastUpdate // история ServiceUpdate
}

func (f *fakeDocker) Ping(ctx context.Context) error { return nil }

func (f *fakeDocker) Nodes(ctx context.Context) ([]swarm.Node, error) { return f.nodes, nil }

func (f *fakeDocker) Services(ctx context.Context) ([]swarm.Service, error) {
	return f.services, nil
}

func (f *fakeDocker) NodeInspect(ctx context.Context, id string) (swarm.Node, error) {
	for _, n := range f.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return swarm.Node{}, errors.New("no such node")
}

func (f *fakeDocker) NodeUpdate(ctx context.Context, id string, version swarm.Version, spec swarm.NodeSpec) error {
	for i := range f.nodes {
		if f.nodes[i].ID == id {
			f.nodes[i].Spec = spec
			return nil
		}
	}
	return errors.New("no such node")
}

func (f *fakeDocker) NodeRemove(ctx context.Context, id string, force bool) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeDocker) SwarmInspect(ctx context.Context) (swarm.Swarm, error) { return f.swarm, nil }

func (f *fakeDocker) RotateJoinTokens(ctx context.Context) error {
	f.rotated = true
	return nil
}

func (f *fakeDocker) NodeTasks(ctx context.Context, nodeID string) ([]swarm.Task, error) {
	out := []swarm.Task{}
	for _, t := range f.tasks {
		if t.NodeID == nodeID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDocker) ServiceInspect(ctx context.Context, id string) (swarm.Service, error) {
	for _, s := range f.services {
		if s.ID == id {
			return s, nil
		}
	}
	return swarm.Service{}, errors.New("no such service")
}

// lastUpdate — параметры последнего ServiceUpdate (для проверок в тестах).
type lastUpdate struct {
	ID           string
	Spec         swarm.ServiceSpec
	RegistryAuth string
	Rollback     string
}

func (f *fakeDocker) ServiceUpdate(ctx context.Context, id string, version swarm.Version,
	spec swarm.ServiceSpec, registryAuth, rollback string) ([]string, error) {
	for i := range f.services {
		if f.services[i].ID == id {
			f.services[i].Spec = spec
			f.updates = append(f.updates, lastUpdate{ID: id, Spec: spec, RegistryAuth: registryAuth, Rollback: rollback})
			return nil, nil
		}
	}
	return nil, errors.New("no such service")
}

func (f *fakeDocker) ServiceRemove(ctx context.Context, id string) error {
	for i := range f.services {
		if f.services[i].ID == id {
			f.services = append(f.services[:i], f.services[i+1:]...)
			return nil
		}
	}
	return errors.New("no such service")
}

func (f *fakeDocker) ServiceTasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	out := []swarm.Task{}
	for _, t := range f.tasks {
		if t.ServiceID == serviceID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDocker) Tasks(ctx context.Context) ([]swarm.Task, error) { return f.tasks, nil }

// mkService — конструктор тестового replicated-сервиса.
func mkService(id, name, image string, replicas uint64, stack string) swarm.Service {
	labels := map[string]string{}
	if stack != "" {
		labels["com.docker.stack.namespace"] = stack
	}
	r := replicas
	return swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name, Labels: labels},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: image},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &r}},
		},
	}
}

// mkNode — конструктор тестовой ноды.
func mkNode(id string, role swarm.NodeRole, avail swarm.NodeAvailability, state swarm.NodeState, leader bool) swarm.Node {
	n := swarm.Node{
		ID: id,
		Spec: swarm.NodeSpec{
			Role:         role,
			Availability: avail,
			Annotations:  swarm.Annotations{Labels: map[string]string{}},
		},
		Status: swarm.NodeStatus{State: state, Addr: "10.0.0.1"},
	}
	n.Description.Hostname = "host-" + id
	if role == swarm.NodeRoleManager {
		n.ManagerStatus = &swarm.ManagerStatus{Leader: leader, Addr: "10.0.0.1:2377"}
	}
	return n
}
