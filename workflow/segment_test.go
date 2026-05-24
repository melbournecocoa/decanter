package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/melbournecocoa/decanter/activity"
	"github.com/melbournecocoa/decanter/model"
)

func TestSegmentWorkflow_Talk(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	a := activity.New("/tmp/test", "assets/bumper_reference.png", "scripts", "", "", "", "Melbourne-CocoaHeads")

	// Register mocks for all four activities in the talk path.
	env.OnActivity(a.Classify, mock.Anything, mock.Anything).Return(
		model.ClassifyOutput{Type: model.SegmentTypeTalk}, nil,
	)
	env.OnActivity(a.Transcribe, mock.Anything, mock.Anything).Return(
		model.TranscribeOutput{SubtitlePath: "processed/segment-01/transcript.srt"}, nil,
	)
	env.OnActivity(a.CleanTranscript, mock.Anything, mock.Anything).Return(
		model.CleanTranscriptOutput{SubtitlePath: "processed/segment-01/transcript_clean.srt"}, nil,
	)
	env.OnActivity(a.GatherMetadata, mock.Anything, mock.Anything).Return(
		model.GatherMetadataOutput{Metadata: model.TalkMetadata{
			Title:       "Test Talk",
			Speaker:     "Test Speaker",
			Description: "Test description",
			Tags:        []string{"test"},
		}}, nil,
	)

	input := SegmentWorkflowInput{
		Segment:       model.Segment{Index: 1, FilePath: "segments/segment-01.mp4", Start: 704, End: 3766},

		TotalSegments: 3,
	}
	env.ExecuteWorkflow(SegmentWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.ProcessedSegment
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.False(t, result.Skipped)
	assert.Equal(t, model.SegmentTypeTalk, result.Type)
	assert.Equal(t, "Test Talk", result.Metadata.Title)
	assert.Equal(t, "Test Speaker", result.Metadata.Speaker)
	assert.Equal(t, "Test description", result.Metadata.Description)
	assert.Equal(t, []string{"test"}, result.Metadata.Tags)
	assert.Equal(t, "processed/segment-01/transcript_clean.srt", result.SubtitlePath)

	env.AssertExpectations(t)
}

func TestSegmentWorkflow_Welcome(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	a := activity.New("/tmp/test", "assets/bumper_reference.png", "scripts", "", "", "", "Melbourne-CocoaHeads")

	env.OnActivity(a.Classify, mock.Anything, mock.Anything).Return(
		model.ClassifyOutput{Type: model.SegmentTypeWelcome}, nil,
	)

	input := SegmentWorkflowInput{
		Segment:       model.Segment{Index: 0, FilePath: "segments/segment-00.mp4", Start: 0, End: 666},

		TotalSegments: 3,
	}
	env.ExecuteWorkflow(SegmentWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result model.ProcessedSegment
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Skipped)
	assert.Equal(t, model.SegmentTypeWelcome, result.Type)

	env.AssertExpectations(t)
}
