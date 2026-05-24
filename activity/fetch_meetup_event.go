package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

// meetupEndpoint is the Meetup GraphQL endpoint. Overridden in tests via the
// package-internal name; the canonical path is /gql-ext (the legacy /gql 404s).
var meetupEndpoint = "https://api.meetup.com/gql-ext"

// meetupHTTPTimeout bounds each Meetup HTTP call. The API itself is fast
// (~300ms) — the timeout exists to keep the activity heartbeating cleanly
// rather than hanging on a stuck TCP connection.
const meetupHTTPTimeout = 30 * time.Second

// meetupEventFileName is the filename of the cached event JSON at the
// workspace root. {} is a deliberate "looked up but no match" marker;
// GatherMetadata treats that as "no agenda available, fall back to LLM-only".
const meetupEventFileName = "meetup_event.json"

// melbourneTZ is the timezone events are scoped to for same-calendar-day
// matching. CocoaHeads recordings are produced in Melbourne and any
// RecordingDate the operator supplies is implicitly local-of-record.
const melbourneTZ = "Australia/Melbourne"

// meetupGraphQLQuery requests the fields we persist. Kept verbatim as a
// constant so the wire payload is greppable.
const meetupGraphQLQuery = `query($urlname: String!, $filter: GroupEventFilter!) {
  groupByUrlname(urlname: $urlname) {
    events(first: 20, status: PAST, filter: $filter) {
      edges { node { id title dateTime endTime eventUrl description } }
    }
  }
}`

type meetupGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type meetupGraphQLError struct {
	Message string `json:"message"`
}

type meetupGraphQLResponse struct {
	Data struct {
		Group *struct {
			Events struct {
				Edges []struct {
					Node model.MeetupEvent `json:"node"`
				} `json:"edges"`
			} `json:"events"`
		} `json:"groupByUrlname"`
	} `json:"data"`
	Errors []meetupGraphQLError `json:"errors,omitempty"`
}

