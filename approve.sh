#!/bin/bash
# approve.sh — send a review_approval or upload_approval signal to a
# running PipelineWorkflow.
#
# Usage:
#   ./approve.sh <review|upload> [workflow-id] [--reject]
#
# If workflow-id is omitted, the script auto-detects the single Running
# PipelineWorkflow on the decanter-pipeline task queue. With 0 or >1
# running, it lists candidates and exits non-zero.
#
# --reject sends {"Approved":false} instead of {"Approved":true}.
#
# Reads TEMPORAL_ADDRESS from .env in the script's directory.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "$SCRIPT_DIR/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$SCRIPT_DIR/.env"
    set +a
fi

TEMPORAL_ADDRESS="${TEMPORAL_ADDRESS:-localhost:7233}"

usage() {
    echo "usage: $0 <review|upload> [workflow-id] [--reject]" >&2
    exit 2
}

[[ $# -lt 1 ]] && usage

GATE="$1"
shift

case "$GATE" in
    review) SIGNAL="review_approval" ;;
    upload) SIGNAL="upload_approval" ;;
    *)
        echo "unknown gate: $GATE (expected 'review' or 'upload')" >&2
        exit 2
        ;;
esac

APPROVED=true
WORKFLOW_ID=""

for arg in "$@"; do
    case "$arg" in
        --reject) APPROVED=false ;;
        --*)
            echo "unknown flag: $arg" >&2
            usage
            ;;
        *)
            if [[ -n "$WORKFLOW_ID" ]]; then
                echo "multiple workflow-ids supplied" >&2
                usage
            fi
            WORKFLOW_ID="$arg"
            ;;
    esac
done

if [[ -z "$WORKFLOW_ID" ]]; then
    IDS=()
    while IFS= read -r line; do
        IDS+=("$line")
    done < <(temporal workflow list \
        --address "$TEMPORAL_ADDRESS" \
        --query 'ExecutionStatus="Running" AND WorkflowType="PipelineWorkflow" AND TaskQueue="decanter-pipeline"' \
        --output json 2>/dev/null \
        | jq -r '.[].execution.workflowId')

    if [[ ${#IDS[@]} -eq 0 ]]; then
        echo "no Running PipelineWorkflows on decanter-pipeline" >&2
        exit 1
    fi
    if [[ ${#IDS[@]} -gt 1 ]]; then
        echo "multiple Running PipelineWorkflows — pass workflow-id explicitly:" >&2
        printf '  %s\n' "${IDS[@]}" >&2
        exit 1
    fi
    WORKFLOW_ID="${IDS[0]}"
    echo "→ auto-detected workflow: $WORKFLOW_ID"
fi

JSON=$(printf '{"Approved":%s}' "$APPROVED")
echo "→ signal $SIGNAL → $WORKFLOW_ID → $JSON"

exec temporal workflow signal \
    --address "$TEMPORAL_ADDRESS" \
    --workflow-id "$WORKFLOW_ID" \
    --name "$SIGNAL" \
    --input "$JSON"
