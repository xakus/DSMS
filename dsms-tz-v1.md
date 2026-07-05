# DSMS — панель управления Docker Swarm
## Техническое задание, версия 1.1

**Дата:** 05.07.2026
**Статус:** черновик для утверждения
**Рабочее название:** Docker Swarm Management System (DSMS)

> **Изменения v1.1 относительно v1.0:** в скоуп v1 добавлены управление стеками целиком (FR-08), Secrets/Configs (FR-09), Networks/Volumes (FR-10), disk usage + prune (FR-11), in-app алерты (FR-12), метрики по контейнерам (расширение FR-02), registry auth для Update Image (FR-13) и архитектурный задел под RBAC (расширение FR-07). Telegram-нотификации, роли пользователей и exec-терминал остаются в v2.

---

## 1. Назначение и цели

### 1.1. Проблема

Существующие open-source панели управления Docker Swarm (Swarmpit, Portainer CE) устарели, используют несовместимые версии Docker API, создают паразитную нагрузку и не покрывают требуемый функционал (живые метрики нод, удобный редеплой, стриминг логов).

### 1.2. Цель

Единая веб-панель для управления production-кластером Docker Swarm:

- Живой мониторинг ресурсов каждой ноды (CPU, RAM, Network, Disk)
- Полное управление сервисами (start / stop / redeploy / scale / логи)
- Управление нодами (promote / demote, drain / active, добавление новых)
- Деплой самой панели одной командой `docker stack deploy`

### 1.3. Не-цели (v1)

- Управление несколькими кластерами из одной панели
- Kubernetes-совместимость
- CI/CD-пайплайны (только ручной force-redeploy и обновление image)
- Внешние нотификации (Telegram/email) — отложено в v2; в v1 только in-app алерты (FR-12)
- Роли пользователей (viewer/operator/admin) — в v1 один admin-аккаунт, но модель данных проектируется с заделом под RBAC (FR-07); сама реализация ролей — v2
- Exec-терминал в контейнер и деплой стеков из compose-файла через UI — v2

---

## 2. Архитектура

### 2.1. Компоненты

```
┌─────────────────────────────────────────────────────────┐
│                     Browser (SPA)                        │
│                 Vue 3 + WebSocket + REST                 │
└──────────────────────────┬──────────────────────────────┘
                           │ HTTPS (за Nginx Proxy Manager)
┌──────────────────────────▼──────────────────────────────┐
│  PANEL (backend, Go)                                     │
│  - REST API + WebSocket hub                              │
│  - Docker Engine API client (/var/run/docker.sock)      │
│  - Приём метрик от агентов                               │
│  - Кольцевой буфер метрик в памяти + SQLite для истории  │
│  - Auth (сессии, bcrypt)                                 │
│  constraint: node.role == manager                        │
└──────────────▲───────────────────────▲──────────────────┘
               │ HTTP push (метрики)   │
┌──────────────┴──────┐   ┌────────────┴──────┐
│  AGENT (Go)         │   │  AGENT (Go)       │   ... на каждой ноде
│  mode: global       │   │  mode: global     │
│  /proc, /sys :ro    │   │                   │
└─────────────────────┘   └───────────────────┘
```

### 2.2. Компонент PANEL

| Параметр | Значение |
|---|---|
| Язык | Go 1.23+ |
| Docker SDK | `github.com/docker/docker/client` (официальный) |
| HTTP-фреймворк | `net/http` + `chi` router (минимум зависимостей) |
| WebSocket | `nhooyr.io/websocket` |
| Хранилище | SQLite (файл на volume) — история метрик, пользователи, настройки |
| Образ | multi-stage build, финальный `scratch`/`alpine`, ≤ 25 МБ |
| Размещение | только manager-нода (нужен доступ к Swarm API через socket) |

**Обязанности:**

