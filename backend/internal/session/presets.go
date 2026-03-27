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
