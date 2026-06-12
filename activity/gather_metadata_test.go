package activity

import (
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestApplyTrimFallback_FillsWhenModelOmitsTrim(t *testing.T) {
	seg := model.Segment{Start: 100, End: 400, StartOffset: 2}
	m := model.TalkMetadata{Title: "x"}

	changed := applyTrimFallback(&m, seg)

	if !changed {
		t.Fatal("expected changed=true when Trim is nil")
	}
	if m.Trim == nil {
		t.Fatal("expected Trim to be filled")
	}
	if m.Trim.StartSeconds != 2 {
		t.Errorf("StartSeconds = %v, want 2 (StartOffset)", m.Trim.StartSeconds)
	}
	if m.Trim.EndSeconds != 302 { // StartOffset + (End-Start) = 2 + 300
		t.Errorf("EndSeconds = %v, want 302", m.Trim.EndSeconds)
	}
}

func TestApplyTrimFallback_PreservesModelProposal(t *testing.T) {
	seg := model.Segment{Start: 100, End: 400, StartOffset: 2}
	m := model.TalkMetadata{Trim: &model.TrimRange{StartSeconds: 28, EndSeconds: 350}}

	changed := applyTrimFallback(&m, seg)

	if changed {
		t.Fatal("expected changed=false when Trim already set; the model proposal must not be clobbered")
	}
	if m.Trim.StartSeconds != 28 || m.Trim.EndSeconds != 350 {
		t.Errorf("Trim mutated to %+v, want {28 350}", *m.Trim)
	}
}
