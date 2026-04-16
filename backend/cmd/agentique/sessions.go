package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	sessProject string
	sessAll     bool
)

// Terminal states hidden by default.
var terminalStates = map[string]bool{
	"done":    true,
	"stopped": true,
	"failed":  true,
}

func init() {
	sessionsCmd.Flags().StringVarP(&sessProject, "project", "p", "", "filter by project slug")
	sessionsCmd.Flags().BoolVarP(&sessAll, "all", "a", false, "include completed/stopped/failed sessions")
	rootCmd.AddCommand(sessionsCmd)
}

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List sessions",
	RunE:  runSessions,
}

func runSessions(cmd *cobra.Command, args []string) error {
	base := baseURL()
	client := apiClient()

	// Fetch projects for slug resolution and display.
	projects, err := fetchJSON[[]projectBrief](client, base+"/api/projects")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch projects: %v\n", err)
		return nil
	}

	projectByID := make(map[string]projectBrief)
	projectBySlug := make(map[string]projectBrief)
	for _, p := range projects {
		projectByID[p.ID] = p
		if p.Slug != "" {
			projectBySlug[p.Slug] = p
		}
	}

	// Resolve -p slug to project ID.
	endpoint := base + "/api/sessions"
	if sessProject != "" {
		p, ok := projectBySlug[sessProject]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown project slug: %s\n", sessProject)
			fmt.Fprintf(os.Stderr, "available: %s\n", availableSlugs(projects))
			return nil
		}
		endpoint += "?project=" + p.ID
	}

	sessions, err := fetchJSON[[]sessionBrief](client, endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch sessions: %v\n", err)
		return nil
	}

	// Filter terminal states unless -a.
	if !sessAll {
		filtered := sessions[:0]
		for _, s := range sessions {
			if !terminalStates[s.State] {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	if len(sessions) == 0 {
		if sessAll {
			fmt.Println("No sessions")
		} else {
			fmt.Println("No active sessions (use -a for all)")
		}
		return nil
	}

	// Print table.
	fmt.Printf("  %-10s %-12s %-32s %-10s %s\n", "ID", "PROJECT", "NAME", "STATE", "MODEL")
	for _, s := range sessions {
		p := projectByID[s.ProjectID]
		slug := p.Slug
		if slug == "" {
			slug = p.ID[:8]
		}
		name := s.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		shortID := s.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fmt.Printf("  %-10s %-12s %-32s %-10s %s\n", shortID, slug, name, s.State, s.Model)
	}

	return nil
}

func availableSlugs(projects []projectBrief) string {
	slugs := make([]string, 0, len(projects))
	for _, p := range projects {
		if p.Slug != "" {
			slugs = append(slugs, p.Slug)
		}
	}
	return strings.Join(slugs, ", ")
}
