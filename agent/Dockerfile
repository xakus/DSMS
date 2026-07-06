# AGENT: multi-stage build → scratch, ≤ 15 МБ (разд. 2.3 ТЗ).
# Build-стадия на $BUILDPLATFORM + Go кросс-компиляция под целевую арху —
# без QEMU-эмуляции (см. комментарий в panel/Dockerfile).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src/agent
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/agent ./cmd/agent

FROM scratch
COPY --from=build /out/agent /agent
ENTRYPOINT ["/agent"]
