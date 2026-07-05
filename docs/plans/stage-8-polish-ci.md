# Этап 8 — Полировка, безопасность, CI, документация

## Безопасность (финальный проход по FR-07)

1. Аудит всех мутирующих маршрутов: CSRF, подтверждения, audit-записи
2. Rate-limit: экспоненциальный backoff на login
3. Заголовки: CSP, X-Content-Type-Options, X-Frame-Options
4. Проверка: пароли/токены/секреты нигде не логируются и не отдаются

## Agent: per-container метрики (долг этапа 1)

1. Чтение cgroups v1/v2 из /host/sys/fs/cgroup + маппинг на контейнеры
2. Топ-N по CPU/RAM (NFR-2a), таблица контейнеров на Node detail

## CI/CD (NFR-7)

1. GitHub Actions: build + vet + test (panel, agent), typecheck+build (frontend)
2. Multi-arch образы (amd64+arm64) через buildx → ghcr.io, теги по релизам
3. Проверка размеров образов: panel ≤ 25 МБ, agent ≤ 15 МБ

## Документация

1. README: установка со скриншотами, раздел про reverse proxy (обязателен
   по 3.7.5), FAQ
2. Диаграмма архитектуры (PlantUML/C4) в docs/
3. i18n: полнота RU-словаря, проверка всех строк

## Критерии проверки

- [ ] CI зелёный на чистом клоне
- [ ] Образы собираются multi-arch и проходят лимиты размера
- [ ] Деплой по README на чистом Swarm — панель работает end-to-end
