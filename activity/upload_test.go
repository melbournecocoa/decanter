package activity

import (
	"testing"
	"time"
)

func TestResolvePlaylistYear(t *testing.T) {
	currentYear := time.Now().Year()

	tests := []struct {
		name          string
		recordingDate string
		want          int
	}{
		{
			name:          "valid Melbourne-local date",
			recordingDate: "2021-06-10T18:30:00+10:00",
			want:          2021,
		},
		{
			name:          "valid UTC date",
			recordingDate: "2021-06-10T08:30:00Z",
			want:          2021,
		},
		{
			name:          "empty falls back to current year",
			recordingDate: "",
			want:          currentYear,
		},
		{
			name:          "malformed falls back to current year",
			recordingDate: "not-a-date",
			want:          currentYear,
		},
		{
			name:          "missing-timezone date is not RFC3339, falls back",
			recordingDate: "2021-06-10",
			want:          currentYear,
		},
		{
			name:          "TZ edge: late-night UTC rolls into next Melbourne year",
			recordingDate: "2025-12-31T23:30:00Z",
			want:          2026,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePlaylistYear(tt.recordingDate)
			if got != tt.want {
				t.Errorf("resolvePlaylistYear(%q) = %d, want %d", tt.recordingDate, got, tt.want)
			}
		})
	}
}

func TestAppendRecordingYear(t *testing.T) {
	// Fix "now" so the test is deterministic regardless of when it runs.
	// Noon UTC on 2026-06-25 is unambiguously 2026 in Melbourne.
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		title         string
		recordingDate string
		want          string
	}{
		{
			name:          "recording year differs from current year → appends",
			title:         "Rob Amos - Custom Publishers with Combine",
			recordingDate: "2021-04-08T00:00:00Z",
			want:          "Rob Amos - Custom Publishers with Combine (2021)",
		},
		{
			name:          "recording year equals current year → no suffix",
			title:         "Rob Amos - Forging a Sword Spirit",
			recordingDate: "2026-05-21T00:00:00Z",
			want:          "Rob Amos - Forging a Sword Spirit",
		},
		{
			name:          "empty recording date → no suffix",
			title:         "Rob Amos - Foo",
			recordingDate: "",
			want:          "Rob Amos - Foo",
		},
		{
			name:          "malformed recording date → no suffix",
			title:         "Rob Amos - Foo",
			recordingDate: "not-a-date",
			want:          "Rob Amos - Foo",
		},
		{
			name:          "already stamped → idempotent, no double suffix",
			title:         "Rob Amos - Foo (2021)",
			recordingDate: "2021-04-08T00:00:00Z",
			want:          "Rob Amos - Foo (2021)",
		},
		{
			// 23:30 UTC on 31 Dec 2025 is already 2026 in Melbourne (AEDT, +11),
			// matching "now" — proves both sides are compared in the same zone.
			name:          "TZ consistency: late-night UTC that is the current Melbourne year → no suffix",
			title:         "Rob Amos - Foo",
			recordingDate: "2025-12-31T23:30:00Z",
			want:          "Rob Amos - Foo",
		},
		{
			name:          "speaker-less title still gets the year",
			title:         "Lightning Talks",
			recordingDate: "2022-09-01T00:00:00Z",
			want:          "Lightning Talks (2022)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendRecordingYear(tt.title, tt.recordingDate, now)
			if got != tt.want {
				t.Errorf("appendRecordingYear(%q, %q) = %q, want %q", tt.title, tt.recordingDate, got, tt.want)
			}
		})
	}
}
