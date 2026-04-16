package session

import "testing"

func TestParsePersonaConfig_Full(t *testing.T) {
	raw := `{
		"model": "sonnet",
		"effort": "high",
		"autoApproveMode": "auto",
		"behaviorPresets": {"autoCommit": true, "terse": true},
		"systemPromptAdditions": "You are a senior backend developer.",
		"communicationMode": "spoke"
	}`
	pc := parsePersonaConfig(raw)

	if pc.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet", pc.Model)
	}
	if pc.Effort != "high" {
		t.Errorf("effort = %q, want high", pc.Effort)
	}
	if pc.AutoApproveMode != "auto" {
		t.Errorf("autoApproveMode = %q, want auto", pc.AutoApproveMode)
	}
	if !pc.BehaviorPresets.AutoCommit {
		t.Error("behaviorPresets.autoCommit should be true")
	}
	if !pc.BehaviorPresets.Terse {
		t.Error("behaviorPresets.terse should be true")
	}
	if pc.SystemPromptAdditions != "You are a senior backend developer." {
		t.Errorf("systemPromptAdditions = %q", pc.SystemPromptAdditions)
	}
	if pc.CommunicationMode != "spoke" {
		t.Errorf("communicationMode = %q, want spoke", pc.CommunicationMode)
	}
}

func TestParsePersonaConfig_Empty(t *testing.T) {
	pc := parsePersonaConfig("")
	if pc.Model != "" || pc.Effort != "" || pc.SystemPromptAdditions != "" {
		t.Error("empty string should return zero-value PersonaConfig")
	}
}

func TestParsePersonaConfig_EmptyObject(t *testing.T) {
	pc := parsePersonaConfig("{}")
	if pc.Model != "" || pc.Effort != "" || pc.SystemPromptAdditions != "" {
		t.Error("empty JSON object should return zero-value PersonaConfig")
	}
}

func TestParsePersonaConfig_Partial(t *testing.T) {
	raw := `{"model": "opus", "systemPromptAdditions": "Be terse."}`
	pc := parsePersonaConfig(raw)

	if pc.Model != "opus" {
		t.Errorf("model = %q, want opus", pc.Model)
	}
	if pc.SystemPromptAdditions != "Be terse." {
		t.Errorf("systemPromptAdditions = %q", pc.SystemPromptAdditions)
	}
	if pc.Effort != "" {
		t.Errorf("effort should be empty, got %q", pc.Effort)
	}
}

func TestParsePersonaConfig_Malformed(t *testing.T) {
	pc := parsePersonaConfig("not json")
	if pc.Model != "" {
		t.Error("malformed JSON should return zero-value PersonaConfig")
	}
}
