package voice

import "testing"

// The inputs here are the real ones: a name that went through a microphone, a
// speech model, and someone who half-remembers it.
func TestMatchSessionsFindsWhatWasActuallySaid(t *testing.T) {
	rows := []SessionRow{
		{ID: "s1", Name: "Live Voice Dialog", ProjectName: "agentique", ProjectSlug: "agentique", MachineName: "workstation", LastActivity: "2026-08-26T12:00:00Z"},
		{ID: "s2", Name: "Reconnect Drops", ProjectName: "agentique", ProjectSlug: "agentique", MachineName: "workstation", LastActivity: "2026-08-26T11:00:00Z"},
		{ID: "s3", Name: "Invoice Importer", ProjectName: "billing service", ProjectSlug: "billing", MachineName: "laptop", LastActivity: "2026-08-26T10:00:00Z"},
		{ID: "s4", Name: "Voice Settings Page", ProjectName: "agentique", ProjectSlug: "agentique", MachineName: "laptop", LastActivity: "2026-08-26T09:00:00Z"},
	}

	tests := []struct {
		name      string
		query     string
		wantTop   string
		wantClear bool
		// wantIn is a session that must appear somewhere in the candidates.
		wantIn string
		// wantNone means the query must find nothing at all.
		wantNone bool
	}{
		{
			name:    "the transcription heard a longer word",
			query:   "live voice dialogue",
			wantTop: "s1", wantClear: true,
		},
		{
			name:    "said exactly, and it is the obvious one",
			query:   "Live Voice Dialog",
			wantTop: "s1", wantClear: true,
		},
		{
			name:    "filler words around the name",
			query:   "the reconnect one please",
			wantTop: "s2", wantClear: true,
		},
		{
			name:    "named by its project",
			query:   "billing",
			wantTop: "s3", wantClear: true,
		},
		{
			name:    "a partial project name",
			query:   "billing serv",
			wantTop: "s3",
		},
		{
			name:    "named by machine and project together",
			query:   "the agentique one on the laptop",
			wantTop: "s4",
		},
		{
			name:  "an ambiguous word is not resolved for the user",
			query: "voice",
			// Two sessions have "voice" in the name; the assistant must ask.
			wantClear: false,
			wantIn:    "s4",
		},
		{
			name:     "nothing like it",
			query:    "kubernetes upgrade",
			wantNone: true,
		},
		{
			name:     "an empty query is not a match-everything",
			query:    "   ",
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, clear := MatchSessions(tt.query, rows)

			if tt.wantNone {
				if len(got) != 0 {
					t.Fatalf("query %q matched %v, want nothing", tt.query, ids(got))
				}
				if clear {
					t.Error("nothing matched, so nothing can be clear")
				}
				return
			}

			if len(got) == 0 {
				t.Fatalf("query %q matched nothing", tt.query)
			}
			if tt.wantTop != "" && got[0].Row.ID != tt.wantTop {
				t.Errorf("query %q ranked %v, want %q first", tt.query, ids(got), tt.wantTop)
			}
			if tt.wantIn != "" && !containsID(got, tt.wantIn) {
				t.Errorf("query %q gave %v, want %q among them", tt.query, ids(got), tt.wantIn)
			}
			if clear != tt.wantClear {
				t.Errorf("query %q: topIsClear = %v, want %v (candidates %v)", tt.query, clear, tt.wantClear, ids(got))
			}
		})
	}
}

// Among equally good matches, the one waiting on the operator is the one they
// probably meant — the same ordering the deck's Needs-you band uses.
func TestMatchRanksAttentionAboveRecency(t *testing.T) {
	rows := []SessionRow{
		{ID: "recent", Name: "Voice Work", LastActivity: "2026-08-26T12:00:00Z"},
		{ID: "waiting", Name: "Voice Work", Attention: AttentionApproval, LastActivity: "2026-08-26T08:00:00Z"},
	}
	got, _ := MatchSessions("voice work", rows)
	if len(got) != 2 {
		t.Fatalf("matched %v, want both", ids(got))
	}
	if got[0].Row.ID != "waiting" {
		t.Errorf("ranked %v first, want the session that is waiting on the operator", got[0].Row.ID)
	}
}

// Five is already more than anyone wants read aloud.
func TestMatchIsBounded(t *testing.T) {
	rows := make([]SessionRow, 0, maxCandidates*3)
	for i := range maxCandidates * 3 {
		rows = append(rows, SessionRow{ID: string(rune('a' + i)), Name: "Voice Work"})
	}
	got, clear := MatchSessions("voice work", rows)
	if len(got) != maxCandidates {
		t.Errorf("returned %d candidates, want the %d cap", len(got), maxCandidates)
	}
	if clear {
		t.Error("fifteen identical names cannot have a clear winner")
	}
}

func ids(candidates []Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Row.ID)
	}
	return out
}

func containsID(candidates []Candidate, id string) bool {
	for _, candidate := range candidates {
		if candidate.Row.ID == id {
			return true
		}
	}
	return false
}
