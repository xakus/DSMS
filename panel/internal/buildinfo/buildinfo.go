// Package buildinfo хранит версию сборки панели (NFR-11).
//
// Значения подставляются линкером:
//
//	go build -ldflags "-X github.com/xakus/DSMS/panel/internal/buildinfo.Version=v2.0.0 \
//	                   -X github.com/xakus/DSMS/panel/internal/buildinfo.Commit=abc1234"
//
// Зачем: раньше версия бралась только из package.json фронта, поэтому
// расхождение «свежий бандл в браузере, старый бинарник на сервере» (или
// наоборот) было невидимым. Теперь версию отдаёт сам сервер в /healthz.
package buildinfo

// Значения по умолчанию — для локальной сборки без ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
)

// String — компактное представление «версия (коммит)» для логов.
func String() string {
	if Commit == "unknown" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
