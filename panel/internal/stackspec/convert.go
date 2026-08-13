package stackspec

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
)

// StackLabel — label, по которому Docker/Swarm группирует сервисы в стек.
// Тот же, что используется в FR-08 для агрегации.
const StackLabel = "com.docker.stack.namespace"

// Converted — результат конвертации стека: готовые к созданию сети и сервисы.
type Converted struct {
	Networks []NetworkSpec
	Services []ServiceSpec
}

// NetworkSpec — сеть стека (создаётся, если не external).
type NetworkSpec struct {
	Name       string // реальное имя в Docker: <stack>_<key> (или своё для external)
	Driver     string
	Attachable bool
	External   bool // external — панель не создаёт, только ссылается
}

// ServiceSpec — сервис стека с полным именем <stack>_<key>.
type ServiceSpec struct {
	Name string
	Spec swarm.ServiceSpec
}

// Convert превращает распарсенный проект в спецификации Swarm.
// stack — имя стека (namespace); имена сервисов/сетей получают префикс <stack>_.
func Convert(stack string, p *types.Project) (*Converted, error) {
	out := &Converted{}

	// Сети: строим карту «ключ compose → реальное имя в Docker».
	netName := map[string]string{}
	netKeys := sortedKeys(p.Networks)
	for _, k := range netKeys {
		n := p.Networks[k]
		if n.External {
			// external: ссылаемся по заданному имени (или ключу), не создаём.
			name := n.Name
			if name == "" {
				name = k
			}
			netName[k] = name
			continue
		}
		full := stack + "_" + k
		netName[k] = full
		driver := n.Driver
		if driver == "" {
			driver = "overlay" // дефолт для swarm-стека
		}
		out.Networks = append(out.Networks, NetworkSpec{
			Name:       full,
			Driver:     driver,
			Attachable: n.Attachable,
		})
	}

	// Сервисы.
	svcKeys := sortedServiceKeys(p.Services)
	for _, key := range svcKeys {
		s := p.Services[key]
		spec, err := toServiceSpec(stack, key, s, netName)
		if err != nil {
			return nil, fmt.Errorf("сервис %q: %w", key, err)
		}
		out.Services = append(out.Services, ServiceSpec{Name: stack + "_" + key, Spec: spec})
	}
	return out, nil
}

// toServiceSpec конвертирует один сервис compose в swarm.ServiceSpec.
func toServiceSpec(stack, key string, s types.ServiceConfig, netName map[string]string) (swarm.ServiceSpec, error) {
	full := stack + "_" + key

	// Container-level labels: top-level labels + namespace.
	containerLabels := map[string]string{StackLabel: stack}
	for k, v := range s.Labels {
		containerLabels[k] = v
	}

	cs := &swarm.ContainerSpec{
		Image:  s.Image,
		Labels: containerLabels,
		Env:    envToSlice(s.Environment),
		Hosts:  extraHosts(s.ExtraHosts),
	}
	if len(s.Entrypoint) > 0 {
		cs.Command = []string(s.Entrypoint)
	}
	if len(s.Command) > 0 {
		cs.Args = []string(s.Command)
	}
	if s.HealthCheck != nil && !s.HealthCheck.Disable {
		cs.Healthcheck = healthConfig(s.HealthCheck)
	}

	// Service-level labels: deploy.labels + namespace.
	serviceLabels := map[string]string{StackLabel: stack}

	spec := swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: full, Labels: serviceLabels},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: cs},
	}
	spec.TaskTemplate.Networks = serviceNetworks(s.Networks, netName)

	if s.Deploy != nil {
		applyDeploy(&spec, *s.Deploy)
	} else {
		spec.Mode = replicated(1)
	}

	if len(s.Ports) > 0 {
		ep, err := endpointSpec(s.Ports)
		if err != nil {
			return swarm.ServiceSpec{}, err
		}
		spec.EndpointSpec = ep
	}
	return spec, nil
}

// applyDeploy переносит блок deploy (replicas/placement/resources/restart/update).
// ВАЖНО: order из update_config берём как есть из файла — панель НЕ навязывает
// start-first (урок FR-08: навязанный order ломал ноды с нехваткой памяти).
func applyDeploy(spec *swarm.ServiceSpec, d types.DeployConfig) {
	// Mode: global или replicated (дефолт 1 реплика).
	if strings.EqualFold(d.Mode, "global") {
		spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	} else {
		var n uint64 = 1
		if d.Replicas != nil {
			n = uint64(*d.Replicas)
		}
		spec.Mode = replicated(n)
	}

	// deploy.labels — это service-level labels.
	for k, v := range d.Labels {
		spec.Annotations.Labels[k] = v
	}

	// Placement.
	if len(d.Placement.Constraints) > 0 {
		spec.TaskTemplate.Placement = &swarm.Placement{Constraints: d.Placement.Constraints}
	}

	// Resources.
	if res := resourceReq(d.Resources); res != nil {
		spec.TaskTemplate.Resources = res
	}

	// RestartPolicy.
	if d.RestartPolicy != nil {
		spec.TaskTemplate.RestartPolicy = restartPolicy(d.RestartPolicy)
	}

	// UpdateConfig.
	if d.UpdateConfig != nil {
		spec.UpdateConfig = updateConfig(d.UpdateConfig)
	}
}

