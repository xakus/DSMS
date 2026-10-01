package stackspec

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// StackLabel — label, по которому Docker/Swarm группирует сервисы в стек.
// Тот же, что используется в FR-08 для агрегации.
const StackLabel = "com.docker.stack.namespace"

// Converted — результат конвертации стека: сети, secrets/configs, сервисы
// и предупреждения о полях, которые не удалось перенести (ТЗ 3.14.10).
type Converted struct {
	Networks []NetworkSpec
	Secrets  []FileObject
	Configs  []FileObject
	Services []ServiceSpec
	Warnings []Warning
}

// NetworkSpec — сеть стека (создаётся, если не external).
type NetworkSpec struct {
	Name       string // реальное имя в Docker: <stack>_<key> (или своё для external)
	Driver     string
	Attachable bool
	Internal   bool
	EnableIPv6 bool
	Subnet     string
	Options    map[string]string // driver_opts
	Labels     map[string]string // включая com.docker.stack.namespace
	External   bool              // external — панель не создаёт, только ссылается
}

// ServiceSpec — сервис стека с полным именем <stack>_<key>.
type ServiceSpec struct {
	Name string
	Spec swarm.ServiceSpec
}

// Convert превращает распарсенный проект в спецификации Swarm.
// stack — имя стека (namespace); имена сервисов/сетей получают префикс <stack>_.
// env нужен для секретов/конфигов, объявленных через `environment:`.
func Convert(stack string, p *types.Project, env map[string]string) (*Converted, error) {
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
			out.Networks = append(out.Networks, NetworkSpec{Name: name, External: true})
			continue
		}
		full := stack + "_" + k
		netName[k] = full
		driver := n.Driver
		if driver == "" {
			driver = "overlay" // дефолт для swarm-стека
		}
		// Label стека обязателен: без него сеть не опознаётся как часть стека
		// при удалении (ТЗ 3.14.5).
		labels := map[string]string{StackLabel: stack}
		for lk, lv := range n.Labels {
			labels[lk] = lv
		}
		out.Networks = append(out.Networks, NetworkSpec{
			Name:       full,
			Driver:     driver,
			Attachable: n.Attachable,
			Internal:   n.Internal,
			EnableIPv6: n.EnableIPv6 != nil && *n.EnableIPv6,
			Subnet:     firstSubnet(n),
			Options:    optionsMap(n.DriverOpts),
			Labels:     labels,
		})
	}

	// Secrets и configs стека.
	secrets, err := fileObjects(stack, toFileObjects(p.Secrets), env, "secret")
	if err != nil {
		return nil, err
	}
	configs, err := fileObjects(stack, toFileObjectsCfg(p.Configs), env, "config")
	if err != nil {
		return nil, err
	}
	out.Secrets, out.Configs = secrets, configs
	secretName, configName := objectNameMap(secrets), objectNameMap(configs)
	volName := volumeNames(stack, p.Volumes)

	// Сервисы.
	svcKeys := sortedServiceKeys(p.Services)
	for _, key := range svcKeys {
		s := p.Services[key]
		full := stack + "_" + key
		spec, warns, err := toServiceSpec(stack, key, s, p, convertCtx{
			networks: netName,
			secrets:  secretName,
			configs:  configName,
			volumes:  volName,
		})
		if err != nil {
			return nil, fmt.Errorf("сервис %q: %w", key, err)
		}
		out.Warnings = append(out.Warnings, warns...)
		out.Services = append(out.Services, ServiceSpec{Name: full, Spec: spec})
	}
	return out, nil
}

// convertCtx — карты «ключ compose → реальное имя» для одного стека.
type convertCtx struct {
	networks map[string]string
	secrets  map[string]string
	configs  map[string]string
	volumes  map[string]string
}

// toFileObjects приводит секцию secrets к общему типу (SecretConfig —
// именованный алиас FileObjectConfig).
func toFileObjects(in types.Secrets) map[string]types.FileObjectConfig {
	out := make(map[string]types.FileObjectConfig, len(in))
	for k, v := range in {
		out[k] = types.FileObjectConfig(v)
	}
	return out
}

// toFileObjectsCfg — то же для configs.
func toFileObjectsCfg(in types.Configs) map[string]types.FileObjectConfig {
	out := make(map[string]types.FileObjectConfig, len(in))
	for k, v := range in {
		out[k] = types.FileObjectConfig(v)
	}
	return out
}

