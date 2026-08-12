package activity

import "testing"

// Trimmed `claude -p --output-format json` result envelope (real shape from the
// CLI; many fields — usage, cost, session_id — omitted). Locks parseClaudeResult
// against the fields we log for segment-timing diagnostics.
const claudeResultJSON = `{"type":"result","subtype":"success","is_error":false,"duration_ms":5732,"duration_api_ms":5001,"num_turns":7,"result":"done","session_id":"abc","total_cost_usd":0.12}`

func TestParseClaudeResult_ExtractsTurnsAndDuration(t *testing.T) {
	res, err := parseClaudeResult([]byte(claudeResultJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NumTurns != 7 {
		t.Errorf("NumTurns = %d, want 7", res.NumTurns)
	}
	if res.DurationMS != 5732 {
		t.Errorf("DurationMS = %d, want 5732", res.DurationMS)
	}
	if res.IsError {
		t.Error("IsError = true, want false")
	}
	if res.Subtype != "success" {
		t.Errorf("Subtype = %q, want %q", res.Subtype, "success")
	}
}

func TestParseClaudeResult_MalformedReturnsError(t *testing.T) {
	if _, err := parseClaudeResult([]byte("not json")); err == nil {
		t.Error("expected an error for a malformed result envelope")
	}
}
