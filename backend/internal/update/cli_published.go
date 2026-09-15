package update

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// Published-version verdicts, as they ride the wire. Mirrors
// runtime.VersionStatus so a client never has to import a provider's words.
const (
	// PublishedUnknown is no verdict. It is the zero value on purpose: a row
	// that nobody could compare must never read as up to date.
	PublishedUnknown = ""
	// PublishedCurrent means the install is at or ahead of what its own
	// channel publishes.
	PublishedCurrent = "current"
	// PublishedBehind means the install is genuinely older than what its own
	// channel publishes.
	PublishedBehind = "behind"
)

// defaultPublishedInterval is the slow beat. A CLI release is news measured in
// days, and each ask is a network request — for codex, a spawned CLI — so there
// is nothing to gain from asking as often as detection runs.
const defaultPublishedInterval = 6 * time.Hour

// defaultPublishedTimeout bounds one provider's published-version check. Claude
// is one HTTP request; codex goes through `codex doctor`, measured at ~1.2s, so
// this is a guard against a hung registry or CLI, not a budget.
const defaultPublishedTimeout = 45 * time.Second

// CLIPublished is what a provider CLI's own release channel publishes, and the
// verdict on the install against it.
//
// Status is three-valued and never a bool: PublishedCurrent, PublishedBehind,
// or PublishedUnknown (""), which means no verdict. The channel is always the
// one the install follows, chosen by the provider library — agentique never
// picks one, because Claude's `latest` and `stable` were measured ten patch
// versions apart and comparing against the wrong one manufactures a "behind".
type CLIPublished struct {
	// Status is the verdict; "" is no verdict, never "up to date".
	Status string `json:"status,omitempty"`
	// Version is the published version. It can be set with no verdict: "what
	// is on this channel" is a fair answer even when "am I behind" is not.
	Version string `json:"version,omitempty"`
	// Channel is the release channel consulted, "" for a source with none.
	Channel string `json:"channel,omitempty"`
	// Source names the service that answered ("npm-registry",
	// "release-channel", "homebrew-cask", "provider-doctor"). A label.
	Source string `json:"source,omitempty"`
	// Reason is prose explaining why there is no verdict, "" when there is
	// one. Provider-written, meant to be shown as-is.
	Reason string `json:"reason,omitempty"`
	// CheckedAt is when this answer was obtained, RFC3339 UTC. A transient
	// failure leaves the previous answer standing, so this is also how old it
	// is.
	CheckedAt string `json:"checkedAt"`
}

// publishedEntry is one provider's last answer and the install it was about.
type publishedEntry struct {
	answer CLIPublished
	// installed is the version the provider compared against. When detection
	// has since seen a different version, the verdict describes a binary that
	// is no longer there.
	installed string
	// fingerprint is the detected install at the time of asking. An unknown
	// is a property of an install, so it is re-asked only once that changes.
	fingerprint string
	// unknown marks runtime.ErrPublishedVersionUnknown: a stable answer, not a
	// failure.
	unknown bool
}

// forInstall returns the answer as it applies to the install detected now. A
// verdict about a different version is withdrawn, keeping the published
// number: after an update lands between two slow beats, "behind" would be the
// most confidently wrong thing the row could say.
func (e publishedEntry) forInstall(installed string) *CLIPublished {
	out := e.answer
	if out.Status != PublishedUnknown && e.installed != installed {
		out.Status = PublishedUnknown
		out.Reason = "the installed version changed since this was checked"
	}
	return &out
}

// publishedLoop asks on the slow beat, and again for any provider whose
// install detection has seen change.
func (p *CLIProbe) publishedLoop(ctx context.Context) {
	p.RefreshPublished(ctx)
	t := time.NewTicker(p.publishedInterval)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			p.RefreshPublished(ctx)
		case <-p.installChanged:
			p.refreshPublished(ctx, true)
		}
	}
}