// firstSubnet берёт первую подсеть из ipam-конфига (панель поддерживает одну).
func firstSubnet(n types.NetworkConfig) string {
	if len(n.Ipam.Config) == 0 {
		return ""
	}
	return n.Ipam.Config[0].Subnet
}

// optionsMap приводит driver_opts к map[string]string.
func optionsMap(o types.Options) map[string]string {
	if len(o) == 0 {
		return nil
	}
	out := make(map[string]string, len(o))
	for k, v := range o {
		out[k] = v
	}
	return out
}

// toServiceSpec конвертирует один сервис compose в swarm.ServiceSpec.
// Возвращает также предупреждения о полях, которые не переносятся (3.14.10).
func toServiceSpec(stack, key string, s types.ServiceConfig, p *types.Project,
	ctx convertCtx) (swarm.ServiceSpec, []Warning, error) {

	full := stack + "_" + key
	warns := serviceWarnings(key, s)

	// Container-level labels: top-level labels + namespace.
	containerLabels := map[string]string{StackLabel: stack}
	for k, v := range s.Labels {
		containerLabels[k] = v
	}

	cs := &swarm.ContainerSpec{
		Image:          s.Image,
		Labels:         containerLabels,
		Env:            envToSlice(s.Environment),
		Hosts:          extraHosts(s.ExtraHosts),
		Hostname:       s.Hostname,
		User:           s.User,
		Dir:            s.WorkingDir,
		Groups:         s.GroupAdd,
		StopSignal:     s.StopSignal,
		TTY:            s.Tty,
		OpenStdin:      s.StdinOpen,
		ReadOnly:       s.ReadOnly,
		Init:           s.Init,
		Sysctls:        mappingToMap(s.Sysctls),
		CapabilityAdd:  s.CapAdd,
		CapabilityDrop: s.CapDrop,
		OomScoreAdj:    s.OomScoreAdj,
		Isolation:      container.Isolation(s.Isolation),
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
	if s.StopGracePeriod != nil {
		d := time.Duration(*s.StopGracePeriod)
		cs.StopGracePeriod = &d
	}
	if dns := dnsConfig(s); dns != nil {
		cs.DNSConfig = dns
	}
	if u := ulimits(s.Ulimits); len(u) > 0 {
		cs.Ulimits = u
	}

	// Тома (3.14.9) — без них сервис теряет данные, поэтому ошибки здесь
	// прерывают конвертацию, а сомнительные конструкции идут в warnings.
	mounts, mountWarns, err := serviceMounts(stack, key, s, p, ctx.volumes)
	if err != nil {
		return swarm.ServiceSpec{}, nil, err
	}
	warns = append(warns, mountWarns...)
	mounts = append(mounts, tmpfsMounts(s.Tmpfs)...)
	cs.Mounts = mounts

	// Secrets / configs.
	if len(s.Secrets) > 0 {
		refs, err := secretRefs(key, s.Secrets, ctx.secrets)
		if err != nil {
			return swarm.ServiceSpec{}, nil, err
		}
		cs.Secrets = refs
	}
	if len(s.Configs) > 0 {
		refs, err := configRefs(key, s.Configs, ctx.configs)
		if err != nil {
			return swarm.ServiceSpec{}, nil, err
		}
		cs.Configs = refs
	}

	// Service-level labels: deploy.labels + namespace.
	serviceLabels := map[string]string{StackLabel: stack}

	spec := swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: full, Labels: serviceLabels},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: cs},
	}
	spec.TaskTemplate.Networks = serviceNetworks(s.Networks, ctx.networks)
	if drv := logDriver(s); drv != nil {
		spec.TaskTemplate.LogDriver = drv
	}
	if pl := platformPlacement(s.Platform); pl != nil {
		spec.TaskTemplate.Placement = pl
	}

	var endpointMode string
	if s.Deploy != nil {
		applyDeploy(&spec, *s.Deploy)
		endpointMode = s.Deploy.EndpointMode
	} else {
		spec.Mode = replicated(1)
	}

	if len(s.Ports) > 0 || endpointMode != "" {
		ep, err := endpointSpec(s.Ports)
		if err != nil {
			return swarm.ServiceSpec{}, nil, err
		}
		ep.Mode = swarm.ResolutionMode(endpointMode)
		spec.EndpointSpec = ep
	}
	return spec, warns, nil
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
		// Отрицательные replicas в файле — опечатка; без проверки приведение
		// к uint64 дало бы кластеру команду поднять ~1.8e19 задач.
		if d.Replicas != nil && *d.Replicas >= 0 {
			n = uint64(*d.Replicas)
		}
		spec.Mode = replicated(n)
	}

	// deploy.labels — это service-level labels.
	for k, v := range d.Labels {
		spec.Labels[k] = v
	}

	// Placement: constraints + preferences + max_replicas_per_node.
	// Platform мог быть заполнен раньше (поле platform сервиса) — не затираем.
	if len(d.Placement.Constraints) > 0 || len(d.Placement.Preferences) > 0 || d.Placement.MaxReplicas > 0 {
		pl := spec.TaskTemplate.Placement
		if pl == nil {
			pl = &swarm.Placement{}
		}
		pl.Constraints = d.Placement.Constraints
		pl.MaxReplicas = d.Placement.MaxReplicas
		for _, p := range d.Placement.Preferences {
			if p.Spread == "" {
				continue
			}
			pl.Preferences = append(pl.Preferences, swarm.PlacementPreference{
				Spread: &swarm.SpreadOver{SpreadDescriptor: p.Spread},
			})
		}
		spec.TaskTemplate.Placement = pl
	}

	// Resources.
	if res := resourceReq(d.Resources); res != nil {
		spec.TaskTemplate.Resources = res
	}

	// RestartPolicy.
	if d.RestartPolicy != nil {
		spec.TaskTemplate.RestartPolicy = restartPolicy(d.RestartPolicy)
	}

	// UpdateConfig и RollbackConfig.
	if d.UpdateConfig != nil {
		spec.UpdateConfig = updateConfig(d.UpdateConfig)
	}
	if d.RollbackConfig != nil {
		spec.RollbackConfig = updateConfig(d.RollbackConfig)
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
	// retries приходит из файла пользователя: мусорное значение не должно
	// стать отрицательным числом попыток после приведения типа.
	if h.Retries != nil && *h.Retries <= math.MaxInt32 {
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

// mappingToMap приводит types.Mapping (sysctls и т.п.) к обычной карте.
func mappingToMap(m types.Mapping) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// dnsConfig собирает DNS-настройки сервиса; nil, если ничего не задано.
func dnsConfig(s types.ServiceConfig) *swarm.DNSConfig {
	if len(s.DNS) == 0 && len(s.DNSSearch) == 0 && len(s.DNSOpts) == 0 {
		return nil
	}
	return &swarm.DNSConfig{
		Nameservers: s.DNS,
		Search:      s.DNSSearch,
		Options:     s.DNSOpts,
	}
}

// ulimits переносит ulimits сервиса. Форма `single` в compose означает,
// что soft и hard равны.
func ulimits(in map[string]*types.UlimitsConfig) []*container.Ulimit {
	if len(in) == 0 {
		return nil
	}
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]*container.Ulimit, 0, len(names))
	for _, n := range names {
		u := in[n]
		if u == nil {
			continue
		}
		soft, hard := int64(u.Soft), int64(u.Hard)
		if u.Single != 0 {
			soft, hard = int64(u.Single), int64(u.Single)
		}
		out = append(out, &container.Ulimit{Name: n, Soft: soft, Hard: hard})
	}
	return out
}

