package ui

import "strings"

// SRTToVTT converts SubRip captions to WebVTT for an HTML5 <track>. Only the
// comma→dot change on timing lines (those containing "-->") is needed; commas
// in caption text are left intact.
func SRTToVTT(in []byte) []byte {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, line := range strings.Split(string(in), "\n") {
		if strings.Contains(line, "-->") {
			line = strings.ReplaceAll(line, ",", ".")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
