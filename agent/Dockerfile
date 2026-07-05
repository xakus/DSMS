# AGENT: multi-stage build → scratch, ≤ 15 МБ (разд. 2.3 ТЗ).
FROM golang:1.25-alpine AS build
WORKDIR /src/agent
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/agent ./cmd/agent

FROM scratch
COPY --from=build /out/agent /agent
ENTRYPOINT ["/agent"]
