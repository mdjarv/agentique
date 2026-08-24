package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// fakeInspector stands in for a provider connector. Detection is the provider
// library's job, so what is tested here is only what agentique adds: caching,
// ordering, and how a machine without that CLI is reported.
type fakeInspector struct {
	info *runtime.Install
	err  error
	// calls counts probes, so a cached read can be proven not to re-probe.
	calls int
	// sawDeadline records whether the probe was given a bounded context.
	sawDeadline bool
}

func (f *fakeInspector) InstallInfo(ctx context.Context) (*runtime.Install, error) {
	f.calls++
	_, ok := ctx.Deadline()
	f.sawDeadline = ok
	return f.info, f.err
}

func TestCLIProbeMapsEveryFieldItReports(t *testing.T) {
	in := &fakeInspector{info: &runtime.Install{
		Tool:           "claude",
		Path:           "/home/u/.local/bin/claude",
		RealPath:       "/home/u/.local/share/claude/versions/2.1.241",
		Version:        "2.1.241",
		Method:         runtime.InstallMethodNative,
		Source:         "path-layout",
		SelfManaged:    true,
		UpdateCmd:      "claude update",
		VersionManager: "fnm",
		PackageManager: "npm",
		Warnings:       []string{"another copy at /usr/local/bin/claude"},
	}}
	p := NewCLIProbe(map[string]runtime.InstallInspectable{"claude": in}, time.Hour)

	got := p.Refresh(context.Background())
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	st := got[0]
	if st.Tool != "claude" || st.Installed != "2.1.241" {
		t.Fatalf("tool/version not carried: %+v", st)
	}
	if st.RealPath != "/home/u/.local/share/claude/versions/2.1.241" {
		t.Errorf("realPath dropped: %q", st.RealPath)
	}
	if !st.SelfManaged || st.UpdateCmd != "claude update" {
		t.Errorf("the fields a button keys on were dropped: %+v", st)
	}
	if st.VersionManager != "fnm" || st.PackageManager != "npm" {
		t.Errorf("ownership fields dropped: %+v", st)
	}
	// The shadow warning is the whole point of C9 — losing it in the mapping
	// would be silent, and would look exactly like a healthy machine.
	if len(st.Warnings) != 1 {
		t.Errorf("warnings dropped: %+v", st.Warnings)
	}
}

func TestCLIProbeOmitsAProviderThatCannotAnswer(t *testing.T) {
	p := NewCLIProbe(map[string]runtime.InstallInspectable{
		"claude": &fakeInspector{info: &runtime.Install{Tool: "claude", Version: "2.1.241"}},
		"codex":  &fakeInspector{err: errors.New("codex CLI not found on PATH")},
	}, time.Hour)

	got := p.Refresh(context.Background())
	if len(got) != 1 || got[0].Tool != "claude" {
		t.Fatalf("a machine without codex must show one row, not an error row: %+v", got)
	}
}

func TestCLIProbeOrdersRowsStably(t *testing.T) {
	p := NewCLIProbe(map[string]runtime.InstallInspectable{
		"codex":  &fakeInspector{info: &runtime.Install{Tool: "codex", Version: "0.148.0"}},
		"claude": &fakeInspector{info: &runtime.Install{Tool: "claude", Version: "2.1.241"}},
	}, time.Hour)

	// Map iteration order would reshuffle the dialog's rows on every poll.
	for i := range 5 {
		got := p.Refresh(context.Background())
		if len(got) != 2 || got[0].Tool != "claude" || got[1].Tool != "codex" {
			t.Fatalf("refresh %d reordered rows: %+v", i, got)
		}
	}
}

func TestCLIProbeStatusReadsCacheWithoutProbing(t *testing.T) {
	in := &fakeInspector{info: &runtime.Install{Tool: "claude", Version: "2.1.241"}}
	p := NewCLIProbe(map[string]runtime.InstallInspectable{"claude": in}, time.Hour)
	p.Refresh(context.Background())

	// Status is served on every client's 15-minute beat across every machine.
	// If it probed, that would spawn `--version` per read.
	for range 10 {
		if got := p.Status(); len(got) != 1 {
			t.Fatalf("cached read lost the row: %+v", got)
		}
	}
	if in.calls != 1 {
		t.Errorf("Status must not probe: %d probes for 1 refresh + 10 reads", in.calls)
	}
}

func TestCLIProbeBoundsEachProbe(t *testing.T) {
	in := &fakeInspector{info: &runtime.Install{Tool: "claude"}}
	p := NewCLIProbe(map[string]runtime.InstallInspectable{"claude": in}, time.Hour)
	// context.Background() has no deadline; the probe must add its own, or a
	// wedged binary parks the poll loop forever.
	p.Refresh(context.Background())
	if !in.sawDeadline {
		t.Error("detection ran without a deadline")
	}
}

func TestCLIProbeSurvivesAProviderReportingNothing(t *testing.T) {
	p := NewCLIProbe(map[string]runtime.InstallInspectable{
		"claude": &fakeInspector{info: nil, err: nil},
	}, time.Hour)
	if got := p.Refresh(context.Background()); len(got) != 0 {
		t.Fatalf("a nil install with no error must not become a row: %+v", got)
	}
}
