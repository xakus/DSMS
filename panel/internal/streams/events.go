// Стриминг Docker events → WS topic "events" (FR-06 3.6.1).
// Живёт весь uptime панели; при обрыве соединения с daemon — reconnect (NFR-5).
package streams

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/events"
)

// EventSource — источник docker events (реализуется dockerapi.Client).
type EventSource interface {
	Events(ctx context.Context) (<-chan events.Message, <-chan error)
}

// EventMsg — событие в WS-формате (разд. 4.2).
type EventMsg struct {
	Topic  string `json:"topic"` // всегда "events"
	Type   string `json:"type"`  // service | node | container | ...
	Action string `json:"action"`
	Actor  string `json:"actor"` // имя объекта (из attributes) либо ID
	TS     int64  `json:"ts"`
}

// RunEvents запускает вечный цикл трансляции событий в hub.
// onEvent (может быть nil) — дополнительный обработчик каждого события:
// движок алертов ловит через него task failure (FR-12).
// Блокирует до отмены ctx — вызывать в горутине из main.
func RunEvents(ctx context.Context, src EventSource, sink LogSink, onEvent func(events.Message)) {
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, errs := src.Events(ctx)
	stream:
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-msgs:
				actor := m.Actor.Attributes["name"]
				if actor == "" {
					actor = m.Actor.ID
				}
				sink.Broadcast("events", "", EventMsg{
					Topic:  "events",
					Type:   string(m.Type),
					Action: string(m.Action),
					Actor:  actor,
					TS:     m.Time,
				})
				if onEvent != nil {
					onEvent(m)
				}
			case err := <-errs:
				if err != nil {
					slog.Warn("docker events stream broken, reconnecting", "err", err)
				}
				break stream
			}
		}
		// Пауза перед переподключением (рестарт daemon и т.п.).
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
