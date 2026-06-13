package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
)

// Controller mutates a workflow via the `temporal` CLI (parity with approve.sh
// and the documented reset recipes).
type Controller interface {
	Signal(ctx context.Context, workflowID, gate string, approved bool) error
	Reset(ctx context.Context, workflowID string, eventID int64, reason string) error
}

// gateSignal maps the UI gate key to the workflow signal name.
func gateSignal(gate string) string {
	if gate == "upload" {
		return "upload_approval"
	}
	return "review_approval"
}

func buildSignalArgs(addr, workflowID, gate string, approved bool) []string {
	return []string{
		"workflow", "signal",
		"--address", addr,
		"--workflow-id", workflowID,
		"--name", gateSignal(gate),
		"--input", fmt.Sprintf(`{"Approved":%t}`, approved),
	}
}

func buildResetArgs(addr, workflowID string, eventID int64, reason string) []string {
	return []string{
		"workflow", "reset",
		"--address", addr,
		"--workflow-id", workflowID,
		"--event-id", strconv.FormatInt(eventID, 10),
		"--reapply-exclude", "Signal",
		"--reason", reason,
	}
}

// cliController runs the real `temporal` binary.
type cliController struct {
	bin  string // "temporal"
	addr string
}

func NewCLIController(bin, addr string) Controller {
	if bin == "" {
		bin = "temporal"
	}
	return &cliController{bin: bin, addr: addr}
}

func (c *cliController) run(ctx context.Context, args []string) error {
	out, err := exec.CommandContext(ctx, c.bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", c.bin, args, err, out)
	}
	return nil
}

func (c *cliController) Signal(ctx context.Context, workflowID, gate string, approved bool) error {
	return c.run(ctx, buildSignalArgs(c.addr, workflowID, gate, approved))
}

func (c *cliController) Reset(ctx context.Context, workflowID string, eventID int64, reason string) error {
	return c.run(ctx, buildResetArgs(c.addr, workflowID, eventID, reason))
}
