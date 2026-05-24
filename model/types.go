package model

// PipelineInput is the workflow input. Exactly one of YouTubeURL or
// LocalFileName must be set. LocalFileName references a file inside
// <BasePath>/imports/ — used when a YouTube stream dropped out and we have
// a local recording instead.
//
// RecordingDate is optional and, when supplied, seeds event.json directly
// (bypassing yt-dlp info.json extraction for the YouTube flow). It must be
// RFC3339, e.g. "2026-05-15T19:00:00+10:00" or "2026-05-15T00:00:00Z".
// Intended for automated triggers (UI, scheduler) that already know the
// broadcast date — leaves the review_approval gate as confirmation rather
// than data entry.
type PipelineInput struct {
	YouTubeURL    string
	LocalFileName string
	RecordingDate string
}

// BumperRegion represents the visual boundaries of a detected bumper.
type BumperRegion struct {
	VisualStart float64 // seconds
	VisualEnd   float64 // seconds
}

// Segment represents a split video segment.
// Split produces rough keyframe-aligned copies (-c copy); StartOffset records
// how far the actual cut precedes the intended Start so that Assemble can
// re-cut precisely from the source and shift subtitle timecodes accordingly.
type Segment struct {
	Index       int
	FilePath    string  // relative to workspace
	Start       float64 // seconds in original video (intended start)
	End         float64 // seconds in original video (intended end)
	StartOffset float64 // seconds: Start minus the actual keyframe the rough cut began at
}

// SegmentType classifies a segment.
type SegmentType string

const (
	SegmentTypeWelcome SegmentType = "welcome"
	SegmentTypeTalk    SegmentType = "talk"
	SegmentTypeWrapUp  SegmentType = "wrapup"
)

// ProcessedSegment is the output of a SegmentWorkflow.
type ProcessedSegment struct {
	Segment      Segment
	Type         SegmentType
	SubtitlePath string
	Metadata     TalkMetadata
	Skipped      bool
}

// Chapter is a single chapter marker identified during metadata extraction.
// Time is in seconds, relative to the cleaned-transcript / rough-segment-file
// start (same coordinate system as Whisper SRT timestamps).
type Chapter struct {
	Time  float64 `json:"time"`
	Title string  `json:"title"`
}

// TalkMetadata holds extracted metadata for a talk.
//
// Skip is the reviewer escape hatch: when true, the pipeline filters this
// segment out after the review_approval gate so it is neither assembled nor
// uploaded. Used for MC-between-talks intros and for speakers who withhold
// upload consent. The field is absent from metadata.json by default
// (omitempty) — the reviewer adds it manually during the gate.
type TalkMetadata struct {
	Title       string     `json:"title"`
	Speaker     string     `json:"speaker"`
	Description string     `json:"description"`
	Tags        []string   `json:"tags"`
	Chapters    []Chapter  `json:"chapters"`
	Trim        *TrimRange `json:"trim,omitempty"`
	Skip        bool       `json:"skip,omitempty"`
}

