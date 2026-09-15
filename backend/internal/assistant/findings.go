package assistant

import (
	"context"
	"fmt"
	"strconv"
)

// Finding is a machine's steward finding as the assistant hears it
// (docs/peers.md): opened when a condition starts holding, resolved when it
// stops. The steward sends facts; the sentence is written here.
type Finding struct {
	Kind     string
	Subject  string
	Severity string
	Remedy   string
	Facts    map[string]any
	// Opened is true for a finding that started holding.
	Opened bool
	// Machine is where it holds, "" for this machine.
	Machine string
}

// IngestFinding journals one finding. Unlike a report it is trusted: the
// summary is this server's own sentence about what a sensor read.
func (s *Service) IngestFinding(ctx context.Context, f Finding) {
	payload := map[string]any{
		"finding":  f.Kind,
		"severity": f.Severity,
		"remedy":   f.Remedy,
		"opened":   f.Opened,
	}
	if f.Subject != "" {
		payload["subject"] = f.Subject
	}
	if f.Machine != "" {
		payload["machine"] = f.Machine
	}
	for k, v := range f.Facts {
		if _, taken := payload[k]; !taken {
			payload[k] = v
		}
	}
	sessionID := ""
	if f.Kind == "session-blocked-long" {
		sessionID = f.Subject
	}
	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:      JournalFinding,
		SessionID: sessionID,
		Summary:   FindingSentence(f),
		Payload:   payload,
	}); err != nil {
		s.log.Warn("assistant: finding not journaled", "kind", f.Kind, "error", err)
	}
}

// FindingSentence is one finding in words, placing it on its machine. An
// unknown kind — a newer steward on a paired machine — still reads.
func FindingSentence(f Finding) string {
	where := "this machine"
	if f.Machine != "" {
		where = f.Machine
	}
	str := func(key string) string {
		v, _ := f.Facts[key].(string)
		return v
	}
	switch f.Kind {
	case "cli-signed-out":
		agent := orWord(str("agent"), "a provider CLI")
		if !f.Opened {
			return fmt.Sprintf("%s: %s is signed in again", where, agent)
		}
		if help := str("help"); help != "" {
			return fmt.Sprintf("%s: %s is signed out, so nothing can run on it there. %s", where, agent, help)
		}
		return fmt.Sprintf("%s: %s is signed out, so nothing can run on it there", where, agent)
	case "disk-low":
		if !f.Opened {
			return where + ": the data disk has room again"
		}
		line := fmt.Sprintf("%s: only %s free on the data disk", where, bytesWords(f.Facts["freeBytes"]))
		if f.Remedy == "reclaim" {
			line += fmt.Sprintf("; %s could be reclaimed from finished sessions", bytesWords(f.Facts["reclaimableBytes"]))
		}
		return line
	case "loop-paused":
		loop := orWord(str("loop"), "a scheduled loop")
		if !f.Opened {
			return fmt.Sprintf("%s: %s is no longer paused", where, loop)
		}
		return fmt.Sprintf("%s: %s paused itself after repeated failures and waits for a person", where, loop)
	case "session-blocked-long":
		session := orWord(str("session"), "a session")
		if !f.Opened {
			return fmt.Sprintf("%s: %s is no longer waiting", where, session)
		}
		return fmt.Sprintf("%s: %s has been waiting on %s since %s", where, session,
			orWord(str("waiting"), "a person"), orWord(str("since"), "a while"))
	case "update-waiting":
		if !f.Opened {
			return where + ": is up to date"
		}
		return fmt.Sprintf("%s: %s is available (it runs %s); applying it costs any turn in flight",
			where, orWord(str("latest"), "a newer release"), orWord(str("current"), "an older one"))
	case "backup-failing":
		if !f.Opened {
			return where + ": database backups are landing again"
		}
		if newest := str("newest"); newest != "" {
			return fmt.Sprintf("%s: no database backup since %s", where, newest)
		}
		return where + ": no database backup has been written"
	case "semantic-recall-down":
		if !f.Opened {
			return where + ": memory recall is semantic again"
		}
		part := "the vector index"
		switch str("reason") {
		case "chroma-unreachable":
			part = "Chroma"
		case "embedder-unreachable":
			part = "the embedding service"
		}
		return fmt.Sprintf("%s: %s has not answered since %s, so memory recall is keyword-only and consolidation is paused until it does",
			where, part, orWord(str("since"), "a while"))
	default:
		state := "opened"
		if !f.Opened {
			state = "resolved"
		}
		return fmt.Sprintf("%s: %s %s", where, f.Kind, state)
	}
}

func orWord(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// bytesWords renders a byte count from JSON (a float64) or Go (an integer).
func bytesWords(v any) string {
	var n float64
	switch t := v.(type) {
	case float64:
		n = t
	case int64:
		n = float64(t)
	case uint64:
		n = float64(t)
	case int:
		n = float64(t)
	default:
		return "little"
	}
	const gb = 1 << 30
	if n >= gb {
		return strconv.FormatFloat(n/gb, 'f', 1, 64) + " GB"
	}
	return strconv.FormatFloat(n/(1<<20), 'f', 0, 64) + " MB"
}
