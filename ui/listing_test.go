package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestSegmentType(t *testing.T) {
	if segmentType(0, 4) != model.SegmentTypeWelcome {
		t.Fatal("idx 0 should be welcome")
	}
	if segmentType(3, 4) != model.SegmentTypeWrapUp {
		t.Fatal("last should be wrapup")
	}
	if segmentType(1, 4) != model.SegmentTypeTalk {
		t.Fatal("middle should be talk")
	}
}

func TestListRunSegments(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-20260610-225332"
	// segments 00..03 exist on disk; only talks (01,02) have processed dirs.
	for _, i := range []int{0, 1, 2, 3} {
		p := SegmentVideoPath(base, wf, i)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	_ = os.MkdirAll(ProcessedDir(base, wf, 2), 0o755)
	_ = WriteMetadata(MetadataPath(base, wf, 2), model.TalkMetadata{Title: "T", Skip: true})

	segs, err := ListRunSegments(base, wf)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(segs) != 4 {
		t.Fatalf("want 4 segments, got %d", len(segs))
	}
	if segs[0].Type != model.SegmentTypeWelcome || segs[3].Type != model.SegmentTypeWrapUp {
		t.Fatalf("type classification wrong: %+v", segs)
	}
	if !segs[2].HasMetadata || !segs[2].Skip {
		t.Fatalf("seg 2 should have metadata + skip: %+v", segs[2])
	}
	if segs[1].HasMetadata {
		t.Fatalf("seg 1 should not have metadata yet")
	}
}

// A run that hasn't reached Split has no segments/ dir yet — a valid empty
// state, not an error (Split creates the dir). Regression: ListRunSegments used
// to propagate the ReadDir ENOENT, which handleRunDetail then 404'd as
// "run not found" for legitimately in-flight, pre-Split runs.
func TestListRunSegments_NoSegmentsDir(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-early"
	// Run dir exists (Download ran) but Split hasn't created segments/ yet.
	if err := os.MkdirAll(WorkspacePath(base, wf), 0o755); err != nil {
		t.Fatal(err)
	}
	segs, err := ListRunSegments(base, wf)
	if err != nil {
		t.Fatalf("pre-Split run should list zero segments, got error: %v", err)
	}
	if segs == nil {
		t.Fatal("want empty non-nil slice so it marshals to [] not null")
	}
	if len(segs) != 0 {
		t.Fatalf("want 0 segments, got %d", len(segs))
	}
}

func TestListRunSegments_HasFinal(t *testing.T) {
	base := t.TempDir()
	wf := "wf1"
	for _, i := range []int{0, 1} {
		p := SegmentVideoPath(base, wf, i)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	// only segment 1 has a final.mp4
	fp := FinalVideoPath(base, wf, 1)
	_ = os.MkdirAll(filepath.Dir(fp), 0o755)
	_ = os.WriteFile(fp, []byte("v"), 0o644)

	segs, err := ListRunSegments(base, wf)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]bool{}
	for _, s := range segs {
		got[s.Index] = s.HasFinal
	}
	if got[0] || !got[1] {
		t.Fatalf("hasFinal = %v, want {0:false,1:true}", got)
	}
}
