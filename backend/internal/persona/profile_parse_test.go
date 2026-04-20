package persona

import "testing"

func TestParseProfileResponse_AllFields(t *testing.T) {
	raw := `NAME: Rex
ROLE: backend architect
DESCRIPTION: Loves TDD.
Keeps migrations small and idempotent.
AVATAR: 🦖
SYSTEM_PROMPT: You are a senior backend architect.
Prioritize correctness over speed.
Always explain trade-offs.
CUSTOM_INSTRUCTIONS: Only touch backend files.
CONFIG: {"autoCommit": true, "terse": true}`
	got := parseProfileResponse(raw)

	if got.Name != "Rex" {
		t.Errorf("Name = %q, want Rex", got.Name)
	}
	if got.Role != "backend architect" {
		t.Errorf("Role = %q, want backend architect", got.Role)
	}
	wantDesc := "Loves TDD.\nKeeps migrations small and idempotent."
	if got.Description != wantDesc {
		t.Errorf("Description = %q, want %q", got.Description, wantDesc)
	}
	if got.Avatar != "🦖" {
		t.Errorf("Avatar = %q, want 🦖", got.Avatar)
	}
	wantSys := "You are a senior backend architect.\nPrioritize correctness over speed.\nAlways explain trade-offs."
	if got.SystemPromptAdditions != wantSys {
		t.Errorf("SystemPromptAdditions = %q, want %q", got.SystemPromptAdditions, wantSys)
	}
	if got.CustomInstructions != "Only touch backend files." {
		t.Errorf("CustomInstructions = %q, want %q", got.CustomInstructions, "Only touch backend files.")
	}
	if got.Config != `{"autoCommit": true, "terse": true}` {
		t.Errorf("Config = %q", got.Config)
	}
}

func TestParseProfileResponse_MultilineFieldsStopAtNextLabel(t *testing.T) {
	// SYSTEM_PROMPT spans multiple lines, then CUSTOM_INSTRUCTIONS starts.
	// Parser must not fold CUSTOM_INSTRUCTIONS text into SYSTEM_PROMPT.
	raw := `NAME: A
ROLE: R
DESCRIPTION: D
AVATAR: 🧠
SYSTEM_PROMPT: one
two
three
CUSTOM_INSTRUCTIONS: x
CONFIG: {}`
	got := parseProfileResponse(raw)
	if got.SystemPromptAdditions != "one\ntwo\nthree" {
		t.Errorf("SystemPromptAdditions = %q", got.SystemPromptAdditions)
	}
	if got.CustomInstructions != "x" {
		t.Errorf("CustomInstructions = %q", got.CustomInstructions)
	}
}

func TestParseProfileResponse_EmptyMultilineFields(t *testing.T) {
	raw := `NAME: A
ROLE: R
DESCRIPTION: D
AVATAR: 🧠
SYSTEM_PROMPT:
CUSTOM_INSTRUCTIONS:
CONFIG: {}`
	got := parseProfileResponse(raw)
	if got.SystemPromptAdditions != "" {
		t.Errorf("SystemPromptAdditions = %q, want empty", got.SystemPromptAdditions)
	}
	if got.CustomInstructions != "" {
		t.Errorf("CustomInstructions = %q, want empty", got.CustomInstructions)
	}
}

func TestParseProfileResponse_MalformedConfigFallsBackToEmpty(t *testing.T) {
	raw := `NAME: A
ROLE: R
DESCRIPTION: D
AVATAR: 🧠
SYSTEM_PROMPT:
CUSTOM_INSTRUCTIONS:
CONFIG: not-json`
	got := parseProfileResponse(raw)
	if got.Config != "{}" {
		t.Errorf("Config = %q, want {}", got.Config)
	}
}

func TestBuildProfilePrompt_IncludesDraftHints(t *testing.T) {
	out := buildProfilePrompt(GenerateProfileInput{
		ProjectName:           "demo",
		Role:                  "Architect",
		SystemPromptAdditions: "You love DDD.",
		CustomInstructions:    "Only backend.",
	})
	// Must include the authoritative-draft section and each hint.
	for _, want := range []string{
		"User's Draft (authoritative",
		"ROLE: Architect",
		"SYSTEM_PROMPT:",
		"You love DDD.",
		"CUSTOM_INSTRUCTIONS:",
		"Only backend.",
	} {
		if !containsSubstring(out, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
}

func TestBuildProfilePrompt_NoDraftSectionWhenEmpty(t *testing.T) {
	out := buildProfilePrompt(GenerateProfileInput{ProjectName: "demo"})
	if containsSubstring(out, "User's Draft") {
		t.Errorf("prompt should not contain draft section when no hints set\n--- prompt ---\n%s", out)
	}
}

func containsSubstring(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
