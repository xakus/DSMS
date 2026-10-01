// Тесты WebSocket-хаба (разд. 4.2).
//
// Ключевое здесь — учёт подписчиков: по нему панель запускает и останавливает
// стримы логов. Ошибка в счётчике = либо вечно висящий стрим логов от Docker,
// либо мёртвый экран логов у пользователя. Поэтому тесты гоняют реальные
// соединения, включая обрыв клиента без unsub.
package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// hubServer поднимает hub на httptest и возвращает URL для подключения.
func hubServer(t *testing.T, h *Hub) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	t.Cleanup(srv.Close)
	return "ws" + srv.URL[len("http"):]
}

// dial подключает клиента к хабу.
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// send отправляет операцию подписки.
func send(t *testing.T, conn *websocket.Conn, op Op) {
	t.Helper()
	data, _ := json.Marshal(op)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readJSON читает одно сообщение с таймаутом.
func readJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
}

// waitFor ждёт выполнения условия — подписка обрабатывается асинхронно
// в reader-цикле хаба, поэтому опрос вместо фиксированной паузы.
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

// TestBroadcastReachesSubscribersOnly: сообщение получает только подписчик
// нужного топика.
func TestBroadcastReachesSubscribersOnly(t *testing.T) {
	h := NewHub()
	url := hubServer(t, h)

	subscriber := dial(t, url)
	other := dial(t, url)

	send(t, subscriber, Op{Op: "sub", Topic: "metrics"})
	send(t, other, Op{Op: "sub", Topic: "events"})
	waitFor(t, "подписки metrics и events", func() bool {
		return h.HasSubscribers("metrics", "") && h.HasSubscribers("events", "")
	})

	h.Broadcast("metrics", "", map[string]any{"topic": "metrics", "cpu": 42})

	var got map[string]any
	readJSON(t, subscriber, &got)
	if got["topic"] != "metrics" || got["cpu"].(float64) != 42 {
		t.Fatalf("подписчик получил не то: %+v", got)
	}

	// Второму клиенту ничего прийти не должно: читаем с коротким таймаутом.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, _, err := other.Read(ctx); err == nil {
		t.Fatal("клиент получил сообщение чужого топика")
	}
}

// TestPerServiceTopics: подписки логов разных сервисов не пересекаются.
func TestPerServiceTopics(t *testing.T) {
	h := NewHub()
	url := hubServer(t, h)

	a := dial(t, url)
	send(t, a, Op{Op: "sub", Topic: "logs", Service: "svc-a"})
	waitFor(t, "подписка на логи svc-a", func() bool { return h.HasSubscribers("logs", "svc-a") })

	if h.HasSubscribers("logs", "svc-b") {
		t.Fatal("подписка на один сервис включила другой")
	}

	h.Broadcast("logs", "svc-b", map[string]any{"topic": "logs", "line": "чужое"})
	h.Broadcast("logs", "svc-a", map[string]any{"topic": "logs", "line": "своё"})

	var got map[string]any
	readJSON(t, a, &got)
	if got["line"] != "своё" {
		t.Fatalf("пришло сообщение чужого сервиса: %+v", got)
	}
}

// TestFirstSubLastUnsubCallbacks: стрим стартует на первом подписчике и
// останавливается на последнем — по одному разу, а не на каждого клиента.
func TestFirstSubLastUnsubCallbacks(t *testing.T) {
	h := NewHub()

	var mu sync.Mutex
	var starts, stops int
	var gotTail int
	h.OnFirstSub = func(topic, service string, tail int) {
		mu.Lock()
		defer mu.Unlock()
		starts++
		gotTail = tail
	}
	h.OnLastUnsub = func(topic, service string) {
		mu.Lock()
		defer mu.Unlock()
		stops++
	}
	count := func(p *int) func() int {
		return func() int {
			mu.Lock()
			defer mu.Unlock()
			return *p
		}
	}
	startCount, stopCount := count(&starts), count(&stops)

	url := hubServer(t, h)
	first := dial(t, url)
	second := dial(t, url)

	send(t, first, Op{Op: "sub", Topic: "logs", Service: "svc", Tail: 100})
	waitFor(t, "старт стрима", func() bool { return startCount() == 1 })
	if gotTail != 100 {
		t.Fatalf("tail не передан в колбэк: %d", gotTail)
	}

	// Второй подписчик на тот же ключ стрим повторно не запускает.
	send(t, second, Op{Op: "sub", Topic: "logs", Service: "svc"})
	// Ждём именно двух подписчиков: HasSubscribers истинно уже после первого,
	// и unsub первого мог обогнать sub второго — тогда стрим законно стоп.
	waitFor(t, "подписка второго клиента", func() bool { return subscriberCount(h, "logs", "svc") == 2 })
	if startCount() != 1 {
		t.Fatalf("стрим запущен повторно: %d раз", startCount())
	}

	// Повторный sub от того же клиента тоже не должен считаться.
	send(t, second, Op{Op: "sub", Topic: "logs", Service: "svc"})
	send(t, first, Op{Op: "unsub", Topic: "logs", Service: "svc"})
	time.Sleep(100 * time.Millisecond)
	if stopCount() != 0 {
		t.Fatal("стрим остановлен, пока есть второй подписчик")
	}

	send(t, second, Op{Op: "unsub", Topic: "logs", Service: "svc"})
	waitFor(t, "остановка стрима", func() bool { return stopCount() == 1 })
	if h.HasSubscribers("logs", "svc") {
		t.Fatal("подписчики остались после отписки всех")
	}
}

// TestDisconnectReleasesSubscriptions: клиент отвалился без unsub — стрим
// всё равно должен остановиться, иначе логи Docker текут вечно.
func TestDisconnectReleasesSubscriptions(t *testing.T) {
	h := NewHub()
	var mu sync.Mutex
	stopped := false
	h.OnLastUnsub = func(topic, service string) {
		mu.Lock()
		stopped = true
		mu.Unlock()
	}

	url := hubServer(t, h)
	conn := dial(t, url)
	send(t, conn, Op{Op: "sub", Topic: "logs", Service: "svc"})
	waitFor(t, "подписка", func() bool { return h.HasSubscribers("logs", "svc") })

	// Резкий обрыв без вежливого unsub.
	conn.Close(websocket.StatusGoingAway, "bye")

	waitFor(t, "освобождение подписки при обрыве", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return stopped && !h.HasSubscribers("logs", "svc")
	})
}

// TestGarbageIsIgnored: мусорные фреймы не роняют соединение.
func TestGarbageIsIgnored(t *testing.T) {
	h := NewHub()
	url := hubServer(t, h)
	conn := dial(t, url)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte("{это не json")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Соединение живо: подписка после мусора работает.
	send(t, conn, Op{Op: "sub", Topic: "metrics"})
	waitFor(t, "подписка после мусора", func() bool { return h.HasSubscribers("metrics", "") })
}

// TestUnsubWithoutSub: отписка без подписки не уводит счётчик в минус.
func TestUnsubWithoutSub(t *testing.T) {
	h := NewHub()
	if h.decSub(subKey{topic: "metrics"}) {
		t.Fatal("decSub без подписки сообщил об остановке стрима")
	}
	if h.HasSubscribers("metrics", "") {
		t.Fatal("появились подписчики из ниоткуда")
	}
}

// subscriberCount — число подписчиков ключа (тестовый доступ к счётчику хаба).
func subscriberCount(h *Hub, topic, service string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.subCount[subKey{topic: topic, service: service}]
}
