package activity

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/melbournecocoa/decanter/model"
)

// withMeetupEndpoint rebinds the package-level meetupEndpoint var to the test
// server URL for the duration of t, restoring the original on cleanup.
func withMeetupEndpoint(t *testing.T, url string) {
	t.Helper()
	prev := meetupEndpoint
	meetupEndpoint = url
	t.Cleanup(func() { meetupEndpoint = prev })
}

// defaultTestWorkflowID matches the synthetic ID Temporal's
// TestActivityEnvironment hands to activity.GetInfo. workspaceDir(ctx) uses
// that ID to compute <basePath>/<workflowID>, so we seed event.json there.
const defaultTestWorkflowID = "default-test-workflow-id"

// newFetchMeetupActivityEnv constructs an Activities + activity env with a
// workspace at <basePath>/default-test-workflow-id seeded with the given
// event.json contents.
func newFetchMeetupActivityEnv(t *testing.T, ev model.EventMetadata) (*testsuite.TestActivityEnvironment, *Activities, string) {
	t.Helper()

	basePath := t.TempDir()
	wsDir := filepath.Join(basePath, defaultTestWorkflowID)
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	raw, err := json.MarshalIndent(ev, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, eventFileName), raw, 0o644))

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := New(basePath, "", "", "", "", "", "Melbourne-CocoaHeads")
	env.RegisterActivity(a.FetchMeetupEvent)
	return env, a, wsDir
}

// runFetchMeetup invokes the activity and returns its output plus any error.
func runFetchMeetup(t *testing.T, env *testsuite.TestActivityEnvironment, a *Activities) (model.FetchMeetupEventOutput, error) {
	t.Helper()
	val, err := env.ExecuteActivity(a.FetchMeetupEvent, model.FetchMeetupEventInput{})
	if err != nil {
		return model.FetchMeetupEventOutput{}, err
	}
	var out model.FetchMeetupEventOutput
	require.NoError(t, val.Get(&out))
	return out, nil
}

// graphqlEvent is the shape FetchMeetupEvent unmarshals — match the GraphQL
// envelope exactly so we test the same parsing path production uses.
func graphqlEvent(events ...model.MeetupEvent) string {
	type edge struct {
		Node model.MeetupEvent `json:"node"`
	}
	type events_ struct {
		Edges []edge `json:"edges"`
	}
	type group struct {
		Events events_ `json:"events"`
	}
	type data struct {
		Group group `json:"groupByUrlname"`
	}
	type envelope struct {
		Data data `json:"data"`
	}
	edges := make([]edge, len(events))
	for i, ev := range events {
		edges[i] = edge{Node: ev}
	}
	raw, _ := json.Marshal(envelope{Data: data{Group: group{Events: events_{Edges: edges}}}})
	return string(raw)
}

func TestFetchMeetupEvent_HappyPath_SingleMatch(t *testing.T) {
	want := model.MeetupEvent{
		ID:          "314359425",
		Title:       "Melbourne CocoaHeads No. 195",
		DateTime:    "2026-05-14T18:30:00+10:00",
		EndTime:     "2026-05-14T20:30:00+10:00",
		EventURL:    "https://www.meetup.com/melbourne-cocoaheads/events/314359425/",
		Description: "| 6:40pm | Andrew Murphy — The five stages of losing our craft |",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var req meetupGraphQLRequest
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "Melbourne-CocoaHeads", req.Variables["urlname"])
		filter, ok := req.Variables["filter"].(map[string]interface{})
		require.True(t, ok, "filter should be an object")
		after, _ := filter["afterDateTime"].(string)
		before, _ := filter["beforeDateTime"].(string)
		assert.True(t, strings.HasPrefix(after, "2026-05-14T00:00:00"), "after should be start of Melbourne day, got %s", after)
		assert.True(t, strings.HasPrefix(before, "2026-05-14T23:59:59"), "before should be end of Melbourne day, got %s", before)

		_, _ = w.Write([]byte(graphqlEvent(want)))
	}))
	defer srv.Close()
	withMeetupEndpoint(t, srv.URL)

	env, a, wsDir := newFetchMeetupActivityEnv(t, model.EventMetadata{
		RecordingDate: "2026-05-14T18:30:00+10:00",
	})
	out, err := runFetchMeetup(t, env, a)
	require.NoError(t, err)
	assert.Equal(t, meetupEventFileName, out.MeetupEventPath)

	raw, err := os.ReadFile(filepath.Join(wsDir, meetupEventFileName))
	require.NoError(t, err)
	var got model.MeetupEvent
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, want, got)
}

