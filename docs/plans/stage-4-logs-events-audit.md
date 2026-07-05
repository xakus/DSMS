# Этап 4 — Логи, события, audit

Покрывает FR-05 (логи), FR-06 (события + журнал действий).

## Backend

1. Стриминг логов сервиса/задачи: docker `ServiceLogs`/`TaskLogs`
   (follow, tail, timestamps) → WS topic `logs` (метка task/node/stream);
   демультиплексирование stdout/stderr (stdcopy)
2. Backpressure (разд. 10 ТЗ): ограничение tail, drop старых строк
   при медленном клиенте
3. Docker events (`/events`) → WS topic `events`; переподключение
   к daemon при обрыве (NFR-5)
4. REST: `GET /audit?limit=&offset=` (+ username join)

## Frontend

1. Экран Logs (терминало-подобный, моношрифт): follow, tail-селектор,
   timestamps on/off, раскраска stdout/stderr, метки task/node,
   клиентский фильтр по подстроке, пауза автоскролла, скачивание .txt
2. Вкладка Logs на странице сервиса; логи отдельной задачи
3. Events / Audit: две вкладки — живая лента + журнал с пагинацией

## Критерии проверки

- [ ] go test: парсинг stdcopy-потока, protocol WS logs sub/unsub
- [ ] Живой кластер: 1000 строк/сек не вешают UI (NFR-4)
- [ ] Audit показывает действия из этапов 2–3
