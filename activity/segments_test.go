package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/melbournecocoa/decanter/model"
)

func TestCalculateSegments_TwoBumpers(t *testing.T) {
	// Matches initial PoC data: two bumpers, three segments.
	bumpers := []model.BumperRegion{
		{VisualStart: 666, VisualEnd: 704},
		{VisualStart: 3766, VisualEnd: 3820},
	}
	segments := calculateSegments(bumpers, 3921)

	assert.Len(t, segments, 3)

	// Segment 0: start → first bumper
	assert.Equal(t, 0, segments[0].Index)
	assert.Equal(t, 0.0, segments[0].Start)
	assert.Equal(t, 666.0, segments[0].End)
	assert.Equal(t, "segments/segment-00.mp4", segments[0].FilePath)

	// Segment 1: after first bumper → second bumper
	assert.Equal(t, 1, segments[1].Index)
	assert.Equal(t, 704.0, segments[1].Start)
	assert.Equal(t, 3766.0, segments[1].End)
	assert.Equal(t, "segments/segment-01.mp4", segments[1].FilePath)

	// Segment 2: after second bumper → end
	assert.Equal(t, 2, segments[2].Index)
	assert.Equal(t, 3820.0, segments[2].Start)
	assert.Equal(t, 3921.0, segments[2].End)
	assert.Equal(t, "segments/segment-02.mp4", segments[2].FilePath)
}

func TestCalculateSegments_OneBumper(t *testing.T) {
	bumpers := []model.BumperRegion{
		{VisualStart: 100, VisualEnd: 120},
	}
	segments := calculateSegments(bumpers, 500)

	assert.Len(t, segments, 2)
	assert.Equal(t, 0.0, segments[0].Start)
	assert.Equal(t, 100.0, segments[0].End)
	assert.Equal(t, 120.0, segments[1].Start)
	assert.Equal(t, 500.0, segments[1].End)
}

func TestCalculateSegments_ThreeBumpers(t *testing.T) {
	bumpers := []model.BumperRegion{
		{VisualStart: 100, VisualEnd: 120},
		{VisualStart: 500, VisualEnd: 520},
		{VisualStart: 900, VisualEnd: 920},
	}
	segments := calculateSegments(bumpers, 1000)

	assert.Len(t, segments, 4)
	assert.Equal(t, 0.0, segments[0].Start)
	assert.Equal(t, 100.0, segments[0].End)
	assert.Equal(t, 120.0, segments[1].Start)
	assert.Equal(t, 500.0, segments[1].End)
	assert.Equal(t, 520.0, segments[2].Start)
	assert.Equal(t, 900.0, segments[2].End)
	assert.Equal(t, 920.0, segments[3].Start)
	assert.Equal(t, 1000.0, segments[3].End)
}

func TestParseBumperJSON(t *testing.T) {
	input := `[{"visual_start": 666.0, "visual_end": 704.0}, {"visual_start": 3766.0, "visual_end": 3820.0}]`
	bumpers, err := parseBumperJSON([]byte(input))
	require.NoError(t, err)

	assert.Len(t, bumpers, 2)
	assert.Equal(t, 666.0, bumpers[0].VisualStart)
	assert.Equal(t, 704.0, bumpers[0].VisualEnd)
	assert.Equal(t, 3766.0, bumpers[1].VisualStart)
	assert.Equal(t, 3820.0, bumpers[1].VisualEnd)
}

func TestParseBumperJSON_Empty(t *testing.T) {
	input := `[]`
	bumpers, err := parseBumperJSON([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, bumpers)
}

func TestParseKeyframePTS_FindsNearest(t *testing.T) {
	// Keyframes every 5s, looking for nearest before 3819.007
	raw := "3805.600000\n3810.600000\n3815.600000\n3820.600000\n"
	pts, found := parseKeyframePTS(raw, 3819.007)
	require.True(t, found)
	assert.InDelta(t, 3815.6, pts, 0.001)
}

func TestParseKeyframePTS_ExactMatch(t *testing.T) {
	raw := "700.600000\n705.600000\n"
	pts, found := parseKeyframePTS(raw, 700.600)
	require.True(t, found)
	assert.InDelta(t, 700.6, pts, 0.001)
}

func TestParseKeyframePTS_NoKeyframes(t *testing.T) {
	_, found := parseKeyframePTS("", 100.0)
	assert.False(t, found)
}

func TestParseKeyframePTS_AllAfterTimestamp(t *testing.T) {
	raw := "705.600000\n710.600000\n"
	_, found := parseKeyframePTS(raw, 700.0)
	assert.False(t, found)
}

func TestParseKeyframePTS_StripsCSVTrailingComma(t *testing.T) {
	// ffprobe -of csv=p=0 emits each value with a trailing comma even for a
	// single column. Without comma-stripping, every line fails to parse,
	// found stays false, and probeKeyframeBefore silently falls back to
	// timestamp itself — silently producing StartOffset = 0.
	raw := "536.000000,\n538.000000,\n"
	pts, found := parseKeyframePTS(raw, 538.726)
	require.True(t, found)
	assert.InDelta(t, 538.0, pts, 0.001)
}

func TestParseDuration(t *testing.T) {
	got, err := parseDuration("3921.456\n")
	require.NoError(t, err)
	assert.InDelta(t, 3921.456, got, 0.001)
}

func TestParseDuration_Whitespace(t *testing.T) {
	got, err := parseDuration("  1234.5 \n")
	require.NoError(t, err)
	assert.InDelta(t, 1234.5, got, 0.001)
}

func TestParseDuration_Invalid(t *testing.T) {
	_, err := parseDuration("not a number")
	require.Error(t, err)
}
