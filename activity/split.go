package activity

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

// parseDuration parses the float64 duration from ffprobe output.
func parseDuration(raw string) (float64, error) {
	trimmed := strings.TrimSpace(raw)
	d, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", trimmed, err)
	}
	return d, nil
}

// parseKeyframePTS parses newline-separated PTS values from ffprobe output
// and returns the largest value that is <= timestamp. Strips ffprobe's
// trailing CSV comma — `-of csv=p=0` emits `<pts>,` per line even with a
// single column.
func parseKeyframePTS(raw string, timestamp float64) (float64, bool) {
	var nearest float64
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimRight(line, ",")
		if line == "" {
			continue
		}
		pts, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		if pts <= timestamp+0.001 && pts >= nearest {
			nearest = pts
			found = true
		}
	}
	return nearest, found
}

// probeKeyframeBefore finds the PTS of the nearest keyframe at or before the given timestamp.
// For timestamps at or near 0, returns 0 with no probing.
func probeKeyframeBefore(ctx context.Context, videoPath string, timestamp float64) (float64, error) {
	if timestamp < 0.1 {
		return 0, nil
	}

	windowStart := timestamp - 15
	if windowStart < 0 {
		windowStart = 0
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-skip_frame", "nokey",
		"-show_entries", "frame=pts_time",
		"-of", "csv=p=0",
		"-read_intervals", fmt.Sprintf("%.3f%%%.3f", windowStart, timestamp+0.1),
		videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		return 0, fmt.Errorf("ffprobe keyframes failed: %w\nstderr: %s", err, stderr)
	}

	nearest, found := parseKeyframePTS(string(out), timestamp)
	if !found {
		return timestamp, nil // no keyframe found; assume no offset
	}
	return nearest, nil
}

// probeDuration uses ffprobe to get the total duration of a video file in seconds.
func probeDuration(ctx context.Context, videoPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		return 0, fmt.Errorf("ffprobe failed: %w\nstderr: %s", err, stderr)
	}
	return parseDuration(string(out))
}

func (a *Activities) Split(ctx context.Context, input model.SplitInput) (model.SplitOutput, error) {
	logger := activity.GetLogger(ctx)

	// Each ffmpeg copy is usually fast, but a multi-hour talk could outlast
	// the HeartbeatTimeout between the per-segment heartbeats below.
	defer keepalive(ctx, 30*time.Second)()

	wsDir := a.workspaceDir(ctx)
	segmentsDir := filepath.Join(wsDir, "segments")
	if err := os.MkdirAll(segmentsDir, 0o755); err != nil {
		return model.SplitOutput{}, fmt.Errorf("create segments dir: %w", err)
	}

	// Get total video duration.
	totalDuration, err := probeDuration(ctx, input.VideoPath)
	if err != nil {
		return model.SplitOutput{}, err
	}
	logger.Info("Video duration", "seconds", totalDuration)

	// Calculate segment boundaries (relative paths).
	segments := calculateSegments(input.Bumpers, totalDuration)
	logger.Info("Splitting video", "segmentCount", len(segments))

	// Run ffmpeg for each segment. These are rough keyframe-aligned copies
	// (-c copy); Assemble will re-cut precisely from the source later.
	for i := range segments {
		seg := &segments[i]
		activity.RecordHeartbeat(ctx, fmt.Sprintf("splitting segment %d/%d", seg.Index+1, len(segments)))
		duration := seg.End - seg.Start
		absPath := filepath.Join(wsDir, seg.FilePath)
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-ss", fmt.Sprintf("%.3f", seg.Start),
			"-i", input.VideoPath,
			"-t", fmt.Sprintf("%.3f", duration),
			"-c", "copy",
			"-avoid_negative_ts", "make_zero",
			"-y",
			absPath,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			return model.SplitOutput{}, fmt.Errorf("ffmpeg split segment %d failed: %w\noutput: %s", seg.Index, err, string(output))
		}

		// Compute how far the keyframe-aligned cut precedes the intended start.
		// Downstream activities (transcription) produce timestamps relative to
		// the rough cut; Assemble uses this offset to shift subtitles.
		keyframePTS, err := probeKeyframeBefore(ctx, input.VideoPath, seg.Start)
		if err != nil {
			return model.SplitOutput{}, fmt.Errorf("probe keyframe for segment %d: %w", seg.Index, err)
		}
		seg.StartOffset = seg.Start - keyframePTS

		logger.Info("Split segment", "index", seg.Index, "start", seg.Start, "end", seg.End, "startOffset", seg.StartOffset, "path", seg.FilePath)
	}

	return model.SplitOutput{Segments: segments}, nil
}
