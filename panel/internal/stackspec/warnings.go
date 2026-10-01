// Предупреждения конвертера (ТЗ 3.14.10): какие поля compose-файла панель
// НЕ переносит в Swarm-спеку. Молчаливое игнорирование недопустимо —
// пользователь должен увидеть потерю до деплоя, а не искать её в рантайме.
package stackspec

import (
	"github.com/compose-spec/compose-go/v2/types"
)

// Warning — одно предупреждение о поле, которое не попало в спеку.
// Service пустой, если предупреждение относится ко всему файлу.
type Warning struct {
	Service string `json:"service,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// unsupported описывает правило: поле → причина, почему оно не переносится.
type unsupported struct {
	field  string
	reason string
	// present сообщает, задано ли поле в сервисе.
	present func(s types.ServiceConfig) bool
}

// swarmUnsupported — поля compose, не имеющие эквивалента в swarm.ServiceSpec.
// Список сознательно подробный: каждое такое поле в реальном файле — это
// либо ошибка ожиданий, либо потерянная настройка.
var swarmUnsupported = []unsupported{
	{"build", "образ нужно собрать заранее и указать в image — Swarm не собирает образы",
		func(s types.ServiceConfig) bool { return s.Build != nil }},
	{"depends_on", "Swarm не гарантирует порядок запуска; используйте healthcheck и перезапуск задачи",
		func(s types.ServiceConfig) bool { return len(s.DependsOn) > 0 }},
	{"network_mode", "в Swarm-стеке доступны только сети из секции networks",
		func(s types.ServiceConfig) bool { return s.NetworkMode != "" }},
	{"container_name", "имя контейнера в Swarm назначается автоматически (<стек>_<сервис>.N)",
		func(s types.ServiceConfig) bool { return s.ContainerName != "" }},
	{"restart", "в стеке действует deploy.restart_policy, а не restart",
		func(s types.ServiceConfig) bool { return s.Restart != "" }},
	{"profiles", "профили compose не применяются при деплое стека",
		func(s types.ServiceConfig) bool { return len(s.Profiles) > 0 }},
	{"links", "устаревшая опция; сервисы находят друг друга по DNS-имени в overlay-сети",
		func(s types.ServiceConfig) bool { return len(s.Links) > 0 }},
	{"external_links", "не поддерживается в Swarm",
		func(s types.ServiceConfig) bool { return len(s.ExternalLinks) > 0 }},
	{"volumes_from", "не поддерживается в Swarm; смонтируйте том явно через volumes",
		func(s types.ServiceConfig) bool { return len(s.VolumesFrom) > 0 }},
	{"devices", "проброс устройств не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return len(s.Devices) > 0 }},
	{"privileged", "привилегированный режим не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return s.Privileged }},
	{"cgroup_parent", "не поддерживается в Swarm",
		func(s types.ServiceConfig) bool { return s.CgroupParent != "" }},
	{"pid", "не поддерживается в Swarm",
		func(s types.ServiceConfig) bool { return s.Pid != "" }},
	{"ipc", "не поддерживается в Swarm",
		func(s types.ServiceConfig) bool { return s.Ipc != "" }},
	{"uts", "не поддерживается в Swarm",
		func(s types.ServiceConfig) bool { return s.Uts != "" }},
	{"userns_mode", "не поддерживается в Swarm",
		func(s types.ServiceConfig) bool { return s.UserNSMode != "" }},
	{"security_opt", "не переносится панелью; используйте deploy и privileges кластера",
		func(s types.ServiceConfig) bool { return len(s.SecurityOpt) > 0 }},
	{"shm_size", "не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return s.ShmSize > 0 }},
	{"mac_address", "не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return s.MacAddress != "" }},
	{"domainname", "не поддерживается в Swarm; задайте hostname",
		func(s types.ServiceConfig) bool { return s.DomainName != "" }},
	{"expose", "информационное поле; в Swarm порты публикуются через ports",
		func(s types.ServiceConfig) bool { return len(s.Expose) > 0 }},
	{"scale", "в стеке число реплик задаётся через deploy.replicas",
		func(s types.ServiceConfig) bool { return s.Scale != nil }},
	{"pull_policy", "Swarm тянет образ при создании задачи; политика не переносится",
		func(s types.ServiceConfig) bool { return s.PullPolicy != "" }},
	{"runtime", "не переносится панелью",
		func(s types.ServiceConfig) bool { return s.Runtime != "" }},
	{"storage_opt", "не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return len(s.StorageOpt) > 0 }},
	{"volume_driver", "устаревшая опция; задайте драйвер в секции volumes",
		func(s types.ServiceConfig) bool { return s.VolumeDriver != "" }},
	{"blkio_config", "не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return s.BlkioConfig != nil }},
	{"device_cgroup_rules", "не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return len(s.DeviceCgroupRules) > 0 }},
	{"gpus", "проброс GPU не переносится панелью; используйте deploy.resources.reservations",
		func(s types.ServiceConfig) bool { return len(s.Gpus) > 0 }},
	{"credential_spec", "не переносится панелью",
		func(s types.ServiceConfig) bool { return s.CredentialSpec != nil }},
	{"develop", "режим разработки compose не применяется к стеку",
		func(s types.ServiceConfig) bool { return s.Develop != nil }},
}

// legacyLimits — «плоские» лимиты уровня контейнера: в стеке их место в
// deploy.resources, иначе они просто теряются.
var legacyLimits = []unsupported{
	{"mem_limit", "в стеке лимит памяти задаётся через deploy.resources.limits.memory",
		func(s types.ServiceConfig) bool { return s.MemLimit > 0 }},
	{"mem_reservation", "в стеке резерв памяти задаётся через deploy.resources.reservations.memory",
		func(s types.ServiceConfig) bool { return s.MemReservation > 0 }},
	{"cpus", "в стеке лимит CPU задаётся через deploy.resources.limits.cpus",
		func(s types.ServiceConfig) bool { return s.CPUS > 0 }},
	{"cpu_shares", "не переносится в Swarm; используйте deploy.resources",
		func(s types.ServiceConfig) bool { return s.CPUShares > 0 }},
	{"cpuset", "не переносится в Swarm; используйте deploy.placement.constraints",
		func(s types.ServiceConfig) bool { return s.CPUSet != "" }},
	{"pids_limit", "не поддерживается в Swarm-сервисах",
		func(s types.ServiceConfig) bool { return s.PidsLimit > 0 }},
}

// serviceWarnings собирает предупреждения по одному сервису.
func serviceWarnings(name string, s types.ServiceConfig) []Warning {
	var out []Warning
	add := func(rules []unsupported) {
		for _, r := range rules {
			if r.present(s) {
				out = append(out, Warning{Service: name, Field: r.field, Message: r.reason})
			}
		}
	}
	add(swarmUnsupported)
	add(legacyLimits)
	return out
}
