package activity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/melbournecocoa/decanter/model"
)

func TestReadEvent_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, eventFileName)

	want := model.EventMetadata{RecordingDate: "2026-05-15T19:00:00+10:00"}
	require.NoError(t, writeEvent(path, want))

	got, err := readEvent(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// Pretty-printed for human editing.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\n  \"recordingDate\":")
}

func TestReadEvent_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, eventFileName)

	got, err := readEvent(path)
	require.NoError(t, err)
	assert.Equal(t, model.EventMetadata{}, got)
}

func TestReadEvent_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, eventFileName)
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	_, err := readEvent(path)
	require.Error(t, err)
}

func TestWriteEvent_EmptyStub(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, eventFileName)

	require.NoError(t, writeEvent(path, model.EventMetadata{}))

	got, err := readEvent(path)
	require.NoError(t, err)
	assert.Equal(t, "", got.RecordingDate)
}
