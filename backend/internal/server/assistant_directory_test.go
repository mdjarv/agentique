package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/session"
)

// Attention is the deck's vocabulary, and the order is the deck's order: the
// two reasons that hold a process outrank the one that does not.
func TestAttentionOfRanksApprovalAboveAQuestion(t *testing.T) {
	tests := []struct {
		name string
		info session.SessionInfo
		want string
	}{
		{name: "idle", info: session.SessionInfo{}, want: ""},
		{
			name: "waiting on approval",
			info: session.SessionInfo{PendingApproval: &session.WirePendingApproval{ApprovalID: "a"}},
			want: assistant.AttentionApproval,
		},
		{
			name: "waiting on an answer",
			info: session.SessionInfo{PendingQuestion: &session.WirePendingQuestion{QuestionID: "q"}},
			want: assistant.AttentionQuestion,
		},
		{
			name: "both — approval holds the process first",
			info: session.SessionInfo{
				PendingApproval: &session.WirePendingApproval{ApprovalID: "a"},
				PendingQuestion: &session.WirePendingQuestion{QuestionID: "q"},
			},
			want: assistant.AttentionApproval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := attentionOf(tt.info); got != tt.want {
				t.Errorf("attentionOf = %q, want %q", got, tt.want)
			}
		})
	}
}

// A mis-transcribed filter must not turn into an empty answer — "nothing is
// running" and "I did not understand you" sound identical over a call.
func TestKeepForFilter(t *testing.T) {
	waiting := assistant.SessionRow{State: "idle", Attention: assistant.AttentionApproval}
	running := assistant.SessionRow{State: string(session.StateRunning)}
	idle := assistant.SessionRow{State: "idle"}

	tests := []struct {
		filter string
		row    assistant.SessionRow
		want   bool
	}{
		{assistant.FilterNeedsAttention, waiting, true},
		{assistant.FilterNeedsAttention, running, false},
		{assistant.FilterRunning, running, true},
		{assistant.FilterRunning, idle, false},
		{assistant.FilterRecent, idle, true},
		{assistant.FilterAll, idle, true},
		{"whatever the model said", idle, true},
	}

	for _, tt := range tests {
		if got := keepForFilter(tt.row, tt.filter); got != tt.want {
			t.Errorf("keepForFilter(%q, state %q/attention %q) = %v, want %v",
				tt.filter, tt.row.State, tt.row.Attention, got, tt.want)
		}
	}
}

// The orientation paragraph names the sessions waiting on the operator, and
// says how many it left out rather than trailing off.
func TestNamesWithReasonIsBoundedAndSaysWhy(t *testing.T) {
	rows := make([]assistant.SessionRow, 0, maxOrientationNames+3)
	for i := range maxOrientationNames + 3 {
		rows = append(rows, assistant.SessionRow{
			Name:      string(rune('A' + i)),
			Attention: assistant.AttentionApproval,
		})
	}

	got := namesWithReason(rows)
	if !strings.Contains(got, "needs approval") {
		t.Errorf("%q does not say what the session is waiting for", got)
	}
	if !strings.Contains(got, "and 3 more") {
		t.Errorf("%q does not account for the sessions it left out", got)
	}
}

// A spoken model name resolves through the same catalog the picker renders, so
// a family somebody can choose on screen is one they can ask for out loud.
func TestResolveSpokenModelUsesTheCatalog(t *testing.T) {
	d := &assistantDirectory{catalog: providers.New(
		providers.WithCLIOptionsPath(filepath.Join(t.TempDir(), "absent.json")),
	)}

	slug, family, err := d.resolveSpokenModel(context.Background(), "fable")
	if err != nil {
		t.Fatalf("resolveSpokenModel(fable): %v", err)
	}
	if slug != "fable" || family != "Fable" {
		t.Errorf("resolved to %q/%q, want the fable slug and its family label", slug, family)
	}

	// Empty is the composer's default, and resolving one here would be a second
	// copy of a decision the session service already makes.
	slug, family, err = d.resolveSpokenModel(context.Background(), "")
	if err != nil || slug != "" || family != "" {
		t.Errorf("an unspecified model resolved to %q/%q (%v), want the service's own default",
			slug, family, err)
	}
}

// A model nobody has is a spoken question. The error carries the families that
// DO exist, because the answer is the list, not a substitute.
func TestResolveSpokenModelNamesTheFamiliesItHas(t *testing.T) {
	d := &assistantDirectory{catalog: providers.New(
		providers.WithCLIOptionsPath(filepath.Join(t.TempDir(), "absent.json")),
	)}

	_, _, err := d.resolveSpokenModel(context.Background(), "grok")
	var unknown *assistant.UnknownModelError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v, want an UnknownModelError the tool can speak", err)
	}
	if unknown.Spoken != "grok" {
		t.Errorf("Spoken = %q, want what was asked for", unknown.Spoken)
	}
	for _, want := range []string{"Opus", "Fable"} {
		if !strings.Contains(unknown.Error(), want) {
			t.Errorf("%q does not offer %q as an option", unknown.Error(), want)
		}
	}

	// No catalog at all still refuses rather than guessing an id.
	if _, _, err := (&assistantDirectory{}).resolveSpokenModel(context.Background(), "opus"); err == nil {
		t.Error("a directory with no catalog invented a model")
	}
}

// An unnamed session still has to be sayable: its id is not.
func TestDisplayNameNeverSpeaksAnID(t *testing.T) {
	if got := displayName(assistant.SessionRow{ID: "8f1c-…", Name: "Live Voice Dialog"}); got != "Live Voice Dialog" {
		t.Errorf("displayName = %q, want the name", got)
	}
	got := displayName(assistant.SessionRow{ID: "8f1c-…", ProjectName: "agentique"})
	if strings.Contains(got, "8f1c") {
		t.Errorf("displayName = %q, want no id read aloud", got)
	}
	if !strings.Contains(got, "agentique") {
		t.Errorf("displayName = %q, want the project as the next best handle", got)
	}
	if got := displayName(assistant.SessionRow{ID: "8f1c-…"}); strings.Contains(got, "8f1c") {
		t.Errorf("displayName = %q, want no id read aloud", got)
	}
}

// A peer request that reached the owner and got no answer is the one place a
// create and a send are told apart from a failure: it becomes an unknown
// outcome naming the machine. An owner's refusal and a plain error stay what
// they were.
func TestPeerErrorCallsAnUnansweredRequestUnknown(t *testing.T) {
	t.Parallel()
	unanswered := &machine.UnansweredError{Path: "/api/peer/sessions", Err: errors.New("i/o timeout")}
	var unknown *assistant.OutcomeUnknownError
	if err := peerError(fmt.Errorf("create: %w", unanswered), "zbook"); !errors.As(err, &unknown) || unknown.Machine != "zbook" {
		t.Errorf("peerError(unanswered) = %v, want an unknown outcome on zbook", err)
	}

	refusal := &peerlink.RefusalError{Status: 403, Reason: "actions-off", Message: "does not accept work"}
	if err := peerError(refusal, "zbook"); errors.As(err, &unknown) {
		t.Errorf("peerError(refusal) = %v: the owner answered no", err)
	}
	plain := errors.New("dial tcp: connection refused")
	if err := peerError(plain, "zbook"); errors.As(err, &unknown) {
		t.Errorf("peerError(%v) = %v: nothing left this machine", plain, err)
	}
}
