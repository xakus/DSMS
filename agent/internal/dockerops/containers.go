// Per-container метрики (расширение FR-02): CPU/RAM из cgroups хоста.
//
// Список контейнеров — один вызов Docker API; счётчики CPU/RAM читаются
// напрямую из /host/sys/fs/cgroup (быстро, без нагрузки на daemon).
// Поддерживаются layout'ы: cgroup v2 и v1, драйверы systemd и cgroupfs.
// Если cgroup контейнера не виден (нестандартная раскладка, нет доступа к
// /sys хоста) — те же цифры берутся у Docker (как `docker stats`): медленнее,
// зато без прочерков в UI (см. stats.go).
// CPU % считается как дельта usage между тиками относительно всех ядер.
//
// Для задач Swarm дополнительно собирается потребление по каждой задаче
// (ТЗ 3.4.7): CPU в ядрах, RAM и скорость сети. Сети в cgroups нет — её
// счётчики читаются из /host/proc/<pid>/net/dev (см. net.go).
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

// TaskMetric — потребление одной задачи Swarm (ТЗ 3.4.7, поле `tasks` батча).
// В отличие от ContainerMetric шлётся по ВСЕМ задачам ноды, без отсечки топ-N:
// иначе у «тихих» сервисов на плотной ноде не было бы цифр вообще.
type TaskMetric struct {
	// Task — ID задачи Swarm (label com.docker.swarm.task.id); по нему панель
	// привязывает цифры к задаче и сервису.
	Task string `json:"task"`
	// CPUCores — занято ядер: 0.35 = 35% одного ядра (как в `docker stats`).
	// Не нормируется на число ядер ноды, поэтому значения с разных нод
	// можно складывать.
	CPUCores float64 `json:"cpu_cores"`
	// MemUsed — байты, без неактивного файлового кэша.
	MemUsed uint64 `json:"mem_used"`
	// MemLimit — лимит памяти в байтах; 0 — лимита нет.
	MemLimit uint64 `json:"mem_limit"`
	// RxBps / TxBps — скорость приёма и отдачи, байт/с (сумма по интерфейсам
	// контейнера, кроме loopback). 0 для контейнеров в сети хоста.
	RxBps uint64 `json:"rx_bps"`
	TxBps uint64 `json:"tx_bps"`
}

// Labels, которыми Swarm помечает контейнеры задач.
const (
	labelService = "com.docker.swarm.service.name"
	labelTask    = "com.docker.swarm.task.id"
)

// nsecPerUsec — наносекунд в микросекунде (счётчики CPU v1 и Docker — в нс).
const nsecPerUsec = 1000

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

// candidatePaths — возможные расположения cgroup контейнера по id
// (для v1 — в иерархии контроллера памяти).
func candidatePaths(base, id string) []string {
	scope := "docker-" + id + ".scope"
	return []string{
		filepath.Join(base, "system.slice", scope),           // v2 + systemd
		filepath.Join(base, "docker", id),                    // v2 + cgroupfs
		filepath.Join(base, "memory", "docker", id),          // v1 + cgroupfs
		filepath.Join(base, "memory", "system.slice", scope), // v1 + systemd
	}
}

