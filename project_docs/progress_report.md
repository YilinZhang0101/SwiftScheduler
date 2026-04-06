# CS 512 Project Progress Report


**Adaptive Keepalive Tuning for Reliable Failure Detection in gRPC-Based Distributed Systems**

Yilin (Elaine) Zhang · March 2025

---

## 1. Project Overview

This project investigates how keepalive parameter configuration affects failure detection latency in gRPC-based distributed systems. The core observation is that static keepalive parameters — set once at startup and never updated — perform poorly under real-world network conditions. The goal is to (1) quantify this gap through controlled experiments, and (2) develop an adaptive algorithm that derives parameters from measured network behavior.

The system is implemented in Go as a Master/Worker scheduler communicating over long-lived bidirectional gRPC streams, with a containerized experiment environment for reproducible measurement.

---

## 2. Completed Work

### 2.1 Core System Implementation

- **Master node** (`cmd/master/master_main.go`): gRPC server on `:50051` with configurable `KeepaliveParams` and `EnforcementPolicy`; implements `Connect()` handler with structured `[Detect]` logging across three detection types:
  - `type=EOF` — normal connection close
  - `type=RECV_ERR` — abnormal disconnect (crash / keepalive expiry)
  - `type=HB_TIMEOUT` — application-level heartbeat timeout

- **Worker node** (`cmd/worker/worker_main.go`): gRPC client with `ClientParameters`; sends `RegisterRequest` on connect, periodic `StatusUpdate` every 2s (application-level heartbeat), and handles `TaskAssignment` with concurrency semaphore (`MAX_CONCURRENCY`).

- **State manager** (`internal/scheduler/state.go`): thread-safe worker registry with `RWMutex`; tracks `LastSeen` timestamps, `ActiveTaskCount`, and `Suspected` state; exposes `CheckHeartbeatTimeout()` scanned every 200ms.

- All keepalive parameters (`MASTER_KA_TIME`, `MASTER_KA_TIMEOUT`, `WORKER_KA_TIME`, `WORKER_KA_TIMEOUT`, `MASTER_HB_TIMEOUT`) are configurable via environment variables — no recompilation needed between experiments.

### 2.2 Experiment Infrastructure

- **Dockerized environment**: multi-stage Dockerfile builds Linux binaries; `docker-compose.yml` runs master and worker in separate containers on a bridge network (`grpc-net`); worker container runs with `--cap-add NET_ADMIN` for kernel-level network manipulation.

- **Network jitter injection**: `tc netem` applied to worker's `eth0` interface at container startup via entrypoint; supports arbitrary delay and jitter (e.g., `delay 100ms 30ms distribution normal`), affecting keepalive PINGs at the kernel level — not mocked at the application layer.

- **Automated experiment runner** (`scripts/run_experiment.sh`): single command launches containers, waits for worker registration, injects failure (`SIGKILL` or `SIGSTOP`), polls master logs for `[Detect]` event, computes detection latency, and tears down — fully reproducible with no manual steps.

- **Failure simulation**: `SIGKILL` triggers TCP RST (crash scenario); `SIGSTOP` suspends the process while leaving the TCP connection open (freeze scenario), isolating the keepalive detection path.

### 2.3 Adaptive Keepalive Algorithm

- Implemented **RFC 6298 Jacobson/Karels algorithm** — the same formula TCP uses to compute Retransmission Timeout (RTO) — applied to keepalive parameter derivation:
  - `SRTT = (1 − α)·SRTT + α·sample`,  α = 1/8  (smoothed RTT)
  - `RTTVAR = (1 − β)·RTTVAR + β·|SRTT − sample|`,  β = 1/4  (variance)
  - `KA_TIMEOUT = SRTT + 4·RTTVAR`  (equivalent to TCP RTO)
  - `KA_TIME = 3·SRTT`  (headroom for a full round-trip before PING fires)

