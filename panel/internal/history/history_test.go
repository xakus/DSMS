// Тесты истории метрик: агрегация минуты (разд. 2.2), ретенция из настроек
// и прогрев буфера после рестарта панели.
//
// Проверяется то, что видит пользователь: график за 15 минут не пустеет
// после рестарта, а недельная история не растёт бесконечно.
package history

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// snapshot собирает точку метрик ноды.
func snapshot(nodeID string, ts int64, cpu float64, memUsed uint64) metrics.Snapshot {
	return metrics.Snapshot{
		NodeID: nodeID,
		TS:     ts,
		CPU:    metrics.CPU{TotalPct: cpu},
		Mem:    metrics.Mem{Used: memUsed, Total: 8 << 30},
		Disk:   []metrics.Disk{{ReadBps: 100, WriteBps: 200}},
		Net:    []metrics.Net{{RxBps: 1000, TxBps: 2000}},
	}
}

// TestAggregateWritesMinutePoint: точки минуты сворачиваются в одну запись
// со средним CPU и средними скоростями.
func TestAggregateWritesMinutePoint(t *testing.T) {
	st := newStore(t)
	buf := metrics.NewClusterBuffer(15*time.Minute, 3*time.Second)

	now := time.Now().Unix()
	// Три точки внутри последней минуты: CPU 10/20/30 → среднее 20.
	buf.Put(snapshot("node-1", now-30, 10, 1<<30))
	buf.Put(snapshot("node-1", now-20, 20, 2<<30))
	buf.Put(snapshot("node-1", now-10, 30, 3<<30))

	New(st, buf).aggregate()

	points, err := st.MetricsRange("node-1", now-3600, now+3600)
	if err != nil {
		t.Fatalf("MetricsRange: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("ожидалась одна минутная точка, получено %d", len(points))
	}
	p := points[0]
	if p.CPUPct < 19.9 || p.CPUPct > 20.1 {
		t.Fatalf("среднее CPU за минуту неверно: %v", p.CPUPct)
	}
	// Память — из последней точки минуты, а не среднее.
	if p.MemUsed != 3<<30 {
		t.Fatalf("mem_used должен быть из последней точки: %d", p.MemUsed)
	}
	var disk DiskAgg
	if err := json.Unmarshal([]byte(p.DiskJSON), &disk); err != nil {
		t.Fatalf("disk_json: %v", err)
	}
	if disk.ReadBps != 100 || disk.WriteBps != 200 {
		t.Fatalf("средние скорости диска неверны: %+v", disk)
	}
}

// TestAggregateSkipsOldPoints: точки старше минуты уже агрегированы —
// повторно они попадать не должны.
func TestAggregateSkipsOldPoints(t *testing.T) {
	st := newStore(t)
	buf := metrics.NewClusterBuffer(15*time.Minute, 3*time.Second)

	now := time.Now().Unix()
	buf.Put(snapshot("node-old", now-600, 99, 1<<30)) // 10 минут назад

	New(st, buf).aggregate()

	points, err := st.MetricsRange("node-old", now-3600, now+3600)
	if err != nil {
		t.Fatalf("MetricsRange: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("старые точки не должны агрегироваться повторно: %+v", points)
	}
}

// TestRetentionFromSettings: срок хранения берётся из настроек, некорректные
// значения игнорируются (иначе опечатка в UI снесла бы всю историю).
func TestRetentionFromSettings(t *testing.T) {
	st := newStore(t)
	a := New(st, metrics.NewClusterBuffer(time.Minute, time.Second))

	if got := a.retentionDays(); got != defaultRetentionDays {
		t.Fatalf("без настройки должно быть %d, получено %d", defaultRetentionDays, got)
	}
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"30", 30},
		{"1", 1},
		{"90", 90},
		{"0", defaultRetentionDays},      // вне диапазона
		{"91", defaultRetentionDays},     // вне диапазона
		{"-5", defaultRetentionDays},     // отрицательное
		{"неделя", defaultRetentionDays}, // не число
		{"", defaultRetentionDays},
	} {
		if err := st.SetSetting(SettingRetentionDays, tc.value); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if got := a.retentionDays(); got != tc.want {
			t.Fatalf("ретенция для %q: want %d, got %d", tc.value, tc.want, got)
		}
	}
}

