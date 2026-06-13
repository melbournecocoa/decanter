package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestBumpersRoundTripSortedSnakeCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bumpers.json")

	// Deliberately out of order — write must sort ascending by VisualStart.
	in := []model.BumperRegion{
		{VisualStart: 200, VisualEnd: 215},
		{VisualStart: 50, VisualEnd: 95},
		{VisualStart: 120, VisualEnd: 120}, // zero-width manual boundary
	}
	if err := WriteBumpers(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "\"visual_start\"") {
		t.Fatalf("expected snake_case keys, got: %s", raw)
	}
	out, err := ReadBumpers(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 3 || out[0].VisualStart != 50 || out[1].VisualStart != 120 || out[2].VisualStart != 200 {
		t.Fatalf("expected sorted ascending, got: %+v", out)
	}
}

func TestSourceTimeForBoundary(t *testing.T) {
	seg := model.Segment{Start: 894.83, StartOffset: 0.726}
	// roughStartInSource = 894.83 - 0.726 = 894.104; + 12.0 file seconds = 906.104
	got := SourceTimeForBoundary(seg, 12.0)
	if got < 906.103 || got > 906.105 {
		t.Fatalf("SourceTimeForBoundary = %f, want ~906.104", got)
	}
}
