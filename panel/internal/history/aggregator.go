// Package history — минутные агрегаты метрик в SQLite (разд. 2.2, 5 ТЗ):
// раз в 60с сворачивает live-буфер в metrics_1m, ретенция 7 дней,
// VACUUM по расписанию (разд. 10 ТЗ).
package history

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
)

// Интервалы обслуживания.
const (
	aggregateEvery = time.Minute
	vacuumEvery    = 24 * time.Hour

	// SettingRetentionDays — ключ настройки ретенции (Settings, экран 12).
	SettingRetentionDays = "metrics.retention_days"
	defaultRetentionDays = 7 // ретенция metrics_1m по умолчанию (разд. 5 ТЗ)
)

// DiskAgg / NetAgg — агрегированные скорости за минуту (в *_json колонки).
type DiskAgg struct {
	ReadBps  uint64 `json:"read_bps"`
	WriteBps uint64 `json:"write_bps"`
}

// NetAgg — суммарные rx/tx по интерфейсам.
type NetAgg struct {
	RxBps uint64 `json:"rx_bps"`
	TxBps uint64 `json:"tx_bps"`
}

// Aggregator сворачивает буфер в минутные точки.
type Aggregator struct {
	store  *store.Store
	buffer *metrics.ClusterBuffer
}

// New создаёт агрегатор.
func New(st *store.Store, buf *metrics.ClusterBuffer) *Aggregator {
	return &Aggregator{store: st, buffer: buf}
}

// retentionDays возвращает срок хранения истории из настроек (1..90, дефолт 7).
func (a *Aggregator) retentionDays() int {
	if v, _ := a.store.GetSetting(SettingRetentionDays); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d >= 1 && d <= 90 {
			return d
		}
	}
	return defaultRetentionDays
}

// Run — цикл агрегации/ретенции/VACUUM; блокирует до отмены ctx.
func (a *Aggregator) Run(ctx context.Context) {
	agg := time.NewTicker(aggregateEvery)
	vac := time.NewTicker(vacuumEvery)
	defer agg.Stop()
	defer vac.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-agg.C:
			a.aggregate()
			// Ретенция настраивается из UI (Settings) без рестарта.
			cutoff := time.Now().AddDate(0, 0, -a.retentionDays()).Unix()
			if err := a.store.CleanupMetrics(cutoff); err != nil {
				slog.Warn("metrics cleanup failed", "err", err)
			}
		case <-vac.C:
			if err := a.store.Vacuum(); err != nil {
				slog.Warn("vacuum failed", "err", err)
			}
		}
	}
}

// aggregate сворачивает последнюю минуту каждой ноды в одну точку.
func (a *Aggregator) aggregate() {
	cutoff := time.Now().Add(-aggregateEvery).Unix()
	minuteTS := time.Now().Truncate(time.Minute).Unix()

	for nodeID := range a.buffer.Latests() {
		var (
			n        int
			cpuSum   float64
			memUsed  uint64
			memTotal uint64
			disk     DiskAgg
			net      NetAgg
		)
		for _, s := range a.buffer.Window(nodeID) {
			if s.TS < cutoff {
				continue // старше минуты — уже агрегировано
			}
			n++
			cpuSum += s.CPU.TotalPct
			memUsed = s.Mem.Used // последняя точка минуты
			memTotal = s.Mem.Total
			var d DiskAgg
			for _, x := range s.Disk {
				d.ReadBps += x.ReadBps
				d.WriteBps += x.WriteBps
			}
			var nt NetAgg
			for _, x := range s.Net {
				nt.RxBps += x.RxBps
				nt.TxBps += x.TxBps
			}
			// средние скорости минуты — накапливаем и усредним ниже
			disk.ReadBps += d.ReadBps
			disk.WriteBps += d.WriteBps
			net.RxBps += nt.RxBps
			net.TxBps += nt.TxBps
		}
		if n == 0 {
			continue
		}
		disk.ReadBps /= uint64(n)
		disk.WriteBps /= uint64(n)
		net.RxBps /= uint64(n)
		net.TxBps /= uint64(n)

		diskJSON, _ := json.Marshal(disk)
		netJSON, _ := json.Marshal(net)
		if err := a.store.InsertMetrics1m(nodeID, minuteTS, cpuSum/float64(n),
			memUsed, memTotal, string(diskJSON), string(netJSON)); err != nil {
			slog.Warn("metrics aggregate insert failed", "node", nodeID, "err", err)
		}
	}
}
