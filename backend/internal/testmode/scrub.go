package testmode

import "strings"

// Scrubber replaces sensitive paths in serialized event data.
type Scrubber struct {
	replacements []replacement
}

type replacement struct {
	old, new string
}

// NewScrubber creates a scrubber that replaces workDir and home directory
// references with safe placeholders.
func NewScrubber(workDir, homeDir string) *Scrubber {
	var reps []replacement
	if workDir != "" {
		reps = append(reps, replacement{old: workDir, new: "/tmp/fixture-project"})
	}
	if homeDir != "" {
		reps = append(reps, replacement{old: homeDir, new: "/home/user"})
	}
	return &Scrubber{replacements: reps}
}

// Scrub applies all replacements to the input string.
func (s *Scrubber) Scrub(data string) string {
	for _, r := range s.replacements {
		data = strings.ReplaceAll(data, r.old, r.new)
	}
	return data
}
