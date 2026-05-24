package activity

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// AdjustSRT shifts and clips SRT entries from rough-segment-file time to
// final-video time. The formula applied is:
//
//	final_t = whisper_t - startOffset + introDuration - xfadeDuration
//
// xfadeDuration accounts for the cross-fade Assemble performs at the
// intro→content join: content's t=0 lands at t_final = introDuration -
// xfadeDuration. Pass 0 if Assemble is doing a hard cut.
//
// Entries entirely outside the content window are dropped; entries straddling
// a boundary are clipped. Surviving entries are renumbered 1..N. Times are in
// seconds.
func AdjustSRT(in []byte, startOffset, introDuration, contentDuration, xfadeDuration float64) ([]byte, error) {
	entries, err := parseSRT(in)
	if err != nil {
		return nil, err
	}

	introShift := introDuration - xfadeDuration
	contentEnd := introShift + contentDuration
	var kept []srtEntry
	for _, e := range entries {
		// Whisper times are relative to the rough segment start. Convert to
		// time-relative-to-content-start by subtracting startOffset.
		whisperStart := e.start - startOffset
		whisperEnd := e.end - startOffset

		// Drop if entirely before or after the content window.
		if whisperEnd < 0 || whisperStart > contentDuration {
			continue
		}

		finalStart := whisperStart + introShift
		finalEnd := whisperEnd + introShift

		if finalStart < introShift {
			finalStart = introShift
		}
		if finalEnd > contentEnd {
			finalEnd = contentEnd
		}

		kept = append(kept, srtEntry{
			start: finalStart,
			end:   finalEnd,
			text:  e.text,
		})
	}

	return formatSRT(kept), nil
}

type srtEntry struct {
	start float64 // seconds
	end   float64 // seconds
	text  []string
}

func parseSRT(in []byte) ([]srtEntry, error) {
	// Normalise line endings and split into blocks separated by blank lines.
	src := strings.ReplaceAll(string(in), "\r\n", "\n")
	src = strings.Trim(src, "\n")
	blocks := strings.Split(src, "\n\n")

	var entries []srtEntry
	for i, block := range blocks {
		block = strings.Trim(block, "\n")
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		if len(lines) < 2 {
			return nil, fmt.Errorf("srt block %d: expected at least 2 lines, got %d", i+1, len(lines))
		}
		// First line is the entry number. We don't preserve it; renumber on output.
		if _, err := strconv.Atoi(strings.TrimSpace(lines[0])); err != nil {
			return nil, fmt.Errorf("srt block %d: bad entry number %q: %w", i+1, lines[0], err)
		}
		start, end, err := parseTimeRange(lines[1])
		if err != nil {
			return nil, fmt.Errorf("srt block %d: %w", i+1, err)
		}
		entries = append(entries, srtEntry{
			start: start,
			end:   end,
			text:  lines[2:],
		})
	}
	return entries, nil
}

func parseTimeRange(line string) (start, end float64, err error) {
	parts := strings.Split(line, "-->")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad timerange %q", line)
	}
	start, err = parseSRTTime(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	end, err = parseSRTTime(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func parseSRTTime(s string) (float64, error) {
	// Format: HH:MM:SS,mmm
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad timestamp %q (want HH:MM:SS,mmm)", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("bad hour in %q: %w", s, err)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("bad minute in %q: %w", s, err)
	}
	secStr := strings.Replace(parts[2], ",", ".", 1)
	sec, err := strconv.ParseFloat(secStr, 64)
	if err != nil {
		return 0, fmt.Errorf("bad seconds in %q: %w", s, err)
	}
	return float64(h)*3600 + float64(m)*60 + sec, nil
}

func formatSRTTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalMs := int64(seconds*1000 + 0.5)
	ms := totalMs % 1000
	totalSec := totalMs / 1000
	s := totalSec % 60
	totalMin := totalSec / 60
	m := totalMin % 60
	h := totalMin / 60
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func formatSRT(entries []srtEntry) []byte {
	var buf bytes.Buffer
	for i, e := range entries {
		fmt.Fprintf(&buf, "%d\n", i+1)
		fmt.Fprintf(&buf, "%s --> %s\n", formatSRTTime(e.start), formatSRTTime(e.end))
		for _, line := range e.text {
			fmt.Fprintln(&buf, line)
		}
		fmt.Fprintln(&buf)
	}
	return buf.Bytes()
}
