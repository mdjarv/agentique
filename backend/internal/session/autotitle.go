package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeProjectDir mirrors the Claude CLI's cwd sanitizer: every "/" and "." in
// the absolute workdir is replaced with "-". The session jsonl lives under
// ~/.claude/projects/<sanitized>/<claudeSessionID>.jsonl.
func claudeProjectDir(homeDir, workDir string) string {
	s := strings.ReplaceAll(workDir, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return filepath.Join(homeDir, ".claude", "projects", s)
}

// readUserTurns reads the jsonl transcript for (workDir, claudeSessionID) and
// returns the plain-text content of every user-role message, in order.
//
// Defensive: tolerates unknown fields, skips malformed lines, and caps the
// total number of turns + bytes so a long session can't blow the prompt.
func readUserTurns(homeDir, workDir, claudeSessionID string, maxTurns, maxBytes int) ([]string, error) {
	if claudeSessionID == "" {
		return nil, fmt.Errorf("claude session id empty")
	}
	path := filepath.Join(claudeProjectDir(homeDir, workDir), claudeSessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()

	var turns []string
	totalBytes := 0
	scanner := bufio.NewScanner(f)
	// jsonl lines can be long (tool results); raise buffer ceiling.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		var row struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			continue
		}
		if row.Type != "user" || row.Message.Role != "user" {
			continue
		}
		text := extractUserText(row.Message.Content)
		if text == "" {
			continue
		}
		turns = append(turns, text)
		totalBytes += len(text)
		if maxTurns > 0 && len(turns) >= maxTurns {
			break
		}
		if maxBytes > 0 && totalBytes >= maxBytes {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return turns, fmt.Errorf("scan jsonl: %w", err)
	}
	return turns, nil
}

// extractUserText pulls the human-typed text out of a user message's content
// field. Content is either a JSON string (simple prompt) or an array of blocks
// where tool_result entries and other non-text blocks are skipped.
func extractUserText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
