package activity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

// bumperJSON matches the JSON output of detect_bumpers.py.
type bumperJSON struct {
	VisualStart float64 `json:"visual_start"`
	VisualEnd   float64 `json:"visual_end"`
}

// parseBumperJSON parses the JSON output of detect_bumpers.py into BumperRegions.
func parseBumperJSON(data []byte) ([]model.BumperRegion, error) {
	var raw []bumperJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse bumper JSON: %w", err)
	}
	bumpers := make([]model.BumperRegion, len(raw))
	for i, b := range raw {
		bumpers[i] = model.BumperRegion{VisualStart: b.VisualStart, VisualEnd: b.VisualEnd}
	}
	return bumpers, nil
}

func (a *Activities) DetectBumpers(ctx context.Context, input model.DetectBumpersInput) (model.DetectBumpersOutput, error) {
	logger := activity.GetLogger(ctx)

	// bumpers.json is a human-editable override sidecar (mirrors the
	// metadata.json review contract). When present it is authoritative and
	// detection is skipped — typically because a reviewer hand-corrected a
	// boundary the detector cannot see, e.g. an MC-was-the-speaker handoff
	// with no visual bumper. A manual boundary is a zero-width region
	// {VisualStart: T, VisualEnd: T} at the cut point.
	overridePath := filepath.Join(filepath.Dir(input.VideoPath), "bumpers.json")
	if override, err := readBumperOverride(overridePath); err != nil {
		return model.DetectBumpersOutput{}, err
	} else if override != nil {
		if len(override) == 0 {
			return model.DetectBumpersOutput{}, fmt.Errorf("bumper override %s contains no bumpers", overridePath)
		}
		sortBumpers(override)
		logger.Info("Using bumper override from disk", "path", overridePath, "count", len(override))
		return model.DetectBumpersOutput{Bumpers: override}, nil
	}

	logger.Info("Detecting bumpers", "videoPath", input.VideoPath, "refImage", a.BumperRefImage)

	// The Python script's silencedetect pass runs ffmpeg over the whole video
	// without emitting stderr lines, so the line-based heartbeat below goes
	// quiet for the duration. Keep the activity alive in the background.
	defer keepalive(ctx, 30*time.Second)()

	scriptPath := filepath.Join(a.ScriptDir, "detect_bumpers.py")
	cmd := exec.CommandContext(ctx, "python3", scriptPath,
		input.VideoPath, a.BumperRefImage,
	)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return model.DetectBumpersOutput{}, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return model.DetectBumpersOutput{}, fmt.Errorf("start detect_bumpers.py: %w", err)
	}

	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		stderrBuf.WriteString(line + "\n")
		activity.RecordHeartbeat(ctx, line)
	}
	if err := scanner.Err(); err != nil {
		return model.DetectBumpersOutput{}, fmt.Errorf("reading stderr: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return model.DetectBumpersOutput{}, fmt.Errorf("detect_bumpers.py failed: %w\nstderr: %s", err, stderrBuf.String())
	}

	bumpers, err := parseBumperJSON(stdoutBuf.Bytes())
	if err != nil {
		return model.DetectBumpersOutput{}, err
	}

	if len(bumpers) == 0 {
		return model.DetectBumpersOutput{}, fmt.Errorf("no bumpers detected — every stream should have at least one")
	}

	sortBumpers(bumpers)

	// Persist detected bumpers as the editable override sidecar so a reviewer
	// can correct a missed or spurious boundary and re-run from DetectBumpers.
	// Only written here, when detection actually ran: on a re-run the file
	// already exists and is read above, so human edits are never clobbered.
	if err := writeBumperJSON(overridePath, bumpers); err != nil {
		logger.Warn("Failed to write bumpers.json sidecar", "path", overridePath, "error", err)
	}

	logger.Info("Bumpers detected", "count", len(bumpers))
	return model.DetectBumpersOutput{Bumpers: bumpers}, nil
}

// readBumperOverride loads a hand-editable bumpers.json sidecar. A missing file
// returns (nil, nil) — the signal to fall through to detection — mirroring the
// optional-sidecar pattern in readEvent (event.go).
func readBumperOverride(path string) ([]model.BumperRegion, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read bumper override %s: %w", path, err)
	}
	bumpers, err := parseBumperJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse bumper override %s: %w", path, err)
	}
	// Guarantee non-nil so the caller can distinguish "present" from "absent"
	// even when the file holds an empty array.
	if bumpers == nil {
		bumpers = []model.BumperRegion{}
	}
	return bumpers, nil
}

// writeBumperJSON writes bumpers to path as an indented JSON array using the
// same visual_start/visual_end shape detect_bumpers.py emits, so the file is
// directly hand-editable. Matches the JSON conventions in writeEvent (event.go).
func writeBumperJSON(path string, bumpers []model.BumperRegion) error {
	raw := make([]bumperJSON, len(bumpers))
	for i, b := range bumpers {
		raw[i] = bumperJSON{VisualStart: b.VisualStart, VisualEnd: b.VisualEnd}
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bumpers.json: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write bumpers.json: %w", err)
	}
	return nil
}

// sortBumpers orders bumpers ascending by VisualStart so calculateSegments
// produces monotonic boundaries even if a hand-edited override appended a
// region (e.g. a zero-width manual boundary) out of order.
func sortBumpers(bumpers []model.BumperRegion) {
	sort.Slice(bumpers, func(i, j int) bool {
		return bumpers[i].VisualStart < bumpers[j].VisualStart
	})
}
