# План работ DSMS

Детализация раздела 9 ТЗ ([dsms-tz-v1.md](../../dsms-tz-v1.md)).
Один файл — один этап; внутри: объём, порядок, критерии проверки.

| Этап | Файл | Содержание | Статус |
|---|---|---|---|
| 1 | — | Skeleton: panel + agent + frontend, auth, ingest, stack file | ✅ готов |
| 2 | [stage-2-dashboard-nodes.md](stage-2-dashboard-nodes.md) | Dashboard, Node detail, управление нодами (FR-01, FR-02, FR-03) | ✅ готов |
| 3 | [stage-3-services-stacks.md](stage-3-services-stacks.md) | Сервисы + стеки + registry auth (FR-04, FR-08, FR-13) | ✅ готов |
| 4 | [stage-4-logs-events-audit.md](stage-4-logs-events-audit.md) | Логи, события, audit (FR-05, FR-06) | ✅ готов |
| 5 | [stage-5-resources.md](stage-5-resources.md) | Secrets/Configs/Networks/Volumes + prune (FR-09, FR-10, FR-11) | 🔄 в работе |
| 6 | [stage-6-alerts.md](stage-6-alerts.md) | In-app алерты (FR-12) | ⬜ |
| 7 | [stage-7-history-settings.md](stage-7-history-settings.md) | История метрик SQLite, Settings (окна 1ч–7дн) | ⬜ |
| 8 | [stage-8-polish-ci.md](stage-8-polish-ci.md) | Безопасность, i18n, multi-arch CI, документация | ⬜ |

Правила: MVP = этапы 1–4. Каждый этап завершается тестами
(`go test`, `npm run build`), обновлением README/CLAUDE.md и коммитом.
Изменения объёма — сначала в ТЗ, потом сюда, потом в код.
