// Тесты менеджера стримов логов (FR-05).
//
// Стрим логов держит открытым соединение к Docker daemon, поэтому важны две
// вещи: он поднимается ровно один раз на сервис и гарантированно умирает,
// когда подписчиков не осталось. Иначе на активной панели остаются висящие
// потоки, которые никто не читает.
package streams

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// frames собирает stdcopy-поток из строк (как отдаёт Docker).
func frames(stream byte, lines ...string) []byte {
	var buf bytes.Buffer
	for _, ln := range lines {
		payload := []byte(ln + "\n")
		var h [8]byte
		h[0] = stream
		binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
		buf.Write(h[:])
		buf.Write(payload)
	}
	return buf.Bytes()
}

// fakeSource — источник логов: отдаёт заготовленные потоки по очереди.
type fakeSource struct {
	mu      sync.Mutex
	streams [][]byte // что вернуть на каждый следующий вызов
	calls   int
	tails   []int
	err     error
}

func (f *fakeSource) ServiceLogs(ctx context.Context, serviceID string, tail int, follow bool) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.tails = append(f.tails, tail)
	if f.err != nil {
		return nil, f.err
	}
	var data []byte
	if len(f.streams) > 0 {
		data = f.streams[0]
		f.streams = f.streams[1:]
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSource) tailArgs() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.tails...)
}

// fakeSink — приёмник строк с управляемым числом подписчиков.
type fakeSink struct {
	mu          sync.Mutex
	lines       []LogLine
	subscribers bool
}

func (f *fakeSink) Broadcast(topic, service string, payload any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ll, ok := payload.(LogLine); ok {
		f.lines = append(f.lines, ll)
	}
}

func (f *fakeSink) HasSubscribers(topic, service string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subscribers
}

func (f *fakeSink) got() []LogLine {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]LogLine(nil), f.lines...)
}

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

// TestStreamDeliversLines: строки из потока Docker доходят до hub'а
// с проставленными topic/service/stream.
func TestStreamDeliversLines(t *testing.T) {
	src := &fakeSource{streams: [][]byte{frames(1, "первая", "вторая")}}
	sink := &fakeSink{}
	ls := NewLogStreamer(src, sink)

	ls.Start("svc-1", 0)
	waitFor(t, "доставку строк", func() bool { return len(sink.got()) == 2 })
	ls.Stop("svc-1")

	lines := sink.got()
	if lines[0].Line != "первая" || lines[1].Line != "вторая" {
		t.Fatalf("строки искажены: %+v", lines)
	}
	for _, l := range lines {
		if l.Topic != "logs" || l.Service != "svc-1" || l.Stream != "stdout" {
			t.Fatalf("служебные поля не проставлены: %+v", l)
		}
	}
}

// TestStderrMarked: поток stderr помечается — по нему в UI подсвечиваются ошибки.
func TestStderrMarked(t *testing.T) {
	src := &fakeSource{streams: [][]byte{frames(2, "паника")}}
	sink := &fakeSink{}
	ls := NewLogStreamer(src, sink)

	ls.Start("svc-1", 0)
	waitFor(t, "строку stderr", func() bool { return len(sink.got()) == 1 })
	ls.Stop("svc-1")

	if got := sink.got()[0].Stream; got != "stderr" {
		t.Fatalf("поток должен быть stderr: %q", got)
	}
}

// TestStartIsIdempotent: повторный Start того же сервиса не открывает второй
// поток к Docker.
func TestStartIsIdempotent(t *testing.T) {
	// Подписчики есть — стрим не завершится сам. Пауза реконнекта оставлена
	// штатной (3с), чтобы за время теста переподключений заведомо не было
	// и рост счётчика означал именно лишний Start.
	sink := &fakeSink{subscribers: true}
	src := &fakeSource{}
	ls := NewLogStreamer(src, sink)

	ls.Start("svc-1", 0)
	waitFor(t, "первое подключение", func() bool { return src.callCount() >= 1 })

	ls.Start("svc-1", 0)
	ls.Start("svc-1", 0)
	time.Sleep(100 * time.Millisecond)

	if got := src.callCount(); got != 1 {
		t.Fatalf("повторный Start открыл лишние потоки: подключений %d, ожидалось 1", got)
	}
	ls.Stop("svc-1")
}

