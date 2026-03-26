package main

import (
	"encoding/json"
	"fmt"
)

func renderEvent(raw json.RawMessage) {
	var typed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &typed); err != nil {
		return
	}

	switch typed.Type {
	case "text":
		var e struct {
			Content string `json:"content"`
		}
		json.Unmarshal(raw, &e)
		fmt.Print(e.Content)

	case "thinking":
		// Skip — too verbose.

	case "tool_use":
		var e struct {
			ToolName string `json:"toolName"`
		}
		json.Unmarshal(raw, &e)
		fmt.Printf("\n[tool] %s\n", e.ToolName)

	case "tool_result":
		// Skip — too verbose.

	case "result":
		var e struct {
			StopReason string `json:"stopReason"`
		}
		json.Unmarshal(raw, &e)
		fmt.Printf("\n[result] stop=%s\n", e.StopReason)

	case "error":
		var e struct {
			Message string `json:"message"`
			Fatal   bool   `json:"fatal"`
		}
		json.Unmarshal(raw, &e)
		label := "error"
		if e.Fatal {
			label = "FATAL"
		}
		fmt.Printf("[%s] %s\n", label, e.Message)
	}
}