1. Прокси-слой над Docker Engine API (nodes, services, tasks, logs, swarm)
2. Приём метрик от агентов (`POST /api/v1/ingest`, авторизация shared-token)
3. Хранение метрик: последние 15 минут — в памяти (кольцевой буфер), агрегаты (1 мин) — в SQLite с ретенцией 7 дней
4. WebSocket-hub: рассылка живых метрик, событий Docker (`/events`), логов сервисов подписанным клиентам
5. Аутентификация и сессии

### 2.3. Компонент AGENT

| Параметр | Значение |
|---|---|
| Язык | Go 1.23+ |
| Библиотека метрик | `github.com/shirou/gopsutil/v4` |
| Режим деплоя | `mode: global` — автоматически поднимается на каждой ноде, включая добавленные позже |
| Образ | `scratch`, ≤ 15 МБ |
| Потребление | ≤ 20 МБ RAM, < 0.5% CPU |

**Собираемые метрики (интервал 3 сек, настраиваемый):**

| Группа | Метрики |
|---|---|
| CPU | загрузка общая и по ядрам (%), load average 1/5/15 |
| RAM | total / used / available / cached (байты, %) |
| Disk | по каждой точке монтирования: total / used / free; IOPS read/write; throughput МБ/с |
| Network | по интерфейсам: rx/tx байт/с, пакеты/с, errors, drops |
| Система | uptime, hostname, kernel, кол-во контейнеров на ноде |
| Контейнеры | per-container: id, имя, service/task, CPU %, RAM used/limit — читается из cgroups (`/host/sys/fs/cgroup`). Слать топ-N по CPU и RAM (N настраивается, по умолчанию 20), чтобы не раздувать батч на плотных нодах |

**Механика:**

- Хост-ресурсы монтируются read-only: `/proc → /host/proc`, `/sys → /host/sys`, `/ → /host/rootfs`
- Переменная `HOST_PROC=/host/proc` и т.д. для gopsutil
- Агент шлёт JSON-батч на panel: `POST http://panel:9000/api/v1/ingest` с заголовком `X-Agent-Token`
- Идентификация ноды — по `NODE_ID` из `{{.Node.ID}}` (Swarm template в env)
- При недоступности panel — буферизация до 60 сек, затем отбрасывание старых точек

### 2.4. Компонент FRONTEND

| Параметр | Значение |
|---|---|
| Фреймворк | Vue 3 (Composition API) + Vite + TypeScript |
| UI-kit | Naive UI (тёмная тема по умолчанию) |
| Графики | uPlot (лёгкий, тянет живые данные без лагов) |
| Состояние | Pinia |
| Доставка | статика, встроенная в бинарник panel через `go:embed` — один образ, один процесс |

---

## 3. Функциональные требования

### FR-01. Dashboard кластера

- 3.1.1. Карточка каждой ноды: hostname, роль (manager/worker, лидер — короной/значком), состояние (ready/down/drain), IP
- 3.1.2. На карточке — живые мини-графики CPU/RAM (sparkline, окно 5 мин) и текущие значения Disk/Network
- 3.1.3. Сводка кластера: кол-во нод, сервисов, задач running/failed, суммарные ресурсы
- 3.1.4. Индикация недоступности агента на ноде (метрики устарели > 15 сек → серая карточка + warning)
- 3.1.5. Обновление всех данных по WebSocket без перезагрузки страницы

### FR-02. Страница ноды

- 3.2.1. Полноразмерные графики CPU / RAM / Disk I/O / Network с выбором окна: 15 мин (live), 1 ч, 6 ч, 24 ч, 7 дн
- 3.2.2. Таблица дисков (точки монтирования, used %, прогресс-бар, подсветка > 85%)
- 3.2.3. Таблица сетевых интерфейсов с текущими rx/tx
- 3.2.4. Список задач (контейнеров), запущенных на ноде, со ссылками на сервисы
- 3.2.5. Labels ноды: просмотр, добавление, удаление, редактирование
- 3.2.6. Таблица контейнеров ноды с per-container CPU % и RAM used/limit (данные из метрик агента), сортировка по нагрузке — ответ на вопрос «кто ест ресурсы на этой ноде»

### FR-03. Управление нодами

