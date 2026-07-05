# Этап 3 — Сервисы, стеки, registry auth

Покрывает FR-04 (сервисы), FR-08 (стеки), FR-13 (реестры).

## Backend

1. `dockerapi`: ServiceInspect, ServiceUpdate (scale / image / force /
   rollback), ServiceRemove, TaskList по сервису
2. REST сервисов:
   - `GET  /services` — список + running/desired (агрегация task-статусов), стек
   - `GET  /services/{id}` — спецификация + задачи с историей
   - `POST /services/{id}/scale` `{replicas}` — Stop=0 запоминает реплики
     в service_state, Start восстанавливает
   - `POST /services/{id}/redeploy` — ForceUpdate++
   - `POST /services/{id}/image` `{image, registry_id?}` — X-Registry-Auth
   - `POST /services/{id}/rollback`, `DELETE /services/{id}`
3. Стеки (FR-08): `GET /stacks`, `GET /stacks/{name}`,
   `POST /stacks/{name}/redeploy`, `DELETE /stacks/{name}` —
   агрегация по label com.docker.stack.namespace
4. Реестры (FR-13): CRUD `/registries`, пароли AES-GCM
   (ключ DSMS_ENCRYPTION_KEY из secret), пароль никогда не отдаётся клиенту
5. Всё мутирующее — в audit

## Frontend

1. Services: таблица с группировкой по стекам, цветовая индикация реплик,
   быстрые действия (Stop/Start/Redeploy/Scale/Update image/Rollback/Remove
   с двойным подтверждением имени)
2. Service detail: спецификация read-only, задачи с историей, UpdateStatus
3. Stacks: список + страница стека, Redeploy/Remove stack
4. Settings-вкладка реестров: добавление/редактирование/удаление

## Критерии проверки

- [ ] go test: scale 0/восстановление реплик, redeploy инкремент,
      группировка стеков, guards remove
- [ ] go test: шифрование реестров (roundtrip, пароль не в JSON-ответах)
- [ ] npm run build; действия сервисов работают на живом кластере
