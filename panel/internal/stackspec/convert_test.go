// Тесты конвертера compose → swarm (FR-14): проверяем поля из реального
// стека Мурада — якоря, ${VAR:-default}, deploy, extra_hosts, ports mode:host.
package stackspec

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

// yamlSample — срез реального SHAD-файла: якоря *db-hosts/*app-deploy/*edge-deploy,
// интерполяция, overlay-сеть, порт mode:host.
const yamlSample = `
version: "3.9"

x-db-hosts: &db-hosts
  - "postgres:46.37.123.158"
  - "redis:46.37.123.158"

x-svc-env: &svc-env
  SPRING_PROFILES_ACTIVE: prod
  POSTGRES_USER: ${POSTGRES_USER:-shad}
  POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-shad}

x-app-deploy: &app-deploy
  replicas: 2
  placement:
    constraints: [ "node.labels.role == app" ]
  resources:
    reservations:
      memory: 512M
  restart_policy:
    condition: any
    delay: 10s
  update_config:
    parallelism: 1
    order: start-first
    failure_action: rollback

networks:
  shad-net:
    driver: overlay
    attachable: true

services:
  auth:
    image: ${REGISTRY}/shad-authservice:${TAG:-latest}
    environment: *svc-env
    extra_hosts: *db-hosts
    networks: [ shad-net ]
    deploy: *app-deploy

  gateway:
    image: ${REGISTRY}/shad-gatewayservice:${TAG:-latest}
    ports:
      - target: 8080
        published: 8080
        protocol: tcp
        mode: host
    networks: [ shad-net ]
    deploy:
      replicas: 1
      placement:
        constraints: [ "node.labels.role == edge" ]
`

// loadConvert — хелпер: парсит + конвертит с заданным env.
func loadConvert(t *testing.T, env map[string]string) *Converted {
	t.Helper()
	p, err := Load("shad", []byte(yamlSample), env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, err := Convert("shad", p)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return c
}

// findService ищет сервис по полному имени <stack>_<key>.
func findService(c *Converted, name string) *ServiceSpec {
	for i := range c.Services {
		if c.Services[i].Name == name {
			return &c.Services[i]
		}
	}
	return nil
}

func TestConvertBasics(t *testing.T) {
	c := loadConvert(t, map[string]string{"REGISTRY": "myuser", "TAG": "v3"})

	// Сеть: <stack>_shad-net, overlay, attachable, не external.
	if len(c.Networks) != 1 {
		t.Fatalf("networks: want 1, got %d", len(c.Networks))
	}
	n := c.Networks[0]
	if n.Name != "shad_shad-net" || n.Driver != "overlay" || !n.Attachable || n.External {
		t.Fatalf("network wrong: %+v", n)
	}

	// Два сервиса с префиксом стека.
	if len(c.Services) != 2 {
		t.Fatalf("services: want 2, got %d", len(c.Services))
	}
	auth := findService(c, "shad_auth")
	if auth == nil {
		t.Fatal("сервис shad_auth не найден")
	}

	// Имя, namespace-label (service и container уровни).
	if auth.Spec.Annotations.Name != "shad_auth" {
		t.Fatalf("name: %q", auth.Spec.Annotations.Name)
	}
	if auth.Spec.Annotations.Labels[StackLabel] != "shad" {
		t.Fatal("service namespace label потерян")
	}
	if auth.Spec.TaskTemplate.ContainerSpec.Labels[StackLabel] != "shad" {
		t.Fatal("container namespace label потерян")
	}

	// Интерполяция образа: ${REGISTRY} задан, ${TAG:-latest} переопределён.
	if got := auth.Spec.TaskTemplate.ContainerSpec.Image; got != "myuser/shad-authservice:v3" {
		t.Fatalf("image интерполяция: %q", got)
	}
}

func TestConvertDeployFields(t *testing.T) {
	c := loadConvert(t, map[string]string{"REGISTRY": "myuser"})
	auth := findService(c, "shad_auth")

	// Replicas из якоря *app-deploy = 2.
	if r := auth.Spec.Mode.Replicated; r == nil || *r.Replicas != 2 {
		t.Fatalf("replicas: %+v", auth.Spec.Mode.Replicated)
	}
	// Placement constraint.
	if p := auth.Spec.TaskTemplate.Placement; p == nil || len(p.Constraints) != 1 ||
		p.Constraints[0] != "node.labels.role == app" {
		t.Fatalf("placement: %+v", auth.Spec.TaskTemplate.Placement)
	}
	// Reservations 512M.
	res := auth.Spec.TaskTemplate.Resources
	if res == nil || res.Reservations == nil || res.Reservations.MemoryBytes != 512*1024*1024 {
		t.Fatalf("reservations: %+v", res)
	}
	// update_config.order — ПРОБРОШЕН из файла (не навязан и не затёрт).
	if u := auth.Spec.UpdateConfig; u == nil || u.Order != "start-first" {
		t.Fatalf("update order должен быть из файла start-first: %+v", auth.Spec.UpdateConfig)
	}
	// restart_policy.
	if rp := auth.Spec.TaskTemplate.RestartPolicy; rp == nil ||
		rp.Condition != swarm.RestartPolicyConditionAny {
		t.Fatalf("restart policy: %+v", auth.Spec.TaskTemplate.RestartPolicy)
	}
}

func TestConvertEnvDefaultsAndHosts(t *testing.T) {
	// POSTGRES_USER задаём, POSTGRES_PASSWORD — нет (сработает :-shad).
	c := loadConvert(t, map[string]string{"REGISTRY": "x", "POSTGRES_USER": "prod_user"})
	auth := findService(c, "shad_auth")
	env := auth.Spec.TaskTemplate.ContainerSpec.Env

	has := func(want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}
	if !has("POSTGRES_USER=prod_user") {
		t.Fatalf("env POSTGRES_USER не из env: %v", env)
	}
	if !has("POSTGRES_PASSWORD=shad") {
		t.Fatalf("env default ${..:-shad} не сработал: %v", env)
	}

	// extra_hosts (якорь *db-hosts) → формат "ip host".
	hosts := auth.Spec.TaskTemplate.ContainerSpec.Hosts
	if len(hosts) != 2 {
		t.Fatalf("extra_hosts: want 2, got %v", hosts)
	}
	found := false
	for _, h := range hosts {
		if h == "46.37.123.158 postgres" {
			found = true
		}
	}
	if !found {
		t.Fatalf("формат extra_hosts должен быть 'ip host': %v", hosts)
	}
}

func TestConvertPortHostMode(t *testing.T) {
	c := loadConvert(t, map[string]string{"REGISTRY": "x"})
	gw := findService(c, "shad_gateway")
	if gw == nil {
		t.Fatal("shad_gateway не найден")
	}
	ep := gw.Spec.EndpointSpec
	if ep == nil || len(ep.Ports) != 1 {
		t.Fatalf("endpoint ports: %+v", ep)
	}
	p := ep.Ports[0]
	if p.TargetPort != 8080 || p.PublishedPort != 8080 ||
		p.PublishMode != swarm.PortConfigPublishModeHost {
		t.Fatalf("порт mode:host неверен: %+v", p)
	}
}
