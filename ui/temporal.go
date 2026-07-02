package ui

import (
	"context"
	"fmt"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	"github.com/melbournecocoa/decanter/model"
)

// TemporalRun is the list view of a pipeline workflow execution.
type TemporalRun struct {
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
	Status     string `json:"status"`
	StartTime  string `json:"startTime"`
}

// ActivityProgress is a pending activity with its decoded heartbeat payload.
type ActivityProgress struct {
	Name       string // activity type name
	ActivityID string // matches the ActivityTaskScheduled event's ActivityId
	Heartbeat  any    // int64 | string | nil (see decodeHeartbeat)
}

// PendingChild is an open child workflow.
type PendingChild struct {
	WorkflowID string
}

// WorkflowDescription is the distilled DescribeWorkflowExecution view.
type WorkflowDescription struct {
	Status   string
	Pending  []ActivityProgress
	Children []PendingChild
}

// TemporalReader is the read-only Temporal surface the console needs.
type TemporalReader interface {
	ListPipelineRuns(ctx context.Context) ([]TemporalRun, error)
	History(ctx context.Context, workflowID, runID string) ([]*historypb.HistoryEvent, error)
	Status(ctx context.Context, workflowID string) (string, error)
	Describe(ctx context.Context, workflowID string) (*WorkflowDescription, error)
}

// decodeHeartbeat best-effort decodes a heartbeat payload: int64 first
// (Assemble out-seconds, Upload byte counts), then string (stage labels),
// else nil. Never panics on unexpected shapes.
func decodeHeartbeat(p *commonpb.Payloads) any {
	if p == nil || len(p.GetPayloads()) == 0 {
		return nil
	}
	dc := converter.GetDefaultDataConverter()
	var i int64
	if err := dc.FromPayloads(p, &i); err == nil {
		return i
	}
	var s string
	if err := dc.FromPayloads(p, &s); err == nil {
		return s
	}
	return nil
}

type sdkReader struct{ c client.Client }

// NewSDKReader constructs a TemporalReader backed by a live Temporal SDK client.
func NewSDKReader(c client.Client) TemporalReader { return &sdkReader{c: c} }

// pipelineQuery lists the parent pipeline runs. NO `ORDER BY`: miyuki's Temporal
// runs basic (non-Elasticsearch) visibility, which rejects order-by with
// "operation is not supported: 'order by' clause" — erroring the whole list call
// and (because handleRuns swallows that error) dropping every run to
// GateUnknown. Display order comes from the disk listing, so ordering here is
// unnecessary anyway. Guarded by TestPipelineQueryHasNoOrderBy.
const pipelineQuery = `WorkflowType="PipelineWorkflow" AND TaskQueue="decanter-pipeline"`

func (r *sdkReader) ListPipelineRuns(ctx context.Context) ([]TemporalRun, error) {
	resp, err := r.c.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Query:    pipelineQuery,
		PageSize: 50,
	})
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	out := make([]TemporalRun, 0, len(resp.GetExecutions()))
	for _, e := range resp.GetExecutions() {
		run := TemporalRun{
			WorkflowID: e.GetExecution().GetWorkflowId(),
			RunID:      e.GetExecution().GetRunId(),
			Status:     statusString(e.GetStatus()),
		}
		if t := e.GetStartTime(); t != nil {
			run.StartTime = t.AsTime().Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, run)
	}
	return out, nil
}

func (r *sdkReader) History(ctx context.Context, workflowID, runID string) ([]*historypb.HistoryEvent, error) {
	iter := r.c.GetWorkflowHistory(ctx, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	var events []*historypb.HistoryEvent
	for iter.HasNext() {
		ev, err := iter.Next()
		if err != nil {
			return nil, fmt.Errorf("history: %w", err)
		}
		events = append(events, ev)
	}
	return events, nil
}

func (r *sdkReader) Status(ctx context.Context, workflowID string) (string, error) {
	resp, err := r.c.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return "", fmt.Errorf("describe: %w", err)
	}
	return statusString(resp.GetWorkflowExecutionInfo().GetStatus()), nil
}

func (r *sdkReader) Describe(ctx context.Context, workflowID string) (*WorkflowDescription, error) {
	resp, err := r.c.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return nil, fmt.Errorf("describe: %w", err)
	}
	out := &WorkflowDescription{
		Status: statusString(resp.GetWorkflowExecutionInfo().GetStatus()),
	}
	for _, p := range resp.GetPendingActivities() {
		out.Pending = append(out.Pending, ActivityProgress{
			Name:       p.GetActivityType().GetName(),
			ActivityID: p.GetActivityId(),
			Heartbeat:  decodeHeartbeat(p.GetHeartbeatDetails()),
		})
	}
	for _, c := range resp.GetPendingChildren() {
		out.Children = append(out.Children, PendingChild{WorkflowID: c.GetWorkflowId()})
	}
	return out, nil
}

func statusString(s enumspb.WorkflowExecutionStatus) string {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "Running"
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "Completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "Failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "Terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "Canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "TimedOut"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "ContinuedAsNew"
	default:
		return "Unknown"
	}
}

// summarizeHistory distils a parent workflow history into a HistorySummary.
func summarizeHistory(events []*historypb.HistoryEvent) HistorySummary {
	sum := HistorySummary{
		ScheduledActivities: map[string]int{},
		CompletedActivities: map[string]int{},
		Signals:             map[string]int{},
	}
	schedNames := map[int64]string{} // scheduledEventId -> activity name
	for _, e := range events {
		switch e.GetEventType() {
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED:
			name := e.GetActivityTaskScheduledEventAttributes().GetActivityType().GetName()
			sum.ScheduledActivities[name]++
			schedNames[e.GetEventId()] = name
		case enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED:
			if name := schedNames[e.GetActivityTaskCompletedEventAttributes().GetScheduledEventId()]; name != "" {
				sum.CompletedActivities[name]++
			}
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED:
			sum.Signals[e.GetWorkflowExecutionSignaledEventAttributes().GetSignalName()]++
		case enumspb.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_INITIATED:
			sum.ChildrenInitiated++
		case enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_COMPLETED:
			sum.ChildrenClosed++
		}
	}
	return sum
}

// SegmentsFromHistory decodes the Split activity's result payload from history,
// yielding segment timing (Start/StartOffset) for the bumpers playhead-mark
// conversion. Returns the first Split result found.
func SegmentsFromHistory(events []*historypb.HistoryEvent) ([]model.Segment, error) {
	var splitSchedID int64 = -1
	for _, e := range events {
		if e.GetEventType() == enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED &&
			e.GetActivityTaskScheduledEventAttributes().GetActivityType().GetName() == "Split" {
			splitSchedID = e.GetEventId()
			break
		}
	}
	if splitSchedID < 0 {
		return nil, fmt.Errorf("no Split activity in history")
	}
	for _, e := range events {
		if e.GetEventType() == enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED &&
			e.GetActivityTaskCompletedEventAttributes().GetScheduledEventId() == splitSchedID {
			var out model.SplitOutput
			payloads := e.GetActivityTaskCompletedEventAttributes().GetResult()
			if err := converter.GetDefaultDataConverter().FromPayloads(payloads, &out); err != nil {
				return nil, fmt.Errorf("decode split output: %w", err)
			}
			return out.Segments, nil
		}
	}
	return nil, fmt.Errorf("Split scheduled but not completed")
}
