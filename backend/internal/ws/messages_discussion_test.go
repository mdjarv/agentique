package ws_test

import (
	"testing"

	"github.com/mdjarv/agentique/backend/internal/ws"
)

func twoPersonas() []ws.DiscussionPersonaPayload {
	return []ws.DiscussionPersonaPayload{
		{AgentProfileID: "a", Name: "Alice"},
		{AgentProfileID: "b", Name: "Bob"},
	}
}

func TestDiscussionStartPayload_Validate_ScopeProjectCoupling(t *testing.T) {
	const validUUID = "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name    string
		payload ws.DiscussionStartPayload
		wantErr bool
	}{
		{
			name:    "web-only without project is allowed",
			payload: ws.DiscussionStartPayload{Scope: "web-only", Personas: twoPersonas(), Prompt: "go"},
		},
		{
			name:    "empty scope defaults to web-only (project optional)",
			payload: ws.DiscussionStartPayload{Personas: twoPersonas(), Prompt: "go"},
		},
		{
			name:    "web-only with a valid project is allowed",
			payload: ws.DiscussionStartPayload{Scope: "web-only", ProjectID: validUUID, Personas: twoPersonas(), Prompt: "go"},
		},
		{
			name:    "web-only with a malformed project is rejected",
			payload: ws.DiscussionStartPayload{Scope: "web-only", ProjectID: "not-a-uuid", Personas: twoPersonas(), Prompt: "go"},
			wantErr: true,
		},
		{
			name:    "repo-backed requires a project",
			payload: ws.DiscussionStartPayload{Scope: "repo-backed", Personas: twoPersonas(), Prompt: "go"},
			wantErr: true,
		},
		{
			name:    "repo-backed with a project is allowed",
			payload: ws.DiscussionStartPayload{Scope: "repo-backed", ProjectID: validUUID, Personas: twoPersonas(), Prompt: "go"},
		},
		{
			name:    "unknown scope is rejected",
			payload: ws.DiscussionStartPayload{Scope: "galaxy-brained", ProjectID: validUUID, Personas: twoPersonas(), Prompt: "go"},
			wantErr: true,
		},
		{
			name:    "unknown mode is rejected",
			payload: ws.DiscussionStartPayload{Scope: "web-only", Mode: "freeform", Personas: twoPersonas(), Prompt: "go"},
			wantErr: true,
		},
		{
			name:    "fewer than 2 personas is rejected",
			payload: ws.DiscussionStartPayload{Scope: "web-only", Personas: twoPersonas()[:1], Prompt: "go"},
			wantErr: true,
		},
		{
			name:    "empty prompt is rejected",
			payload: ws.DiscussionStartPayload{Scope: "web-only", Personas: twoPersonas()},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.payload.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
