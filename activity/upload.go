package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"go.temporal.io/sdk/activity"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"github.com/melbournecocoa/decanter/model"
)

type ytCredsFile struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

const (
	youTubeCategoryScienceTech = "28"
	youTubeMaxTitleLength      = 100
	descriptionFooter          = "---\nMelbourne CocoaHeads — a community of iOS & macOS developers in Melbourne, Australia.\nhttps://www.melbournecocoaheads.com/"
)

func (a *Activities) Upload(ctx context.Context, input model.UploadInput) (model.UploadOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Uploading segment", "segmentIndex", input.Video.SegmentIndex)

	defer keepalive(ctx, 30*time.Second)()

	if a.YouTubeCreds == "" {
		return model.UploadOutput{}, fmt.Errorf("DECANTER_YOUTUBE_CREDS_FILE not configured")
	}

	svc, err := newYouTubeService(ctx, a.YouTubeCreds)
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("init youtube service: %w", err)
	}

	wsDir := a.workspaceDir(ctx)
	videoAbs := filepath.Join(wsDir, input.Video.VideoPath)
	srtAbs := filepath.Join(wsDir, input.Video.SubtitlePath)

	// Read metadata.json from disk at upload time so any human edits made
	// during the review_approval gate are picked up — metadata.json is the
	// authoritative human-editable contract right up to the moment of upload.
	metadataPath := filepath.Join(wsDir, filepath.Dir(input.Video.VideoPath), "metadata.json")
	metadata, err := readMetadata(metadataPath)
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("read metadata.json: %w", err)
	}
	logger.Info("Metadata loaded from disk", "path", metadataPath, "title", metadata.Title, "speaker", metadata.Speaker)

	// Event-level metadata lives at the workspace root (not per-segment).
	// Same "read at upload time" rationale as metadata.json — the reviewer may
	// have edited recordingDate at the review_approval gate.
	event, err := readEvent(filepath.Join(wsDir, eventFileName))
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("read event.json: %w", err)
	}
	logger.Info("Event metadata loaded", "recordingDate", event.RecordingDate)

	chapters := BuildChapters(
		metadata.Chapters,
		input.Video.StartOffset,
		input.Video.IntroDuration,
		input.Video.ContentDuration,
		input.Video.XfadeDuration,
		metadata.Title,
	)

	description := buildDescription(metadata, event, chapters)

	title := buildTitle(metadata)
	if err := validateYouTubeTitle(title); err != nil {
		return model.UploadOutput{}, fmt.Errorf("%w — edit metadata.json (title and/or speaker) at %s and retry via UploadOnlyWorkflow", err, metadataPath)
	}

	logger.Info("Opening video file for upload", "path", videoAbs)
	videoFile, err := os.Open(videoAbs)
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("open video: %w", err)
	}
	defer videoFile.Close()

	parts := []string{"snippet", "status"}
	var recordingDetails *youtube.VideoRecordingDetails
	if event.RecordingDate != "" {
		recordingDetails = &youtube.VideoRecordingDetails{RecordingDate: event.RecordingDate}
		parts = append(parts, "recordingDetails")
	}

	videoCall := svc.Videos.Insert(parts, &youtube.Video{
		Snippet: &youtube.VideoSnippet{
			Title:                title,
			Description:          description,
			Tags:                 metadata.Tags,
			CategoryId:           youTubeCategoryScienceTech,
			DefaultLanguage:      "en",
			DefaultAudioLanguage: "en",
		},
		Status: &youtube.VideoStatus{
			PrivacyStatus:           "unlisted",
			SelfDeclaredMadeForKids: false,
		},
		RecordingDetails: recordingDetails,
	}).Media(videoFile, googleapi.ChunkSize(8<<20))
	videoCall = videoCall.ProgressUpdater(func(current, total int64) {
		activity.RecordHeartbeat(ctx, current)
	})

	uploaded, err := videoCall.Do()
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("upload video: %w", err)
	}
	logger.Info("Video uploaded", "videoID", uploaded.Id)
	activity.RecordHeartbeat(ctx, "video uploaded")

	// Custom thumbnail. Reviewer-replaceable on disk; missing is fine
	// (YouTube falls back to its auto-pick). Failures after videos.Insert
	// succeeded are logged but do not fail the activity — captions and
	// playlist still need to run, and re-running Upload would create a
	// duplicate video (MaximumAttempts=1, no idempotency key).
	thumbPath := filepath.Join(wsDir, filepath.Dir(input.Video.VideoPath), "thumbnail.jpg")
	if _, err := os.Stat(thumbPath); err == nil {
		if err := setThumbnail(svc, uploaded.Id, thumbPath); err != nil {
			logger.Warn("Set thumbnail failed — continuing", "path", thumbPath, "error", err)
		} else {
			logger.Info("Thumbnail set", "path", thumbPath)
			activity.RecordHeartbeat(ctx, "thumbnail set")
		}
	} else if os.IsNotExist(err) {
		logger.Info("No thumbnail at path, skipping", "path", thumbPath)
	} else {
		logger.Warn("Stat thumbnail failed, skipping", "path", thumbPath, "error", err)
	}

	// Caption track.
	srtFile, err := os.Open(srtAbs)
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("open srt: %w", err)
	}
	defer srtFile.Close()

	captionCall := svc.Captions.Insert([]string{"snippet"}, &youtube.Caption{
		Snippet: &youtube.CaptionSnippet{
			VideoId:  uploaded.Id,
			Language: "en",
			Name:     "",
			// The struct tag for Name is `json:"name,omitempty"`, so an empty
			// string is dropped from the JSON payload — and YouTube's
			// captions.insert validator then rejects the request as missing
			// snippet.name. ForceSendFields makes the SDK emit "name":"".
			ForceSendFields: []string{"Name"},
		},
	}).Media(srtFile)
	if _, err := captionCall.Do(); err != nil {
		return model.UploadOutput{}, fmt.Errorf("upload caption: %w", err)
	}
	logger.Info("Caption uploaded")
	activity.RecordHeartbeat(ctx, "caption uploaded")

	// Playlist.
	year := time.Now().Year()
	playlistTitle := fmt.Sprintf("%d Presentations", year)
	playlistID, err := findOrCreatePlaylist(ctx, svc, playlistTitle)
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("playlist: %w", err)
	}
	logger.Info("Playlist resolved", "id", playlistID, "title", playlistTitle)

	_, err = svc.PlaylistItems.Insert([]string{"snippet"}, &youtube.PlaylistItem{
		Snippet: &youtube.PlaylistItemSnippet{
			PlaylistId: playlistID,
			ResourceId: &youtube.ResourceId{
				Kind:    "youtube#video",
				VideoId: uploaded.Id,
			},
		},
	}).Do()
	if err != nil {
		return model.UploadOutput{}, fmt.Errorf("add video to playlist: %w", err)
	}
	logger.Info("Video added to playlist")

	return model.UploadOutput{YouTubeVideoID: uploaded.Id}, nil
}

