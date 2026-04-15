package session

import "encoding/json"

// BehaviorPresets holds the toggleable behavior flags and custom instructions
// that are injected into the system prompt preamble.
type BehaviorPresets struct {
	AutoCommit         bool   `json:"autoCommit"`
	SuggestParallel    bool   `json:"suggestParallel"`
	PlanFirst          bool   `json:"planFirst"`
	Terse              bool   `json:"terse"`
	CustomInstructions string `json:"customInstructions,omitempty"`
}

// DefaultPresets returns presets matching the previously hardcoded behavior:
// parallel session suggestions on, auto-commit on.
func DefaultPresets() BehaviorPresets {
	return BehaviorPresets{
		AutoCommit:      true,
		SuggestParallel: true,
	}
}

// IsZero reports whether all fields are at their zero values,
// indicating the caller did not explicitly set any presets.
func (bp BehaviorPresets) IsZero() bool {
	return !bp.AutoCommit && !bp.SuggestParallel && !bp.PlanFirst && !bp.Terse && bp.CustomInstructions == ""
}

// String marshals presets to JSON for DB storage.
func (bp BehaviorPresets) String() string {
	b, _ := json.Marshal(bp)
	return string(b)
}

// PresetDefinition describes a single toggleable behavior preset.
// The frontend uses this to render toggle UI dynamically.
type PresetDefinition struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// PresetRegistry is the authoritative list of curated behavior presets.
var PresetRegistry = []PresetDefinition{
	{Key: "autoCommit", Title: "Auto-commit at milestones", Description: "Commit proactively after each logical unit of work in worktree sessions."},
	{Key: "suggestParallel", Title: "Suggest parallel sessions", Description: "Suggest independent tasks as prompt blocks that can be launched as separate sessions."},
	{Key: "planFirst", Title: "Plan before implementing", Description: "Outline approach and wait for confirmation before writing code. Soft instruction, distinct from plan permission mode."},
	{Key: "terse", Title: "Terse output", Description: "Minimize explanations. Show code changes directly without summaries."},
}

// ParsePresets unmarshals JSON into BehaviorPresets.
// Returns DefaultPresets() for empty, "{}", or invalid input.
func ParsePresets(raw string) BehaviorPresets {
	if raw == "" || raw == "{}" {
		return DefaultPresets()
	}
	var bp BehaviorPresets
	if err := json.Unmarshal([]byte(raw), &bp); err != nil {
		return DefaultPresets()
	}
	return bp
}
