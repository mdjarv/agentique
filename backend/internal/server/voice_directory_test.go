package server

import (
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/voice"
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
			want: voice.AttentionApproval,
		},
		{
			name: "waiting on an answer",
			info: session.SessionInfo{PendingQuestion: &session.WirePendingQuestion{QuestionID: "q"}},
			want: voice.AttentionQuestion,
		},
		{
			name: "both — approval holds the process first",
			info: session.SessionInfo{
				PendingApproval: &session.WirePendingApproval{ApprovalID: "a"},
				PendingQuestion: &session.WirePendingQuestion{QuestionID: "q"},
			},
			want: voice.AttentionApproval,
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
	waiting := voice.SessionRow{State: "idle", Attention: voice.AttentionApproval}
	running := voice.SessionRow{State: string(session.StateRunning)}
	idle := voice.SessionRow{State: "idle"}

	tests := []struct {
		filter string
		row    voice.SessionRow
		want   bool
	}{
		{voice.FilterNeedsAttention, waiting, true},
		{voice.FilterNeedsAttention, running, false},
		{voice.FilterRunning, running, true},
		{voice.FilterRunning, idle, false},
		{voice.FilterRecent, idle, true},
		{voice.FilterAll, idle, true},
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
	rows := make([]voice.SessionRow, 0, maxOrientationNames+3)
	for i := range maxOrientationNames + 3 {
		rows = append(rows, voice.SessionRow{
			Name:      string(rune('A' + i)),
			Attention: voice.AttentionApproval,
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

// An unnamed session still has to be sayable: its id is not.
func TestDisplayNameNeverSpeaksAnID(t *testing.T) {
	if got := displayName(voice.SessionRow{ID: "8f1c-…", Name: "Live Voice Dialog"}); got != "Live Voice Dialog" {
		t.Errorf("displayName = %q, want the name", got)
	}
	got := displayName(voice.SessionRow{ID: "8f1c-…", ProjectName: "agentique"})
	if strings.Contains(got, "8f1c") {
		t.Errorf("displayName = %q, want no id read aloud", got)
	}
	if !strings.Contains(got, "agentique") {
		t.Errorf("displayName = %q, want the project as the next best handle", got)
	}
	if got := displayName(voice.SessionRow{ID: "8f1c-…"}); strings.Contains(got, "8f1c") {
		t.Errorf("displayName = %q, want no id read aloud", got)
	}
}
