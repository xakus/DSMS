// Package buildinfo хранит версию сборки агента (NFR-11).
//
// Значения подставляются линкером:
//
//	go build -ldflags "-X github.com/xakus/DSMS/agent/internal/buildinfo.Version=v2.0.0 \
//	                   -X github.com/xakus/DSMS/agent/internal/buildinfo.Commit=abc1234"
//
// Агент — global-сервис на всех нодах, и при частичном обновлении кластера
// версии на нодах расходятся. Версия в логе и в /healthz показывает, какая
// нода на какой сборке.
package buildinfo

// Значения по умолчанию — для локальной сборки без ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
)
