package peer

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/auth"
)

// ownerActions is the owner's executor: facts it answers, and what it did.
type ownerActions struct {
	mu      sync.Mutex
	branch  assistant.BranchFacts
	verdict assistant.DeleteVerdict
	merged  int
	deleted int
}

func (a *ownerActions) Merge(context.Context, string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.merged++
	return "merged into the project's branch", nil
}
func (a *ownerActions) Rebase(context.Context, string) (string, error) { return "rebased", nil }
func (a *ownerActions) Archive(context.Context, string) error          { return nil }
func (a *ownerActions) Delete(context.Context, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleted++
	return nil
}
func (a *ownerActions) Reclaim(context.Context, string) (string, error) { return "freed", nil }
func (a *ownerActions) Dissolve(context.Context, string, bool) error    { return nil }
func (a *ownerActions) SetModel(context.Context, string, string) error  { return nil }
func (a *ownerActions) SetMode(context.Context, string, string) error   { return nil }
func (a *ownerActions) Busy(context.Context, string) bool               { return false }
func (a *ownerActions) ChannelBusy(context.Context, string) (assistant.ChannelFacts, error) {
	return assistant.ChannelFacts{}, nil
}
func (a *ownerActions) BranchFacts(context.Context, string) (assistant.BranchFacts, error) {
	return a.branch, nil
}
func (a *ownerActions) DeleteVerdict(context.Context, string) (assistant.DeleteVerdict, error) {
	return a.verdict, nil
}
func (a *ownerActions) SessionSettings(context.Context, string) (assistant.SessionSettings, error) {
	return assistant.SessionSettings{Provider: "claude", ModelSwitch: true, Live: true}, nil
}
func (a *ownerActions) ResolveModel(_ context.Context, _, spoken string) (assistant.ModelChoice, error) {
	if spoken == "opus" {
		return assistant.ModelChoice{ID: "claude-opus-5", Label: "Opus"}, nil
	}
	return assistant.ModelChoice{}, &assistant.UnknownModelError{Spoken: spoken, Families: []string{"Opus"}}
}

// The owner performs an accepted verb only when its OWN facts still allow it:
// a branch that fell behind since the card was written is refused here with
// the outcome word, and nothing is merged.
func TestDoReChecksOnTheOwner(t *testing.T) {
	actions := &ownerActions{branch: assistant.BranchFacts{Ahead: 2}}
	h := New(newFakeSessions(), testProjects, WithSettings(Settings{AcceptActions: true}), WithProposalActions(actions))
	peer := peerRow(auth.KindPeer)
	target := "/api/peer/sessions/" + worktreeSession + "/do/" + assistant.VerbMergeSession

	rec := serve(t, h, peer, http.MethodPost, target, `{}`)
	if rec.Code != http.StatusOK || actions.merged != 1 {
		t.Fatalf("merge = %d %s merged=%d", rec.Code, rec.Body.String(), actions.merged)
	}

	actions.branch = assistant.BranchFacts{Ahead: 2, Behind: 3}
	rec = serve(t, h, peer, http.MethodPost, target, `{}`)
	if reasonOf(t, rec) != ReasonOutcome || actions.merged != 1 {
		t.Fatalf("stale merge = %d %s merged=%d", rec.Code, rec.Body.String(), actions.merged)
	}

	// Delete is irreversible, and storage's verdict decides it on the owner.
	del := "/api/peer/sessions/" + worktreeSession + "/do/" + assistant.VerbDeleteSession
	if rec := serve(t, h, peer, http.MethodPost, del, `{}`); reasonOf(t, rec) != ReasonOutcome || actions.deleted != 0 {
		t.Fatalf("unsafe delete = %d %s deleted=%d", rec.Code, rec.Body.String(), actions.deleted)
	}
}

func TestDoIsGatedAndClosed(t *testing.T) {
	actions := &ownerActions{branch: assistant.BranchFacts{Ahead: 1}}
	off := New(newFakeSessions(), testProjects, WithProposalActions(actions))
	peer := peerRow(auth.KindPeer)
	rec := serve(t, off, peer, http.MethodPost, "/api/peer/sessions/"+worktreeSession+"/do/"+assistant.VerbMergeSession, `{}`)
	if reasonOf(t, rec) != ReasonActionsOff || actions.merged != 0 {
		t.Fatalf("with actions off = %d %s", rec.Code, rec.Body.String())
	}

	on := New(newFakeSessions(), testProjects, WithSettings(Settings{AcceptActions: true}), WithProposalActions(actions))
	for _, verb := range []string{assistant.VerbDissolveChannel, assistant.VerbRunPrompt, "rm-rf"} {
		rec := serve(t, on, peer, http.MethodPost, "/api/peer/sessions/"+worktreeSession+"/do/"+verb, `{}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("verb %q = %d %s, want refused as no such verb", verb, rec.Code, rec.Body.String())
		}
	}
}

// Facts are reads and need no opt-in; model resolution answers the owner's
// families when a name is unknown.
func TestFactsAndModelResolution(t *testing.T) {
	actions := &ownerActions{branch: assistant.BranchFacts{Ahead: 4, MergeStatus: "clean"}}
	h := New(newFakeSessions(), testProjects, WithProposalActions(actions))
	peer := peerRow(auth.KindPeer)

	rec := serve(t, h, peer, http.MethodGet, "/api/peer/sessions/"+worktreeSession+"/facts/branch", "")
	var facts assistant.BranchFacts
	_ = json.Unmarshal(rec.Body.Bytes(), &facts)
	if rec.Code != http.StatusOK || facts.Ahead != 4 {
		t.Fatalf("branch facts = %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(t, h, peer, http.MethodPost, "/api/peer/sessions/"+worktreeSession+"/models/resolve", `{"provider":"claude","spoken":"gpt"}`)
	var refusal ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
	if refusal.Reason != ReasonUnknownModel || len(refusal.Families) != 1 {
		t.Fatalf("unknown model = %d %s", rec.Code, rec.Body.String())
	}
}
