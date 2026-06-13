package ui

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/melbournecocoa/decanter/model"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

func (s *Server) registerRunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("GET /api/runs/{wf}", s.handleRunDetail)
}

// RunListItem merges Temporal status with the workspace's event name.
type RunListItem struct {
	WorkflowID string    `json:"workflowId"`
	State      GateState `json:"state"`
	Status     string    `json:"status"`
	StartTime  string    `json:"startTime,omitempty"`
	EventName  string    `json:"eventName,omitempty"`
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	seen := map[string]*RunListItem{}
	order := []string{}
	if ids, err := ListWorkspaceRuns(s.Base); err == nil {
		for _, id := range ids {
			item := &RunListItem{WorkflowID: id, State: GateUnknown}
			if ev, err := ReadEvent(s.Base, id); err == nil {
				item.EventName = ev.EventName
			}
			seen[id] = item
			order = append(order, id)
		}
	}
	if s.Temporal != nil {
		if runs, err := s.Temporal.ListPipelineRuns(r.Context()); err == nil {
			for _, run := range runs {
				item, ok := seen[run.WorkflowID]
				if !ok {
					item = &RunListItem{WorkflowID: run.WorkflowID}
					seen[run.WorkflowID] = item
					order = append(order, run.WorkflowID)
				}
				item.Status = run.Status
				item.StartTime = run.StartTime
				item.State = s.deriveState(r, run.WorkflowID, run.RunID, run.Status)
			}
		}
	}
	out := make([]*RunListItem, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	wf := r.PathValue("wf")
	segs, err := ListRunSegments(s.Base, wf)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found: " + err.Error()})
		return
	}
	event, _ := ReadEvent(s.Base, wf)
	state := GateUnknown
	var status string
	if s.Temporal != nil {
		if st, err := s.Temporal.Status(r.Context(), wf); err == nil {
			status = st
			state = s.deriveState(r, wf, "", st)
		}
	}
	bumpers, _ := ReadBumpers(BumpersPath(s.Base, wf))
	writeJSON(w, http.StatusOK, map[string]any{
		"workflowId": wf,
		"event":      event,
		"segments":   segs,
		"bumpers":    bumpers,
		"state":      state,
		"status":     status,
	})
}

// deriveState pulls history and classifies; falls back to GateUnknown on error.
func (s *Server) deriveState(r *http.Request, wf, runID, status string) GateState {
	if s.Temporal == nil {
		return GateUnknown
	}
	events, err := s.Temporal.History(r.Context(), wf, runID)
	if err != nil {
		return GateUnknown
	}
	return classifyState(summarizeHistory(events), status)
}

// ReadEvent loads event.json (zero value + error if absent).
func ReadEvent(base, wf string) (model.EventMetadata, error) {
	raw, err := os.ReadFile(EventPath(base, wf))
	if err != nil {
		return model.EventMetadata{}, err
	}
	var ev model.EventMetadata
	if err := json.Unmarshal(raw, &ev); err != nil {
		return model.EventMetadata{}, err
	}
	return ev, nil
}
