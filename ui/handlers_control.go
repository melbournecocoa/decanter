package ui

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

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

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

var errBadRecipe = &httpError{code: http.StatusNotFound, msg: "unknown reset recipe"}

// currentState fetches status + history and classifies the run. Requires Temporal.
func (s *Server) currentState(r *http.Request, wf string) (GateState, string, error) {
	status, err := s.Temporal.Status(r.Context(), wf)
	if err != nil {
		return GateUnknown, "", err
	}
	events, err := s.Temporal.History(r.Context(), wf, "")
	if err != nil {
		return GateUnknown, status, err
	}
	return classifyState(summarizeHistory(events), status), status, nil
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if s.Control == nil || s.Temporal == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "control/temporal unavailable"})
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
	wf := r.PathValue("wf")
	state, _, err := s.currentState(r, wf)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	wantGate := GateReview
	if body.Gate == "upload" {
		wantGate = GateUpload
	}
	if state != wantGate {
		writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("run not parked at %s gate (current state: %s)", body.Gate, state)})
		return
	}
	if err := s.Control.Signal(r.Context(), wf, body.Gate, body.Approved); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signalled"})
}

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

// writeResetError writes an HTTP error response, honouring httpError.code.
func writeResetError(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeJSON(w, he.code, map[string]string{"error": he.msg})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (s *Server) handleResetPreview(w http.ResponseWriter, r *http.Request) {
	if s.Temporal == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal unavailable"})
		return
	}
	wf := r.PathValue("wf")
	recipe, id, err := s.resetTarget(r, wf, r.PathValue("recipe"))
	if err != nil {
		writeResetError(w, err)
		return
	}
	reason := "decanter-ui: " + recipe.Key
	command := "temporal " + strings.Join(buildResetArgs(s.Addr, wf, id, reason), " ")
	out := map[string]any{
		"recipe":        recipe.Key,
		"label":         recipe.Label,
		"explanation":   recipe.Explanation,
		"targetEventId": id,
		"command":       command,
	}
	// Open children get terminated by the reset (see handleResetExecute) — name
	// them so the reviewer knows what work is about to be thrown away.
	if kids, err := s.pendingChildren(r, wf); err == nil {
		ids := make([]string, len(kids))
		for i, c := range kids {
			ids[i] = c.WorkflowID
		}
		out["pendingChildren"] = ids
	}
	// Echo the sidecar the anchor activity will read. A missing or unreadable
	// file reports zero rather than erroring — "detection will re-run" is the
	// useful answer here, not a failed preview.
	if recipe.UsesBumpers {
		bumpers, _ := ReadBumpers(BumpersPath(s.Base, wf))
		rows := make([]bumperJSON, len(bumpers))
		for i, b := range bumpers {
			rows[i] = bumperJSON{VisualStart: b.VisualStart, VisualEnd: b.VisualEnd}
		}
		out["bumperCount"] = len(rows)
		out["bumpers"] = rows
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleResetExecute(w http.ResponseWriter, r *http.Request) {
	if s.Temporal == nil || s.Control == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporal/control unavailable"})
		return
	}
	wf := r.PathValue("wf")
	var body struct {
		TargetEventID int64 `json:"targetEventId"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status, err := s.Temporal.Status(r.Context(), wf)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if status != "Running" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "run is not Running (current: " + status + ")"})
		return
	}
	recipe, id, err := s.resetTarget(r, wf, r.PathValue("recipe"))
	if err != nil {
		writeResetError(w, err)
		return
	}
	if body.TargetEventID != id {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "history moved since preview; re-open the confirm dialog", "targetEventId": id})
		return
	}
	reason := "decanter-ui: " + recipe.Key
	// Reset does NOT close the base run's children: an in-flight SegmentWorkflow
	// outlives the reset, and the new run's StartChildWorkflowExecution then dies
	// with "child workflow execution already started" — failing the whole
	// pipeline and leaving nothing to re-signal. Kill them first, and abort if we
	// can't, because resetting anyway walks straight into that collision.
	// Terminate (not cancel): the child's work is being discarded regardless, and
	// terminate is closed-on-return, so the reset's fan-out can reuse the ID.
	killed, err := s.terminateChildren(r, wf, reason)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "terminate in-flight children: " + err.Error()})
		return
	}
	if err := s.Control.Reset(r.Context(), wf, id, reason); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "reset", "targetEventId": id, "terminatedChildren": killed})
}

// pendingChildren lists the current run's open child workflows.
func (s *Server) pendingChildren(r *http.Request, wf string) ([]PendingChild, error) {
	desc, err := s.Temporal.Describe(r.Context(), wf)
	if err != nil {
		return nil, err
	}
	return desc.Children, nil
}

// terminateChildren kills every open child of wf, returning the IDs it closed.
func (s *Server) terminateChildren(r *http.Request, wf, reason string) ([]string, error) {
	kids, err := s.pendingChildren(r, wf)
	if err != nil {
		return nil, err
	}
	killed := make([]string, 0, len(kids))
	for _, c := range kids {
		if err := s.Control.Terminate(r.Context(), c.WorkflowID, c.RunID, reason); err != nil {
			return killed, fmt.Errorf("%s: %w", c.WorkflowID, err)
		}
		killed = append(killed, c.WorkflowID)
	}
	return killed, nil
}