// RefreshPublished is one slow tick: it asks every installed provider that can
// report, except one whose install already answered that there is no
// trustworthy source — that answer does not change until the install does.
func (p *CLIProbe) RefreshPublished(ctx context.Context) {
	p.refreshPublished(ctx, false)
}

// refreshPublished asks the providers that need asking. onlyChanged narrows it
// to installs that differ from the one the last answer was about, which is what
// a nudge from detection wants.
func (p *CLIProbe) refreshPublished(ctx context.Context, onlyChanged bool) {
	for _, target := range p.publishedTargets(onlyChanged) {
		p.askPublished(ctx, target)
	}
}

type publishedTarget struct {
	provider    string
	fingerprint string
}

// publishedTargets picks, under the lock, which providers this pass asks. Only
// providers with a detected row qualify: a CLI that is not installed has no row
// to carry an answer, and asking codex would spawn it to find that out.
func (p *CLIProbe) publishedTargets(onlyChanged bool) []publishedTarget {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []publishedTarget
	for _, row := range p.cached {
		if _, ok := p.reporters[row.provider]; !ok {
			continue
		}
		fp := installFingerprint(row.status)
		prev, asked := p.published[row.provider]
		sameInstall := asked && prev.fingerprint == fp
		if onlyChanged && sameInstall {
			continue
		}
		if sameInstall && prev.unknown {
			continue
		}
		out = append(out, publishedTarget{provider: row.provider, fingerprint: fp})
	}
	return out
}

// askPublished asks one provider and records the answer. A transient failure
// records nothing, so the previous answer stands until the next tick.
func (p *CLIProbe) askPublished(ctx context.Context, target publishedTarget) {
	reporter := p.reporters[target.provider]
	checkCtx, cancel := context.WithTimeout(ctx, p.publishedTimeout)
	defer cancel()

	pub, err := reporter.PublishedVersion(checkCtx)
	now := time.Now().UTC().Format(time.RFC3339)
	switch {
	case errors.Is(err, runtime.ErrPublishedVersionUnknown):
		p.storePublished(target.provider, publishedEntry{
			answer:      CLIPublished{Reason: unknownReason(err), CheckedAt: now},
			fingerprint: target.fingerprint,
			unknown:     true,
		})
	case err != nil:
		slog.Info("update: cli published-version check failed, keeping last answer",
			"provider", target.provider, "error", err)
	case pub == nil:
		slog.Info("update: cli published-version check returned nothing, keeping last answer",
			"provider", target.provider)
	default:
		p.storePublished(target.provider, publishedEntry{
			answer:      publishedAnswer(pub, now),
			installed:   pub.Installed,
			fingerprint: target.fingerprint,
		})
	}
}

func (p *CLIProbe) storePublished(provider string, e publishedEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published[provider] = e
}

// publishedAnswer maps the library's report onto the wire. Status.Known() is
// the only gate on a verdict: anything the library did not call current or
// behind is no verdict here either.
func publishedAnswer(pub *runtime.Published, checkedAt string) CLIPublished {
	out := CLIPublished{
		Version:   pub.Version,
		Channel:   pub.Channel,
		Source:    pub.Source,
		CheckedAt: checkedAt,
	}
	switch pub.Status {
	case runtime.VersionStatusCurrent:
		out.Status = PublishedCurrent
	case runtime.VersionStatusBehind:
		out.Status = PublishedBehind
	default:
		out.Status = PublishedUnknown
		out.Reason = pub.Reason
	}
	return out
}

// unknownReason is the provider's explanation with the neutral sentinel's own
// words trimmed off the end: every such error ends in the same sentence, and
// the part worth showing is what came before it.
func unknownReason(err error) string {
	msg := strings.TrimSuffix(err.Error(), ": "+runtime.ErrPublishedVersionUnknown.Error())
	if msg == runtime.ErrPublishedVersionUnknown.Error() {
		return "no trustworthy published-version source for this install"
	}
	return msg
}
