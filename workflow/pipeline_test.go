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

// testSegments returns the standard 3-segment test data from PoC results.
func testSegments() []model.Segment {
	return []model.Segment{
		{Index: 0, FilePath: "segments/segment-00.mp4", Start: 0, End: 666},
		{Index: 1, FilePath: "segments/segment-01.mp4", Start: 704, End: 3766},
		{Index: 2, FilePath: "segments/segment-02.mp4", Start: 3820, End: 3921},
	}
}

// setupPipelineEnvCore creates a test workflow environment with mocks for
// every step *except* video acquisition (Download / Import). The caller is
// responsible for mocking whichever acquisition activity their input exercises.
func setupPipelineEnvCore(t *testing.T) (*testsuite.TestWorkflowEnvironment, *activity.Activities, []model.Segment) {
	t.Helper()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	a := activity.New("/tmp/test", "assets/bumper_reference.png", "scripts", "", "", "", "Melbourne-CocoaHeads")
	segments := testSegments()

	// FetchMeetupEvent runs unconditionally before DetectBumpers. Tests
	// assert MeetupEventPath propagates through to child workflows below.
	env.OnActivity(a.FetchMeetupEvent, mock.Anything, mock.Anything).Return(
		model.FetchMeetupEventOutput{MeetupEventPath: "meetup_event.json"}, nil,
	)

	env.OnActivity(a.DetectBumpers, mock.Anything, mock.Anything).Return(
		model.DetectBumpersOutput{Bumpers: []model.BumperRegion{
			{VisualStart: 666, VisualEnd: 704},
			{VisualStart: 3766, VisualEnd: 3820},
		}}, nil,
	)
	env.OnActivity(a.Split, mock.Anything, mock.Anything).Return(
		model.SplitOutput{Segments: segments}, nil,
	)

	// Mock child SegmentWorkflow for each segment. MatchedBy asserts that
	// MeetupEventPath is plumbed through from the FetchMeetupEvent mock.
	// Segment 0: welcome, skipped.
	env.OnWorkflow(SegmentWorkflow, mock.Anything, mock.MatchedBy(func(input SegmentWorkflowInput) bool {
		return input.Segment.Index == 0 && input.TotalSegments == 3 && input.MeetupEventPath == "meetup_event.json"
	})).Return(model.ProcessedSegment{
		Segment: segments[0],
		Type:    model.SegmentTypeWelcome,
		Skipped: true,
	}, nil)

	// Segment 1: talk, not skipped.
	env.OnWorkflow(SegmentWorkflow, mock.Anything, mock.MatchedBy(func(input SegmentWorkflowInput) bool {
		return input.Segment.Index == 1 && input.TotalSegments == 3 && input.MeetupEventPath == "meetup_event.json"
	})).Return(model.ProcessedSegment{
		Segment:      segments[1],
		Type:         model.SegmentTypeTalk,
		SubtitlePath: "processed/segment-01/transcript_clean.srt",
		Metadata: model.TalkMetadata{
			Title:       "Test Talk",
			Speaker:     "Test Speaker",
			Description: "Test description",
			Tags:        []string{"test"},
		},
		Skipped: false,
	}, nil)

	// Segment 2: wrapup, skipped.
	env.OnWorkflow(SegmentWorkflow, mock.Anything, mock.MatchedBy(func(input SegmentWorkflowInput) bool {
		return input.Segment.Index == 2 && input.TotalSegments == 3 && input.MeetupEventPath == "meetup_event.json"
	})).Return(model.ProcessedSegment{
		Segment: segments[2],
		Type:    model.SegmentTypeWrapUp,
		Skipped: true,
	}, nil)

	return env, a, segments
}

// mockNoReviewerSkips installs a ReadSegmentMetadata mock returning Skip=false
// for the single talk segment (index 1) — the default happy-path expectation.
// Tests exercising the skip path register their own ReadSegmentMetadata mock
// instead.
func mockNoReviewerSkips(env *testsuite.TestWorkflowEnvironment, a *activity.Activities) {
	env.OnActivity(a.ReadSegmentMetadata, mock.Anything, mock.Anything).Return(
		model.ReadSegmentMetadataOutput{
			Segments: []model.SegmentMetadata{
				{Index: 1, Metadata: model.TalkMetadata{Title: "Test Talk", Speaker: "Test Speaker"}},
			},
		}, nil,
	)
}

// setupPipelineEnv layers a Download mock on top of the core mocks — convenient
// for the existing YouTube-input tests.
func setupPipelineEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, []model.Segment) {
	t.Helper()
	env, a, segments := setupPipelineEnvCore(t)
	env.OnActivity(a.Download, mock.Anything, mock.Anything).Return(
		model.DownloadOutput{VideoPath: "source.mp4"}, nil,
	)
	return env, segments
}

// testPipelineInput returns the standard workflow input for tests.
func testPipelineInput() model.PipelineInput {
	return model.PipelineInput{
		YouTubeURL: "https://www.youtube.com/watch?v=test123",
	}
}

