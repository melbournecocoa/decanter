package activity

import (
	"context"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

func (a *Activities) Classify(ctx context.Context, input model.ClassifyInput) (model.ClassifyOutput, error) {
	logger := activity.GetLogger(ctx)

	var segType model.SegmentType
	switch {
	case input.Segment.Index == 0:
		segType = model.SegmentTypeWelcome
	case input.Segment.Index == input.TotalSegments-1:
		segType = model.SegmentTypeWrapUp
	default:
		segType = model.SegmentTypeTalk
	}

	logger.Info("Classified segment", "index", input.Segment.Index, "total", input.TotalSegments, "type", segType)
	return model.ClassifyOutput{Type: segType}, nil
}
