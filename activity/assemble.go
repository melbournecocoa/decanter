package activity

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

// assembleXfadeDuration is the cosmetic dissolve duration applied at the
// intro→content and content→outro joins (seconds). Short enough to feel like
// polish, not a transition.
const assembleXfadeDuration = 0.3

// assembleOutputFPS normalises all video inputs to a common framerate before
// xfade. xfade requires both pads to share fps AND timebase; intro/outro are
// 25fps Keynote exports while yt-dlp source is 30fps with tbn=1/15360, so we
// resample to a single rate and pin the timebase via settb=1/30000 below.
const assembleOutputFPS = 30

// thumbnailOffsetSeconds is how far past contentStart Assemble pulls a frame
// for the YouTube thumbnail. CocoaHeads speakers almost always have a title
// slide up by this point, which is what we want on the thumbnail tile.
const thumbnailOffsetSeconds = 3.0

// resolveContentRange translates reviewer-supplied trim (in rough-cut local
// time) into source-timeline start/end. Rough cut local 0 corresponds to
// source time (seg.Start - seg.StartOffset). nil trim = no override = current
// behaviour (cut at seg.Start..seg.End). Returns an error for invalid ranges.
func resolveContentRange(seg model.Segment, trim *model.TrimRange) (startSrc, endSrc float64, err error) {
	if trim == nil {
		return seg.Start, seg.End, nil
	}
	roughStartInSource := seg.Start - seg.StartOffset
	startSrc = roughStartInSource + trim.StartSeconds
	endSrc = roughStartInSource + trim.EndSeconds
	if startSrc < 0 {
		return 0, 0, fmt.Errorf("trim.startSeconds=%.3f resolves to negative source time (rough cut starts at source=%.3f)", trim.StartSeconds, roughStartInSource)
	}
	if endSrc <= startSrc {
		return 0, 0, fmt.Errorf("trim.endSeconds=%.3f must be greater than trim.startSeconds=%.3f", trim.EndSeconds, trim.StartSeconds)
	}
	return startSrc, endSrc, nil
}

// assembleLoudnorm is the loudness target applied to every Assemble input.
const assembleLoudnorm = "loudnorm=I=-14:LRA=11:TP=-1.5"

// Auto pre-gain tuning. Talks measuring below autoGainThresholdLUFS get lifted
// to autoGainHeadroomDB under the loudnorm target, capped at autoGainMaxDB so
// a near-silent recording doesn't get its noise floor dragged up 50dB.
const (
	assembleTargetLUFS    = -14.0
	autoGainThresholdLUFS = -30.0
	autoGainHeadroomDB    = 4.0
	autoGainMaxDB         = 40.0
)

var integratedLoudnessRe = regexp.MustCompile(`(?m)^\s*I:\s+(-?[0-9.]+) LUFS`)

// parseIntegratedLoudness pulls the integrated loudness (LUFS) out of an
// ebur128 filter summary on ffmpeg's stderr.
func parseIntegratedLoudness(stderr string) (float64, error) {
	m := integratedLoudnessRe.FindStringSubmatch(stderr)
	if m == nil {
		return 0, fmt.Errorf("no integrated loudness in ebur128 output")
	}
	return strconv.ParseFloat(m[1], 64)
}

// measureLoudness runs a fast audio-only ebur128 pass over the content range.
// Don't measure with loudnorm itself: it upsamples to 192kHz and runs ~100x
// slower than ebur128.
func measureLoudness(ctx context.Context, sourcePath string, start, duration float64) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-nostats",
		"-ss", fmt.Sprintf("%.3f", start),
		"-t", fmt.Sprintf("%.3f", duration),
		"-i", sourcePath,
		"-vn",
		"-af", "ebur128=framelog=quiet",
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffmpeg ebur128: %w", err)
	}
	return parseIntegratedLoudness(string(out))
}

