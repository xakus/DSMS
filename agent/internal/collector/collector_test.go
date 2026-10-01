// Тесты сборщика метрик (разд. 2.3, формат 4.3).
//
// Собственная арифметика агента (скорости из дельт счётчиков) проверяется
// отдельно: счётчики ядра переполняются и сбрасываются при рестарте
// интерфейса, и без защиты это давало бы гигантские выбросы на графиках.
package collector

import (
	"context"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/xakus/DSMS/agent/internal/config"
)

// TestPerSecHandlesCounterReset: сброс счётчика (cur < prev) даёт 0,
// а не «отрицательную» скорость, развёрнутую в огромное uint64.
func TestPerSecHandlesCounterReset(t *testing.T) {
	cases := []struct {
		name      string
		cur, prev uint64
		dt        float64
		want      uint64
	}{
		{"обычная дельта", 3000, 1000, 2, 1000},
		{"дробный интервал", 1500, 1000, 0.5, 1000},
		{"счётчик сброшен", 100, 5000, 3, 0},
		{"нулевой интервал", 5000, 1000, 0, 0},
		{"отрицательный интервал", 5000, 1000, -1, 0},
		{"без изменений", 1000, 1000, 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := perSec(tc.cur, tc.prev, tc.dt); got != tc.want {
				t.Fatalf("perSec(%d,%d,%v) = %d, want %d", tc.cur, tc.prev, tc.dt, got, tc.want)
			}
		})
	}
}

// TestRound1: формат разд. 4.3 — один знак после запятой.
func TestRound1(t *testing.T) {
	cases := map[float64]float64{
		12.34: 12.3,
		12.35: 12.4, // округление вверх на половине
		0:     0,
		99.99: 100,
		7.0:   7.0,
	}
	for in, want := range cases {
		if got := round1(in); got != want {
			t.Fatalf("round1(%v) = %v, want %v", in, got, want)
		}
	}
}

// TestFindIOMatchesBySuffix: устройство /dev/sda1 сопоставляется со счётчиками
// ядра по имени sda1 — так их отдаёт gopsutil.
func TestFindIOMatchesBySuffix(t *testing.T) {
	io := map[string]disk.IOCountersStat{
		"sda1":      {ReadBytes: 100},
		"nvme0n1p2": {ReadBytes: 200},
	}
	if s, ok := findIO(io, "/dev/sda1"); !ok || s.ReadBytes != 100 {
		t.Fatalf("устройство по полному пути не найдено: %+v (%v)", s, ok)
	}
	if s, ok := findIO(io, "/dev/nvme0n1p2"); !ok || s.ReadBytes != 200 {
		t.Fatalf("nvme-устройство не найдено: %+v (%v)", s, ok)
	}
	if _, ok := findIO(io, "/dev/sdb9"); ok {
		t.Fatal("найдено несуществующее устройство")
	}
	if _, ok := findIO(map[string]disk.IOCountersStat{}, "/dev/sda1"); ok {
		t.Fatal("пустая карта счётчиков вернула совпадение")
	}
}

// TestCollectSnapshotShape: сборщик отдаёт заполненный снапшот текущей
// машины — smoke-тест формата (NodeID, время, CPU, память).
func TestCollectSnapshotShape(t *testing.T) {
	c := New(&config.Config{NodeID: "node-under-test"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snap, err := c.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if snap.NodeID != "node-under-test" {
		t.Fatalf("NodeID не проставлен: %+v", snap)
	}
	if snap.TS == 0 || time.Since(time.Unix(snap.TS, 0)) > time.Minute {
		t.Fatalf("метка времени неправдоподобна: %d", snap.TS)
	}
	if snap.Mem.Total == 0 {
		t.Fatal("объём памяти не собран")
	}
	if snap.CPU.TotalPct < 0 || snap.CPU.TotalPct > 100 {
		t.Fatalf("CPU вне диапазона 0..100: %v", snap.CPU.TotalPct)
	}
	// Второй вызов считает скорости от предыдущего тика — не должен падать.
	if _, err := c.Collect(ctx); err != nil {
		t.Fatalf("повторный Collect: %v", err)
	}
}
