package activity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadEventFromYtdlp_ReleaseTimestamp(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "source.mp4.info.json")
	// 2026-05-15T09:00:00Z = epoch 1778835600
	require.NoError(t, os.WriteFile(infoPath, []byte(`{"release_timestamp": 1778835600, "release_date": "20260515", "upload_date": "20260516"}`), 0o644))

	ev, err := readEventFromYtdlp(infoPath)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15T09:00:00Z", ev.RecordingDate)
}

func TestReadEventFromYtdlp_ReleaseDateFallback(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "source.mp4.info.json")
	require.NoError(t, os.WriteFile(infoPath, []byte(`{"release_date": "20260515", "upload_date": "20260516"}`), 0o644))

	ev, err := readEventFromYtdlp(infoPath)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15T00:00:00Z", ev.RecordingDate)
}

func TestReadEventFromYtdlp_UploadDateFallback(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "source.mp4.info.json")
	require.NoError(t, os.WriteFile(infoPath, []byte(`{"upload_date": "20260516"}`), 0o644))

	ev, err := readEventFromYtdlp(infoPath)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-16T00:00:00Z", ev.RecordingDate)
}

func TestReadEventFromYtdlp_MissingInfoJSON(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "does-not-exist.info.json")

	ev, err := readEventFromYtdlp(infoPath)
	require.NoError(t, err)
	assert.Equal(t, "", ev.RecordingDate)
	assert.Equal(t, "", ev.EventName)
	assert.Equal(t, "", ev.SourceURL)
}

func TestReadEventFromYtdlp_NoDateFields(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "source.mp4.info.json")
	require.NoError(t, os.WriteFile(infoPath, []byte(`{"title": "some video"}`), 0o644))

	ev, err := readEventFromYtdlp(infoPath)
	require.NoError(t, err)
	assert.Equal(t, "", ev.RecordingDate)
	assert.Equal(t, "some video", ev.EventName)
}

func TestReadEventFromYtdlp_MalformedJSON(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "source.mp4.info.json")
	require.NoError(t, os.WriteFile(infoPath, []byte(`{not json`), 0o644))

	_, err := readEventFromYtdlp(infoPath)
	require.Error(t, err)
}

func TestReadEventFromYtdlp_TitleAndWebpageURL(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "source.mp4.info.json")
	require.NoError(t, os.WriteFile(infoPath, []byte(`{
		"release_timestamp": 1778835600,
		"title": "Melbourne CocoaHeads — April 2026",
		"webpage_url": "https://www.youtube.com/watch?v=abc123"
	}`), 0o644))

	ev, err := readEventFromYtdlp(infoPath)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15T09:00:00Z", ev.RecordingDate)
	assert.Equal(t, "Melbourne CocoaHeads — April 2026", ev.EventName)
	assert.Equal(t, "https://www.youtube.com/watch?v=abc123", ev.SourceURL)
}

func TestFormatYtdlpDate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"20260515", "2026-05-15T00:00:00Z"},
		{"19991231", "1999-12-31T00:00:00Z"},
		{"", ""},
		{"2026-05-15", ""},
		{"abcdefgh", ""},
		{"2026051", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, formatYtdlpDate(tc.in))
		})
	}
}
