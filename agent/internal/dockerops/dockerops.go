// Package dockerops — локальные Docker-операции агента (FR-10, FR-11).
//
// Тома и disk usage локальны для каждого Engine, поэтому агент (он есть
// на каждой ноде) выполняет их по командам панели через свой docker.sock.
package dockerops

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Ops — обёртка docker client для локальных операций ноды.
type Ops struct {
	c *client.Client
}

// New создаёт клиента к локальному docker.sock.
// Версия API согласуется автоматически (тот же принцип, что в panel).
func New() (*Ops, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Ops{c: c}, nil
}

// Close освобождает ресурсы клиента.
func (o *Ops) Close() error { return o.c.Close() }

// DF — данные `docker system df` для ответа панели (3.11.1).
type DF struct {
	Images      DFSection `json:"images"`
	Containers  DFSection `json:"containers"`
	Volumes     DFSection `json:"volumes"`
	BuildCache  DFSection `json:"build_cache"`
}

// DFSection — одна категория disk usage.
type DFSection struct {
	Count       int   `json:"count"`
	Size        int64 `json:"size"`
	Reclaimable int64 `json:"reclaimable"`
}

// DiskUsage собирает локальный system df.
func (o *Ops) DiskUsage(ctx context.Context) (*DF, error) {
	du, err := o.c.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return nil, err
	}
	out := &DF{}
	for _, img := range du.Images {
		out.Images.Count++
		out.Images.Size += img.Size
		// Reclaimable: образ не используется контейнерами.
		if img.Containers == 0 {
			out.Images.Reclaimable += img.Size
		}
	}
	for _, c := range du.Containers {
		out.Containers.Count++
		out.Containers.Size += c.SizeRw
		if c.State != "running" {
			out.Containers.Reclaimable += c.SizeRw
		}
	}
	for _, v := range du.Volumes {
		out.Volumes.Count++
		if v.UsageData != nil {
			out.Volumes.Size += v.UsageData.Size
			if v.UsageData.RefCount == 0 {
				out.Volumes.Reclaimable += v.UsageData.Size
			}
		}
	}
	for _, b := range du.BuildCache {
		out.BuildCache.Count++
		out.BuildCache.Size += b.Size
		if !b.InUse {
			out.BuildCache.Reclaimable += b.Size
		}
	}
	return out, nil
}

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

// Prune выполняет очистку по списку целей (3.11.2).
// Только неиспользуемое: dangling-образы (allImages — все без контейнеров),
// остановленные контейнеры, неиспользуемые тома, build cache.
func (o *Ops) Prune(ctx context.Context, targets []string, allImages bool) []PruneResult {
	out := make([]PruneResult, 0, len(targets))
	for _, target := range targets {
		res := PruneResult{Target: target}
		switch target {
		case "images":
			f := filters.NewArgs(filters.Arg("dangling", "true"))
			if allImages {
				f = filters.NewArgs(filters.Arg("dangling", "false"))
			}
			if rep, err := o.c.ImagesPrune(ctx, f); err != nil {
				res.Err = err.Error()
			} else {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		case "containers":
			if rep, err := o.c.ContainersPrune(ctx, filters.NewArgs()); err != nil {
				res.Err = err.Error()
			} else {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		case "volumes":
			if rep, err := o.c.VolumesPrune(ctx, filters.NewArgs()); err != nil {
				res.Err = err.Error()
			} else {
				res.SpaceReclaimed = rep.SpaceReclaimed
			}
		case "build-cache":
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

// Containers возвращает число запущенных контейнеров (для sys.containers батча).
func (o *Ops) Containers(ctx context.Context) (int, error) {
	list, err := o.c.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return 0, err
	}
	return len(list), nil
}
