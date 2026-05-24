package activity

import (
	"fmt"

	"github.com/melbournecocoa/decanter/model"
)

// calculateSegments computes segment boundaries from bumper regions and total video duration.
// Returns segments representing the content between bumpers (and before first / after last).
func calculateSegments(bumpers []model.BumperRegion, totalDuration float64) []model.Segment {
	var segments []model.Segment

	// First segment: start of video to first bumper
	start := 0.0
	for i, b := range bumpers {
		segments = append(segments, model.Segment{
			Index:    i,
			FilePath: fmt.Sprintf("segments/segment-%02d.mp4", i),
			Start:    start,
			End:      b.VisualStart,
		})
		start = b.VisualEnd
	}

	// Last segment: after last bumper to end of video
	segments = append(segments, model.Segment{
		Index:    len(bumpers),
		FilePath: fmt.Sprintf("segments/segment-%02d.mp4", len(bumpers)),
		Start:    start,
		End:      totalDuration,
	})

	return segments
}
