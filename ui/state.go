package ui

// GateState is the reviewer-facing run state.
type GateState string

const (
	GateRunning    GateState = "running"
	GateReview     GateState = "review_gate"
	GateUpload     GateState = "upload_gate"
	GateCompleted  GateState = "completed"
	GateFailed     GateState = "failed"
	GateTerminated GateState = "terminated"
	GateUnknown    GateState = "unknown"
)

// HistorySummary is the distilled, testable view of a parent workflow history.
type HistorySummary struct {
	ScheduledActivities map[string]int // activity type name -> count scheduled
	CompletedActivities map[string]int // activity type name -> count completed
	Signals             map[string]int // signal name -> count received
	ChildrenInitiated   int            // StartChildWorkflowExecutionInitiated count
	ChildrenClosed      int            // ChildWorkflowExecutionCompleted count
}

func (s HistorySummary) scheduled(name string) bool { return s.ScheduledActivities[name] > 0 }
func (s HistorySummary) completed(name string) bool { return s.CompletedActivities[name] > 0 }
func (s HistorySummary) signalled(name string) bool { return s.Signals[name] > 0 }

// allCompleted reports whether every scheduled instance of an activity has
// completed. The pipeline fans out per-segment activities (Assemble, Upload)
// in a single workflow task, so all instances are scheduled at once; this
// distinguishes "the whole fan-out finished" from "the first one finished".
func (s HistorySummary) allCompleted(name string) bool {
	return s.ScheduledActivities[name] > 0 && s.CompletedActivities[name] >= s.ScheduledActivities[name]
}

// classifyState maps (history summary, describe status) to a GateState.
// Status strings are the Temporal enum String() values: "Running",
// "Completed", "Failed", "Terminated", "Canceled", "TimedOut", "ContinuedAsNew".
func classifyState(sum HistorySummary, status string) GateState {
	switch status {
	case "Completed":
		return GateCompleted
	case "Failed", "TimedOut":
		return GateFailed
	case "Terminated", "Canceled":
		return GateTerminated
	case "Running":
		// upload gate: past review, EVERY assemble done, awaiting upload
		// approval. Waiting on all (not the first) keeps the pill on "running"
		// while sibling segments are still encoding.
		if sum.signalled("review_approval") && sum.allCompleted("Assemble") && !sum.signalled("upload_approval") {
			return GateUpload
		}
		// review gate: children fanned out AND all closed, assemble not yet
		// scheduled, no review approval.
		if sum.ChildrenInitiated > 0 && sum.ChildrenClosed >= sum.ChildrenInitiated &&
			!sum.scheduled("Assemble") && !sum.signalled("review_approval") {
			return GateReview
		}
		return GateRunning
	default:
		return GateUnknown
	}
}
