package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestBumpersGetPut(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	_ = ensureDir(WorkspacePath(base, wf))

	s := &Server{Base: base}
	body, _ := json.Marshal([]model.BumperRegion{{VisualStart: 200, VisualEnd: 215}, {VisualStart: 50, VisualEnd: 95}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/runs/"+wf+"/bumpers", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := ReadBumpers(BumpersPath(base, wf))
	if len(got) != 2 || got[0].VisualStart != 50 {
		t.Fatalf("not sorted/persisted: %+v", got)
	}
}
