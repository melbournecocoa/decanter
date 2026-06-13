package ui

import (
	"fmt"
	"path/filepath"
)

// WorkspacePath returns the per-run workspace directory. The run's workflow ID
// is its directory name (see activity/workspace.go).
func WorkspacePath(base, wf string) string { return filepath.Join(base, wf) }

// segName is the zero-padded segment directory/file stem, e.g. "segment-02".
func segName(idx int) string { return fmt.Sprintf("segment-%02d", idx) }

// SegmentVideoPath is the rough-cut segment file (exists for every segment,
// including welcome/wrap-up).
func SegmentVideoPath(base, wf string, idx int) string {
	return filepath.Join(WorkspacePath(base, wf), "segments", segName(idx)+".mp4")
}

// ProcessedDir holds a talk segment's metadata/final/thumbnail artefacts.
func ProcessedDir(base, wf string, idx int) string {
	return filepath.Join(WorkspacePath(base, wf), "processed", segName(idx))
}

func MetadataPath(base, wf string, idx int) string   { return filepath.Join(ProcessedDir(base, wf, idx), "metadata.json") }
func ReasoningPath(base, wf string, idx int) string  { return filepath.Join(ProcessedDir(base, wf, idx), "metadata_reasoning.md") }
func FinalVideoPath(base, wf string, idx int) string { return filepath.Join(ProcessedDir(base, wf, idx), "final.mp4") }
func FinalSRTPath(base, wf string, idx int) string   { return filepath.Join(ProcessedDir(base, wf, idx), "final.srt") }
func ThumbnailPath(base, wf string, idx int) string  { return filepath.Join(ProcessedDir(base, wf, idx), "thumbnail.jpg") }

func EventPath(base, wf string) string   { return filepath.Join(WorkspacePath(base, wf), "event.json") }
func BumpersPath(base, wf string) string { return filepath.Join(WorkspacePath(base, wf), "bumpers.json") }
