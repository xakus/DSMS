# DSMS — Docker Swarm Management System

Веб-панель для управления production-кластером Docker Swarm: живые метрики нод,
управление сервисами/стеками/нодами, стриминг логов, события и audit.
Полное ТЗ — [dsms-tz-v1.md](dsms-tz-v1.md) (v1.3).

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

Подробные схемы — [docs/architecture/](docs/architecture/):
[контекст](docs/architecture/c4-context.svg) ·
[контейнеры](docs/architecture/c4-containers.svg) ·
[деплой стека из файла](docs/architecture/stack-deploy-sequence.svg)
(исходники PlantUML лежат рядом с картинками).

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

Проверки перед коммитом (то же гоняет CI):

```bash
cd panel    && go build ./... && go vet ./... && go test ./... && golangci-lint run ./...
cd agent    && go build ./... && go vet ./... && go test ./... && golangci-lint run ./...
cd frontend && npm run lint && npm run test && npm run build
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

### Reverse proxy (обязательно)

Панель **не публикует порт наружу**: доступ к ней равен доступу к `docker.sock`,
то есть к root-правам на всём кластере. Единственный поддерживаемый способ
попасть в интерфейс — reverse proxy с HTTPS, подключённый к overlay-сети `dsms`.

**Nginx Proxy Manager.** Подключите контейнер NPM к сети стека и создайте
Proxy Host:

```bash
docker network connect dsms npm-app-1   # имя контейнера NPM в вашей установке
```

| Поле | Значение |
|---|---|
| Domain Names | `dsms.example.com` |
| Scheme | `http` |
| Forward Hostname | `dsms_panel` |
| Forward Port | `9000` |
| Websockets Support | **включить** — без этого не будет живых метрик и логов |
| Block Common Exploits | включить |
| SSL | запросить сертификат Let's Encrypt, включить Force SSL и HTTP/2 |

**Обычный nginx** — минимальный рабочий конфиг:

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name dsms.example.com;

    ssl_certificate     /etc/letsencrypt/live/dsms.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/dsms.example.com/privkey.pem;

    location / {
        proxy_pass http://dsms_panel:9000;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        # Панель берёт ПОСЛЕДНИЙ адрес из этой цепочки — не заменяйте
        # директиву на $http_x_forwarded_for, иначе клиент сможет подделать
        # свой IP и обойти ограничение попыток входа.
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket: живые метрики, логи, события, алерты.
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";

        # Стримы логов долгие — короткий таймаут рвал бы их каждую минуту.
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_buffering off;
    }
}
```

Проверка: `curl -s https://dsms.example.com/api/v1/healthz` должен вернуть
`{"status":"ok","version":...}`.

### Резервная копия

В базе панели лежат **исходники стеков, зашифрованный env, пользователи и
журнал действий** — в кластере этих данных нет, восстановить их неоткуда.

- **Settings → Резервная копия → «Скачать копию»** — снимок делается на живой
  панели (`VACUUM INTO`), останавливать ничего не нужно.
- Секрет `encryption_key` **храните отдельно от копии**: без него зашифрованные
  значения (пароли реестров, env стеков) не прочитать. И наоборот — копия
  вместе с ключом даёт доступ ко всем секретам, обращайтесь с ней как с паролем.
- Восстановление: положите файл на место `/data/dsms.db` (том `panel-data`)
  при остановленной панели и запустите её снова.

### Обновление

```bash
# 1. Снять резервную копию (Settings → Резервная копия)
# 2. Обновить образы
docker service update --image ghcr.io/xakus/dsms-panel:X.Y.Z dsms_panel   # тег образа без «v»: git-тег v2.1.0 → образ 2.1.0
docker service update --image ghcr.io/xakus/dsms-agent:X.Y.Z dsms_agent
```

Схема БД версионируется и мигрирует при старте сама — ручных SQL не нужно.
Панель **откажется стартовать на базе от более новой сборки** (защита от отката):
в этом случае вернитесь на прежнюю версию образа или восстановите копию.
Какая версия реально работает — видно в `GET /api/v1/healthz` и в углу шапки UI;
если версия сервера подсвечена рядом с версией интерфейса — у браузера
устаревший бандл в кэше, обновите страницу.

## Деплой стека из файла (FR-14, v2)

Панель умеет разворачивать стек **из compose-файла прямо из UI** — как
`docker stack deploy`, но без командной строки. На экране **Стеки** →
кнопка **«Развернуть стек»**:

1. Вставьте `docker-stack.yml` (или загрузите файл). Поддержаны YAML-якоря,
   интерполяция `${VAR}` / `${VAR:-default}`, `deploy` (replicas, placement
   с preferences и max_replicas_per_node, resources, restart/update/rollback
   config, endpoint_mode), `volumes` (именованные, bind, tmpfs), `secrets` и
   `configs`, `healthcheck`, `logging`, `extra_hosts`, `ports: mode: host`,
   overlay-сети с `driver_opts`/`internal`.
