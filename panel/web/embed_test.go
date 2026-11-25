// Тесты раздачи интерфейса: что уходит в index.html, а что — честный 404.
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// get выполняет запрос к обработчику SPA.
func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	SPAHandler()(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// Адрес экрана (клиентский роутинг) отдаёт index.html и не кэшируется:
// после обновления панели браузер обязан получить свежие ссылки на файлы.
func TestSPAFallbackServesIndex(t *testing.T) {
	for _, path := range []string{"/", "/services", "/stacks/shad"} {
		rec := get(t, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: код %d, ожидался 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: Content-Type %q, ожидался text/html", path, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("%s: Cache-Control %q, ожидался no-cache", path, cc)
		}
	}
}

// Файла сборки с таким именем нет (вкладка со старым интерфейсом после
// обновления панели) — 404, а не index.html: HTML вместо скрипта браузер
// молча отбрасывает, и экран после входа не открывается.
func TestMissingAssetIsNotFound(t *testing.T) {
	rec := get(t, "/assets/DashboardView-0ldHash0.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Fatal("вместо 404 отдан HTML интерфейса")
	}
}
