package ui

import "testing"

func TestPathHelpers(t *testing.T) {
	const base = "/ws"
	const wf = "decanter-yt-20260610-225332"

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"workspace", WorkspacePath(base, wf), "/ws/decanter-yt-20260610-225332"},
		{"segment video", SegmentVideoPath(base, wf, 1), "/ws/decanter-yt-20260610-225332/segments/segment-01.mp4"},
		{"segment video pad", SegmentVideoPath(base, wf, 10), "/ws/decanter-yt-20260610-225332/segments/segment-10.mp4"},
		{"processed dir", ProcessedDir(base, wf, 2), "/ws/decanter-yt-20260610-225332/processed/segment-02"},
		{"metadata", MetadataPath(base, wf, 2), "/ws/decanter-yt-20260610-225332/processed/segment-02/metadata.json"},
		{"final video", FinalVideoPath(base, wf, 2), "/ws/decanter-yt-20260610-225332/processed/segment-02/final.mp4"},
		{"final srt", FinalSRTPath(base, wf, 2), "/ws/decanter-yt-20260610-225332/processed/segment-02/final.srt"},
		{"thumbnail", ThumbnailPath(base, wf, 2), "/ws/decanter-yt-20260610-225332/processed/segment-02/thumbnail.jpg"},
		{"reasoning", ReasoningPath(base, wf, 2), "/ws/decanter-yt-20260610-225332/processed/segment-02/metadata_reasoning.md"},
		{"event", EventPath(base, wf), "/ws/decanter-yt-20260610-225332/event.json"},
		{"bumpers", BumpersPath(base, wf), "/ws/decanter-yt-20260610-225332/bumpers.json"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
