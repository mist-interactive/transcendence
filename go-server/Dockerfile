#Stage 1: Build executable
FROM golang:1.25-bookworm as base

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

#Stage 2a: hot reloading server for development
FROM base AS dev

  RUN go install github.com/air-verse/air@v1.61.7

  COPY . .

  EXPOSE 8080

  CMD ["air"]

#stage 2b: build executable
FROM base as builder

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o backend main.go

#Stage 3b: execute
FROM debian:bookworm-slim as production

RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/backend /usr/local/bin/backend

EXPOSE 8080

CMD ["backend"]
