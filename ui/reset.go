package ui

import (
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
)

// ResetRecipe names a well-trodden recovery reset.
type ResetRecipe struct {
	Key            string // URL/key, e.g. "redo-split"
	AnchorActivity string // first scheduled activity to land just before
	Label          string
	Explanation    string
}

var ResetRecipes = map[string]ResetRecipe{
	"redo-split": {
		Key:            "redo-split",
		AnchorActivity: "DetectBumpers",
		Label:          "Redo Split (missed bumper)",
		Explanation:    "Re-runs DetectBumpers (reading your edited bumpers.json) → Split → all per-segment work. Keeps Download & Meetup results. You'll re-review both gates.",
	},
	"redo-assemble": {
		Key:            "redo-assemble",
		AnchorActivity: "Assemble",
		Label:          "Redo Assemble (trim fix)",
		Explanation:    "Re-runs Assemble with the new trim. Keeps your review approval; re-blocks at the upload gate for fresh sign-off. Does NOT pick up skip-flag changes.",
	},
}

// findResetEventID returns the EventId of the WorkflowTaskStarted event
// immediately preceding the FIRST ActivityTaskScheduled for anchorActivity.
// Resetting there (temporal reset --event-id N) re-runs that workflow task,
// which re-schedules the anchor activity so it re-executes.
func findResetEventID(events []*historypb.HistoryEvent, anchorActivity string) (int64, error) {
	anchorIdx := -1
	for i, e := range events {
		if e.GetEventType() == enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED &&
			e.GetActivityTaskScheduledEventAttributes().GetActivityType().GetName() == anchorActivity {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 {
		return 0, fmt.Errorf("no scheduled activity %q in history", anchorActivity)
	}
	for i := anchorIdx - 1; i >= 0; i-- {
		if events[i].GetEventType() == enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED {
			return events[i].GetEventId(), nil
		}
	}
	return 0, fmt.Errorf("no WorkflowTaskStarted before %q", anchorActivity)
}
