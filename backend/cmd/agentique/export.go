package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testmode"
	"github.com/spf13/cobra"
)

var exportOutput string

func init() {
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "write to file instead of stdout")
	rootCmd.AddCommand(exportCmd)
}

var exportCmd = &cobra.Command{
	Use:   "export <session-id>",
	Short: "Export a session as a Playwright test fixture",
	Long:  "Reads session events directly from the database and outputs a JSON fixture compatible with the hybrid E2E test infrastructure.",
	Args:  cobra.ExactArgs(1),
	RunE:  runExport,
}

func runExport(cmd *cobra.Command, args []string) error {
	sessionPrefix := args[0]

	dbFile := resolveDBPath()
	db, err := store.Open(dbFile)
	if err != nil {
		return fmt.Errorf("open database %s: %w", dbFile, err)
	}
	defer db.Close()

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	q := store.New(db)
	ctx := context.Background()

	// Resolve prefix to full session ID.
	dbSess, err := resolveSessionFromDB(ctx, q, sessionPrefix)
	if err != nil {
		return err
	}

	project, err := q.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}

	events, err := q.ListEventsBySession(ctx, dbSess.ID)
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}

	if len(events) == 0 {
		return fmt.Errorf("session %s has no events", shortID(dbSess.ID))
	}

	scrubber := testmode.NewScrubber(dbSess.WorkDir, os.Getenv("HOME"))

	// Group events by turn.
	type turnData struct {
		prompt string
		events []testmode.ScriptedEvent
		start  time.Time
	}
	turnMap := make(map[int64]*turnData)
	var turnOrder []int64

	for _, ev := range events {
		td, ok := turnMap[ev.TurnIndex]
		if !ok {
			td = &turnData{}
			turnMap[ev.TurnIndex] = td
			turnOrder = append(turnOrder, ev.TurnIndex)
		}

		if ev.Type == "prompt" {
			var p struct {
				Prompt string `json:"prompt"`
			}
			if json.Unmarshal([]byte(ev.Data), &p) == nil {
				td.prompt = p.Prompt
			}
			continue
		}

		ts, tsErr := time.Parse("2006-01-02T15:04:05.000", ev.CreatedAt)
		if tsErr != nil {
			ts = time.Time{}
		}

		var delayMs int
		if !ts.IsZero() {
			if td.start.IsZero() {
				td.start = ts
			}
			delayMs = int(ts.Sub(td.start).Milliseconds())
		}

		data := session.NormalizeEventJSON(ev.Type, []byte(ev.Data))
		var m map[string]any
		if json.Unmarshal(data, &m) == nil {
			m["type"] = ev.Type
			scrubbed, _ := json.Marshal(m)
			td.events = append(td.events, testmode.ScriptedEvent{
				Delay: delayMs,
				Event: json.RawMessage(scrubber.Scrub(string(scrubbed))),
			})
		}
	}

	// Assemble export.
	type exportTurn struct {
		Prompt   string           `json:"prompt"`
		Scenario testmode.Scenario `json:"scenario"`
	}
	type exportData struct {
		Metadata struct {
			SessionID   string `json:"sessionId"`
			SessionName string `json:"sessionName"`
			ProjectName string `json:"projectName"`
			ProjectPath string `json:"projectPath"`
			Model       string `json:"model"`
			CapturedAt  string `json:"capturedAt"`
		} `json:"metadata"`
		Turns []exportTurn `json:"turns"`
	}

	export := exportData{}
	export.Metadata.SessionID = dbSess.ID
	export.Metadata.SessionName = dbSess.Name
	export.Metadata.ProjectName = project.Name
	export.Metadata.ProjectPath = scrubber.Scrub(project.Path)
	export.Metadata.Model = dbSess.Model
	export.Metadata.CapturedAt = time.Now().UTC().Format(time.RFC3339)

	for _, idx := range turnOrder {
		td := turnMap[idx]
		export.Turns = append(export.Turns, exportTurn{
			Prompt:   scrubber.Scrub(td.prompt),
			Scenario: testmode.Scenario{Events: td.events},
		})
	}

	out, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if exportOutput != "" {
		if err := os.WriteFile(exportOutput, out, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", exportOutput, err)
		}
		fmt.Fprintf(os.Stderr, "exported %d turns (%d events) -> %s\n",
			len(export.Turns), len(events), exportOutput)
	} else {
		fmt.Println(string(out))
	}

	return nil
}

func resolveSessionFromDB(ctx context.Context, q *store.Queries, prefix string) (store.Session, error) {
	// Try exact match first.
	sess, err := q.GetSession(ctx, prefix)
	if err == nil {
		return sess, nil
	}

	// Fall back to prefix match across all sessions.
	all, err := q.ListAllSessions(ctx)
	if err != nil {
		return store.Session{}, fmt.Errorf("list sessions: %w", err)
	}

	var matches []store.Session
	for _, s := range all {
		if len(s.ID) >= len(prefix) && s.ID[:len(prefix)] == prefix {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return store.Session{}, fmt.Errorf("no session matching %q", prefix)
	case 1:
		return matches[0], nil
	default:
		fmt.Fprintf(os.Stderr, "ambiguous prefix %q matches %d sessions:\n", prefix, len(matches))
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s  %s\n", shortID(m.ID), m.Name)
		}
		return store.Session{}, fmt.Errorf("use a longer prefix")
	}
}
