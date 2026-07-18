// Handler истории логов сервиса (FR-05): постраничная выборка для входа в
// экран и подгрузки старого по скроллу вверх. Live-поток идёт отдельно по
// WebSocket (topic=logs, tail=0) — здесь только исторический снапшот.
package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xakus/DSMS/panel/internal/streams"
)

// maxLogPage — верхний предел строк на одну страницу истории.
const maxLogPage = 1000

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

	rc, err := h.Docker.ServiceLogsSnapshot(r.Context(), id, tail, until)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "docker logs error")
		return
	}
	defer rc.Close()

	lines := streams.ParseSnapshot(rc)
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

	writeJSON(w, map[string]any{
		"lines":    lines,
		"oldest":   oldest,  // курсор для следующей страницы (?until=oldest)
		"has_more": hasMore, // есть ли ещё более старые строки
	})
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