- **RTT proxy**: since gRPC does not expose raw keepalive PING timestamps at the application layer, `StatusUpdate` inter-arrival times are used as RTT proxies — they traverse the same network path and capture the same congestion and jitter characteristics.

- **Go implementation** (`adaptive.go`): `RTTEstimator` struct with `Update(sample)` and `Recommend()` methods; safety bounds enforced (KA_TIME: 1–60s, KA_TIMEOUT: 1–30s, KA_TIME ≥ KA_TIMEOUT + 2s); confidence scoring based on sample count.

- **Analyzer script** (`scripts/parse_rtt.py`): reads master logs, extracts RTT samples, runs RFC 6298 algorithm, and outputs recommended parameters with statistics.

---

## 3. Experimental Results

### 3.1 Crash vs. Freeze Detection Gap

All experiments use Config A baseline (KA_TIME=10s, KA_TIMEOUT=5s, HB_TIMEOUT=15s) unless noted.

| Experiment | Failure | Jitter | Detection Latency |
|---|---|---|---|
| Config A | crash (SIGKILL) | none | **175ms** |
| Config A | freeze (SIGSTOP) | none | **14,568ms** |
| Config B (2s/1s) | freeze | none | **2,606ms** |
| Config B (2s/1s) | freeze | 100ms±30ms | **2,628ms** |
| Adaptive (5.6s/2.8s) | freeze | 100ms±30ms | **8,014ms** |

**Key findings:**

- 83× latency difference between crash and freeze confirms that keepalive tuning only impacts freeze-type failures. TCP RST handles crash detection instantly regardless of keepalive config.
- Freeze detection triggered `type=HB_TIMEOUT` rather than `type=RECV_ERR` — the application-level heartbeat monitor fired before gRPC keepalive, demonstrating the two-layer detection design working as intended.

### 3.2 Keepalive Parameter Sensitivity

- Config B detects 5.6× faster than Config A (2,606ms vs. 14,568ms), and maintained speed under 100ms±30ms jitter (2,628ms).
- However, Config B parameters were chosen arbitrarily without RTT measurement. Under more extreme jitter, the 1s timeout could be exceeded by normal PING latency, causing false positives.

### 3.3 Adaptive Algorithm Validation

- 17 RTT samples extracted from Config A freeze experiment logs.
- RFC 6298 output: SRTT=1.9s, RTTVAR=221ms, RTO=2.8s → KA_TIME=5.6s, KA_TIMEOUT=2.8s.
- Adaptive parameters under 100ms±30ms jitter: **8,014ms** detection latency, zero false positives.
- Conclusion: adaptive tuning is slower than aggressive static config but grounded in measured network behavior, making it robust across varying network conditions.

---

## 4. Remaining Work

### 4.1 Live Sidecar Agent Integration
`adaptive.go` implements the algorithm; what remains is wiring `Update()` into gRPC keepalive PING callbacks so parameters update at runtime without connection restart. Implementation path: intercept keepalive PING RTTs via gRPC stats handler → feed to `RTTEstimator.Update()` → apply `Recommend()` output via server/client reconfiguration.

### 4.2 Multi-Worker Experiments
Current experiments use a single worker. Planned: multiple workers with heterogeneous network conditions to test whether per-worker adaptive tuning outperforms a single global config.

### 4.3 False Positive Characterization
Planned: systematically identify the jitter threshold at which static aggressive parameters (Config B) produce false positives, to quantify the safety margin of adaptive vs. static configs.

### 4.4 Persistent Metrics Storage
Detection events currently written to flat log files. Planned: structured storage for analytical queries across experiments by config, jitter level, and failure type.

---

## 5. Revised Timeline

| Week | Goals |
|---|---|
| Week 1–2 (complete) | Core system + experiment infrastructure + adaptive algorithm ✓ |
| Week 3 | Sidecar agent integration; live parameter update without connection restart |
| Week 4 | Multi-worker experiments; false positive threshold characterization |
| Week 5 | Analysis, visualization, final report writing |
