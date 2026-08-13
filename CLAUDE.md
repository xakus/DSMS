# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Текущее состояние

Техническое задание — `dsms-tz-v1.md` (**версия 1.2**) — единственный источник истины по архитектуре и требованиям; перед реализацией любой части сверяйся с ним. FR-нумерация в ТЗ (FR-01…FR-14) — опорная система координат: в коде и коммитах ссылайся на FR-номера.

Название продукта — **DSMS** (Docker Swarm Management System). Репозиторий: `git@github.com:xakus/DSMS.git`. **Все 8 этапов v1 реализованы** (статусы — `docs/plans/README.md`); осталась приёмка на живом Swarm и тег `v1.0.0`. **Открыт скоуп v2: этап 9 — деплой стека из compose-файла (FR-14).** Ключевые архитектурные факты сверх ТЗ: агент имеет docker.sock и локальный HTTP API `:9001` (df/volumes/prune — панель адресует его по IP из ingest); стек — агрегация по label, не объект (но FR-14 добавляет managed-стеки с сохранённым исходником в БД); Stop сервиса запоминает реплики в `service_state`.

**FR-14 (деплой стека из файла):** пакет `internal/stackspec` — чистый конвертер compose→swarm на `compose-spec/compose-go/v2` (парсинг+интерполяция) + свой маппинг в `swarm.ServiceSpec` (НЕ docker/cli — образ остаётся ~15 МБ; конвертер уважает `UpdateConfig.Order` из файла, не навязывает start-first). Оркестратор `applyStack` в `handlers_stackdeploy.go`: сети → create/update сервисов по имени `<stack>_<svc>` → prune (по флагу). Исходник YAML+env хранится в таблице `stacks` (env шифруется AES-GCM, как registries). Эндпоинты: `POST /stacks/validate` (dry-run), `POST /stacks` (создать+развернуть), `PUT /stacks/{name}`, `GET /stacks/{name}/source`. Фронт: `components/StackDeployModal.vue` + кнопка/бейдж в `StacksView.vue`.

## Команды

Go лежит в `~/.local/opt/go/bin` (может отсутствовать в PATH). Node 22+.

```bash
# Фронтенд собирается ПЕРВЫМ: go:embed требует panel/web/dist
cd frontend && npm install && npm run build   # vue-tsc typecheck + vite build

# Панель: сборка, vet, тесты (тесты API не требуют Docker daemon)
cd panel && go build ./... && go vet ./... && go test ./...

# Один тест: go test ./internal/api -run TestAuthFlow

# Агент
cd agent && go build ./... && go vet ./...

# Локальный запуск (dev): panel + agent + фронт с hot-reload
DSMS_DB=/tmp/dsms.db DSMS_AGENT_TOKEN=dev-token DSMS_COOKIE_INSECURE=1 go run ./cmd/panel   # в panel/
NODE_ID=local-dev PANEL_URL=http://localhost:9000 DSMS_AGENT_TOKEN=dev-token go run ./cmd/agent  # в agent/
npm run dev   # в frontend/, проксирует /api на :9000

# Docker-образы (контекст сборки — корень репозитория!)
docker build -f panel/Dockerfile -t ghcr.io/xakus/dsms-panel:latest .
docker build -f agent/Dockerfile -t ghcr.io/xakus/dsms-agent:latest .
```

Нюансы: `DSMS_COOKIE_INSECURE=1` снимает Secure с cookie — только dev, в тестах вместо него `CookieSecure: false`. Docker SDK запинен на `v28.5.2+incompatible` (v29 разбит на модули `moby/moby/*` и конфликтует); опции списков — в `api/types/swarm` (`swarm.NodeListOptions`). SQLite — `modernc.org/sqlite` (чистый Go, без cgo — обязательное условие для scratch-образа). WebSocket — `github.com/coder/websocket` (актуальное имя nhooyr.io/websocket).

## Что это за продукт

Веб-панель для управления production-кластером **Docker Swarm**: живой мониторинг ресурсов нод (CPU/RAM/Disk/Network), полное управление сервисами (start/stop/redeploy/scale/логи), управление нодами (promote/demote, drain, добавление), стриминг логов. Замена устаревшим Swarmpit/Portainer CE. Сама панель разворачивается одной командой `docker stack deploy`.

## Архитектура (три компонента, три отдельных проекта)

Согласно ТЗ проект состоит из трёх независимых частей. **Каждая — в своей подпапке, без общего кода между ними** (даже ценой дублирования DTO).