func TestPipelineWorkflow_HappyPath(t *testing.T) {
	env, _ := setupPipelineEnv(t)

	a := activity.New("/tmp/test", "assets/bumper_reference.png", "scripts", "", "", "", "Melbourne-CocoaHeads")

	mockNoReviewerSkips(env, a)

	// Mock Assemble for the single talk segment.
	env.OnActivity(a.Assemble, mock.Anything, mock.Anything).Return(
		model.AssembleOutput{Video: model.AssembledVideo{
			SegmentIndex:    1,
			VideoPath:       "processed/segment-01/final.mp4",
			SubtitlePath:    "processed/segment-01/final.srt",
			IntroDuration:   12.0,
			ContentDuration: 3000.0,
			StartOffset:     0.0,
		}}, nil,
	)

	// Mock Upload for the single assembled video.
	env.OnActivity(a.Upload, mock.Anything, mock.Anything).Return(
		model.UploadOutput{YouTubeVideoID: "stub-video-id-01"}, nil,
	)

	// Register signals: review_approval (approved) and upload_approval (approved).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("review_approval", model.ReviewApproval{Approved: true})
	}, 0)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("upload_approval", model.ReviewApproval{Approved: true})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testPipelineInput())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PipelineResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Len(t, result.UploadedVideos, 1)
	assert.Equal(t, 2, result.SkippedSegments)
	assert.Equal(t, 1, result.UploadedVideos[0].SegmentIndex)
	assert.Equal(t, []string{"stub-video-id-01"}, result.YouTubeVideoIDs)

	env.AssertExpectations(t)
}

func TestPipelineWorkflow_ReviewRejected(t *testing.T) {
	env, _ := setupPipelineEnv(t)

	// Send review_approval signal with Approved=false to reject.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("review_approval", model.ReviewApproval{Approved: false})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testPipelineInput())

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review rejected")
}

func TestPipelineWorkflow_LocalFileHappyPath(t *testing.T) {
	env, a, _ := setupPipelineEnvCore(t)

	mockNoReviewerSkips(env, a)

	// Acquisition mock: Import instead of Download.
	env.OnActivity(a.Import, mock.Anything, mock.MatchedBy(func(input model.ImportInput) bool {
		return input.FileName == "recovered-stream.mp4"
	})).Return(model.ImportOutput{VideoPath: "source.mp4"}, nil)

	env.OnActivity(a.Assemble, mock.Anything, mock.Anything).Return(
		model.AssembleOutput{Video: model.AssembledVideo{
			SegmentIndex:    1,
			VideoPath:       "processed/segment-01/final.mp4",
			SubtitlePath:    "processed/segment-01/final.srt",
			IntroDuration:   12.0,
			ContentDuration: 3000.0,
			StartOffset:     0.0,
		}}, nil,
	)
	env.OnActivity(a.Upload, mock.Anything, mock.Anything).Return(
		model.UploadOutput{YouTubeVideoID: "stub-video-id-01"}, nil,
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("review_approval", model.ReviewApproval{Approved: true})
	}, 0)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("upload_approval", model.ReviewApproval{Approved: true})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, model.PipelineInput{LocalFileName: "recovered-stream.mp4"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PipelineResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Len(t, result.UploadedVideos, 1)
	assert.Equal(t, 2, result.SkippedSegments)

	env.AssertExpectations(t)
}

// TestPipelineWorkflow_ReviewerSkip exercises the metadata.json skip flag
// path: ReadSegmentMetadata returns Skip=true for the only talk segment,
// so Assemble and Upload must not run, and the segment is counted as skipped.
func TestPipelineWorkflow_ReviewerSkip(t *testing.T) {
	env, a, segments := setupPipelineEnvCore(t)
	env.OnActivity(a.Download, mock.Anything, mock.Anything).Return(
		model.DownloadOutput{VideoPath: "source.mp4"}, nil,
	)

	// Register ReadSegmentMetadata returning Skip=true for the talk segment.
	// No default mock to override — testify-mock matches in declaration order,
	// not specificity, so a shared default would have eaten this expectation.
	env.OnActivity(a.ReadSegmentMetadata, mock.Anything, mock.MatchedBy(func(input model.ReadSegmentMetadataInput) bool {
		return len(input.Segments) == 1 && input.Segments[0].Index == 1
	})).Return(model.ReadSegmentMetadataOutput{
		Segments: []model.SegmentMetadata{
			{Index: 1, Metadata: model.TalkMetadata{Skip: true}},
		},
	}, nil)

	// Assemble and Upload must NOT be called — no mocks registered. If the
	// filter is broken and the workflow tries to call either, the test fails
	// because the test environment rejects unregistered activity calls.

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("review_approval", model.ReviewApproval{Approved: true})
	}, 0)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("upload_approval", model.ReviewApproval{Approved: true})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testPipelineInput())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result PipelineResult
	require.NoError(t, env.GetWorkflowResult(&result))
	// All three segments skipped: welcome (classify), seg-01 (reviewer), wrapup (classify).
	assert.Equal(t, 3, result.SkippedSegments)
	assert.Empty(t, result.UploadedVideos)
	assert.Empty(t, result.YouTubeVideoIDs)
	_ = segments

	env.AssertExpectations(t)
}

func TestPipelineWorkflow_InvalidInput_NeitherSet(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(PipelineWorkflow, model.PipelineInput{})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of YouTubeURL or LocalFileName")
}

func TestPipelineWorkflow_InvalidInput_BothSet(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(PipelineWorkflow, model.PipelineInput{
		YouTubeURL:    "https://www.youtube.com/watch?v=test123",
		LocalFileName: "recovered.mp4",
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of YouTubeURL or LocalFileName")
}

func TestPipelineWorkflow_InvalidInput_BadRecordingDate(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(PipelineWorkflow, model.PipelineInput{
		LocalFileName: "recovered.mp4",
		RecordingDate: "2026-02-19",
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RecordingDate must be RFC3339")
}
