package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/melbournecocoa/decanter/model"
)

// SegmentInfo is the disk-derived view of one segment for the run overview.
type SegmentInfo struct {
	Index       int               `json:"index"`
	Type        model.SegmentType `json:"type"`
	HasMetadata bool              `json:"hasMetadata"`
	Skip        bool              `json:"skip"`
	Title       string            `json:"title,omitempty"`
	Speaker     string            `json:"speaker,omitempty"`
	HasFinal    bool              `json:"hasFinal"`
}

// segmentType mirrors activity/classify.go's purely positional heuristic.
func segmentType(idx, total int) model.SegmentType {
	switch {
	case idx == 0:
		return model.SegmentTypeWelcome
	case idx == total-1:
		return model.SegmentTypeWrapUp
	default:
		return model.SegmentTypeTalk
	}
}

var segFileRe = regexp.MustCompile(`^segment-(\d+)\.mp4$`)

// ListRunSegments scans <ws>/segments for segment-NN.mp4 files and enriches
// each with its positional type and (if present) metadata.json skip/title.
func ListRunSegments(base, wf string) ([]SegmentInfo, error) {
	segDir := filepath.Join(WorkspacePath(base, wf), "segments")
	entries, err := os.ReadDir(segDir)
	if err != nil {
		// Split creates segments/; a run that hasn't reached Split has zero
		// segments — a valid empty state, not an error.
		if os.IsNotExist(err) {
			return []SegmentInfo{}, nil
		}
		return nil, err
	}
	var idxs []int
	for _, e := range entries {
		if m := segFileRe.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[1])
			idxs = append(idxs, n)
		}
	}
	sort.Ints(idxs)
	total := len(idxs)
	out := make([]SegmentInfo, 0, total)
	for _, idx := range idxs {
		info := SegmentInfo{Index: idx, Type: segmentType(idx, total)}
		if m, err := ReadMetadata(MetadataPath(base, wf, idx)); err == nil {
			info.HasMetadata = true
			info.Skip = m.Skip
			info.Title = m.Title
			info.Speaker = m.Speaker
		}
		info.HasFinal = statExists(FinalVideoPath(base, wf, idx))
		out = append(out, info)
	}
	return out, nil
}

// ListWorkspaceRuns returns the workflow IDs that have a workspace directory,
// newest first (directory names are timestamped, so reverse lexical = newest).
func ListWorkspaceRuns(base string) ([]string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "imports" {
			ids = append(ids, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}
