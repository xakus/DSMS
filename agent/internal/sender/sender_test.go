// Тесты отправки метрик на панель (разд. 2.3).
//
// Главное требование: агент переживает недоступность панели, не растёт по
// памяти и не заваливает её мусором из прошлого — старые точки для live-панели
// бесполезны, поэтому они дропаются, а не копятся.
package sender

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/xakus/DSMS/agent/internal/collector"
	"github.com/xakus/DSMS/agent/internal/config"
)

// recorder — панель-заглушка: пишет полученные снапшоты и умеет ломаться.
type recorder struct {
	mu     sync.Mutex
	got    []collector.Snapshot
	tokens []string
	fail   bool
}

func (r *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var snap collector.Snapshot
		_ = json.NewDecoder(req.Body).Decode(&snap)
		r.got = append(r.got, snap)
		r.tokens = append(r.tokens, req.Header.Get("X-Agent-Token"))
	}
}

func (r *recorder) received() []collector.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]collector.Snapshot(nil), r.got...)
}

func (r *recorder) setFail(v bool) {
	r.mu.Lock()
	r.fail = v
	r.mu.Unlock()
}

// newSender поднимает заглушку панели и подключённый к ней Sender.
func newSender(t *testing.T, bufferAge time.Duration) (*Sender, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		PanelURL:   srv.URL,
		AgentToken: "test-token",
		BufferAge:  bufferAge,
		NodeID:     "node-1",
	}
	return New(cfg), rec
}

// waitFor опрашивает условие до таймаута — отправка асинхронна.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func snap(ts int64) collector.Snapshot {
	return collector.Snapshot{NodeID: "node-1", TS: ts}
}

// TestSendsWithToken: снапшот доходит до панели вместе с токеном агента (3.7.4).
func TestSendsWithToken(t *testing.T) {
	s, rec := newSender(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	s.Enqueue(snap(time.Now().Unix()))

	waitFor(t, "доставка снапшота", func() bool { return len(rec.received()) == 1 })
	rec.mu.Lock()
	token := rec.tokens[0]
	rec.mu.Unlock()
	if token != "test-token" {
		t.Fatalf("панель получила чужой токен: %q", token)
	}
}

// TestKeepsOrderAndDrainsQueue: очередь уходит целиком и по порядку (FIFO).
func TestKeepsOrderAndDrainsQueue(t *testing.T) {
	s, rec := newSender(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		s.Enqueue(snap(now + int64(i)))
	}

	waitFor(t, "доставка всех точек", func() bool { return len(rec.received()) == 5 })
	got := rec.received()
	for i := range got {
		if got[i].TS != now+int64(i) {
			t.Fatalf("нарушен порядок отправки: %+v", got)
		}
	}
	if s.queueLen() != 0 {
		t.Fatalf("очередь не опустела: %d", s.queueLen())
	}
}

// TestRetriesAfterPanelFailure: панель полежала и вернулась — накопленное
// уходит без потерь (NFR-5).
func TestRetriesAfterPanelFailure(t *testing.T) {
	s, rec := newSender(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	rec.setFail(true)
	now := time.Now().Unix()
	s.Enqueue(snap(now))
	s.Enqueue(snap(now + 1))

	// Пока панель отвечает 503, точки остаются в очереди.
	waitFor(t, "накопление очереди", func() bool { return s.queueLen() == 2 })
	if len(rec.received()) != 0 {
		t.Fatal("данные приняты, хотя панель отвечала ошибкой")
	}

	rec.setFail(false)
	s.Enqueue(snap(now + 2)) // новый kick разбудит цикл

	waitFor(t, "досылку после восстановления", func() bool { return len(rec.received()) == 3 })
	if s.queueLen() != 0 {
		t.Fatalf("очередь не разобрана: %d", s.queueLen())
	}
}

// TestDropsStalePoints: точки старше BufferAge выбрасываются — иначе после
// долгого простоя панели агент завалил бы её историей и раздул память.
func TestDropsStalePoints(t *testing.T) {
	s, rec := newSender(t, 60*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec.setFail(true)
	now := time.Now().Unix()
	s.Enqueue(snap(now - 3600)) // час назад — заведомо протухла
	s.Enqueue(snap(now - 300))  // 5 минут назад — тоже
	if s.queueLen() != 0 {
		t.Fatalf("протухшие точки должны отбрасываться сразу: %d", s.queueLen())
	}

	s.Enqueue(snap(now))
	if s.queueLen() != 1 {
		t.Fatalf("свежая точка должна остаться: %d", s.queueLen())
	}

	rec.setFail(false)
	go s.Run(ctx)
	s.Enqueue(snap(now + 1))

	waitFor(t, "отправку свежих точек", func() bool { return len(rec.received()) == 2 })
	for _, g := range rec.received() {
		if g.TS < now {
			t.Fatalf("протухшая точка всё-таки ушла на панель: %+v", g)
		}
	}
}

// TestStopsOnContextCancel: остановка агента завершает цикл отправки.
func TestStopsOnContextCancel(t *testing.T) {
	s, _ := newSender(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("цикл отправки не завершился после отмены контекста")
	}
}
