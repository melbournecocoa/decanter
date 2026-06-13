package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
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
	events := []*historypb.HistoryEvent{
		{EventId: 4, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED},
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

	// reset execute (POST) → runs reset to id 4
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/runs/"+wf+"/reset/redo-split", nil))
	if rec.Code != http.StatusOK || !fc.resetCalled || fc.resetEventID != 4 {
		t.Fatalf("execute: %d called=%v id=%d", rec.Code, fc.resetCalled, fc.resetEventID)
	}
}