1. **PANEL** (backend, Go 1.25+) — размещается только на manager-ноде (`constraint: node.role == manager`, нужен доступ к `/var/run/docker.sock`).
   - REST API + WebSocket hub (префикс `/api/v1`).
   - Клиент Docker Engine API: `github.com/docker/docker/client` с **обязательным** `client.WithAPIVersionNegotiation()` (иначе несовместимость версий API — известный риск).
   - Приём метрик от агентов: `POST /api/v1/ingest`, авторизация по `X-Agent-Token` (Docker secret).
   - Хранение метрик трёхуровневое: **живые точки (шаг 1–30с) — кольцевой буфер в памяти И таблица `metrics_live`** (ретенция ~20 мин; при старте панели буфер восстанавливается из БД — 15-мин график переживает рестарт, см. `internal/history/warmup.go`); **1-мин агрегаты — в `metrics_1m`**, ретенция 7 дней, фоновая очистка + VACUUM (`internal/history/aggregator.go`).
   - Auth: сессии (HttpOnly/Secure/SameSite=Strict cookie, TTL 24ч), пароли bcrypt cost 12, CSRF-токены на мутирующие запросы, rate-limit на `/login`.
   - Стек: `net/http` + `chi` router, `github.com/coder/websocket`. Минимум зависимостей.
   - Финальный образ ≤ 25 МБ (multi-stage → scratch/alpine).

2. **AGENT** (Go 1.25+) — деплой `mode: global` (автоматически на каждой ноде, включая добавленные позже).
   - Метрики через `github.com/shirou/gopsutil/v4`. Интервал сбора — **серверная настройка** `metrics.interval_sec` (1..30с, дефолт 3с): агент раз в 10с тянет её через `GET /api/v1/agent/config` (`internal/remotecfg`).
   - Хост монтируется read-only: `/proc→/host/proc`, `/sys→/host/sys`, `/→/host/rootfs`; env `HOST_PROC`/`HOST_SYS` для gopsutil.
   - Идентификация ноды по `NODE_ID={{.Node.ID}}` (Swarm template в env).
   - Шлёт JSON-батч на `POST http://panel:9000/api/v1/ingest`. При недоступности panel — буфер до 60с, потом дроп старых точек.
   - Образ ≤ 15 МБ (scratch), ≤ 20 МБ RAM, < 0.5% CPU.

3. **FRONTEND** (Vue 3 Composition API + Vite + TypeScript).
   - UI-kit **Naive UI** (тёмная тема по умолчанию, светлая переключателем).
   - Графики — **uPlot** (лёгкий, тянет живые данные).
   - Состояние — **Pinia**. i18n — **vue-i18n** с JSON-словарями (`src/i18n/*.json`: EN базовый, RU; AZ добавляется тривиально). Иконки — `@vicons/ionicons5`.
   - Роутинг — **vue-router**; экраны в `src/views/*.vue` (Login, Setup, Dashboard, NodeDetail, Services/ServiceDetail, Stacks, Logs, Events, Resources, DiskUsage, Alerts, Settings).
   - **Собирается в статику и встраивается в бинарник panel через `go:embed`** — один образ, один процесс. Никаких перезагрузок страницы: SPA + WebSocket.

## Поток данных

```
Browser (SPA) ──HTTPS(за Nginx Proxy Manager)──> PANEL <──HTTP push метрик── AGENT (на каждой ноде)
                                                    │
                                          docker.sock (Docker Engine API)
```

- **Живые данные идут через WebSocket** (`/api/v1/ws`), один мультиплексированный канал с протоколом подписок (`{"op":"sub","topic":"metrics|logs|events"}`). Ping/pong 30с, клиент переподключается с восстановлением подписок.
- Метрики агент→panel и формат ingest — см. раздел 4.3 ТЗ.
- Панель — **только наблюдатель/управлятор**: её потеря не влияет на кластер, своего state в кластере она не держит (NFR-6).

## Ключевые доменные правила (легко упустить)

