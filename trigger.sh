#!/bin/bash
# trigger.sh — kick off a Decanter PipelineWorkflow.
#
# Usage:
#   ./trigger.sh <youtube-url>
#   ./trigger.sh <path-to-local-file>
#
# Arguments starting with http(s):// trigger the YouTube flow. Anything else is
# treated as a local file path: the file is copied into
# $DECANTER_WORKSPACE_PATH/imports/ and the workflow is started with
# LocalFileName set to its basename.
#
# If the local filename contains a Mimolive timestamp
#   "... YYYY-MM-DD HH-MM-SS.<ext>"
# the date is extracted and passed as RecordingDate (RFC3339, local tz).
#
# Reads TEMPORAL_ADDRESS and DECANTER_WORKSPACE_PATH from .env in the script's
# directory.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "$SCRIPT_DIR/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$SCRIPT_DIR/.env"
    set +a
fi

TEMPORAL_ADDRESS="${TEMPORAL_ADDRESS:-localhost:7233}"
DECANTER_WORKSPACE_PATH="${DECANTER_WORKSPACE_PATH:-/tmp/decanter}"

if [[ $# -lt 1 ]]; then
    echo "usage: $0 <youtube-url-or-local-file>" >&2
    exit 2
fi

INPUT="$1"

if [[ "$DECANTER_WORKSPACE_PATH" = /* ]]; then
    WORKSPACE="$DECANTER_WORKSPACE_PATH"
else
    WORKSPACE="$SCRIPT_DIR/$DECANTER_WORKSPACE_PATH"
fi

# Echoes RFC3339 (e.g. 2026-05-14T18:27:04+10:00) for a Mimolive-style basename,
# or nothing if no timestamp is found. Uses macOS `date -j` so the tz offset
# reflects AEST/AEDT for the parsed instant.
extract_recording_date() {
    local name="$1"
    if [[ "$name" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\ ([0-9]{2})-([0-9]{2})-([0-9]{2}) ]]; then
        local date="${BASH_REMATCH[1]}"
        local h="${BASH_REMATCH[2]}"
        local m="${BASH_REMATCH[3]}"
        local s="${BASH_REMATCH[4]}"
        local tz
        if tz=$(date -j -f "%Y-%m-%d %H:%M:%S" "$date $h:$m:$s" "+%z" 2>/dev/null); then
            tz="${tz:0:3}:${tz:3:2}"
            printf '%sT%s:%s:%s%s' "$date" "$h" "$m" "$s" "$tz"
        fi
    fi
}

# Escapes a string for embedding in a JSON string literal.
json_escape() {
    local s="$1"
    s="${s//\\/\\\\}"
    s="${s//\"/\\\"}"
    printf '%s' "$s"
}

NOW=$(date "+%Y%m%d-%H%M%S")

if [[ "$INPUT" =~ ^https?:// ]]; then
    WORKFLOW_ID="decanter-yt-$NOW"
    JSON=$(printf '{"YouTubeURL":"%s"}' "$(json_escape "$INPUT")")
    echo "→ YouTube URL: $INPUT"
else
    if [[ ! -f "$INPUT" ]]; then
        echo "file not found: $INPUT" >&2
        exit 1
    fi
    BASENAME=$(basename "$INPUT")
    IMPORTS_DIR="$WORKSPACE/imports"
    DEST_PATH="$IMPORTS_DIR/$BASENAME"

    mkdir -p "$IMPORTS_DIR"
    if [[ -e "$DEST_PATH" ]]; then
        echo "→ Already in imports: $DEST_PATH"
    else
        echo "→ Copying to $DEST_PATH"
        cp "$INPUT" "$DEST_PATH"
    fi

    REC_DATE=$(extract_recording_date "$BASENAME")
    WORKFLOW_ID="decanter-import-$NOW"

    if [[ -n "$REC_DATE" ]]; then
        echo "→ RecordingDate: $REC_DATE"
        JSON=$(printf '{"LocalFileName":"%s","RecordingDate":"%s"}' \
            "$(json_escape "$BASENAME")" "$REC_DATE")
    else
        echo "→ No recording date found in filename"
        JSON=$(printf '{"LocalFileName":"%s"}' "$(json_escape "$BASENAME")")
    fi
fi

echo "→ WorkflowId: $WORKFLOW_ID"
echo "→ Input: $JSON"

exec temporal workflow start \
    --address "$TEMPORAL_ADDRESS" \
    --task-queue decanter-pipeline \
    --type PipelineWorkflow \
    --workflow-id "$WORKFLOW_ID" \
    --input "$JSON"
