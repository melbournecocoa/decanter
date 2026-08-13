package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	historypb "go.temporal.io/api/history/v1"

	"github.com/melbournecocoa/decanter/model"
)

type fakeController struct {
	signalGate   string
	signalOK     bool
	resetEventID int64
	resetCalled  bool
	terminated   []string // child workflow IDs, in call order
	calls        []string // "terminate:<wf>" / "reset", in call order
	terminateErr error
}

func (f *fakeController) Signal(_ context.Context, wf, gate string, approved bool) error {
	f.signalGate = gate
	f.signalOK = approved
	return nil
}
func (f *fakeController) Reset(_ context.Context, wf string, eventID int64, reason string) error {
	f.resetCalled = true
	f.resetEventID = eventID
	f.calls = append(f.calls, "reset")
	return nil
}
func (f *fakeController) Terminate(_ context.Context, wf, runID, reason string) error {
	if f.terminateErr != nil {
		return f.terminateErr
	}
	f.terminated = append(f.terminated, wf)
	f.calls = append(f.calls, "terminate:"+wf)
	return nil
}

func TestApproveAndReset(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	_ = ensureDir(WorkspacePath(base, wf))

	fc := &fakeController{}
	// Review-gate run: children initiated AND all closed, DetectBumpers scheduled
	// with a preceding WorkflowTaskStarted, no review signal.
	events := []*historypb.HistoryEvent{
		childInitiated(2),
		childCompleted(3),
		wfTaskStarted(4),
		schedActivity(6, "DetectBumpers"),
	}
	s := &Server{Base: base, Control: fc, Temporal: &fakeReader{events: events, status: "Running"}}

	// approve review
	body, _ := json.Marshal(map[string]any{"gate": "review", "approved": true})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/approve", bytes.NewReader(body)))
	if rec.Code != http.StatusOK || fc.signalGate != "review" || !fc.signalOK {
		t.Fatalf("approve: %d gate=%q ok=%v", rec.Code, fc.signalGate, fc.signalOK)
	}

	// reset preview (GET) → returns target event id, no execution
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/reset/redo-split", nil))
	var prev map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &prev)
	if rec.Code != http.StatusOK || prev["targetEventId"].(float64) != 4 {
		t.Fatalf("preview: %d %v", rec.Code, prev)
	}
	if fc.resetCalled {
		t.Fatal("preview must not execute reset")
	}

	// reset execute (POST) → runs reset to id 4 (must echo the previewed targetEventId)
	body2, _ := json.Marshal(map[string]any{"targetEventId": 4})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/reset/redo-split", bytes.NewReader(body2)))
	if rec.Code != http.StatusOK || !fc.resetCalled || fc.resetEventID != 4 {
		t.Fatalf("execute: %d called=%v id=%d", rec.Code, fc.resetCalled, fc.resetEventID)
	}
}

// The redo-split preview must report what DetectBumpers will actually read off
// disk. Unsaved edits in the bumpers panel are invisible to the worker, so a
// preview that stays silent lets a reviewer reset into the same failure.
func TestResetPreviewReportsBumpersOnDisk(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	_ = ensureDir(WorkspacePath(base, wf))

	events := []*historypb.HistoryEvent{
		wfTaskStarted(4),
		schedActivity(6, "DetectBumpers"),
		wfTaskStarted(8),
		schedActivity(10, "Assemble"),
	}
	s := &Server{Base: base, Control: &fakeController{}, Temporal: &fakeReader{events: events, status: "Running"}}

	preview := func(recipe string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/reset/"+recipe, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s preview: %d %s", recipe, rec.Code, rec.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s preview: %v", recipe, err)
		}
		return out
	}

	// No bumpers.json yet: count 0 is the signal that detection will re-run and
	// fail again exactly as before.
	got := preview("redo-split")
	if c, ok := got["bumperCount"].(float64); !ok || c != 0 {
		t.Fatalf("missing bumpers → want bumperCount 0, got %v", got["bumperCount"])
	}

	// Saved sidecar: report the count and the boundaries themselves so the
	// reviewer can confirm the times are the ones they entered.
	if err := WriteBumpers(BumpersPath(base, wf), []model.BumperRegion{
		{VisualStart: 6330, VisualEnd: 6330},
		{VisualStart: 3065, VisualEnd: 3065},
	}); err != nil {
		t.Fatalf("write bumpers: %v", err)
	}
	got = preview("redo-split")
	if c := got["bumperCount"].(float64); c != 2 {
		t.Fatalf("want bumperCount 2, got %v", c)
	}
	list, _ := got["bumpers"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 bumpers echoed, got %v", got["bumpers"])
	}
	// WriteBumpers sorts, so the preview reflects worker order, not entry order.
	if first := list[0].(map[string]any)["visual_start"].(float64); first != 3065 {
		t.Fatalf("want boundaries sorted ascending, got %v", list)
	}

	// redo-assemble does not consume bumpers.json — stay silent rather than
	// implying the reset depends on it.
	if got := preview("redo-assemble"); got["bumperCount"] != nil {
		t.Fatalf("redo-assemble must not report bumpers, got %v", got["bumperCount"])
	}
}

