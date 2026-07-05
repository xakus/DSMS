// Package ws — WebSocket-hub PANEL (разд. 4.2 ТЗ).
//
// Один мультиплексированный канал /api/v1/ws с протоколом подписок:
//
//	→ {"op":"sub","topic":"metrics"} / {"op":"unsub", ...}
//	← {"topic":"metrics","node":"...","ts":...,"data":{...}}
//
// Топики v1: metrics, logs (per-service), events, alerts.
// Библиотека github.com/coder/websocket — актуальное имя nhooyr.io/websocket.
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Op — входящее сообщение подписки от клиента.
type Op struct {
	Op      string `json:"op"`                // sub | unsub
	Topic   string `json:"topic"`             // metrics | logs | events | alerts
	Service string `json:"service,omitempty"` // для topic=logs
	Tail    int    `json:"tail,omitempty"`    // для topic=logs
}

// subKey — ключ подписки: топик + опциональный объект (сервис для логов).
type subKey struct {
	topic   string
	service string
}

// client — одно WebSocket-соединение с его подписками.
type client struct {
	conn *websocket.Conn
	mu   sync.Mutex // сериализация записи в conn и доступа к subs
	subs map[subKey]bool
}

// subscribed потокобезопасно проверяет наличие подписки.
func (c *client) subscribed(key subKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subs[key]
}

// setSub потокобезопасно ставит/снимает подписку.
func (c *client) setSub(key subKey, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		c.subs[key] = true
	} else {
		delete(c.subs, key)
	}
}

// Hub — реестр подключённых клиентов и рассылка по топикам.
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]bool
}

// NewHub создаёт пустой hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*client]bool)}
}

// Broadcast шлёт payload всем подписчикам ключа {topic, service}.
// payload уже должен содержать поле "topic" (формат разд. 4.2).
func (h *Hub) Broadcast(topic, service string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("ws marshal failed", "err", err)
		return
	}
	key := subKey{topic: topic, service: service}

	h.mu.RLock()
	targets := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		if c.subscribed(key) {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.send(data)
	}
}

// send пишет фрейм клиенту; при ошибке соединение закроет reader-цикл.
func (c *client) send(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.conn.Write(ctx, websocket.MessageText, data)
}

// Handle — HTTP-handler апгрейда соединения. Аутентификация — снаружи
// (middleware сессий), сюда попадают только авторизованные клиенты.
func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, subs: make(map[subKey]bool)}

	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Ping/pong каждые 30 сек (разд. 4.2) — фоновая горутина.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Reader-цикл: принимаем sub/unsub до закрытия соединения.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var op Op
		if err := json.Unmarshal(data, &op); err != nil {
			continue // молча пропускаем мусор
		}
		key := subKey{topic: op.Topic, service: op.Service}
		switch op.Op {
		case "sub":
			c.setSub(key, true)
		case "unsub":
			c.setSub(key, false)
		}
	}
}
