// Per-container метрики (расширение FR-02): CPU/RAM из cgroups хоста.
//
// Список контейнеров — один вызов Docker API; счётчики CPU/RAM читаются
// напрямую из /host/sys/fs/cgroup (быстро, без нагрузки на daemon).
// Поддерживаются layout'ы: cgroup v2 (systemd и cgroupfs) и v1.
// CPU % считается как дельта usage между тиками относительно всех ядер.
package dockerops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
)

// ContainerMetric — метрики одного контейнера (формат батча, разд. 4.3).
type ContainerMetric struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Service  string  `json:"service"` // из label com.docker.swarm.service.name
	CPUPct   float64 `json:"cpu_pct"`
	MemUsed  uint64  `json:"mem_used"`
	MemLimit uint64  `json:"mem_limit"`
}

// cpuSample — предыдущее показание CPU-счётчика контейнера.
type cpuSample struct {
	usageUsec uint64
	at        time.Time
}

// cgroupBase возвращает корень cgroup-иерархии хоста.
func cgroupBase() string {
	if hs := os.Getenv("HOST_SYS"); hs != "" {
		return filepath.Join(hs, "fs/cgroup")
	}
	return "/sys/fs/cgroup"
}

// candidatePaths — возможные расположения cgroup контейнера по id.
func candidatePaths(base, id string) []string {
	return []string{
		filepath.Join(base, "system.slice", "docker-"+id+".scope"), // v2 + systemd
		filepath.Join(base, "docker", id),                          // v2 + cgroupfs
		filepath.Join(base, "memory", "docker", id),                // v1 (memory ctrl)
	}
}

// readUint читает число из файла ("max" → 0 = безлимит).
func readUint(path string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return 0, true
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// readCPUUsec возвращает потреблённое CPU-время контейнера в микросекундах.
func readCPUUsec(base, dir, id string) (uint64, bool) {
	// v2: cpu.stat → строка "usage_usec N"
	if b, err := os.ReadFile(filepath.Join(dir, "cpu.stat")); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "usage_usec "); ok {
				if n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64); err == nil {
					return n, true
				}
			}
		}
	}
	// v1: cpuacct.usage (наносекунды) в отдельной иерархии cpuacct
	if v, ok := readUint(filepath.Join(base, "cpuacct", "docker", id, "cpuacct.usage")); ok {
		return v / 1000, true
	}
	return 0, false
}

// readMem возвращает used/limit контейнера в байтах.
func readMem(dir string) (used, limit uint64) {
	// v2
	if v, ok := readUint(filepath.Join(dir, "memory.current")); ok {
		used = v
		limit, _ = readUint(filepath.Join(dir, "memory.max"))
		return used, limit
	}
	// v1
	if v, ok := readUint(filepath.Join(dir, "memory.usage_in_bytes")); ok {
		used = v
		limit, _ = readUint(filepath.Join(dir, "memory.limit_in_bytes"))
		// v1 «безлимит» — гигантское число; нормализуем в 0
		if limit > 1<<60 {
			limit = 0
		}
	}
	return used, limit
}

// ContainerMetrics собирает метрики запущенных контейнеров,
// возвращая топ-N по CPU (NFR-2a) и полное число контейнеров ноды.
func (o *Ops) ContainerMetrics(ctx context.Context, topN int) ([]ContainerMetric, int, error) {
	list, err := o.c.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, 0, err
	}
	base := cgroupBase()
	now := time.Now()
	ncpu := float64(runtime.NumCPU())

	out := make([]ContainerMetric, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, c := range list {
		m := ContainerMetric{
			ID:      c.ID[:12],
			Service: c.Labels["com.docker.swarm.service.name"],
		}
		if len(c.Names) > 0 {
			m.Name = strings.TrimPrefix(c.Names[0], "/")
		}

		// Найти живой cgroup-каталог контейнера.
		var dir string
		for _, p := range candidatePaths(base, c.ID) {
			if _, err := os.Stat(p); err == nil {
				dir = p
				break
			}
		}
		if dir == "" {
			continue // контейнер без видимого cgroup (гонка со стартом/стопом)
		}

		m.MemUsed, m.MemLimit = readMem(dir)

		// CPU %: дельта usage к прошлому тику, нормированная на все ядра.
		if usec, ok := readCPUUsec(base, dir, c.ID); ok {
			seen[c.ID] = true
			if prev, exists := o.prevCPU[c.ID]; exists {
				dt := now.Sub(prev.at).Seconds()
				if dt > 0 && usec >= prev.usageUsec {
					m.CPUPct = float64(usec-prev.usageUsec) / 1e6 / dt / ncpu * 100
					m.CPUPct = float64(int(m.CPUPct*10+0.5)) / 10 // 1 знак
				}
			}
			o.prevCPU[c.ID] = cpuSample{usageUsec: usec, at: now}
		}
		out = append(out, m)
	}

	// Чистка prev-счётчиков умерших контейнеров (защита от утечки памяти).
	for id := range o.prevCPU {
		if !seen[id] {
			delete(o.prevCPU, id)
		}
	}

	// Топ-N по CPU, при равенстве — по RAM (NFR-2a).
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPUPct != out[j].CPUPct {
			return out[i].CPUPct > out[j].CPUPct
		}
		return out[i].MemUsed > out[j].MemUsed
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out, len(list), nil
}
