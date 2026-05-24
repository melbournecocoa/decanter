package activity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/melbournecocoa/decanter/model"
)

func TestValidateImportFileName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "talk.mp4", false},
		{"with spaces", "Melbourne CocoaHeads 2026-05-01.mp4", false},
		{"empty", "", true},
		{"forward slash", "subdir/talk.mp4", true},
		{"backslash", "subdir\\talk.mp4", true},
		{"dot-dot traversal", "../etc/passwd", true},
		{"leading dot", ".hidden", true},
		{"just dot", ".", true},
		{"just dot-dot", "..", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImportFileName(tc.input)
			if tc.wantErr {
				assert.Error(t, err, "expected error for %q", tc.input)
			} else {
				assert.NoError(t, err, "unexpected error for %q", tc.input)
			}
		})
	}
}

func TestImport_HappyPath(t *testing.T) {
	basePath := t.TempDir()
	importsDir := filepath.Join(basePath, "imports")
	require.NoError(t, os.MkdirAll(importsDir, 0o755))

	payload := []byte("fake mp4 bytes for testing")
	srcPath := filepath.Join(importsDir, "talk.mp4")
	require.NoError(t, os.WriteFile(srcPath, payload, 0o644))

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New(basePath, "", "", "", "", "", "")
	env.RegisterActivity(a.Import)

	result, err := env.ExecuteActivity(a.Import, model.ImportInput{FileName: "talk.mp4"})
	require.NoError(t, err)

	var output model.ImportOutput
	require.NoError(t, result.Get(&output))

	// Destination should exist with the right contents.
	got, err := os.ReadFile(output.VideoPath)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.Equal(t, "source.mp4", filepath.Base(output.VideoPath))

	// Original should be gone (move semantics).
	_, statErr := os.Stat(srcPath)
	assert.True(t, os.IsNotExist(statErr), "imports file should have been consumed")

	// Empty event.json stub should be seeded at the workspace root.
	ev, err := readEvent(filepath.Join(filepath.Dir(output.VideoPath), "event.json"))
	require.NoError(t, err)
	assert.Equal(t, "", ev.RecordingDate, "Import should seed event.json with empty RecordingDate")
}

func TestImport_HappyPath_WithRecordingDate(t *testing.T) {
	basePath := t.TempDir()
	importsDir := filepath.Join(basePath, "imports")
	require.NoError(t, os.MkdirAll(importsDir, 0o755))

	srcPath := filepath.Join(importsDir, "talk.mp4")
	require.NoError(t, os.WriteFile(srcPath, []byte("fake mp4 bytes"), 0o644))

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()
	a := New(basePath, "", "", "", "", "", "")
	env.RegisterActivity(a.Import)

	recordingDate := "2026-02-19T18:30:00+11:00"
	result, err := env.ExecuteActivity(a.Import, model.ImportInput{
		FileName:      "talk.mp4",
		RecordingDate: recordingDate,
	})
	require.NoError(t, err)

	var output model.ImportOutput
	require.NoError(t, result.Get(&output))

	ev, err := readEvent(filepath.Join(filepath.Dir(output.VideoPath), "event.json"))
	require.NoError(t, err)
	assert.Equal(t, recordingDate, ev.RecordingDate, "Import should propagate RecordingDate into event.json")
}

func TestImport_MissingFile(t *testing.T) {
	basePath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(basePath, "imports"), 0o755))

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New(basePath, "", "", "", "", "", "")
	env.RegisterActivity(a.Import)

	_, err := env.ExecuteActivity(a.Import, model.ImportInput{FileName: "nope.mp4"})
	require.Error(t, err)
}

func TestImport_RejectsTraversal(t *testing.T) {
	basePath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(basePath, "imports"), 0o755))

	// A file outside imports/ that traversal would try to reach.
	outside := filepath.Join(basePath, "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o644))

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New(basePath, "", "", "", "", "", "")
	env.RegisterActivity(a.Import)

	_, err := env.ExecuteActivity(a.Import, model.ImportInput{FileName: "../secret.txt"})
	require.Error(t, err)

	// Outside file must still exist — activity must not have touched it.
	_, statErr := os.Stat(outside)
	assert.NoError(t, statErr)
}
