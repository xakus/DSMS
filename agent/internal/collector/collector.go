// Package collector — сбор метрик хоста через gopsutil (разд. 2.3 ТЗ).
//
// Хост-ресурсы смонтированы read-only (/host/proc, /host/sys, /host/rootfs);
// gopsutil направляется туда переменными HOST_PROC / HOST_SYS из stack file.
// Скорости (bps, iops, pps) считаются как дельта между соседними тиками.
package collector

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"github.com/xakus/DSMS/agent/internal/config"
	"github.com/xakus/DSMS/agent/internal/dockerops"
)

// dockerTimeout — сколько ждём Docker при сборе метрик контейнеров.
// Docker под нагрузкой (сборка образа, массовый перезапуск) может отвечать
// секундами; без предела зависший запрос остановил бы весь цикл сбора, и
// нода пропала бы с дашборда целиком.
const dockerTimeout = 10 * time.Second

// Collector собирает снапшоты; хранит предыдущие счётчики для расчёта дельт.
type Collector struct {
	cfg      *config.Config
	ops      *dockerops.Ops // nil — docker.sock не смонтирован
	hostname string
	// dockerFailing — прошлый опрос Docker не удался (чтобы писать в журнал
	// один раз на серию сбоев, а не на каждый тик).
	dockerFailing bool

	prevTime time.Time                      // время предыдущего тика
	prevDisk map[string]disk.IOCountersStat // имя устройства → счётчики
	prevNet  map[string]gnet.IOCountersStat // интерфейс → счётчики
}

// New создаёт Collector; hostname читается один раз при старте.
func New(cfg *config.Config, ops *dockerops.Ops) *Collector {
	hn, _ := os.Hostname()
	return &Collector{cfg: cfg, ops: ops, hostname: hn}
}

// Collect собирает полный снапшот метрик ноды.
func (c *Collector) Collect(ctx context.Context) (Snapshot, error) {
	now := time.Now()
	snap := Snapshot{
		NodeID:   c.cfg.NodeID,
		Hostname: c.hostname,
		TS:       now.Unix(),
	}

	// --- CPU: суммарная, по ядрам, load average ---
	if total, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(total) > 0 {
		snap.CPU.TotalPct = round1(total[0])
	}
	if cores, err := cpu.PercentWithContext(ctx, 0, true); err == nil {
		snap.CPU.PerCore = make([]float64, len(cores))
		for i, v := range cores {
			snap.CPU.PerCore[i] = round1(v)
		}
	}
	if avg, err := load.AvgWithContext(ctx); err == nil {
		snap.CPU.Load = []float64{avg.Load1, avg.Load5, avg.Load15}
	}

	// --- RAM ---
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		snap.Mem = Mem{
			Total:     vm.Total,
			Used:      vm.Used,
			Available: vm.Available,
			Cached:    vm.Cached,
		}
	}

	// --- Диски: место по точкам монтирования + скорость I/O (дельта) ---
	snap.Disk = c.collectDisk(ctx, now)

	// --- Сеть: скорости по интерфейсам (дельта) ---
	snap.Net = c.collectNet(ctx, now)

	// --- Система ---
	if up, err := host.UptimeWithContext(ctx); err == nil {
		snap.Sys.Uptime = up
	}
	// --- Контейнеры: per-container CPU/RAM из cgroups, топ-N (FR-02, NFR-2a),
	//     плюс потребление по задачам Swarm с сетью (FR-04 3.4.7) ---
	c.collectContainers(ctx, &snap)

	c.prevTime = now
	return snap, nil
}

// collectContainers дописывает в снапшот метрики контейнеров и задач Swarm.
// Итог опроса Docker уходит панели в TasksState: по нему она отличает
// «на ноде нет задач» от «агент не смог спросить Docker» и во втором случае
// не затирает прошлые цифры (ТЗ 3.4.7).
func (c *Collector) collectContainers(ctx context.Context, snap *Snapshot) {
	if c.ops == nil {
		snap.TasksState = TasksFailed
		return
	}
	dctx, cancel := context.WithTimeout(ctx, dockerTimeout)
	defer cancel()
	u, err := c.ops.ContainerMetrics(dctx, c.cfg.TopContainers)
	if err != nil {
		snap.TasksState = TasksFailed
		if !c.dockerFailing {
			slog.Warn("container metrics failed, task usage is not sent", "err", err)
		}
		c.dockerFailing = true
		return
	}
	if c.dockerFailing {
		slog.Info("container metrics recovered")
	}
	c.dockerFailing = false

	snap.TasksState = TasksOK
	snap.Sys.Containers = u.Total
	snap.Containers = make([]Container, 0, len(u.Top))
	for _, m := range u.Top {
		snap.Containers = append(snap.Containers, Container{
			ID: m.ID, Name: m.Name, Service: m.Service,
			CPUPct: m.CPUPct, MemUsed: m.MemUsed, MemLimit: m.MemLimit,
		})
	}
	// Потребление каждой задачи Swarm — без отсечки топ-N (ТЗ 3.4.7).
	snap.Tasks = make([]Task, 0, len(u.Tasks))
	for _, m := range u.Tasks {
		snap.Tasks = append(snap.Tasks, Task{
			Task: m.Task, CPUCores: m.CPUCores,
			MemUsed: m.MemUsed, MemLimit: m.MemLimit,
			RxBps: m.RxBps, TxBps: m.TxBps,
		})
	}
}

