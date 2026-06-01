package activity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/melbournecocoa/decanter/model"
)

func TestBumperOverride_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bumpers.json")
	want := []model.BumperRegion{
		{VisualStart: 1146.023771, VisualEnd: 1172.923771},
		{VisualStart: 2226.926354, VisualEnd: 2268.226354},
	}

	require.NoError(t, writeBumperJSON(path, want))

	// Written in the hand-editable visual_start/visual_end shape, indented.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\n    \"visual_start\":")

	got, err := readBumperOverride(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestReadBumperOverride_Missing(t *testing.T) {
	// A missing sidecar is the signal to fall through to detection: (nil, nil).
	got, err := readBumperOverride(filepath.Join(t.TempDir(), "bumpers.json"))
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestReadBumperOverride_EmptyArrayIsPresent(t *testing.T) {
	// An empty array is "present but empty" — non-nil so the caller treats it
	// as an override (and rejects it), not as an absent file.
	path := filepath.Join(t.TempDir(), "bumpers.json")
	require.NoError(t, os.WriteFile(path, []byte("[]"), 0o644))

	got, err := readBumperOverride(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got)
}

func TestSortBumpers(t *testing.T) {
	// A reviewer appends a zero-width manual boundary out of order; sortBumpers
	// must reorder ascending by VisualStart so calculateSegments stays monotonic.
	bumpers := []model.BumperRegion{
		{VisualStart: 2226.9, VisualEnd: 2268.2},
		{VisualStart: 3000.0, VisualEnd: 3000.0}, // zero-width manual boundary
		{VisualStart: 1146.0, VisualEnd: 1172.9},
	}
	sortBumpers(bumpers)

	starts := []float64{bumpers[0].VisualStart, bumpers[1].VisualStart, bumpers[2].VisualStart}
	assert.Equal(t, []float64{1146.0, 2226.9, 3000.0}, starts)
}

func TestCalculateSegments_ZeroWidthManualBoundary(t *testing.T) {
	// The end-to-end effect: a zero-width bumper splits one fused segment into
	// two adjacent segments with no gap or overlap at the cut point.
	bumpers := []model.BumperRegion{
		{VisualStart: 2268.2, VisualEnd: 2268.2},
	}
	segments := calculateSegments(bumpers, 3600)

	require.Len(t, segments, 2)
	assert.Equal(t, 0.0, segments[0].Start)
	assert.Equal(t, 2268.2, segments[0].End)
	assert.Equal(t, 2268.2, segments[1].Start)
	assert.Equal(t, 3600.0, segments[1].End)
}
