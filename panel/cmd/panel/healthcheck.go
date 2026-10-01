// Режим самопроверки для Docker HEALTHCHECK (A-23).
//
// Финальный образ панели — scratch: в нём нет ни shell, ни curl/wget, поэтому
// проверять здоровье нечем. Стандартное решение — сам бинарник умеет ходить
// к себе: HEALTHCHECK CMD ["/panel", "-healthcheck"].
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// healthcheckFlag — аргумент, включающий режим проверки вместо запуска сервера.
const healthcheckFlag = "-healthcheck"

// isHealthcheckMode — вызван ли бинарник как проба.
func isHealthcheckMode(args []string) bool {
	for _, a := range args[1:] {
		if a == healthcheckFlag || a == "--healthcheck" {
			return true
		}
	}
	return false
}

// runHealthcheck опрашивает собственный /healthz и возвращает код выхода:
// 0 — здоров, 1 — нет (как ожидает Docker).
func runHealthcheck() int {
	// Слушающий адрес берём из той же переменной, что и сервер; ":9000" →
	// стучимся на loopback, а не на все интерфейсы.
	addr := os.Getenv("DSMS_LISTEN")
	if addr == "" {
		addr = ":9000"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/api/v1/healthz", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
