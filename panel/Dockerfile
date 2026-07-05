# PANEL: multi-stage build → минимальный образ ≤ 25 МБ (NFR-1).
# Стадия 1: сборка фронтенда (статика для go:embed).
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Vite кладёт сборку в ../panel/web/dist (см. vite.config.ts)
RUN mkdir -p ../panel/web && npm run build

# Стадия 2: сборка Go-бинарника со встроенной статикой.
FROM golang:1.25-alpine AS build
WORKDIR /src/panel
COPY panel/go.mod panel/go.sum ./
RUN go mod download
COPY panel/ ./
COPY --from=frontend /src/panel/web/dist ./web/dist
# CGO выключен (sqlite — чистый Go), статический бинарник для scratch.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/panel ./cmd/panel

# Стадия 3: финальный образ.
FROM scratch
COPY --from=build /out/panel /panel
# TLS-корни не нужны: panel ходит только в unix-socket и слушает HTTP внутри overlay.
EXPOSE 9000
ENTRYPOINT ["/panel"]
