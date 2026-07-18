// Handler истории логов сервиса (FR-05): постраничная выборка для входа в
// экран и подгрузки старого по скроллу вверх. Live-поток идёт отдельно по
// WebSocket (topic=logs, tail=0) — здесь только исторический снапшот.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xakus/DSMS/panel/internal/streams"
)

// maxLogPage — верхний предел строк на одну страницу истории.
const maxLogPage = 1000

// logSnapshotTimeout — потолок ожидания снапшота. `docker service logs` умеет
// «залипать» на мёртвых задачах сервиса (особенно при большом tail): по дедлайну
// отдаём то, что успели прочитать, вместо бесконечного ожидания/пустого экрана.
const logSnapshotTimeout = 15 * time.Second

// safeLogTail — размер tail, который docker отдаёт стабильно; на него падаем
// откатом, если запрос с большим tail вернул пусто (см. serviceLogs).
const safeLogTail = 100

// serviceLogs — GET /services/{id}/logs?tail=N&until=RFC3339Nano.
//
// Возвращает последние N строк логов сервиса (по всем репликам), объединённых
// и отсортированных по времени. until листает историю назад: отдаются строки
// ДО указанного момента — так фронт подгружает предыдущие страницы.
func (h *handlers) serviceLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	tail := 500
	if v, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && v > 0 {
		tail = v
	}
	if tail > maxLogPage {
		tail = maxLogPage
	}
	until := r.URL.Query().Get("until")

	ctx, cancel := context.WithTimeout(r.Context(), logSnapshotTimeout)
	defer cancel()

	lines, err := h.snapshotLines(ctx, id, tail, until)
	if err != nil {
		slog.Error("service logs snapshot open failed",
			"service", id, "tail", tail, "until", until, "err", err)
		writeErr(w, http.StatusBadGateway, "docker logs error")
		return
	}

	// Обход бага docker: `service logs --tail N` при большом N на кластере с
	// мёртвыми задачами часто отдаёт пусто, а маленький tail — стабильно. Если
	// большой запрос вернул 0 строк — повторяем безопасным tail, чтобы показать
	// пользователю хотя бы последние строки, а не чёрный экран.
	if len(lines) == 0 && tail > safeLogTail {
		slog.Warn("empty snapshot, retrying with smaller tail",
			"service", id, "tail", tail, "retry_tail", safeLogTail)
		if retry, rerr := h.snapshotLines(ctx, id, safeLogTail, until); rerr == nil && len(retry) > 0 {
			lines = retry
		}
	}

	// Проставляем id сервиса (клиент сопоставляет строки с выбранным сервисом).
	for i := range lines {
		lines[i].Service = id
	}

	// Реплики читаются блоками — сшиваем в единую ленту по времени.
	sort.SliceStable(lines, func(i, j int) bool {
		return logTime(lines[i].TS).Before(logTime(lines[j].TS))
	})

	// has_more до обрезки: если реплики отдали больше страницы — старое ещё есть.
	hasMore := len(lines) >= tail && tail > 0
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}

	oldest := ""
	if len(lines) > 0 {
		oldest = lines[0].TS
	}

	// Диагностика: сколько строк реально отдал docker при этом tail (в т.ч. 0).
	// Пустой снапшот при непустом сервисе = сигнал проблемы `docker service logs`.
	if len(lines) == 0 {
		slog.Warn("service logs snapshot empty", "service", id, "tail", tail, "until", until)
	} else {
		slog.Info("service logs snapshot", "service", id, "tail", tail, "returned", len(lines))
	}

	writeJSON(w, map[string]any{
		"lines":    lines,
		"oldest":   oldest,  // курсор для следующей страницы (?until=oldest)
		"has_more": hasMore, // есть ли ещё более старые строки
	})
}

// snapshotLines открывает снапшот логов сервиса и разбирает его в строки,
// гарантированно закрывая поток. Вынесено ради отката на меньший tail.
func (h *handlers) snapshotLines(ctx context.Context, id string, tail int, until string) ([]streams.LogLine, error) {
	rc, err := h.Docker.ServiceLogsSnapshot(ctx, id, tail, until)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return streams.ParseSnapshot(rc), nil
}

// logTime парсит RFC3339Nano-метку строки лога для сортировки; при неудаче
// возвращает нулевое время (строка уедет в начало, порядок сохранит SliceStable).
func logTime(ts string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}