// FetchMeetupEvent looks up the Meetup event matching the workspace's
// RecordingDate and persists it to <wsDir>/meetup_event.json for
// GatherMetadata to consume.
//
// Failure mode (decided in plan):
//   - RecordingDate not set on event.json → soft-skip (return empty path, no file written).
//   - Meetup HTTP/transport error or non-2xx → hard error (Temporal retries).
//   - GraphQL response carries `errors[]` → hard error.
//   - 0 matching events or >1 matching events → soft-fail: write `{}` marker
//     so GatherMetadata sees "looked up, nothing useful".
//   - Exactly 1 match → write the full event payload.
func (a *Activities) FetchMeetupEvent(ctx context.Context, _ model.FetchMeetupEventInput) (model.FetchMeetupEventOutput, error) {
	logger := activity.GetLogger(ctx)

	wsDir := a.workspaceDir(ctx)
	ev, err := readEvent(filepath.Join(wsDir, eventFileName))
	if err != nil {
		return model.FetchMeetupEventOutput{}, fmt.Errorf("read event.json: %w", err)
	}
	if ev.RecordingDate == "" {
		logger.Info("No RecordingDate on event.json — skipping Meetup lookup")
		return model.FetchMeetupEventOutput{MeetupEventPath: ""}, nil
	}

	dayStart, dayEnd, err := melbourneDayWindow(ev.RecordingDate)
	if err != nil {
		return model.FetchMeetupEventOutput{}, fmt.Errorf("compute day window: %w", err)
	}

	body, err := json.Marshal(meetupGraphQLRequest{
		Query: meetupGraphQLQuery,
		Variables: map[string]interface{}{
			"urlname": a.MeetupGroupURLName,
			"filter": map[string]string{
				"afterDateTime":  dayStart,
				"beforeDateTime": dayEnd,
			},
		},
	})
	if err != nil {
		return model.FetchMeetupEventOutput{}, fmt.Errorf("marshal request: %w", err)
	}

	// Background heartbeat ticker — the HTTP timeout is short, but the
	// activity HeartbeatTimeout is 2 min and we want to keep it green.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "waiting for Meetup API")
			}
		}
	}()
	defer close(done)

	logger.Info("Querying Meetup", "urlname", a.MeetupGroupURLName, "after", dayStart, "before", dayEnd)
	respBody, err := doMeetupRequest(ctx, body)
	if err != nil {
		return model.FetchMeetupEventOutput{}, err
	}

	var parsed meetupGraphQLResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return model.FetchMeetupEventOutput{}, fmt.Errorf("parse response: %w\nbody: %s", err, truncate(respBody, 500))
	}
	if len(parsed.Errors) > 0 {
		return model.FetchMeetupEventOutput{}, fmt.Errorf("meetup graphql errors: %s", parsed.Errors[0].Message)
	}

	outPath := filepath.Join(wsDir, meetupEventFileName)
	if parsed.Data.Group == nil {
		logger.Warn("Group not found", "urlname", a.MeetupGroupURLName)
		if err := writeMeetupMarker(outPath); err != nil {
			return model.FetchMeetupEventOutput{}, err
		}
		return model.FetchMeetupEventOutput{MeetupEventPath: meetupEventFileName}, nil
	}

	edges := parsed.Data.Group.Events.Edges
	switch len(edges) {
	case 0:
		logger.Warn("No PAST events on recording date", "date", ev.RecordingDate)
		if err := writeMeetupMarker(outPath); err != nil {
			return model.FetchMeetupEventOutput{}, err
		}
		return model.FetchMeetupEventOutput{MeetupEventPath: meetupEventFileName}, nil
	case 1:
		event := edges[0].Node
		if err := writeMeetupEvent(outPath, event); err != nil {
			return model.FetchMeetupEventOutput{}, err
		}
		logger.Info("Meetup event matched", "title", event.Title, "url", event.EventURL)
		return model.FetchMeetupEventOutput{MeetupEventPath: meetupEventFileName}, nil
	default:
		titles := make([]string, 0, len(edges))
		for _, e := range edges {
			titles = append(titles, e.Node.Title)
		}
		logger.Warn("Multiple PAST events on recording date — refusing to guess",
			"date", ev.RecordingDate, "candidates", titles)
		if err := writeMeetupMarker(outPath); err != nil {
			return model.FetchMeetupEventOutput{}, err
		}
		return model.FetchMeetupEventOutput{MeetupEventPath: meetupEventFileName}, nil
	}
}

// doMeetupRequest POSTs the GraphQL body and returns the response body on a
// 2xx, or an error on any non-2xx or transport failure.
func doMeetupRequest(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meetupEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "decanter-meetup/0.1")

	client := &http.Client{Timeout: meetupHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meetup request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meetup HTTP %d: %s", resp.StatusCode, truncate(respBody, 500))
	}
	return respBody, nil
}

// melbourneDayWindow takes an RFC3339 timestamp and returns the start and end
// of the same calendar day in Australia/Melbourne, both as RFC3339 strings
// with explicit local offsets (what the Meetup filter wants).
func melbourneDayWindow(rfc3339 string) (string, string, error) {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return "", "", fmt.Errorf("parse RecordingDate: %w", err)
	}
	loc, err := time.LoadLocation(melbourneTZ)
	if err != nil {
		return "", "", fmt.Errorf("load %s: %w", melbourneTZ, err)
	}
	local := t.In(loc)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	dayEnd := time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 59, int(999*time.Millisecond), loc)
	return dayStart.Format(time.RFC3339), dayEnd.Format(time.RFC3339), nil
}

// writeMeetupEvent persists a matched event as pretty-printed JSON for the
// human reviewer at the review_approval gate.
func writeMeetupEvent(path string, event model.MeetupEvent) error {
	raw, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meetup event: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write meetup_event.json: %w", err)
	}
	return nil
}

// writeMeetupMarker writes an empty-object marker file. GatherMetadata reads
// {} as the explicit signal "no agenda available, use LLM-only metadata".
func writeMeetupMarker(path string) error {
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		return fmt.Errorf("write meetup_event.json marker: %w", err)
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
