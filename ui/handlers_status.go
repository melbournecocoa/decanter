package ui

import (
	"net/http"
	"os"

	"github.com/melbournecocoa/decanter/model"
)

func (s *Server) registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs/{wf}/status", s.handleRunStatus)
}

func (s *Server) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	wf := r.PathValue("wf")
	rs := RunStatus{State: GateUnknown, Phase: "unknown", Segments: []SegmentStatus{}}

	if s.Temporal == nil {
		writeJSON(w, http.StatusOK, rs)
		return
	}
	desc, err := s.Temporal.Describe(r.Context(), wf)
	if err != nil {
		writeJSON(w, http.StatusOK, rs) // never 5xx the poller
		return
	}
	events, _ := s.Temporal.History(r.Context(), wf, "")
	rs.State = classifyState(summarizeHistory(events), desc.Status)
	rs.Phase = derivePhase(rs.State, desc.Pending, len(desc.Children) > 0)

	segs, _ := ListRunSegments(s.Base, wf)
	splitSegs, _ := SegmentsFromHistory(events) // may be empty pre-Split; error intentionally ignored
	splitByIdx := map[int]model.Segment{}
	for _, sg := range splitSegs {
		splitByIdx[sg.Index] = sg
	}
	idxByActivity := assembleUploadIndex(events)

	// invert: map segment index -> pending activity name + heartbeat
	type pend struct {
		name string
		hb   any
	}
	pendBySeg := map[int]pend{}
	for _, a := range desc.Pending {
		if idx, ok := idxByActivity[a.ActivityID]; ok {
			pendBySeg[idx] = pend{name: a.Name, hb: a.Heartbeat}
		}
	}

	for _, si := range segs {
		ss := SegmentStatus{Index: si.Index, HasFinal: statExists(FinalVideoPath(s.Base, wf, si.Index))}
		switch {
		case si.Type != model.SegmentTypeTalk || si.Skip:
			ss.Phase = "skipped"
		case rs.Phase == "processing":
			// Child workflows are described ONLY during the processing phase.
			ss.Phase, ss.Step = childPhase(s, r, wf, si.Index)
		default:
			if p, ok := pendBySeg[si.Index]; ok {
				resolveActivePercent(&ss, p.name, p.hb, splitByIdx[si.Index],
					readMetaQuiet(s.Base, wf, si.Index), FinalVideoPath(s.Base, wf, si.Index))
			} else if ss.HasFinal {
				ss.Phase = "done"
			} else {
				ss.Phase = "queued"
			}
		}
		rs.Segments = append(rs.Segments, ss)
	}
	writeJSON(w, http.StatusOK, rs)
}

// childPhase describes the child sub-workflow and maps its single pending
// activity to a phase label + step dots. Falls back to "done"/"queued".
func childPhase(s *Server, r *http.Request, wf string, idx int) (string, *Step) {
	childID := childWorkflowID(wf, idx)
	d, err := s.Temporal.Describe(r.Context(), childID)
	if err != nil || d == nil {
		return "queued", nil
	}
	for _, a := range d.Pending {
		if step, label := childStep(a.Name); step != nil {
			return label, step
		}
	}
	if d.Status == "Completed" {
		return "done", nil
	}
	return "queued", nil
}

// resolveActivePercent fills phase/percent/detail for an in-flight Assemble or
// Upload activity from its heartbeat.
// Note: the Assemble % may saturate slightly early because the denominator is
// content duration (trim or Split window) while the heartbeat is encode time,
// which can slightly exceed the nominal duration due to encoding overhead.
// This is acceptable — the bar will clamp to 100% regardless.
func resolveActivePercent(ss *SegmentStatus, activity string, hb any, seg model.Segment, m model.TalkMetadata, finalPath string) {
	switch activity {
	case "Assemble":
		ss.Phase = "assembling"
		if secs, ok := hb.(int64); ok {
			den := assembleDenominator(m, seg)
			ss.Percent = percentOf(float64(secs), den)
			ss.Detail = fmtClock(float64(secs)) + " / " + fmtClock(den)
		}
	case "Upload":
		ss.Phase = "uploading"
		if bytes, ok := hb.(int64); ok {
			if fi, err := os.Stat(finalPath); err == nil {
				ss.Percent = percentOf(float64(bytes), float64(fi.Size()))
				ss.Detail = fmtMB(float64(bytes)) + " / " + fmtMB(float64(fi.Size()))
			}
		}
	}
}

func statExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readMetaQuiet(base, wf string, idx int) model.TalkMetadata {
	m, _ := ReadMetadata(MetadataPath(base, wf, idx))
	return m
}
