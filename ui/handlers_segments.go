package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/melbournecocoa/decanter/model"
)

func (s *Server) registerSegmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs/{wf}/segments/{idx}/metadata", s.handleGetMetadata)
	mux.HandleFunc("PUT /api/runs/{wf}/segments/{idx}/metadata", s.handlePutMetadata)
	s.registerMediaRoutes(mux)
}

func segIndex(r *http.Request) (int, error) { return strconv.Atoi(r.PathValue("idx")) }

func (s *Server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	wf := r.PathValue("wf")
	idx, err := segIndex(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad index"})
		return
	}
	m, err := ReadMetadata(MetadataPath(s.Base, wf, idx))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	reasoning, _ := os.ReadFile(ReasoningPath(s.Base, wf, idx))
	writeJSON(w, http.StatusOK, map[string]any{
		"metadata":  m,
		"reasoning": string(reasoning),
	})
}

func (s *Server) handlePutMetadata(w http.ResponseWriter, r *http.Request) {
	wf := r.PathValue("wf")
	idx, err := segIndex(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad index"})
		return
	}
	var m model.TalkMetadata
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
		return
	}
	if err := WriteMetadata(MetadataPath(s.Base, wf, idx), m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// registerMediaRoutes is a temporary stub — Task 13 replaces it (in video.go)
// with the real video/subtitles/thumbnail routes and DELETES this stub.
func (s *Server) registerMediaRoutes(mux *http.ServeMux) {}
