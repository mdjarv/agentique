package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// notResolved is what an empty resolved id prints as. Never a blank column: a
// blank reads as a broken command, where these words are the answer.
const notResolved = "not yet resolved"

var modelAll bool

// sessionModel is the wire shape of GET /api/sessions/{id}/model.
type sessionModel struct {
	SessionID       string `json:"sessionId"`
	Provider        string `json:"provider"`
	RequestedSlug   string `json:"requestedSlug"`
	ResolvedModelID string `json:"resolvedModelId"`
	ResolvedAt      string `json:"resolvedAt"`
	Source          string `json:"source"`
}

func init() {
	sessionsModelCmd.Flags().BoolVarP(&modelAll, "all", "a", false, "include archived, stopped and failed sessions")
	sessionsCmd.AddCommand(sessionsModelCmd)
}

var sessionsModelCmd = &cobra.Command{
	Use:   "model [session-id]",
	Short: "Show which upstream model a session actually runs on",
	Long: `Show which upstream model a session actually runs on.

The model a session was started with is usually an alias -- "opus", "sonnet" --
and an alias moves between provider releases, so it does not name the model that
answered. This asks the running agentique server, which means a live session's
own CLI process reports for itself rather than the last thing written to the
database.

With no argument, prints a row per active session (-a includes archived and
finished ones). With a session id, or any unambiguous prefix of one, prints the
full report:

  Session        the session's id
  Provider       claude / codex
  Requested      the slug the session asked for
  Resolved       the concrete upstream model id
  Resolved at    when that id was learned (UTC)
  Source         init_event  this session's own run reported it
                 catalog     it never did, so this is what the same slug
                             resolved to elsewhere -- a hint, not history
                 unresolved  nothing has reported a model for this slug yet`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSessionsModel,
}

func runSessionsModel(cmd *cobra.Command, args []string) error {
	base := baseURL()
	client := apiClient()

	sessions, err := fetchJSON[[]sessionBrief](client, base+"/api/sessions")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch sessions: %v\n", err)
		return nil
	}

	if len(args) == 1 {
		id, err := resolveSessionID(sessions, args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil
		}
		report, err := fetchJSON[sessionModel](client, base+"/api/sessions/"+id+"/model")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to fetch model for %s: %v\n", id, err)
			return nil
		}
		printSessionModel(report)
		return nil
	}

	if !modelAll {
		active := sessions[:0]
		for _, s := range sessions {
			if !terminalStates[s.State] && !s.archived() {
				active = append(active, s)
			}
		}
		sessions = active
	}

	if len(sessions) == 0 {
		if modelAll {
			fmt.Println("No sessions")
		} else {
			fmt.Println("No active sessions (use -a for all)")
		}
		return nil
	}

	fmt.Printf("  %-10s %-30s %-14s %-28s %s\n", "ID", "NAME", "REQUESTED", "RESOLVED", "SOURCE")
	for _, s := range sessions {
		report, err := fetchJSON[sessionModel](client, base+"/api/sessions/"+s.ID+"/model")
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %-10s failed: %v\n", shortID(s.ID), err)
			continue
		}
		fmt.Printf("  %-10s %-30s %-14s %-28s %s\n",
			shortID(s.ID), truncate(s.Name, 30), report.RequestedSlug,
			resolvedOrNot(report.ResolvedModelID), report.Source)
	}

	return nil
}

func printSessionModel(r sessionModel) {
	resolvedAt := r.ResolvedAt
	switch {
	case r.ResolvedModelID == "":
		resolvedAt = notResolved
	case resolvedAt == "":
		// The live session reported an id the row has not caught up with, so
		// there is no stamp to print. Saying so beats inventing "now".
		resolvedAt = "unknown"
	}

	fmt.Printf("%-14s %s\n", "Session", r.SessionID)
	fmt.Printf("%-14s %s\n", "Provider", r.Provider)
	fmt.Printf("%-14s %s\n", "Requested", r.RequestedSlug)
	fmt.Printf("%-14s %s\n", "Resolved", resolvedOrNot(r.ResolvedModelID))
	fmt.Printf("%-14s %s\n", "Resolved at", resolvedAt)
	fmt.Printf("%-14s %s\n", "Source", r.Source)
}

func resolvedOrNot(id string) string {
	if id == "" {
		return notResolved
	}
	return id
}

// resolveSessionID accepts a full session id or any prefix of one, because the
// ids this CLI prints are truncated to eight characters. An ambiguous prefix is
// an error naming the candidates, never a guess.
func resolveSessionID(sessions []sessionBrief, want string) (string, error) {
	var matches []sessionBrief
	for _, s := range sessions {
		if s.ID == want {
			return s.ID, nil
		}
		if strings.HasPrefix(s.ID, want) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no session matches %q", want)
	case 1:
		return matches[0].ID, nil
	default:
		names := make([]string, 0, len(matches))
		for _, s := range matches {
			names = append(names, fmt.Sprintf("%s (%s)", shortID(s.ID), s.Name))
		}
		return "", fmt.Errorf("%q matches %d sessions: %s", want, len(matches), strings.Join(names, ", "))
	}
}
