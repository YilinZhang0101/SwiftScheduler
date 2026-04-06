#!/usr/bin/env bash
# run_experiment.sh — Run a keepalive experiment using docker-compose.
#
# Usage:
#   ./scripts/run_experiment.sh [config] [failure_mode]
#
# Config labels:
#   A  — default      (KA_TIME=10s / KA_TIMEOUT=5s)
#   B  — aggressive   (KA_TIME=2s  / KA_TIMEOUT=1s)
#   C  — conservative (KA_TIME=30s / KA_TIMEOUT=10s)
#   D  — tight        (KA_TIME=5s  / KA_TIMEOUT=2s)
#   E  — jitter-safe  (KA_TIME=10s / KA_TIMEOUT=8s)
#
# Failure modes:
#   crash  — SIGKILL worker (TCP RST → fast detection)
#   freeze — SIGSTOP worker (silent connection → keepalive-based detection)
#
# Examples:
#   ./scripts/run_experiment.sh A freeze
#   ./scripts/run_experiment.sh B crash
#   DELAY_MS=100 JITTER_MS=30 ./scripts/run_experiment.sh E freeze

set -euo pipefail

CONFIG="${1:-A}"
FAILURE_MODE="${2:-freeze}"
DELAY_MS="${DELAY_MS:-0}"
JITTER_MS="${JITTER_MS:-0}"
WORKER_WAIT="${WORKER_WAIT:-20}"
RESULT_CSV="${RESULT_CSV:-./results/experiment_summary.csv}"

case "$CONFIG" in
  A) KA_TIME="10s"; KA_TIMEOUT="5s"  ;;
  B) KA_TIME="2s";  KA_TIMEOUT="1s"  ;;
  C) KA_TIME="30s"; KA_TIMEOUT="10s" ;;
  D) KA_TIME="5s";  KA_TIMEOUT="2s"  ;;
  E) KA_TIME="10s"; KA_TIMEOUT="8s"  ;;
  *)
    echo "Unknown config: $CONFIG. Choose from: A B C D E"
    exit 1
    ;;
esac

# Allow external override of KA_TIME / KA_TIMEOUT
# e.g. MASTER_KA_TIME=5.6s MASTER_KA_TIMEOUT=2.8s ./scripts/run_experiment.sh A freeze
if [[ -n "${MASTER_KA_TIME:-}" ]]; then KA_TIME="$MASTER_KA_TIME"; fi
if [[ -n "${MASTER_KA_TIMEOUT:-}" ]]; then KA_TIMEOUT="$MASTER_KA_TIMEOUT"; fi

LOG_DIR="./logs/config_${CONFIG}_${FAILURE_MODE}_$(date +%s)"
mkdir -p "$LOG_DIR"
mkdir -p "$(dirname "$RESULT_CSV")"

if [[ ! -f "$RESULT_CSV" ]]; then
  echo "run_ts,config,failure_mode,ka_time,ka_timeout,delay_ms,jitter_ms,failure_time,detect_time,detect_side,detect_type,latency_ms,status,log_dir" > "$RESULT_CSV"
fi

echo "╔══════════════════════════════════════════════╗"
echo "║         SwiftScheduler Experiment            ║"
echo "╠══════════════════════════════════════════════╣"
printf "║  Config       : %-28s║\n" "$CONFIG"
printf "║  KA_TIME      : %-28s║\n" "$KA_TIME"
printf "║  KA_TIMEOUT   : %-28s║\n" "$KA_TIMEOUT"
printf "║  Failure mode : %-28s║\n" "$FAILURE_MODE"
printf "║  Delay        : %-28s║\n" "${DELAY_MS}ms"
printf "║  Jitter       : %-28s║\n" "${JITTER_MS}ms"
echo "╚══════════════════════════════════════════════╝"

# ── Start containers ──────────────────────────────────────────────────────────
echo ""
echo "[1/4] Starting master and worker..."

MASTER_KA_TIME="$KA_TIME" \
MASTER_KA_TIMEOUT="$KA_TIMEOUT" \
WORKER_KA_TIME="$KA_TIME" \
WORKER_KA_TIMEOUT="$KA_TIMEOUT" \
DELAY_MS="$DELAY_MS" \
JITTER_MS="$JITTER_MS" \
  docker compose up -d --build 2>&1 | tail -5

# Wait for worker to register
echo "      Waiting for worker to register..."
for i in $(seq 1 10); do
  if docker compose logs master 2>/dev/null | grep -q "registered"; then
    echo "      ✓ Worker registered."
    break
  fi
  sleep 1
done

# ── Wait before injecting failure ─────────────────────────────────────────────
echo "[2/4] Waiting ${WORKER_WAIT}s before injecting failure..."
sleep "$WORKER_WAIT"

# ── Inject failure ────────────────────────────────────────────────────────────
WORKER_PID=$(docker exec worker pgrep -x worker 2>/dev/null | head -1 || true)

if [[ -z "$WORKER_PID" ]]; then
  echo "[error] Could not find worker process inside container."
  docker compose logs > "$LOG_DIR/all.log" 2>&1
  docker compose down
  exit 1
fi

FAILURE_TIME=$(date -u +"%Y-%m-%dT%H:%M:%S.%NZ")
echo "[3/4] Injecting '$FAILURE_MODE' at $FAILURE_TIME (worker PID=$WORKER_PID)"

