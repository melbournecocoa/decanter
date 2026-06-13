package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestMetadataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")

	in := model.TalkMetadata{
		Title:       "An Introduction to HealthKit (Part 1)",
		Speaker:     "Matt Delves",
		Description: "A beginner's overview.",
		Tags:        []string{"HealthKit", "iOS"},
		Chapters:    []model.Chapter{{Time: 167.988, Title: "Data Types"}},
		Trim:        &model.TrimRange{StartSeconds: 3.59, EndSeconds: 1057.59},
	}
	if err := WriteMetadata(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.Title != in.Title || out.Trim == nil || out.Trim.StartSeconds != 3.59 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}

	// omitempty contract: unset trim/skip must NOT appear in the JSON.
	in.Trim = nil
	if err := WriteMetadata(path, in); err != nil {
		t.Fatalf("write2: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "\"trim\"") || strings.Contains(string(raw), "\"skip\"") {
		t.Fatalf("omitempty violated: %s", raw)
	}
	// 2-space indent, matching the rest of the codebase.
	if !strings.Contains(string(raw), "\n  \"title\"") {
		t.Fatalf("expected 2-space indent, got: %s", raw)
	}
}
