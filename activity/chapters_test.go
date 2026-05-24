package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/melbournecocoa/decanter/model"
)

func TestBuildChapters_EmptyLLM_Fallback(t *testing.T) {
	got := BuildChapters(nil, 0, 12, 600, 0, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 12, Title: "My Talk"},
		{Time: 612, Title: "Outro"},
	}, got)
}

func TestBuildChapters_LLMHappyPath(t *testing.T) {
	llm := []model.Chapter{
		{Time: 5, Title: "Introduction"},
		{Time: 120, Title: "Background"},
		{Time: 300, Title: "Demo"},
		{Time: 540, Title: "Q&A"},
	}
	got := BuildChapters(llm, 0, 12, 600, 0, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 17, Title: "Introduction"},
		{Time: 132, Title: "Background"},
		{Time: 312, Title: "Demo"},
		{Time: 552, Title: "Q&A"},
		{Time: 612, Title: "Outro"},
	}, got)
}

func TestBuildChapters_LLMTooCloseToPrev_Dropped(t *testing.T) {
	llm := []model.Chapter{
		{Time: 5, Title: "Introduction"},
		{Time: 6, Title: "Background (too close)"},
		{Time: 120, Title: "Demo"},
	}
	got := BuildChapters(llm, 0, 12, 600, 0, "My Talk")
	// "Background" is dropped because final_t=18 is only 1s after "Introduction" at 17.
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 17, Title: "Introduction"},
		{Time: 132, Title: "Demo"},
		{Time: 612, Title: "Outro"},
	}, got)
}

func TestBuildChapters_LLMOutOfRange_Dropped(t *testing.T) {
	llm := []model.Chapter{
		{Time: -5, Title: "Way before"},
		{Time: 599, Title: "Way after"}, // contentDuration=600 → cutoff=590, this falls within outro 10s buffer
	}
	got := BuildChapters(llm, 0, 12, 600, 0, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 12, Title: "My Talk"},
		{Time: 612, Title: "Outro"},
	}, got)
}

func TestBuildChapters_LLMTooCloseToOutro_Dropped(t *testing.T) {
	llm := []model.Chapter{
		{Time: 5, Title: "Introduction"},
		{Time: 595, Title: "Closing thoughts"}, // final_t=607, outro at 612 → 5s gap, fails 10s rule
	}
	got := BuildChapters(llm, 0, 12, 600, 0, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 17, Title: "Introduction"},
		{Time: 612, Title: "Outro"},
	}, got)
}

func TestBuildChapters_StartOffsetApplied(t *testing.T) {
	llm := []model.Chapter{
		{Time: 5, Title: "Section"},
	}
	// startOffset=3, introDuration=12. final_t = 5 - 3 + 12 = 14.
	got := BuildChapters(llm, 3, 12, 600, 0, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 14, Title: "Section"},
		{Time: 612, Title: "Outro"},
	}, got)
}

func TestBuildChapters_XfadeShift(t *testing.T) {
	llm := []model.Chapter{
		{Time: 5, Title: "Introduction"},
		{Time: 300, Title: "Demo"},
	}
	// introDuration=12, xfadeDuration=0.3 → introShift=11.7; outroTime=611.7.
	// final_t = ch.Time - 0 + 11.7
	got := BuildChapters(llm, 0, 12, 600, 0.3, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 16.7, Title: "Introduction"},
		{Time: 311.7, Title: "Demo"},
		{Time: 611.7, Title: "Outro"},
	}, got)
}

func TestBuildChapters_XfadeShift_Fallback(t *testing.T) {
	// No usable LLM chapters → talk title sits at introShift = 11.7.
	got := BuildChapters(nil, 0, 12, 600, 0.3, "My Talk")
	assert.Equal(t, []ChapterMarker{
		{Time: 0, Title: "Intro"},
		{Time: 11.7, Title: "My Talk"},
		{Time: 611.7, Title: "Outro"},
	}, got)
}
