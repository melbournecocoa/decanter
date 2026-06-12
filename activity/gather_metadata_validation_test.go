package activity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/melbournecocoa/decanter/model"
)

// TestTrimAccuracy_2022Batch is a report-only accuracy harness, NOT a CI test.
// It re-runs the GatherMetadata prompt against the cleaned transcripts of the
// 2022 batch and compares the model's proposed trim to the human-tuned trim
// already on disk (ground truth). Skipped unless DECANTER_TRIM_VALIDATION=1.
//
// DECANTER_WORKSPACE_PATH must be ABSOLUTE: `go test` runs with the working
// directory set to the package dir (activity/), so a relative ./workspace would
// resolve to activity/workspace and find nothing.
//
// Run with:
//   DECANTER_TRIM_VALIDATION=1 DECANTER_WORKSPACE_PATH="$(pwd)/workspace" \
//     go test ./activity -run TestTrimAccuracy_2022Batch -v -timeout 30m
func TestTrimAccuracy_2022Batch(t *testing.T) {
	if os.Getenv("DECANTER_TRIM_VALIDATION") != "1" {
		t.Skip("set DECANTER_TRIM_VALIDATION=1 to run the trim accuracy harness")
	}
	wsRoot := os.Getenv("DECANTER_WORKSPACE_PATH")
	if wsRoot == "" {
		wsRoot = "workspace"
	}

	// Report tolerances for clean cases. Flagged/ambiguous cases are expected to
	// exceed these and are reported (REVIEW), not failed.
	const startTolerance = 3.0
	const endTolerance = 5.0
	// A segment counts as human-tuned (worth scoring) when its ground-truth trim
	// cuts a meaningful amount off either end; otherwise the on-disk trim is just
	// the old mechanical default and is not a useful label.
	const headTrimThreshold = 5.0
	const tailTrimThreshold = 8.0

	runDirs, err := filepath.Glob(filepath.Join(wsRoot, "decanter-*"))
	if err != nil {
		t.Fatalf("glob workspaces: %v", err)
	}

	type row struct {
		label              string
		wantStart, wantEnd float64
		gotStart, gotEnd   float64
		ok                 bool
	}
	var rows []row
	scored, within := 0, 0

	for _, dir := range runDirs {
		ev, err := os.ReadFile(filepath.Join(dir, "event.json"))
		if err != nil {
			continue
		}
		var event model.EventMetadata
		if json.Unmarshal(ev, &event) != nil || !strings.HasPrefix(event.RecordingDate, "2022") {
			continue
		}
		segDirs, _ := filepath.Glob(filepath.Join(dir, "processed", "segment-*"))
		for _, segDir := range segDirs {
			srtBytes, err := os.ReadFile(filepath.Join(segDir, "transcript_clean.srt"))
			if err != nil {
				continue // not a talk segment (no cleaned transcript)
			}
			mdBytes, err := os.ReadFile(filepath.Join(segDir, "metadata.json"))
			if err != nil {
				continue
			}
			var truth model.TalkMetadata
			if json.Unmarshal(mdBytes, &truth) != nil || truth.Trim == nil || truth.Skip {
				continue
			}
			entries, err := parseSRT(srtBytes)
			if err != nil || len(entries) == 0 {
				continue
			}
			srtDur := entries[len(entries)-1].end
			headTrim := truth.Trim.StartSeconds
			tailTrim := srtDur - truth.Trim.EndSeconds
			if headTrim < headTrimThreshold && tailTrim < tailTrimThreshold {
				continue // ground truth ~= mechanical default; skip as a label
			}

			// Run the model against a temp copy so real files are never touched.
			tmp := t.TempDir()
			tmpSRT := filepath.Join(tmp, "transcript_clean.srt")
			if err := os.WriteFile(tmpSRT, srtBytes, 0o644); err != nil {
				t.Fatalf("write temp srt: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			cmd := gatherMetadataCommand(ctx, buildGatherMetadataPrompt(tmpSRT, ""))
			runErr := cmd.Run()
			cancel()

			label := filepath.Base(dir) + "/" + filepath.Base(segDir)
			if runErr != nil {
				t.Logf("%s: claude run failed: %v", label, runErr)
				continue
			}
			gotBytes, err := os.ReadFile(filepath.Join(tmp, "metadata.json"))
			if err != nil {
				t.Logf("%s: no metadata.json produced: %v", label, err)
				continue
			}
			var got model.TalkMetadata
			if err := json.Unmarshal(gotBytes, &got); err != nil || got.Trim == nil {
				t.Logf("%s: model produced no usable trim", label)
				continue
			}
			ok := absDiff(got.Trim.StartSeconds, truth.Trim.StartSeconds) <= startTolerance &&
				absDiff(got.Trim.EndSeconds, truth.Trim.EndSeconds) <= endTolerance
			scored++
			if ok {
				within++
			}
			rows = append(rows, row{label, truth.Trim.StartSeconds, truth.Trim.EndSeconds,
				got.Trim.StartSeconds, got.Trim.EndSeconds, ok})
		}
	}

	t.Log("=== Trim accuracy vs 2022 human-tuned ground truth ===")
	for _, r := range rows {
		flag := "WITHIN"
		if !r.ok {
			flag = "REVIEW"
		}
		t.Logf("%-45s want[%.1f, %.1f] got[%.1f, %.1f] dStart=%+.1f dEnd=%+.1f  %s",
			r.label, r.wantStart, r.wantEnd, r.gotStart, r.gotEnd,
			r.gotStart-r.wantStart, r.gotEnd-r.wantEnd, flag)
	}
	if scored > 0 {
		t.Logf("SUMMARY: %d/%d within tolerance (start +-%.0fs, end +-%.0fs)",
			within, scored, startTolerance, endTolerance)
	} else {
		t.Logf("SUMMARY: no human-tuned 2022 segments found under %s", wsRoot)
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
