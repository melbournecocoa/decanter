package ui

import "testing"

func TestClassifyState(t *testing.T) {
	cases := []struct {
		name   string
		sum    HistorySummary
		status string
		want   GateState
	}{
		{
			name:   "review gate: children done, no assemble, no review signal",
			sum:    HistorySummary{ChildrenInitiated: 4, ChildrenClosed: 4, ScheduledActivities: map[string]int{}, Signals: map[string]int{}},
			status: "Running",
			want:   GateReview,
		},
		{
			name:   "still running: children initiated but not all closed",
			sum:    HistorySummary{ChildrenInitiated: 4, ChildrenClosed: 1, ScheduledActivities: map[string]int{}, Signals: map[string]int{}},
			status: "Running",
			want:   GateRunning,
		},
		{
			name:   "upload gate: review signalled, assemble done, no upload signal",
			sum:    HistorySummary{ChildrenInitiated: 4, CompletedActivities: map[string]int{"Assemble": 2}, ScheduledActivities: map[string]int{"Assemble": 2}, Signals: map[string]int{"review_approval": 1}},
			status: "Running",
			want:   GateUpload,
		},
		{
			// Assembles fan out in one workflow task, so all are scheduled at
			// once; the upload gate must wait for EVERY one to complete, not the
			// first. Otherwise the pill flips to "ready for upload" while
			// siblings are still encoding.
			name:   "still assembling: review signalled but only some Assembles done",
			sum:    HistorySummary{ChildrenInitiated: 3, CompletedActivities: map[string]int{"Assemble": 1}, ScheduledActivities: map[string]int{"Assemble": 3}, Signals: map[string]int{"review_approval": 1}},
			status: "Running",
			want:   GateRunning,
		},
		{
			name:   "completed",
			sum:    HistorySummary{},
			status: "Completed",
			want:   GateCompleted,
		},
		{
			name:   "early running: still splitting",
			sum:    HistorySummary{ScheduledActivities: map[string]int{"DetectBumpers": 1}, Signals: map[string]int{}},
			status: "Running",
			want:   GateRunning,
		},
	}
	for _, c := range cases {
		if got := classifyState(c.sum, c.status); got != c.want {
			t.Errorf("%s: classifyState = %q, want %q", c.name, got, c.want)
		}
	}
}