- 3.3.1. Promote worker → manager, demote manager → worker (кнопка + подтверждение)
- 3.3.2. Смена availability: Active / Pause / **Drain** (с предупреждением, что задачи переедут)
- 3.3.3. Удаление ноды из кластера (только для нод в состоянии down; force — с двойным подтверждением)
- 3.3.4. **Добавление ноды:** модальное окно "Add Node" показывает готовые команды с актуальным join-token:
  ```
  docker swarm join --token SWMTKN-... <manager-ip>:2377
  ```
  отдельно для worker и manager, кнопка Copy. Ротация токенов из UI.
- 3.3.5. Защита от выстрела в ногу: запрет demote/drain последнего manager; предупреждение при потере кворума (чётное число manager'ов)

### FR-04. Управление сервисами

- 3.4.1. Таблица сервисов: имя, image:tag, mode (replicated/global), replicas (running/desired с цветовой индикацией), порты, ноды размещения, стек
- 3.4.2. Группировка по стекам (label `com.docker.stack.namespace`)
- 3.4.3. Действия над сервисом:
  - **Stop** = scale 0 (с запоминанием прежнего кол-ва реплик в label панели)
  - **Start** = scale обратно
  - **Redeploy** = `ForceUpdate++` (аналог `docker service update --force`)
  - **Scale** — ввод числа реплик
  - **Update image** — смена тега/образа с подтверждением
  - **Rollback** — откат на предыдущую спецификацию
  - **Remove** — двойное подтверждение с вводом имени сервиса
- 3.4.4. Страница сервиса: спецификация (env, mounts, networks, constraints, limits — read-only в v1), список задач с историей (state, exit code, нода, время), события обновления
- 3.4.5. Индикация зависших update (`UpdateStatus: paused/rollback`)
- 3.4.6. **Registry auth для Update Image / Redeploy:** если образ тянется из приватного реестра, панель передаёт в Docker API заголовок `X-Registry-Auth`. Учётные данные реестров хранятся в панели (таблица `registries`, пароль шифруется, см. FR-13), выбор реестра — в диалоге Update Image; при `RegistryAuthFrom: previous-spec` берётся авторизация из прежней спецификации сервиса

### FR-05. Логи

- 3.5.1. Живой стриминг логов сервиса (все реплики, объединённый поток) через WebSocket, `follow` режим
- 3.5.2. Метка задачи/ноды у каждой строки, раскраска stdout/stderr
- 3.5.3. Tail: 100 / 500 / 1000 / всё за период; timestamps on/off
- 3.5.4. Поиск/фильтр по подстроке на клиенте; пауза автоскролла
- 3.5.5. Логи отдельной задачи (контейнера)
- 3.5.6. Скачивание видимого буфера в .txt

### FR-06. События кластера

- 3.6.1. Живая лента Docker events (service create/update/remove, node join/down, task fail)
- 3.6.2. Журнал действий пользователя в панели (audit log в SQLite): кто, что, когда, над каким объектом

### FR-07. Аутентификация и безопасность

- 3.7.1. Один admin-аккаунт; пароль задаётся при первом запуске (setup-экран) либо через env `ADMIN_PASSWORD_HASH`
- 3.7.2. Пароли — bcrypt (cost 12); сессии — HttpOnly, Secure, SameSite=Strict cookie, TTL 24 ч
- 3.7.3. Rate-limit на `/api/v1/login`: 5 попыток / 15 мин с IP, экспоненциальный backoff
- 3.7.4. Метрики от агентов — только с валидным `X-Agent-Token` (генерируется при деплое, передаётся через Docker secret)
- 3.7.5. Панель не публикует порт наружу напрямую — только внутренняя overlay-сеть + reverse proxy (Nginx Proxy Manager) с HTTPS. В документации — обязательный раздел про это.
- 3.7.6. CSRF-токен на все мутирующие запросы
- 3.7.7. Все опасные операции (remove node, remove service, demote) — подтверждение в UI + запись в audit log
- 3.7.8. **Задел под RBAC (реализация ролей — v2):** в v1 один admin, но модель и код проектируются заранее многопользовательскими. У `users` есть поле `role` (в v1 всегда `admin`), сессия несёт `user_id`, а audit ссылается на `user_id`, а не на строку-имя. Это позволит в v2 добавить роли viewer/operator/admin без миграции схемы и переписывания auth/audit
- 3.7.9. **Учётные данные приватных реестров** (FR-13) хранятся зашифрованными (AES-GCM, ключ — из env/Docker secret `DSMS_ENCRYPTION_KEY`), никогда не отдаются клиенту в открытом виде и не пишутся в audit
- 3.7.10. **Опасные prune-операции** (FR-11) и удаление secret/volume/network (FR-09, FR-10) — с подтверждением и записью в audit; `prune` с флагом «включая используемые» запрещён

### FR-08. Управление стеками

- 3.8.1. Таблица стеков (агрегация по label `com.docker.stack.namespace`): имя стека, кол-во сервисов, суммарно задач running/desired, суммарный статус (зелёный/жёлтый/красный)
- 3.8.2. Страница стека: список входящих сервисов с их статусом, задействованные сети/тома/секреты
- 3.8.3. Действия над стеком целиком:
  - **Redeploy stack** = force update всех сервисов стека (`ForceUpdate++` каждому)
  - **Remove stack** = удаление всех объектов стека (сервисы, сети, конфиги) — двойное подтверждение с вводом имени стека
- 3.8.4. Деплой/обновление стека из compose-файла через UI — **не в v1** (см. v2), в v1 только операции над уже существующими стеками
- 3.8.5. Все действия над стеком — в audit log

### FR-09. Secrets и Configs

- 3.9.1. Таблица **secrets**: имя, id, дата создания, какие сервисы используют (по `Spec.TaskTemplate`)
- 3.9.2. Создание secret (имя + значение; значение вводится в UI, шлётся один раз, обратно не читается — Docker secrets неизвлекаемы), удаление secret (запрет удаления, если используется сервисом; force — с подтверждением)
- 3.9.3. Таблица **configs**: имя, id, размер, использующие сервисы; просмотр содержимого config (в отличие от secret, config читаем); создание/удаление config
- 3.9.4. Значения secrets **никогда не показываются и не логируются**; в audit пишется только имя и факт операции

### FR-10. Networks и Volumes

- 3.10.1. Таблица **networks**: имя, driver (overlay/bridge/...), scope (swarm/local), attachable, подсеть, кол-во подключённых сервисов/контейнеров
- 3.10.2. Детали сети: список подключённых сервисов и задач (кто в этой сети)
- 3.10.3. Создание overlay-сети (имя, driver, attachable, подсеть — опц.), удаление сети (запрет, если используется)
- 3.10.4. Таблица **volumes** (по нодам, т.к. local-тома нодоспецифичны): имя, driver, mountpoint, нода, используется ли, размер (если доступен из `system df`)
- 3.10.5. Удаление тома (запрет для используемых; force — подтверждение), prune неиспользуемых томов (см. FR-11)

### FR-11. Disk usage и очистка (prune)

- 3.11.1. Экран **Disk usage** — аналог `docker system df` по каждой ноде: images (кол-во, размер, reclaimable), containers, local volumes, build cache
- 3.11.2. Кнопки очистки с предпросмотром освобождаемого места:
  - **Prune images** (только dangling по умолчанию; «все неиспользуемые» — отдельный чекбокс с предупреждением)
  - **Prune build cache**
  - **Prune volumes** (только неиспользуемые)
  - **Prune containers** (только остановленные)
- 3.11.3. Prune выполняется per-node (панель адресует конкретный Docker Engine); агрегированный результат «освобождено X ГБ» показывается в UI и пишется в audit
- 3.11.4. Запрет опасных комбинаций: нельзя prune-ить используемые образы/тома; «включая используемые» недоступен из UI

### FR-12. In-app алерты (внешние нотификации — v2)

- 3.12.1. Движок правил на стороне panel, вычисляется на каждом батче метрик и Docker-событии. Базовые правила v1:
  - диск на ноде used > 90% (порог настраивается)
  - нода перешла в down / агент молчит > 60 сек
  - сервис: desired > running дольше N сек (реплики не поднимаются)
  - задача завершилась с non-zero exit code
- 3.12.2. Доставка — **баннер/тост в UI** через существующий WebSocket (новый topic `alerts`); индикатор-колокольчик с счётчиком активных алертов
- 3.12.3. Алерты имеют состояние: active → resolved (когда условие ушло); список активных и история последних (в памяти + короткая история в SQLite)
- 3.12.4. Экран **Alerts**: активные + журнал, фильтр по типу/ноде/сервису
- 3.12.5. Внешние каналы (Telegram/email) — v2; в v1 движок правил проектируется так, чтобы канал доставки был подключаемым

### FR-13. Учётные данные реестров (registry auth)

- 3.13.1. Таблица **registries**: адрес реестра (`registry-1.docker.io`, `ghcr.io`, приватный), username, метка. Пароль/токен хранится зашифрованным (см. FR-07 3.7.9)
- 3.13.2. Добавление/редактирование/удаление реестра; проверка логина (опциональный `docker login`-подобный ping)
- 3.13.3. При Update Image / Redeploy сервиса с приватным образом — выбор реестра; панель формирует `X-Registry-Auth` для Docker API
- 3.13.4. Пароли реестров никогда не отдаются клиенту и не пишутся в audit

---

## 4. API (сокращённая спецификация)

Базовый префикс: `/api/v1`. Формат: JSON. Все эндпоинты кроме `/login` требуют сессию.

### 4.1. REST

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/login` | вход |
| POST | `/logout` | выход |
| GET | `/cluster` | сводка кластера |
| GET | `/nodes` | список нод + последние метрики |
| GET | `/nodes/{id}` | детали ноды |
| POST | `/nodes/{id}/role` | `{role: manager\|worker}` |
| POST | `/nodes/{id}/availability` | `{availability: active\|pause\|drain}` |
| PUT | `/nodes/{id}/labels` | замена labels |
| DELETE | `/nodes/{id}?force=bool` | удаление ноды |
| GET | `/swarm/join-tokens` | токены worker/manager + IP manager'а |
| POST | `/swarm/join-tokens/rotate` | ротация |
| GET | `/services` | список сервисов + статус реплик |
| GET | `/services/{id}` | спецификация + задачи |
| POST | `/services/{id}/scale` | `{replicas: n}` (0 = Stop, реплики запоминаются) |
| POST | `/services/{id}/start` | Start: scale обратно к запомненным репликам (3.4.3) |
| POST | `/services/{id}/redeploy` | force update |
| POST | `/services/{id}/image` | `{image, registry_id?}` — смена образа с registry auth (FR-13) |
| POST | `/services/{id}/rollback` | откат |
| DELETE | `/services/{id}` | удаление |
| GET | `/stacks` | список стеков + агрегированный статус (FR-08) |
| GET | `/stacks/{name}` | сервисы/сети/тома/секреты стека |
| POST | `/stacks/{name}/redeploy` | force update всех сервисов стека |
| DELETE | `/stacks/{name}` | удаление стека целиком |
| GET | `/secrets` | список secrets + использующие сервисы (FR-09) |
| POST | `/secrets` | `{name, data}` — создание |
| DELETE | `/secrets/{id}?force=bool` | удаление |
| GET | `/configs` | список configs |
| GET | `/configs/{id}` | содержимое config |
| POST | `/configs` | создание |
| DELETE | `/configs/{id}` | удаление |
| GET | `/networks` | список сетей + использующие сервисы (FR-10) |
| GET | `/networks/{id}` | детали сети |
| POST | `/networks` | создание overlay-сети |
| DELETE | `/networks/{id}` | удаление |
| GET | `/volumes` | список томов по нодам |
| DELETE | `/volumes/{name}?node=&force=bool` | удаление тома |
| GET | `/system/df` | disk usage по нодам, аналог `docker system df` (FR-11) |
| POST | `/system/prune` | `{node_id, targets:[images\|containers\|volumes\|build-cache], all_images?}` |
| GET | `/registries` | список реестров (без паролей) (FR-13) |
| POST | `/registries` | `{address, username, password, label}` — создание |
| PUT | `/registries/{id}` | редактирование |
| DELETE | `/registries/{id}` | удаление |
| GET | `/alerts` | активные алерты + история (FR-12) |
| GET | `/metrics/nodes/{id}?window=1h&step=60s` | история метрик |
| GET | `/audit?limit=&offset=` | журнал действий |
| POST | `/ingest` | приём метрик от агентов (token auth) |
| GET | `/healthz` | liveness (без auth) |

### 4.2. WebSocket `/api/v1/ws`

Один мультиплексированный канал, протокол подписок:

```json
→ {"op": "sub", "topic": "metrics"}                  // все ноды, live
→ {"op": "sub", "topic": "logs", "service": "id", "tail": 500}
→ {"op": "sub", "topic": "events"}
→ {"op": "sub", "topic": "alerts"}
→ {"op": "unsub", "topic": "logs", "service": "id"}

← {"topic": "metrics", "node": "...", "ts": 1720180000, "data": {...}}
← {"topic": "logs", "service": "...", "task": "...", "node": "...", "stream": "stdout", "line": "..."}
← {"topic": "events", "type": "service", "action": "update", "actor": "..."}
← {"topic": "alerts", "id": "...", "rule": "disk_high", "severity": "warning", "state": "active", "node": "...", "message": "disk / used 92%"}
```

Ping/pong каждые 30 сек; переподключение клиента с восстановлением подписок.

### 4.3. Формат метрик (agent → panel)

```json
POST /api/v1/ingest
{
  "node_id": "abc...",
  "hostname": "vps-7ed76d0c",
  "ts": 1720180000,
  "cpu": {"total_pct": 34.2, "per_core": [30.1, 38.3], "load": [0.8, 0.6, 0.5]},
  "mem": {"total": 8322816000, "used": 5100000000, "available": 2900000000, "cached": 1200000000},
  "disk": [{"mount": "/", "total": 80000000000, "used": 61000000000,
            "read_bps": 1048576, "write_bps": 524288, "read_iops": 40, "write_iops": 12}],
  "net": [{"iface": "eth0", "rx_bps": 2097152, "tx_bps": 1048576,
           "rx_pps": 900, "tx_pps": 700, "errors": 0, "drops": 0}],
  "sys": {"uptime": 864000, "containers": 14},
  "containers": [{"id": "3f2a...", "name": "web.1.xyz", "service": "myapp_web",
                  "cpu_pct": 12.4, "mem_used": 268435456, "mem_limit": 536870912}]
}
```

---

## 5. Модель данных (SQLite)

```sql
-- пользователи; role в v1 всегда 'admin' (задел под RBAC, FR-07)
users(id, username UNIQUE, password_hash, role DEFAULT 'admin', created_at);

-- агрегированные метрики, шаг 60с, ретенция 7 дней (фоновая очистка)
metrics_1m(node_id, ts, cpu_pct, mem_used, mem_total,
           disk_json, net_json, PRIMARY KEY(node_id, ts));

-- журнал действий; ссылка на user_id, а не на строку-имя (задел под RBAC)
audit(id, ts, user_id, action, object_type, object_id, details_json);

-- служебное: запомненные реплики "остановленных" сервисов
service_state(service_id PRIMARY KEY, saved_replicas, stopped_at);

-- учётные данные приватных реестров; password_enc — AES-GCM (FR-13, FR-07)
registries(id, address, username, password_enc, label, created_at);

-- история алертов (активные держатся в памяти, тут — короткий журнал) (FR-12)
alerts(id, ts_opened, ts_resolved, rule, severity, object_type, object_id, message);

-- настройки (agent token, пороги алертов, ретенция и пр.)
settings(key PRIMARY KEY, value);
```

Live-метрики (шаг 3с, окно 15 мин) и per-container метрики — только кольцевой буфер в памяти, в БД не пишутся. Secrets/configs/networks/volumes в SQLite не дублируются — читаются напрямую из Docker API.

---

## 6. UI / дизайн

### 6.1. Общее

- Тёмная тема по умолчанию, светлая — переключателем
- Адаптив: desktop-first, но таблицы читабельны на планшете
- Языки интерфейса: EN (базовый), RU. Структура i18n — JSON-словари, добавление AZ тривиально
- Никаких перезагрузок страницы: SPA + WebSocket

### 6.2. Экраны

1. **Login** — минимальный, лого + форма
2. **Dashboard** — сетка карточек нод + сводная панель кластера сверху; колокольчик активных алертов
3. **Node detail** — графики, диски, сеть, задачи, контейнеры (CPU/RAM), labels, кнопки управления
4. **Services** — таблица с группировкой по стекам, быстрые действия в строке
5. **Service detail** — спецификация, задачи, кнопки, вкладка Logs
6. **Stacks** — список стеков + страница стека (сервисы, сети, тома, секреты, redeploy/remove)
7. **Logs** — полноэкранный терминало-подобный вид (моношрифт, чёрный фон)
8. **Resources** — вкладки Secrets / Configs / Networks / Volumes (CRUD согласно FR-09, FR-10)
9. **Disk usage** — `system df` по нодам + кнопки prune (FR-11)
10. **Alerts** — активные алерты + журнал (FR-12)
11. **Events / Audit** — две вкладки, живая лента + журнал
12. **Settings** — смена пароля, ротация join-token, ротация agent-token, реестры (FR-13), пороги алертов, ретенция метрик

### 6.3. Цветовая индикация состояний

| Состояние | Цвет |
|---|---|
| ready / running / desired==running | зелёный |
| drain / pause / updating | жёлтый |
| down / failed / desired>running | красный |
| агент молчит | серый + иконка ⚠ |

---

## 7. Деплой

### 7.1. Stack file (эталон поставки)

```yaml
version: "3.8"

services:
  panel:
    image: ghcr.io/<user>/dsms-panel:latest
    networks: [dsms]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - panel-data:/data
    environment:
      - DSMS_LISTEN=:9000
      - DSMS_DB=/data/dsms.db
    secrets: [agent_token]
    deploy:
      placement:
        constraints: [node.role == manager]
      resources:
        limits: {memory: 256M}
      restart_policy: {condition: on-failure}

  agent:
    image: ghcr.io/<user>/dsms-agent:latest
    networks: [dsms]
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/host/rootfs:ro
    environment:
      - NODE_ID={{.Node.ID}}
      - PANEL_URL=http://panel:9000
      - HOST_PROC=/host/proc
      - HOST_SYS=/host/sys
    secrets: [agent_token]
    deploy:
      mode: global
      resources:
        limits: {memory: 64M}
      restart_policy: {condition: any}

networks:
  dsms:
    driver: overlay
    attachable: false

volumes:
  panel-data:

secrets:
  agent_token:
    external: true
```

### 7.2. Процедура установки (документируемая)

```bash
openssl rand -hex 32 | docker secret create agent_token -
docker stack deploy -c dsms.yml dsms
# затем: проксирование panel:9000 через Nginx Proxy Manager с HTTPS
```

### 7.3. Требования к окружению

- Docker Engine ≥ 24.x, Swarm mode активен
- Linux amd64 + arm64 (multi-arch образы)
- Открытый трафик внутри overlay-сети между нодами (штатные порты Swarm: 2377/tcp, 7946/tcp+udp, 4789/udp)

---

## 8. Нефункциональные требования

| # | Требование |
|---|---|
| NFR-1 | Panel: ≤ 256 МБ RAM при 10 нодах / 100 сервисах; образ ≤ 25 МБ |
| NFR-2 | Agent: ≤ 64 МБ RAM (лимит), фактически ≤ 20 МБ; ≤ 0.5% CPU — включая сбор per-container метрик (топ-N, чтение cgroups) |
| NFR-2a | Размер ingest-батча ограничен: per-container метрики шлются только для топ-N контейнеров, чтобы батч не рос линейно с плотностью ноды |
| NFR-3 | Задержка live-метрик end-to-end ≤ 5 сек |
| NFR-4 | Стриминг логов: ≥ 1000 строк/сек без деградации UI |
| NFR-5 | Панель переживает рестарт Docker daemon и обрыв соединений агентов (auto-reconnect) |
| NFR-6 | Потеря панели не влияет на кластер (панель — только наблюдатель/управлятор, без своего state в кластере) |
| NFR-7 | Все образы multi-arch (amd64, arm64), сборка в GitHub Actions |
| NFR-8 | Логи самой панели — структурированный JSON в stdout |

---

## 9. План работ

| Этап | Содержание | Оценка |
|---|---|---|
| 1 | Skeleton: Go panel + Docker client, auth (с задел под RBAC), отдача SPA; agent с метриками нод + контейнеров; ingest; stack file | 1 нед |
| 2 | Dashboard + Node detail: WebSocket метрик, графики uPlot, таблица контейнеров, управление нодами (FR-02, FR-03) | 1 нед |
| 3 | Services + Stacks: таблицы, действия, страницы, registry auth (FR-04, FR-08, FR-13) | 1–1.5 нед |
| 4 | Логи + события + audit (FR-05, FR-06) | 0.5–1 нед |
| 5 | Resources: Secrets/Configs/Networks/Volumes + Disk usage/prune (FR-09, FR-10, FR-11) | 1 нед |
| 6 | In-app алерты: движок правил + UI (FR-12) | 0.5 нед |
| 7 | История метрик в SQLite, окна 1ч–7дн, Settings | 0.5 нед |
| 8 | Полировка: безопасность (7.3, CSRF, rate-limit, шифрование реестров), i18n RU, multi-arch CI, документация | 1 нед |

**Итого: ~7–8 недель** до production-ready v1.0. MVP (этапы 1–4) — за ~4 недели.

## 10. Риски

| Риск | Митигция |
|---|---|
| Доступ к docker.sock = root кластера | Разд. FR-07: не публиковать порт, reverse proxy + HTTPS, rate-limit, audit |
| Логи "толстых" сервисов заваливают WebSocket | Ограничение tail, backpressure, drop старых строк на сервере |
| Рост SQLite на большом кластере | Только 1-мин агрегаты, ретенция 7 дн, VACUUM по расписанию |
| Смена лидера manager'ов (panel прибит к одной ноде) | v1: constraint `node.role == manager` — переедет на любой живой manager; docker.sock есть на каждом |
| Несовместимость версий Docker API | Клиент с `client.WithAPIVersionNegotiation()` |
| Утечка паролей реестров из SQLite | Шифрование AES-GCM, ключ `DSMS_ENCRYPTION_KEY` из Docker secret, не в БД; пароли не отдаются клиенту и не пишутся в audit (FR-13, FR-07) |
| Ошибочный prune на проде (снёс нужные образы/тома) | Только неиспользуемые по умолчанию, предпросмотр освобождаемого места, подтверждение, запись в audit; «включая используемые» недоступно из UI (FR-11) |
| prune/volumes per-node на большом кластере | Операции адресуются конкретной ноде, выполняются последовательно, результат агрегируется — без параллельного шторма на docker.sock |

## 11. Направления v2 (вне текущего объёма)

- Внешние нотификации: Telegram-бот / email поверх движка алертов v1 (FR-12 спроектирован под подключаемый канал)
- Мульти-пользователи + роли viewer / operator / admin (в v1 заложены `users.role`, `session.user_id`, `audit.user_id` — FR-07)
- Деплой стеков из UI: загрузка/редактирование compose-файла (в v1 — только операции над существующими стеками, FR-08)
- Управление несколькими кластерами из одной панели
- Экспорт метрик в Prometheus-формате (`/metrics`)
- Терминал в контейнер (exec) через WebSocket
- Редактирование полной спецификации сервиса из UI (env, mounts, constraints — в v1 read-only)