// TestTailIsClamped: tail ограничен сверху (защита WS от лавины строк)
// и не может быть отрицательным.
func TestTailIsClamped(t *testing.T) {
	src := &fakeSource{}
	sink := &fakeSink{}
	ls := NewLogStreamer(src, sink)

	ls.Start("svc-big", 99999)
	waitFor(t, "вызов с обрезанным tail", func() bool { return len(src.tailArgs()) >= 1 })
	ls.Stop("svc-big")

	ls.Start("svc-neg", -5)
	waitFor(t, "вызов с нулевым tail", func() bool { return len(src.tailArgs()) >= 2 })
	ls.Stop("svc-neg")

	tails := src.tailArgs()
	if tails[0] != 1000 {
		t.Fatalf("tail должен обрезаться до 1000, получено %d", tails[0])
	}
	if tails[1] != 0 {
		t.Fatalf("отрицательный tail должен стать 0, получено %d", tails[1])
	}
}

// TestStreamStopsWhenNoSubscribers: поток закончился, подписчиков нет —
// стрим завершается и не переподключается вечно.
func TestStreamStopsWhenNoSubscribers(t *testing.T) {
	src := &fakeSource{streams: [][]byte{frames(1, "строка")}}
	sink := &fakeSink{subscribers: false}
	ls := NewLogStreamer(src, sink)

	ls.Start("svc-1", 0)
	waitFor(t, "доставку строки", func() bool { return len(sink.got()) == 1 })

	// Стрим должен сам убраться из реестра — иначе повторный Start будет
	// считать его работающим и логи больше не откроются.
	waitFor(t, "самоочистку реестра стримов", func() bool {
		ls.mu.Lock()
		defer ls.mu.Unlock()
		return len(ls.cancels) == 0
	})

	// И повторный запуск действительно работает.
	src.mu.Lock()
	src.streams = [][]byte{frames(1, "после перезапуска")}
	src.mu.Unlock()

	ls.Start("svc-1", 0)
	waitFor(t, "перезапуск стрима", func() bool { return len(sink.got()) == 2 })
	ls.Stop("svc-1")
}

// TestReconnectSkipsBacklog: после обрыва потока история повторно не тянется
// (tail=0), иначе после каждого реконнекта UI дублировал бы строки.
func TestReconnectSkipsBacklog(t *testing.T) {
	src := &fakeSource{streams: [][]byte{frames(1, "старое")}}
	sink := &fakeSink{subscribers: true} // подписчики есть → будет реконнект
	ls := NewLogStreamer(src, sink)
	ls.reconnectDelay = 10 * time.Millisecond // не ждём реальные 3с

	ls.Start("svc-1", 500)
	waitFor(t, "второе подключение", func() bool { return len(src.tailArgs()) >= 2 })
	ls.Stop("svc-1")

	tails := src.tailArgs()
	if tails[0] != 500 {
		t.Fatalf("первое подключение должно нести tail=500: %v", tails)
	}
	if tails[1] != 0 {
		t.Fatalf("после реконнекта бэклог не запрашивается: %v", tails)
	}
}

// TestSourceErrorDoesNotPanic: недоступный Docker не роняет панель.
func TestSourceErrorDoesNotPanic(t *testing.T) {
	src := &fakeSource{err: errors.New("docker недоступен")}
	sink := &fakeSink{subscribers: false}
	ls := NewLogStreamer(src, sink)

	ls.Start("svc-1", 0)
	waitFor(t, "попытку подключения", func() bool { return src.callCount() >= 1 })
	ls.Stop("svc-1")

	if len(sink.got()) != 0 {
		t.Fatalf("при ошибке источника строк быть не должно: %+v", sink.got())
	}
}

// TestStopUnknownService: остановка незапущенного стрима безопасна.
func TestStopUnknownService(t *testing.T) {
	ls := NewLogStreamer(&fakeSource{}, &fakeSink{})
	ls.Stop("никогда-не-запускался") // не должно паниковать
}
