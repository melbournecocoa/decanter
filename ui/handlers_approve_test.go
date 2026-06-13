package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	historypb "go.temporal.io/api/history/v1"
)

type fakeController struct {
	signalGate   string
	signalOK     bool
	resetEventID int64
	resetCalled  bool
}

func (f *fakeController) Signal(_ context.Context, wf, gate string, approved bool) error {
	f.signalGate = gate
	f.signalOK = approved
	return nil
}
func (f *fakeController) Reset(_ context.Context, wf string, eventID int64, reason string) error {
	f.resetCalled = true
	f.resetEventID = eventID
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
