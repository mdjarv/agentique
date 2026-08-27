// Package usage answers one question about this machine: how much of each AI
// subscription's rate-limit window has been spent, and when it resets.
//
// The hard part is not the display. It is getting trustworthy numbers out of
// vendors that expose them completely differently, and degrading honestly when
// one of them will not answer. Every rule in here exists because the naive
// version was wrong against the live endpoint.
//
// The split is strict: a collector per vendor produces one normalized record,
// and the client renders records without ever learning that Claude is HTTP and
// Codex is a JSON-RPC subprocess. Adding a third vendor is one collector and no
// UI change.
package usage

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is bumped when the document's shape changes incompatibly.
const SchemaVersion = 1

// Kind separates the two things this document carries.
//
// An *allowance* is spent and then resets — a rate-limit window. A *gauge* is a
// level that is simply where it is: disk. The distinction is load-bearing
// rather than cosmetic, because it decides what may claim attention. A gauge
// at 88% is the normal state of a small machine and must never escalate to a
// warning colour or take a headline; an allowance at 88% is news.
const (
	KindAllowance = "allowance"
	KindGauge     = "gauge"
)

// Document is the whole answer, one record per agent, sorted by id.
type Document struct {
	SchemaVersion int     `json:"schemaVersion"`
	Agents        []Agent `json:"agents"`
	// FetchedAt is when this document was assembled (RFC3339 UTC).
	FetchedAt string `json:"fetchedAt,omitempty"`
}

// Agent is one vendor's account of itself, or the disk gauge.
type Agent struct {
	// ID is the join key for colour and icon on the client. An unknown id must
	// still render.
	ID string `json:"id"`
	// Name is what the panel calls it.
	Name string `json:"name"`
	// Kind is KindAllowance (default, omitted) or KindGauge.
	Kind string `json:"kind,omitempty"`
	// TierLabel is the plan, e.g. "Max 20x". Display only — never derived from
	// anything that could identify the account.
	TierLabel string `json:"tierLabel,omitempty"`
	// Ready reports that Limits is a current, trustworthy answer. False means
	// the numbers (if any) are stale or absent, and UsageStatusText says why.
	Ready bool `json:"ready"`
	// UpdatedAt is when these limits were last successfully read.
	UpdatedAt string `json:"updatedAt,omitempty"`
	// UsageStatusText explains stale or missing data, in a sentence a person
	// can act on. Empty when everything is fine.
	UsageStatusText string `json:"usageStatusText,omitempty"`
	// AuthHelpText names the command that fixes an auth problem. Only the CLI
	// can mint a fresh token, so "error" is useless where this is the answer.
	AuthHelpText string `json:"authHelpText,omitempty"`
	// Limits is whatever this vendor reported. The set is NOT fixed: scoped
	// allowances come and go as the account spends against them. Never
	// hardcode the count or the labels.
	Limits []Limit `json:"limits,omitempty"`
	// TodayTokens and TodayPrompts are what this server itself spent today.
	// They come from agentique's own turn results, not from the vendor — so
	// they count work done through agentique and nothing else.
	TodayTokens  int64 `json:"todayTokens,omitempty"`
	TodayPrompts int   `json:"todayPrompts,omitempty"`
}

// Limit is one window.
type Limit struct {
	Label string `json:"label"`
	// Percent is a FRACTION, 0..1, never 0..100. It may exceed 1 — clamp when
	// drawing, never when reporting. A negative value means UNKNOWN, not zero,
	// and is filtered from every surface.
	Percent float64 `json:"percent"`
	// Severity is the vendor's own verdict where it gives one ("normal",
	// "warning", …). The server knows what counts as a warning for its own
	// window; a client-side threshold is a guess about somebody else's limit.
	Severity string `json:"severity,omitempty"`
	// ResetsAt is RFC3339 UTC. Empty on a gauge, which has nothing to reset to.
	ResetsAt string `json:"resetsAt,omitempty"`
	// Detail is an optional right-hand figure for a gauge ("9.2 GB free"),
	// where a percentage alone says less than the absolute number.
	Detail string `json:"detail,omitempty"`
}