// tmpfsMounts переводит короткую форму `tmpfs: [/path]` в монтирования.
func tmpfsMounts(paths types.StringList) []mount.Mount {
	out := make([]mount.Mount, 0, len(paths))
	for _, p := range paths {
		out = append(out, mount.Mount{Type: mount.TypeTmpfs, Target: p})
	}
	return out
}

// logDriver переносит logging (и устаревшие log_driver/log_opt) в спеку задачи.
func logDriver(s types.ServiceConfig) *swarm.Driver {
	name, opts := s.LogDriver, s.LogOpt
	if s.Logging != nil {
		if s.Logging.Driver != "" {
			name = s.Logging.Driver
		}
		if len(s.Logging.Options) > 0 {
			opts = map[string]string{}
			for k, v := range s.Logging.Options {
				opts[k] = v
			}
		}
	}
	if name == "" && len(opts) == 0 {
		return nil
	}
	return &swarm.Driver{Name: name, Options: opts}
}

// platformPlacement превращает `platform: linux/arm64` в фильтр планировщика.
func platformPlacement(p string) *swarm.Placement {
	if p == "" {
		return nil
	}
	parts := strings.SplitN(p, "/", 2)
	pl := swarm.Platform{OS: parts[0]}
	if len(parts) == 2 {
		pl.Architecture = parts[1]
	}
	return &swarm.Placement{Platforms: []swarm.Platform{pl}}
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