// A reset re-runs the fan-out, but Temporal does NOT close the base run's
// children on reset — an in-flight SegmentWorkflow survives the reset and the
// new run's StartChildWorkflowExecution dies with "child workflow execution
// already started", failing the whole pipeline. The reset must therefore
// terminate open children FIRST, and must not fire at all if that fails.
func TestResetTerminatesInFlightChildrenBeforeResetting(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	_ = ensureDir(WorkspacePath(base, wf))

	events := []*historypb.HistoryEvent{
		childInitiated(2), wfTaskStarted(4), schedActivity(6, "DetectBumpers"),
	}
	desc := &WorkflowDescription{
		Status: "Running",
		Children: []PendingChild{
			{WorkflowID: wf + "-segment-1", RunID: "run-1"},
			{WorkflowID: wf + "-segment-2", RunID: "run-2"},
		},
	}
	newServer := func(fc *fakeController) *Server {
		return &Server{Base: base, Control: fc, Temporal: &fakeReader{
			events: events, status: "Running", descs: map[string]*WorkflowDescription{wf: desc},
		}}
	}

	// Preview names the children that will be killed, so the confirm dialog can
	// say so before the reviewer commits.
	rec := httptest.NewRecorder()
	newServer(&fakeController{}).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/reset/redo-split", nil))
	var prev map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &prev)
	kids, _ := prev["pendingChildren"].([]any)
	if rec.Code != http.StatusOK || len(kids) != 2 || kids[0] != wf+"-segment-1" {
		t.Fatalf("preview children: %d %v", rec.Code, prev["pendingChildren"])
	}

	// Execute: both children terminated, and every terminate lands before reset.
	fc := &fakeController{}
	body, _ := json.Marshal(map[string]any{"targetEventId": 4})
	rec = httptest.NewRecorder()
	newServer(fc).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/reset/redo-split", bytes.NewReader(body)))
	if rec.Code != http.StatusOK || len(fc.terminated) != 2 {
		t.Fatalf("execute: %d terminated=%v body=%s", rec.Code, fc.terminated, rec.Body)
	}
	want := []string{"terminate:" + wf + "-segment-1", "terminate:" + wf + "-segment-2", "reset"}
	if strings.Join(fc.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("call order: got %v want %v", fc.calls, want)
	}

	// A failed terminate must abort: resetting anyway recreates the exact
	// collision this guard exists to prevent.
	fc = &fakeController{terminateErr: errors.New("boom")}
	rec = httptest.NewRecorder()
	newServer(fc).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/reset/redo-split", bytes.NewReader(body)))
	if rec.Code != http.StatusBadGateway || fc.resetCalled {
		t.Fatalf("terminate failure: code=%d resetCalled=%v", rec.Code, fc.resetCalled)
	}
}

func TestApproveWrongGateAndStaleReset(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	_ = ensureDir(WorkspacePath(base, wf))
	fc := &fakeController{}
	// Review-gate run (same fixture as above).
	events := []*historypb.HistoryEvent{
		childInitiated(2), childCompleted(3), wfTaskStarted(4), schedActivity(6, "DetectBumpers"),
	}
	s := &Server{Base: base, Control: fc, Temporal: &fakeReader{events: events, status: "Running"}}

	// Approving the UPLOAD gate while parked at REVIEW → 409, no signal sent.
	body, _ := json.Marshal(map[string]any{"gate": "upload", "approved": true})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/approve", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict || fc.signalGate != "" {
		t.Fatalf("wrong-gate approve: code=%d signalGate=%q", rec.Code, fc.signalGate)
	}

	// Reset with a stale targetEventId (99 != real 4) → 409, no reset executed.
	body2, _ := json.Marshal(map[string]any{"targetEventId": 99})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/reset/redo-split", bytes.NewReader(body2)))
	if rec.Code != http.StatusConflict || fc.resetCalled {
		t.Fatalf("stale reset: code=%d resetCalled=%v", rec.Code, fc.resetCalled)
	}
}
