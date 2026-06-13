package ui

import (
	"strings"
	"testing"
)

func TestSRTToVTT(t *testing.T) {
	srt := "1\n00:00:01,000 --> 00:00:04,500\nHello, world\n\n2\n00:01:02,250 --> 00:01:03,000\nNext, line\n"
	out := string(SRTToVTT([]byte(srt)))

	if !strings.HasPrefix(out, "WEBVTT\n\n") {
		t.Fatalf("missing WEBVTT header: %q", out)
	}
	// Commas in TIMING lines become dots...
	if !strings.Contains(out, "00:00:01.000 --> 00:00:04.500") {
		t.Fatalf("timing not converted: %q", out)
	}
	// ...but commas in subtitle TEXT must survive.
	if !strings.Contains(out, "Hello, world") || !strings.Contains(out, "Next, line") {
		t.Fatalf("text commas damaged: %q", out)
	}
}
