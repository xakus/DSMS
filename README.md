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

## Деплой стека из файла (FR-14, v2)

Панель умеет разворачивать стек **из compose-файла прямо из UI** — как
`docker stack deploy`, но без командной строки. На экране **Стеки** →
кнопка **«Развернуть стек»**:

1. Вставьте `docker-stack.yml` (или загрузите файл). Поддержаны YAML-якоря,
   интерполяция `${VAR}` / `${VAR:-default}`, `deploy`, `extra_hosts`,
   `ports: mode: host`, overlay-сети.
2. Задайте переменные окружения: поле **.env** (строки `KEY=value`) и/или
   ручные пары (перекрывают .env). Значения хранятся зашифрованными.
3. **«Проверить»** — dry-run: покажет, какие сервисы и сети будут созданы.
4. **«Развернуть»** — панель создаёт сети → создаёт/обновляет сервисы.

Стек, развёрнутый из файла, помечается бейджем **«из файла»**: его YAML+env
хранятся в панели, можно **редактировать и передеплоить** одной кнопкой.
Опция **prune** (удалить сервисы, ушедшие из файла) по умолчанию выключена.

## Статус

**Все 8 этапов v1 реализованы** (план — [docs/plans/](docs/plans/README.md)):

1. ✅ Skeleton: auth + setup, ingest, WebSocket-hub, stack file
2. ✅ Dashboard + Node detail + управление нодами (FR-01..03)
3. ✅ Сервисы + стеки + registry auth (FR-04, FR-08, FR-13)
4. ✅ Логи + события + audit (FR-05, FR-06)
5. ✅ Secrets/Configs/Networks/Volumes + prune (FR-09..11)
6. ✅ In-app алерты (FR-12)
7. ✅ История метрик (окна 1ч–7дн) + Settings
8. ✅ Per-container метрики, security headers, CI (multi-arch)

Следующий шаг — приёмка на живом Swarm-кластере и тег `v1.0.0`
(release-workflow опубликует образы в ghcr.io).

**v2 (в работе):** этап 9 — деплой стека из compose-файла через UI (FR-14),
см. [docs/plans/stage-9-stack-deploy.md](docs/plans/stage-9-stack-deploy.md).
