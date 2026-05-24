# Decanter

Automated pipeline to split CocoaHeads Melbourne YouTube live stream archives into individual talk videos, generate subtitles, and upload to YouTube. Built as a Temporal durable workflow in Go.

## Quick Reference

- **Module:** `github.com/melbournecocoa/decanter`
- **Task queue:** `decanter-pipeline`
- **Build:** `go build ./...`
- **Test:** `go test ./...`
- **Run worker:** `go run ./cmd/worker` (requires Temporal server)
- **Trigger workflow (YouTube):** `tctl workflow start --taskqueue decanter-pipeline --workflow_type PipelineWorkflow --input '{"YouTubeURL":"https://..."}'`
- **Trigger workflow (local file):** drop the file into `$DECANTER_WORKSPACE_PATH/imports/<name>.mp4`, then `tctl workflow start --taskqueue decanter-pipeline --workflow_type PipelineWorkflow --input '{"LocalFileName":"<name>.mp4"}'`

## Architecture

Two Temporal workflows:

- **PipelineWorkflow** (`workflow/pipeline.go`) — top-level orchestrator. Downloads video, detects bumpers, splits segments, fans out to child workflows, blocks on signal gates for human review, assembles final videos, uploads.
- **SegmentWorkflow** (`workflow/segment.go`) — child workflow per segment. Classifies, transcribes (if talk), cleans transcript, gathers metadata.

Human review gates use Temporal Signals: `review_approval` and `upload_approval`.

## Project Layout

```
cmd/worker/main.go       # Temporal worker entry point
workflow/pipeline.go      # PipelineWorkflow (top-level)
workflow/segment.go       # SegmentWorkflow (child, per segment)
workflow/*_test.go        # Workflow tests (Temporal test framework)
activity/activities.go    # Activities struct + constructor
activity/*.go             # One file per activity
model/types.go            # All shared types and activity I/O structs
scripts/                  # Python scripts (bumper detection, transcription)
```

## Pipeline Steps (Activity Mapping)

| Step | Activity | Status |
|------|----------|--------|
| 1. Download | `Download` | Implemented (yt-dlp) |
| 1b. Import (local) | `Import` | Implemented (alt to Download — moves from `<workspace>/imports/`) |
| 2. Detect Bumpers | `DetectBumpers` | Implemented (Python silence+dHash) |
| 3. Split Segments | `Split` | Implemented (ffmpeg, rough keyframe-aligned cuts + StartOffset tracking) |
| 4. Classify | `Classify` | Implemented (positional heuristic) |
| 5. Transcribe | `Transcribe` | Implemented (Groq API, whisper-large-v3) |
| 5b. Clean Transcript | `CleanTranscript` | Implemented (claude CLI) |
| 6. Gather Metadata | `GatherMetadata` | Implemented (claude CLI) |
| 7. Human Review | Signal gate (`review_approval`) | Working |
| 7b. Apply reviewer skip-flags | `ReadSegmentMetadata` | Implemented (filters out talks where the reviewer set `"skip": true` in metadata.json) |
| 8. Assemble | `Assemble` | Implemented (ffmpeg filter_complex, re-cuts from source) |
| 9. Human Review | Signal gate (`upload_approval`) | Working |
| 10. Upload | `Upload` | Implemented (YouTube Data API v3 + captions + playlist) |

## Environment Variables

- `TEMPORAL_ADDRESS` — Temporal server address (default: `localhost:7233`)
- `DECANTER_WORKSPACE_PATH` — Base path for video file storage (default: `/tmp/decanter`, designed for NAS mount). The worker creates `<path>/imports/` on startup as the drop zone for local-file workflow inputs.
- `DECANTER_BUMPER_REF_IMAGE` — Path to bumper reference image for dHash detection (default: `assets/bumper_reference.png`)
- `DECANTER_SCRIPT_DIR` — Path to Python scripts directory (default: `scripts`)
- `GROQ_API_KEY` — **required**. Groq Cloud API key for the Transcribe activity (whisper-large-v3). Worker fails fast at startup if unset.

## Key Design Decisions

- Activities pass **file paths** (not contents) via a configurable workspace directory
- One activity per pipeline step for independent stub-to-real swaps
- Fan-out pattern: child workflows for per-segment processing, activity futures for parallel assembly/upload
- Python tools (bumper detection, transcription) called as subprocesses from Go activities
- **Split produces rough cuts, Assemble re-cuts precisely:** Split uses `-c copy` (fast, lossless, keyframe-aligned). Segments may include bumper bleed at boundaries — acceptable for transcription/metadata. `Segment.StartOffset` records the keyframe alignment delta. Assemble re-cuts from the original source during its single re-encode pass, using StartOffset to shift subtitle timecodes.
