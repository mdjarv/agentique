package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var cleanupForce bool

func init() {
	cleanupCmd.Flags().BoolVarP(&cleanupForce, "force", "f", false, "skip confirmation")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Delete merged terminal sessions",
	RunE:  runCleanup,
}

func runCleanup(cmd *cobra.Command, args []string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	base := baseURL()

	projects, err := fetchJSON[[]projectBrief](client, base+"/api/projects")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch projects: %v\n", err)
		return nil
	}
	projectByID := make(map[string]projectBrief)
	for _, p := range projects {
		projectByID[p.ID] = p
	}

	sessions, err := fetchJSON[[]sessionBrief](client, base+"/api/sessions")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch sessions: %v\n", err)
		return nil
	}

	// Candidates: terminal state + merged worktree (or no worktree at all).
	var candidates []sessionBrief
	for _, s := range sessions {
		if !terminalStates[s.State] {
			continue
		}
		if s.WorktreeBranch == "" || s.WorktreeMerged {
			candidates = append(candidates, s)
		}
	}

	if len(candidates) == 0 {
		fmt.Println("nothing to clean up")
		return nil
	}

	fmt.Printf("Sessions to clean up (%d):\n\n", len(candidates))
	fmt.Printf("  %-10s %-12s %-32s %-10s %s\n", "ID", "PROJECT", "NAME", "STATE", "BRANCH")
	for _, s := range candidates {
		p := projectByID[s.ProjectID]
		slug := p.Slug
		if slug == "" {
			slug = shortID(p.ID)
		}
		name := s.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		branch := s.WorktreeBranch
		if branch == "" {
			branch = "-"
		}
		fmt.Printf("  %-10s %-12s %-32s %-10s %s\n", shortID(s.ID), slug, name, s.State, branch)
	}

	if !cleanupForce {
		fmt.Printf("\nDelete %d sessions? [y/N] ", len(candidates))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("cancelled")
			return nil
		}
	}

	fmt.Println()
	var deleted int
	for _, s := range candidates {
		if err := doRequest(client, http.MethodDelete, base+"/api/sessions/"+s.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  failed %s: %v\n", shortID(s.ID), err)
			continue
		}
		fmt.Printf("  deleted %s  %s\n", shortID(s.ID), s.Name)
		deleted++
	}

	fmt.Printf("\n%d/%d deleted\n", deleted, len(candidates))
	return nil
}
