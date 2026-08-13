# Этап 9 — Деплой стека из compose-файла (FR-14, v2)

Первая фича v2. В v1 стек — только агрегация живых сервисов по label
(`com.docker.stack.namespace`); панель умела лишь операции над уже поднятыми
сервисами. Здесь панель начинает **сама разворачивать стек из файла** —
то, что сейчас делается руками через `docker stack deploy -c file.yml`.

## Цель

Загрузить compose/stack YAML целиком + переменные окружения, панель хранит это
в своей БД, разворачивает весь стек (сети → секреты/конфиги → сервисы),
и позволяет редактировать файл и передеплоить одной кнопкой.

## Решения (согласовано с Мурадом)

- **Хранение — в БД (SQLite).** Панель помнит YAML+env, можно вернуться,
  отредактировать, передеплоить. GitOps-лайт.
- **Env — отдельное поле `.env` (текст) + ручные пары ключ-значение в UI.**
  Значения секретов в env шифруются (AES-GCM, ключ `DSMS_ENCRYPTION_KEY` —
  переиспользуем механизм из FR-13 registries).

## Объём

**Входит (v2.0):**
- Загрузка YAML: вставка текста + upload `.yml`-файла в то же поле.
- Env: textarea `.env` + таблица ручных пар (add/edit/remove).
- Интерполяция `${VAR}`, `${VAR:-default}`, YAML-якоря (`&anchor`/`*ref`).
- Создание overlay-сетей, secrets, configs, сервисов стека.
- Обновление существующего стека: diff — create новых / update изменённых /
  prune удалённых сервисов.
- Валидация до деплоя (dry-run parse: сколько сервисов/сетей/секретов, ошибки).
- Хранение в БД + редактирование + передеплой из файла.
- Audit каждой операции (без утечки секретов).

**Не входит (позже):**
- Редактор с подсветкой синтаксиса (сначала обычная textarea).
- Создание volumes/bind-mounts (используем только существующие).
- Полный rollback стека одним действием (per-service rollback уже есть, FR-04).
- Авто-`X-Registry-Auth` для приватных образов — задел есть (FR-13), включим
  вторым шагом.

## Технические решения

1. **Парсинг:** `github.com/compose-spec/compose-go/v2` (loader + interpolation).
   Лёгкий, ровно то, чем парсит сам docker. Проверяем на реальном файле Мурада
   (SHAD: version 3.9, якоря, `extra_hosts`, `${VAR:-default}`, `mode: host`).
2. **Конвертация compose → `swarm.ServiceSpec`:** **собственный конвертер**
   (рекомендация). Причины: контроль размера образа (лимит 25 МБ — тянуть весь
   `docker/cli` рискованно), контроль поведения (наше правило «не навязывать
   `start-first`», см. deploy-start-first-lesson). Покрываем поля, реально
   используемые в стеках: image, command, environment, deploy(replicas,
   placement.constraints, resources, restart_policy, update_config), networks,
   extra_hosts, ports (в т.ч. `mode: host`), secrets, configs, healthcheck,
   labels. _Fallback:_ если своя конвертация окажется дороже ожидаемого —
   оценить `docker/cli` compose/convert, но только после замера размера образа.
3. **Шифрование env:** весь env-blob шифруется целиком (в нём бывают
   `JWT_SECRET`, пароли БД) — не только «секретные» ключи.

## Модель данных (новая таблица)

```sql
CREATE TABLE IF NOT EXISTS stacks (
  name         TEXT PRIMARY KEY,   -- имя стека = namespace
  compose_yaml TEXT NOT NULL,      -- исходный YAML как есть
  env_enc      BLOB,               -- .env + ручные пары, AES-GCM
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  user_id      INTEGER             -- кто последним деплоил (RBAC-задел)
);
```

`GET /stacks` теперь = merge: **managed** (из таблицы, с файлом) + **discovered**
(агрегация по label, как в v1). Managed-стек в UI помечается «управляется из файла».

## Backend

