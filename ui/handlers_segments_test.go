package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

func TestSegmentMetadataGetPut(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	_ = os.MkdirAll(ProcessedDir(base, wf, 1), 0o755)
	_ = WriteMetadata(MetadataPath(base, wf, 1), model.TalkMetadata{Title: "Old", Speaker: "Y"})

	s := &Server{Base: base}

	// GET
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/segments/1/metadata", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get %d", rec.Code)
	}
	var got struct{ Metadata model.TalkMetadata }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Metadata.Title != "Old" {
		t.Fatalf("title = %q", got.Metadata.Title)
	}

	// PUT
	body, _ := json.Marshal(model.TalkMetadata{Title: "New", Speaker: "Y", Trim: &model.TrimRange{StartSeconds: 1, EndSeconds: 2}})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/runs/"+wf+"/segments/1/metadata", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put %d: %s", rec.Code, rec.Body.String())
	}
	reread, _ := ReadMetadata(MetadataPath(base, wf, 1))
	if reread.Title != "New" || reread.Trim == nil || reread.Trim.EndSeconds != 2 {
		t.Fatalf("not persisted: %+v", reread)
	}
}
