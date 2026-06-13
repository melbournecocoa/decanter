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

func (s *Server) registerApprovalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/runs/{wf}/approve", s.handleApprove)
	mux.HandleFunc("GET /api/runs/{wf}/reset/{recipe}", s.handleResetPreview)
	mux.HandleFunc("POST /api/runs/{wf}/reset/{recipe}", s.handleResetExecute)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if s.Control == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "control unavailable"})
		return
	}
	var body struct {
		Gate     string `json:"gate"`
		Approved bool   `json:"approved"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Gate != "review" && body.Gate != "upload" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gate must be review|upload"})
		return
	}
	if err := s.Control.Signal(r.Context(), r.PathValue("wf"), body.Gate, body.Approved); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signalled"})
}

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

var errBadRecipe = &httpError{code: http.StatusNotFound, msg: "unknown reset recipe"}

// resetTarget resolves a recipe to its target WorkflowTaskStarted event id.
func (s *Server) resetTarget(r *http.Request, wf, recipeKey string) (ResetRecipe, int64, error) {
	recipe, ok := ResetRecipes[recipeKey]
	if !ok {
		return ResetRecipe{}, 0, errBadRecipe
	}
	events, err := s.Temporal.History(r.Context(), wf, "")
	if err != nil {
		return recipe, 0, err
	}
	id, err := findResetEventID(events, recipe.AnchorActivity)
	return recipe, id, err
}

func (s *Server) handleResetPreview(w http.ResponseWriter, r *http.Request) {
	if s.Temporal == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal unavailable"})
		return
	}
	recipe, id, err := s.resetTarget(r, r.PathValue("wf"), r.PathValue("recipe"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recipe":        recipe.Key,
		"label":         recipe.Label,
		"explanation":   recipe.Explanation,
		"targetEventId": id,
	})
}

func (s *Server) handleResetExecute(w http.ResponseWriter, r *http.Request) {
	if s.Temporal == nil || s.Control == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal/control unavailable"})
		return
	}
	wf := r.PathValue("wf")
	recipe, id, err := s.resetTarget(r, wf, r.PathValue("recipe"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	reason := "decanter-ui: " + recipe.Key
	if err := s.Control.Reset(r.Context(), wf, id, reason); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "reset", "targetEventId": id})
}
