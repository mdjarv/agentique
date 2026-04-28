package persona

import (
	"encoding/json"
	"strconv"
	"strings"
)

// parseResponse extracts a QueryResult from a Haiku reply formatted with the
// ACTION/CONFIDENCE/REDIRECT_TO/REASON/RESPONSE field labels. Defaults to
// action=answer, confidence=0.5 when fields are missing. Falls back to
// using the raw text as Response when no RESPONSE field was present.
func parseResponse(text string) QueryResult {
	text = strings.TrimSpace(text)

	result := QueryResult{
		Action:     "answer",
		Confidence: 0.5,
	}

	lines := strings.Split(text, "\n")
	var responseLines []string
	inResponse := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if val, ok := strings.CutPrefix(trimmed, "ACTION:"); ok {
			result.Action = strings.ToLower(strings.TrimSpace(val))
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "CONFIDENCE:"); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
				result.Confidence = f
			}
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "REDIRECT_TO:"); ok {
			result.RedirectTo = strings.TrimSpace(val)
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "REASON:"); ok {
			result.Reason = strings.TrimSpace(val)
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "RESPONSE:"); ok {
			inResponse = true
			rest := strings.TrimSpace(val)
			if rest != "" {
				responseLines = append(responseLines, rest)
			}
			continue
		}
		if inResponse {
			responseLines = append(responseLines, line)
		}
	}

	result.Response = strings.TrimSpace(strings.Join(responseLines, "\n"))

	if result.Response == "" {
		result.Response = text
	}

	return result
}

// parseProfileResponse extracts a GenerateProfileResult from a Haiku reply
// formatted with NAME/ROLE/DESCRIPTION/AVATAR/SYSTEM_PROMPT/CUSTOM_INSTRUCTIONS/
// CAPABILITIES/CONFIG field labels. CONFIG is validated as JSON, falling back
// to "{}" if malformed.
func parseProfileResponse(text string) GenerateProfileResult {
	text = strings.TrimSpace(text)
	var result GenerateProfileResult

	var descLines, systemPromptLines, customInstLines []string
	var capabilitiesRaw string
	current := ""

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case matchField(trimmed, "NAME:", &result.Name):
			current = ""
		case matchField(trimmed, "ROLE:", &result.Role):
			current = ""
		case matchField(trimmed, "AVATAR:", &result.Avatar):
			current = ""
		case matchField(trimmed, "CAPABILITIES:", &capabilitiesRaw):
			current = ""
		case matchField(trimmed, "CONFIG:", &result.Config):
			current = ""
		case startsMultiline(trimmed, "DESCRIPTION:", &descLines):
			current = "description"
		case startsMultiline(trimmed, "SYSTEM_PROMPT:", &systemPromptLines):
			current = "systemPrompt"
		case startsMultiline(trimmed, "CUSTOM_INSTRUCTIONS:", &customInstLines):
			current = "customInst"
		default:
			switch current {
			case "description":
				descLines = append(descLines, line)
			case "systemPrompt":
				systemPromptLines = append(systemPromptLines, line)
			case "customInst":
				customInstLines = append(customInstLines, line)
			}
		}
	}

	result.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
	result.SystemPromptAdditions = strings.TrimSpace(strings.Join(systemPromptLines, "\n"))
	result.CustomInstructions = strings.TrimSpace(strings.Join(customInstLines, "\n"))
	result.Capabilities = parseCapabilities(capabilitiesRaw)

	if result.Config != "" {
		var tmp map[string]any
		if json.Unmarshal([]byte(result.Config), &tmp) != nil {
			result.Config = "{}"
		}
	} else {
		result.Config = "{}"
	}

	return result
}

// parseCapabilities splits a comma-separated capability list, trims each tag,
// and drops empties. Returns nil when the input is empty/whitespace.
func parseCapabilities(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if tag := strings.TrimSpace(p); tag != "" {
			out = append(out, tag)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// matchField returns true and writes into dst when trimmed starts with prefix.
// Used for single-line fields (NAME/ROLE/AVATAR/CONFIG).
func matchField(trimmed, prefix string, dst *string) bool {
	val, ok := strings.CutPrefix(trimmed, prefix)
	if !ok {
		return false
	}
	*dst = strings.TrimSpace(val)
	return true
}

// startsMultiline returns true and seeds lines with any content on the
// same line as the label, when trimmed begins with prefix.
func startsMultiline(trimmed, prefix string, lines *[]string) bool {
	val, ok := strings.CutPrefix(trimmed, prefix)
	if !ok {
		return false
	}
	if rest := strings.TrimSpace(val); rest != "" {
		*lines = append(*lines, rest)
	}
	return true
}
