package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var addr string

var rootCmd = &cobra.Command{
	Use:   "agentique",
	Short: "Agentique — manage concurrent Claude Code agents",
	RunE:  runStatus,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&addr, "addr", "localhost:9201", "server address")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runStatus checks server health and shows active session summary.
func runStatus(cmd *cobra.Command, args []string) error {
	base := baseURL()

	// Health check.
	client := &http.Client{Timeout: 2 * time.Second}
	_, err := client.Get(base + "/api/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agentique not running at %s\n", addr)
		return nil
	}

	fmt.Printf("Agentique running at %s\n\n", addr)

	// Fetch projects.
	type projectInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	projects, err := fetchJSON[[]projectInfo](client, base+"/api/projects")
	if err != nil {
		return fmt.Errorf("failed to fetch projects: %w", err)
	}

	projectNames := make(map[string]string)
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}

	// Fetch all sessions.
	type sessionInfo struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
		State     string `json:"state"`
		Connected bool   `json:"connected"`
	}
	sessions, err := fetchJSON[[]sessionInfo](client, base+"/api/sessions")
	if err != nil {
		return fmt.Errorf("failed to fetch sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("  No sessions")
		return nil
	}

	// Group by project.
	byProject := make(map[string][]sessionInfo)
	for _, s := range sessions {
		byProject[s.ProjectID] = append(byProject[s.ProjectID], s)
	}

	for projectID, ss := range byProject {
		name := projectNames[projectID]
		if name == "" {
			name = projectID[:8]
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

		fmt.Printf("  %-16s %d %s (%s)\n", name, len(ss), label, strings.Join(parts, ", "))
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
