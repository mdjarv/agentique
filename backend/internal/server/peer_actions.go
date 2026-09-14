package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
)

// peerProposalLink is what the routed executor asks of a paired machine.
// *peerlink.Client implements it.
type peerProposalLink interface {
	Facts(ctx context.Context, machineID, sessionID, kind string, dst any) error
	ResolveModel(ctx context.Context, machineID, sessionID string, req peer.ResolveModelRequest, dst any) error
	Do(ctx context.Context, machineID, sessionID, verb string, args map[string]any) (string, error)
}

// routedActions is the assistant's uncontained-tier executor across machines
// (docs/peers.md): a session on this machine goes to the local executor, a
// session on a paired machine goes to that machine's peer surface, where the
// same check that wrote the card runs again before anything is performed.
//
// Channels stay local: a paired machine's channels are not listed here, so no
// card can name one.
type routedActions struct {
	local *assistantActions
	peers peerSource
	link  peerProposalLink
}

var (
	_ assistant.Actions              = (*routedActions)(nil)
	_ assistant.SessionModelResolver = (*routedActions)(nil)
)

// remote answers where a session lives when it is not this machine's.
func (a *routedActions) remote(ctx context.Context, sessionID string) (peerLocation, bool) {
	if a.peers == nil || a.link == nil {
		return peerLocation{}, false
	}
	if _, err := a.local.svc.GetSessionInfo(ctx, sessionID); err == nil {
		return peerLocation{}, false
	}
	loc, ok := a.peers.Locate(ctx, sessionID)
	return loc, ok
}

// do performs a verb on the owner and maps its answer back: an outcome word is
// an [assistant.OutcomeError], exactly as a local executor reports one.
func (a *routedActions) do(ctx context.Context, loc peerLocation, verb string, args map[string]any) (string, error) {
	outcome, err := a.link.Do(ctx, loc.Machine.MachineID, loc.Session.ID, verb, args)
	if err == nil {
		// The session just changed on its machine; the next list should ask.
		a.peers.Invalidate(loc.Machine.MachineID)
		return outcome, nil
	}
	var refusal *peerlink.RefusalError
	if errors.As(err, &refusal) {
		if refusal.Reason == peer.ReasonOutcome {
			return "", &assistant.OutcomeError{Outcome: refusal.Message, Detail: "on " + machineLabel(loc.Machine)}
		}
		return "", &assistant.OutcomeError{Outcome: machineLabel(loc.Machine) + " refused: " + refusal.Message,
			Detail: refusal.Reason}
	}
	return "", fmt.Errorf("%s on %s: %w", verb, loc.Machine.MachineID, err)
}

func (a *routedActions) Merge(ctx context.Context, sessionID string) (string, error) {
	if loc, ok := a.remote(ctx, sessionID); ok {
		return a.do(ctx, loc, assistant.VerbMergeSession, nil)
	}
	return a.local.Merge(ctx, sessionID)
}

func (a *routedActions) Rebase(ctx context.Context, sessionID string) (string, error) {
	if loc, ok := a.remote(ctx, sessionID); ok {
		return a.do(ctx, loc, assistant.VerbRebaseSession, nil)
	}
	return a.local.Rebase(ctx, sessionID)
}

func (a *routedActions) Archive(ctx context.Context, sessionID string) error {
	if loc, ok := a.remote(ctx, sessionID); ok {
		_, err := a.do(ctx, loc, assistant.VerbArchiveSession, nil)
		return err
	}
	return a.local.Archive(ctx, sessionID)
}

func (a *routedActions) Delete(ctx context.Context, sessionID string) error {
	if loc, ok := a.remote(ctx, sessionID); ok {
		_, err := a.do(ctx, loc, assistant.VerbDeleteSession, nil)
		return err
	}
	return a.local.Delete(ctx, sessionID)
}

func (a *routedActions) Reclaim(ctx context.Context, sessionID string) (string, error) {
	if loc, ok := a.remote(ctx, sessionID); ok {
		return a.do(ctx, loc, assistant.VerbReclaimSession, nil)
	}
	return a.local.Reclaim(ctx, sessionID)
}

