package session

import "encoding/json"

// Wire event types for JSON serialization to the frontend.

type WireTextEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type WireThinkingEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type WireToolUseEvent struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type WireToolResultEvent struct {
	Type      string `json:"type"`
	ToolUseID string `json:"toolUseId"`
	Content   string `json:"content"`
}

type WireResultEvent struct {
	Type       string  `json:"type"`
	CostUSD    float64 `json:"costUsd"`
	Duration   int64   `json:"duration"`
	Usage      any     `json:"usage"`
	StopReason string  `json:"stopReason"`
}

type WireErrorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}
