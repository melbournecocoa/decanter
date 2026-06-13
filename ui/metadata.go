package ui

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/melbournecocoa/decanter/model"
)

// ReadMetadata loads a talk's metadata.json into model.TalkMetadata.
func ReadMetadata(path string) (model.TalkMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return model.TalkMetadata{}, err
	}
	var m model.TalkMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return model.TalkMetadata{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// WriteMetadata serialises model.TalkMetadata back to disk using the same
// 2-space indent the worker uses (activity/gather_metadata.go). omitempty on
// Trim/Skip is preserved by the struct tags.
func WriteMetadata(path string, m model.TalkMetadata) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
