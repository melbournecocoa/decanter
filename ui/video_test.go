package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVideoRangeAndSubtitles(t *testing.T) {
	base := t.TempDir()
	wf := "decanter-yt-1"
	// fake segment file
	p := SegmentVideoPath(base, wf, 1)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("0123456789"), 0o644)
	// fake final.srt
	_ = os.MkdirAll(ProcessedDir(base, wf, 1), 0o755)
	_ = os.WriteFile(FinalSRTPath(base, wf, 1), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)

	s := &Server{Base: base, FFmpegPath: "ffmpeg"}

	// Range request → 206 Partial Content
	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/segments/1/video", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}

	// Subtitles → WebVTT
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/"+wf+"/segments/1/subtitles", nil))
	if rec.Code != http.StatusOK || rec.Body.String()[:6] != "WEBVTT" {
		t.Fatalf("subtitles: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/vtt; charset=utf-8" {
		t.Fatalf("vtt content-type: %q", rec.Header().Get("Content-Type"))
	}
}
