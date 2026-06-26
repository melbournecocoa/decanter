package ui

import "fmt"

// Step is a discrete progress position within the 4-activity child workflow.
type Step struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

var childStepNum = map[string]int{
	"Classify": 1, "Transcribe": 2, "CleanTranscript": 3, "GatherMetadata": 4,
}
var childStepLabel = map[int]string{1: "classify", 2: "transcribe", 3: "clean", 4: "metadata"}

// childStep maps a child sub-workflow activity name to its step + short label.
// Returns (nil, "") for anything that is not one of the four child activities.
func childStep(activityName string) (*Step, string) {
	n, ok := childStepNum[activityName]
	if !ok {
		return nil, ""
	}
	return &Step{Current: n, Total: 4}, childStepLabel[n]
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
