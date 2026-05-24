package activity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdjustSRT_HappyPath(t *testing.T) {
	in := `1
00:00:01,000 --> 00:00:03,000
First line

2
00:00:05,000 --> 00:00:07,000
Second line
`
	// startOffset=0, introDuration=12, contentDuration=120
	// final_t = whisper_t + 12 (no offset trim)
	out, err := AdjustSRT([]byte(in), 0, 12, 120, 0)
	require.NoError(t, err)

	got := string(out)
	assert.Contains(t, got, "00:00:13,000 --> 00:00:15,000")
	assert.Contains(t, got, "First line")
	assert.Contains(t, got, "00:00:17,000 --> 00:00:19,000")
	assert.Contains(t, got, "Second line")
}

func TestAdjustSRT_StartOffset(t *testing.T) {
	in := `1
00:00:05,000 --> 00:00:07,000
Hello
`
	// startOffset=2 means rough file starts 2s before Segment.Start.
	// final_t = 5 - 2 + 12 = 15
	out, err := AdjustSRT([]byte(in), 2, 12, 120, 0)
	require.NoError(t, err)
	assert.Contains(t, string(out), "00:00:15,000 --> 00:00:17,000")
}

func TestAdjustSRT_DropEntirelyBefore(t *testing.T) {
	in := `1
00:00:00,500 --> 00:00:01,500
Bleed before content

2
00:00:05,000 --> 00:00:07,000
Talk starts
`
	// startOffset=2: anything with whisper_t < 2 is before Segment.Start.
	// Entry 1 (0.5-1.5) is entirely below startOffset → drop.
	// Entry 2 (5-7) → final 15-17 → kept.
	out, err := AdjustSRT([]byte(in), 2, 12, 120, 0)
	require.NoError(t, err)
	got := string(out)
	assert.NotContains(t, got, "Bleed before content")
	assert.Contains(t, got, "Talk starts")
	// Renumbered: surviving entry should be #1, not #2.
	assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "1\n"))
}

func TestAdjustSRT_DropEntirelyAfter(t *testing.T) {
	in := `1
00:00:05,000 --> 00:00:07,000
In content

2
00:01:00,000 --> 00:01:02,000
Bleed after content
`
	// startOffset=0, contentDuration=10 → cutoff at whisper_t=10.
	out, err := AdjustSRT([]byte(in), 0, 12, 10, 0)
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "In content")
	assert.NotContains(t, got, "Bleed after content")
}

func TestAdjustSRT_ClipStraddleStart(t *testing.T) {
	in := `1
00:00:01,000 --> 00:00:05,000
Straddles start
`
	// startOffset=3: entry [1,5] straddles the start (3).
	// Effective: clip start to 3 (whisper_t), shift: final_start = max(introDur, 3-3+12) = 12; final_end = 5-3+12 = 14.
	out, err := AdjustSRT([]byte(in), 3, 12, 120, 0)
	require.NoError(t, err)
	assert.Contains(t, string(out), "00:00:12,000 --> 00:00:14,000")
}

func TestAdjustSRT_ClipStraddleEnd(t *testing.T) {
	in := `1
00:00:08,000 --> 00:00:15,000
Straddles end
`
	// startOffset=0, contentDuration=10 → cutoff at whisper_t=10.
	// final_start = 8 + 12 = 20; final_end = min(introDur+contentDur, 15+12) = min(22, 27) = 22.
	out, err := AdjustSRT([]byte(in), 0, 12, 10, 0)
	require.NoError(t, err)
	assert.Contains(t, string(out), "00:00:20,000 --> 00:00:22,000")
}

func TestAdjustSRT_MultiLineText(t *testing.T) {
	in := `1
00:00:01,000 --> 00:00:03,000
Line one
Line two
Line three
`
	out, err := AdjustSRT([]byte(in), 0, 0, 120, 0)
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "Line one\nLine two\nLine three")
}

func TestAdjustSRT_Renumbering(t *testing.T) {
	in := `1
00:00:00,100 --> 00:00:00,500
Drop

2
00:00:05,000 --> 00:00:07,000
Keep one

3
00:00:08,000 --> 00:00:09,000
Keep two
`
	// startOffset=1: entry 1 (ends at 0.5) drops, entries 2 and 3 stay.
	// Surviving block numbers should be 1 and 2, not 2 and 3.
	out, err := AdjustSRT([]byte(in), 1, 0, 120, 0)
	require.NoError(t, err)
	got := string(out)
	assert.Regexp(t, `(?m)^1\n00:00:04,000 --> 00:00:06,000\nKeep one`, got)
	assert.Regexp(t, `(?m)^2\n00:00:07,000 --> 00:00:08,000\nKeep two`, got)
}

func TestAdjustSRT_XfadeShift(t *testing.T) {
	in := `1
00:00:01,000 --> 00:00:03,000
First line

2
00:00:05,000 --> 00:00:07,000
Second line
`
	// startOffset=0, introDuration=12, contentDuration=120, xfadeDuration=0.3
	// final_t = whisper_t + 12 - 0.3 = whisper_t + 11.7
	out, err := AdjustSRT([]byte(in), 0, 12, 120, 0.3)
	require.NoError(t, err)

	got := string(out)
	assert.Contains(t, got, "00:00:12,700 --> 00:00:14,700")
	assert.Contains(t, got, "00:00:16,700 --> 00:00:18,700")
}

