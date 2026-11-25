// Package dockerops — локальные Docker-операции агента (FR-10, FR-11).
//
// Тома и disk usage локальны для каждого Engine, поэтому агент (он есть
// на каждой ноде) выполняет их по командам панели через свой docker.sock.
package dockerops

import (
	"context"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Ops — обёртка docker client для локальных операций ноды.
type Ops struct {
	c *client.Client
	// prevCPU — предыдущие CPU-счётчики контейнеров (для расчёта дельт).
	prevCPU map[string]cpuSample
	// prevNet — предыдущие сетевые счётчики контейнеров задач Swarm.
	prevNet map[string]netSample
	// pids — PID главного процесса контейнера на хосте (ключ — ID контейнера).
	pids map[string]int
	// memLimits — лимит памяти контейнера из его настроек, байты (0 — нет).
	memLimits map[string]uint64
	// warnedNoCgroup — предупреждение «cgroup не виден» уже записано в журнал.
	warnedNoCgroup bool

	// layerMu защищает layerCache: df запрашивают HTTP-обработчики, их
	// может быть несколько одновременно.
	layerMu sync.Mutex
	// layerCache — слои образов по ID образа. Образ неизменяем, поэтому
	// запись верна, пока образ существует (см. layersOf).
	layerCache map[string]imageLayers
}

// New создаёт клиента к локальному docker.sock.
// Версия API согласуется автоматически (тот же принцип, что в panel).
func New() (*Ops, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Ops{
		c:          c,
		prevCPU:    make(map[string]cpuSample),
		prevNet:    make(map[string]netSample),
		pids:       make(map[string]int),
		memLimits:  make(map[string]uint64),
		layerCache: make(map[string]imageLayers),
	}, nil
}

// Close освобождает ресурсы клиента.
func (o *Ops) Close() error { return o.c.Close() }

// Volume — локальный том в ответе панели (3.10.4).
type Volume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	Size       int64  `json:"size"`   // -1 если неизвестен
	InUse      bool   `json:"in_use"` // RefCount > 0
}

// Volumes возвращает локальные тома с признаком использования.
func (o *Ops) Volumes(ctx context.Context) ([]Volume, error) {
	// DiskUsage даёт UsageData (size, refcount) — полнее, чем VolumeList.
	du, err := o.c.DiskUsage(ctx, types.DiskUsageOptions{Types: []types.DiskUsageObject{types.VolumeObject}})
	if err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(du.Volumes))
	for _, v := range du.Volumes {
		item := Volume{Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, Size: -1}
		if v.UsageData != nil {
			item.Size = v.UsageData.Size
			item.InUse = v.UsageData.RefCount > 0
		}
		out = append(out, item)
	}
	return out, nil
}

// VolumeRemove удаляет локальный том (3.10.5).
func (o *Ops) VolumeRemove(ctx context.Context, name string, force bool) error {
	return o.c.VolumeRemove(ctx, name, force)
}

// PruneResult — итог очистки: сколько байт освобождено (3.11.3).
type PruneResult struct {
	Target         string `json:"target"`
	SpaceReclaimed uint64 `json:"space_reclaimed"`
	Err            string `json:"err,omitempty"`
}

// orderTargets расставляет цели в порядке PruneOrder, в каком бы порядке их
// ни прислали: оценка полной очистки (DF.Total) верна только для него.
// Неизвестные цели идут в конец — Prune вернёт по ним ошибку.
func orderTargets(targets []string) []string {
	out := make([]string, 0, len(targets))
	known := make(map[string]bool, len(PruneOrder))
	for _, want := range PruneOrder {
		known[want] = true
		for _, t := range targets {
			if t == want {
				out = append(out, t)
				break // повтор цели в запросе выполняется один раз
			}
		}
	}
	for _, t := range targets {
		if !known[t] {
			out = append(out, t)
		}
	}
	return out
}

// Prune выполняет очистку по списку целей (3.11.2).
// Только неиспользуемое — и ровно то, что обещает оценка DiskUsage (3.11.5):
//   - images: образы без контейнеров и без тега (allImages — все без контейнеров);
//   - containers: остановленные контейнеры;
//   - volumes: неиспользуемые анонимные тома. Именованные не трогаем: у
//     сервиса, остановленного через scale 0, контейнеров нет, и его том с
//     данными выглядел бы «неиспользуемым»;
//   - build-cache: кэш сборки, который сейчас не используется.
func (o *Ops) Prune(ctx context.Context, targets []string, allImages bool) []PruneResult {
	out := make([]PruneResult, 0, len(targets))
	for _, target := range orderTargets(targets) {
		res := PruneResult{Target: target}
		switch target {
		case TargetImages:
			f := filters.NewArgs(filters.Arg("dangling", "true"))
			if allImages {
				f = filters.NewArgs(filters.Arg("dangling", "false"))
			}
			if rep, err := o.c.ImagesPrune(ctx, f); err != nil {
				res.Err = err.Error()
			} else {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		case TargetContainers:
			if rep, err := o.c.ContainersPrune(ctx, filters.NewArgs()); err != nil {
				res.Err = err.Error()
			} else {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		case TargetVolumes:
			if rep, err := o.c.VolumesPrune(ctx, filters.NewArgs()); err != nil {
				res.Err = err.Error()
			} else {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		case TargetBuildCache:
			if rep, err := o.c.BuildCachePrune(ctx, build.CachePruneOptions{}); err != nil {
				res.Err = err.Error()
			} else if rep != nil {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		default:
			res.Err = "unknown target"
		}
		out = append(out, res)
	}
	return out
}
