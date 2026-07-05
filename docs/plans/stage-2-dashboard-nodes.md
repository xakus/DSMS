# Этап 2 — Dashboard + Node detail + управление нодами

Покрывает FR-01 (dashboard), FR-02 (страница ноды), FR-03 (управление нодами).

## Backend (panel)

1. `dockerapi`: NodeInspect, NodeUpdate (role / availability / labels),
   NodeRemove, SwarmInspect (join-tokens + manager addr), RotateJoinTokens,
   TaskList по ноде (+ маппинг task → service name)
2. REST (все действия — в audit):
   - `GET  /nodes/{id}` — детали ноды + задачи на ней
   - `POST /nodes/{id}/role` `{role}` — promote/demote
   - `POST /nodes/{id}/availability` `{availability}` — active/pause/drain
   - `PUT  /nodes/{id}/labels` — замена labels
   - `DELETE /nodes/{id}?force=` — удаление (только down, force — с флагом)
   - `GET  /swarm/join-tokens`, `POST /swarm/join-tokens/rotate`
   - `GET  /metrics/nodes/{id}` — live-окно из кольцевого буфера (история — этап 7)
3. Защита от выстрела в ногу (3.3.5): запрет demote/drain последнего
   manager'а; предупреждение о чётном кворуме в ответе
4. Интерфейс DockerAPI в пакете api + fake в тестах: guards тестируются
   без Docker daemon

## Frontend

1. WS-клиент с переподключением и восстановлением подписок (разд. 4.2)
2. Pinia-store метрик: последний снапшот + окно на ноду (для графиков)
3. Dashboard (FR-01): сводка + сетка карточек нод — роль/лидер/состояние,
   sparkline CPU/RAM (uPlot, окно 5 мин), Disk/Net текущие, серая карточка
   при молчании агента > 15с
4. Node detail (FR-02): графики CPU/RAM/Disk I/O/Net (live 15 мин),
   таблица дисков (подсветка > 85%), интерфейсов, задач, labels-редактор
5. Управление нодами (FR-03): promote/demote/availability/remove
   с подтверждениями; модал "Add Node" с командами join + Copy + ротация
6. i18n EN/RU для всего нового

## Критерии проверки

- [ ] go test: guards (последний manager, кворум, удаление не-down ноды)
- [ ] go test: join-tokens, labels, метрики-окно
- [ ] npm run build без ошибок typecheck
- [ ] Просмотр UI: карточки нод обновляются по WS без перезагрузки
