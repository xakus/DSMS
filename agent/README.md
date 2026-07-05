# DSMS Agent

Агент метрик DSMS: работает на каждой ноде кластера (`mode: global`),
собирает метрики хоста через gopsutil и шлёт JSON-батчи на panel.

## Механика

- Интервал сбора 3с (env `DSMS_INTERVAL`), формат батча — раздел 4.3 ТЗ
- Хост-ресурсы read-only: `/proc→/host/proc`, `/sys→/host/sys`, `/→/host/rootfs`
  (gopsutil направляется env-переменными `HOST_PROC`, `HOST_SYS`)
- Отправка: `POST {PANEL_URL}/api/v1/ingest` с заголовком `X-Agent-Token`
- При недоступности panel — буферизация 60с, старые точки отбрасываются
- Скорости (bps/iops/pps) считаются как дельта между соседними тиками

## Запуск

```bash
NODE_ID=local-dev PANEL_URL=http://localhost:9000 DSMS_AGENT_TOKEN=dev-token go run ./cmd/agent
```

Обязателен `NODE_ID` — в Swarm подставляется шаблоном `{{.Node.ID}}` (stack file).

## Пакеты

- `internal/config` — настройки из env/secrets
- `internal/collector` — сбор метрик (CPU/RAM/Disk/Net/Sys) + типы батча
- `internal/sender` — очередь и отправка с ретраями

Типы батча — осознанная копия panel-типов: общего кода между сервисами нет
(правило проекта). Меняешь формат — синхронизируй обе стороны и ТЗ.