// TrimRange lets the reviewer override where Assemble cuts content out of the
// source. Both values are in rough-cut local time — i.e. seconds from the
// start of the segments/segment-NN.mp4 file the reviewer is watching.
// GatherMetadata pre-populates this with the auto-detected defaults
// (StartSeconds = Segment.StartOffset, EndSeconds = StartOffset + duration)
// so the reviewer has reference numbers to edit from.
type TrimRange struct {
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// EventMetadata holds workspace-level (event-level, not per-talk) metadata.
// Persisted as <wsDir>/event.json. Seeded by Download (from yt-dlp info.json)
// or Import (empty stub), human-editable during the review_approval gate,
// read by Upload at upload time.
type EventMetadata struct {
	// RecordingDate is RFC3339 (e.g. "2026-05-15T19:00:00+10:00" or
	// "2026-05-15T00:00:00Z"). Empty means "not known" — Upload omits
	// recordingDetails entirely.
	RecordingDate string `json:"recordingDate,omitempty"`
	// EventName is the human-readable name of the source event — for YouTube
	// downloads this is the live stream's video title. Rendered in the talk's
	// upload description as "From <EventName>".
	EventName string `json:"eventName,omitempty"`
	// SourceURL is the canonical URL of the original recording (yt-dlp's
	// webpage_url). Rendered on the line below EventName so YouTube
	// auto-links it.
	SourceURL string `json:"sourceURL,omitempty"`
}

// ReviewApproval is the signal payload for human review gates.
type ReviewApproval struct {
	Approved bool
}

// AssembledVideo represents a final assembled video ready for upload.
// IntroDuration / ContentDuration / StartOffset / XfadeDuration are needed by
// Upload to build chapter markers in the final video's time coordinate.
// Metadata is intentionally not carried in-memory through the workflow:
// Upload re-reads metadata.json from disk (keyed by SegmentIndex) so that any
// human edits made during the review_approval gate are picked up regardless
// of when LoadMetadata last ran.
type AssembledVideo struct {
	SegmentIndex    int
	VideoPath       string
	SubtitlePath    string
	IntroDuration   float64
	ContentDuration float64
	StartOffset     float64
	XfadeDuration   float64
}

// --- Activity Input/Output Structs ---

type DownloadInput struct {
	YouTubeURL string
	// RecordingDate, if non-empty, is written verbatim into event.json,
	// bypassing yt-dlp info.json extraction. RFC3339.
	RecordingDate string
}

type DownloadOutput struct {
	VideoPath string
}

type ImportInput struct {
	FileName string
	// RecordingDate, if non-empty, is written verbatim into event.json
	// instead of an empty stub. RFC3339.
	RecordingDate string
}

type ImportOutput struct {
	VideoPath string
}

type DetectBumpersInput struct {
	VideoPath string
}

type DetectBumpersOutput struct {
	Bumpers []BumperRegion
}

type SplitInput struct {
	VideoPath string
	Bumpers   []BumperRegion
}

type SplitOutput struct {
	Segments []Segment
}

type ClassifyInput struct {
	Segment       Segment
	TotalSegments int
}

type ClassifyOutput struct {
	Type SegmentType
}

type TranscribeInput struct {
	Segment Segment
}

type TranscribeOutput struct {
	SubtitlePath string
}

type CleanTranscriptInput struct {
	Segment         Segment
	SubtitlePath    string
	MeetupEventPath string
}

type CleanTranscriptOutput struct {
	SubtitlePath string
}

type GatherMetadataInput struct {
	Segment      Segment
	SubtitlePath string
	// MeetupEventPath is the workspace-relative path to the cached Meetup
	// event JSON written by FetchMeetupEvent. Empty when no Meetup lookup
	// ran (e.g. RecordingDate not supplied). A file containing {} is a
	// deliberate "no match / no agenda" marker — the activity should still
	// surface it to the LLM so the reasoning can record the outcome.
	MeetupEventPath string
}

type GatherMetadataOutput struct {
	Metadata TalkMetadata
}

// FetchMeetupEventInput is intentionally empty — the activity reads
// RecordingDate from <wsDir>/event.json on disk, matching the same
// human-edits-survive-replay pattern Upload uses to re-read metadata.json.
type FetchMeetupEventInput struct{}

type FetchMeetupEventOutput struct {
	// MeetupEventPath is the workspace-relative path the activity wrote
	// (typically "meetup_event.json"), or "" when the lookup was skipped
	// (no RecordingDate available).
	MeetupEventPath string
}

// MeetupEvent mirrors the subset of Meetup's GraphQL Event type we persist.
// Written to <wsDir>/meetup_event.json by FetchMeetupEvent. An empty JSON
// object ({}) means "looked up but no event / no agenda found".
type MeetupEvent struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	DateTime    string `json:"dateTime"`
	EndTime     string `json:"endTime,omitempty"`
	EventURL    string `json:"eventUrl"`
	Description string `json:"description"`
}

// ReadSegmentMetadataInput is the input to the ReadSegmentMetadata activity.
// Segments lists the talk segments whose metadata.json files should be read.
// Each entry's SubtitlePath is workspace-relative; the activity locates
// metadata.json in the same directory.
type ReadSegmentMetadataInput struct {
	Segments []SegmentMetadataRef
}

// SegmentMetadataRef pairs a segment index with the path used to locate its
// on-disk metadata.json file (which sits next to the subtitle).
type SegmentMetadataRef struct {
	Index        int
	SubtitlePath string
}

// ReadSegmentMetadataOutput returns the parsed metadata for each requested
// segment, in the same order as the input.
type ReadSegmentMetadataOutput struct {
	Segments []SegmentMetadata
}

// SegmentMetadata pairs a segment index with the metadata read from its
// metadata.json file.
type SegmentMetadata struct {
	Index    int
	Metadata TalkMetadata
}

type AssembleInput struct {
	Segment      Segment
	SubtitlePath string
}

type AssembleOutput struct {
	Video AssembledVideo
}

type UploadInput struct {
	Video AssembledVideo
}

type UploadOutput struct {
	YouTubeVideoID string
}
