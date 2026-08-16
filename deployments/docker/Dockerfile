# build phase
FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/aggregator ./cmd/aggregator
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux GOBIN=/bin go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

# prod phase - basically takes the compiled binaries and puts into the final image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /bin/server /usr/local/bin/server
COPY --from=builder /bin/aggregator /usr/local/bin/aggregator
COPY --from=builder /bin/worker /usr/local/bin/worker
COPY --from=builder /bin/goose /usr/local/bin/goose
COPY migrations /app/migrations
COPY configs /app/configs

WORKDIR /app