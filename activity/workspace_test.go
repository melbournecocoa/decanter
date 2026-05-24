package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkspacePath(t *testing.T) {
	got := workspacePath("/mnt/nas/decanter/runs", "pipeline-abc123")
	assert.Equal(t, "/mnt/nas/decanter/runs/pipeline-abc123", got)
}

func TestWorkspacePath_TrailingSlash(t *testing.T) {
	got := workspacePath("/mnt/nas/decanter/runs/", "pipeline-abc123")
	assert.Equal(t, "/mnt/nas/decanter/runs/pipeline-abc123", got)
}
