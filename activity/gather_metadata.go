package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

const gatherMetadataPrompt = `You are extracting metadata from a Melbourne CocoaHeads meetup talk transcript (SRT file).

Instructions:
1. Read the SRT file at the path below.
2. If a Meetup event JSON file path is also provided below, read it first. It contains the night's event details, including a "description" field with an agenda listing the talks (typically a markdown-style pipe-delimited table). The agenda is authoritative for speaker name and talk title because the speaker chose what to publish — prefer it over inferring from the transcript when there is a confident match. Notes:
   - Meetup escapes markdown punctuation with backslashes on the wire: strip a leading backslash before any of | - + ( ) . before parsing.
   - The agenda row format is roughly "| TIME | SPEAKER NAME — TALK TITLE |", separator is the em-dash character (U+2014).
   - Speaker names may include a parenthetical nickname, e.g. "Rob Amos (Bok)" — keep it as written.
   - Confident match = the agenda's talk title and/or speaker name corresponds clearly to the transcript's subject matter or self-introduction. If only one talk on the agenda plausibly matches a longer talk transcript, that is enough; if multiple talks could match, fall back rather than guess.
   - If no Meetup file is provided, the file contains "{}", the event has no usable agenda (e.g. "Agenda will be posted once finalised"), or no talk is a confident match, fall back to inferring "title" and "speaker" from the transcript only.
3. Extract metadata and write it as a JSON file to the same directory as the SRT, named "metadata.json".
4. The JSON schema is:
{
  "title": "The talk title",
  "speaker": "The speaker's full name",
  "description": "A 2-3 sentence summary of the talk content",
  "tags": ["relevant", "topic", "tags"],
  "chapters": [
    {"time": 123.4, "title": "Short chapter title"}
  ]
}
   YouTube title length budget: the uploaded video title is composed as "<speaker> - <title>" (e.g. "Rob Amos - Forging a Sword Spirit") and YouTube hard-caps it at 100 characters. Keep the talk title short enough that the combined form fits. Multi-speaker talks (e.g. "April Staines & Nabila Herzegovina") spend 35-40 characters on the prefix alone, so for multiple speakers keep the title under about 55 characters; for a single speaker target around 60 characters for the title.
5. This is an Australian community — write the title, description, tags, and chapter titles in Australian English (colour not color, organise not organize, behaviour not behavior, recognise not recognize, centre not center, analyse not analyze, defence not defense, etc.) regardless of how words appear in the transcript. Exception: if the Meetup agenda's title or speaker name is being used verbatim, preserve its spelling as published.
6. Omit any field you are not confident about. For example, if the speaker never clearly states their name and no agenda entry matches, leave "speaker" out entirely rather than guessing.
7. Tags should be relevant YouTube tags for discoverability — include technology names, frameworks, and broad topics (e.g. "iOS", "Swift", "SwiftUI", "testing", "CocoaHeads", "Melbourne").
8. Chapters identify 3–7 natural section boundaries in the talk:
   - "time" is the start of the section in seconds, taken directly from the SRT timestamps you read.
   - Chapter titles should be short (2–5 words) and descriptive (e.g. "Background", "What we built", "Demo", "Q&A", "Lessons learned").
   - Do NOT produce a generic "Introduction" / "Intro" chapter. The final video opens with a sponsor bumper followed by a hardcoded "Intro" marker at 0:00, so a chapter named Introduction near the start would be visibly redundant. Begin your chapters at the first substantive section transition instead (e.g. the speaker moving from self-intro into background, problem statement, demo, etc.).
   - Chapters are REQUIRED for any talk longer than ~20 minutes. Identify topic shifts even when the speaker doesn't explicitly announce them — moving between distinct projects, demos, frameworks, or subjects all count as chapter boundaries. Use the description you're writing as a guide: if the description enumerates multiple topics, those are your chapters.
   - Omit the "chapters" field entirely only if the talk is genuinely short (under ~15 minutes).
9. Write ONLY valid JSON to the metadata.json file — no markdown, no commentary.
10. Also write a "metadata_reasoning.md" file in the same directory explaining your key decisions: how you chose the title and speaker (and specifically whether you used a Meetup agenda match — name the matched agenda entry; or note "no agenda available" / "no confident match"), which boundaries became chapters and why (or, if applicable, why you omitted chapters), and any judgement calls on tags. Keep it brief — a few short paragraphs or a bulleted list. This file is read by a human reviewer before the video is assembled.
11. When done, reply with just the word "done".

SRT file path: `

