package session

import "testing"

func TestCapabilitiesForProvider_Claude(t *testing.T) {
	t.Parallel()
	caps := CapabilitiesForProvider("claude")
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
		"EffortSwitch":       caps.EffortSwitch,
	} {
		if !got {
			t.Errorf("claude.%s expected true, got false", name)
		}
	}
}

func TestCapabilitiesForProvider_Codex(t *testing.T) {
	t.Parallel()
	caps := CapabilitiesForProvider("codex")
	if caps.Provider != "codex" {
		t.Fatalf("expected provider=codex, got %q", caps.Provider)
	}
	// The frontend gates UI on these — if any flip to true without
	// docs/tech-debt.md being updated, the gating goes silently wrong.
	for name, got := range map[string]bool{
		"PlanMode":         caps.PlanMode,
		"Thinking":         caps.Thinking,
		"Subagents":        caps.Subagents,
		"CompactionEvents": caps.CompactionEvents,
		"Attachments":      caps.Attachments,
		"ModelSwitch":      caps.ModelSwitch,
	} {
		if got {
			t.Errorf("codex.%s expected false, got true", name)
		}
	}
	// What codex does support. MidTurnSendMessage is true even though the codex
	// adapter has no native mid-turn channel: agentique emulates it by buffering
	// the message and replaying it as a fresh turn at the next idle boundary
	// (Session.QueuePendingMessage / flushPendingMessages), and the wire flag
	// drives the UI affordance.
	for name, got := range map[string]bool{
		"Effort":                 caps.Effort,
		"InteractivePermissions": caps.InteractivePermissions,
		"AskUserQuestion":        caps.AskUserQuestion,
		"Ping":                   caps.Ping,
		"Resume":                 caps.Resume,
		"RateLimitEvents":        caps.RateLimitEvents,
		"MidTurnSendMessage":     caps.MidTurnSendMessage,
		// Codex takes effort per turn, so a live change applies from the
		// next one: the ramp is live even though the model picker is not.
		"EffortSwitch": caps.EffortSwitch,
	} {
		if !got {
			t.Errorf("codex.%s expected true, got false", name)
		}
	}
}

func TestCapabilitiesForProvider_EmptyDefaultsToClaude(t *testing.T) {
	t.Parallel()
	// normalizeProvider turns "" into "claude" — keep this seam working so a
	// stale frontend payload still ends up with claude capabilities.
	caps := CapabilitiesForProvider("")
	if caps.Provider != "claude" {
		t.Fatalf("empty provider should default to claude caps, got %q", caps.Provider)
	}
}

func TestCapabilitiesForProvider_Unknown(t *testing.T) {
	t.Parallel()
	// An unknown provider name should not silently advertise claude's
	// feature set — the safe default is "nothing supported".
	caps := CapabilitiesForProvider("made-up")
	// normalizeProvider currently coerces anything non-codex to claude, so
	// this test pins that exact behavior. If the coercion ever loosens,
	// this guards against falsely advertising features the new provider
	// may not implement.
	if caps.Provider == "made-up" && (caps.PlanMode || caps.Resume) {
		t.Fatalf("unknown provider should not advertise claude flags")
	}
}
