# Stage 1: Build
FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build both binaries for linux/arm64 (Apple Silicon)
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/master ./cmd/master
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

# Stage 2: Runtime (slim image with iproute2 for tc netem)
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    iproute2 \
    procps \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/master /usr/local/bin/master
COPY --from=builder /out/worker /usr/local/bin/worker
