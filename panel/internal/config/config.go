// Package config — все настройки PANEL из переменных окружения.
// Настройки не хардкодятся в коде (правило проекта): всё, что можно менять
// при деплое, живёт здесь и в stack file (dsms.yml).
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Пути и значения по умолчанию.
const (
	defaultListen         = ":9000"
	defaultDBPath         = "/data/dsms.db"
	defaultSessionTTL     = 24 * time.Hour
	defaultAgentTokenFile = "/run/secrets/agent_token" // Docker secret (FR-07 3.7.4)
)

// Config — конфигурация процесса panel.
type Config struct {
	// Listen — адрес HTTP-listener'а (env DSMS_LISTEN, по умолчанию :9000).
	Listen string
	// DBPath — путь к файлу SQLite на volume (env DSMS_DB).
	DBPath string
	// AgentToken — shared-token для авторизации агентов на /ingest.
	// Читается из Docker secret (файл), fallback — env DSMS_AGENT_TOKEN (для локальной разработки).
	AgentToken string
	// AdminPasswordHash — bcrypt-хэш пароля admin из env ADMIN_PASSWORD_HASH (3.7.1).
	// Пустой — пароль задаётся через setup-экран при первом запуске.
	AdminPasswordHash string
	// SessionTTL — время жизни сессии (3.7.2: 24 ч).
	SessionTTL time.Duration
	// CookieSecure — флаг Secure на сессионной cookie (3.7.2).
	// Выключается только для локальной разработки без TLS: DSMS_COOKIE_INSECURE=1.
	CookieSecure bool
}

// Load собирает конфигурацию из окружения и секретов.
func Load() (*Config, error) {
	cfg := &Config{
		Listen:            envOr("DSMS_LISTEN", defaultListen),
		DBPath:            envOr("DSMS_DB", defaultDBPath),
		AdminPasswordHash: os.Getenv("ADMIN_PASSWORD_HASH"),
		SessionTTL:        defaultSessionTTL,
		CookieSecure:      os.Getenv("DSMS_COOKIE_INSECURE") != "1",
	}

	token, err := loadAgentToken()
	if err != nil {
		return nil, fmt.Errorf("agent token: %w", err)
	}
	cfg.AgentToken = token

	return cfg, nil
}

// loadAgentToken читает токен агентов: сначала Docker secret, затем env.
// Отсутствие токена — не фатально для старта (ingest будет отклонять всё),
// но пишется предупреждение на уровне вызывающего кода.
func loadAgentToken() (string, error) {
	path := envOr("DSMS_AGENT_TOKEN_FILE", defaultAgentTokenFile)
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(os.Getenv("DSMS_AGENT_TOKEN")), nil
}

// envOr возвращает значение переменной окружения либо значение по умолчанию.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