// ── Хелперы ────────────────────────────────────────────────────────────────

func replicated(n uint64) swarm.ServiceMode {
	r := n
	return swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &r}}
}

// envToSlice превращает map окружения compose в отсортированный []string "K=V".
// Сортировка — для детерминизма (стабильные спеки, предсказуемые тесты).
func envToSlice(env types.MappingWithEquals) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		v := env[k]
		if v == nil {
			out = append(out, k) // KEY без значения — пробрасывается как есть
			continue
		}
		out = append(out, k+"="+*v)
	}
	return out
}

// extraHosts переводит compose extra_hosts (host → [ip...]) в формат swarm
// ContainerSpec.Hosts: строки вида "ip host" (как строка /etc/hosts).
func extraHosts(h types.HostsList) []string {
	if len(h) == 0 {
		return nil
	}
	hosts := make([]string, 0, len(h))
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, ip := range h[name] {
			hosts = append(hosts, ip+" "+name)
		}
	}
	return hosts
}

// serviceNetworks строит привязки сервиса к сетям по реальным именам.
func serviceNetworks(nets map[string]*types.ServiceNetworkConfig, netName map[string]string) []swarm.NetworkAttachmentConfig {
	if len(nets) == 0 {
		return nil
	}
	keys := make([]string, 0, len(nets))
	for k := range nets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]swarm.NetworkAttachmentConfig, 0, len(keys))
	for _, k := range keys {
		target := netName[k]
		if target == "" {
			target = k
		}
		att := swarm.NetworkAttachmentConfig{Target: target}
		if cfg := nets[k]; cfg != nil && len(cfg.Aliases) > 0 {
			att.Aliases = cfg.Aliases
		}
		out = append(out, att)
	}
	return out
}

// resourceReq конвертирует limits/reservations. nil, если ничего не задано.
func resourceReq(r types.Resources) *swarm.ResourceRequirements {
	var out swarm.ResourceRequirements
	has := false
	if r.Limits != nil {
		out.Limits = &swarm.Limit{
			NanoCPUs:    nanoCPUs(r.Limits.NanoCPUs),
			MemoryBytes: int64(r.Limits.MemoryBytes),
		}
		has = true
	}
	if r.Reservations != nil {
		out.Reservations = &swarm.Resources{
			NanoCPUs:    nanoCPUs(r.Reservations.NanoCPUs),
			MemoryBytes: int64(r.Reservations.MemoryBytes),
		}
		has = true
	}
	if !has {
		return nil
	}
	return &out
}

// nanoCPUs переводит compose cpus (например 0.5) в наноядра Swarm (0.5 → 5e8).
func nanoCPUs(c types.NanoCPUs) int64 {
	return int64(float64(c) * 1e9)
}

func restartPolicy(r *types.RestartPolicy) *swarm.RestartPolicy {
	out := &swarm.RestartPolicy{
		Condition:   swarm.RestartPolicyCondition(r.Condition),
		MaxAttempts: r.MaxAttempts,
	}
	if r.Delay != nil {
		d := time.Duration(*r.Delay)
		out.Delay = &d
	}
	if r.Window != nil {
		w := time.Duration(*r.Window)
		out.Window = &w
	}
	return out
}

func updateConfig(u *types.UpdateConfig) *swarm.UpdateConfig {
	out := &swarm.UpdateConfig{
		FailureAction:   u.FailureAction,
		Order:           u.Order, // как в файле — не навязываем
		MaxFailureRatio: u.MaxFailureRatio,
		Delay:           time.Duration(u.Delay),
		Monitor:         time.Duration(u.Monitor),
	}
	if u.Parallelism != nil {
		out.Parallelism = *u.Parallelism
	}
	return out
}

func healthConfig(h *types.HealthCheckConfig) *container.HealthConfig {
	out := &container.HealthConfig{Test: []string(h.Test)}
	if h.Retries != nil {
		out.Retries = int(*h.Retries)
	}
	if h.Interval != nil {
		out.Interval = time.Duration(*h.Interval)
	}
	if h.Timeout != nil {
		out.Timeout = time.Duration(*h.Timeout)
	}
	if h.StartPeriod != nil {
		out.StartPeriod = time.Duration(*h.StartPeriod)
	}
	return out
}

// endpointSpec конвертирует опубликованные порты. mode:host → PublishModeHost,
// иначе ingress-mesh (дефолт Swarm).
func endpointSpec(ports []types.ServicePortConfig) (*swarm.EndpointSpec, error) {
	pcs := make([]swarm.PortConfig, 0, len(ports))
	for _, p := range ports {
		pc := swarm.PortConfig{
			TargetPort: p.Target,
			Protocol:   swarm.PortConfigProtocol(orDefault(p.Protocol, "tcp")),
		}
		if p.Published != "" {
			pub, err := strconv.ParseUint(p.Published, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("порт published=%q: %w", p.Published, err)
			}
			pc.PublishedPort = uint32(pub)
		}
		if strings.EqualFold(p.Mode, "host") {
			pc.PublishMode = swarm.PortConfigPublishModeHost
		} else {
			pc.PublishMode = swarm.PortConfigPublishModeIngress
		}
		pcs = append(pcs, pc)
	}
	return &swarm.EndpointSpec{Ports: pcs}, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func sortedKeys(m types.Networks) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedServiceKeys(m types.Services) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
