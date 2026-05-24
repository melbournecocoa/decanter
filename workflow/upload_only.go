package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/melbournecocoa/decanter/activity"
	"github.com/melbournecocoa/decanter/model"
)

// UploadOnlyInput is the input for UploadOnlyWorkflow — used to (re)upload
// previously assembled videos without re-running the upstream pipeline.
type UploadOnlyInput struct {
	Videos []model.AssembledVideo
}

// UploadOnlyResult is the output of UploadOnlyWorkflow.
type UploadOnlyResult struct {
	YouTubeVideoIDs []string
}

// UploadOnlyWorkflow uploads a slice of already-assembled videos to YouTube.
// Use it to retry uploads after fixing an issue (auth scope, network, etc.)
// without paying for re-downloading, re-transcribing, or re-encoding.
// Retries are capped at 1 (matching PipelineWorkflow's upload step) so a
// transient failure does not create duplicate YouTube entries.
//
// The workflow ID should match the original pipeline run so that activities
// resolve to the same workspace directory and find the assembled artefacts.
func UploadOnlyWorkflow(ctx workflow.Context, input UploadOnlyInput) (UploadOnlyResult, error) {
	if len(input.Videos) == 0 {
		return UploadOnlyResult{}, fmt.Errorf("UploadOnlyWorkflow requires at least one video")
	}

	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	var a *activity.Activities

	futures := make([]workflow.Future, len(input.Videos))
	for i, video := range input.Videos {
		futures[i] = workflow.ExecuteActivity(actCtx, a.Upload, model.UploadInput{Video: video})
	}

	var ids []string
	for _, fut := range futures {
		var out model.UploadOutput
		if err := fut.Get(ctx, &out); err != nil {
			return UploadOnlyResult{}, fmt.Errorf("upload failed: %w", err)
		}
		ids = append(ids, out.YouTubeVideoID)
	}

	return UploadOnlyResult{YouTubeVideoIDs: ids}, nil
}