// TestCleanupRemovesOldMetrics: старые агрегаты удаляются, свежие остаются.
func TestCleanupRemovesOldMetrics(t *testing.T) {
	st := newStore(t)
	now := time.Now().Unix()

	old := now - 30*24*3600 // 30 дней назад
	if err := st.InsertMetrics1m("node-1", old, 50, 1<<30, 8<<30, "{}", "{}"); err != nil {
		t.Fatalf("insert old: %v", err)
	}
	if err := st.InsertMetrics1m("node-1", now, 60, 1<<30, 8<<30, "{}", "{}"); err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	cutoff := time.Now().AddDate(0, 0, -7).Unix()
	if err := st.CleanupMetrics(cutoff); err != nil {
		t.Fatalf("CleanupMetrics: %v", err)
	}

	points, err := st.MetricsRange("node-1", 0, now+3600)
	if err != nil {
		t.Fatalf("MetricsRange: %v", err)
	}
	if len(points) != 1 || points[0].TS != now {
		t.Fatalf("ретенция сработала неверно: %+v", points)
	}
}

// TestWarmBufferRestoresWindow: после рестарта панели 15-минутный график
// восстанавливается из metrics_live — ради этого таблица и заведена.
func TestWarmBufferRestoresWindow(t *testing.T) {
	st := newStore(t)
	now := time.Now().Unix()

	// Точка в окне и точка сильно старше окна.
	for _, s := range []metrics.Snapshot{
		snapshot("node-1", now-60, 42, 4<<30),
		snapshot("node-1", now-3600, 99, 1<<30),
	} {
		blob, _ := json.Marshal(s)
		if err := st.InsertMetricsLive(s.NodeID, s.TS, blob); err != nil {
			t.Fatalf("InsertMetricsLive: %v", err)
		}
	}
	// Битая точка не должна ломать прогрев.
	if err := st.InsertMetricsLive("node-1", now-30, []byte("{не json")); err != nil {
		t.Fatalf("InsertMetricsLive (битая): %v", err)
	}

	buf := metrics.NewClusterBuffer(15*time.Minute, 3*time.Second)
	WarmBuffer(st, buf, 15*time.Minute)

	window := buf.Window("node-1")
	if len(window) != 1 {
		t.Fatalf("в буфер должна попасть одна точка окна, получено %d: %+v", len(window), window)
	}
	if window[0].CPU.TotalPct != 42 {
		t.Fatalf("восстановлена не та точка: %+v", window[0])
	}
	latest, ok := buf.Latest("node-1")
	if !ok || latest.TS != now-60 {
		t.Fatalf("последняя точка не восстановлена: %+v", latest)
	}
}

// TestCleanupMetricsLive: живые точки живут коротко (~20 мин) — иначе
// таблица растёт линейно при шаге 1–3 секунды.
func TestCleanupMetricsLive(t *testing.T) {
	st := newStore(t)
	now := time.Now().Unix()

	blob, _ := json.Marshal(snapshot("node-1", now, 10, 1<<30))
	if err := st.InsertMetricsLive("node-1", now-3600, blob); err != nil {
		t.Fatalf("insert old: %v", err)
	}
	if err := st.InsertMetricsLive("node-1", now, blob); err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	if err := st.CleanupMetricsLive(time.Now().Add(-liveRetention).Unix()); err != nil {
		t.Fatalf("CleanupMetricsLive: %v", err)
	}
	rows, err := st.RecentMetricsLive(0)
	if err != nil {
		t.Fatalf("RecentMetricsLive: %v", err)
	}
	if len(rows) != 1 || rows[0].TS != now {
		t.Fatalf("осталось не то, что ожидалось: %+v", rows)
	}
}