**DockerAPI (расширения интерфейса + реальный клиент + fake):**
- `ServiceCreate(ctx, spec, registryAuth) (string, error)` — **нового ещё нет.**
- `NetworkCreate` — добавить labels (`com.docker.stack.namespace`) и driver-opts.
- `SecretCreate` / `ConfigCreate` — добавить labels стека.

**Оркестратор `deployStack(name, yaml, env)`** (`internal/stackdeploy` или
`internal/api/handlers_stacks.go`):
1. Parse + interpolate → набор (services, networks, secrets, configs).
2. Валидация (имена, обязательные поля).
3. Networks: non-external → `NetworkCreate`, если ещё нет (label namespace).
4. Secrets/Configs из файла → create/обновить (external — пропустить).
5. Services: имя `<stack>_<svc>` + label namespace; convert → ServiceSpec;
   есть по имени → `ServiceUpdate`, иначе `ServiceCreate`; registryAuth (опц.).
6. Prune: сервисы стека, которых больше нет в файле → `ServiceRemove`.
7. Audit `stack.deploy_file` (только имя + сводка, без yaml/env).
   Ошибки не роняют весь деплой — собираем per-service сводку (создано/обновлено/упало).

**Эндпоинты:**
- `POST /stacks/validate` — dry-run, ничего не создаёт, отдаёт превью/ошибки.
- `POST /stacks` — сохранить {name, compose_yaml, env} в БД + задеплоить.
- `GET  /stacks/{name}/source` — вернуть сохранённый yaml+env для редактирования.
- `PUT  /stacks/{name}` — обновить yaml/env + передеплой (diff).
- `DELETE /stacks/{name}` — уже есть; добавить удаление записи БД (+ опц. сети/секреты).

## Frontend

- **StacksView:** кнопка «Развернуть стек» / «Загрузить файл».
- **Экран/модалка StackDeploy:** textarea YAML (upload `.yml` → в неё же);
  textarea `.env` + таблица ручных пар; кнопка «Проверить» (validate-превью:
  N сервисов, сети, секреты, ошибки); кнопка «Развернуть».
- **StackDetail:** бейдж «управляется из файла», кнопки «Редактировать файл»,
  «Передеплой из файла», «Скачать yaml».
- i18n ключи (en базовый, ru).

## Безопасность

- env_enc шифруется (AES-GCM) — секреты в env не лежат открыто в БД.
- Никогда не логировать yaml/env в audit (только name + сводка).
- CSRF на все мутации; подтверждение на деплой/удаление; лимит размера загрузки.

## Порядок работ

1. compose-go: прототип parse+interpolate, прогнать на реальном файле SHAD.
2. Конвертер compose→ServiceSpec + юнит-тесты (поля из файла Мурада).
3. DockerAPI: `ServiceCreate` + labels в Network/Secret/Config + fake.
4. Оркестратор `deployStack` (create/update/prune) + тесты на fakeDocker.
5. Store: таблица `stacks`, шифрование env, CRUD.
6. Эндпоинты (validate/create/update/source/delete) + тесты.
7. Frontend: экран загрузки/редактирования + validate-превью + i18n.
8. `GET /stacks` merge managed+discovered; StackDetail «управляется из файла».
9. Замер размера образа (≤25 МБ), README-раздел, аккуратная проверка на тестовом
   стеке (**не** на живом SHAD с 17 сервисами — сначала мелкий тестовый стек).
10. Внести FR-14 в ТЗ, обновить docs/plans/README, тег `v2.0.0-…`.

## Критерии проверки

- Реальный файл SHAD парсится без ошибок (якоря, `${VAR:-default}`, extra_hosts).
- Тестовый стек из 2–3 сервисов поднимается «с нуля» одной кнопкой (сеть+сервисы).
- Повторный деплой изменённого файла: изменённый сервис — update, новый — create,
  удалённый из файла — prune; чужие стеки не тронуты.
- Секреты из env не появляются в audit, в ответах API и в логах.
- `go build ./... && go vet ./... && go test ./...` зелёные; `npm run build` чист.
- Образ panel ≤ 25 МБ.
