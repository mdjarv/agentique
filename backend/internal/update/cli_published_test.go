package update

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// fakeReporter stands in for a connector implementing
// runtime.PublishedVersionReportable. What is tested is only what agentique
// adds: which beat asks, what survives a failure, and how a verdict rides the
// wire.
type fakeReporter struct {
	pub         *runtime.Published
	err         error
	calls       int
	sawDeadline bool
}

func (f *fakeReporter) PublishedVersion(ctx context.Context) (*runtime.Published, error) {
	f.calls++
	_, f.sawDeadline = ctx.Deadline()
	return f.pub, f.err
}

func claudeInstall(version string) *fakeInspector {
	return &fakeInspector{info: &runtime.Install{
		Tool: "claude", Path: "/home/u/.local/bin/claude", Method: runtime.InstallMethodNative, Version: version,
	}}
}

func newPublishedProbe(in *fakeInspector, rep *fakeReporter) *CLIProbe {
	p := NewCLIProbe(
		map[string]runtime.InstallInspectable{"claude": in},
		time.Hour,
		WithPublishedReporters(map[string]runtime.PublishedVersionReportable{"claude": rep}),
	)
	p.Refresh(context.Background())
	return p
}

func onlyRow(t *testing.T, p *CLIProbe) CLIStatus {
	t.Helper()
	got := p.Status()
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %+v", got)
	}
	return got[0]
}

func published(version, installed string, status runtime.VersionStatus) *runtime.Published {
	pub := runtime.NewPublished("claude", version, installed, status)
	pub.Channel = "latest"
	pub.Source = "release-channel"
	return pub
}

func TestCLIPublishedBehindVerdict(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{pub: published("2.1.245", "2.1.241", runtime.VersionStatusBehind)}
	p := newPublishedProbe(claudeInstall("2.1.241"), rep)
	p.RefreshPublished(context.Background())

	got := onlyRow(t, p).Published
	if got == nil || got.Status != PublishedBehind || got.Version != "2.1.245" {
		t.Fatalf("behind verdict not carried: %+v", got)
	}
	if got.Channel != "latest" || got.Source != "release-channel" || got.CheckedAt == "" {
		t.Errorf("provenance dropped: %+v", got)
	}
	if !rep.sawDeadline {
		t.Error("published check ran without a deadline")
	}
}

func TestCLIPublishedCurrentVerdict(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{pub: published("2.1.241", "2.1.241", runtime.VersionStatusCurrent)}
	p := newPublishedProbe(claudeInstall("2.1.241"), rep)
	p.RefreshPublished(context.Background())

	if got := onlyRow(t, p).Published; got == nil || got.Status != PublishedCurrent || got.Reason != "" {
		t.Fatalf("current verdict not carried: %+v", got)
	}
}

func TestCLIPublishedNotAskedIsAbsent(t *testing.T) {
	t.Parallel()
	p := newPublishedProbe(claudeInstall("2.1.241"), &fakeReporter{})
	// Nobody has looked: that is no field at all, never an empty verdict.
	if got := onlyRow(t, p).Published; got != nil {
		t.Fatalf("an unasked row must carry no published answer: %+v", got)
	}
}

func TestCLIPublishedNoVerdictRendersNone(t *testing.T) {
	t.Parallel()
	pub := published("2.1.231", "2.1.241", runtime.VersionStatusUnknown)
	pub.Reason = "no verdict: the channel consulted is not the one this install tracks"
	p := newPublishedProbe(claudeInstall("2.1.241"), &fakeReporter{pub: pub})
	p.RefreshPublished(context.Background())

	got := onlyRow(t, p).Published
	if got == nil || got.Status != PublishedUnknown {
		t.Fatalf("no verdict must stay no verdict: %+v", got)
	}
	if got.Version != "2.1.231" || got.Reason == "" {
		t.Errorf("the number and the reason are still worth carrying: %+v", got)
	}
}

