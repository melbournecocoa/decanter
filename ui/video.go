package ui

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
)

func (s *Server) registerMediaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs/{wf}/segments/{idx}/video", s.handleSegmentVideo)
	mux.HandleFunc("GET /api/runs/{wf}/segments/{idx}/final", s.handleFinalVideo)
	mux.HandleFunc("GET /api/runs/{wf}/segments/{idx}/subtitles", s.handleSubtitles)
	mux.HandleFunc("GET /api/runs/{wf}/segments/{idx}/thumbnail", s.handleGetThumbnail)
	mux.HandleFunc("POST /api/runs/{wf}/segments/{idx}/thumbnail", s.handlePostThumbnail)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not available yet", http.StatusNotFound)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func (s *Server) handleSegmentVideo(w http.ResponseWriter, r *http.Request) {
	idx, _ := segIndex(r)
	s.serveFile(w, r, SegmentVideoPath(s.Base, r.PathValue("wf"), idx))
}

func (s *Server) handleFinalVideo(w http.ResponseWriter, r *http.Request) {
	idx, _ := segIndex(r)
	s.serveFile(w, r, FinalVideoPath(s.Base, r.PathValue("wf"), idx))
}

func (s *Server) handleSubtitles(w http.ResponseWriter, r *http.Request) {
	idx, _ := segIndex(r)
	raw, err := os.ReadFile(FinalSRTPath(s.Base, r.PathValue("wf"), idx))
	if err != nil {
		http.Error(w, "no subtitles", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Write(SRTToVTT(raw))
}

func (s *Server) handleGetThumbnail(w http.ResponseWriter, r *http.Request) {
	idx, _ := segIndex(r)
	s.serveFile(w, r, ThumbnailPath(s.Base, r.PathValue("wf"), idx))
}

// handlePostThumbnail re-extracts thumbnail.jpg from final.mp4 at {seconds}.
func (s *Server) handlePostThumbnail(w http.ResponseWriter, r *http.Request) {
	wf := r.PathValue("wf")
	idx, _ := segIndex(r)
	var body struct {
		Seconds float64 `json:"seconds"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	final := FinalVideoPath(s.Base, wf, idx)
	thumb := ThumbnailPath(s.Base, wf, idx)
	cmd := exec.CommandContext(r.Context(), s.FFmpegPath,
		"-y", "-ss", strconv.FormatFloat(body.Seconds, 'f', 3, 64),
		"-i", final, "-frames:v", "1", "-q:v", "2", thumb)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("ffmpeg: %v: %s", err, out)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "thumbnail updated"})
}
