package activity

import "strings"

// Activities holds shared config and dependencies for all pipeline activities.
type Activities struct {
	BasePath       string // workspace root, e.g. "/mnt/nas/decanter/runs"
	BumperRefImage string // absolute path to bumper reference image for dHash
	ScriptDir      string // absolute path to scripts directory

	// YouTubeCreds is the absolute path to the OAuth creds JSON
	// ({client_id, client_secret, refresh_token}). Required for Upload;
	// other activities ignore it.
	YouTubeCreds string

	// IntroVideoPath and OutroVideoPath are absolute paths to the static
	// intro/outro clips concatenated to each talk during Assemble.
	IntroVideoPath string
	OutroVideoPath string

	// MeetupGroupURLName is the Meetup group's URL slug (e.g. "Melbourne-CocoaHeads").
	// Used by FetchMeetupEvent to query the anonymous GraphQL endpoint.
	MeetupGroupURLName string

	// transcribeSem caps concurrent Transcribe activities. Whisper large-v3
	// is RAM-heavy (~10 GB); running multiple in parallel can OOM the worker
	// host (macOS jetsam SIGKILLs the offending python3 processes). Buffer
	// size = max concurrent transcriptions.
	transcribeSem chan struct{}
}

// New creates a new Activities instance.
func New(basePath, bumperRefImage, scriptDir, youTubeCreds, introVideoPath, outroVideoPath, meetupGroupURLName string) *Activities {
	return &Activities{
		BasePath:           basePath,
		BumperRefImage:     bumperRefImage,
		ScriptDir:          scriptDir,
		YouTubeCreds:       youTubeCreds,
		IntroVideoPath:     introVideoPath,
		OutroVideoPath:     outroVideoPath,
		MeetupGroupURLName: meetupGroupURLName,
		transcribeSem:      make(chan struct{}, 1),
	}
}

// filterEnv returns a copy of environ with the named variable removed.
func filterEnv(environ []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(environ))
	for _, e := range environ {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
