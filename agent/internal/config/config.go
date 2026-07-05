// Package config — настройки AGENT из переменных окружения (разд. 2.3 ТЗ).
package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

// Значения по умолчанию.
const (
	defaultPanelURL       = "http://panel:9000"
	defaultInterval       = 3 * time.Second
	defaultBufferAge      = 60 * time.Second // буферизация при недоступности panel
	defaultAgentTokenFile = "/run/secrets/agent_token"
	defaultTopContainers  = 20 // топ-N контейнеров в батче (NFR-2a)
)

// Config — конфигурация агента.
type Config struct {
	// NodeID — ID ноды Swarm из env NODE_ID ({{.Node.ID}} в stack file).
	NodeID string
	// PanelURL — базовый URL панели (env PANEL_URL).
	PanelURL string
	// AgentToken — shared-token для заголовка X-Agent-Token.
	AgentToken string
	// Interval — период сбора метрик (env DSMS_INTERVAL, по умолчанию 3s).
	Interval time.Duration
	// BufferAge — сколько держать неотправленные точки (60с по ТЗ).
	BufferAge time.Duration
	// TopContainers — сколько контейнеров слать в батче (топ по CPU/RAM).
	TopContainers int
	// Listen — адрес локального HTTP API агента (df/volumes/prune, разд. 2.3).
	Listen string
}

// Load читает конфигурацию; NODE_ID обязателен — без него панель
// не сможет привязать метрики к ноде.
func Load() (*Config, error) {
	cfg := &Config{
		NodeID:        os.Getenv("NODE_ID"),
		PanelURL:      envOr("PANEL_URL", defaultPanelURL),
		Interval:      defaultInterval,
		BufferAge:     defaultBufferAge,
		TopContainers: defaultTopContainers,
		Listen:        envOr("DSMS_AGENT_LISTEN", ":9001"),
	}
	if cfg.NodeID == "" {
		return nil, errors.New("NODE_ID is required (set {{.Node.ID}} in stack file)")
	}
	if v := os.Getenv("DSMS_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, errors.New("DSMS_INTERVAL: invalid duration")
		}
		cfg.Interval = d
	}

	// Токен: Docker secret → env (для локальной разработки).
	path := envOr("DSMS_AGENT_TOKEN_FILE", defaultAgentTokenFile)
	if b, err := os.ReadFile(path); err == nil {
		cfg.AgentToken = strings.TrimSpace(string(b))
	} else {
		cfg.AgentToken = strings.TrimSpace(os.Getenv("DSMS_AGENT_TOKEN"))
	}
	return cfg, nil
}

// envOr возвращает значение переменной окружения либо значение по умолчанию.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
