package remotecfg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/xakus/DSMS/agent/internal/config"
)

// TestPollerFetch: fetch подхватывает интервал с сервера и клампит границы.
func TestPollerFetch(t *testing.T) {
	// сервер возвращает управляемый interval_sec и проверяет токен
	intervalSec := 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Agent-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"interval_sec":` + strconv.Itoa(intervalSec) + `}`))
	}))
	defer srv.Close()

	p := New(&config.Config{
		PanelURL:   srv.URL,
		AgentToken: "tok",
		Interval:   3 * time.Second,
	})

	// стартовое значение — из конфига
	if p.Interval() != 3*time.Second {
		t.Fatalf("initial: want 3s, got %v", p.Interval())
	}

	// после fetch — 10с
	p.fetch(context.Background())
	if p.Interval() != 10*time.Second {
		t.Fatalf("after fetch: want 10s, got %v", p.Interval())
	}

	// кламп сверху: 100с → 30с
	intervalSec = 100
	p.fetch(context.Background())
	if p.Interval() != maxInterval {
		t.Fatalf("clamp max: want %v, got %v", maxInterval, p.Interval())
	}

	// кламп снизу: 0 игнорируется (<=0 не применяется), остаётся прежнее
	intervalSec = 0
	p.fetch(context.Background())
	if p.Interval() != maxInterval {
		t.Fatalf("zero ignored: want %v, got %v", maxInterval, p.Interval())
	}
}

// TestPollerServerDown: при недоступной панели интервал не меняется.
func TestPollerServerDown(t *testing.T) {
	p := New(&config.Config{
		PanelURL:   "http://127.0.0.1:0", // заведомо недоступно
		AgentToken: "tok",
		Interval:   5 * time.Second,
	})
	p.fetch(context.Background())
	if p.Interval() != 5*time.Second {
		t.Fatalf("server down: want 5s, got %v", p.Interval())
	}
}
