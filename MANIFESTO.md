# CocoaHeads Decanter

## Overview

Automated pipeline to take a YouTube live stream archive, split it into individual talk segments, generate subtitles, prepend/append intro/outro videos, gather metadata, and upload to YouTube as separate videos.

Built as a **Temporal durable workflow** — starts with manual steps, automate incrementally.

## Architecture

### Temporal Workflow (Go)

Self-hosted Temporal server. Single workflow triggered by YouTube live stream URL.

```
┌──────────────────────────────────────────────────────────┐
│ CocoaHeads Video Pipeline Workflow                        │
│                                                          │
│  1. Download           yt-dlp from YouTube URL           │
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

### Step Details

#### 1. Download (automated)
- **Tool:** yt-dlp
- **Input:** YouTube live stream URL
- **Output:** Video file (highest quality available)
- **Notes:** Live stream archives can take a while to process on YouTube's end — may need retry logic

#### 2. Detect Bumpers (automated) ✅ PROVEN
- **Problem:** Find occurrences of a looping branded animation within the video
- **Duration:** Variable — 20 seconds to ~2 minutes
- **Key fact:** The bumper animation is **silent** — no audio track
- **Approach: Silence detection + perceptual hash verification**
  1. `ffmpeg silencedetect` (noise=-40dB, duration=10s) finds candidate silent regions — instant, audio-only
  2. For each candidate, probe frames at 2-second intervals with dHash (hash_size=16) to find exact visual boundaries
  3. Distance < 30 from reference bumper image = bumper frame, > 30 = content
  4. Silence boundaries and visual boundaries are offset by ~1-2s — always use the **visual** boundaries
- **Tested on 2026-02-13 stream:**
  - Found exactly 2 bumpers (38s and 54s duration)
  - Bumper frame distance: 15-24 (well under threshold)
  - Content frame distance: 45-131 (well above threshold)
  - Zero false positives/negatives
- **Performance:** ~30 seconds total for a 1-hour stream
- **Dependencies:** ffmpeg (silencedetect filter), Python imagehash + Pillow
- **Output:** List of `(visual_start, visual_end)` tuples for each bumper occurrence

#### 3. Split Segments (automated) ✅ PROVEN
- **Tool:** ffmpeg
- **Input:** Original video + bumper visual timestamps
- **Logic:** Split between bumper regions. Each segment = content between end of one bumper and start of the next.
- **Rough cuts only:** Uses `-ss` before `-i` with `-c copy` for fast, lossless keyframe-aligned splitting. Segments may include a few frames to several seconds of bumper at the boundaries depending on keyframe alignment. This is intentional — Split produces working copies for transcription and metadata gathering, not final output.
- **Start offset tracking:** After each split, probes the source for the nearest keyframe before the intended start and records the delta as `StartOffset` on the Segment. This offset is used by Assemble to shift subtitle timecodes.
- **Output:** Individual video files per segment + `StartOffset` per segment

#### 4. Classify Segments (manual → automated)
- **Structure:** Every stream has: welcome → bumper → talk 1 → bumper → talk 2 → ... → bumper → wrap up
- **First segment is always welcome, last segment is always wrap up** — exclude both
- **Everything between bumpers = a talk**
- **Initial:** Human confirms segment classification
- **Future automation:** First = welcome, last = wrap up, middle = talks. Use transcript to verify if needed.
- **Output:** Tagged segment list — only `talk` segments proceed

#### 5. Transcribe + Subtitles (automated)
- **Tool:** Whisper (OpenAI's speech-to-text model)
- **Input:** Talk segment audio
- **Output:** SRT and/or VTT subtitle files per talk
- **Options:**
  - **whisper.cpp** — C++ port, fast on CPU, runs locally on the worker host. Good for offline batch processing.
  - **faster-whisper** — Python, CTranslate2 backend, ~4x faster than original Whisper. GPU optional.
  - **OpenAI Whisper API** — cloud, pay-per-minute, highest accuracy. ~$0.36/hr of audio.
- **Recommendation:** Start with **faster-whisper** (large-v3 model) locally. A modern CPU should handle it — a 50-minute talk takes ~10-15 min to transcribe.
- **Subtitle delivery:**
  - Bake into video (hardcoded) — not recommended, inflexible
  - Upload as separate subtitle track to YouTube — **preferred**, viewers can toggle
  - Both: upload soft subs + provide SRT download in description
- **Language:** English (primary). Whisper auto-detects but can be forced.
- **Quality pass:** Subtitles may need a human review step for technical terms (Swift, Xcode, API names). Could use LLM post-processing to fix common misheard dev terms.
- **Bonus uses:**
  - Transcript feeds into metadata gathering (step 5) — LLM can extract talk title, speaker name, summary
  - Searchable archive of all talks
  - Chapter markers from transcript structure

#### 6. Gather Metadata (LLM-assisted → automated)
- **Input:** Transcript from step 5, plus optionally Meetup event data
- **LLM extracts from transcript:**
  - Talk title (often stated at the start or visible on title slide)
  - Speaker name (usually introduces themselves)
  - Abstract/description (summarise the talk content)
  - Key topics/tags for YouTube
- **Phase 1:** LLM generates draft metadata from transcript → human reviews and corrects
- **Phase 2:** Cross-reference with Meetup API data to validate/enrich
- **Phase 3:** Fully automated with high confidence, human spot-checks only
- **Output:** Per-talk metadata object (title, speaker, description, tags)

#### 7. Human Review — Approval Gate
- Present: segment list with classifications, metadata, subtitles, thumbnails (auto-captured or from title slide)
- Human can: adjust split points, reclassify segments, edit metadata, correct subtitles, reorder
- **UI:** TBD — could be a simple web form, CLI prompts, or even Telegram inline buttons

#### 8. Assemble Final Videos (automated)
- **Tool:** ffmpeg
- **Per talk:**
  1. Sponsor intro video (if applicable)
  2. Title slide (generated — talk title + speaker name, branded template)
  3. Talk content — **re-cut precisely from the original source** using `Segment.Start`/`End` timestamps, not the rough segment file. Single re-encode pass.
  4. Outro/copyright video
- **Why re-cut from source:** Split produces rough keyframe-aligned copies (`-c copy`). Rather than re-encoding twice (once at Split for precision, once at Assemble for compositing), we re-encode once at Assemble and get frame-accurate cuts in the same pass.
- **Subtitles:** Adjust SRT timecodes: subtract `Segment.StartOffset` (to correct for keyframe-aligned rough cut) then add the duration of prepended intro/title slide.
- **Output:** Final MP4 + adjusted SRT per talk, ready for upload

#### 9. Human Review — Final Check
- Review assembled videos (or at least thumbnails + metadata summary)
- Spot-check subtitles
- Approve for upload

#### 10. Upload to YouTube (automated)
- **API:** YouTube Data API v3
- **Per video:** title, description, tags, thumbnail, playlist assignment
- **Subtitles:** Upload SRT/VTT as caption track via YouTube Captions API
- **Playlist:** Auto-add to monthly playlist (e.g. "CocoaHeads February 2026")
- **Privacy:** Upload as unlisted first, human publishes

## Assets Needed

| Asset | Status | Notes |
|-------|--------|-------|
| Bumper reference image | ✅ Exists | dHash reference for detection |
| Sponsor intro video(s) | ✅ Exists | Prepended to each talk |
| Outro/copyright video | ✅ Exists | Appended to each talk |
| Title slide template | ❌ Needed | Branded template, dynamic text overlay |
| YouTube API credentials | ❌ Needed | OAuth for CocoaHeads channel |
| Meetup API credentials | ❌ Needed | For automated metadata (future) |
| Whisper model | ❌ Needed | Download large-v3 for faster-whisper |

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Workflow engine | Temporal (self-hosted) |
| Workers | Go |
| Video processing | ffmpeg, yt-dlp |
| Bumper detection | ffmpeg silencedetect + Python imagehash (dHash) |
| Transcription/subtitles | faster-whisper (large-v3) or whisper.cpp |
| Title slide generation | ffmpeg drawtext or ImageMagick |
| Upload | YouTube Data API v3 (Go client) |
| Human review UI | TBD |

## Incremental Automation Plan

| Step | Phase 1 (MVP) | Phase 2 | Phase 3 |
|------|--------------|---------|---------|
| Download | Automated | Automated | Automated |
| Bumper detection | Automated | Automated | Automated |
| Split | Automated | Automated | Automated |
| Classify | Manual | Heuristic + manual override | Fully automated |
| Metadata | Manual | Meetup API + manual override | Transcript + LLM |
| Subtitles | Automated + manual review | Automated + LLM cleanup | Fully automated |
| Assembly | Automated | Automated | Automated |
| Upload | Automated (unlisted) | Automated (unlisted) | Automated (public with schedule) |

## Proof of Concept Results (2026-02-14)

Tested against the 2026-02-13 CocoaHeads stream (https://www.youtube.com/live/LvQVY9FKhQY).

### Silence Detection
```
ffmpeg -af silencedetect=noise=-40dB:d=10
```
Found 2 silent segments in ~30 seconds:
- Silence 1: 664.6s → 702.6s (38s)
- Silence 2: 3765.0s → 3819.2s (54s)

### Visual Boundary Refinement
Probed frames at 2-second intervals with dHash (hash_size=16, threshold=30):
- Bumper 1 visual: 666s → 704s (audio boundaries ±1.4s offset)
- Bumper 2 visual: 3766s → 3820s (consistent offset)

### Segmentation Result
```
 WELCOME: 00:00 → 11:06  (11m 6s)
    TALK: 11:44 → 62:46  (51m 2s)  ← "Causes of Colour" by Andrew Murphy
 WRAP_UP: 63:40 → 65:21  (1m 41s)
```

### Key Findings
- Silence + dHash combo is fast (~30s) and 100% accurate on this sample
- Audio silence boundaries ≠ visual boundaries — always refine with hashing
- ffmpeg `-ss` before `-i` with `-c copy` is fast but keyframe-aligned (up to one GOP of inaccuracy). Frame-accurate cuts require re-encoding. Split uses keyframe-aligned rough cuts; Assemble re-cuts precisely from source during its single re-encode pass.
- dHash distance: bumper frames 15-24, content frames 45-131 — clean separation at threshold 30

## Open Questions

- [ ] What resolution/format should final videos target? (1080p? 4K?)
- [ ] Is there a CocoaHeads branded font/colour scheme for title slides?
- [ ] Should we generate thumbnails automatically (title slide frame) or manually?
- [ ] Temporal UI — is the built-in Temporal web UI enough for approvals, or do we want something custom?
- [ ] Storage — where do intermediate/final files live? Worker-local? NAS?
- [ ] Whisper model size — large-v3 for accuracy or medium for speed? Benchmark needed.
- [ ] Subtitle format preference — SRT or VTT? (YouTube accepts both)
- [ ] LLM post-processing for subtitles — worth it for fixing technical terms?
