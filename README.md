# adaptive-grpc-keepalive

A distributed master-worker system that detects **silent failures (freezes)** significantly faster than static keepalive.

> TCP detects crashes quickly — but fails to detect freezes.
> This project applies adaptive keepalive (RFC 6298) to close that gap.

## Why Failure Detection is Hard

- TCP detects connection failures, not application progress
- A frozen process may keep the connection alive but stop responding
- Timeouts must balance:
  - fast detection (aggressive)
  - stability under jitter (conservative)
  
## Features

- Bidirectional gRPC streaming between master and workers
- Worker registration and state management
- Heartbeat-based liveness detection
- Adaptive keepalive tuning inspired by RFC 6298
- Failure injection experiments for crash and freeze scenarios
- Automated experiment scripts for:
  - network jitter
  - CPU pressure
  - capacity testing

## System Architecture
```
                +----------------------+
                |        Master        |
                |----------------------|
                |  Connect(stream)     |
                |  Recv StatusUpdate   |
                |  Send TaskAssignment |
                +----------+-----------+
                           |
                           | bidirectional gRPC stream
                           |
          +----------------+----------------+
          |                                 |
          v                                 v
+----------------------+       +----------------------+
|        Worker        |       |     StateManager     |
|----------------------|       |----------------------|
| RegisterRequest      |       | RegisterWorker       |
| Periodic heartbeat   |-----> | UpdateWorkerStatus   |
| Execute task         |       | SelectWorker         |
| Send status update   |       | CheckHeartbeatTimeout|
+----------+-----------+       +----------------------+
           |
           | timing samples
           v
+----------------------+
|  Adaptive Estimator  |
|----------------------|
| SRTT / RTTVAR / RTO  |
| Recommend KA params  |
+----------------------+
```

## Repository Structure

- `cmd/master`: master server entrypoint
- `cmd/worker`: worker client entrypoint
- `internal/scheduler`: worker state tracking and task scheduling
- `internal/adaptive`: adaptive keepalive estimator
- `proto`: gRPC protobuf definitions and generated code
- `scripts`: experiment automation scripts
- `docs`: system design notes

## Why This Matters

In real distributed systems, not all failures are equal:

- Crash failures are quickly detected by TCP
- Silent failures (e.g., process freeze, GC stall, CPU starvation) are not

This leads to a fundamental tradeoff:

- aggressive timeouts → faster detection but false positives
- conservative timeouts → stable but slow detection

Adaptive keepalive aims to balance this tradeoff dynamically across environments.

## Current Status

Initial public cleanup and documentation in progress.

## Quick Start

```bash
git clone ...
cd adaptive-grpc-keepalive

# First run or after code changes
docker compose up --build

# Subsequent runs
docker compose up

# run in background
docker compose up -d

# check logs
docker compose logs -f
```

## Scripts (shareable, reproducible)

The `scripts/` directory is intended to be published on GitHub and used by others.

### Dependencies

- `docker` + `docker compose`
- `bash` (tested with `set -euo pipefail`)
- `python3` (used for latency parsing and small helper math)
- **Optional (local-only)**: `lsof` / `pkill` (used by `run_capacity.sh`)

### Experiment runner (Docker-based)

- **`scripts/run_experiment.sh`**: run one failure-detection experiment (crash / freeze), optionally with network delay/jitter injected via `tc netem` inside the worker container.
- **`scripts/auto_run_freeze.sh`**: batch-run multiple freeze configs and append to a unified CSV.

Examples:

```bash
./scripts/run_experiment.sh A freeze
DELAY_MS=100 JITTER_MS=30 ./scripts/run_experiment.sh E freeze
./scripts/auto_run_freeze.sh
```

Outputs:

- Logs: `./logs/config_<CONFIG>_<MODE>_<ts>/`
- Summary CSV (default): `./results/experiment_summary.csv`

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

## Adaptive Keepalive Tuning

Static keepalive parameters are difficult to tune across different environments.

- If timeouts are too aggressive, the system risks false positives under jitter.
- If timeouts are too conservative, freeze detection becomes unnecessarily slow.

To address this, SwiftScheduler includes an adaptive estimator inspired by RFC 6298, the same algorithm family TCP uses for retransmission timeout calculation.

The estimator tracks:
- `SRTT` — smoothed round-trip timing estimate
- `RTTVAR` — timing variance
- `RTO = SRTT + 4 * RTTVAR`

These values are then used to recommend:
- `KA_TIMEOUT` based on the computed RTO
- `KA_TIME` based on a multiple of SRTT, with safety bounds

### Why RFC 6298?

Failure detection and retransmission timeout share the same core question:

> how long should we wait before deciding the other side is no longer responding?

TCP solved this with a smoothed estimate plus a variance penalty.  
This project applies the same idea to keepalive timeout tuning.

## Example Results

| Scenario                | Detection Latency |
|------------------------|------------------|
| Crash (TCP RST)        | ~100–200 ms      |
| Freeze (baseline)      | ~14–15 s         |
| Freeze (adaptive)      | ~8–10 s          |

Adaptive tuning significantly reduces freeze detection latency under unstable conditions.

### Tradeoffs

Adaptive tuning improves responsiveness but introduces tradeoffs:

- aggressive parameters reduce detection latency but risk false positives
- conservative parameters improve stability but delay detection
- under CPU pressure, timing samples may include host scheduling delay
- current implementation uses status-update timing as a proxy instead of direct keepalive RTT