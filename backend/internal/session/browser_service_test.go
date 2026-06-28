package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandalonePlaywrightMCPConfig(t *testing.T) {
	cfg := StandalonePlaywrightMCPConfig(54321, "/data/session-files/sess-1")

	for _, want := range []string{
		`"agentique-playwright"`,
		`"@playwright/mcp"`,
		`"--cdp-endpoint"`,
		`"http://127.0.0.1:54321"`,
		`"--output-dir"`,
		"/data/session-files/sess-1",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\ngot: %s", want, cfg)
		}
	}

	// In --cdp-endpoint mode the MCP attaches to agentique's Chrome — it must not
	// carry any launch/profile flags.
	for _, no := range []string{"--isolated", "--headless", "--user-data-dir"} {
		if strings.Contains(cfg, no) {
			t.Errorf("config should not contain launch flag %q\ngot: %s", no, cfg)
		}
	}

	// Must be valid JSON.
	var v any
	if err := json.Unmarshal([]byte(cfg), &v); err != nil {
		t.Fatalf("config is not valid JSON: %v\ngot: %s", err, cfg)
	}
}

func TestIsBrowserTool(t *testing.T) {
	cases := map[string]bool{
		"mcp__agentique-playwright__browser_navigate":        true,
		"mcp__agentique-playwright__browser_take_screenshot": true,
		"mcp__playwright__browser_navigate":                  false, // global plugin, not ours
		"mcp__agentique__SendMessage":                        false, // the channel/HTTP server
		"Read":                                               false,
		"":                                                   false,
	}
	for name, want := range cases {
		if got := isBrowserTool(name); got != want {
			t.Errorf("isBrowserTool(%q) = %v, want %v", name, got, want)
		}
	}
}
