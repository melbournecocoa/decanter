package activity

import (
	"context"
	"path/filepath"
	"strings"

	"go.temporal.io/sdk/activity"
)

// workspacePath returns the workspace directory for a workflow run.
func workspacePath(basePath, workflowID string) string {
	return filepath.Join(basePath, workflowID)
}

// workspaceDir returns the workspace directory for the pipeline run.
// For child workflows (e.g. "parent-id-segment-1"), it strips the suffix
// so all activities resolve to the parent pipeline's workspace.
func (a *Activities) workspaceDir(ctx context.Context) string {
	info := activity.GetInfo(ctx)
	id := info.WorkflowExecution.ID
	if i := strings.LastIndex(id, "-segment-"); i != -1 {
		id = id[:i]
	}
	return workspacePath(a.BasePath, id)
}
