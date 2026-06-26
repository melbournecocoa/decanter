package ui

import (
	"testing"

	historypb "go.temporal.io/api/history/v1"

	"github.com/melbournecocoa/decanter/model"
)

func TestChildStep(t *testing.T) {
	cases := map[string]struct {
		step  int
		label string
	}{
		"Classify":        {1, "classify"},
		"Transcribe":      {2, "transcribe"},
		"CleanTranscript": {3, "clean"},
		"GatherMetadata":  {4, "metadata"},
	}
	for name, want := range cases {
		s, label := childStep(name)
		if s == nil || s.Current != want.step || s.Total != 4 || label != want.label {
			t.Fatalf("%s -> %+v %q, want {%d 4} %q", name, s, label, want.step, want.label)
		}
	}
	if s, label := childStep("Assemble"); s != nil || label != "" {
		t.Fatalf("non-child returned %+v %q", s, label)
	}
}

func TestPercentOf(t *testing.T) {
	if p := percentOf(72, 124); p == nil || *p != 58 {
		t.Fatalf("72/124 -> %v, want 58", p)
	}
	if p := percentOf(200, 100); p == nil || *p != 100 {
		t.Fatalf("over -> %v, want clamp 100", p)
	}
	if p := percentOf(50, 0); p != nil {
		t.Fatalf("zero den -> %v, want nil", p)
	}
	if p := percentOf(-10, 100); p == nil || *p != 0 {
		t.Fatalf("percentOf(-10,100) = %v, want 0", p)
	}
}

func TestFormatters(t *testing.T) {
	if got := fmtClock(72); got != "1:12" {
		t.Fatalf("fmtClock(72) = %q, want 1:12", got)
	}
	if got := fmtMB(4_100_000); got != "4.1 MB" {
		t.Fatalf("fmtMB = %q, want 4.1 MB", got)
	}
	if got := fmtClock(-5); got != "0:00" {
		t.Fatalf("fmtClock(-5) = %q, want 0:00", got)
	}
}

func TestDerivePhase(t *testing.T) {
	if got := derivePhase(GateReview, nil, false); got != "review_gate" {
		t.Fatalf("review -> %q", got)
	}
	asm := []ActivityProgress{{Name: "Assemble", ActivityID: "5"}}
	if got := derivePhase(GateRunning, asm, false); got != "assembling" {
		t.Fatalf("assemble pending -> %q, want assembling", got)
	}
	if got := derivePhase(GateRunning, nil, true); got != "processing" {
		t.Fatalf("open children -> %q, want processing", got)
	}
	if got := derivePhase(GateRunning, nil, false); got != "running" {
		t.Fatalf("bare running -> %q, want running", got)
	}
}

func TestAssembleUploadIndex(t *testing.T) {
	events := []*historypb.HistoryEvent{
		schedActivityFull(5, "Assemble", "act-5", model.AssembleInput{Segment: model.Segment{Index: 3}}),
		schedActivityFull(6, "Upload", "act-6", model.UploadInput{Video: model.AssembledVideo{SegmentIndex: 7}}),
		schedActivity(7, "DetectBumpers"), // ignored
	}
	m := assembleUploadIndex(events)
	if m["act-5"] != 3 {
		t.Fatalf("assemble act-5 -> %d, want 3", m["act-5"])
	}
	if m["act-6"] != 7 {
		t.Fatalf("upload act-6 -> %d, want 7", m["act-6"])
	}
	if _, ok := m["7"]; ok {
		t.Fatalf("DetectBumpers should not be mapped")
	}
}
