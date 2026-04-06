# adaptive-grpc-keepalive

A Go-based master-worker distributed system built on bidirectional gRPC streams, with adaptive keepalive tuning for failure detection under network jitter/CPU pressure and process-freeze scenarios.

## Features

- Bidirectional gRPC streaming between master and workers
- Worker registration and state management
- Heartbeat-based liveness detection
- Adaptive keepalive tuning inspired by RFC 6298
- Failure injection experiments for crash and freeze scenarios
- Automated experiment scripts for jitter and capacity testing
- Automated experiment scripts for CPU pressure and capacity testing (haven't upload)

## Repository Structure

- `cmd/master`: master server entrypoint
- `cmd/worker`: worker client entrypoint
- `internal/scheduler`: worker state tracking and task scheduling
- `internal/adaptive`: adaptive keepalive estimator
- `proto`: gRPC protobuf definitions and generated code
- `scripts`: experiment automation scripts (haven't upload)
- `docs`: system design notes (haven't upload)

## Current Status

Initial public cleanup and documentation in progress.

## Quick Start

```bash
go build ./...
docker compose up --build