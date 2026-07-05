# Этап 5 — Resources: Secrets/Configs/Networks/Volumes + Disk usage

Покрывает FR-09, FR-10, FR-11.

## Backend

1. Secrets (FR-09): `GET /secrets` (+ использующие сервисы из TaskTemplate),
   `POST /secrets` (значение принимается один раз, не логируется),
   `DELETE /secrets/{id}?force=` (запрет удаления используемого)
2. Configs: `GET /configs`, `GET /configs/{id}` (содержимое читаемо),
   `POST /configs`, `DELETE /configs/{id}`
3. Networks (FR-10): `GET /networks` (+ подключённые сервисы),
   `GET /networks/{id}`, `POST /networks` (overlay), `DELETE /networks/{id}`
4. Volumes: `GET /volumes` (по нодам), `DELETE /volumes/{name}?node=&force=`
5. Disk usage (FR-11): `GET /system/df` (аналог docker system df),
   `POST /system/prune` `{node_id, targets[], all_images?}` —
   последовательно per-node, результат «освобождено X байт» в ответ и audit
6. Guard: prune только неиспользуемого; force-удаления — с подтверждением

## Frontend

1. Экран Resources: вкладки Secrets / Configs / Networks / Volumes
2. Создание secret/config (textarea, значение secret после создания
   не показывается), создание overlay-сети
3. Экран Disk usage: таблица по нодам (images/containers/volumes/cache,
   reclaimable), кнопки prune с предпросмотром и подтверждением

## Критерии проверки

- [ ] go test: значения secrets не появляются в ответах и audit
- [ ] go test: запрет удаления используемых объектов без force
- [ ] Живой кластер: prune освобождает место, отчёт в UI
