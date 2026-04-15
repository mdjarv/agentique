package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(projectsCmd)
}

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List projects",
	RunE:  runProjects,
}

func runProjects(cmd *cobra.Command, args []string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	base := baseURL()

	projects, err := fetchJSON[[]projectBrief](client, base+"/api/projects")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch projects: %v\n", err)
		return nil
	}

	if len(projects) == 0 {
		fmt.Println("No projects")
		return nil
	}

	sessions, err := fetchJSON[[]sessionBrief](client, base+"/api/sessions")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch sessions: %v\n", err)
		return nil
	}

	// Count sessions per project grouped by state.
	type projectStats struct {
		total  int
		states map[string]int
	}
	stats := make(map[string]*projectStats)
	for _, s := range sessions {
		ps, ok := stats[s.ProjectID]
		if !ok {
			ps = &projectStats{states: make(map[string]int)}
			stats[s.ProjectID] = ps
		}
		ps.total++
		ps.states[s.State]++
	}

	fmt.Printf("  %-16s %-12s %-40s %s\n", "NAME", "SLUG", "PATH", "SESSIONS")
	for _, p := range projects {
		ps := stats[p.ID]
		sessInfo := "0"
		if ps != nil {
			parts := make([]string, 0)
			for _, state := range []string{"running", "idle", "done", "failed", "stopped"} {
				if n := ps.states[state]; n > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", n, state))
				}
			}
			sessInfo = fmt.Sprintf("%d (%s)", ps.total, strings.Join(parts, ", "))
		}

		path := p.Path
		if len(path) > 38 {
			path = "..." + path[len(path)-35:]
		}

		fmt.Printf("  %-16s %-12s %-40s %s\n", p.Name, p.Slug, path, sessInfo)
	}

	return nil
}
