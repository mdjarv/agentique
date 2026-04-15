package persona

import (
	"database/sql"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   QueryResult
	}{
		{
			name: "full structured response",
			input: `ACTION: answer
CONFIDENCE: 0.9
REDIRECT_TO:
REASON: I can answer capability questions directly.

RESPONSE: Yes, I handle REST API endpoints. I maintain all routes under /api/ including authentication, users, and projects.`,
			want: QueryResult{
				Action:     "answer",
				Confidence: 0.9,
				Reason:     "I can answer capability questions directly.",
				Response:   "Yes, I handle REST API endpoints. I maintain all routes under /api/ including authentication, users, and projects.",
			},
		},
		{
			name: "redirect action",
			input: `ACTION: redirect
CONFIDENCE: 0.85
REDIRECT_TO: Frontend Expert
REASON: CSS layout is frontend domain.

RESPONSE: I don't handle CSS layout. Frontend Expert would be the right person to ask about this.`,
			want: QueryResult{
				Action:     "redirect",
				Confidence: 0.85,
				RedirectTo: "Frontend Expert",
				Reason:     "CSS layout is frontend domain.",
				Response:   "I don't handle CSS layout. Frontend Expert would be the right person to ask about this.",
			},
		},
		{
			name: "spawn action",
			input: `ACTION: spawn
CONFIDENCE: 0.95
REDIRECT_TO:
REASON: This is a work request that needs a full session.

RESPONSE: I can implement that endpoint. This will need a full session — I'll need to create the route handler, add database queries, and write tests.`,
			want: QueryResult{
				Action:     "spawn",
				Confidence: 0.95,
				Reason:     "This is a work request that needs a full session.",
				Response:   "I can implement that endpoint. This will need a full session — I'll need to create the route handler, add database queries, and write tests.",
			},
		},
		{
			name: "fallback when no structured format",
			input: "I'm not sure how to answer that in the structured format, but yes I handle API routes.",
			want: QueryResult{
				Action:     "answer",
				Confidence: 0.5,
				Response:   "I'm not sure how to answer that in the structured format, but yes I handle API routes.",
			},
		},
		{
			name: "multiline response",
			input: `ACTION: answer
CONFIDENCE: 0.8
REDIRECT_TO:
REASON: Status check

RESPONSE: I'm currently idle. My recent work includes:
- Added user authentication endpoints
- Fixed pagination in the projects list
- Wrote migration for new teams table`,
			want: QueryResult{
				Action:     "answer",
				Confidence: 0.8,
				Reason:     "Status check",
				Response:   "I'm currently idle. My recent work includes:\n- Added user authentication endpoints\n- Fixed pagination in the projects list\n- Wrote migration for new teams table",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResponse(tt.input)
			if got.Action != tt.want.Action {
				t.Errorf("Action = %q, want %q", got.Action, tt.want.Action)
			}
			if got.Confidence != tt.want.Confidence {
				t.Errorf("Confidence = %f, want %f", got.Confidence, tt.want.Confidence)
			}
			if got.RedirectTo != tt.want.RedirectTo {
				t.Errorf("RedirectTo = %q, want %q", got.RedirectTo, tt.want.RedirectTo)
			}
			if got.Reason != tt.want.Reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.want.Reason)
			}
			if got.Response != tt.want.Response {
				t.Errorf("Response = %q, want %q", got.Response, tt.want.Response)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	profile := store.AgentProfile{
		ID:          "profile-1",
		Name:        "Backend Expert",
		Role:        "backend developer",
		Description: "Maintains Go backend, REST APIs, and database.",
	}

	team := store.Team{
		ID:   "team-1",
		Name: "Core Team",
	}

	members := []store.AgentProfile{
		profile,
		{
			ID:          "profile-2",
			Name:        "Frontend Expert",
			Role:        "frontend developer",
			Description: "Builds React UI components.",
			ProjectID:   sql.NullString{},
		},
	}

	input := QueryInput{
		ProfileID: "profile-1",
		TeamID:    "team-1",
		AskerType: "agent",
		AskerID:   "profile-2",
		AskerName: "Frontend Expert",
		Question:  "Do you handle REST API endpoints?",
	}

	prompt := buildPrompt(profile, team, members, input)

	// Check key sections exist.
	mustContain := []string{
		`persona of "Backend Expert"`,
		"backend developer",
		`"Core Team"`,
		"Maintains Go backend",
		`"Frontend Expert"`,
		"frontend developer",
		`"Frontend Expert" (a teammate) asks: Do you handle REST API endpoints?`,
		"ACTION:",
		"CONFIDENCE:",
		"RESPONSE:",
	}

	for _, s := range mustContain {
		if !contains(prompt, s) {
			t.Errorf("prompt missing %q\n\nFull prompt:\n%s", s, prompt)
		}
	}

	// Self should be excluded from teammates.
	if contains(prompt, "- \"Backend Expert\"") {
		t.Error("prompt should not list self as a teammate")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		findSubstring(haystack, needle))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
