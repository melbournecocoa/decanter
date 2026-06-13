#!/bin/bash
# ui.sh — launch the Decanter Review Console against the local workspace.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
exec go run ./cmd/decanter-ui "$@"
