package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The Claude collector.
//
// Credentials come from the CLI's own store; the token goes into an
// Authorization header and NOWHERE else — not the cache, not the response, not
// a log line. Only the derived plan label ever leaves this file.
//
// Reading that file is not "running a provider CLI" (docs/upgrades.md): it is a
// file read and an HTTPS request. Keeping it here rather than in claudecli-go
// also preserves that library's documented network-free property.

// DefaultUsageURL is Anthropic's OAuth usage endpoint.
const DefaultUsageURL = "https://api.anthropic.com/api/oauth/usage"

// oauthBeta is the header the endpoint requires.
const oauthBeta = "oauth-2025-04-20"

var (
	// ErrNoCredentials means the CLI has never signed in here.
	ErrNoCredentials = errors.New("not signed in")
	// ErrTokenExpired means the stored token is past its expiry. Only the CLI
	// can mint a fresh one, so this must be reported as its own state.
	ErrTokenExpired = errors.New("sign-in expired")
	// ErrRejected means a server answered and refused us.
	ErrRejected = errors.New("rejected by the server")
	// ErrUnreachable means nothing answered — no DNS, no route, refused.
	// Distinct from ErrRejected on purpose: nothing-answered warrants a fast
	// retry (the first probe after a laptop wakes commonly beats DHCP), while
	// a server that answered should be left alone.
	ErrUnreachable = errors.New("could not reach the usage endpoint")
)

// credentials is the subset of ~/.claude/.credentials.json we read. Nothing
// else from that file may reach the output document.
type credentials struct {
	AccessToken      string `json:"accessToken"`
	ExpiresAt        int64  `json:"expiresAt"` // epoch ms
	RateLimitTier    string `json:"rateLimitTier"`
	SubscriptionType string `json:"subscriptionType"`
}

// maxTier pulls "20x" out of "default_claude_max_20x".
var maxTier = regexp.MustCompile(`max_(\d+x)`)

// planLabel renders the tier for display. It is the ONLY thing derived from the
// credential store that leaves this package.
func (c credentials) planLabel() string {
	if m := maxTier.FindStringSubmatch(c.RateLimitTier); m != nil {
		return "Max " + m[1]
	}
	if c.SubscriptionType == "" {
		return ""
	}
	return strings.ToUpper(c.SubscriptionType[:1]) + c.SubscriptionType[1:]
}

// readCredentials loads the CLI's credential store.
func readCredentials(path string) (credentials, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // a fixed path in the user's home
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return credentials{}, ErrNoCredentials
		}
		return credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var doc struct {
		OAuth credentials `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	if doc.OAuth.AccessToken == "" {
		return credentials{}, ErrNoCredentials
	}
	return doc.OAuth, nil
}

// DefaultCredentialsPath is where the Claude CLI keeps its OAuth store.
func DefaultCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

// usagePayload is the shape we care about. Note what is absent: the flat
// per-model buckets (seven_day_opus, seven_day_sonnet) are deliberately not
// read — see collectClaude.
type usagePayload struct {
	FiveHour *bucket `json:"five_hour"`
	SevenDay *bucket `json:"seven_day"`
	Limits   []entry `json:"limits"`
}

type bucket struct {
	Utilization *float64        `json:"utilization"`
	ResetsAt    json.RawMessage `json:"resets_at"`
}

type entry struct {
	Kind     string          `json:"kind"`
	Percent  *float64        `json:"percent"`
	Severity string          `json:"severity"`
	ResetsAt json.RawMessage `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

// ClaudeOptions configures the collector.
type ClaudeOptions struct {
	// CredentialsPath overrides the CLI's store, for tests.
	CredentialsPath string
	// URL overrides the usage endpoint, for tests.
	URL string
	// Client defaults to a 10s-timeout client.
	Client *http.Client
	// Now is injected in tests.
	Now func() time.Time
}

func (o ClaudeOptions) withDefaults() ClaudeOptions {
	if o.CredentialsPath == "" {
		o.CredentialsPath = DefaultCredentialsPath()
	}
	if o.URL == "" {
		o.URL = DefaultUsageURL
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	return o
}

// collectClaude produces the Claude record. It never returns an error: every
// failure becomes a state ON the record, because a failed refresh must never
// blank the component.
func collectClaude(ctx context.Context, opts ClaudeOptions) Agent {
	opts = opts.withDefaults()
	now := opts.Now()
	agent := Agent{ID: "claude", Name: "Claude Code"}

	creds, err := readCredentials(opts.CredentialsPath)
	if err != nil {
		if errors.Is(err, ErrNoCredentials) {
			agent.UsageStatusText = "Not signed in."
			agent.AuthHelpText = "Run `claude auth login` to see usage."
			return agent
		}
		agent.UsageStatusText = "Could not read the Claude credential store."
		return agent
	}
	agent.TierLabel = creds.planLabel()

	// An expired token is its own state: no probe will succeed, and only the
	// CLI can fix it. Saying "error" here would send the user looking in the
	// wrong place.
	if creds.ExpiresAt > 0 && now.After(time.UnixMilli(creds.ExpiresAt)) {
		agent.UsageStatusText = "Sign-in expired."
		agent.AuthHelpText = "Run `claude auth login` to restore usage."
		return agent
	}

	limits, err := probeClaude(ctx, opts, creds.AccessToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnreachable):
			agent.UsageStatusText = "Offline — could not reach the usage endpoint."
		case errors.Is(err, ErrRejected):
			agent.UsageStatusText = "Rejected by the server."
			agent.AuthHelpText = "Run `claude auth login` to restore usage."
		default:
			agent.UsageStatusText = err.Error()
		}
		return agent
	}
	if len(limits) == 0 {
		// A 200 that reports nothing is not a failure, and it is not zero
		// either. Say which it is.
		agent.UsageStatusText = "No limits reported for this account."
		return agent
	}

	agent.Ready = true
	agent.Limits = limits
	agent.UpdatedAt = now.Format(time.RFC3339)
	return agent
}

