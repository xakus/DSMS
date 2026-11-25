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
	c, err := Convert("shad", p, env)
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
	if auth.Spec.Name != "shad_auth" {
		t.Fatalf("name: %q", auth.Spec.Name)
	}
	if auth.Spec.Labels[StackLabel] != "shad" {
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

// TestConvertNetworkAliases: каждому сервису на каждой сети добавляется
// псевдоним — короткое имя из файла, как у `docker stack deploy`. Без него
// «app» не находит «db» по имени. Найдено живой проверкой 25.11.2026:
// wget http://web из соседнего сервиса падал с «bad address 'web'».
func TestConvertNetworkAliases(t *testing.T) {
	const y = `
services:
  web:
    image: nginx:1.27
    networks:
      front:
        aliases: [ site, web ]
      back: {}
  db:
    image: postgres:17
  edge:
    image: nginx:1.27
    networks: [ hostnet ]
networks:
  front: {}
  back: {}
  hostnet:
    external: true
    name: host
`
	proj, err := Load("app", []byte(y), nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	conv, err := Convert("app", proj, nil)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	aliases := map[string]map[string][]string{} // сервис → сеть → псевдонимы
	for _, s := range conv.Services {
		aliases[s.Name] = map[string][]string{}
		for _, n := range s.Spec.TaskTemplate.Networks {
			aliases[s.Name][n.Target] = n.Aliases
		}
	}

	// Сеть без явных псевдонимов — только короткое имя.
	if got := aliases["app_web"]["app_back"]; len(got) != 1 || got[0] != "web" {
		t.Fatalf("web@back: want [web], got %v", got)
	}
	// Явные псевдонимы сохраняются, имя сервиса не дублируется.
	if got := aliases["app_web"]["app_front"]; len(got) != 2 || got[0] != "site" || got[1] != "web" {
		t.Fatalf("web@front: want [site web], got %v", got)
	}
	// Сервис без networks сидит в default — и там тоже находится по имени.
	if got := aliases["app_db"]["app_default"]; len(got) != 1 || got[0] != "db" {
		t.Fatalf("db@default: want [db], got %v", got)
	}
	// Встроенная сеть host псевдонимов не принимает.
	if got := aliases["app_edge"]["host"]; len(got) != 0 {
		t.Fatalf("edge@host: aliases must be empty, got %v", got)
	}
}