// chooseAudioGain decides the content pre-gain. A reviewer-set manualDB always
// wins; otherwise a measured talk below the threshold is auto-lifted. A failed
// measurement degrades to no pre-gain (the pre-existing behaviour). source is
// "manual", "auto" or "none", for logging.
func chooseAudioGain(manualDB, measuredLUFS float64, measureErr error) (gainDB float64, source string) {
	if manualDB != 0 {
		return manualDB, "manual"
	}
	if measureErr != nil || measuredLUFS >= autoGainThresholdLUFS {
		return 0, "none"
	}
	gain := math.Round((assembleTargetLUFS-measuredLUFS-autoGainHeadroomDB)*10) / 10
	return math.Min(gain, autoGainMaxDB), "auto"
}

// contentAudioFilter builds the filter chain for the content audio pad. A
// non-zero gainDB (from chooseAudioGain) is applied as a fixed pre-gain ahead
// of loudnorm. Single-pass loudnorm only ramps its gain up
// gradually, so on a very quiet recording (~-48 LUFS vs the -14 target) the
// first ~40s come out soft; lifting the input near the target first removes
// that ramp. Filters run in float, so a large pre-gain doesn't clip before
// loudnorm's limiter.
func contentAudioFilter(gainDB float64) string {
	if gainDB == 0 {
		return "[1:a]" + assembleLoudnorm + "[a1]"
	}
	return fmt.Sprintf("[1:a]volume=%.1fdB,%s[a1]", gainDB, assembleLoudnorm)
}

// resolveIntroPath picks the intro file Assemble should use. If the event's
// RecordingDate parses cleanly and a year-specific sibling of the configured
// base intro exists on disk (e.g. assets/intro.m4v → assets/intro-2026.m4v),
// it's returned; otherwise the base path is returned unchanged. Every failure
// mode (empty date, malformed date, missing sibling) falls back silently to
// base — sponsors change, the base intro is the safety net.
func resolveIntroPath(baseIntroPath, recordingDate string) string {
	if recordingDate == "" {
		return baseIntroPath
	}
	t, err := time.Parse(time.RFC3339, recordingDate)
	if err != nil {
		return baseIntroPath
	}
	loc, err := time.LoadLocation(melbourneTZ)
	if err != nil {
		return baseIntroPath
	}
	year := t.In(loc).Year()

	ext := filepath.Ext(baseIntroPath)
	stem := strings.TrimSuffix(baseIntroPath, ext)
	candidate := fmt.Sprintf("%s-%d%s", stem, year, ext)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return baseIntroPath
}

