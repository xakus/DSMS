// Интеграционные тесты API этапа 1: setup → login → CSRF → ingest.
// Поднимают полный роутер с временной SQLite и httptest-сервером;
// Docker daemon не требуется (эндпоинты с Docker API не трогаем).
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xakus/DSMS/panel/internal/auth"
	"github.com/xakus/DSMS/panel/internal/config"
	"github.com/xakus/DSMS/panel/internal/crypto"
	"github.com/xakus/DSMS/panel/internal/metrics"
	"github.com/xakus/DSMS/panel/internal/store"
	"github.com/xakus/DSMS/panel/internal/ws"
)

// newTestServer собирает роутер с временной БД, fake-Docker и тестовым agent-token.
func newTestServer(t *testing.T, docker *fakeDocker) (*httptest.Server, *metrics.ClusterBuffer) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if docker == nil {
		docker = &fakeDocker{}
	}
	// CookieSecure=false: httptest работает по http, Secure-cookie jar не примет.
	cfg := &config.Config{AgentToken: "test-token", SessionTTL: time.Hour, CookieSecure: false}
	buf := metrics.NewClusterBuffer(15*time.Minute, 3*time.Second)

	// Тестовый ключ шифрования (32 байта) для registries (FR-13).
	box, err := crypto.NewBox(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}

	srv := httptest.NewServer(NewRouter(Deps{
		Cfg:      cfg,
		Store:    db,
		Docker:   docker,
		Buffer:   buf,
		Hub:      ws.NewHub(),
		Sessions: auth.NewManager(db, cfg.SessionTTL),
		Crypto:   box,
	}))
	t.Cleanup(srv.Close)
	return srv, buf
}

// loginClient делает setup+login и возвращает клиент с cookie и CSRF-токеном.
func loginClient(t *testing.T, srv *httptest.Server) (*http.Client, string) {
	t.Helper()
	jar := newCookieClient(t)
	resp := postJSON(t, jar, srv.URL+"/api/v1/setup",
		map[string]string{"username": "admin", "password": "secret123"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: got %d", resp.StatusCode)
	}
	resp = postJSON(t, jar, srv.URL+"/api/v1/login",
		map[string]string{"username": "admin", "password": "secret123"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: got %d", resp.StatusCode)
	}
	var login struct {
		CSRF string `json:"csrf"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&login)
	return jar, login.CSRF
}

// postJSON — POST с JSON-телом и произвольными заголовками.
func postJSON(t *testing.T, client *http.Client, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	return resp
}

// TestAuthFlow проверяет полный цикл: setup → login → me → CSRF → logout.
func TestAuthFlow(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	jar := newCookieClient(t)

	// healthz без auth
	resp, err := http.Get(srv.URL + "/api/v1/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %v %v", err, resp.StatusCode)
	}

	// setup обязателен: пользователей нет
	resp, _ = http.Get(srv.URL + "/api/v1/setup")
	var st struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if !st.NeedsSetup {
		t.Fatal("expected needs_setup=true on fresh db")
	}

	// создание admin
	resp = postJSON(t, jar, srv.URL+"/api/v1/setup",
		map[string]string{"username": "admin", "password": "secret123"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: got %d", resp.StatusCode)
	}

	// повторный setup закрыт навсегда
	resp = postJSON(t, jar, srv.URL+"/api/v1/setup",
		map[string]string{"username": "evil", "password": "password1"}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup: want 409, got %d", resp.StatusCode)
	}

	// неверный пароль
	resp = postJSON(t, jar, srv.URL+"/api/v1/login",
		map[string]string{"username": "admin", "password": "wrong"}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login: want 401, got %d", resp.StatusCode)
	}

	// верный пароль → cookie + CSRF
	resp = postJSON(t, jar, srv.URL+"/api/v1/login",
		map[string]string{"username": "admin", "password": "secret123"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: got %d", resp.StatusCode)
	}
	var login struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		CSRF     string `json:"csrf"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&login)
	if login.Role != "admin" || login.CSRF == "" {
		t.Fatalf("login response incomplete: %+v", login)
	}

	// /me под сессией
	resp, _ = jar.Get(srv.URL + "/api/v1/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: got %d", resp.StatusCode)
	}

	// мутирующий запрос без CSRF → 403
	resp = postJSON(t, jar, srv.URL+"/api/v1/logout", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout w/o csrf: want 403, got %d", resp.StatusCode)
	}

	// с CSRF → ok
	resp = postJSON(t, jar, srv.URL+"/api/v1/logout", nil,
		map[string]string{"X-CSRF-Token": login.CSRF})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: got %d", resp.StatusCode)
	}

	// после logout сессии нет
	resp, _ = jar.Get(srv.URL + "/api/v1/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d", resp.StatusCode)
	}
}

// TestIngest проверяет авторизацию агентов и попадание метрик в буфер.
func TestIngest(t *testing.T) {
	srv, buf := newTestServer(t, nil)
	client := &http.Client{}

	snap := map[string]any{
		"node_id": "n1", "hostname": "host1", "ts": time.Now().Unix(),
		"cpu": map[string]any{"total_pct": 42.5},
	}

	// без токена → 401
	resp := postJSON(t, client, srv.URL+"/api/v1/ingest", snap, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ingest w/o token: want 401, got %d", resp.StatusCode)
	}

	// с неверным токеном → 401
	resp = postJSON(t, client, srv.URL+"/api/v1/ingest", snap,
		map[string]string{"X-Agent-Token": "bad"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ingest bad token: want 401, got %d", resp.StatusCode)
	}

	// с верным → 204 и метрика в буфере
	resp = postJSON(t, client, srv.URL+"/api/v1/ingest", snap,
		map[string]string{"X-Agent-Token": "test-token"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ingest: want 204, got %d", resp.StatusCode)
	}
	got, ok := buf.Latest("n1")
	if !ok || got.CPU.TotalPct != 42.5 {
		t.Fatalf("buffer: want cpu 42.5, got %+v ok=%v", got, ok)
	}
}

// TestRequireSession: закрытые эндпоинты недоступны без cookie.
func TestRequireSession(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	resp, _ := http.Get(srv.URL + "/api/v1/cluster")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cluster w/o session: want 401, got %d", resp.StatusCode)
	}
}

// newCookieClient — HTTP-клиент с cookie-jar (сессионная cookie между запросами).
func newCookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := newJar()
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}