func (a *routedActions) Dissolve(ctx context.Context, channelID string, keepHistory bool) error {
	return a.local.Dissolve(ctx, channelID, keepHistory)
}

func (a *routedActions) SetModel(ctx context.Context, sessionID, model string) error {
	if loc, ok := a.remote(ctx, sessionID); ok {
		_, err := a.do(ctx, loc, assistant.VerbSetSessionModel, map[string]any{"model": model})
		return err
	}
	return a.local.SetModel(ctx, sessionID, model)
}

func (a *routedActions) SetMode(ctx context.Context, sessionID, mode string) error {
	if loc, ok := a.remote(ctx, sessionID); ok {
		_, err := a.do(ctx, loc, assistant.VerbSetSessionMode, map[string]any{"mode": mode})
		return err
	}
	return a.local.SetMode(ctx, sessionID, mode)
}

func (a *routedActions) BranchFacts(ctx context.Context, sessionID string) (assistant.BranchFacts, error) {
	if loc, ok := a.remote(ctx, sessionID); ok {
		var facts assistant.BranchFacts
		err := a.link.Facts(ctx, loc.Machine.MachineID, sessionID, "branch", &facts)
		return facts, err
	}
	return a.local.BranchFacts(ctx, sessionID)
}

func (a *routedActions) DeleteVerdict(ctx context.Context, sessionID string) (assistant.DeleteVerdict, error) {
	if loc, ok := a.remote(ctx, sessionID); ok {
		var verdict assistant.DeleteVerdict
		err := a.link.Facts(ctx, loc.Machine.MachineID, sessionID, "delete", &verdict)
		return verdict, err
	}
	return a.local.DeleteVerdict(ctx, sessionID)
}

// Busy fails closed for a paired machine that does not answer: a turn that may
// be in flight is treated as one.
func (a *routedActions) Busy(ctx context.Context, sessionID string) bool {
	if loc, ok := a.remote(ctx, sessionID); ok {
		var out struct {
			Busy bool `json:"busy"`
		}
		if err := a.link.Facts(ctx, loc.Machine.MachineID, sessionID, "busy", &out); err != nil {
			return true
		}
		return out.Busy
	}
	return a.local.Busy(ctx, sessionID)
}

func (a *routedActions) ChannelBusy(ctx context.Context, channelID string) (assistant.ChannelFacts, error) {
	return a.local.ChannelBusy(ctx, channelID)
}

func (a *routedActions) SessionSettings(ctx context.Context, sessionID string) (assistant.SessionSettings, error) {
	if loc, ok := a.remote(ctx, sessionID); ok {
		var settings assistant.SessionSettings
		err := a.link.Facts(ctx, loc.Machine.MachineID, sessionID, "settings", &settings)
		return settings, err
	}
	return a.local.SessionSettings(ctx, sessionID)
}

func (a *routedActions) ResolveModel(ctx context.Context, provider, spoken string) (assistant.ModelChoice, error) {
	return a.local.ResolveModel(ctx, provider, spoken)
}

// ResolveModelFor implements assistant.SessionModelResolver: a paired machine's
// session resolves against that machine's catalog.
func (a *routedActions) ResolveModelFor(ctx context.Context, sessionID, provider, spoken string) (assistant.ModelChoice, error) {
	loc, ok := a.remote(ctx, sessionID)
	if !ok {
		return a.local.ResolveModel(ctx, provider, spoken)
	}
	var choice assistant.ModelChoice
	err := a.link.ResolveModel(ctx, loc.Machine.MachineID, sessionID, peer.ResolveModelRequest{Provider: provider, Spoken: spoken}, &choice)
	var refusal *peerlink.RefusalError
	if errors.As(err, &refusal) && refusal.Reason == peer.ReasonUnknownModel {
		return assistant.ModelChoice{}, &assistant.UnknownModelError{Spoken: spoken, Families: refusal.Families}
	}
	return choice, err
}