// findCgroup возвращает существующий cgroup-каталог контейнера или "".
func findCgroup(base, id string) string {
	for _, p := range candidatePaths(base, id) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// cpuacctPaths — где в cgroup v1 лежит счётчик CPU контейнера. Контроллер
// называется cpuacct или «cpu,cpuacct» (зависит от дистрибутива).
func cpuacctPaths(base, id string) []string {
	scope := "docker-" + id + ".scope"
	out := make([]string, 0, 4)
	for _, ctrl := range []string{"cpuacct", "cpu,cpuacct"} {
		out = append(out,
			filepath.Join(base, ctrl, "docker", id, "cpuacct.usage"),
			filepath.Join(base, ctrl, "system.slice", scope, "cpuacct.usage"),
		)
	}
	return out
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
	for _, p := range cpuacctPaths(base, id) {
		if v, ok := readUint(p); ok {
			return v / nsecPerUsec, true
		}
	}
	return 0, false
}

// readStat ищет в файле вида «ключ значение» (memory.stat) число по ключу.
func readStat(path, key string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, key+" "); ok {
			if n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// subCache вычитает из потребления памяти неактивный файловый кэш.
//
// Ядро считает страницы прочитанных файлов в память контейнера, хотя отдаёт
// их по первому требованию. Без вычитания база данных «занимает» гигабайты,
// которых не занимает. Так же считает `docker stats`:
// used = usage − inactive_file. Пример: usage 900 МБ, inactive_file 600 МБ →
// показываем 300 МБ.
func subCache(usage, inactiveFile uint64) uint64 {
	if inactiveFile >= usage {
		return usage // счётчики сняты в разные моменты — не уходим в минус
	}
	return usage - inactiveFile
}

// readMem возвращает used/limit контейнера в байтах.
// used — без неактивного файлового кэша (см. subCache); limit 0 — без лимита.
func readMem(dir string) (used, limit uint64) {
	// v2
	if v, ok := readUint(filepath.Join(dir, "memory.current")); ok {
		inactive, _ := readStat(filepath.Join(dir, "memory.stat"), "inactive_file")
		used = subCache(v, inactive)
		limit, _ = readUint(filepath.Join(dir, "memory.max"))
		return used, limit
	}
	// v1
	if v, ok := readUint(filepath.Join(dir, "memory.usage_in_bytes")); ok {
		// В v1 у иерархии свой ключ: total_inactive_file (с дочерними группами).
		inactive, _ := readStat(filepath.Join(dir, "memory.stat"), "total_inactive_file")
		used = subCache(v, inactive)
		limit, _ = readUint(filepath.Join(dir, "memory.limit_in_bytes"))
		// v1 «безлимит» — гигантское число; нормализуем в 0
		if limit > 1<<60 {
			limit = 0
		}
	}
	return used, limit
}

// Usage — результат одного прохода по контейнерам ноды.
type Usage struct {
	// Top — топ-N контейнеров по CPU для страницы ноды (NFR-2a).
	Top []ContainerMetric
	// Tasks — потребление каждой задачи Swarm на ноде (ТЗ 3.4.7).
	Tasks []TaskMetric
	// Total — сколько контейнеров запущено на ноде всего.
	Total int
}

// ContainerMetrics собирает метрики запущенных контейнеров: топ-N по CPU
// (NFR-2a), потребление по каждой задаче Swarm и полное число контейнеров.
//
// Вызывается из одного потока (цикл сборщика): карты prev* без блокировок.
func (o *Ops) ContainerMetrics(ctx context.Context, topN int) (Usage, error) {
	list, err := o.c.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return Usage{}, err
	}
	base := cgroupBase()
	now := time.Now()
	ncpu := float64(runtime.NumCPU())

	out := make([]ContainerMetric, 0, len(list))
	tasks := make([]TaskMetric, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, c := range list {
		seen[c.ID] = true
		m := ContainerMetric{
			ID:      c.ID[:12],
			Service: c.Labels[labelService],
		}
		if len(c.Names) > 0 {
			m.Name = strings.TrimPrefix(c.Names[0], "/")
		}

		// Показания контейнера: из cgroup, а если его не видно — у Docker.
		smp, ok := o.readSample(ctx, base, c.ID)
		if !ok {
			continue // контейнер исчез между списком и чтением (гонка со стопом)
		}
		m.MemUsed, m.MemLimit = smp.memUsed, smp.memLimit

		// CPU: дельта usage к прошлому тику. cores — сколько ядер занято;
		// CPUPct — то же, но в процентах от всех ядер ноды.
		// Пример: за 3 с контейнер наработал 1.5 с CPU-времени → 0.5 ядра;
		// на 4-ядерной ноде это 12.5%.
		var cores float64
		if smp.cpuOK {
			if prev, exists := o.prevCPU[c.ID]; exists {
				dt := now.Sub(prev.at).Seconds()
				if dt > 0 && smp.cpuUsec >= prev.usageUsec {
					cores = float64(smp.cpuUsec-prev.usageUsec) / 1e6 / dt
					m.CPUPct = float64(int(cores/ncpu*1000+0.5)) / 10 // 1 знак
				}
			}
			o.prevCPU[c.ID] = cpuSample{usageUsec: smp.cpuUsec, at: now}
		}
		out = append(out, m)

		// Задача Swarm — отдельная компактная запись с сетью.
		if taskID := c.Labels[labelTask]; taskID != "" {
			tm := TaskMetric{
				Task:     taskID,
				CPUCores: float64(int(cores*1000+0.5)) / 1000, // 3 знака
				MemUsed:  m.MemUsed,
				MemLimit: m.MemLimit,
			}
			// В сети хоста счётчики контейнера = счётчики всей ноды: не считаем.
			if c.HostConfig.NetworkMode != "host" {
				tm.RxBps, tm.TxBps = o.netRate(ctx, c.ID, now)
			}
			tasks = append(tasks, tm)
		}
	}

	// Чистка prev-счётчиков умерших контейнеров (защита от утечки памяти).
	for id := range o.prevCPU {
		if !seen[id] {
			delete(o.prevCPU, id)
		}
	}
	o.forgetNet(seen)

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
	// Стабильный порядок задач в батче.
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Task < tasks[j].Task })
	return Usage{Top: out, Tasks: tasks, Total: len(list)}, nil
}
