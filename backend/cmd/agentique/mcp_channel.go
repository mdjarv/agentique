package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// mcpChannelCmd runs a minimal MCP server over stdio that exposes a
// SendMessage tool. This replaces the CLI's built-in SendMessage (gated
// behind an experimental flag) with one we fully control.
//
// The tool is never actually executed — Agentique's permission interceptor
// denies it with a success message, and the EventPipeline routes the message
// through the channel system. This server exists solely to make the tool
// visible in Claude's tool list.
var mcpChannelCmd = &cobra.Command{
	Use:    "mcp-channel",
	Short:  "MCP server exposing SendMessage for channel messaging",
	Hidden: true,
	RunE:   runMCPChannel,
}

func init() {
	rootCmd.AddCommand(mcpChannelCmd)
}

// JSON-RPC types — minimal subset for MCP stdio protocol.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // null for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func runMCPChannel(_ *cobra.Command, _ []string) error {
	scanner := bufio.NewScanner(os.Stdin)
	// MCP messages can be large (tool inputs).
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue // skip malformed
		}

		// Notifications (no id) don't get responses.
		if req.ID == nil || string(req.ID) == "null" {
			continue
		}

		var resp jsonrpcResponse
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":     map[string]interface{}{"name": "agentique-channel", "version": "1.0.0"},
			}
		case "tools/list":
			resp.Result = map[string]interface{}{
				"tools": []interface{}{sendMessageToolDef()},
			}
		case "tools/call":
			// Fallback: if the permission interceptor didn't catch this,
			// return a success so Claude isn't confused.
			resp.Result = map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "Message delivered."},
				},
			}
		default:
			resp.Error = &jsonrpcError{Code: -32601, Message: "method not found"}
		}

		out, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(out))
	}
	return scanner.Err()
}

func sendMessageToolDef() map[string]interface{} {
	return map[string]interface{}{
		"name":        "SendMessage",
		"description": "Send a message to a teammate in this channel.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Recipient: teammate name, or \"@spawn\" to create workers, or \"@dissolve\" to close the channel.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message content. For @spawn, a JSON string with channelName and workers array.",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"plan", "progress", "done", "message"},
					"description": "Message type for status signaling. Workers must set this to communicate progress.",
				},
			},
			"required": []string{"to", "message"},
		},
	}
}
