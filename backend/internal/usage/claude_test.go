package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// livePayload is the real response from api.anthropic.com/api/oauth/usage,
// captured 2026-08-27. It is the fixture because every trap this file guards
// against is present in it and in nothing simpler:
//
//   - the model-scoped weekly window exists ONLY in limits[]
//   - the legacy per-model buckets are null
//   - `nimbus_quill` is a live codenamed bucket with no limits[] entry
const livePayload = `{
  "five_hour": {"utilization": 15.0, "resets_at": "2026-08-27T09:40:00.412946+00:00"},
  "seven_day": {"utilization": 44.0, "resets_at": "2026-08-30T22:00:00.412971+00:00"},
  "seven_day_oauth_apps": null,
  "seven_day_opus": null,
  "seven_day_sonnet": null,
  "nimbus_quill": {"utilization": 0.0, "resets_at": null},
  "amber_ladder": null,
  "tangelo": null,
  "limits": [
    {"kind":"session","group":"session","percent":15,"severity":"normal","resets_at":"2026-08-27T09:40:00.412946+00:00","scope":null},
    {"kind":"weekly_all","group":"weekly","percent":44,"severity":"normal","resets_at":"2026-08-30T22:00:00.412971+00:00","scope":null},
    {"kind":"weekly_scoped","group":"weekly","percent":11,"severity":"normal","resets_at":"2026-08-30T22:00:00.413296+00:00",
     "scope":{"model":{"id":null,"display_name":"Fable"},"surface":null}}
  ]
}`

func parsePayload(t *testing.T, raw string) usagePayload {
	t.Helper()
	var p usagePayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return p
}

func TestNormalizeLivePayload(t *testing.T) {
	limits := normalizeClaude(parsePayload(t, livePayload))

	if len(limits) != 3 {
		t.Fatalf("want 3 windows, got %d: %+v", len(limits), limits)
	}
	want := []struct {
		label   string
		percent float64
	}{
		{"Session (5-hour)", 0.15},
		{"Weekly (7-day)", 0.44},
		{"Fable Weekly", 0.11},
	}
	for i, w := range want {
		if limits[i].Label != w.label {
			t.Errorf("limit %d label = %q, want %q", i, limits[i].Label, w.label)
		}
		if diff := limits[i].Percent - w.percent; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("limit %d percent = %v, want %v", i, limits[i].Percent, w.percent)
		}
		if limits[i].Severity != "normal" {
			t.Errorf("limit %d severity = %q, want the server's own verdict", i, limits[i].Severity)
		}
	}
}

// The whole reason limits[] is the source of truth: the scoped window is
// invisible to a bucket reader.
func TestScopedWindowIsInvisibleToBuckets(t *testing.T) {
	p := parsePayload(t, livePayload)
	fromBuckets := normalizeBuckets(p)
	for _, l := range fromBuckets {
		if l.Label == "Fable Weekly" {
			t.Fatal("the fixture no longer demonstrates the trap")
		}
	}
	if len(fromBuckets) != 2 {
		t.Fatalf("the fallback reads two known buckets, got %d", len(fromBuckets))
	}
}

// Iterating every top-level bucket would invent a meter for `nimbus_quill`,
// which has no entry in limits[] and no name a user would recognise.
func TestCodenamedBucketsNeverAppear(t *testing.T) {
	for _, l := range normalizeClaude(parsePayload(t, livePayload)) {
		switch l.Label {
		case "nimbus quill", "nimbus_quill", "amber ladder", "tangelo":
			t.Fatalf("a codenamed bucket reached the output: %q", l.Label)
		}
	}
}

// Scale is decided once per payload, from the whole payload. Per-value would
// make a genuine 1.0 render as 100% when it meant 1%.
func TestScaleIsDecidedPerPayload(t *testing.T) {
	fractions := `{"limits":[
	  {"kind":"session","percent":0.37,"resets_at":null},
	  {"kind":"weekly_all","percent":0.9,"resets_at":null}]}`
	limits := normalizeClaude(parsePayload(t, fractions))
	if limits[0].Percent != 0.37 || limits[1].Percent != 0.9 {
		t.Fatalf("a fraction payload must pass through: %+v", limits)
	}

	// One value >= 1 makes the WHOLE payload percent-scaled, including the 1.0
	// that would otherwise be read as 100%.
	mixed := `{"limits":[
	  {"kind":"session","percent":1.0,"resets_at":null},
	  {"kind":"weekly_all","percent":44,"resets_at":null}]}`
	limits = normalizeClaude(parsePayload(t, mixed))
	if limits[0].Percent != 0.01 {
		t.Fatalf("1.0 in a percent-scaled payload is 1%%, got %v", limits[0].Percent)
	}
	if limits[1].Percent != 0.44 {
		t.Fatalf("44 in a percent-scaled payload is 44%%, got %v", limits[1].Percent)
	}
}

// A window name derived from a model name yields "1M" — one minute — from
// "Opus 5 (1M context)". The kind is the only safe source.
func TestWindowComesFromKindNotModelName(t *testing.T) {
	raw := `{"limits":[{"kind":"weekly_scoped","percent":11,"resets_at":null,
	  "scope":{"model":{"display_name":"Opus 5 (1M context)"}}}]}`
	limits := normalizeClaude(parsePayload(t, raw))
	if len(limits) != 1 {
		t.Fatalf("want 1 limit, got %d", len(limits))
	}
	if limits[0].Label != "Opus 5 (1M context) Weekly" {
		t.Fatalf("label = %q — the window must come from kind", limits[0].Label)
	}
}