2. Задайте переменные окружения: поле **.env** (строки `KEY=value`) и/или
   ручные пары (перекрывают .env). Значения хранятся зашифрованными.
3. **«Проверить»** — dry-run: покажет сервисы, сети, секреты, что именно будет
   создано/обновлено/удалено, отсутствующие `external`-ресурсы и **список того,
   что не переносится в Swarm** (`build`, `depends_on`, `privileged` и т.п.).
4. **«Развернуть»** — панель создаёт сети → secrets/configs → создаёт/обновляет
   сервисы.

Стек, развёрнутый из файла, помечается бейджем **«из файла»**: его YAML+env
хранятся в панели, можно **редактировать, скачать и передеплоить** одной кнопкой.
Опция **prune** (удалить сервисы, ушедшие из файла) по умолчанию выключена.

Важные ограничения:

- `secrets`/`configs` с `file:` не поддерживаются — у панели нет доступа к
  вашему диску. Используйте `content:`, `environment:` или создайте объект
  заранее и укажите `external: true`.
- Существующий secret/config не пересоздаётся: swarm-объекты неизменяемы.
  Меняете значение — заводите новое имя (`app_jwt_v2`).
- Размер файла ограничен 1 МБ.

## FAQ

**Агент не шлёт метрики, нода в дашборде пустая.**
Проверьте, что в `dsms.yml` у агента `PANEL_URL=http://tasks.dsms_panel:9000`
именно с префиксом `tasks.` и с именем вашего стека. Через VIP-имя Swarm
делает source-NAT, панель видит адрес балансировщика вместо реального IP
агента и потом не может достучаться до него на `:9001` (df/volumes/prune).
Дальше — `docker service logs dsms_agent`: там видно, отвечает ли ingest.

**Панель не видит новую ноду.**
Агент разворачивается в режиме `global` и приезжает на ноду автоматически.
Если этого не произошло — нода не вошла в Swarm (`docker node ls` на manager)
либо на ней не хватило ресурсов под лимит 64 МБ.

**Забыл пароль.**
Восстановления «по почте» нет: панель не знает вашей почты. Остановите панель,
удалите строку пользователя из БД (`sqlite3 /var/lib/docker/volumes/dsms_panel-data/_data/dsms.db
"DELETE FROM users;"`) и запустите снова — откроется экран первичной настройки.
Данные кластера при этом не страдают: панель их не хранит.

**Потерян `encryption_key`.**
Пароли реестров и env managed-стеков расшифровать больше нельзя — их нужно
ввести заново. Сами стеки и сервисы продолжают работать: ключ используется
только панелью. Заведите новый секрет и обновите записи в Settings.

**После обновления в браузере старый интерфейс.**
Посмотрите на версию в углу шапки: если рядом подсвечена версия сервера —
у браузера закэширован старый бандл, обновите страницу с Ctrl+F5.

**Панель не стартует после отката на старую версию.**
Так и задумано: схема БД версионируется, и старая сборка не умеет читать
новую схему. Вернитесь на прежний образ или восстановите резервную копию.

**Можно ли открыть панель напрямую, без reverse proxy?**
Технически да (опубликовать порт 9000), но это означает панель без HTTPS:
сессионная cookie и пароль уйдут по открытому каналу, а доступ к панели
равен root-доступу ко всему кластеру. Не делайте так.

**Сколько ресурсов ест сама панель?**
Лимиты в `dsms.yml`: панель 256 МБ, агент 64 МБ. Агент по замерам — до 20 МБ
RAM и меньше 0.5% CPU; интервал сбора настраивается в Settings (1–30 с).

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

**v2:** этапы 9–12 готовы — деплой стека из compose-файла через UI (FR-14),
закрытие его пробелов (тома, secrets/configs, предупреждения, лимиты) и
надёжность данных (миграции схемы, резервная копия, версия сборки, хелсчеки).

**Этап 13** (диаграммы, документация, LICENSE/CHANGELOG) выполнен; осталась
приёмка на живом Swarm-кластере и теги `v1.0.0` / `v2.0.0` — после них
release-workflow опубликует multi-arch образы в ghcr.io.

История изменений — [CHANGELOG.md](CHANGELOG.md), реестр находок аудита —
[docs/plans/audit-2026-08-15.md](docs/plans/audit-2026-08-15.md).

## Лицензия

[GNU General Public License v3.0](LICENSE). Коротко: пользоваться, изучать и
менять можно свободно; если вы распространяете изменённую версию — исходники
изменений тоже должны быть открыты под GPL v3.
