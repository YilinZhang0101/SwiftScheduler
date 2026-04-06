Designed and implemented a Master/Worker bidirectional gRPC streaming scheduling system (registration, status reporting, task dispatching, and concurrent execution).

Implemented dual-layer fault detection: transport-level (EOF/RECV_ERR) + application-level (HB_TIMEOUT).

Built a thread-safe StateManager (LastSeen, ActiveTaskCount, Suspected, and least-load worker selection).

Established a configurable keepalive/heartbeat parameter system with full environment variable control, requiring no recompilation.

Set up a Docker + tc netem experimental environment supporting jitter/delay injection.

Implemented crash/freeze fault injection and an automated experiment script pipeline capable of reproducing detection latency.

Implemented an RFC 6298-style adaptive algorithm module (internal/adaptive).

Provided an offline RTT parsing and parameter recommendation script (scripts/parse_rtt.py).

Produced preliminary experimental results and documentation (design/progress report + result artifacts).



P0 Data pipeline closure: run_experiment.sh automatically outputs unified structured results to results/experiment_summary.csv.

P0 Unified batch entry point: refactored auto_run_freeze.sh to batch-invoke run_experiment.sh, with A–H parameter sweep writing to the same CSV automatically.

P0 Metric definition standardization: every experiment consistently records failure_time / detect_time / detect_type / detect_side / latency / status / log_dir.

P1 Online adaptive MVP: worker supports runtime RTT sampling, periodic re-estimation, and keepalive parameter updates that take effect on reconnect.

P1 Adaptive feature toggle: introduced a WORKER_ADAPTIVE_* environment variable group (enable/disable, window size, minimum sample count, minimum change threshold).

P1 Stability hardening: worker session refactored into a reconnectable loop with automatic backoff reconnection after abnormal disconnection.

P1 Test baseline establishment: added core unit tests for adaptive and scheduler state (convergence, boundary conditions, timeout deduplication, and least-load selection).