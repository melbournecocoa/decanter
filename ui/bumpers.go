package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/melbournecocoa/decanter/model"
)

// bumperJSON matches the on-disk shape detect_bumpers.py / activity emit.
type bumperJSON struct {
	VisualStart float64 `json:"visual_start"`
	VisualEnd   float64 `json:"visual_end"`
}

// ReadBumpers loads bumpers.json (the human-editable override sidecar).
func ReadBumpers(path string) ([]model.BumperRegion, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []bumperJSON
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]model.BumperRegion, len(rows))
	for i, r := range rows {
		out[i] = model.BumperRegion{VisualStart: r.VisualStart, VisualEnd: r.VisualEnd}
	}
	return out, nil
}

// WriteBumpers writes bumpers ascending by VisualStart, in the snake_case shape
// DetectBumpers reads back as an authoritative override. Sorting matches the
// worker's defensive sort so hand-edit order does not matter.
func WriteBumpers(path string, b []model.BumperRegion) error {
	sorted := append([]model.BumperRegion(nil), b...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].VisualStart < sorted[j].VisualStart })
	rows := make([]bumperJSON, len(sorted))
	for i, r := range sorted {
		rows[i] = bumperJSON{VisualStart: r.VisualStart, VisualEnd: r.VisualEnd}
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bumpers: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// SourceTimeForBoundary converts a time read off segments/segment-NN.mp4 (the
// player's coordinate) into an absolute source-video time, matching Assemble's
// resolveContentRange: roughStartInSource = seg.Start - seg.StartOffset.
func SourceTimeForBoundary(seg model.Segment, fileSeconds float64) float64 {
	return seg.Start - seg.StartOffset + fileSeconds
}
