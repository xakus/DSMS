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
			}
		}
		fileServer.ServeHTTP(w, r)
	}
}
