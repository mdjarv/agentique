package usage

import (
	"context"
	"errors"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// Collecting from a connector that can answer for its own account.
//
// This is deliberately vendor-NEUTRAL. agentkit's `AccountInspectable` hangs
// off `CLIConnector`, so any provider whose connector implements it becomes a
// record here with no code of its own — the Claude collector is bespoke only
// because Anthropic exposes usage over HTTP rather than through its CLI.
//
// The probe dials a fresh app-server, asks, and hangs up (~1s for codex), so it
// belongs on the poll and never on a request path.

// probeBudget bounds one connector probe. Generous, because the cost is
// dominated by spawning a process, and an inspect call that hangs is worse than
// one that answers "unknown".
const probeBudget = 20 * time.Second

// collectConnector turns one connector's answer into a record.
//
// `previous` is what we last knew, and it is what makes a failure non-
// destructive: a probe that cannot answer keeps the numbers and replaces only
// the explanation.
//
// Returning ok=false means "say nothing at all". That is the right answer for a
// provider this machine cannot ask — a CLI that is not installed is a normal
// state, not a row reporting that it is missing.
func collectConnector(
	ctx context.Context,
	id, name string,
	src runtime.AccountInspectable,
	previous *Agent,
	now time.Time,
) (Agent, bool) {
	agent := Agent{ID: id, Name: name}

	ctx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	limits, err := src.AccountRateLimits(ctx)
	if err != nil {
		return degrade(agent, previous, err)
	}

	agent.TierLabel = limits.PlanLabel
	for _, w := range limits.Windows {
		// agentkit's contract already says an adapter with nothing to report
		// leaves the window out rather than emitting a 0% placeholder. Honour
		// the same rule on the way in: unknown and unused are different answers,
		// and only one of them is a bar.
		if !w.Known() {
			continue
		}
		lim := Limit{Label: w.Label, Percent: w.Percent}
		if !w.ResetsAt.IsZero() {
			lim.ResetsAt = w.ResetsAt.UTC().Format(time.RFC3339)
		}
		agent.Limits = append(agent.Limits, lim)
	}

	if len(agent.Limits) == 0 {
		// A clean answer that reports nothing is not a failure and is not zero.
		// With no previous numbers there is nothing worth a row.
		if previous == nil || len(previous.Limits) == 0 {
			return Agent{}, false
		}
		kept := *previous
		kept.Ready = false
		kept.UsageStatusText = "No limits reported for this account."
		return kept, true
	}

	agent.Ready = true
	agent.UpdatedAt = now.Format(time.RFC3339)
	return agent, true
}

// degrade decides what a failed probe should say.
//
// The split that matters is STRUCTURAL versus TRANSIENT, not failed versus
// fine:
//
//   - Structural — the connector cannot report at all (ErrNotSupported), which
//     is what an uninstalled or downgraded CLI looks like. There is nothing to
//     be stale about, so the record is forgotten entirely. Keeping a meter
//     alive for a provider that is gone is the one thing worse than dropping
//     it, because it never stops being wrong.
//   - Transient — a dial that failed, a timeout, backend prose. The numbers
//     were true a minute ago and are the best answer available, so they stay
//     with a line saying why they are old.
//
// Signed-out sits with the transient half deliberately: the operator can fix
// it, the last numbers are still meaningful context, and the command that fixes
// it is not guessable. It is also the only failure worth a row on a machine
// that has never had numbers at all.
func degrade(agent Agent, previous *Agent, err error) (Agent, bool) {
	if errors.Is(err, runtime.ErrNotSupported) {
		return Agent{}, false
	}
	signedOut := errors.Is(err, runtime.ErrNotSignedIn)

	if previous == nil || len(previous.Limits) == 0 {
		if !signedOut {
			return Agent{}, false
		}
		agent.UsageStatusText = "Not signed in."
		agent.AuthHelpText = "Run `codex login` to see usage."
		return agent, true
	}

	kept := *previous
	kept.Ready = false
	switch {
	case signedOut:
		kept.UsageStatusText = "Not signed in."
		kept.AuthHelpText = "Run `codex login` to restore usage."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		kept.UsageStatusText = "Timed out reading limits."
	default:
		kept.UsageStatusText = "Could not read limits."
	}
	return kept, true
}
