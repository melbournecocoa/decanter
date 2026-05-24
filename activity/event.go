package activity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/melbournecocoa/decanter/model"
)

// eventFileName is the filename of the event.json sidecar inside a workspace.
const eventFileName = "event.json"

// readEvent loads event.json from a workspace dir. A missing file is not an
// error — older workspaces (pre-dating the feature) or workflows that fail
// to seed it should still upload, just without recordingDetails.
func readEvent(path string) (model.EventMetadata, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.EventMetadata{}, nil
	}
	if err != nil {
		return model.EventMetadata{}, err
	}
	var ev model.EventMetadata
	if err := json.Unmarshal(raw, &ev); err != nil {
		return model.EventMetadata{}, fmt.Errorf("parse event.json: %w", err)
	}
	return ev, nil
}

// writeEvent persists event.json (pretty-printed for human editing).
func writeEvent(path string, ev model.EventMetadata) error {
	raw, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal event.json: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write event.json: %w", err)
	}
	return nil
}
