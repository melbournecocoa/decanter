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
  "trim": {"startSeconds": 28.0, "endSeconds": 1882.8},
  "chapters": [
    {"time": 123.4, "title": "Short chapter title"}
  ]
  // optional: "skip": true — see the MC-handoff rule below; omit this field entirely in the normal case.
}
   YouTube title length budget: the uploaded video title is composed as "<speaker> - <title>" (e.g. "Rob Amos - Forging a Sword Spirit") and YouTube hard-caps it at 100 characters. Keep the talk title short enough that the combined form fits. Multi-speaker talks (e.g. "April Staines & Nabila Herzegovina") spend 35-40 characters on the prefix alone, so for multiple speakers keep the title under about 55 characters; for a single speaker target around 60 characters for the title.
5. This is an Australian community — write the title, description, tags, and chapter titles in Australian English (colour not color, organise not organize, behaviour not behavior, recognise not recognize, centre not center, analyse not analyze, defence not defense, etc.) regardless of how words appear in the transcript. Exception: if the Meetup agenda's title or speaker name is being used verbatim, preserve its spelling as published.
6. Omit any field you are not confident about. For example, if the speaker never clearly states their name and no agenda entry matches, leave "speaker" out entirely rather than guessing.
7. Tags should be relevant YouTube tags for discoverability — include technology names, frameworks, and broad topics (e.g. "iOS", "Swift", "SwiftUI", "testing", "CocoaHeads", "Melbourne").
8. **Detecting MC-handoff segments.** Occasionally a segment is not actually a talk — it's the host briefly introducing the next speaker and handing over. Typical signals: short total duration (often well under 5 minutes); the speaker is announcing someone else by name; phrases like "please welcome", "give a warm round of applause", "without further ado", "I'd like to introduce", "next up we have"; the named talk title is something the speaker is *about to* give rather than *giving*; no substantive content of the speaker's own. The Meetup agenda is a useful cross-check — if the segment introduces a talk that appears on the agenda but the segment itself contains none of that talk's content, that's a handoff. When you are confident this is what the segment is, set "skip": true in metadata.json. Still fill in title, speaker, description as best you can from the transcript so the reviewer can audit your decision and flip skip back to false if they disagree. In metadata_reasoning.md, lead with a clearly-marked section (heading "## Skip Decision") explaining what the segment actually contains, which signals triggered the skip, and which talk on the agenda the hand-off was *to*. **Be conservative** — short, dense, or oddly-structured talks are still talks; only skip when the segment is *clearly* introduction-only. When in doubt, do NOT skip; the human reviewer can still flag it manually.
9. Chapters identify 3–7 natural section boundaries in the talk:
   - "time" is the start of the section in seconds, taken directly from the SRT timestamps you read.
   - Chapter titles should be short (2–5 words) and descriptive (e.g. "Background", "What we built", "Demo", "Q&A", "Lessons learned").
   - Do NOT produce a generic "Introduction" / "Intro" chapter. The final video opens with a sponsor bumper followed by a hardcoded "Intro" marker at 0:00, so a chapter named Introduction near the start would be visibly redundant. Begin your chapters at the first substantive section transition instead (e.g. the speaker moving from self-intro into background, problem statement, demo, etc.).
   - Chapters are REQUIRED for any talk longer than ~20 minutes. Identify topic shifts even when the speaker doesn't explicitly announce them — moving between distinct projects, demos, frameworks, or subjects all count as chapter boundaries. Use the description you're writing as a guide: if the description enumerates multiple topics, those are your chapters.
   - Omit the "chapters" field entirely only if the talk is genuinely short (under ~15 minutes).
   - All chapter "time" values must fall within the trim window you set in "trim" (see the next step) — i.e. between "startSeconds" and "endSeconds". Never place a chapter before "startSeconds".
   - If you are setting "skip": true, chapters are not required — the segment will not be assembled or uploaded.
10. **Trim boundaries — when the presentation actually starts and ends.** The SRT was transcribed from a rough-cut segment file that includes extra material at each end: an opening sponsor bumper, setup chatter, possibly an MC introducing this speaker, and at the tail an MC introducing the *next* speaker, the next speaker's audio bleeding in, or trailing dead air / silence (sometimes the speaker just unplugs and walks off before the closing bumper fires). Set "trim" so it spans only the actual presentation. Both values are in seconds, taken directly from the SRT timestamps you read:
   - "startSeconds" = the start timestamp of the speaker's first words *to the audience* — their greeting or first substantive sentence (e.g. "Hi everyone, thanks for coming", "So tonight I want to talk about ..."). Everything before it is trimmed: bumper-bleed text (meme intros, a raffle or giveaway carried over from the previous segment), pure logistics ("are we good to go?", mic checks), and any incoming MC handoff ("take it away Ben", "please welcome ..."). A speaker greeting or thanking the audience IS the start — that is not faff.
   - "endSeconds" = the end timestamp of the speaker's last real content. This INCLUDES any audience Q&A the speaker takes — Q&A is part of the talk and must NOT be cut. Everything after the natural end of the presentation is trimmed: an MC handing off to the next speaker, the next speaker bleeding in, and trailing dead air or silence.
   - Cut tight when you are confident. When a boundary is genuinely ambiguous — the speaker false-starts and restarts, rambles through preamble before truly beginning, or the Q&A blurs into the MC taking over — still set your best-guess value, but flag it in the "## Trim Decision" section (see below) so the human reviewer knows to check it.
