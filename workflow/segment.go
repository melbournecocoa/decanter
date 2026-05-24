package workflow

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/melbournecocoa/decanter/activity"
	"github.com/melbournecocoa/decanter/model"
)

// SegmentWorkflowInput is the input for the SegmentWorkflow child workflow.
type SegmentWorkflowInput struct {
	Segment       model.Segment
	TotalSegments int
	// MeetupEventPath is the workspace-relative path to the cached Meetup
	// event JSON (forwarded to GatherMetadata). Empty when no lookup ran.
	MeetupEventPath string
}

// SegmentWorkflow is the child workflow that processes a single segment.
// It classifies the segment, and if it is a talk, transcribes it, cleans the
// transcript, and gathers metadata.
func SegmentWorkflow(ctx workflow.Context, input SegmentWorkflowInput) (model.ProcessedSegment, error) {
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    2 * time.Minute,
	}
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	var a *activity.Activities

	// Step 1: Classify the segment.
	classifyInput := model.ClassifyInput{
		Segment:       input.Segment,
		TotalSegments: input.TotalSegments,
	}
	var classifyOutput model.ClassifyOutput
	err := workflow.ExecuteActivity(actCtx, a.Classify, classifyInput).Get(ctx, &classifyOutput)
	if err != nil {
		return model.ProcessedSegment{}, err
	}

	// If the segment is not a talk, skip further processing.
	if classifyOutput.Type != model.SegmentTypeTalk {
		return model.ProcessedSegment{
			Segment: input.Segment,
			Type:    classifyOutput.Type,
			Skipped: true,
		}, nil
	}

	// Step 2: Transcribe the segment.
	transcribeInput := model.TranscribeInput{
		Segment: input.Segment,
	}
	var transcribeOutput model.TranscribeOutput
	err = workflow.ExecuteActivity(actCtx, a.Transcribe, transcribeInput).Get(ctx, &transcribeOutput)
	if err != nil {
		return model.ProcessedSegment{}, err
	}

	// Step 3: Clean transcript (fix technical terminology).
	cleanInput := model.CleanTranscriptInput{
		Segment:         input.Segment,
		SubtitlePath:    transcribeOutput.SubtitlePath,
		MeetupEventPath: input.MeetupEventPath,
	}
	var cleanOutput model.CleanTranscriptOutput
	err = workflow.ExecuteActivity(actCtx, a.CleanTranscript, cleanInput).Get(ctx, &cleanOutput)
	if err != nil {
		return model.ProcessedSegment{}, err
	}

	// Step 4: Gather metadata from the cleaned transcription.
	gatherMetadataInput := model.GatherMetadataInput{
		Segment:         input.Segment,
		SubtitlePath:    cleanOutput.SubtitlePath,
		MeetupEventPath: input.MeetupEventPath,
	}
	var metadataOutput model.GatherMetadataOutput
	err = workflow.ExecuteActivity(actCtx, a.GatherMetadata, gatherMetadataInput).Get(ctx, &metadataOutput)
	if err != nil {
		return model.ProcessedSegment{}, err
	}

	return model.ProcessedSegment{
		Segment:      input.Segment,
		Type:         model.SegmentTypeTalk,
		SubtitlePath: cleanOutput.SubtitlePath,
		Metadata:     metadataOutput.Metadata,
		Skipped:      false,
	}, nil
}
