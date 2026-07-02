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

// fakeReader implements TemporalReader for handler tests.
type fakeReader struct {
	runs   []TemporalRun
	events []*historypb.HistoryEvent
	status string
	descs  map[string]*WorkflowDescription // keyed by workflow ID
}

func (f *fakeReader) ListPipelineRuns(context.Context) ([]TemporalRun, error) { return f.runs, nil }
func (f *fakeReader) History(context.Context, string, string) ([]*historypb.HistoryEvent, error) {
	return f.events, nil
}
func (f *fakeReader) Status(context.Context, string) (string, error) { return f.status, nil }
func (f *fakeReader) Describe(_ context.Context, wf string) (*WorkflowDescription, error) {
	if d, ok := f.descs[wf]; ok {
		return d, nil
	}
	return &WorkflowDescription{Status: f.status}, nil
}

func TestRunDetailHandler(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	for _, i := range []int{0, 1, 2} {
		p := SegmentVideoPath(base, wf, i)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	_ = os.MkdirAll(ProcessedDir(base, wf, 1), 0o755)
	_ = WriteMetadata(MetadataPath(base, wf, 1), model.TalkMetadata{Title: "Hi", Speaker: "X"})
	_ = os.WriteFile(EventPath(base, wf), []byte(`{"eventName":"No. 159"}`), 0o644)

	s := &Server{Base: base, Temporal: &fakeReader{status: "Running"}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["event"].(map[string]any)["eventName"] != "No. 159" {
		t.Fatalf("event missing: %v", body)
	}
	if len(body["segments"].([]any)) != 3 {
		t.Fatalf("want 3 segments: %v", body["segments"])
	}
}

// An in-flight run that hasn't reached Split (no segments/ dir) must still
// resolve to 200 with an empty segment list, not 404 "run not found".
func TestRunDetailHandler_EarlyStage(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-20260702-211524"
	// Download ran (workspace dir + event.json exist) but Split hasn't.
	if err := os.MkdirAll(WorkspacePath(base, wf), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(EventPath(base, wf), []byte(`{"eventName":"No. 160"}`), 0o644)

	s := &Server{Base: base, Temporal: &fakeReader{status: "Running"}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("early-stage run should be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if segs, ok := body["segments"].([]any); !ok || len(segs) != 0 {
		t.Fatalf("want empty segments, got %v", body["segments"])
	}
	if body["event"].(map[string]any)["eventName"] != "No. 160" {
		t.Fatalf("event missing: %v", body)
	}
}

// A reset (or terminate+restart) leaves the superseded run in visibility under
// the same WorkflowID, so ListWorkflow returns several rows for one ID in
// arbitrary order. currentRuns must collapse to the live run — the Running one
// — so a stale Terminated row can't clobber the pill.
func TestCurrentRuns(t *testing.T) {
	runs := []TemporalRun{
		{WorkflowID: "wf-a", RunID: "old", Status: "Terminated", StartTime: "2026-07-02T10:00:00Z"},
		{WorkflowID: "wf-a", RunID: "new", Status: "Running", StartTime: "2026-07-02T11:00:00Z"},
		{WorkflowID: "wf-b", RunID: "only", Status: "Completed", StartTime: "2026-07-01T09:00:00Z"},
	}
	got := currentRuns(runs)
	byID := map[string]TemporalRun{}
	for _, r := range got {
		byID[r.WorkflowID] = r
	}
	if len(got) != 2 {
		t.Fatalf("want 2 runs (one per workflow ID), got %d: %+v", len(got), got)
	}
	if byID["wf-a"].RunID != "new" || byID["wf-a"].Status != "Running" {
		t.Errorf("wf-a: want the Running run 'new', got %+v", byID["wf-a"])
	}
	if byID["wf-b"].RunID != "only" {
		t.Errorf("wf-b: want the sole run 'only', got %+v", byID["wf-b"])
	}
}

// With neither run Running (e.g. an old Failed run and a newer Terminated one),
// currentRuns keeps the most recently started.
func TestCurrentRuns_NoRunningPrefersLatest(t *testing.T) {
	runs := []TemporalRun{
		{WorkflowID: "wf-a", RunID: "newer", Status: "Terminated", StartTime: "2026-07-02T11:00:00Z"},
		{WorkflowID: "wf-a", RunID: "older", Status: "Failed", StartTime: "2026-07-02T10:00:00Z"},
	}
	got := currentRuns(runs)
	if len(got) != 1 || got[0].RunID != "newer" {
		t.Fatalf("want the later-started run 'newer', got %+v", got)
	}
}

// End-to-end through the list handler: a WorkflowID with a stale Terminated run
// and a live Running run must render as Running, not terminated.
func TestRunsHandler_SupersededRunIgnored(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	s := &Server{Base: base, Temporal: &fakeReader{
		status: "Running",
		// Running first, Terminated last: without dedup the last row wins and
		// clobbers the pill — the exact failure this guards against.
		runs: []TemporalRun{
			{WorkflowID: wf, RunID: "new", Status: "Running", StartTime: "2026-07-02T11:00:00Z"},
			{WorkflowID: wf, RunID: "old", Status: "Terminated", StartTime: "2026-07-02T10:00:00Z"},
		},
	}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var items []RunListItem
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 {
		t.Fatalf("want 1 run item, got %d: %+v", len(items), items)
	}
	if items[0].Status != "Running" || items[0].State == GateTerminated {
		t.Fatalf("want live Running run, got status=%q state=%q", items[0].Status, items[0].State)
	}
}

// A workflow ID with no workspace dir at all is genuinely not found → 404.
func TestRunDetailHandler_UnknownRun(t *testing.T) {
	base := t.TempDir()
	s := &Server{Base: base, Temporal: &fakeReader{status: "Running"}}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run should be 404, got %d", rec.Code)
	}
}
