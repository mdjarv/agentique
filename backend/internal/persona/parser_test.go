package persona

import "testing"

func TestParseResponse_AllFields(t *testing.T) {
	in := `ACTION: spawn
CONFIDENCE: 0.85
REDIRECT_TO: alice
REASON: needs deep dive
RESPONSE: I think you should spawn a session and dig in.`

	got := parseResponse(in)
	want := QueryResult{
		Action:     "spawn",
		Confidence: 0.85,
		RedirectTo: "alice",
		Reason:     "needs deep dive",
		Response:   "I think you should spawn a session and dig in.",
	}
	if got != want {
		t.Errorf("parseResponse mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParseResponse_DefaultsAndLowercaseAction(t *testing.T) {
	got := parseResponse("ACTION: ANSWER\nRESPONSE: hi")
	if got.Action != "answer" {
		t.Errorf("expected action lowercased to 'answer', got %q", got.Action)
	}
	if got.Confidence != 0.5 {
		t.Errorf("expected default confidence 0.5, got %v", got.Confidence)
	}
	if got.Response != "hi" {
		t.Errorf("expected response 'hi', got %q", got.Response)
	}
}

func TestParseResponse_MultiLineResponse(t *testing.T) {
	in := `ACTION: answer
RESPONSE: line one
line two
line three`
	got := parseResponse(in)
	want := "line one\nline two\nline three"
	if got.Response != want {
		t.Errorf("multi-line response: got %q, want %q", got.Response, want)
	}
}

func TestParseResponse_FallbackUsesRawText(t *testing.T) {
	// No RESPONSE field — use the whole text as the response.
	in := "I am just plain text without labels"
	got := parseResponse(in)
	if got.Response != in {
		t.Errorf("fallback: got %q, want %q", got.Response, in)
	}
	if got.Action != "answer" {
		t.Errorf("fallback action: got %q, want 'answer'", got.Action)
	}
}

func TestParseResponse_InvalidConfidenceIgnored(t *testing.T) {
	got := parseResponse("CONFIDENCE: not-a-number\nRESPONSE: ok")
	if got.Confidence != 0.5 {
		t.Errorf("invalid confidence should keep default 0.5, got %v", got.Confidence)
	}
}

func TestParseResponse_EmptyInput(t *testing.T) {
	got := parseResponse("")
	if got.Action != "answer" {
		t.Errorf("empty: action default should be 'answer', got %q", got.Action)
	}
	if got.Response != "" {
		t.Errorf("empty: response should be empty, got %q", got.Response)
	}
}

func TestParseResponse_ResponseInlineWithLabel(t *testing.T) {
	got := parseResponse("RESPONSE: same line content")
	if got.Response != "same line content" {
		t.Errorf("inline RESPONSE: got %q", got.Response)
	}
}

func TestParseProfileResponse_MissingConfigDefaultsToEmpty(t *testing.T) {
	got := parseProfileResponse("NAME: X")
	if got.Config != "{}" {
		t.Errorf("missing CONFIG should default to {}, got %q", got.Config)
	}
}

func TestParseProfileResponse_EmptyCapabilitiesGivesNil(t *testing.T) {
	got := parseProfileResponse("NAME: X\nCAPABILITIES:")
	if got.Capabilities != nil {
		t.Errorf("empty CAPABILITIES should give nil, got %v", got.Capabilities)
	}
}

func TestMatchField(t *testing.T) {
	var dst string
	if !matchField("NAME: alice", "NAME:", &dst) {
		t.Fatal("expected true")
	}
	if dst != "alice" {
		t.Errorf("dst = %q, want 'alice'", dst)
	}

	dst = ""
	if matchField("ROLE: dev", "NAME:", &dst) {
		t.Error("expected false for non-matching prefix")
	}
	if dst != "" {
		t.Errorf("dst should not change when no match, got %q", dst)
	}
}

func TestStartsMultiline(t *testing.T) {
	var lines []string
	if !startsMultiline("DESC: first line", "DESC:", &lines) {
		t.Fatal("expected true")
	}
	if len(lines) != 1 || lines[0] != "first line" {
		t.Errorf("lines = %v, want [first line]", lines)
	}

	lines = nil
	if !startsMultiline("DESC:", "DESC:", &lines) {
		t.Fatal("expected true even with no inline content")
	}
	if len(lines) != 0 {
		t.Errorf("lines should be empty, got %v", lines)
	}
}
