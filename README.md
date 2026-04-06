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
```

## Failure Detection Model

The system distinguishes several failure detection paths:

- `EOF`  
  Triggered when the stream closes cleanly.

- `RECV_ERR`  
  Triggered when the master encounters an abnormal receive-side stream error.

- `SEND_ERR`  
  Triggered when the worker fails to send a heartbeat/status update.

- `HB_TIMEOUT`  
  Triggered by application-level heartbeat timeout scanning on the master.

This separation is important because different failure modes surface differently:
- crash failures often produce transport-level errors quickly
- freeze failures may remain invisible to TCP and require application- or keepalive-level detection

| Detection Type | Where Triggered | Typical Scenario |
|---|---|---|
| EOF | master / worker | normal disconnect |
| RECV_ERR | master | abnormal stream receive failure |
| SEND_ERR | worker | worker cannot send status update |
| HB_TIMEOUT | master | worker becomes silent without closing connection |

### Example Detection Log

```text
[Detect] side=master worker=worker-1 time=... type=HB_TIMEOUT hb_timeout=6s
```
