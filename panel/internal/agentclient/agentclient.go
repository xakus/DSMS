// Package agentclient — вызовы локального HTTP API агентов (разд. 2.3 ТЗ):
// df / volumes / prune на конкретной ноде. Адрес агента панель узнаёт
// из RemoteAddr ingest-запросов (node_id → IP).
package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Directory — реестр адресов агентов: node_id → IP (обновляется на ingest).
type Directory struct {
	mu    sync.RWMutex
	addrs map[string]string
}

// NewDirectory создаёт пустой справочник.
func NewDirectory() *Directory {
	return &Directory{addrs: make(map[string]string)}
}

// Set запоминает адрес агента ноды.
func (d *Directory) Set(nodeID, ip string) {
	d.mu.Lock()
	d.addrs[nodeID] = ip
	d.mu.Unlock()
}

// Get возвращает адрес агента; ok=false — агент ноды ещё не объявлялся.
func (d *Directory) Get(nodeID string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ip, ok := d.addrs[nodeID]
	return ip, ok
}

// All возвращает копию справочника (для fan-out по всем нодам).
func (d *Directory) All() map[string]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]string, len(d.addrs))
	for k, v := range d.addrs {
		out[k] = v
	}
	return out
}

// Client — HTTP-клиент к API агентов.
type Client struct {
	http  *http.Client
	token string // X-Agent-Token (тот же shared-token, 3.7.4)
	port  string // порт API агентов (по умолчанию 9001)
}

// New создаёт клиента агентов.
func New(token, port string) *Client {
	if port == "" {
		port = "9001"
	}
	return &Client{
		// Prune может работать долго (удаление больших образов).
		http:  &http.Client{Timeout: 120 * time.Second},
		token: token,
		port:  port,
	}
}

// do выполняет запрос к агенту и декодирует JSON-ответ в out.
func (c *Client) do(ctx context.Context, method, ip, path string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	url := fmt.Sprintf("http://%s:%s%s", ip, c.port, path)
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("agent: %s", e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// DF запрашивает docker system df ноды (3.11.1).
func (c *Client) DF(ctx context.Context, ip string, out any) error {
	return c.do(ctx, http.MethodGet, ip, "/api/v1/df", nil, out)
}

// Volumes запрашивает локальные тома ноды (3.10.4).
func (c *Client) Volumes(ctx context.Context, ip string, out any) error {
	return c.do(ctx, http.MethodGet, ip, "/api/v1/volumes", nil, out)
}

// VolumeRemove удаляет том на ноде (3.10.5).
func (c *Client) VolumeRemove(ctx context.Context, ip, name string, force bool) error {
	return c.do(ctx, http.MethodPost, ip, "/api/v1/volumes/remove",
		map[string]any{"name": name, "force": force}, nil)
}

// Prune запускает очистку на ноде (3.11.2).
func (c *Client) Prune(ctx context.Context, ip string, targets []string, allImages bool, out any) error {
	return c.do(ctx, http.MethodPost, ip, "/api/v1/prune",
		map[string]any{"targets": targets, "all_images": allImages}, out)
}
