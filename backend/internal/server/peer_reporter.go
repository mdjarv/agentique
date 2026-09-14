package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/peer"
)

// peerReportBudget bounds the outbox write inside a tool call an agent is
// waiting on.
const peerReportBudget = 3 * time.Second

// peerAwareReporter is the one AssistantReport implementation: it files a
// report with every party following the run, here and on paired servers.
//
// The two halves are independent. A local follower (the assistant's journal, a
// live call) and a paired server's assistant may both be following one run, or
// either alone, or neither — so both are always tried, and the answer the agent
// gets is about whether the report went anywhere, which is what decides whether
// it should keep reporting.
type peerAwareReporter struct {
	// local is the service or the bare registry; nil when this server has
	// neither the assistant nor voice.
	local  mcphttp.AssistantReporter
	outbox *peer.Outbox
}

// Report implements mcphttp.AssistantReporter.
func (r *peerAwareReporter) Report(sessionID, kind, headline string) (string, error) {
	report, err := assistant.ParseReport(kind, headline)
	if err != nil {
		return "", err
	}

	kept := false
	if r.outbox != nil {
		ctx, cancel := context.WithTimeout(context.Background(), peerReportBudget)
		kept, err = r.outbox.Report(ctx, sessionID, report)
		cancel()
		if err != nil {
			slog.Warn("assistant report: peer outbox write failed", "session", sessionID, "error", err)
		}
	}

	if r.local != nil {
		msg, err := r.local.Report(sessionID, kind, headline)
		// A paired server keeping it outranks a local "nobody is following":
		// the agent must not be told to stop reporting to someone who is
		// listening from another machine.
		if !kept {
			return msg, err
		}
		if err != nil {
			slog.Warn("assistant report: local delivery failed", "session", sessionID, "error", err)
		}
	}
	if kept {
		return "Kept for the assistant following this run from another machine. It reaches them the next " +
			"time that machine checks in, so you do not need to repeat it.", nil
	}
	return "Nobody is following this run, so that went nowhere and was not kept. You can stop reporting; " +
		"say what you found in your reply instead.", nil
}