// Unknown is the percent value meaning "we could not tell". It is filtered
// rather than drawn — a window we cannot read is not a window at zero.
const Unknown = -1.0

// Known reports whether a limit carries a usable reading.
func (l Limit) Known() bool { return l.Percent >= 0 }

// Expired reports whether this window has already rolled over.
//
// A cached percentage expires when its WINDOW rolls over, not on a timer: a
// stale 78% would misreport an allowance that has since reset to zero. A limit
// with no reset time, or one that will not parse, is NOT expired — an
// unreadable timestamp is no reason to throw away a real number.
func (l Limit) Expired(now time.Time) bool {
	if l.ResetsAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, l.ResetsAt)
	if err != nil {
		return false
	}
	return now.After(t)
}

// scaleOf decides, ONCE PER PAYLOAD, whether utilizations are percentages or
// fractions.
//
// The endpoint currently reports percentages (37.0); older payloads used
// fractions (0.37). Deciding per-value is the bug: a genuine 1.0 would render
// as 100% when it meant 1%. So the whole payload votes — if anything anywhere
// in it is >= 1, the payload is percent-scaled.
func scaleOf(values []float64) float64 {
	for _, v := range values {
		if v >= 1 {
			return 100
		}
	}
	return 1
}

// normalizeReset accepts the three shapes a reset time arrives in — an ISO
// string, epoch seconds, or epoch milliseconds — and returns RFC3339 UTC.
//
// The 1e12 test separates seconds from milliseconds: 1e12 seconds is the year
// 33658, and 1e12 milliseconds is 2001, so no real timestamp is ambiguous.
func normalizeReset(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		if t == "" {
			return ""
		}
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return ""
		}
		return parsed.UTC().Format(time.RFC3339)
	case float64:
		return epochToRFC3339(int64(t))
	case int64:
		return epochToRFC3339(t)
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			f, ferr := t.Float64()
			if ferr != nil {
				return ""
			}
			n = int64(f)
		}
		return epochToRFC3339(n)
	default:
		return ""
	}
}

func epochToRFC3339(n int64) string {
	if n <= 0 {
		return ""
	}
	if n < 1e12 {
		n *= 1000
	}
	return time.UnixMilli(n).UTC().Format(time.RFC3339)
}

// windowFromKind names a window from the vendor's own enum, never from free
// text.
//
// This is the trap that looks like nothing: a model called "Opus 5 (1M
// context)" parsed for a window yields "1M", which reads as a one-minute
// window. The live endpoint's kinds are `session`, `weekly_all` and
// `weekly_scoped`, so substring matching on the KIND is safe where substring
// matching on a display name is not.
func windowFromKind(kind string) string {
	k := strings.ToLower(kind)
	switch {
	case strings.Contains(k, "month"):
		return "Monthly"
	case strings.Contains(k, "week"), strings.Contains(k, "day"):
		return "Weekly"
	case strings.Contains(k, "hour"), strings.Contains(k, "session"):
		return "Session"
	default:
		return ""
	}
}

// windowFromMinutes names a window from its duration, for a vendor that
// reports one. Trusting a duration beats trusting a name.
func windowFromMinutes(mins int64) string {
	switch {
	case mins <= 0:
		return "window"
	case mins == 10080:
		return "Weekly (7-day)"
	case mins%1440 == 0:
		return strconv.FormatInt(mins/1440, 10) + "d window"
	case mins%60 == 0:
		return strconv.FormatInt(mins/60, 10) + "h window"
	default:
		return strconv.FormatInt(mins, 10) + "m window"
	}
}

// toFraction converts a raw utilization to the 0..1 contract, given the scale
// the whole payload voted for. Values are never clamped here — reporting a
// number above 1 is the honest thing; clamping belongs to the drawing.
func toFraction(v, scale float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Unknown
	}
	return v / scale
}
