package activity

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

func (a *Activities) Transcribe(ctx context.Context, input model.TranscribeInput) (model.TranscribeOutput, error) {
	logger := activity.GetLogger(ctx)

	// Heartbeat throughout — covers both the semaphore wait below and any
	// silent phase inside the script (model load, finalisation).
	defer keepalive(ctx, 30*time.Second)()

	wsDir := a.workspaceDir(ctx)
	videoPath := filepath.Join(wsDir, input.Segment.FilePath)

	outDir := filepath.Join(wsDir, fmt.Sprintf("processed/segment-%02d", input.Segment.Index))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return model.TranscribeOutput{}, fmt.Errorf("create output dir: %w", err)
	}
	srtPath := filepath.Join(outDir, "transcript.srt")

	logger.Info("Transcribing segment", "segmentIndex", input.Segment.Index, "video", videoPath, "output", srtPath)

	// Serialise Transcribe runs. Whisper large-v3 occupies ~10 GB; running
	// per-segment children in parallel OOMs the worker.
	logger.Info("Waiting for transcribe slot")
	select {
	case a.transcribeSem <- struct{}{}:
	case <-ctx.Done():
		return model.TranscribeOutput{}, ctx.Err()
	}
	defer func() { <-a.transcribeSem }()
	logger.Info("Acquired transcribe slot")

	scriptPath := filepath.Join(a.ScriptDir, "transcribe.py")
	cmd := exec.CommandContext(ctx, "python3", scriptPath, videoPath, srtPath)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return model.TranscribeOutput{}, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return model.TranscribeOutput{}, fmt.Errorf("start transcribe.py: %w", err)
	}

	scanner := bufio.NewScanner(stderr)
	var stderrLines []string
	for scanner.Scan() {
		line := scanner.Text()
		stderrLines = append(stderrLines, line)
		activity.RecordHeartbeat(ctx, line)
	}
	if err := scanner.Err(); err != nil {
		return model.TranscribeOutput{}, fmt.Errorf("reading stderr: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return model.TranscribeOutput{}, fmt.Errorf("transcribe.py failed: %w\nstderr: %s", err, strings.Join(stderrLines, "\n"))
	}

	relPath := fmt.Sprintf("processed/segment-%02d/transcript.srt", input.Segment.Index)
	logger.Info("Transcription complete", "output", relPath)
	return model.TranscribeOutput{SubtitlePath: relPath}, nil
}
