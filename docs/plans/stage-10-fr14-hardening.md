# Этап 10 — FR-14 hardening: закрыть дыры деплоя стека

Закрывает находки аудита **A-01…A-07, A-24, A-25** ([audit-2026-08-15.md](audit-2026-08-15.md)).
Требования ТЗ v1.3: 3.14.5, 3.14.9, 3.14.10, 3.14.11.

## Зачем

Базовый FR-14 (этап 9) разворачивает стек, но **молча теряет часть файла**:
тома, секреты, конфиги и десяток полей сервиса. Пользователь видит зелёное
превью и получает сервис без данных. До закрытия этих дыр фичу нельзя
считать готовой и нельзя ставить тег v2.

## Объём

### 10.1. Тома сервисов (A-01, ТЗ 3.14.9)

- `volumes:` → `TaskTemplate.ContainerSpec.Mounts`.
- Типы: `volume` (именованный), `bind`, `tmpfs`. Флаг `read_only`.
- Именованный том стека → `<stack>_<vol>` (как `docker stack deploy`);
  `external: true` — имя как есть, префикс не добавляется.
- Top-level `volumes:` с драйверами/опциями — не создаём заранее (Docker создаёт
  при запуске задачи), но нестандартный `driver`/`driver_opts` → предупреждение.

### 10.2. Secrets и Configs (A-02, ТЗ 3.14.5)

- `secrets:`/`configs:` сервиса → `SecretReferences`/`ConfigReferences`
  (`File.Name`, `UID`/`GID`/`Mode`, дефолты `0`/`0`/`0444`).
- Top-level `secrets:`/`configs:` с `file:` — панель **не создаёт** (содержимое
  файла ей недоступно, загрузка контента — отдельная задача); такой ресурс даёт
  ошибку валидации с понятным текстом «создайте secret заранее или пометьте external».
- `external: true` — используем существующий, проверяем наличие (см. 10.4).
- Резолв ID: имя из файла → ID через `Secrets()`/`Configs()`; отсутствующий →
  ошибка до деплоя, а не падение сервиса.

### 10.3. Предупреждения о неподдерживаемых полях (A-03, A-07, ТЗ 3.14.10)

- Конвертер возвращает `[]Warning{Service, Field, Message}` вместе со спеками.
- Два класса: **не переносится** (`build`, `depends_on`, `network_mode`,
  `container_name`, `restart` вне `deploy`, `profiles`) и **неприменимо к Swarm**.
- Поля, которые вместо предупреждения нужно **поддержать** (A-07):
  `user`, `working_dir`, `hostname`, `stop_grace_period`, `stop_signal`,
  `sysctls`, `cap_add`/`cap_drop`, `dns`, `init`, `read_only`, `privileged`,
  `logging` (LogDriver), `deploy.endpoint_mode`, `deploy.placement.preferences`,
  `deploy.placement.max_replicas_per_node`, `networks.driver_opts`/`internal`/`ipv6`.
- `POST /stacks/validate` и деплой отдают `warnings` в ответе; UI показывает их
  жёлтым блоком до кнопки «Развернуть».

### 10.4. Labels стека и external-ресурсы (A-04, A-05)

- `NetworkCreate` получает параметр `labels` → на сети ставится
  `com.docker.stack.namespace=<stack>` (тот же label — на secrets/configs,
  если панель их создаёт).
- Валидация проверяет наличие `external` сетей/секретов/конфигов и возвращает
  список отсутствующих как ошибки.

### 10.5. Лимиты ввода (A-06, ТЗ 3.14.11)

- `http.MaxBytesReader` 1 МБ на `/stacks/validate`, `POST /stacks`, `PUT /stacks/{name}`.
- Внятная ошибка 413 вместо обрыва парсинга.

### 10.6. UI (A-24, A-25)

- Блок `warnings` в превью валидации.
- Кнопка «Скачать YAML» для managed-стека.
- Превью изменений при передеплое: какие сервисы будут созданы / обновлены /
  удалены (сравнение с текущим состоянием кластера) — до нажатия «Развернуть».

## Порядок работ

1. `stackspec`: тип `Warning`, `Converted.Warnings`, тесты на пустой список.
2. Mounts (10.1) + юнит-тесты (volume/bind/tmpfs/read_only/external).
3. Secrets/Configs-референсы (10.2) + резолв ID в оркестраторе + тесты на fake.
4. Остальные поля сервиса (10.3, вторая половина) + тесты.
5. Предупреждения для непереносимых полей + отдача в API.
6. `NetworkCreate` с labels: `dockerapi`, интерфейс `DockerAPI`, fake (10.4).
7. Проверка external-ресурсов при валидации (10.4).
8. Лимиты тела запроса (10.5).
9. Фронт: warnings, скачивание YAML, diff-превью (10.6) + i18n en/ru.
10. Прогон на `examples/demo-stack.yml` и реальном файле SHAD; замер образа.

## Критерии проверки

- [x] Юнит-тест: стек с `volumes` (named + bind + tmpfs) даёт корректные `Mounts`,
      именованный том получает префикс `<stack>_`, external — нет.
      (`stackspec/hardening_test.go: TestVolumesConverted`)
- [x] Юнит-тест: `secrets`/`configs` сервиса превращаются в референсы с
      правильными `File.Name`/`Mode`; отсутствующий secret → ошибка валидации.
      (`TestSecretsConfigsConverted`, `TestSecretFromFileRejected`, `TestUnknownSecretRejected`)
- [x] Юнит-тест: файл с `build:`/`depends_on:` даёт непустой `warnings`,
      чистый swarm-файл — пустой. (`TestWarningsForUnsupported`, `TestNoWarningsForCleanStack`)
- [x] Созданная панелью сеть имеет label `com.docker.stack.namespace`.
      (`api/stackdeploy_test.go: TestStackDeploySecretsAndLabels`)
- [x] Тело > 1 МБ → 413, панель не растёт по памяти. (`TestStackBodyLimit`)
- [x] `go build ./... && go vet ./... && go test ./...` зелёные; `npm run build` чист.
- [x] Размер panel: бинарник 15.4 МБ → образ scratch в пределах лимита 25 МБ.
      Сборка образа в этой среде недоступна (Docker Hub отдаёт 401), замер
      косвенный — прямую проверку автоматизирует этап 12 (A-09).
- [x] Реальные файлы репозитория (`examples/*`, боевой `dsms.yml`) конвертируются
      без ошибок, монтирования и секреты не теряются. (`zz_examples_test.go`)
- [ ] Демо-стек разворачивается на живом Swarm с томом, и после `docker service
      update --force` данные на месте. **Требует живого кластера — этап 13.**