// probeClaude performs the request and normalizes the payload.
func probeClaude(ctx context.Context, opts ClaudeOptions, token string) ([]Limit, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("Accept", "application/json")

	resp, err := opts.Client.Do(req)
	if err != nil {
		// Nothing answered. Distinct from any HTTP status, including an error
		// one: a transport failure warrants a fast retry, a server that
		// answered does not.
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			return nil, fmt.Errorf("rate-limited by the usage endpoint; retry after %ss", ra)
		}
		return nil, errors.New("rate-limited by the usage endpoint")
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, ErrRejected
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("usage endpoint returned %d", resp.StatusCode)
	}

	var payload usagePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse usage payload: %w", err)
	}
	return normalizeClaude(payload), nil
}

// normalizeClaude turns the payload into the contract.
//
// **`limits[]` is the source of truth, and the flat buckets are only a
// fallback.** That is the opposite of the obvious reading, and it is what the
// live payload demands:
//
//   - The model-scoped weekly allowance exists ONLY in limits[]. The matching
//     legacy buckets (seven_day_opus, seven_day_sonnet) sit at null, so a
//     collector reading buckets alone silently drops a limit the account is
//     actively spending against.
//   - The payload also carries codenamed top-level buckets — amber_ladder,
//     nimbus_quill, tangelo and others. Most are null; nimbus_quill is not, and
//     has no limits[] entry. Iterating buckets "to be thorough" therefore
//     invents a meter for something with no name a user would recognise.
//
// So: read limits[] when present. Fall back to the two well-known buckets only
// when it is absent entirely.
func normalizeClaude(p usagePayload) []Limit {
	if len(p.Limits) > 0 {
		return normalizeEntries(p.Limits)
	}
	return normalizeBuckets(p)
}

func normalizeEntries(entries []entry) []Limit {
	raw := make([]float64, 0, len(entries))
	for _, e := range entries {
		if e.Percent != nil {
			raw = append(raw, *e.Percent)
		}
	}
	scale := scaleOf(raw)

	// A model can hold more than one scoped window, so the dedup key is the
	// PAIR (model, window) — not the model alone.
	seen := make(map[string]struct{}, len(entries))
	out := make([]Limit, 0, len(entries))
	for _, e := range entries {
		if e.Percent == nil {
			continue
		}
		window := windowFromKind(e.Kind)
		if window == "" {
			continue
		}
		model := ""
		if e.Scope != nil && e.Scope.Model != nil {
			model = e.Scope.Model.DisplayName
			if model == "" {
				model = e.Scope.Model.ID
			}
		}
		key := model + "\x00" + window
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, Limit{
			Label:    claudeLabel(model, window),
			Percent:  toFraction(*e.Percent, scale),
			Severity: e.Severity,
			ResetsAt: normalizeReset(decodeReset(e.ResetsAt)),
		})
	}
	return out
}

// claudeLabel names a window the way the panel reads it: an account-wide
// window gets a human name, a scoped one is prefixed with its model.
func claudeLabel(model, window string) string {
	if model != "" {
		return model + " " + window
	}
	switch window {
	case "Session":
		return "Session (5-hour)"
	case "Weekly":
		return "Weekly (7-day)"
	default:
		return window
	}
}

// normalizeBuckets is the fallback for a payload with no limits[] at all. It
// reads ONLY the two well-known keys — never every bucket present.
func normalizeBuckets(p usagePayload) []Limit {
	raw := make([]float64, 0, 2)
	for _, b := range []*bucket{p.FiveHour, p.SevenDay} {
		if b != nil && b.Utilization != nil {
			raw = append(raw, *b.Utilization)
		}
	}
	scale := scaleOf(raw)

	out := make([]Limit, 0, 2)
	add := func(label string, b *bucket) {
		if b == nil || b.Utilization == nil {
			return
		}
		out = append(out, Limit{
			Label:    label,
			Percent:  toFraction(*b.Utilization, scale),
			ResetsAt: normalizeReset(decodeReset(b.ResetsAt)),
		})
	}
	add("Session (5-hour)", p.FiveHour)
	add("Weekly (7-day)", p.SevenDay)
	return out
}

// decodeReset unwraps the raw JSON of a reset field into the three shapes
// normalizeReset understands.
func decodeReset(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	return v
}