// collectDisk возвращает место и скорости I/O по точкам монтирования.
func (c *Collector) collectDisk(ctx context.Context, now time.Time) []Disk {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil
	}
	io, _ := disk.IOCountersWithContext(ctx)
	dt := now.Sub(c.prevTime).Seconds()

	out := make([]Disk, 0, len(parts))
	for _, p := range parts {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		d := Disk{Mount: p.Mountpoint, Total: usage.Total, Used: usage.Used}
		// Скорости считаем только со второго тика (есть предыдущие счётчики).
		if cur, ok := findIO(io, p.Device); ok && c.prevDisk != nil && dt > 0 {
			if prev, ok := c.prevDisk[cur.Name]; ok {
				d.ReadBps = perSec(cur.ReadBytes, prev.ReadBytes, dt)
				d.WriteBps = perSec(cur.WriteBytes, prev.WriteBytes, dt)
				d.ReadIOPS = perSec(cur.ReadCount, prev.ReadCount, dt)
				d.WriteIOPS = perSec(cur.WriteCount, prev.WriteCount, dt)
			}
		}
		out = append(out, d)
	}
	c.prevDisk = io
	return out
}

// collectNet возвращает скорости rx/tx по интерфейсам.
func (c *Collector) collectNet(ctx context.Context, now time.Time) []Net {
	// Счётчики сетевого пространства НОДЫ, а не контейнера агента
	// (см. netifaces.go): gnet.IOCounters прочитал бы $HOST_PROC/net/dev,
	// а это ссылка на self — интерфейсы самого агента.
	counters, err := gnet.IOCountersByFileWithContext(ctx, true, hostNetDevPath())
	if err != nil {
		return nil
	}
	dt := now.Sub(c.prevTime).Seconds()

	names := make([]string, 0, len(counters))
	for _, s := range counters {
		names = append(names, s.Name)
	}
	uplinks := uplinkIfaces(names, hostSysBase())

	cur := make(map[string]gnet.IOCountersStat, len(counters))
	out := make([]Net, 0, len(uplinks))
	for _, s := range counters {
		cur[s.Name] = s
		if !uplinks[s.Name] {
			continue // loopback, мосты и veth — не трафик ноды
		}
		n := Net{Iface: s.Name, Errors: s.Errin + s.Errout, Drops: s.Dropin + s.Dropout}
		if c.prevNet != nil && dt > 0 {
			if prev, ok := c.prevNet[s.Name]; ok {
				n.RxBps = perSec(s.BytesRecv, prev.BytesRecv, dt)
				n.TxBps = perSec(s.BytesSent, prev.BytesSent, dt)
				n.RxPps = perSec(s.PacketsRecv, prev.PacketsRecv, dt)
				n.TxPps = perSec(s.PacketsSent, prev.PacketsSent, dt)
			}
		}
		out = append(out, n)
	}
	// Стабильный порядок интерфейсов в батче.
	sort.Slice(out, func(i, j int) bool { return out[i].Iface < out[j].Iface })
	c.prevNet = cur
	return out
}

// findIO ищет счётчики устройства по суффиксу имени (например /dev/sda1 → sda1).
func findIO(io map[string]disk.IOCountersStat, device string) (disk.IOCountersStat, bool) {
	for name, s := range io {
		if len(device) >= len(name) && device[len(device)-len(name):] == name {
			return s, true
		}
	}
	return disk.IOCountersStat{}, false
}

// perSec переводит дельту счётчика в скорость «в секунду»,
// защищаясь от сброса счётчика (cur < prev → 0).
func perSec(cur, prev uint64, dt float64) uint64 {
	if cur < prev || dt <= 0 {
		return 0
	}
	return uint64(float64(cur-prev) / dt)
}

// round1 округляет до одного знака после запятой (формат разд. 4.3).
func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
