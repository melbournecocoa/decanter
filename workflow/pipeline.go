package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/melbournecocoa/decanter/activity"
	"github.com/melbournecocoa/decanter/model"
)

// PipelineResult is the output of the PipelineWorkflow.
type PipelineResult struct {
	UploadedVideos   []model.AssembledVideo
	YouTubeVideoIDs  []string
	SkippedSegments  int
}

// PipelineWorkflow is the top-level orchestrator workflow. It downloads a video,
// detects bumpers, splits segments, fans out to child workflows for each segment,
// waits for human review signals, assembles final videos, and uploads them.
func PipelineWorkflow(ctx workflow.Context, input model.PipelineInput) (PipelineResult, error) {
	// Step 1: Set up activity options.
	// StartToCloseTimeout is generous (2h) because DetectBumpers does a full
	// silencedetect pass over the source video — higher-quality local imports
	// (e.g. 1080p60 OBS captures) take materially longer than the compressed
	// YouTube re-encodes. Other activities finish well under this floor.
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    2 * time.Minute,
	}
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	var a *activity.Activities

	// Step 2: Acquire the video — download from YouTube or import a local file.
	if (input.YouTubeURL == "") == (input.LocalFileName == "") {
		return PipelineResult{}, fmt.Errorf("exactly one of YouTubeURL or LocalFileName must be set")
	}
	if input.RecordingDate != "" {
		if _, err := time.Parse(time.RFC3339, input.RecordingDate); err != nil {
			return PipelineResult{}, fmt.Errorf("RecordingDate must be RFC3339: %w", err)
		}
	}

	var sourceVideoPath string
	if input.LocalFileName != "" {
		var importOutput model.ImportOutput
		err := workflow.ExecuteActivity(actCtx, a.Import,
			model.ImportInput{
				FileName:      input.LocalFileName,
				RecordingDate: input.RecordingDate,
			}).Get(ctx, &importOutput)
		if err != nil {
			return PipelineResult{}, fmt.Errorf("import failed: %w", err)
		}
		sourceVideoPath = importOutput.VideoPath
	} else {
		var downloadOutput model.DownloadOutput
		err := workflow.ExecuteActivity(actCtx, a.Download,
			model.DownloadInput{
				YouTubeURL:    input.YouTubeURL,
				RecordingDate: input.RecordingDate,
			}).Get(ctx, &downloadOutput)
		if err != nil {
			return PipelineResult{}, fmt.Errorf("download failed: %w", err)
		}
		sourceVideoPath = downloadOutput.VideoPath
	}

	// Step 2b: Look up the matching Meetup event (best-effort agenda source
	// for GatherMetadata — anonymous GraphQL, hard-fails on API errors only).
	// Runs before the review gate so the reviewer sees the matched agenda.
	//
	// Gated behind workflow.GetVersion so workflows that were already
	// in-flight when this activity was introduced can replay their existing
	// history cleanly (they skip the new branch and never call
	// FetchMeetupEvent). New workflows record the marker and run it.
	var meetupOutput model.FetchMeetupEventOutput
	if v := workflow.GetVersion(ctx, "add-fetch-meetup-event", workflow.DefaultVersion, 1); v >= 1 {
		err := workflow.ExecuteActivity(actCtx, a.FetchMeetupEvent, model.FetchMeetupEventInput{}).Get(ctx, &meetupOutput)
		if err != nil {
			return PipelineResult{}, fmt.Errorf("fetch meetup event failed: %w", err)
		}
	}

	// Step 3: Detect bumpers.
	detectInput := model.DetectBumpersInput{VideoPath: sourceVideoPath}
	var detectOutput model.DetectBumpersOutput
	err := workflow.ExecuteActivity(actCtx, a.DetectBumpers, detectInput).Get(ctx, &detectOutput)
	if err != nil {
		return PipelineResult{}, fmt.Errorf("detect bumpers failed: %w", err)
	}

	// Step 4: Split the video into segments.
	splitInput := model.SplitInput{VideoPath: sourceVideoPath, Bumpers: detectOutput.Bumpers}
	var splitOutput model.SplitOutput
	err = workflow.ExecuteActivity(actCtx, a.Split, splitInput).Get(ctx, &splitOutput)
	if err != nil {
		return PipelineResult{}, fmt.Errorf("split failed: %w", err)
	}

	// Step 5: Fan-out child workflows for each segment.
	childFutures := make([]workflow.ChildWorkflowFuture, len(splitOutput.Segments))
	for i, seg := range splitOutput.Segments {
		childID := fmt.Sprintf("%s-segment-%d", workflow.GetInfo(ctx).WorkflowExecution.ID, seg.Index)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childID,
		})
		childFutures[i] = workflow.ExecuteChildWorkflow(childCtx, SegmentWorkflow, SegmentWorkflowInput{
			Segment:         seg,
			TotalSegments:   len(splitOutput.Segments),
			MeetupEventPath: meetupOutput.MeetupEventPath,
		})
	}

	// Collect results from all child workflows.
	var processedSegments []model.ProcessedSegment
	for _, future := range childFutures {
		var result model.ProcessedSegment
		if err := future.Get(ctx, &result); err != nil {
			return PipelineResult{}, fmt.Errorf("segment workflow failed: %w", err)
		}
		processedSegments = append(processedSegments, result)
	}

	// Step 6: Signal gate - review_approval. Block until a human sends approval.
	reviewCh := workflow.GetSignalChannel(ctx, "review_approval")
	var reviewApproval model.ReviewApproval
	reviewCh.Receive(ctx, &reviewApproval)
	if !reviewApproval.Approved {
		return PipelineResult{}, fmt.Errorf("review rejected by human")
	}

	// Step 7: Filter to talk segments. Metadata is no longer threaded through
	// the workflow — Upload re-reads metadata.json from disk so that any
	// human edits made during the review_approval gate are picked up.
	var talkSegments []model.ProcessedSegment
	skippedCount := 0
	for _, ps := range processedSegments {
		if ps.Skipped {
			skippedCount++
			continue
		}
		talkSegments = append(talkSegments, ps)
	}

	// Step 8: Fan-out Assemble for each talk segment.
	assembleFutures := make([]workflow.Future, len(talkSegments))
	for i, ts := range talkSegments {
		assembleInput := model.AssembleInput{
			Segment:      ts.Segment,
			SubtitlePath: ts.SubtitlePath,
		}
		assembleFutures[i] = workflow.ExecuteActivity(actCtx, a.Assemble, assembleInput)
	}

	var assembledVideos []model.AssembledVideo
	for _, future := range assembleFutures {
		var assembleOutput model.AssembleOutput
		if err := future.Get(ctx, &assembleOutput); err != nil {
			return PipelineResult{}, fmt.Errorf("assemble failed: %w", err)
		}
		assembledVideos = append(assembledVideos, assembleOutput.Video)
	}

	// Step 9: Signal gate - upload_approval. Block until human approves.
	uploadCh := workflow.GetSignalChannel(ctx, "upload_approval")
	var uploadApproval model.ReviewApproval
	uploadCh.Receive(ctx, &uploadApproval)
	if !uploadApproval.Approved {
		return PipelineResult{}, fmt.Errorf("upload rejected by human")
	}

	// Step 10: Fan-out Upload for each assembled video.
	// Upload is not idempotent — a retry creates a duplicate YouTube video.
	// Cap at one attempt so a transient failure surfaces as a workflow error
	// rather than silently creating duplicates.
	uploadOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}
	uploadCtx := workflow.WithActivityOptions(ctx, uploadOpts)

	uploadFutures := make([]workflow.Future, len(assembledVideos))
	for i, video := range assembledVideos {
		uploadInput := model.UploadInput{Video: video}
		uploadFutures[i] = workflow.ExecuteActivity(uploadCtx, a.Upload, uploadInput)
	}

	var youtubeVideoIDs []string
	for _, future := range uploadFutures {
		var uploadOutput model.UploadOutput
		if err := future.Get(ctx, &uploadOutput); err != nil {
			return PipelineResult{}, fmt.Errorf("upload failed: %w", err)
		}
		youtubeVideoIDs = append(youtubeVideoIDs, uploadOutput.YouTubeVideoID)
	}

	// Step 11: Return result.
	return PipelineResult{
		UploadedVideos:  assembledVideos,
		YouTubeVideoIDs: youtubeVideoIDs,
		SkippedSegments: skippedCount,
	}, nil
}
