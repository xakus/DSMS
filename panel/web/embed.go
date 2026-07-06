// Package web — статика SPA, встроенная в бинарник через go:embed (разд. 2.4 ТЗ).
//
// Каталог dist/ наполняется сборкой фронтенда (frontend/ → panel/web/dist).
// В репозитории лежит только .gitkeep — сборка фронта обязательна перед go build.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// SPAHandler отдаёт файлы SPA; любой не-API путь без файла падает
// в index.html — client-side routing (разд. 6.1: никаких перезагрузок).
//
// Кэширование (иначе после обновления панели браузер держит старый index.html
// с ссылками на старые ассеты — приходится делать hard refresh):
//   - /assets/* — хэшированные имена, кэшируем «навечно» (immutable);
//   - index.html — no-cache, чтобы всегда подтянуть свежие ссылки на ассеты.
func SPAHandler() http.HandlerFunc {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // невозможно при корректной сборке
	}
	fileServer := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			if os.IsNotExist(err) {
				// SPA-fallback: отдать index.html, роутинг сделает Vue Router.
				r.URL.Path = "/"
				p = "index.html"
			}
		}

		// Хэшированные ассеты неизменны — можно кэшировать надолго.
		// Всё остальное (index.html) — не кэшировать, чтобы обновления доходили.
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	}
}