- **Stop сервиса = scale 0**, но прежнее число реплик сохраняется в label панели (таблица `service_state`), чтобы Start вернул как было.
- **Redeploy = `ForceUpdate++`** (аналог `docker service update --force`), не пересоздание. Это же применяется к «Redeploy stack» (FR-08) — force update каждому сервису стека.
- **Deploy vs Redeploy** — разные операции: `redeploy` только форсит рестарт на текущем образе; **`deploy`** (`POST /services/{id}/deploy`, `POST /stacks/{name}/deploy`) подтягивает свежий образ по тому же тегу (обновление до latest-дайджеста). `image` (`POST /services/{id}/image`) меняет образ на указанный; `rollback` откатывает на предыдущую спецификацию сервиса.
- **Deploy НЕ трогает `UpdateConfig` (order/parallelism) — уважает то, что задано в стеке/compose.** Урок: раньше `deployService` навязывал `Order = start-first` каждому сервису — на нодах с нехваткой памяти новая реплика не поднималась («insufficient resources»), update зависал в `updating`, старая задача не убивалась, а каждый повторный клик «Деплой» добавлял ещё задачу (реплики росли как 8/1, 4/1). Не навязывать order/parallelism. На фронте окно подтверждения деплоя закрывается сразу (fire-and-forget + тост), иначе долгий деплой держит `loading` и провоцирует повторные клики.
- **Ноды**: помимо promote/demote/drain (`role`, `availability`) — редактирование label'ов (`PUT /nodes/{id}/labels`) и получение/ротация join-токенов кластера (`GET`/`POST /swarm/join-tokens[/rotate]`) для добавления новых нод.
- **Защита от выстрела в ногу**: запрет demote/drain последнего manager; предупреждение при потере кворума (чётное число manager'ов); удаление ноды — только для `down`, force — с двойным подтверждением.
- Группировка сервисов по стекам — через label `com.docker.stack.namespace` (FR-08 работает с этой же группировкой, стек — не отдельный объект Docker, а агрегация по label).
- **Secrets неизвлекаемы** (FR-09): значение шлётся в Docker один раз при создании, обратно не читается и никогда не логируется. Configs — читаемы.
- **Prune (FR-11) выполняется per-node**: панель адресует конкретный Docker Engine, операции последовательные (не параллельный шторм на docker.sock). Только неиспользуемые по умолчанию; «включая используемые» из UI недоступно.
- **Registry auth (FR-13)**: пароли реестров хранятся зашифрованными (AES-GCM, ключ `DSMS_ENCRYPTION_KEY` из Docker secret), никогда не отдаются клиенту и не пишутся в audit. При Update Image панель формирует заголовок `X-Registry-Auth`.
- **In-app алерты (FR-12)**: движок правил на panel считается на каждом батче метрик/событии, доставка — WebSocket topic `alerts`. Внешние каналы (Telegram) — v2, но канал доставки проектируется подключаемым.
- **RBAC-задел (FR-07)**: в v1 один admin, но `users.role`, `session.user_id`, `audit.user_id` заложены с самого начала — роли в v2 без миграции схемы. Audit ссылается на `user_id`, не на строку-имя.
- Все опасные операции (remove node/service/stack/secret/volume/network, demote, prune) → подтверждение в UI + запись в `audit` (SQLite).
- Локализуется только пользовательский текст; структура i18n — JSON-словари.

## Модель данных (SQLite)

Таблицы (`internal/store/store.go`): `users` (с полем `role`), `metrics_1m` (агрегаты 60с, PK `(node_id, ts)`), `metrics_live` (живые точки 1–30с, ретенция ~20 мин, для warmup буфера после рестарта), `audit` (ссылка на `user_id`), `service_state`, `registries` (`password_enc`), `alerts`, `settings`. Полные определения — раздел 5 ТЗ. Per-container метрики в БД не попадают — только в память. Secrets/configs/networks/volumes в SQLite **не дублируются** — читаются напрямую из Docker API.

## Безопасность (раздел 7 ТЗ — не игнорировать)

Доступ к `docker.sock` = root кластера, поэтому: панель **не публикует порт наружу напрямую** — только overlay-сеть + reverse proxy (Nginx Proxy Manager) с HTTPS. Rate-limit, CSRF, audit — обязательны. В документации нужен обязательный раздел про reverse proxy.

## Деплой

Эталонный `dsms.yml` (stack file) и процедура установки — раздел 7 ТЗ. Установка:

```bash
openssl rand -hex 32 | docker secret create agent_token -
docker stack deploy -c dsms.yml dsms
```

Требования: Docker Engine ≥ 24.x со Swarm mode; образы multi-arch (amd64 + arm64), сборка в GitHub Actions.

## Порядок работ

План разбит на 8 этапов (раздел 9 ТЗ), ~7–8 недель. MVP — этапы 1–4 (skeleton+auth+SPA → dashboard/ноды → сервисы+стеки → логи/события/audit), ~4 недели. Дальше: Resources (Secrets/Configs/Networks/Volumes + prune, этап 5), алерты (этап 6), история метрик + Settings (этап 7), полировка/безопасность/CI (этап 8). Реализуй в этом порядке: зависимости идут снизу вверх (agent+ingest → метрики по WS → управление объектами).
