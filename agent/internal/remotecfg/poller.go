// Package remotecfg — опрос панели на период отдачи метрик. Агент раз в 10с
// тянет GET /api/v1/agent/config и подстраивает интервал сбора под настройку
// из UI, без рестарта контейнера.
package remotecfg

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/xakus/DSMS/agent/internal/config"
)

// Границы интервала (согласованы с панелью: settings 1..30с).
const (
	pollEvery   = 10 * time.Second
	minInterval = 1 * time.Second
	maxInterval = 30 * time.Second
)

// Poller хранит актуальный период сбора (атомарно) и обновляет его опросом.
type Poller struct {
	cfg      *config.Config
	client   *http.Client
	interval atomic.Int64 // текущий интервал, наносекунды
}

// New создаёт поллер со стартовым интервалом из конфигурации агента.
func New(cfg *config.Config) *Poller {
	p := &Poller{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
	}
	p.interval.Store(int64(clamp(cfg.Interval)))
	return p
}

// Interval возвращает актуальный период сбора метрик.
func (p *Poller) Interval() time.Duration {
	return time.Duration(p.interval.Load())
}

// Run периодически (каждые pollEvery) тянет период с панели. При ошибке
// оставляет текущее значение. Завершается по отмене ctx.
func (p *Poller) Run(ctx context.Context) {
	p.fetch(ctx) // сразу, не дожидаясь первого тика
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.fetch(ctx)
		}
	}
}

// fetch запрашивает конфиг и обновляет интервал при изменении.
func (p *Poller) fetch(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.cfg.PanelURL+"/api/v1/agent/config", nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Agent-Token", p.cfg.AgentToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return // панель недоступна — оставляем текущий интервал
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		IntervalSec int `json:"interval_sec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.IntervalSec <= 0 {
		return
	}
	next := clamp(time.Duration(body.IntervalSec) * time.Second)
	if prev := time.Duration(p.interval.Swap(int64(next))); prev != next {
		slog.Info("metrics interval updated", "from", prev.String(), "to", next.String())
	}
}

// clamp зажимает интервал в допустимый диапазон.
func clamp(d time.Duration) time.Duration {
	if d < minInterval {
		return minInterval
	}
	if d > maxInterval {
		return maxInterval
	}
	return d
}
