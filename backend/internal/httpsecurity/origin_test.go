package httpsecurity

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func upgradeRequest(t *testing.T, origin, query string) *http.Request {
	t.Helper()
	target := "/api/voice/live"
	if query != "" {
		target += "?" + query
	}
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = "agentique.example"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestWebSocketOriginAllowed(t *testing.T) {
	allowed := map[string]bool{"https://peer.example": true}

	tests := []struct {
		name              string
		origin            string
		query             string
		allowTicketOrigin bool
		want              bool
	}{
		{
			name: "same origin, no ticket",
			// The request is plain HTTP, so the same-origin scheme is http.
			origin: "http://agentique.example",
			want:   true,
		},
		{
			name:   "absent origin is non-browser or same-origin",
			origin: "",
			want:   true,
		},
		{
			name:   "explicitly allowlisted origin",
			origin: "https://peer.example",
			want:   true,
		},
		{
			name:   "foreign origin without a ticket is refused",
			origin: "https://evil.example",
			want:   false,
		},
		{
			name:              "foreign origin with a ticket is admitted when tickets are enabled",
			origin:            "https://evil.example",
			query:             "wsTicket=abc123",
			allowTicketOrigin: true,
			want:              true,
		},
		{
			name:   "a ticket does not help when ticket origins are disabled",
			origin: "https://evil.example",
			query:  "wsTicket=abc123",
			want:   false,
		},
		{
			name:              "an empty ticket parameter is not a ticket",
			origin:            "https://evil.example",
			query:             "wsTicket=",
			allowTicketOrigin: true,
			want:              false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := upgradeRequest(t, tt.origin, tt.query)
			if got := WebSocketOriginAllowed(r, allowed, tt.allowTicketOrigin); got != tt.want {
				t.Errorf("WebSocketOriginAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
