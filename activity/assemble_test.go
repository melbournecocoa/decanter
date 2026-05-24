package activity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestResolveContentRange(t *testing.T) {
	// Synthetic segment: intended source range [100, 3700], rough cut starts
	// 0.726s earlier than intended (the typical bumper bleed).
	seg := model.Segment{
		Index:       2,
		Start:       100.0,
		End:         3700.0,
		StartOffset: 0.726,
	}
	const so = 0.726
	const d = 3600.0 // seg.End - seg.Start

	tests := []struct {
		name      string
		trim      *model.TrimRange
		wantStart float64
		wantEnd   float64
		wantErr   bool
	}{
		{
			name:      "nil trim falls through to segment defaults",
			trim:      nil,
			wantStart: 100.0,
			wantEnd:   3700.0,
		},
		{
			name:      "defaults (start=so, end=so+D) reproduce segment range",
			trim:      &model.TrimRange{StartSeconds: so, EndSeconds: so + d},
			wantStart: 100.0,
			wantEnd:   3700.0,
		},
		{
			name:      "start trimmed forward 20s",
			trim:      &model.TrimRange{StartSeconds: so + 20, EndSeconds: so + d},
			wantStart: 120.0,
			wantEnd:   3700.0,
		},
		{
			name:      "end trimmed back 30s",
			trim:      &model.TrimRange{StartSeconds: so, EndSeconds: so + d - 30},
			wantStart: 100.0,
			wantEnd:   3670.0,
		},
		{
			name:      "both trimmed",
			trim:      &model.TrimRange{StartSeconds: so + 20, EndSeconds: so + d - 30},
			wantStart: 120.0,
			wantEnd:   3670.0,
		},
		{
			name:    "invalid: start >= end",
			trim:    &model.TrimRange{StartSeconds: so + d, EndSeconds: so},
			wantErr: true,
		},
		{
			name:    "invalid: contentStart negative",
			trim:    &model.TrimRange{StartSeconds: -200, EndSeconds: so + d},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd, err := resolveContentRange(seg, tt.trim)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got start=%.3f end=%.3f", gotStart, gotEnd)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotStart != tt.wantStart {
				t.Errorf("start: got %.3f, want %.3f", gotStart, tt.wantStart)
			}
			if gotEnd != tt.wantEnd {
				t.Errorf("end: got %.3f, want %.3f", gotEnd, tt.wantEnd)
			}
		})
	}
}

func TestResolveIntroPath(t *testing.T) {
	tests := []struct {
		name          string
		baseName      string
		recordingDate string
		existingFiles []string
		wantName      string
	}{
		{
			name:          "year-specific exists",
			baseName:      "intro.m4v",
			recordingDate: "2021-05-15T19:00:00+10:00",
			existingFiles: []string{"intro-2021.m4v"},
			wantName:      "intro-2021.m4v",
		},
		{
			name:          "year-specific missing falls back to base",
			baseName:      "intro.m4v",
			recordingDate: "2021-05-15T19:00:00+10:00",
			existingFiles: nil,
			wantName:      "intro.m4v",
		},
		{
			name:          "empty RecordingDate falls back to base",
			baseName:      "intro.m4v",
			recordingDate: "",
			existingFiles: []string{"intro-2021.m4v"},
			wantName:      "intro.m4v",
		},
		{
			name:          "malformed date falls back to base",
			baseName:      "intro.m4v",
			recordingDate: "not-a-date",
			existingFiles: []string{"intro-2021.m4v"},
			wantName:      "intro.m4v",
		},
		{
			name:          "TZ edge: late-night UTC rolls into next Melbourne year",
			baseName:      "intro.m4v",
			recordingDate: "2025-12-31T23:30:00Z",
			existingFiles: []string{"intro-2026.m4v"},
			wantName:      "intro-2026.m4v",
		},
		{
			name:          "path without extension",
			baseName:      "intro",
			recordingDate: "2021-05-15T19:00:00+10:00",
			existingFiles: []string{"intro-2021"},
			wantName:      "intro-2021",
		},
		{
			name:          "different extension",
			baseName:      "intro.mov",
			recordingDate: "2021-05-15T19:00:00+10:00",
			existingFiles: []string{"intro-2021.mov"},
			wantName:      "intro-2021.mov",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, tt.baseName)
			if err := os.WriteFile(base, []byte{}, 0o644); err != nil {
				t.Fatalf("seed base: %v", err)
			}
			for _, f := range tt.existingFiles {
				if err := os.WriteFile(filepath.Join(dir, f), []byte{}, 0o644); err != nil {
					t.Fatalf("seed %s: %v", f, err)
				}
			}
			got := resolveIntroPath(base, tt.recordingDate)
			want := filepath.Join(dir, tt.wantName)
			if got != want {
				t.Errorf("resolveIntroPath(%q, %q) = %q, want %q", base, tt.recordingDate, got, want)
			}
		})
	}
}
