package session

import (
	"errors"
	"testing"
)

// TestInterceptBrowserTool_LaunchPerMode is the regression guard for the
// fullAuto browser bug: fullAuto (runtime.AutoApproveAll) bypasses the approval
// pump, so the browser-tool interceptor is the only hook that can launch Chrome
// before the @playwright/mcp call dispatches. In every other mode the pump's
// handlePendingChange branch owns the launch, so the interceptor must no-op
// (return nil to fall through) to avoid a redundant launch.
//
// The interceptor runs ahead of the AutoApproveAll short-circuit by construction
// — agentkit's runtime/approval.go consults interceptors before the mode check
// in handleToolPermission — so exercising the interceptor directly mirrors what
// the runtime does on the permission goroutine.
func TestInterceptBrowserTool_LaunchPerMode(t *testing.T) {
	cases := []struct {
		auto       string
		perm       string
		wantLaunch bool
	}{
		{"manual", "default", false},
		{"manual", "acceptEdits", false},
		{"manual", "plan", false},
		{"auto", "default", false},
		{"auto", "plan", false},
		{"fullAuto", "default", true},
		{"fullAuto", "plan", true},
	}

	for _, c := range cases {
		t.Run(c.auto+"/"+c.perm, func(t *testing.T) {
			sess := newPermTestSession(c.auto, c.perm)
			defer sess.cancelCtx()

			var launches int
			sess.SetEnsureBrowserFunc(func() error { launches++; return nil })

			resp, err := sess.interceptBrowserTool(nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Success / no-op both fall through to the normal flow.
			if resp != nil {
				t.Fatalf("expected nil decision (fall through), got %+v", resp)
			}
			if c.wantLaunch && launches != 1 {
				t.Errorf("expected EnsureBrowser to run once in %s, ran %d times", c.auto, launches)
			}
			if !c.wantLaunch && launches != 0 {
				t.Errorf("expected EnsureBrowser NOT to run in %s (pump owns launch), ran %d times", c.auto, launches)
			}
		})
	}
}

// TestInterceptBrowserTool_DeniesOnLaunchFailure verifies a fullAuto launch
// failure surfaces the actionable EnsureBrowser message as a deny, rather than
// letting @playwright/mcp fail opaquely with a CDP ECONNREFUSED.
func TestInterceptBrowserTool_DeniesOnLaunchFailure(t *testing.T) {
	sess := newPermTestSession("fullAuto", "default")
	defer sess.cancelCtx()

	launchErr := errors.New("launch browser: chrome not found (run: npx playwright install-deps chromium)")
	sess.SetEnsureBrowserFunc(func() error { return launchErr })

	resp, err := sess.interceptBrowserTool(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a deny decision on launch failure, got nil (fall-through would allow the call)")
	}
	if resp.Allow {
		t.Error("expected Allow=false on launch failure")
	}
	if resp.DenyMessage != launchErr.Error() {
		t.Errorf("deny message = %q, want the actionable launch error %q", resp.DenyMessage, launchErr.Error())
	}
}

// TestAgentiqueInterceptorsRegistersBrowserTools asserts every @playwright/mcp
// tool gets the launch interceptor wired (so fullAuto launches Chrome), and that
// browserToolNames stays consistent with the isBrowserTool prefix that backs the
// pump path.
func TestAgentiqueInterceptorsRegistersBrowserTools(t *testing.T) {
	sess := newPermTestSession("fullAuto", "default")
	defer sess.cancelCtx()

	m := sess.agentiqueInterceptors()
	for _, name := range browserToolNames {
		full := browserToolPrefix + name
		if !isBrowserTool(full) {
			t.Errorf("browserToolNames entry %q does not carry the browser prefix %q", name, browserToolPrefix)
		}
		if _, ok := m[full]; !ok {
			t.Errorf("no interceptor registered for browser tool %q (fullAuto would skip its lazy launch)", full)
		}
	}
}
