package ui

import (
	"strings"
	"testing"
)

func TestBuildSignalArgs(t *testing.T) {
	args := buildSignalArgs("miyuki:7233", "decanter-yt-1", "review", true)
	got := strings.Join(args, " ")
	want := `workflow signal --address miyuki:7233 --workflow-id decanter-yt-1 --name review_approval --input {"Approved":true}`
	if got != want {
		t.Fatalf("signal args:\n got: %s\nwant: %s", got, want)
	}
	if !strings.Contains(strings.Join(buildSignalArgs("a", "b", "upload", false), " "), "upload_approval") {
		t.Fatal("upload gate must map to upload_approval")
	}
	if !strings.Contains(strings.Join(buildSignalArgs("a", "b", "upload", false), " "), `{"Approved":false}`) {
		t.Fatal("reject must send Approved:false")
	}
}

func TestBuildResetArgs(t *testing.T) {
	args := buildResetArgs("miyuki:7233", "decanter-yt-1", 20, "decanter-ui: redo-assemble")
	got := strings.Join(args, " ")
	want := `workflow reset --address miyuki:7233 --workflow-id decanter-yt-1 --event-id 20 --reapply-exclude Signal --reason decanter-ui: redo-assemble`
	if got != want {
		t.Fatalf("reset args:\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildTerminateArgs(t *testing.T) {
	got := strings.Join(buildTerminateArgs("miyuki:7233", "decanter-yt-1-segment-1", "run-1", "decanter-ui: redo-split"), " ")
	want := `workflow terminate --address miyuki:7233 --workflow-id decanter-yt-1-segment-1 --run-id run-1 --reason decanter-ui: redo-split`
	if got != want {
		t.Fatalf("terminate args:\n got: %s\nwant: %s", got, want)
	}
	// No run id pinned: terminate the current run rather than passing an empty
	// --run-id, which the CLI rejects.
	if strings.Contains(strings.Join(buildTerminateArgs("a", "b", "", "r"), " "), "--run-id") {
		t.Fatal("empty run id must omit --run-id")
	}
}
