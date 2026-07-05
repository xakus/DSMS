// Тесты metrics_1m: вставка, выборка диапазона, ретенция.
package store

import (
	"path/filepath"
	"testing"
	"time"
)

// newTestStore — временная SQLite.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestMetricsRoundtrip: insert → range → cleanup.
func TestMetricsRoundtrip(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Unix()

	// три точки: 2 свежие, 1 старая (8 дней)
	old := now - 8*24*3600
	for i, ts := range []int64{now - 120, now - 60, old} {
		if err := s.InsertMetrics1m("n1", ts, float64(10+i), 100, 200,
			`{"read_bps":1}`, `{"rx_bps":2}`); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// диапазон последнего часа — 2 точки, по возрастанию ts
	points, err := s.MetricsRange("n1", now-3600, now)
	if err != nil || len(points) != 2 {
		t.Fatalf("range: %v, %d points", err, len(points))
	}
	if points[0].TS > points[1].TS {
		t.Fatal("points must be ordered by ts")
	}
	if points[0].CPUPct != 10 || points[0].MemTotal != 200 {
		t.Fatalf("point content: %+v", points[0])
	}

	// ретенция: старая точка удаляется
	if err := s.CleanupMetrics(now - 7*24*3600); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	all, _ := s.MetricsRange("n1", 0, now)
	if len(all) != 2 {
		t.Fatalf("after cleanup: want 2 points, got %d", len(all))
	}

	// idempotent по PK: повторная вставка того же ts заменяет
	if err := s.InsertMetrics1m("n1", now-60, 99, 1, 2, "", ""); err != nil {
		t.Fatalf("replace: %v", err)
	}
	all, _ = s.MetricsRange("n1", 0, now)
	if len(all) != 2 {
		t.Fatalf("replace must not duplicate: %d", len(all))
	}
}

// TestSettingsRoundtrip: set → get → overwrite.
func TestSettingsRoundtrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetSetting("alert.disk_pct", "85"); err != nil {
		t.Fatal(err)
	}
	v, _ := s.GetSetting("alert.disk_pct")
	if v != "85" {
		t.Fatalf("get: %q", v)
	}
	_ = s.SetSetting("alert.disk_pct", "95")
	v, _ = s.GetSetting("alert.disk_pct")
	if v != "95" {
		t.Fatalf("overwrite: %q", v)
	}
}
