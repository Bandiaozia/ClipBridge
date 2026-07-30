# syntax=docker/dockerfile:1.7
FROM golang:1.26.5-bookworm AS builder
WORKDIR /src
COPY relay-server/go.mod relay-server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY relay-server/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go test ./... && \
    CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/clipbridge-relay ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl tzdata && \
    rm -rf /var/lib/apt/lists/*
RUN groupadd --system --gid 10001 clipbridge && \
    useradd --system --uid 10001 --gid clipbridge --home /var/lib/clipbridge clipbridge
WORKDIR /app
COPY --from=builder /out/clipbridge-relay /usr/local/bin/clipbridge-relay
USER clipbridge:clipbridge
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/clipbridge-relay"]

