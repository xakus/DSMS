// Режим самопроверки для Docker HEALTHCHECK (A-23).
//
// Образ агента — scratch, curl/wget в нём нет, поэтому бинарник умеет
// опрашивать сам себя: HEALTHCHECK CMD ["/agent", "-healthcheck"].
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

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

// runHealthcheck опрашивает локальный API агента; 0 — здоров, 1 — нет.
func runHealthcheck() int {
	addr := os.Getenv("DSMS_AGENT_LISTEN")
	if addr == "" {
		addr = ":9001"
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
