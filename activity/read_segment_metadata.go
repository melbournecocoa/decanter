package activity

import (
	"context"
	"fmt"
	"path/filepath"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

// ReadSegmentMetadata reads each requested segment's metadata.json from disk
// after the review_approval gate. The pipeline uses this to honour the
// reviewer's `skip: true` edits before fanning out Assemble — same
// "metadata.json is the authoritative human-editable contract" pattern that
// Upload already relies on, just applied earlier.
func (a *Activities) ReadSegmentMetadata(ctx context.Context, input model.ReadSegmentMetadataInput) (model.ReadSegmentMetadataOutput, error) {
	logger := activity.GetLogger(ctx)
	wsDir := a.workspaceDir(ctx)

	out := model.ReadSegmentMetadataOutput{
		Segments: make([]model.SegmentMetadata, 0, len(input.Segments)),
	}
	for _, ref := range input.Segments {
		metadataPath := filepath.Join(wsDir, filepath.Dir(ref.SubtitlePath), "metadata.json")
		md, err := readMetadata(metadataPath)
		if err != nil {
			return model.ReadSegmentMetadataOutput{}, fmt.Errorf("read metadata for segment %d (%s): %w", ref.Index, metadataPath, err)
		}
		logger.Info("Read segment metadata", "segmentIndex", ref.Index, "title", md.Title, "skip", md.Skip)
		out.Segments = append(out.Segments, model.SegmentMetadata{
			Index:    ref.Index,
			Metadata: md,
		})
	}
	return out, nil
}
