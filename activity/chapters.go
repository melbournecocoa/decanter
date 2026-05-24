package activity

import "github.com/melbournecocoa/decanter/model"

// ChapterMarker is a chapter entry in the final video's time coordinate.
type ChapterMarker struct {
	Time  float64 // seconds in final video
	Title string
}

// minChapterGap is YouTube's minimum chapter length (with a small safety margin).
const minChapterGap = 10.0

// BuildChapters converts the LLM-supplied chapter list into final-video chapter
// markers, applying the same time-shift formula used for SRT entries and
// enforcing YouTube's chapter constraints (first at 0, ≥10s apart, ≥3 total).
//
// The formula applied is:
//
//	final_t = ch.Time - startOffset + introDuration - xfadeDuration
//
// xfadeDuration accounts for the cross-fade Assemble performs at the
// intro→content and content→outro joins. Pass 0 for a hard-cut Assemble.
//
// If the LLM did not produce usable chapters, a single fallback entry is
// inserted at the intro/content boundary with the talk title.
func BuildChapters(
	llmChapters []model.Chapter,
	startOffset, introDuration, contentDuration, xfadeDuration float64,
	talkTitle string,
) []ChapterMarker {
	introShift := introDuration - xfadeDuration
	outroTime := introShift + contentDuration

	result := []ChapterMarker{
		{Time: 0, Title: "Intro"},
	}

	for _, ch := range llmChapters {
		finalT := ch.Time - startOffset + introShift
		// Out of content range (with a 10s buffer before outro).
		if finalT < introShift || finalT > outroTime-minChapterGap {
			continue
		}
		// Too close to previous chapter.
		if finalT-result[len(result)-1].Time < minChapterGap {
			continue
		}
		result = append(result, ChapterMarker{Time: finalT, Title: ch.Title})
	}

	// Append outro. The 10s-from-outro check above means we never need to drop
	// the preceding LLM chapter here.
	result = append(result, ChapterMarker{Time: outroTime, Title: "Outro"})

	// Fallback: if only Intro + Outro survived, inject the talk title.
	if len(result) == 2 {
		result = []ChapterMarker{
			{Time: 0, Title: "Intro"},
			{Time: introShift, Title: talkTitle},
			{Time: outroTime, Title: "Outro"},
		}
	}

	return result
}
