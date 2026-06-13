package ui

import (
	"net/http"

	"github.com/melbournecocoa/decanter/model"
)

func (s *Server) registerControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs/{wf}/bumpers", s.handleGetBumpers)
	mux.HandleFunc("PUT /api/runs/{wf}/bumpers", s.handlePutBumpers)
	mux.HandleFunc("GET /api/runs/{wf}/segment-timing", s.handleSegmentTiming)
	s.registerApprovalRoutes(mux)
}

func (s *Server) handleGetBumpers(w http.ResponseWriter, r *http.Request) {
	b, err := ReadBumpers(BumpersPath(s.Base, r.PathValue("wf")))
	if err != nil {
		b = []model.BumperRegion{}
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handlePutBumpers(w http.ResponseWriter, r *http.Request) {
	var b []model.BumperRegion
	if err := decodeJSON(r, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := WriteBumpers(BumpersPath(s.Base, r.PathValue("wf")), b); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleSegmentTiming returns Segment timing decoded from the Split payload in
// history, so the frontend can convert a segment-file playhead to source time
// for a new bumper boundary.
func (s *Server) handleSegmentTiming(w http.ResponseWriter, r *http.Request) {
	if s.Temporal == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal unavailable"})
		return
	}
	events, err := s.Temporal.History(r.Context(), r.PathValue("wf"), "")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	segs, err := SegmentsFromHistory(events)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, segs)
}

// registerApprovalRoutes is a temporary stub — Task 15 replaces it (appended to
// this same file) with the real approve/reset routes and DELETES this stub.
func (s *Server) registerApprovalRoutes(mux *http.ServeMux) {}
