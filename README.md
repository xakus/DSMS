# DSMS — Docker Swarm Management System

Веб-панель для управления production-кластером Docker Swarm: живые метрики нод,
управление сервисами/стеками/нодами, стриминг логов, события и audit.
Полное ТЗ — [dsms-tz-v1.md](dsms-tz-v1.md) (v1.1).

## Архитектура

| Компонент | Папка | Стек | Назначение |
|---|---|---|---|
| PANEL | [panel/](panel/) | Go 1.25, chi, SQLite | REST + WebSocket, Docker Engine API, приём метрик, auth |
| AGENT | [agent/](agent/) | Go 1.25, gopsutil | Сбор метрик хоста на каждой ноде (mode: global) |
| FRONTEND | [frontend/](frontend/) | Vue 3, Vite, Naive UI | SPA, встраивается в бинарник panel через go:embed |

```
Browser ──HTTPS(reverse proxy)──> PANEL <──HTTP push метрик── AGENT (на каждой ноде)
                                    │
                          docker.sock (Engine API)
```

Каждый компонент — независимый проект без общего кода (осознанное дублирование DTO).

## Разработка

Требуется Go 1.25+, Node 22+.

```bash
# 1. Фронтенд: собрать статику в panel/web/dist (нужно до go build)
cd frontend && npm install && npm run build

# 2. Панель: локальный запуск (DSMS_COOKIE_INSECURE — только для dev без TLS)
cd panel
DSMS_DB=/tmp/dsms.db DSMS_AGENT_TOKEN=dev-token DSMS_COOKIE_INSECURE=1 go run ./cmd/panel

# 3. Агент: локальный запуск (шлёт метрики на локальную панель)
cd agent
NODE_ID=local-dev PANEL_URL=http://localhost:9000 DSMS_AGENT_TOKEN=dev-token go run ./cmd/agent

# Дев-режим фронта с hot-reload (проксирует /api на localhost:9000)
cd frontend && npm run dev
```

Сборка образов (контекст — корень репозитория):

```bash
docker build -f panel/Dockerfile -t ghcr.io/xakus/dsms-panel:latest .
docker build -f agent/Dockerfile -t ghcr.io/xakus/dsms-agent:latest .
```

## Деплой в Swarm

```bash
openssl rand -hex 32 | docker secret create agent_token -
docker stack deploy -c dsms.yml dsms
```

**Безопасность:** panel не публикует порт наружу. Доступ — только через
reverse proxy (например, Nginx Proxy Manager) с HTTPS, подключённый к
overlay-сети `dsms`. Подробнее — раздел 7 ТЗ.

## Статус

Этап 1 из 8 (skeleton): auth + setup, ingest метрик, WebSocket-hub, сводка
кластера, SPA (login/dashboard), stack file. План работ — раздел 9 ТЗ.