func TestFetchMeetupEvent_SkipsWhenRecordingDateMissing(t *testing.T) {
	// No HTTP server registered — if the activity tries to call out, the
	// canonical endpoint won't be reachable in CI either; this asserts the
	// short-circuit happens before the HTTP layer.
	withMeetupEndpoint(t, "http://127.0.0.1:0")

	env, a, wsDir := newFetchMeetupActivityEnv(t, model.EventMetadata{RecordingDate: ""})
	out, err := runFetchMeetup(t, env, a)
	require.NoError(t, err)
	assert.Equal(t, "", out.MeetupEventPath, "skip should produce empty path")

	_, statErr := os.Stat(filepath.Join(wsDir, meetupEventFileName))
	assert.True(t, os.IsNotExist(statErr), "skip should not create marker file")
}

func TestFetchMeetupEvent_ZeroMatchesWritesMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(graphqlEvent()))
	}))
	defer srv.Close()
	withMeetupEndpoint(t, srv.URL)

	env, a, wsDir := newFetchMeetupActivityEnv(t, model.EventMetadata{
		RecordingDate: "2026-05-14T18:30:00+10:00",
	})
	out, err := runFetchMeetup(t, env, a)
	require.NoError(t, err)
	assert.Equal(t, meetupEventFileName, out.MeetupEventPath)

	raw, err := os.ReadFile(filepath.Join(wsDir, meetupEventFileName))
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(raw))
}

func TestFetchMeetupEvent_MultipleMatchesWritesMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(graphqlEvent(
			model.MeetupEvent{ID: "1", Title: "Presentation Night"},
			model.MeetupEvent{ID: "2", Title: "Drinks Night"},
		)))
	}))
	defer srv.Close()
	withMeetupEndpoint(t, srv.URL)

	env, a, wsDir := newFetchMeetupActivityEnv(t, model.EventMetadata{
		RecordingDate: "2026-05-14T18:30:00+10:00",
	})
	out, err := runFetchMeetup(t, env, a)
	require.NoError(t, err)
	assert.Equal(t, meetupEventFileName, out.MeetupEventPath)

	raw, err := os.ReadFile(filepath.Join(wsDir, meetupEventFileName))
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(raw))
}

func TestFetchMeetupEvent_HTTPErrorHardFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream burning"))
	}))
	defer srv.Close()
	withMeetupEndpoint(t, srv.URL)

	env, a, _ := newFetchMeetupActivityEnv(t, model.EventMetadata{
		RecordingDate: "2026-05-14T18:30:00+10:00",
	})
	_, err := runFetchMeetup(t, env, a)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchMeetupEvent_GraphQLErrorHardFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Validation error (FieldUndefined@..)"}]}`))
	}))
	defer srv.Close()
	withMeetupEndpoint(t, srv.URL)

	env, a, _ := newFetchMeetupActivityEnv(t, model.EventMetadata{
		RecordingDate: "2026-05-14T18:30:00+10:00",
	})
	_, err := runFetchMeetup(t, env, a)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graphql")
}

func TestFetchMeetupEvent_GroupNotFoundWritesMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"groupByUrlname":null}}`))
	}))
	defer srv.Close()
	withMeetupEndpoint(t, srv.URL)

	env, a, wsDir := newFetchMeetupActivityEnv(t, model.EventMetadata{
		RecordingDate: "2026-05-14T18:30:00+10:00",
	})
	out, err := runFetchMeetup(t, env, a)
	require.NoError(t, err)
	assert.Equal(t, meetupEventFileName, out.MeetupEventPath)

	raw, err := os.ReadFile(filepath.Join(wsDir, meetupEventFileName))
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(raw))
}

func TestMelbourneDayWindow(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "evening in AEST converts cleanly",
			input:     "2026-05-14T18:30:00+10:00",
			wantStart: "2026-05-14T00:00:00+10:00",
			wantEnd:   "2026-05-14T23:59:59+10:00",
		},
		{
			name:      "UTC late-night still lands on Melbourne day",
			input:     "2026-05-14T22:00:00Z",
			wantStart: "2026-05-15T00:00:00+10:00",
			wantEnd:   "2026-05-15T23:59:59+10:00",
		},
		{
			name:      "AEDT (summer) date keeps +11 offset",
			input:     "2026-02-19T19:00:00+11:00",
			wantStart: "2026-02-19T00:00:00+11:00",
			wantEnd:   "2026-02-19T23:59:59+11:00",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := melbourneDayWindow(tc.input)
			require.NoError(t, err)
			// Compare with Format-trimmed nanos (the window function adds 999ms
			// to the end — the second-precision RFC3339 string elides it).
			assert.Equal(t, tc.wantStart, start)
			assert.True(t, strings.HasPrefix(end, tc.wantEnd), "end was %s", end)
		})
	}
}

func TestMelbourneDayWindow_RejectsBadInput(t *testing.T) {
	_, _, err := melbourneDayWindow("not a date")
	require.Error(t, err)
}
