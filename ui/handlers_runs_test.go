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
}

func (f *fakeReader) ListPipelineRuns(context.Context) ([]TemporalRun, error) { return f.runs, nil }
func (f *fakeReader) History(context.Context, string, string) ([]*historypb.HistoryEvent, error) {
	return f.events, nil
}
func (f *fakeReader) Status(context.Context, string) (string, error) { return f.status, nil }

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
