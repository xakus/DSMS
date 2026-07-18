// Package streams — стриминг логов и Docker-событий в WebSocket-hub
// (FR-05, FR-06). Стримы логов запускаются по первой подписке и
// останавливаются с последней (хуки hub'а).
package streams

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LogSource — источник лог-потока (реализуется dockerapi.Client).
type LogSource interface {
	ServiceLogs(ctx context.Context, serviceID string, tail int, follow bool) (io.ReadCloser, error)
}

// LogSink — приёмник строк (реализуется ws.Hub).
type LogSink interface {
	Broadcast(topic, service string, payload any)
	HasSubscribers(topic, service string) bool
}

// LogLine — одна строка лога в WS-формате (разд. 4.2).
type LogLine struct {
	Topic   string `json:"topic"`   // всегда "logs"
	Service string `json:"service"` // id сервиса
	Task    string `json:"task,omitempty"`
	// TaskName — "<service>.<slot>.<taskid>": из slot получаем номер реплики.
	TaskName string `json:"task_name,omitempty"`
	Node     string `json:"node,omitempty"`
	Stream   string `json:"stream"` // stdout | stderr
	TS       string `json:"ts,omitempty"`
	Line     string `json:"line"`
}

// LogStreamer управляет активными стримами логов сервисов.
type LogStreamer struct {
	src  LogSource
	sink LogSink

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // serviceID → stop
}

// NewLogStreamer создаёт менеджер стримов.
func NewLogStreamer(src LogSource, sink LogSink) *LogStreamer {
	return &LogStreamer{src: src, sink: sink, cancels: make(map[string]context.CancelFunc)}
}

// Start запускает стрим логов сервиса (идемпотентно).
// tail=0 — только новые строки (историю фронт грузит REST-ом), tail
// ограничен 1000 сверху — защита WS от лавины строк (разд. 10 ТЗ).
func (l *LogStreamer) Start(serviceID string, tail int) {
	if tail < 0 {
		tail = 0
	}
	if tail > 1000 {
		tail = 1000
	}
	l.mu.Lock()
	if _, running := l.cancels[serviceID]; running {
		l.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancels[serviceID] = cancel
	l.mu.Unlock()

	go l.run(ctx, serviceID, tail)
}

// Stop останавливает стрим сервиса.
func (l *LogStreamer) Stop(serviceID string) {
	l.mu.Lock()
	if cancel, ok := l.cancels[serviceID]; ok {
		cancel()
		delete(l.cancels, serviceID)
	}
	l.mu.Unlock()
}

// run читает мультиплексированный поток docker и рассылает строки.
// При обрыве потока (рестарт daemon, NFR-5) — переподключение через 3с,
// пока есть подписчики.
func (l *LogStreamer) run(ctx context.Context, serviceID string, tail int) {
	first := true
	for {
		if ctx.Err() != nil {
			return
		}
		// После реконнекта бэклог не повторяем (tail=0 → только новое).
		t := tail
		if !first {
			t = 0
		}
		first = false

		rc, err := l.src.ServiceLogs(ctx, serviceID, t, true)
		if err != nil {
			slog.Warn("service logs open failed", "service", serviceID, "err", err)
		} else {
			l.pump(ctx, serviceID, rc)
			rc.Close()
		}
		if !l.sink.HasSubscribers("logs", serviceID) {
			return // все отписались, пока поток был мёртв
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// pump разбирает stdcopy-фреймы потока и шлёт строки в hub.
func (l *LogStreamer) pump(ctx context.Context, serviceID string, rc io.Reader) {
	r := bufio.NewReaderSize(rc, 64*1024)
	for {
		if ctx.Err() != nil {
			return
		}
		stream, payload, err := readFrame(r)
		if err != nil {
			return
		}
		for line := range strings.SplitSeq(strings.TrimRight(string(payload), "\n"), "\n") {
			if line == "" {
				continue
			}
			ll := parseLine(line)
			ll.Topic = "logs"
			ll.Service = serviceID
			ll.Stream = stream
			l.sink.Broadcast("logs", serviceID, ll)
		}
	}
}

// ParseSnapshot читает весь stdcopy-поток (без follow) и разбирает строки
// в LogLine. Используется REST-хендлером истории логов (постраничный вход
// и подгрузка по скроллу). Топик и служебные поля Service клиент проставляет
// себе сам — здесь заполняем только содержимое строки.
func ParseSnapshot(rc io.Reader) []LogLine {
	r := bufio.NewReaderSize(rc, 64*1024)
	out := make([]LogLine, 0, 256)
	for {
		stream, payload, err := readFrame(r)
		if err != nil {
			return out
		}
		for line := range strings.SplitSeq(strings.TrimRight(string(payload), "\n"), "\n") {
			if line == "" {
				continue
			}
			ll := parseLine(line)
			ll.Topic = "logs"
			ll.Stream = stream
			out = append(out, ll)
		}
	}
}

// readFrame читает один stdcopy-фрейм: header 8 байт
// [streamType, 0,0,0, len(BE u32)], затем payload.
func readFrame(r io.Reader) (stream string, payload []byte, err error) {
	var header [8]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return "", nil, err
	}
	switch header[0] {
	case 2:
		stream = "stderr"
	default:
		stream = "stdout"
	}
	size := binary.BigEndian.Uint32(header[4:8])
	if size > 1<<20 { // защита от битого заголовка
		return "", nil, io.ErrUnexpectedEOF
	}
	payload = make([]byte, size)
	_, err = io.ReadFull(r, payload)
	return stream, payload, err
}

// parseLine разбирает строку docker-лога с Timestamps+Details:
//
//	"<RFC3339Nano> com.docker.swarm.node.id=X,...,com.docker.swarm.task.id=Z <msg>"
func parseLine(raw string) LogLine {
	ll := LogLine{Line: raw}

	// 1) timestamp — до первого пробела
	sp := strings.IndexByte(raw, ' ')
	if sp <= 0 {
		return ll
	}
	if _, err := time.Parse(time.RFC3339Nano, raw[:sp]); err != nil {
		return ll
	}
	ll.TS = raw[:sp]
	rest := raw[sp+1:]

	// 2) details — "k=v,k=v " до следующего пробела (если есть '=')
	sp = strings.IndexByte(rest, ' ')
	if sp > 0 && strings.Contains(rest[:sp], "=") {
		for pair := range strings.SplitSeq(rest[:sp], ",") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			switch k {
			case "com.docker.swarm.task.id":
				ll.Task = v
			case "com.docker.swarm.task.name":
				ll.TaskName = v
			case "com.docker.swarm.node.id":
				ll.Node = v
			}
		}
		rest = rest[sp+1:]
	}
	ll.Line = rest
	return ll
}
