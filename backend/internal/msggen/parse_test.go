package msggen

import "testing"

func TestParseCommitMessage(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
		wantDesc  string
	}{
		{
			name:      "standard format",
			input:     "TITLE: fix auth bug\nDESCRIPTION:\nFixed the login flow",
			wantTitle: "fix auth bug",
			wantDesc:  "Fixed the login flow",
		},
		{
			name:      "title only",
			input:     "TITLE: quick fix",
			wantTitle: "quick fix",
			wantDesc:  "",
		},
		{
			name:      "no markers",
			input:     "just a plain message\nsecond line",
			wantTitle: "just a plain message",
			wantDesc:  "second line",
		},
		{
			name:      "truncates long title",
			input:     "TITLE: " + string(make([]byte, 80)),
			wantTitle: string(make([]byte, 72)),
			wantDesc:  "",
		},
		{
			name:      "empty description",
			input:     "TITLE: foo\nDESCRIPTION:",
			wantTitle: "foo",
			wantDesc:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCommitMessage(tt.input)
			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", got.Description, tt.wantDesc)
			}
		})
	}
}

func TestParsePRDescription(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "standard format",
			input:     "TITLE: Add auth\nBODY:\n- Added login\n- Added logout",
			wantTitle: "Add auth",
			wantBody:  "- Added login\n- Added logout",
		},
		{
			name:      "title only",
			input:     "TITLE: quick PR",
			wantTitle: "quick PR",
			wantBody:  "",
		},
		{
			name:      "no markers",
			input:     "plain title\nplain body",
			wantTitle: "plain title",
			wantBody:  "plain body",
		},
		{
			name:      "truncates long title",
			input:     "TITLE: " + string(make([]byte, 80)),
			wantTitle: string(make([]byte, 70)),
			wantBody:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePRDescription(tt.input)
			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", got.Body, tt.wantBody)
			}
		})
	}
}
