#!/usr/bin/env bash
# scripts/auto_run_freeze.sh
# Batch-run freeze experiments (A-H) through run_experiment.sh.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_SCRIPT="${ROOT_DIR}/scripts/run_experiment.sh"
RESULT_CSV="${RESULT_CSV:-${ROOT_DIR}/results/experiment_summary.csv}"
DELAY_MS="${DELAY_MS:-0}"
JITTER_MS="${JITTER_MS:-0}"
WORKER_WAIT="${WORKER_WAIT:-20}"

if [[ ! -x "${RUN_SCRIPT}" ]]; then
  echo "[ERROR] run_experiment.sh not executable: ${RUN_SCRIPT}"
  exit 1
fi

CONFIGS=(A B C D E F G H)

echo "[batch] Freeze sweep configs: ${CONFIGS[*]}"
echo "[batch] delay=${DELAY_MS}ms jitter=${JITTER_MS}ms worker_wait=${WORKER_WAIT}s"
echo "[batch] summary_csv=${RESULT_CSV}"

for cfg in "${CONFIGS[@]}"; do
  echo ""
  echo "========== Running freeze config ${cfg} =========="
  RESULT_CSV="${RESULT_CSV}" \
  DELAY_MS="${DELAY_MS}" \
  JITTER_MS="${JITTER_MS}" \
  WORKER_WAIT="${WORKER_WAIT}" \
    "${RUN_SCRIPT}" "${cfg}" freeze
done

echo ""
echo "[ALL DONE] Batch freeze sweep finished."
echo "[INFO] Unified summary: ${RESULT_CSV}"