func TestCLIPublishedUnknownIsStableNotRetried(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{err: fmt.Errorf("claude: installed through mise: %w", runtime.ErrPublishedVersionUnknown)}
	in := claudeInstall("2.1.241")
	p := newPublishedProbe(in, rep)
	p.RefreshPublished(context.Background())

	got := onlyRow(t, p).Published
	if got == nil || got.Status != PublishedUnknown || got.Reason != "claude: installed through mise" {
		t.Fatalf("an unknown is an answer with its reason: %+v", got)
	}

	// The same install cannot grow a trustworthy source, so the next tick
	// does not ask again.
	p.RefreshPublished(context.Background())
	if rep.calls != 1 {
		t.Errorf("unknown was re-asked for an unchanged install: %d calls", rep.calls)
	}

	// A different install can.
	in.info.Version = "2.1.245"
	p.Refresh(context.Background())
	p.RefreshPublished(context.Background())
	if rep.calls != 2 {
		t.Errorf("a changed install must be re-asked: %d calls", rep.calls)
	}
}

func TestCLIPublishedTransientFailureKeepsPreviousAnswer(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{pub: published("2.1.245", "2.1.241", runtime.VersionStatusBehind)}
	p := newPublishedProbe(claudeInstall("2.1.241"), rep)
	p.RefreshPublished(context.Background())
	first := onlyRow(t, p).Published

	rep.pub, rep.err = nil, errors.New("dial tcp: i/o timeout")
	p.RefreshPublished(context.Background())

	got := onlyRow(t, p).Published
	if got == nil || *got != *first {
		t.Fatalf("a transient failure must leave the last answer standing: was %+v, now %+v", first, got)
	}
	// And unlike an unknown, it is retried on the next tick.
	p.RefreshPublished(context.Background())
	if rep.calls != 3 {
		t.Errorf("a transient failure must be retried: %d calls", rep.calls)
	}
}

func TestCLIPublishedVerdictWithdrawnWhenInstallMoves(t *testing.T) {
	t.Parallel()
	in := claudeInstall("2.1.241")
	rep := &fakeReporter{pub: published("2.1.245", "2.1.241", runtime.VersionStatusBehind)}
	p := newPublishedProbe(in, rep)
	p.RefreshPublished(context.Background())

	// The CLI updated itself between two slow beats; detection sees it first.
	in.info.Version = "2.1.245"
	p.Refresh(context.Background())

	got := onlyRow(t, p).Published
	if got == nil || got.Status != PublishedUnknown || got.Version != "2.1.245" {
		t.Fatalf("a verdict about the previous binary must be withdrawn: %+v", got)
	}
	select {
	case <-p.installChanged:
	default:
		t.Error("detection seeing a new install must nudge the published loop")
	}
}

func TestCLIPublishedSkipsProvidersNotInstalled(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{pub: published("0.150.0", "0.148.0", runtime.VersionStatusBehind)}
	p := NewCLIProbe(
		map[string]runtime.InstallInspectable{"codex": &fakeInspector{err: errors.New("codex CLI not found on PATH")}},
		time.Hour,
		WithPublishedReporters(map[string]runtime.PublishedVersionReportable{"codex": rep}),
	)
	p.Refresh(context.Background())
	p.RefreshPublished(context.Background())
	if rep.calls != 0 {
		t.Errorf("a CLI that is not installed must not be asked (codex would spawn): %d calls", rep.calls)
	}
}

func TestCLIPublishedStatusPerformsNoIO(t *testing.T) {
	t.Parallel()
	rep := &fakeReporter{pub: published("2.1.245", "2.1.241", runtime.VersionStatusBehind)}
	p := newPublishedProbe(claudeInstall("2.1.241"), rep)
	p.RefreshPublished(context.Background())
	for range 10 {
		p.Status()
	}
	if rep.calls != 1 {
		t.Errorf("Status must not ask: %d calls", rep.calls)
	}
}
