// Прогрев кольцевого буфера при старте панели: живые точки (metrics_live),
// накопленные до рестарта, возвращаются в память — 15м-график не пустеет.
package history

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
)

// WarmBuffer загружает из SQLite живые точки за последние window и кладёт их
// в буфер (старые первыми — порядок из RecentMetricsLive). Ошибки не фатальны:
// панель поднимется и с пустым буфером.
func WarmBuffer(st *store.Store, buf *metrics.ClusterBuffer, window time.Duration) {
	cutoff := time.Now().Add(-window).Unix()
	rows, err := st.RecentMetricsLive(cutoff)
	if err != nil {
		slog.Warn("warm buffer: read failed", "err", err)
		return
	}
	n := 0
	for _, r := range rows {
		var snap metrics.Snapshot
		if err := json.Unmarshal(r.Snapshot, &snap); err != nil {
			continue // битую точку просто пропускаем
		}
		buf.Put(snap)
		n++
	}
	if n > 0 {
		slog.Info("warm buffer restored live metrics", "points", n)
	}
}
