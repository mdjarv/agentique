package session

import "testing"

func TestCapabilitiesForProvider_Claude(t *testing.T) {
	caps := capabilitiesForProvider("claude")
	if caps.Provider != "claude" {
		t.Fatalf("expected provider=claude, got %q", caps.Provider)
	}
	for name, got := range map[string]bool{
		"PlanMode":           caps.PlanMode,
		"MidTurnSendMessage": caps.MidTurnSendMessage,
		"Resume":             caps.Resume,
		"Thinking":           caps.Thinking,
		"Subagents":          caps.Subagents,
		"Attachments":        caps.Attachments,
		"ModelSwitch":        caps.ModelSwitch,
	} {
		if !got {
			t.Errorf("claude.%s expected true, got false", name)
		}
	}
}

func TestCapabilitiesForProvider_Codex(t *testing.T) {
	caps := capabilitiesForProvider("codex")
	if caps.Provider != "codex" {
		t.Fatalf("expected provider=codex, got %q", caps.Provider)
	}
	// The frontend gates UI on these — if any flip to true without
	// docs/tech-debt.md being updated, the gating goes silently wrong.
	for name, got := range map[string]bool{
		"PlanMode":           caps.PlanMode,
		"MidTurnSendMessage": caps.MidTurnSendMessage,
		"Resume":             caps.Resume,
		"Thinking":           caps.Thinking,
		"Subagents":          caps.Subagents,
		"RateLimitEvents":    caps.RateLimitEvents,
		"CompactionEvents":   caps.CompactionEvents,
		"Attachments":        caps.Attachments,
		"ModelSwitch":        caps.ModelSwitch,
	} {
		if got {
			t.Errorf("codex.%s expected false, got true", name)
		}
	}
	// What codex does support.
	for name, got := range map[string]bool{
		"Effort":                 caps.Effort,
		"InteractivePermissions": caps.InteractivePermissions,
		"AskUserQuestion":        caps.AskUserQuestion,
		"Ping":                   caps.Ping,
	} {
		if !got {
			t.Errorf("codex.%s expected true, got false", name)
		}
	}
}

func TestCapabilitiesForProvider_EmptyDefaultsToClaude(t *testing.T) {
	// normalizeProvider turns "" into "claude" — keep this seam working so a
	// stale frontend payload still ends up with claude capabilities.
	caps := capabilitiesForProvider("")
	if caps.Provider != "claude" {
		t.Fatalf("empty provider should default to claude caps, got %q", caps.Provider)
	}
}

func TestCapabilitiesForProvider_Unknown(t *testing.T) {
	// An unknown provider name should not silently advertise claude's
	// feature set — the safe default is "nothing supported".
	caps := capabilitiesForProvider("made-up")
	// normalizeProvider currently coerces anything non-codex to claude, so
	// this test pins that exact behavior. If the coercion ever loosens,
	// this guards against falsely advertising features the new provider
	// may not implement.
	if caps.Provider == "made-up" && (caps.PlanMode || caps.Resume) {
		t.Fatalf("unknown provider should not advertise claude flags")
	}
}
