package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	historypb "go.temporal.io/api/history/v1"

	"github.com/melbournecocoa/decanter/model"
)

func TestStatusHandler_Assembling(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	// 3 segment files so index 1 classifies as a talk (0=welcome, 2=wrapup).
	for _, i := range []int{0, 1, 2} {
		p := SegmentVideoPath(base, wf, i)
		_ = os.MkdirAll(filepathDir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}

	// History: Split output gives seg 1 duration 124s; Assemble scheduled for seg 1.
	split := model.SplitOutput{Segments: []model.Segment{
		{Index: 0, Start: 0, End: 10},
		{Index: 1, Start: 10, End: 134}, // End-Start = 124 (the talk)
		{Index: 2, Start: 134, End: 140},
	}}
	events := []*historypb.HistoryEvent{
		schedActivity(4, "Split"),
		completedSplit(5, 4, split),
		signalEvent(6, "review_approval"),
		schedActivityFull(8, "Assemble", "act-8", model.AssembleInput{Segment: model.Segment{Index: 1}}),
	}
	desc := &WorkflowDescription{
		Status:  "Running",
		Pending: []ActivityProgress{{Name: "Assemble", ActivityID: "act-8", Heartbeat: int64(72)}},
	}
	s := &Server{Base: base, Temporal: &fakeReader{
		status: "Running", events: events, descs: map[string]*WorkflowDescription{wf: desc},
	}}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var rs RunStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &rs)
	if rs.Phase != "assembling" {
		t.Fatalf("phase = %q, want assembling", rs.Phase)
	}
	var seg1 *SegmentStatus
	for i := range rs.Segments {
		if rs.Segments[i].Index == 1 {
			seg1 = &rs.Segments[i]
		}
	}
	if seg1 == nil || seg1.Percent == nil || *seg1.Percent != 58 {
		t.Fatalf("seg1 percent = %+v, want 58 (72/124)", seg1)
	}
}

func TestStatusHandler_Processing(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-proc"
	// 3 segment files so index 1 classifies as a talk (0=welcome, 2=wrapup).
	for _, i := range []int{0, 1, 2} {
		p := SegmentVideoPath(base, wf, i)
		_ = os.MkdirAll(filepathDir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}

	// History: one child initiated but not yet closed → classifyState returns GateRunning.
	events := []*historypb.HistoryEvent{
		childInitiated(1),
	}

	// Parent: Running, no pending parent activities, one child → derivePhase returns "processing".
	parentDesc := &WorkflowDescription{
		Status:   "Running",
		Pending:  []ActivityProgress{},
		Children: []PendingChild{{WorkflowID: wf + "-segment-1"}},
	}
	// Child: Running, pending Transcribe activity.
	childDesc := &WorkflowDescription{
		Status:  "Running",
		Pending: []ActivityProgress{{Name: "Transcribe"}},
	}

	s := &Server{Base: base, Temporal: &fakeReader{
		status: "Running",
		events: events,
		descs: map[string]*WorkflowDescription{
			wf:                   parentDesc,
			wf + "-segment-1":    childDesc,
		},
	}}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var rs RunStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &rs)
	if rs.Phase != "processing" {
		t.Fatalf("phase = %q, want processing", rs.Phase)
	}
	var seg1 *SegmentStatus
	for i := range rs.Segments {
		if rs.Segments[i].Index == 1 {
			seg1 = &rs.Segments[i]
		}
	}
	if seg1 == nil {
		t.Fatalf("segment with index 1 not found in response")
	}
	if seg1.Phase != "transcribe" {
		t.Fatalf("seg1.Phase = %q, want transcribe", seg1.Phase)
	}
	if seg1.Step == nil {
		t.Fatalf("seg1.Step is nil, want Step{Current:2, Total:4}")
	}
	if seg1.Step.Current != 2 || seg1.Step.Total != 4 {
		t.Fatalf("seg1.Step = %+v, want {Current:2, Total:4}", seg1.Step)
	}
}

func TestStatusHandler_TemporalError_Returns200Unknown(t *testing.T) {
	s := &Server{Base: t.TempDir(), Temporal: &erroringReader{}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/x/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var rs RunStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &rs)
	if rs.State != GateUnknown {
		t.Fatalf("state = %q, want unknown", rs.State)
	}
}

func filepathDir(p string) string { return filepath.Dir(p) }

// completedSplit builds an ActivityTaskCompleted carrying a SplitOutput result.
func completedSplit(id, schedID int64, out model.SplitOutput) *historypb.HistoryEvent {
	p, _ := converterToPayloads(out)
	e := completedActivity(id)
	e.GetActivityTaskCompletedEventAttributes().ScheduledEventId = schedID
	e.GetActivityTaskCompletedEventAttributes().Result = p
	return e
}

type erroringReader struct{ fakeReader }

func (e *erroringReader) Status(context.Context, string) (string, error) {
	return "", errStub
}
func (e *erroringReader) Describe(context.Context, string) (*WorkflowDescription, error) {
	return nil, errStub
}

var errStub = &stubErr{}

type stubErr struct{}

func (*stubErr) Error() string { return "temporal down" }