func (a *Activities) GatherMetadata(ctx context.Context, input model.GatherMetadataInput) (model.GatherMetadataOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Gathering metadata", "segmentIndex", input.Segment.Index)

	wsDir := a.workspaceDir(ctx)
	srtPath := filepath.Join(wsDir, input.SubtitlePath)
	metadataPath := filepath.Join(filepath.Dir(srtPath), "metadata.json")

	// Background heartbeat ticker for long API calls.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "waiting for Claude response")
			}
		}
	}()

	prompt := gatherMetadataPrompt + srtPath
	if input.MeetupEventPath != "" {
		prompt += "\nMeetup event JSON file path: " + filepath.Join(wsDir, input.MeetupEventPath)
	}
	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--output-format", "text",
		"--model", "sonnet",
		"--no-session-persistence",
		"--allowedTools", "Read,Write",
	)
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	if err := cmd.Run(); err != nil {
		close(done)
		return model.GatherMetadataOutput{}, fmt.Errorf("claude CLI failed: %w", err)
	}
	close(done)

	// Append a fixed reviewer cheat sheet to metadata_reasoning.md. The LLM
	// writes the upper portion (its decisions); this footer documents
	// human-only escape hatches the reviewer can apply at the review gate.
	reasoningPath := filepath.Join(filepath.Dir(metadataPath), "metadata_reasoning.md")
	if err := appendReviewerNotes(reasoningPath); err != nil {
		// Best-effort — don't fail the activity if the file is missing or unwriteable.
		logger.Warn("Failed to append reviewer notes to metadata_reasoning.md", "path", reasoningPath, "error", err)
	}

	// Read the JSON file Claude wrote.
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		return model.GatherMetadataOutput{}, fmt.Errorf("read metadata.json: %w", err)
	}

	var metadata model.TalkMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return model.GatherMetadataOutput{}, fmt.Errorf("parse metadata JSON: %w\ncontent: %s", err, string(raw))
	}

	// Pre-populate trim defaults so the reviewer sees rough-cut-relative
	// reference numbers in metadata.json. Defaults reproduce current behaviour;
	// any edit before review_approval shifts the Assemble cut points.
	if metadata.Trim == nil {
		seg := input.Segment
		metadata.Trim = &model.TrimRange{
			StartSeconds: seg.StartOffset,
			EndSeconds:   seg.StartOffset + (seg.End - seg.Start),
		}
		out, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return model.GatherMetadataOutput{}, fmt.Errorf("marshal metadata with trim defaults: %w", err)
		}
		if err := os.WriteFile(metadataPath, out, 0o644); err != nil {
			return model.GatherMetadataOutput{}, fmt.Errorf("write metadata with trim defaults: %w", err)
		}
	}

	logger.Info("Metadata gathered", "title", metadata.Title, "speaker", metadata.Speaker, "trimStart", metadata.Trim.StartSeconds, "trimEnd", metadata.Trim.EndSeconds)
	return model.GatherMetadataOutput{Metadata: metadata}, nil
}

const reviewerNotesFooter = `

---

## Reviewer escape hatches

These flags are NOT set by the metadata extraction — they're for you to add by hand to ` + "`metadata.json`" + ` before approving the review gate.

- **Skip this talk entirely.** Add ` + "`\"skip\": true`" + ` to ` + "`metadata.json`" + ` to exclude this segment from assembly and upload. Use cases: MC speaker introductions that ended up as a separate segment, or talks where the speaker has withheld consent for individual upload. The pipeline will count this segment as skipped and move on.
- **Adjust trim points.** ` + "`trim.startSeconds`" + ` and ` + "`trim.endSeconds`" + ` are pre-filled with the auto-detected rough-cut boundaries (in rough-segment local time, i.e. seconds from the start of segments/segment-NN.mp4). Edit either to shift where Assemble cuts.
`

// appendReviewerNotes appends the fixed reviewer cheat-sheet footer to the
// reasoning markdown the LLM just wrote. Idempotent across re-runs in the
// same workspace: re-running GatherMetadata for the same segment overwrites
// metadata_reasoning.md from scratch (Claude is told to write it), so the
// footer is appended exactly once per write.
func appendReviewerNotes(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(reviewerNotesFooter); err != nil {
		return err
	}
	return nil
}
