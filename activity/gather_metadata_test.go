package activity

import (
	"math"
	"testing"

	"github.com/melbournecocoa/decanter/model"
)

// Real `ffmpeg silencedetect=noise=-35dB:d=0.3` stderr captured from the
// 2026-06-25 run (decanter-yt-20260625-222930). seg-01 has a clean 6s of
// leading silence; seg-03 has a short noise blip (0.0065–1.673) before a second
// silence gap, then the speaker.
const silenceStderrSeg01 = `[Parsed_silencedetect_0 @ 0x781029bc0] silence_start: 0.0065
[Parsed_silencedetect_0 @ 0x781029bc0] silence_end: 6.005417 | silence_duration: 5.998917
[Parsed_silencedetect_0 @ 0x781029bc0] silence_start: 11.054479
[Parsed_silencedetect_0 @ 0x781029bc0] silence_end: 11.830083 | silence_duration: 0.775604`

const silenceStderrSeg03 = `[Parsed_silencedetect_0 @ 0xa15015b00] silence_start: 0.0065
[Parsed_silencedetect_0 @ 0xa15015b00] silence_end: 1.672958 | silence_duration: 1.666458
[Parsed_silencedetect_0 @ 0xa15015b00] silence_start: 1.826167
[Parsed_silencedetect_0 @ 0xa15015b00] silence_end: 4.479146 | silence_duration: 2.652979`

func floatEq(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParseSpeechOnset_LeadingSilence(t *testing.T) {
	onset, ok := parseSpeechOnset(silenceStderrSeg01)
	if !ok {
		t.Fatal("expected ok=true when there is leading silence")
	}
	if !floatEq(onset, 6.005417) {
		t.Errorf("onset = %v, want 6.005417 (end of the leading-silence region)", onset)
	}
}

func TestParseSpeechOnset_BlipBeforeGreeting(t *testing.T) {
	// We deliberately take the end of the FIRST leading-silence period (1.672958),
	// even though a noise blip precedes the real greeting. Erring early keeps the
	// cut safe (never clips the talk); the reviewer can tighten it.
	onset, ok := parseSpeechOnset(silenceStderrSeg03)
	if !ok {
		t.Fatal("expected ok=true when there is leading silence")
	}
	if !floatEq(onset, 1.672958) {
		t.Errorf("onset = %v, want 1.672958 (end of the first leading-silence period)", onset)
	}
}

func TestParseSpeechOnset_NoLeadingSilence(t *testing.T) {
	// First silence starts well after t=0 → speech runs from the top, nothing to floor.
	stderr := `[Parsed_silencedetect_0 @ 0x0] silence_start: 8.5
[Parsed_silencedetect_0 @ 0x0] silence_end: 9.2 | silence_duration: 0.7`
	if _, ok := parseSpeechOnset(stderr); ok {
		t.Error("expected ok=false when the first silence does not begin near t=0")
	}
}

func TestParseSpeechOnset_NoSilenceReported(t *testing.T) {
	if _, ok := parseSpeechOnset("ffmpeg version 8.0\n  Duration: 00:07:41.69\n"); ok {
		t.Error("expected ok=false when silencedetect reports no silence")
	}
}

func TestApplyOnsetFloor_RaisesSmearedStart(t *testing.T) {
	// The classic bug: Whisper pinned the greeting to 0.0; the real onset is 5.391.
	m := model.TalkMetadata{Trim: &model.TrimRange{StartSeconds: 0.0, EndSeconds: 447.0}}

	changed := applyOnsetFloor(&m, 5.391)

	if !changed {
		t.Fatal("expected changed=true: a 0.0 start must be raised to the detected onset")
	}
	want := 5.391 - onsetLeadInMargin
	if !floatEq(m.Trim.StartSeconds, want) {
		t.Errorf("StartSeconds = %v, want %v (onset minus lead-in margin)", m.Trim.StartSeconds, want)
	}
}

func TestApplyOnsetFloor_PreservesLaterGreeting(t *testing.T) {
	// Raffle/MC bleed: the LLM correctly picked a later greeting cue (25s) and the
	// audio onset is back on the bleed at ~2s. The floor must NOT pull the start back.
	m := model.TalkMetadata{Trim: &model.TrimRange{StartSeconds: 25.0, EndSeconds: 400.0}}

	changed := applyOnsetFloor(&m, 2.0)

	if changed {
		t.Fatal("expected changed=false: the floor must not override a legitimately-later start")
	}
	if m.Trim.StartSeconds != 25.0 {
		t.Errorf("StartSeconds = %v, want 25.0 (unchanged)", m.Trim.StartSeconds)
	}
}

func TestApplyOnsetFloor_NilTrimIsNoop(t *testing.T) {
	m := model.TalkMetadata{Title: "x"}
	if applyOnsetFloor(&m, 5.0) {
		t.Error("expected changed=false when Trim is nil")
	}
}

func TestApplyOnsetFloor_NeverFloorsPastEnd(t *testing.T) {
	// Defensive: a nonsensical onset after the talk end must not corrupt the trim.
	m := model.TalkMetadata{Trim: &model.TrimRange{StartSeconds: 0.0, EndSeconds: 30.0}}
	if applyOnsetFloor(&m, 999.0) {
		t.Error("expected changed=false when the floor would land at/after EndSeconds")
	}
	if m.Trim.StartSeconds != 0.0 {
		t.Errorf("StartSeconds = %v, want 0.0 (untouched)", m.Trim.StartSeconds)
	}
}

func TestApplyTrimFallback_FillsWhenModelOmitsTrim(t *testing.T) {
	seg := model.Segment{Start: 100, End: 400, StartOffset: 2}
	m := model.TalkMetadata{Title: "x"}

	changed := applyTrimFallback(&m, seg)

	if !changed {
		t.Fatal("expected changed=true when Trim is nil")
	}
	if m.Trim == nil {
		t.Fatal("expected Trim to be filled")
	}
	if m.Trim.StartSeconds != 2 {
		t.Errorf("StartSeconds = %v, want 2 (StartOffset)", m.Trim.StartSeconds)
	}
	if m.Trim.EndSeconds != 302 { // StartOffset + (End-Start) = 2 + 300
		t.Errorf("EndSeconds = %v, want 302", m.Trim.EndSeconds)
	}
}

func TestApplyTrimFallback_PreservesModelProposal(t *testing.T) {
	seg := model.Segment{Start: 100, End: 400, StartOffset: 2}
	m := model.TalkMetadata{Trim: &model.TrimRange{StartSeconds: 28, EndSeconds: 350}}

	changed := applyTrimFallback(&m, seg)

	if changed {
		t.Fatal("expected changed=false when Trim already set; the model proposal must not be clobbered")
	}
	if m.Trim.StartSeconds != 28 || m.Trim.EndSeconds != 350 {
		t.Errorf("Trim mutated to %+v, want {28 350}", *m.Trim)
	}
}
