package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/doctor"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	addr    string
)

var rootCmd = &cobra.Command{
	Use:     "agentique",
	Short:   "Agentique — manage concurrent Claude Code agents",
	Version: version,
	RunE:    runStatus,
}

func init() {
	rootCmd.SetVersionTemplate("agentique {{.Version}}\n")
	rootCmd.PersistentFlags().StringVar(&addr, "addr", "localhost:9201", "server address")
}

func main() {
	doctor.Version = version
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runStatus checks server health and shows active session summary.
func runStatus(cmd *cobra.Command, args []string) error {
	// First-run welcome.
	if !config.Exists() && !fileExists(paths.DBPath()) {
		fmt.Println("Welcome to Agentique!")
		fmt.Println()
		fmt.Println("  Quick start:  agentique setup     (guided configuration)")
		fmt.Println("  Jump in:      agentique serve     (start with defaults)")
		fmt.Println("  Check deps:   agentique doctor    (verify prerequisites)")
		fmt.Println()
		return nil
	}

	base := baseURL()

	// Health check.
	client := &http.Client{Timeout: 2 * time.Second}
	_, err := client.Get(base + "/api/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agentique not running at %s\n", addr)
		fmt.Fprintf(os.Stderr, "Start with: agentique serve\n")
		return nil
	}

	fmt.Printf("Agentique running at %s\n\n", addr)

	// Fetch projects.
	projects, err := fetchJSON[[]projectBrief](client, base+"/api/projects")
	if err != nil {
		return fmt.Errorf("failed to fetch projects: %w", err)
	}

	projectByID := make(map[string]projectBrief)
	for _, p := range projects {
		projectByID[p.ID] = p
	}

	// Fetch all sessions.
	sessions, err := fetchJSON[[]sessionBrief](client, base+"/api/sessions")
	if err != nil {
		return fmt.Errorf("failed to fetch sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("  No sessions")
		return nil
	}

	// Group by project.
	byProject := make(map[string][]sessionBrief)
	for _, s := range sessions {
		byProject[s.ProjectID] = append(byProject[s.ProjectID], s)
	}

	for projectID, ss := range byProject {
		p := projectByID[projectID]
		name := p.Name
		if name == "" {
			name = projectID[:8]
		}
		slug := p.Slug
		if slug == "" {
			slug = "-"
		}

		stateCounts := make(map[string]int)
		for _, s := range ss {
			stateCounts[s.State]++
		}

		parts := make([]string, 0)
		for _, state := range []string{"running", "idle", "done", "failed", "stopped"} {
			if n := stateCounts[state]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, state))
			}
		}

		label := "sessions"
		if len(ss) == 1 {
			label = "session"
		}

		fmt.Printf("  %-16s %-12s %d %s (%s)\n", name, slug, len(ss), label, strings.Join(parts, ", "))
	}

	return nil
}

func baseURL() string {
	a := addr
	if !strings.Contains(a, "://") {
		a = "http://" + a
	}
	return strings.TrimRight(a, "/")
}

func fetchJSON[T any](client *http.Client, url string) (T, error) {
	var zero T
	resp, err := client.Get(url)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, err
	}
	return result, nil
}

func postJSON(client *http.Client, url string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	resp, err := client.Post(url, "application/json", &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func doRequest(client *http.Client, method, url string) error {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func resolveSession(client *http.Client, prefix string) (sessionBrief, error) {
	sessions, err := fetchJSON[[]sessionBrief](client, baseURL()+"/api/sessions")
	if err != nil {
		return sessionBrief{}, fmt.Errorf("failed to fetch sessions: %w", err)
	}

	var matches []sessionBrief
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, prefix) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return sessionBrief{}, fmt.Errorf("no session matching %q", prefix)
	case 1:
		return matches[0], nil
	default:
		lines := make([]string, len(matches))
		for i, m := range matches {
			lines[i] = fmt.Sprintf("  %s  %s", m.ID[:8], m.Name)
		}
		return sessionBrief{}, fmt.Errorf("ambiguous prefix %q matches %d sessions:\n%s",
			prefix, len(matches), strings.Join(lines, "\n"))
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
