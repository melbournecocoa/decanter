package activity

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

const cleanTranscriptPrompt = `You are cleaning up an SRT subtitle file from a Melbourne CocoaHeads meetup talk (iOS/macOS development community). Fix technical terminology that was misheard by speech-to-text.

Common corrections include:
- "x code" or "ex code" → "Xcode"
- "swift u i" or "swift UI" → "SwiftUI"
- "ui kit" or "you eye kit" → "UIKit"
- "app kit" → "AppKit"
- "eye phone" or "i phone" → "iPhone"
- "eye pad" → "iPad"
- "objective see" or "objective c" → "Objective-C"
- "cocoa pods" → "CocoaPods"
- "x c test" → "XCTest"
- "mac oh es" or "mac OS" → "macOS"
- "eye oh es" or "iOS" → "iOS"
- "watch oh es" → "watchOS"
- "tv oh es" → "tvOS"

Instructions:
1. Read the SRT file at the path below.
2. Work through the file and use the Edit tool to fix any misheard technical terms in place. Only fix terminology and spelling — do not rephrase or restructure sentences.
3. This is an Australian community — normalise any American English spellings Whisper has produced to Australian English (colour not color, organise not organize, behaviour not behavior, recognise not recognize, centre not center, analyse not analyze, defence not defense, etc.). Whisper biases towards American forms regardless of the speaker's accent.
4. If a Meetup event JSON file path is also provided below, read it first. The agenda inside its "description" field lists the canonical spelling of each speaker's name (typically a markdown-style pipe-delimited table; rows are roughly "| TIME | SPEAKER NAME — TALK TITLE |", separator is the em-dash character U+2014). Strip a leading backslash before any of | - + ( ) . before parsing — Meetup escapes markdown punctuation on the wire. Where Whisper has phonetically misspelled an agenda-listed speaker's name in the SRT (e.g. "Nebula" or "Nabula" when the agenda says "Nabila"), use the Edit tool to correct it to the agenda spelling. Preserve any parenthetical nickname (e.g. "Rob Amos (Bok)") if it appears. Apply this to every agenda-listed name regardless of whether they are the primary speaker of this segment — speakers frequently thank or reference other presenters on the night. If no Meetup file is provided or the file contains "{}" (no usable agenda), skip this step entirely. Do not invent or guess names.
5. When done, reply with just the word "done".

SRT file path: `

func (a *Activities) CleanTranscript(ctx context.Context, input model.CleanTranscriptInput) (model.CleanTranscriptOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Cleaning transcript", "segmentIndex", input.Segment.Index)

	wsDir := a.workspaceDir(ctx)
	srcPath := filepath.Join(wsDir, input.SubtitlePath)

	// Copy the original SRT to a _clean variant so Claude edits the copy.
	// This preserves the Whisper output and makes retries idempotent.
	ext := filepath.Ext(input.SubtitlePath)
	cleanRelPath := input.SubtitlePath[:len(input.SubtitlePath)-len(ext)] + "_clean" + ext
	cleanPath := filepath.Join(wsDir, cleanRelPath)

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return model.CleanTranscriptOutput{}, fmt.Errorf("read SRT: %w", err)
	}
	if err := os.WriteFile(cleanPath, raw, 0o644); err != nil {
		return model.CleanTranscriptOutput{}, fmt.Errorf("copy SRT: %w", err)
	}

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

	prompt := cleanTranscriptPrompt + cleanPath
	if input.MeetupEventPath != "" {
		prompt += "\nMeetup event JSON file path: " + filepath.Join(wsDir, input.MeetupEventPath)
	}
	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--output-format", "text",
		"--model", "sonnet",
		"--no-session-persistence",
		"--allowedTools", "Read,Edit",
	)
	// Ensure claude CLI doesn't think it's nested inside another session.
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")

	if err := cmd.Run(); err != nil {
		close(done)
		return model.CleanTranscriptOutput{}, fmt.Errorf("claude CLI failed: %w", err)
	}
	close(done)

	logger.Info("Transcript cleaned", "path", cleanRelPath)
	return model.CleanTranscriptOutput{SubtitlePath: cleanRelPath}, nil
}
