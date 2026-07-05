# DSMS Panel

Backend-компонент DSMS: REST API + WebSocket, прокси над Docker Engine API,
приём метрик от агентов, аутентификация. Живёт только на manager-ноде.

## API (этап 1)

Базовый префикс `/api/v1`, полная спецификация — раздел 4 ТЗ.

| Метод | Путь | Auth | Назначение |
|---|---|---|---|
| GET | `/healthz` | — | liveness |
| GET | `/setup` | — | нужен ли setup-экран |
| POST | `/setup` | — | создание admin (только пока нет пользователей) |
| POST | `/login` | rate-limit | вход, выдаёт cookie-сессию + CSRF |
| POST | `/logout` | сессия | выход |
| GET | `/me` | сессия | текущий пользователь |
| GET | `/cluster` | сессия | сводка кластера |
| GET | `/nodes` | сессия | ноды + последние live-метрики |
| GET | `/nodes/{id}` | сессия | детали ноды + задачи |
| POST | `/nodes/{id}/role` | сессия | promote/demote (guard: последний manager) |
| POST | `/nodes/{id}/availability` | сессия | active/pause/drain (guard: последний активный manager) |
| PUT | `/nodes/{id}/labels` | сессия | полная замена labels |
| DELETE | `/nodes/{id}?force=` | сессия | удаление (без force — только down) |
| GET | `/swarm/join-tokens` | сессия | join-токены + адрес manager |
| POST | `/swarm/join-tokens/rotate` | сессия | ротация токенов |
| GET | `/metrics/nodes/{id}` | сессия | live-окно метрик (история — этап 7) |
| GET | `/ws` | сессия | WebSocket (топики metrics/logs/events/alerts) |
| POST | `/ingest` | X-Agent-Token | приём метрик от агентов |

## Запуск

```bash
# DSMS_COOKIE_INSECURE=1 — только для локальной разработки без TLS
DSMS_DB=/tmp/dsms.db DSMS_AGENT_TOKEN=dev-token DSMS_COOKIE_INSECURE=1 go run ./cmd/panel
```

Переменные окружения: `DSMS_LISTEN` (`:9000`), `DSMS_DB`, `DSMS_AGENT_TOKEN`
(или secret-файл `DSMS_AGENT_TOKEN_FILE`, по умолчанию `/run/secrets/agent_token`),
`ADMIN_PASSWORD_HASH` (опционально), `DSMS_COOKIE_INSECURE=1` (снимает флаг
Secure с cookie — никогда не использовать в проде).

Тесты: `go test ./...` (интеграционные тесты API — без Docker daemon).

Перед `go build` должна быть собрана статика фронтенда:
`cd ../frontend && npm run build` → `web/dist` (встраивается go:embed).

## Пакеты

- `internal/config` — настройки из env/secrets
- `internal/store` — SQLite (schema раздела 5 ТЗ), пользователи, audit
- `internal/auth` — сессии, bcrypt, rate-limit, CSRF
- `internal/dockerapi` — Docker SDK c WithAPIVersionNegotiation
- `internal/metrics` — типы ingest + кольцевой буфер live-метрик
- `internal/ws` — WebSocket-hub с протоколом подписок
- `internal/api` — chi-роутер, handlers, middleware
- `web` — go:embed статики SPA
