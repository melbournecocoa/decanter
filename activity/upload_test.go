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
