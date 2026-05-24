package activity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

func (a *Activities) Download(ctx context.Context, input model.DownloadInput) (model.DownloadOutput, error) {
	logger := activity.GetLogger(ctx)

	// Derive workspace directory and ensure it exists.
	wsDir := a.workspaceDir(ctx)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return model.DownloadOutput{}, fmt.Errorf("create workspace dir: %w", err)
	}

	outputPath := filepath.Join(wsDir, "source.mp4")
	logger.Info("Downloading video", "url", input.YouTubeURL, "output", outputPath)

	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--newline",
		"--merge-output-format", "mp4",
		"--write-info-json",
		"-o", outputPath,
		input.YouTubeURL,
	)

	// yt-dlp writes progress to stdout and errors to stderr.
	// Combine both into one pipe so we get heartbeats from progress lines.
	// --newline prevents carriage-return overwrites so the scanner sees each update.
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return model.DownloadOutput{}, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return model.DownloadOutput{}, fmt.Errorf("start yt-dlp: %w", err)
	}

	// Read stdout line by line for progress heartbeats.
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		activity.RecordHeartbeat(ctx, scanner.Text())
	}

	if err := cmd.Wait(); err != nil {
		return model.DownloadOutput{}, fmt.Errorf("yt-dlp failed: %w\nstderr: %s", err, stderrBuf.String())
	}

	logger.Info("Download complete", "output", outputPath)

	// Seed event.json. Always read the yt-dlp info.json sidecar (best-effort)
	// to harvest event name and source URL — both are useful even when the
	// trigger already supplied a recording date. The supplied RecordingDate,
	// if any, then overrides the date harvested from info.json.
	//
	// yt-dlp strips the output template's extension before appending
	// .info.json (source.mp4 → source.info.json), so derive the sidecar path
	// the same way rather than naively concatenating.
	infoPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".info.json"
	event, err := readEventFromYtdlp(infoPath)
	if err != nil {
		logger.Warn("Failed to read yt-dlp info.json", "error", err)
	}
	if input.RecordingDate != "" {
		event.RecordingDate = input.RecordingDate
	}
	if err := writeEvent(filepath.Join(wsDir, eventFileName), event); err != nil {
		logger.Warn("Failed to write event.json", "error", err)
	}

	return model.DownloadOutput{VideoPath: outputPath}, nil
}

// ytdlpInfoJSON captures the fields we care about from yt-dlp's auto-emitted
// info.json. yt-dlp writes dozens of other fields; we ignore them.
type ytdlpInfoJSON struct {
	ReleaseTimestamp *int64 `json:"release_timestamp"`
	ReleaseDate      string `json:"release_date"`
	UploadDate       string `json:"upload_date"`
	Title            string `json:"title"`
	WebpageURL       string `json:"webpage_url"`
}

// readEventFromYtdlp reads the yt-dlp info.json at infoPath and returns the
// event-level fields it carries (recording date, event name, source URL).
// A missing info.json returns a zero-valued EventMetadata with no error so the
// caller can fall through to writing an empty stub. Returns an error only for
// non-ENOENT I/O failures or unparseable JSON.
func readEventFromYtdlp(infoPath string) (model.EventMetadata, error) {
	raw, err := os.ReadFile(infoPath)
	if errors.Is(err, os.ErrNotExist) {
		return model.EventMetadata{}, nil
	}
	if err != nil {
		return model.EventMetadata{}, fmt.Errorf("read info.json: %w", err)
	}
	var info ytdlpInfoJSON
	if err := json.Unmarshal(raw, &info); err != nil {
		return model.EventMetadata{}, fmt.Errorf("parse info.json: %w", err)
	}
	return model.EventMetadata{
		RecordingDate: pickRecordingDate(info),
		EventName:     info.Title,
		SourceURL:     info.WebpageURL,
	}, nil
}

// pickRecordingDate selects the most precise recording date available, in
// priority order: release_timestamp (epoch UTC, broadcast start for live
// streams) > release_date (YYYYMMDD) > upload_date (YYYYMMDD, publish date,
// approximates broadcast for live content). Returns "" if none available.
func pickRecordingDate(info ytdlpInfoJSON) string {
	if info.ReleaseTimestamp != nil {
		return time.Unix(*info.ReleaseTimestamp, 0).UTC().Format(time.RFC3339)
	}
	if d := formatYtdlpDate(info.ReleaseDate); d != "" {
		return d
	}
	return formatYtdlpDate(info.UploadDate)
}

// formatYtdlpDate converts yt-dlp's YYYYMMDD date string to a midnight-UTC
// RFC3339 string. Returns "" if the input is empty or malformed.
func formatYtdlpDate(d string) string {
	if len(d) != 8 {
		return ""
	}
	t, err := time.Parse("20060102", d)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