func (a *Activities) Assemble(ctx context.Context, input model.AssembleInput) (model.AssembleOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Assembling segment", "segmentIndex", input.Segment.Index)

	defer keepalive(ctx, 30*time.Second)()

	wsDir := a.workspaceDir(ctx)
	// Source is always at <workspace>/source.mp4 — written there by Download
	// or Import. Assemble re-cuts from this master, not from the rough
	// segment file in segments/, to get a frame-accurate cut in the same
	// re-encode pass.
	sourcePath := filepath.Join(wsDir, "source.mp4")
	srtSrcPath := filepath.Join(wsDir, input.SubtitlePath)
	outDir := filepath.Join(wsDir, fmt.Sprintf("processed/segment-%02d", input.Segment.Index))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return model.AssembleOutput{}, fmt.Errorf("create output dir: %w", err)
	}
	outVideoAbs := filepath.Join(outDir, "final.mp4")
	outSRTAbs := filepath.Join(outDir, "final.srt")
	outVideoRel := fmt.Sprintf("processed/segment-%02d/final.mp4", input.Segment.Index)
	outSRTRel := fmt.Sprintf("processed/segment-%02d/final.srt", input.Segment.Index)

	// Apply reviewer-supplied trim (if any) from metadata.json. The file is
	// the authoritative source at activity time — reviewers edit it during the
	// review_approval gate. Trim defaults pre-populated by GatherMetadata
	// reproduce current behaviour; an explicit edit shifts the cut points.
	metadata, err := readMetadata(filepath.Join(outDir, "metadata.json"))
	if err != nil {
		return model.AssembleOutput{}, fmt.Errorf("read metadata.json: %w", err)
	}
	contentStart, contentEnd, err := resolveContentRange(input.Segment, metadata.Trim)
	if err != nil {
		return model.AssembleOutput{}, fmt.Errorf("resolve content range: %w", err)
	}
	contentDuration := contentEnd - contentStart
	roughStartInSource := input.Segment.Start - input.Segment.StartOffset
	effectiveStartOffset := contentStart - roughStartInSource
	if metadata.Trim != nil {
		logger.Info("Resolved content range",
			"segStart", input.Segment.Start, "segEnd", input.Segment.End,
			"contentStart", contentStart, "contentEnd", contentEnd,
			"effectiveStartOffset", effectiveStartOffset, "contentDuration", contentDuration)
	}

	// Pick the year-specific intro if one exists for this event's recording
	// date; otherwise the configured base intro. Malformed/missing event.json
	// degrades to base — same outcome as before this feature existed.
	ev, err := readEvent(filepath.Join(wsDir, eventFileName))
	if err != nil {
		logger.Warn("Read event.json — falling back to base intro", "error", err)
		ev = model.EventMetadata{}
	}
	introPath := resolveIntroPath(a.IntroVideoPath, ev.RecordingDate)
	if introPath != a.IntroVideoPath {
		logger.Info("Resolved year-specific intro", "configured", a.IntroVideoPath, "resolved", introPath)
	}

	introDuration, err := probeDuration(ctx, introPath)
	if err != nil {
		return model.AssembleOutput{}, fmt.Errorf("probe intro duration: %w", err)
	}
	logger.Info("Probed durations", "intro", introDuration, "content", contentDuration)

	srtIn, err := os.ReadFile(srtSrcPath)
	if err != nil {
		return model.AssembleOutput{}, fmt.Errorf("read source SRT: %w", err)
	}
	srtOut, err := AdjustSRT(srtIn, effectiveStartOffset, introDuration, contentDuration, assembleXfadeDuration)
	if err != nil {
		return model.AssembleOutput{}, fmt.Errorf("adjust SRT: %w", err)
	}
	if err := os.WriteFile(outSRTAbs, srtOut, 0o644); err != nil {
		return model.AssembleOutput{}, fmt.Errorf("write adjusted SRT: %w", err)
	}

	// Measure the content's loudness so a very quiet recording gets pre-gained
	// ahead of loudnorm (see chooseAudioGain). Skipped when the reviewer set
	// audioGainDb by hand. Measurement failure is non-fatal.
	var measuredLUFS float64
	var measureErr error
	if metadata.AudioGainDB == 0 {
		stopKeepalive := keepalive(ctx, 30*time.Second)
		measuredLUFS, measureErr = measureLoudness(ctx, sourcePath, contentStart, contentDuration)
		stopKeepalive()
		if measureErr != nil {
			logger.Warn("Loudness measurement failed — no pre-gain", "error", measureErr)
		}
	}
	audioGainDB, gainSource := chooseAudioGain(metadata.AudioGainDB, measuredLUFS, measureErr)
	logger.Info("Content audio gain", "measuredLUFS", measuredLUFS, "gainDB", audioGainDB, "source", gainSource)

	// xfade consumes the last D seconds of stream A and the first D seconds of
	// stream B, producing output of length La + Lb − D. Chain two of them to
	// cross-fade intro→content and content→outro.
	introXfadeOffset := introDuration - assembleXfadeDuration
	icXfadeOffset := introDuration + contentDuration - 2*assembleXfadeDuration

	filterComplex := strings.Join([]string{
		fmt.Sprintf("[0:v]format=yuv420p,setsar=1,fps=%d,settb=1/%d000[v0]", assembleOutputFPS, assembleOutputFPS),
		fmt.Sprintf("[1:v]format=yuv420p,setsar=1,fps=%d,settb=1/%d000[v1]", assembleOutputFPS, assembleOutputFPS),
		fmt.Sprintf("[2:v]format=yuv420p,setsar=1,fps=%d,settb=1/%d000[v2]", assembleOutputFPS, assembleOutputFPS),
		fmt.Sprintf("[v0][v1]xfade=transition=fade:duration=%.3f:offset=%.3f[vic]", assembleXfadeDuration, introXfadeOffset),
		fmt.Sprintf("[vic][v2]xfade=transition=fade:duration=%.3f:offset=%.3f[outv]", assembleXfadeDuration, icXfadeOffset),
		"[0:a]" + assembleLoudnorm + "[a0]",
		contentAudioFilter(audioGainDB),
		"[2:a]" + assembleLoudnorm + "[a2]",
		fmt.Sprintf("[a0][a1]acrossfade=d=%.3f:c1=tri:c2=tri[aic]", assembleXfadeDuration),
		fmt.Sprintf("[aic][a2]acrossfade=d=%.3f:c1=tri:c2=tri[outa]", assembleXfadeDuration),
	}, ";")

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", introPath,
		"-ss", fmt.Sprintf("%.3f", contentStart),
		"-t", fmt.Sprintf("%.3f", contentDuration),
		"-i", sourcePath,
		"-i", a.OutroVideoPath,
		"-filter_complex", filterComplex,
		"-map", "[outv]",
		"-map", "[outa]",
		// Encode on the Apple Silicon media engine (h264_videotoolbox), not
		// libx264. The CPU-bound x264 path oversubscribed the M1 Pro under the
		// parallel Assemble fan-out and ran sub-realtime; offloading the encode
		// to the dedicated HW engine frees the cores for the filter graphs
		// (xfade/loudnorm run on CPU regardless of encoder). VideoToolbox has no
		// -crf/-preset — it's bitrate-driven, and being less efficient per-bit
		// than x264 we target a generous 12M for these low-motion slides +
		// talking-head cuts. -allow_sw falls back to software encode if the
		// hardware engine is unavailable.
		"-c:v", "h264_videotoolbox",
		"-b:v", "12M",
		"-allow_sw", "1",
		"-pix_fmt", "yuv420p",
		"-profile:v", "high",
		"-level", "4.0",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ar", "48000",
		"-ac", "2",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		outVideoAbs,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return model.AssembleOutput{}, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return model.AssembleOutput{}, fmt.Errorf("start ffmpeg: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		// ffmpeg -progress writes key=value lines; out_time_ms is the most useful one.
		if strings.HasPrefix(line, "out_time_ms=") {
			val := strings.TrimPrefix(line, "out_time_ms=")
			if ms, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
				activity.RecordHeartbeat(ctx, ms/1_000_000)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return model.AssembleOutput{}, fmt.Errorf("ffmpeg failed: %w\nstderr: %s", err, stderrBuf.String())
	}

	// Best-effort thumbnail extraction. contentStart already reflects any
	// reviewer-supplied trim, so the offset lands inside the (possibly
	// trimmed) content window — typically on the speaker's title slide.
	// Failures here do not fail Assemble: Upload will log the absence and
	// fall back to YouTube's auto-pick.
	thumbAbs := filepath.Join(outDir, "thumbnail.jpg")
	thumbSeek := contentStart + thumbnailOffsetSeconds
	thumbCmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.3f", thumbSeek),
		"-i", sourcePath,
		"-frames:v", "1",
		"-update", "1",
		"-vf", "scale=1280:720:flags=lanczos",
		"-q:v", "2",
		thumbAbs,
	)
	var thumbStderr strings.Builder
	thumbCmd.Stderr = &thumbStderr
	if err := thumbCmd.Run(); err != nil {
		logger.Warn("Thumbnail extraction failed — continuing without",
			"seek", thumbSeek, "error", err, "stderr", thumbStderr.String())
	} else if _, err := os.Stat(thumbAbs); err != nil {
		logger.Warn("Thumbnail extraction reported success but no file written — continuing without",
			"path", thumbAbs, "stderr", thumbStderr.String())
	} else {
		logger.Info("Thumbnail extracted", "path", thumbAbs, "seek", thumbSeek)
		activity.RecordHeartbeat(ctx, "thumbnail extracted")
	}

	logger.Info("Assemble complete", "video", outVideoRel, "srt", outSRTRel)
	return model.AssembleOutput{Video: model.AssembledVideo{
		SegmentIndex:    input.Segment.Index,
		VideoPath:       outVideoRel,
		SubtitlePath:    outSRTRel,
		IntroDuration:   introDuration,
		ContentDuration: contentDuration,
		StartOffset:     effectiveStartOffset,
		XfadeDuration:   assembleXfadeDuration,
	}}, nil
}