11. Write ONLY valid JSON to the metadata.json file — no markdown, no commentary.
12. Also write a "metadata_reasoning.md" file in the same directory explaining your key decisions: how you chose the title and speaker (and specifically whether you used a Meetup agenda match — name the matched agenda entry; or note "no agenda available" / "no confident match"), which boundaries became chapters and why (or, if applicable, why you omitted chapters), and any judgement calls on tags. Include a "## Trim Decision" section recording the "startSeconds" and "endSeconds" you chose, the verbatim SRT line you cut on at each boundary, and a confidence call (high or low); if low, say what made the boundary ambiguous so the reviewer knows to check it. Keep the whole file brief — a few short paragraphs or a bulleted list. This file is read by a human reviewer before the video is assembled. If you set "skip": true above, begin this file with the "## Skip Decision" section described in step 8 before any other reasoning (in that case omit the "## Trim Decision" section — the segment will not be assembled).
13. When done, reply with just the word "done".

SRT file path: `

// buildGatherMetadataPrompt assembles the prompt sent to the claude CLI: the
// fixed instruction block followed by the SRT path, and the Meetup event path
// when one is available. Extracted so the trim-accuracy validation test runs
// the exact prompt production uses.
func buildGatherMetadataPrompt(srtPath, meetupPath string) string {
	prompt := gatherMetadataPrompt + srtPath
	if meetupPath != "" {
		prompt += "\nMeetup event JSON file path: " + meetupPath
	}
	return prompt
}

// gatherMetadataCommand builds the claude CLI invocation for metadata
// extraction. Extracted so the validation test invokes the model identically to
// production (same model, flags, and environment scrubbing).
func gatherMetadataCommand(ctx context.Context, prompt string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--output-format", "text",
		"--model", "sonnet",
		"--no-session-persistence",
		"--allowedTools", "Read,Write",
	)
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	return cmd
}

// applyTrimFallback fills metadata.Trim with the mechanical rough-cut default
// when the model did not propose a trim (Trim == nil). The default spans the
// whole rough segment (StartSeconds = StartOffset, EndSeconds = StartOffset +
// segment duration), reproducing the behaviour before transcript-driven trim
// detection. Returns true if it modified metadata (caller must persist), false
// when a model-proposed trim is already present and must be preserved verbatim.
func applyTrimFallback(metadata *model.TalkMetadata, seg model.Segment) bool {
	if metadata.Trim != nil {
		return false
	}
	metadata.Trim = &model.TrimRange{
		StartSeconds: seg.StartOffset,
		EndSeconds:   seg.StartOffset + (seg.End - seg.Start),
	}
	return true
}

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

	meetupPath := ""
	if input.MeetupEventPath != "" {
		meetupPath = filepath.Join(wsDir, input.MeetupEventPath)
	}
	prompt := buildGatherMetadataPrompt(srtPath, meetupPath)
	cmd := gatherMetadataCommand(ctx, prompt)

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

	// The prompt asks the model to set trim to the detected presentation
	// boundaries. If it omitted trim, fall back to the mechanical rough-cut
	// default (reproduces pre-trim-detection behaviour). When the model DID set
	// trim, applyTrimFallback leaves it untouched so we never clobber the
	// proposal, and the file already on disk is authoritative — no rewrite.
	if applyTrimFallback(&metadata, input.Segment) {
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

Before approving the review gate, edit ` + "`metadata.json`" + ` to apply either of the following.

- **Skip this talk entirely.** Set ` + "`\"skip\": true`" + ` in ` + "`metadata.json`" + ` to exclude this segment from assembly and upload. The metadata extraction may have already done this for you — if it identified the segment as an MC handoff to the next speaker, you'll find ` + "`\"skip\": true`" + ` in ` + "`metadata.json`" + ` and a ` + "`## Skip Decision`" + ` section at the top of this file explaining why. Audit the decision (cross-check the SRT against the agenda) and flip it back to ` + "`false`" + ` if you disagree. Set ` + "`skip`" + ` yourself for the other case the LLM won't catch: a speaker who has withheld consent for individual upload. The pipeline counts skipped segments and moves on.
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
