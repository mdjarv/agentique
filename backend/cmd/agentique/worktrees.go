package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(worktreesCmd)
}

var worktreesCmd = &cobra.Command{
	Use:   "worktrees",
	Short: "List sessions with active worktrees",
	RunE:  runWorktrees,
}

func runWorktrees(cmd *cobra.Command, args []string) error {
	client := &http.Client{Timeout: 5 * time.Second}
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

	// Filter to sessions with worktrees.
	var wt []sessionBrief
	for _, s := range sessions {
		if s.WorktreeBranch != "" {
			wt = append(wt, s)
		}
	}

	if len(wt) == 0 {
		fmt.Println("No worktrees")
		return nil
	}

	fmt.Printf("  %-10s %-12s %-28s %-10s %-8s %s\n", "ID", "PROJECT", "BRANCH", "STATE", "MERGED", "AHEAD/BEHIND")
	for _, s := range wt {
		p := projectByID[s.ProjectID]
		slug := p.Slug
		if slug == "" {
			slug = shortID(p.ID)
		}

		merged := "no"
		if s.WorktreeMerged {
			merged = "yes"
		}

		branch := s.WorktreeBranch
		if len(branch) > 26 {
			branch = branch[:23] + "..."
		}

		fmt.Printf("  %-10s %-12s %-28s %-10s %-8s %d/%d\n",
			shortID(s.ID), slug, branch, s.State, merged, s.CommitsAhead, s.CommitsBehind)
	}

	return nil
}
