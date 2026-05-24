# CocoaHeads Decanter

Automated pipeline to take a CocoaHeads Melbourne YouTube live stream archive, split it into individual talk segments, generate subtitles, and upload to YouTube as separate videos.

Built as a [Temporal](https://temporal.io) durable workflow — starts with manual steps, automate incrementally.

## How It Works

```
┌──────────────────────────────────────────────────────────┐
│ CocoaHeads Video Pipeline Workflow                       │
│                                                          │
│  1. Acquire            yt-dlp (YouTube) or local import  │
│  2. Detect Bumpers     Silence detection + dHash verify  │
│  3. Split Segments     ffmpeg split on bumper timestamps  │
│  4. Classify           Identify talks vs welcome/closing  │
│  5. Transcribe         Whisper → subtitles (SRT/VTT)     │
│  6. Gather Metadata    Title, speaker, description (LLM) │
│  7. ─── Human Review ────────────────────────────────    │
│  8. Assemble           Intro/title/content/outro concat   │
│  9. ─── Human Review ────────────────────────────────    │
│ 10. Upload             YouTube Data API v3               │
└──────────────────────────────────────────────────────────┘
```

Step 1 has two flavours: `Download` pulls the archive from a YouTube URL, or `Import` ingests a local recording dropped into the workspace's `imports/` directory — useful when a live stream drops out and you only have the local copy.

Each step is a Temporal activity. Steps 4-6 run per segment in parallel via child workflows. Human review gates use Temporal signals — the workflow blocks until a human approves.

## Prerequisites

- Go 1.23+
- [Temporal server](https://docs.temporal.io/self-hosted-guide) (self-hosted)
- ffmpeg, yt-dlp (for video processing activities)
- Python 3.11+ (for bumper detection and the Groq transcription wrapper)
- A [Groq Cloud](https://console.groq.com) API key — transcription runs via Groq's hosted whisper-large-v3

## Getting Started

```bash
# Clone and install dependencies
git clone https://github.com/melbournecocoa/decanter.git
cd decanter
go mod download

# Start the Temporal server (see Temporal docs), then point the worker at it.
# Configure via either shell exports or a .env file in the project root
# (copy .env.example as a starting point). Shell exports override .env.
#
# TEMPORAL_ADDRESS defaults to localhost:7233; set it if your server is elsewhere.
# DECANTER_WORKSPACE_PATH defaults to /tmp/decanter.
# GROQ_API_KEY is required — the Transcribe activity calls Groq's whisper-large-v3.
cp .env.example .env  # then edit
go run ./cmd/worker

# tctl uses its own --address flag (or TEMPORAL_CLI_ADDRESS) to reach the server.

# Trigger a workflow from a YouTube live archive:
tctl workflow start \
  --taskqueue decanter-pipeline \
  --workflow_type PipelineWorkflow \
  --input '{"YouTubeURL":"https://www.youtube.com/live/..."}'

# Or trigger from a local recording (e.g. recovered after a stream dropout).
# Drop the file into the imports/ directory the worker created on startup, then:
cp ~/Recordings/cocoaheads-2026-05.mp4 "$DECANTER_WORKSPACE_PATH/imports/"
tctl workflow start \
  --taskqueue decanter-pipeline \
  --workflow_type PipelineWorkflow \
  --input '{"LocalFileName":"cocoaheads-2026-05.mp4"}'
```

`LocalFileName` must be a plain filename — no path separators, no leading dot. The import is consumed (moved) into the per-run workspace as `source.mp4` and the rest of the pipeline runs identically.

## YouTube Authentication

The Upload activity uses a long-lived refresh token minted by the one-shot helper at `cmd/yt-auth`. Run it whenever you need fresh credentials (Google expires refresh tokens after 7 days while the OAuth consent screen is in "Testing" mode):

```bash
go run ./cmd/yt-auth \
  --client-creds /path/to/client_secret.json \
  --out /path/to/youtube-creds.json
```

Open the printed URL, complete consent, and the helper writes the runtime creds. Then point the worker at it and restart:

```bash
export DECANTER_YOUTUBE_CREDS_FILE=/path/to/youtube-creds.json
```

`client_secret.json` is the Desktop-app OAuth client downloaded from Google Cloud Console. The output creds file contains the refresh token — treat it as a secret.

## Current Status

In production for Melbourne CocoaHeads — all ten pipeline steps are implemented and processing real meetup recordings end-to-end. The workflow handles YouTube live archives or local file imports, detects branded bumpers automatically, transcribes via Groq's whisper-large-v3 (~250× real-time), generates titles, descriptions, and chapter markers via the `claude` CLI, and uploads to YouTube with caption tracks and per-year playlist management.

Two human-review gates (`review_approval`, `upload_approval`) let you edit the LLM's metadata and inspect the final video before publishing.

The pipeline ships with intro/outro/bumper media branded for Melbourne CocoaHeads. If you fork for your own community, replace those assets and point the relevant env vars at your files — see [CONTRIBUTING.md](CONTRIBUTING.md).

## Running Tests

```bash
go test ./... -v
```
