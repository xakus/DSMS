// Конвертация volumes сервиса в Mounts спеки задачи (ТЗ 3.14.9).
//
// Почему это критично: сервис без тома молча теряет данные при перезапуске
// задачи. Поэтому всё, что не удалось перенести один-в-один, обязано попасть
// в предупреждения (3.14.10), а не исчезнуть.
package stackspec

import (
	"fmt"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/mount"
)

// volumeNames строит карту «ключ из секции volumes → реальное имя тома».
// Правило то же, что у `docker stack deploy`: не-external том получает
// префикс стека, external используется как есть. Имя обычно уже проставлено
// загрузчиком compose-go, поэтому оно в приоритете.
func volumeNames(stack string, vols types.Volumes) map[string]string {
	out := map[string]string{}
	for key, v := range vols {
		switch {
		case v.Name != "":
			out[key] = v.Name
		case bool(v.External):
			out[key] = key
		default:
			out[key] = stack + "_" + key
		}
	}
	return out
}

// serviceMounts конвертирует volumes одного сервиса. Возвращает монтирования
// и предупреждения по непереносимым конструкциям.
func serviceMounts(stack, svcName string, s types.ServiceConfig, project *types.Project,
	names map[string]string) ([]mount.Mount, []Warning, error) {

	var mounts []mount.Mount
	var warns []Warning

	for _, v := range s.Volumes {
		m := mount.Mount{
			Target:   v.Target,
			ReadOnly: v.ReadOnly,
		}
		switch v.Type {
		case "bind":
			m.Type = mount.TypeBind
			m.Source = v.Source
			if v.Bind != nil && v.Bind.Propagation != "" {
				m.BindOptions = &mount.BindOptions{Propagation: mount.Propagation(v.Bind.Propagation)}
			}
			// Bind-монтирование привязывает задачу к содержимому конкретной ноды.
			// Для global-сервисов и сервисов с constraints это осознанный выбор
			// (так работает наш собственный агент) — не шумим.
			if !pinnedToNodes(s) {
				warns = append(warns, Warning{Service: svcName, Field: "volumes",
					Message: fmt.Sprintf("bind-монтирование %q существует только на той ноде, где запущена задача — закрепите сервис через deploy.placement.constraints", v.Source)})
			}

		case "tmpfs":
			m.Type = mount.TypeTmpfs
			if v.Tmpfs != nil {
				m.TmpfsOptions = &mount.TmpfsOptions{
					SizeBytes: int64(v.Tmpfs.Size),
					Mode:      fileMode(int64(v.Tmpfs.Mode)),
				}
			}

		case "npipe", "cluster", "image":
			warns = append(warns, Warning{Service: svcName, Field: "volumes",
				Message: fmt.Sprintf("тип тома %q не переносится панелью — монтирование %q пропущено", v.Type, v.Target)})
			continue

		default: // "volume" и пустой тип
			m.Type = mount.TypeVolume
			if v.Source != "" {
				name, ok := names[v.Source]
				if !ok {
					// Ссылка на том, которого нет в секции volumes: docker бы
					// создал анонимный том с этим именем — сохраняем имя как есть.
					name = v.Source
				}
				m.Source = name
			}
			opts, w := volumeOptions(stack, svcName, v, project)
			m.VolumeOptions = opts
			warns = append(warns, w...)
		}

		if m.Target == "" {
			return nil, nil, fmt.Errorf("сервис %q: у монтирования не задан target", svcName)
		}
		mounts = append(mounts, m)
	}
	return mounts, warns, nil
}

// pinnedToNodes: сервис либо global (задача на каждой ноде), либо закреплён
// constraints — тогда bind-монтирование хоста осознанно, а не случайность.
func pinnedToNodes(s types.ServiceConfig) bool {
	if s.Deploy == nil {
		return false
	}
	return strings.EqualFold(s.Deploy.Mode, "global") || len(s.Deploy.Placement.Constraints) > 0
}

// volumeOptions переносит настройки именованного тома (драйвер, opts, labels).
// Том создаётся Docker'ом при первом запуске задачи — панель заранее его не
// создаёт, поэтому драйвер обязан уехать в спеку, иначе он потеряется.
func volumeOptions(stack, svcName string, v types.ServiceVolumeConfig,
	project *types.Project) (*mount.VolumeOptions, []Warning) {

	var warns []Warning
	opts := &mount.VolumeOptions{}
	used := false

	if v.Volume != nil {
		if v.Volume.NoCopy {
			opts.NoCopy = true
			used = true
		}
		if v.Volume.Subpath != "" {
			opts.Subpath = v.Volume.Subpath
			used = true
		}
	}

	if v.Source != "" && project != nil {
		if cfg, ok := project.Volumes[v.Source]; ok && !bool(cfg.External) {
			// Label стека на томе — чтобы том опознавался как часть стека.
			labels := map[string]string{StackLabel: stack}
			for k, val := range cfg.Labels {
				labels[k] = val
			}
			opts.Labels = labels
			used = true
			if cfg.Driver != "" || len(cfg.DriverOpts) > 0 {
				drv := &mount.Driver{Name: cfg.Driver, Options: map[string]string{}}
				for k, val := range cfg.DriverOpts {
					drv.Options[k] = fmt.Sprint(val)
				}
				opts.DriverConfig = drv
			}
		}
	}

	if !used {
		return nil, warns
	}
	return opts, warns
}
