package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

// claudeResult captures the diagnostic fields from a `claude -p --output-format
// json` result envelope. The envelope carries many more fields (usage, cost,
// session_id, …); we only surface turn count and wall time, which answer the
// "why was this segment's Clean/Gather slow?" question — the cost is the agentic
// turn count, not the transcript length.
type claudeResult struct {
	NumTurns   int    `json:"num_turns"`
	DurationMS int    `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
	Subtype    string `json:"subtype"`
}

// parseClaudeResult decodes the JSON result envelope printed on stdout by
// `claude -p --output-format json`.
func parseClaudeResult(stdout []byte) (claudeResult, error) {
	var r claudeResult
	if err := json.Unmarshal(stdout, &r); err != nil {
		return claudeResult{}, fmt.Errorf("parse claude result envelope: %w", err)
	}
	return r, nil
}

// runClaudeCLI executes a claude CLI command, capturing stdout and stderr into
// buffers instead of letting them inherit the worker's process streams (the
// long-standing diagnostic gap: on failure, claude's own error output vanished).
// On failure the returned error includes stderr. On success it parses the JSON
// result envelope and logs the turn count and wall time so segment-timing
// questions have real numbers. Envelope-parse failures are logged, never fatal —
// the activity's real product is the file(s) claude edited/wrote, not stdout.
//
// A 30s ticker heartbeats the activity throughout the (often multi-minute) call;
// callers must pass a command built with --output-format json.
func runClaudeCLI(ctx context.Context, cmd *exec.Cmd, logger log.Logger, label string) error {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "waiting for Claude response")
			}
		}
	}()
	err := cmd.Run()
	close(done)

	if err != nil {
		return fmt.Errorf("claude CLI failed: %w\nstderr: %s", err, stderr.String())
	}

	if res, perr := parseClaudeResult(stdout.Bytes()); perr != nil {
		logger.Warn("Could not parse claude result envelope for diagnostics",
			"label", label, "error", perr)
	} else {
		logger.Info("Claude CLI completed",
			"label", label, "numTurns", res.NumTurns, "durationMs", res.DurationMS, "subtype", res.Subtype)
	}
	return nil
}
