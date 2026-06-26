package ui

import "fmt"

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