// A model can hold more than one scoped window, so the dedup key is the pair.
func TestDedupKeyIsModelAndWindow(t *testing.T) {
	raw := `{"limits":[
	  {"kind":"weekly_scoped","percent":11,"resets_at":null,"scope":{"model":{"display_name":"Fable"}}},
	  {"kind":"session","percent":4,"resets_at":null,"scope":{"model":{"display_name":"Fable"}}},
	  {"kind":"weekly_scoped","percent":99,"resets_at":null,"scope":{"model":{"display_name":"Fable"}}}]}`
	limits := normalizeClaude(parsePayload(t, raw))
	if len(limits) != 2 {
		t.Fatalf("one model with two windows is two limits, got %d: %+v", len(limits), limits)
	}
	if limits[0].Percent != 0.11 {
		t.Errorf("the first of a duplicate pair wins, got %v", limits[0].Percent)
	}
}

func TestResetNormalization(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"iso with offset", "2026-08-27T09:40:00.412946+00:00", "2026-08-27T09:40:00Z"},
		// The same instant either way: the 1e12 test separates seconds from
		// milliseconds, and no real timestamp is ambiguous across it.
		{"epoch seconds", float64(1787820000), "2026-08-27T08:40:00Z"},
		{"epoch millis", float64(1787820000000), "2026-08-27T08:40:00Z"},
		{"nil", nil, ""},
		{"empty", "", ""},
		{"garbage", "not a time", ""},
	}
	for _, c := range cases {
		if got := normalizeReset(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// A cached percentage expires when its WINDOW rolls over, not on a timer — but
// an unreadable timestamp is no reason to discard a real number.
func TestExpiry(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		resets string
		want   bool
	}{
		{"2026-08-27T09:40:00Z", true},
		{"2026-08-30T22:00:00Z", false},
		{"", false},
		{"not a time", false},
	}
	for _, c := range cases {
		if got := (Limit{ResetsAt: c.resets}).Expired(now); got != c.want {
			t.Errorf("resetsAt %q: expired = %v, want %v", c.resets, got, c.want)
		}
	}
}

func TestPlanLabel(t *testing.T) {
	cases := []struct{ tier, sub, want string }{
		{"default_claude_max_20x", "max", "Max 20x"},
		{"default_claude_max_5x", "max", "Max 5x"},
		{"", "pro", "Pro"},
		{"", "", ""},
	}
	for _, c := range cases {
		got := credentials{RateLimitTier: c.tier, SubscriptionType: c.sub}.planLabel()
		if got != c.want {
			t.Errorf("tier %q sub %q: got %q want %q", c.tier, c.sub, got, c.want)
		}
	}
}

// --- the auth states, each of which needs its own words ---

func writeCreds(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuthStatesAreDistinct(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }

	t.Run("no credentials at all", func(t *testing.T) {
		a := collectClaude(context.Background(), ClaudeOptions{
			CredentialsPath: filepath.Join(t.TempDir(), "absent.json"),
			Now:             nowFn,
		})
		if a.Ready || a.AuthHelpText == "" {
			t.Fatalf("must name the command that fixes it: %+v", a)
		}
	})

	t.Run("expired token never probes", func(t *testing.T) {
		var called bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		path := writeCreds(t, `{"claudeAiOauth":{"accessToken":"t","expiresAt":1000,"subscriptionType":"max"}}`)

		a := collectClaude(context.Background(), ClaudeOptions{
			CredentialsPath: path, URL: srv.URL, Now: nowFn,
		})
		if called {
			t.Fatal("an expired token must not be spent on a request that cannot succeed")
		}
		if a.AuthHelpText == "" {
			t.Fatal("only the CLI can mint a token — say so")
		}
	})

	t.Run("rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		path := writeCreds(t, `{"claudeAiOauth":{"accessToken":"t","expiresAt":99999999999999}}`)
		a := collectClaude(context.Background(), ClaudeOptions{CredentialsPath: path, URL: srv.URL, Now: nowFn})
		if a.Ready || a.UsageStatusText == "" {
			t.Fatalf("a rejection is its own state: %+v", a)
		}
	})

	t.Run("200 with no limits", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		path := writeCreds(t, `{"claudeAiOauth":{"accessToken":"t","expiresAt":99999999999999}}`)
		a := collectClaude(context.Background(), ClaudeOptions{CredentialsPath: path, URL: srv.URL, Now: nowFn})
		if a.Ready {
			t.Fatal("no limits is not readiness")
		}
		if a.AuthHelpText != "" {
			t.Fatal("this is not an auth problem and must not blame one")
		}
	})

	t.Run("success carries the token in the header and nowhere else", func(t *testing.T) {
		var gotAuth, gotBeta string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			gotBeta = r.Header.Get("anthropic-beta")
			_, _ = w.Write([]byte(livePayload))
		}))
		defer srv.Close()
		path := writeCreds(t, `{"claudeAiOauth":{"accessToken":"secret-token","expiresAt":99999999999999,"rateLimitTier":"default_claude_max_20x"}}`)

		a := collectClaude(context.Background(), ClaudeOptions{CredentialsPath: path, URL: srv.URL, Now: nowFn})
		if gotAuth != "Bearer secret-token" || gotBeta != oauthBeta {
			t.Fatalf("headers wrong: auth=%q beta=%q", gotAuth, gotBeta)
		}
		if !a.Ready || len(a.Limits) != 3 {
			t.Fatalf("want a ready record with 3 windows: %+v", a)
		}
		if a.TierLabel != "Max 20x" {
			t.Fatalf("tierLabel = %q", a.TierLabel)
		}
		// The token must not survive into anything that reaches a client.
		blob, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if contains(string(blob), "secret-token") {
			t.Fatal("the access token reached the output document")
		}
	})
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