func setThumbnail(svc *youtube.Service, videoID, thumbPath string) error {
	f, err := os.Open(thumbPath)
	if err != nil {
		return fmt.Errorf("open thumbnail: %w", err)
	}
	defer f.Close()
	if _, err := svc.Thumbnails.Set(videoID).Media(f).Do(); err != nil {
		return fmt.Errorf("set thumbnail: %w", err)
	}
	return nil
}

func readMetadata(path string) (model.TalkMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return model.TalkMetadata{}, err
	}
	var m model.TalkMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return model.TalkMetadata{}, fmt.Errorf("parse: %w", err)
	}
	return m, nil
}

func newYouTubeService(ctx context.Context, credsPath string) (*youtube.Service, error) {
	raw, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, fmt.Errorf("read creds: %w", err)
	}
	var c ytCredsFile
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse creds: %w", err)
	}
	config := &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scopes:       []string{youtube.YoutubeForceSslScope},
		Endpoint:     google.Endpoint,
	}
	tok := &oauth2.Token{RefreshToken: c.RefreshToken}
	return youtube.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, tok)))
}

func findOrCreatePlaylist(ctx context.Context, svc *youtube.Service, title string) (string, error) {
	pageToken := ""
	for {
		call := svc.Playlists.List([]string{"snippet"}).Mine(true).MaxResults(50)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return "", fmt.Errorf("list playlists: %w", err)
		}
		for _, pl := range resp.Items {
			if pl.Snippet != nil && pl.Snippet.Title == title {
				return pl.Id, nil
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
		activity.RecordHeartbeat(ctx, "scanning playlists")
	}

	created, err := svc.Playlists.Insert([]string{"snippet", "status"}, &youtube.Playlist{
		Snippet: &youtube.PlaylistSnippet{Title: title},
		Status:  &youtube.PlaylistStatus{PrivacyStatus: "unlisted"},
	}).Do()
	if err != nil {
		return "", fmt.Errorf("create playlist: %w", err)
	}
	return created.Id, nil
}

// buildTitle composes the YouTube video title in the channel's existing
// "Speaker - Title" convention, e.g. "Rob Amos - Forging a Sword Spirit".
// Speaker is omitted when missing.
func buildTitle(metadata model.TalkMetadata) string {
	if metadata.Speaker == "" {
		return metadata.Title
	}
	return fmt.Sprintf("%s - %s", metadata.Speaker, metadata.Title)
}

// validateYouTubeTitle enforces YouTube's 100-character cap on video titles
// before we hand the request to the API — videos.insert rejects over-length
// titles with a misleading "invalid or empty" 400, and the resumable upload
// session that does the rejection happens AFTER the metadata send but is
// indistinguishable from other 400s, so failing fast here keeps the workflow
// out of partial-upload limbo on cancellation.
func validateYouTubeTitle(title string) error {
	n := utf8.RuneCountInString(title)
	if n > youTubeMaxTitleLength {
		return fmt.Errorf("composed YouTube title %q is %d characters, exceeds YouTube's %d-character limit", title, n, youTubeMaxTitleLength)
	}
	return nil
}

func buildDescription(metadata model.TalkMetadata, event model.EventMetadata, chapters []ChapterMarker) string {
	var b strings.Builder

	if metadata.Description != "" {
		b.WriteString(metadata.Description)
		b.WriteString("\n\n")
	}

	if event.EventName != "" {
		fmt.Fprintf(&b, "From %s\n", event.EventName)
		if event.SourceURL != "" {
			b.WriteString(event.SourceURL)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Chapters:\n")
	for _, ch := range chapters {
		fmt.Fprintf(&b, "%s %s\n", formatChapterTime(ch.Time), ch.Title)
	}
	b.WriteString("\n")
	b.WriteString(descriptionFooter)
	return b.String()
}

func formatChapterTime(seconds float64) string {
	total := int64(seconds + 0.5)
	s := total % 60
	total /= 60
	m := total % 60
	h := total / 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
