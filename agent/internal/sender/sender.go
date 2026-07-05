// Package sender — отправка батчей на panel с буферизацией (разд. 2.3 ТЗ).
//
// Очередь в памяти: при недоступности panel точки копятся до 60 сек,
// старее — отбрасываются (старые метрики бесполезны для live-панели).
package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/xakus/DSMS/agent/internal/collector"
	"github.com/xakus/DSMS/agent/internal/config"
)

// Sender шлёт снапшоты на panel, буферизуя при сбоях.
type Sender struct {
	cfg    *config.Config
	client *http.Client

	mu    sync.Mutex
	queue []collector.Snapshot // очередь неотправленных снапшотов
	kick  chan struct{}        // сигнал «появились данные»
}

// New создаёт Sender с коротким HTTP-таймаутом —
// зависший запрос не должен задерживать следующий тик.
func New(cfg *config.Config) *Sender {
	return &Sender{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		kick:   make(chan struct{}, 1),
	}
}

// Enqueue кладёт снапшот в очередь и будит цикл отправки.
func (s *Sender) Enqueue(snap collector.Snapshot) {
	s.mu.Lock()
	s.queue = append(s.queue, snap)
	s.dropStaleLocked()
	s.mu.Unlock()

	select {
	case s.kick <- struct{}{}:
	default: // цикл уже разбужен
	}
}

// Run — цикл отправки; живёт в отдельной горутине до отмены контекста.
func (s *Sender) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
			s.flush(ctx)
		}
	}
}

// flush отправляет очередь по одному снапшоту (FIFO).
// Ошибка отправки прерывает цикл — остальное уйдёт со следующим kick.
func (s *Sender) flush(ctx context.Context) {
	for {
		s.mu.Lock()
		s.dropStaleLocked()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		snap := s.queue[0]
		s.mu.Unlock()

		if err := s.post(ctx, snap); err != nil {
			slog.Warn("ingest failed, will retry", "err", err, "queued", s.queueLen())
			return
		}

		s.mu.Lock()
		s.queue = s.queue[1:]
		s.mu.Unlock()
	}
}

// post выполняет POST /api/v1/ingest с заголовком X-Agent-Token (3.7.4).
func (s *Sender) post(ctx context.Context, snap collector.Snapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.PanelURL+"/api/v1/ingest", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", s.cfg.AgentToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("panel returned %s", resp.Status)
	}
	return nil
}

// dropStaleLocked отбрасывает точки старше BufferAge (вызывать под mu).
func (s *Sender) dropStaleLocked() {
	cutoff := time.Now().Add(-s.cfg.BufferAge).Unix()
	i := 0
	for ; i < len(s.queue); i++ {
		if s.queue[i].TS >= cutoff {
			break
		}
	}
	if i > 0 {
		s.queue = s.queue[i:]
	}
}

// queueLen — текущая длина очереди (для логов).
func (s *Sender) queueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}
