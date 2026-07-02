package ui

import (
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/converter"

	"github.com/melbournecocoa/decanter/model"
)

// Step is a discrete progress position within the 4-activity child workflow.
type Step struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

var childSteps = map[string]struct {
	n     int
	label string
}{
	"Classify":        {1, "classify"},
	"Transcribe":      {2, "transcribe"},
	"CleanTranscript": {3, "clean"},
	"GatherMetadata":  {4, "metadata"},
}

// childStep maps a child sub-workflow activity name to its step + short label.
// Returns (nil, "") for anything that is not one of the four child activities.
func childStep(activityName string) (*Step, string) {
	e, ok := childSteps[activityName]
	if !ok {
		return nil, ""
	}
	return &Step{Current: e.n, Total: 4}, e.label
}

// percentOf returns a clamped 0–100 integer percentage, or nil when the
// denominator is non-positive (unknown duration/size).
func percentOf(num, den float64) *int {
	if den <= 0 {
		return nil
	}
	p := int(num / den * 100)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return &p
}

func fmtClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	s := int(seconds)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func fmtMB(bytes float64) string {
	return fmt.Sprintf("%.1f MB", bytes/1_000_000)
}

// parentPhase maps a pending parent activity name to a run phase label.
var parentPhase = map[string]string{
	"Download": "downloading", "Import": "downloading",
	"FetchMeetupEvent": "fetching_meetup", "DetectBumpers": "detecting_bumpers",
	"Split": "splitting", "ReadSegmentMetadata": "processing",
	"Assemble": "assembling", "Upload": "uploading",
}

// derivePhase produces the run-level phase label. Gate/terminal states map
// straight through; while running, the first recognised pending parent
// activity wins, else open children mean "processing", else bare "running".
func derivePhase(state GateState, pendingParent []ActivityProgress, hasChildren bool) string {
	switch state {
	case GateReview:
		return "review_gate"
	case GateUpload:
		return "upload_gate"
	case GateCompleted:
		return "completed"
	case GateFailed:
		return "failed"
	case GateTerminated:
		return "terminated"
	}
	for _, a := range pendingParent {
		if ph, ok := parentPhase[a.Name]; ok {
			return ph
		}
	}
	if hasChildren {
		return "processing"
	}
	return "running"
}

// SegmentStatus is one segment row's live status.
type SegmentStatus struct {
	Index   int    `json:"index"`
	Phase   string `json:"phase"`             // queued|classify|transcribe|clean|metadata|done|skipped|assembling|uploading|uploaded
	Step    *Step  `json:"step,omitempty"`    // child-phase dots
	Percent *int   `json:"percent,omitempty"` // assembling/uploading only
	Detail  string `json:"detail,omitempty"`  // e.g. "1:12 / 2:04"
	HasFinal bool  `json:"hasFinal"`
}

// RunStatus is the /status payload.
type RunStatus struct {
	State    GateState       `json:"state"`
	Phase    string          `json:"phase"`
	Segments []SegmentStatus `json:"segments"`
}

// assembleDenominator returns the content duration (seconds) used as the
// Assemble % denominator: the reviewer trim window when set, else the Split
// segment's End-Start. Returns 0 when unknown (→ percentOf yields nil).
func assembleDenominator(m model.TalkMetadata, seg model.Segment) float64 {
	if m.Trim != nil && m.Trim.EndSeconds > m.Trim.StartSeconds {
		return m.Trim.EndSeconds - m.Trim.StartSeconds
	}
	return seg.End - seg.Start
}

// childWorkflowID mirrors workflow/pipeline.go's child naming.
func childWorkflowID(wf string, idx int) string {
	return fmt.Sprintf("%s-segment-%d", wf, idx)
}

// assembleUploadIndex decodes scheduled Assemble/Upload inputs from history to
// map each activity's ActivityId to its segment index, so an in-flight parent
// activity (which carries no input) can be attributed to a segment row.
func assembleUploadIndex(events []*historypb.HistoryEvent) map[string]int {
	out := map[string]int{}
	dc := converter.GetDefaultDataConverter()
	for _, e := range events {
		if e.GetEventType() != enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED {
			continue
		}
		a := e.GetActivityTaskScheduledEventAttributes()
		switch a.GetActivityType().GetName() {
		case "Assemble":
			var in model.AssembleInput
			if dc.FromPayloads(a.GetInput(), &in) == nil {
				out[a.GetActivityId()] = in.Segment.Index
			}
		case "Upload":
			var in model.UploadInput
			if dc.FromPayloads(a.GetInput(), &in) == nil {
				out[a.GetActivityId()] = in.Video.SegmentIndex
			}
		}
	}
	return out
}
