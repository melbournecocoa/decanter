package ui

import (
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
)

func wfTaskStarted(id int64) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: id, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED}
}

func TestFindResetEventID(t *testing.T) {
	events := []*historypb.HistoryEvent{
		wfTaskStarted(4),
		schedActivity(6, "DetectBumpers"),
		wfTaskStarted(20),
		schedActivity(22, "Assemble"),
		schedActivity(23, "Assemble"),
	}
	got, err := findResetEventID(events, "DetectBumpers")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 4 {
		t.Fatalf("DetectBumpers reset id = %d, want 4 (the preceding WorkflowTaskStarted)", got)
	}

	got, err = findResetEventID(events, "Assemble")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 20 {
		t.Fatalf("Assemble reset id = %d, want 20 (WFTStarted before FIRST Assemble)", got)
	}

	if _, err := findResetEventID(events, "Nope"); err == nil {
		t.Fatal("expected error for missing anchor activity")
	}
}
