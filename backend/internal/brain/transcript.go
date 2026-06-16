package brain

import (
	"encoding/json"
	"strings"
)

// TranscriptEvent is a minimal projection of a persisted session event, kept
// decoupled from the store types so transcript reconstruction stays portable and
// unit-testable.
type TranscriptEvent struct {
	Type string
	Data string // raw JSON of the event payload
}

// BuildTranscript reconstructs a readable, role-labeled transcript from a
// session's events and splits it into chunks no larger than maxCharsPerChunk
// (<=0 disables chunking). Only content-bearing event types are included;
// tool calls/results and thinking are omitted as noise for fact extraction.
func BuildTranscript(events []TranscriptEvent, maxCharsPerChunk int) []string {
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		if line := transcriptLine(ev); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	if maxCharsPerChunk <= 0 {
		return []string{strings.Join(lines, "\n\n")}
	}

	var chunks []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			chunks = append(chunks, b.String())
			b.Reset()
		}
	}
	for _, line := range lines {
		if len(line) > maxCharsPerChunk {
			line = line[:maxCharsPerChunk]
		}
		if b.Len() > 0 && b.Len()+len(line)+2 > maxCharsPerChunk {
			flush()
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(line)
	}
	flush()
	return chunks
}

func transcriptLine(ev TranscriptEvent) string {
	switch ev.Type {
	case "prompt":
		var d struct {
			Prompt string `json:"prompt"`
		}
		if json.Unmarshal([]byte(ev.Data), &d) == nil {
			if t := strings.TrimSpace(d.Prompt); t != "" {
				return "User: " + t
			}
		}
	case "user_message":
		var d struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(ev.Data), &d) == nil {
			if t := strings.TrimSpace(d.Content); t != "" {
				return "User: " + t
			}
		}
	case "text":
		var d struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(ev.Data), &d) == nil {
			if t := strings.TrimSpace(d.Content); t != "" {
				return "Assistant: " + t
			}
		}
	case "agent_result":
		var d struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(ev.Data), &d) == nil {
			var parts []string
			for _, c := range d.Content {
				if c.Type == "text" {
					if t := strings.TrimSpace(c.Text); t != "" {
						parts = append(parts, t)
					}
				}
			}
			if len(parts) > 0 {
				return "Subagent: " + strings.Join(parts, "\n")
			}
		}
	}
	return ""
}
