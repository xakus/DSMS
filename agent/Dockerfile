# AGENT: multi-stage build → scratch, ≤ 15 МБ (разд. 2.3 ТЗ).
# Build-стадия на $BUILDPLATFORM + Go кросс-компиляция под целевую арху —
# без QEMU-эмуляции (см. комментарий в panel/Dockerfile).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS TARGETARCH
# Версия сборки (NFR-11): подставляется линкером, видна в логах и /healthz.
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src/agent
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags="-s -w \
        -X github.com/xakus/DSMS/agent/internal/buildinfo.Version=${VERSION} \
        -X github.com/xakus/DSMS/agent/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/agent ./cmd/agent

FROM scratch
COPY --from=build /out/agent /agent
EXPOSE 9001
# В scratch нет curl/wget, поэтому проба — сам бинарник (см. healthcheck.go).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/agent", "-healthcheck"]
ENTRYPOINT ["/agent"]
