# DSMS Frontend

SPA панели DSMS: Vue 3 (Composition API) + Vite + TypeScript + Naive UI + Pinia.

## Команды

```bash
npm install      # зависимости
npm run dev      # dev-сервер с hot-reload, /api проксируется на localhost:9000
npm run build    # typecheck (vue-tsc) + сборка в ../panel/web/dist
```

Сборка кладётся в `panel/web/dist` и встраивается в Go-бинарник panel
через `go:embed` — отдельного веб-сервера у фронта нет.

## Структура

- `src/api/client.ts` — fetch-обёртка: JSON, CSRF-заголовок на мутирующих запросах
- `src/stores/` — Pinia: `auth` (сессия), `theme` (тёмная/светлая, тёмная по умолчанию)
- `src/router/` — маршруты + гард сессии
- `src/i18n/` — JSON-словари EN (базовый) и RU; новый язык = новый JSON
- `src/views/` — экраны: Login, Setup (первый запуск), Dashboard

## Экраны (этап 1)

Login → (первый запуск: Setup) → Dashboard со сводкой кластера.
Остальные экраны (Nodes, Services, Stacks, Logs, ...) — этапы 2+ плана работ.
