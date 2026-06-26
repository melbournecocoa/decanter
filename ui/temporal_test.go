package ui

import (
	"testing"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/converter"
)

func schedActivity(id int64, name string) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{
		EventId:   id,
		EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
		Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{
			ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{
				ActivityType: &commonpb.ActivityType{Name: name},
			},
		},
	}
}

func signalEvent(id int64, name string) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{
		EventId:   id,
		EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionSignaledEventAttributes{
			WorkflowExecutionSignaledEventAttributes: &historypb.WorkflowExecutionSignaledEventAttributes{
				SignalName: name,
			},
		},
	}
}

func completedActivity(id int64) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{
		EventId:   id,
		EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED,
		Attributes: &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{
			ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{
				ScheduledEventId: id - 1,
			},
		},
	}
}

func childInitiated(id int64) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{
		EventId:   id,
		EventType: enumspb.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_INITIATED,
		Attributes: &historypb.HistoryEvent_StartChildWorkflowExecutionInitiatedEventAttributes{
			StartChildWorkflowExecutionInitiatedEventAttributes: &historypb.StartChildWorkflowExecutionInitiatedEventAttributes{},
		},
	}
}

func childCompleted(id int64) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: id, EventType: enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_COMPLETED}
}

func TestSummarizeHistory(t *testing.T) {
	events := []*historypb.HistoryEvent{
		schedActivity(5, "DetectBumpers"),
		signalEvent(9, "review_approval"),
		schedActivity(12, "Assemble"),
		completedActivity(13), // ScheduledEventId 12 -> resolves to "Assemble"
		childInitiated(7),
	}
	sum := summarizeHistory(events)
	if !sum.scheduled("DetectBumpers") || !sum.scheduled("Assemble") {
		t.Fatalf("scheduled activities missing: %+v", sum)
	}
	if !sum.signalled("review_approval") {
		t.Fatalf("signal missing: %+v", sum)
	}
	if !sum.completed("Assemble") {
		t.Fatalf("completed activity not resolved: %+v", sum)
	}
	if sum.ChildrenInitiated != 1 {
		t.Fatalf("children initiated = %d, want 1", sum.ChildrenInitiated)
	}
}

func TestDecodeHeartbeat(t *testing.T) {
	dc := converter.GetDefaultDataConverter()
	intP, _ := dc.ToPayloads(int64(72))
	if got := decodeHeartbeat(intP); got != int64(72) {
		t.Fatalf("int heartbeat = %#v, want int64(72)", got)
	}
	strP, _ := dc.ToPayloads("video uploaded")
	if got := decodeHeartbeat(strP); got != "video uploaded" {
		t.Fatalf("string heartbeat = %#v, want \"video uploaded\"", got)
	}
	if got := decodeHeartbeat(nil); got != nil {
		t.Fatalf("nil heartbeat = %#v, want nil", got)
	}
	emptyP := &commonpb.Payloads{}
	if got := decodeHeartbeat(emptyP); got != nil {
		t.Fatalf("empty payloads heartbeat = %#v, want nil", got)
	}
}
