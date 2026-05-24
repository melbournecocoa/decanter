package activity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
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

	logger.Info("Bumpers detected", "count", len(bumpers))
	return model.DetectBumpersOutput{Bumpers: bumpers}, nil
}