case "$FAILURE_MODE" in
  crash)
    docker exec worker kill -KILL "$WORKER_PID"
    echo "      SIGKILL sent → TCP RST expected → detection in ~3-5ms"
    ;;
  freeze)
    docker exec worker kill -STOP "$WORKER_PID"
    echo "      SIGSTOP sent → detection expected in ~$(python3 -c "
import re
def s(v): n=float(re.match(r'[\d.]+',v).group()); return n if 's' in v else n/1000
print(f\"{s('$KA_TIME')+s('$KA_TIMEOUT'):.0f}s\")
" 2>/dev/null || echo "KA_TIME+KA_TIMEOUT")"
    ;;
  *)
    echo "[error] Unknown failure mode: $FAILURE_MODE"
    docker compose down
    exit 1
    ;;
esac

# ── Wait for detection, polling logs every second ────────────────────────────
DETECT_WAIT=$(python3 -c "
import re
def s(v): n=float(re.match(r'[\d.]+',v).group()); return n if 's' in v else n/1000
print(int(s('$KA_TIME') + s('$KA_TIMEOUT') + 10))
" 2>/dev/null || echo "25")

echo "      Polling master logs for [Detect] event (up to ${DETECT_WAIT}s)..."
DETECTED=""
for i in $(seq 1 "$DETECT_WAIT"); do
  CANDIDATE=$(docker compose logs master 2>/dev/null | grep "\[Detect\]" | head -1 || true)
  if [[ -n "$CANDIDATE" ]]; then
    DETECTED="$CANDIDATE"
    echo "      ✓ Detected after ${i}s"
    break
  fi
  sleep 1
done

# ── Collect logs ──────────────────────────────────────────────────────────────
docker compose logs master > "$LOG_DIR/master.log" 2>&1
docker compose logs worker > "$LOG_DIR/worker.log" 2>&1

# ── Parse results ─────────────────────────────────────────────────────────────
echo ""
echo "[4/4] Results:"
echo "──────────────────────────────────────────────"

if [[ -n "$DETECTED" ]]; then
  echo "  Master detected failure:"
  echo "  $DETECTED"

  DETECT_TIME=$(echo "$DETECTED" | grep -oE 'time=[^ ]+' | cut -d= -f2 || true)

  if [[ -n "$DETECT_TIME" ]]; then
    LATENCY_MS=$(python3 -c "
from datetime import datetime, timezone
def parse(s):
    s = s.rstrip('Z')
    # truncate nanoseconds to microseconds (Python only supports 6 digits)
    if '.' in s:
        base, frac = s.split('.', 1)
        s = base + '.' + frac[:6]
    for fmt in ('%Y-%m-%dT%H:%M:%S.%f', '%Y-%m-%dT%H:%M:%S'):
        try: return datetime.strptime(s, fmt).replace(tzinfo=timezone.utc)
        except: pass
f = parse('${FAILURE_TIME}'.rstrip('Z'))
d = parse('$DETECT_TIME')
print(round((d - f).total_seconds() * 1000))
" 2>/dev/null || echo "N/A")
    echo ""
    echo "  ┌─────────────────────────────────────────┐"
    printf "  │  Failure injected : %-20s │\n" "$FAILURE_TIME"
    printf "  │  Detected at      : %-20s │\n" "$DETECT_TIME"
    printf "  │  Detection latency: %-20s │\n" "${LATENCY_MS} ms"
    echo "  └─────────────────────────────────────────┘"
  fi
else
  echo "  [!] No [Detect] event found within ${DETECT_WAIT}s."
  echo "      Check: $LOG_DIR/master.log"
fi

echo ""
echo "  Logs saved to: $LOG_DIR/"
echo "──────────────────────────────────────────────"

# ── Persist structured summary row ────────────────────────────────────────────
RUN_TS="$(date -u +"%Y-%m-%dT%H:%M:%S.%NZ")"
DETECT_TYPE=""
DETECT_SIDE=""
STATUS="ok"
LATENCY_OUT=""
DETECT_TIME_OUT=""

if [[ -n "$DETECTED" ]]; then
  DETECT_TYPE="$(echo "$DETECTED" | grep -oE 'type=[^ ]+' | cut -d= -f2 || true)"
  DETECT_SIDE="$(echo "$DETECTED" | grep -oE 'side=[^ ]+' | cut -d= -f2 || true)"
  DETECT_TIME_OUT="$(echo "$DETECTED" | grep -oE 'time=[^ ]+' | cut -d= -f2 || true)"
  LATENCY_OUT="${LATENCY_MS:-}"
else
  STATUS="no_detect"
fi

echo "${RUN_TS},${CONFIG},${FAILURE_MODE},${KA_TIME},${KA_TIMEOUT},${DELAY_MS},${JITTER_MS},${FAILURE_TIME},${DETECT_TIME_OUT},${DETECT_SIDE},${DETECT_TYPE},${LATENCY_OUT},${STATUS},${LOG_DIR}" >> "$RESULT_CSV"
echo "[summary] appended -> $RESULT_CSV"

# ── Cleanup ───────────────────────────────────────────────────────────────────
docker compose down --remove-orphans 2>/dev/null || true