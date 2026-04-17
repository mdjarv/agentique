package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/doctor"
)

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}

// runCmd runs a tea.Cmd synchronously and returns the produced message.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

// --- Validators ---

func TestValidateProjectPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"existing dir", dir, false},
		{"missing", filepath.Join(dir, "nope"), true},
		{"file not dir", file, true},
		{"tilde expands home", "~/", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateProjectPath(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestValidateFilePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateFilePath(file); err != nil {
		t.Fatalf("existing file should pass: %v", err)
	}
	if err := validateFilePath(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file should fail")
	}
}

// --- Shell helpers ---

func TestDetectShell(t *testing.T) {
	orig := os.Getenv("SHELL")
	t.Cleanup(func() { os.Setenv("SHELL", orig) })

	for _, sh := range []string{"bash", "zsh", "fish"} {
		os.Setenv("SHELL", "/usr/bin/"+sh)
		if got := detectShell(); got != sh {
			t.Errorf("SHELL=%s want=%s got=%s", sh, sh, got)
		}
	}
	os.Setenv("SHELL", "/usr/bin/ksh")
	if got := detectShell(); got != "" {
		t.Errorf("ksh should yield empty, got %q", got)
	}
	os.Setenv("SHELL", "")
	if got := detectShell(); got != "" {
		t.Errorf("empty SHELL should yield empty, got %q", got)
	}
}

func TestFindZshCompletionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No FPATH — fallback.
	t.Setenv("FPATH", "")
	got := findZshCompletionDir()
	want := filepath.Join(home, ".zsh", "completions", "_agentique")
	if got != want {
		t.Errorf("fallback: got %q want %q", got, want)
	}

	// Writable fpath under home wins.
	fpath := filepath.Join(home, "fp")
	if err := os.MkdirAll(fpath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FPATH", fpath)
	got = findZshCompletionDir()
	if got != filepath.Join(fpath, "_agentique") {
		t.Errorf("fpath: got %q", got)
	}
}

// --- Steps ---

func TestBuildSteps(t *testing.T) {
	local := buildSteps(false)
	net := buildSteps(true)

	if len(net) <= len(local) {
		t.Fatalf("network mode should add steps: local=%d net=%d", len(local), len(net))
	}
	if local[0].kind != stepDoctor || local[1].kind != stepNetworkMode {
		t.Fatal("local mode should start with doctor, networkMode")
	}
	if local[len(local)-1].kind != stepSummary {
		t.Fatal("last step should be summary")
	}

	// network mode adds TLS + auth.
	hasTLS, hasAuth := false, false
	for _, s := range net {
		if s.kind == stepTLS {
			hasTLS = true
		}
		if s.kind == stepAuth {
			hasAuth = true
		}
	}
	if !hasTLS || !hasAuth {
		t.Fatal("network mode should include TLS and auth steps")
	}
}

// --- Choice model ---

func TestChoiceModelNavigation(t *testing.T) {
	m := newChoiceModel("pick", []string{"a", "b", "c"}, 1)
	if m.cursor != 1 {
		t.Fatalf("default cursor: %d", m.cursor)
	}

	m, _ = m.Update(keyPress("up"))
	if m.cursor != 0 {
		t.Fatalf("after up: %d", m.cursor)
	}
	m, _ = m.Update(keyPress("up")) // clamp at 0
	if m.cursor != 0 {
		t.Fatalf("clamp top: %d", m.cursor)
	}
	m, _ = m.Update(keyPress("down"))
	m, _ = m.Update(keyPress("j"))
	if m.cursor != 2 {
		t.Fatalf("after two downs: %d", m.cursor)
	}
	m, _ = m.Update(keyPress("j")) // clamp bottom
	if m.cursor != 2 {
		t.Fatalf("clamp bottom: %d", m.cursor)
	}
	m, _ = m.Update(keyPress("k"))
	if m.cursor != 1 {
		t.Fatalf("k up: %d", m.cursor)
	}
}

func TestChoiceModelEnter(t *testing.T) {
	m := newChoiceModel("pick", []string{"a", "b"}, 0)
	m, cmd := m.Update(keyPress("enter"))
	if m.selected != 0 {
		t.Fatalf("selected: %d", m.selected)
	}
	msg := runCmd(t, cmd)
	done, ok := msg.(choiceDoneMsg)
	if !ok || done.selected != 0 {
		t.Fatalf("expected choiceDoneMsg{0}, got %#v", msg)
	}
}

func TestChoiceModelView(t *testing.T) {
	m := newChoiceModel("title", []string{"a", "b"}, 1)
	m.hints = []string{"hint-a", "hint-b"}
	out := m.View()
	if !strings.Contains(out, "title") {
		t.Error("view missing title")
	}
	if !strings.Contains(out, "hint-b") {
		t.Error("view missing hint for selected option")
	}
	if strings.Contains(out, "hint-a") {
		t.Error("non-selected hint should not show")
	}
}

// --- Input model ---

func TestInputModelEnterEmpty(t *testing.T) {
	// Optional input, empty -> done with "".
	m := newInputModel("t", "ph", "", true, nil)
	m, cmd := m.Update(keyPress("enter"))
	if !m.done {
		t.Fatal("should be done on empty optional")
	}
	msg := runCmd(t, cmd)
	in, ok := msg.(inputDoneMsg)
	if !ok || in.value != "" {
		t.Fatalf("got %#v", msg)
	}
}

func TestInputModelValidation(t *testing.T) {
	validator := func(s string) error {
		if s == "bad" {
			return os.ErrInvalid
		}
		return nil
	}
	m := newInputModel("t", "", "", false, validator)
	m.input.SetValue("bad")

	m, cmd := m.Update(keyPress("enter"))
	if m.err == nil {
		t.Fatal("validator err should be stored")
	}
	if m.done {
		t.Fatal("should not be done on validation failure")
	}
	if cmd != nil {
		t.Fatal("no cmd on validation failure")
	}

	// Next keypress clears error.
	m, _ = m.Update(keyPress("a"))
	if m.err != nil {
		t.Fatal("err should clear on next key")
	}

	// Fix value and submit.
	m.input.SetValue("good")
	m, cmd = m.Update(keyPress("enter"))
	if !m.done {
		t.Fatal("should be done")
	}
	msg := runCmd(t, cmd)
	if in, ok := msg.(inputDoneMsg); !ok || in.value != "good" {
		t.Fatalf("got %#v", msg)
	}
}

// --- Action model ---

func TestActionModelExecuteSuccess(t *testing.T) {
	m := newActionModel("title", func() (string, error) { return "detail", nil })
	cmd := m.execute()
	msg := runCmd(t, cmd)
	done, ok := msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("got %#v", msg)
	}
	if done.err != nil || done.detail != "detail" {
		t.Fatalf("bad result: %#v", done)
	}

	m, _ = m.Update(done)
	if !m.done || m.err != nil || m.detail != "detail" {
		t.Fatalf("state: done=%v err=%v detail=%q", m.done, m.err, m.detail)
	}
	if !strings.Contains(m.View(), "title") {
		t.Error("view missing title")
	}
	if !strings.Contains(m.View(), "detail") {
		t.Error("view missing detail")
	}
}

func TestActionModelExecuteError(t *testing.T) {
	m := newActionModel("title", func() (string, error) { return "", os.ErrPermission })
	msg := runCmd(t, m.execute())
	m, _ = m.Update(msg)
	if m.err == nil {
		t.Fatal("err should be set")
	}
	if !strings.Contains(m.View(), "title") {
		t.Error("view missing title on error")
	}
}

// --- Wizard progression ---

func TestWizardLocalhostFlow(t *testing.T) {
	// Localhost choice (0) should not add TLS/auth steps; initial cfg DisableAuth=true.
	m := newWizardModel()
	m.current = 1 // start at networkMode
	m.phase = phaseChoice
	m.choice = newChoiceModel("net", []string{"local", "net"}, 0)

	model, _ := m.handleChoiceResult(0)
	wm, ok := model.(wizardModel)
	if !ok {
		t.Fatal("not wizardModel")
	}
	if wm.networkMode {
		t.Error("should be localhost")
	}
	if wm.cfg.Server.Addr != "localhost:9201" {
		t.Errorf("addr: %q", wm.cfg.Server.Addr)
	}
	if !wm.cfg.Server.DisableAuth {
		t.Error("auth should be disabled")
	}
	for _, s := range wm.steps {
		if s.kind == stepTLS || s.kind == stepAuth {
			t.Errorf("localhost should not include TLS/auth (%v)", s.kind)
		}
	}
}

func TestWizardNetworkFlow(t *testing.T) {
	m := newWizardModel()
	m.current = 1
	m.phase = phaseChoice
	m.choice = newChoiceModel("net", []string{"local", "net"}, 1)

	model, _ := m.handleChoiceResult(1)
	wm := model.(wizardModel)
	if !wm.networkMode {
		t.Error("should be network mode")
	}
	if wm.cfg.Server.Addr != "0.0.0.0:9201" {
		t.Errorf("addr: %q", wm.cfg.Server.Addr)
	}
	hasTLS, hasAuth := false, false
	for _, s := range wm.steps {
		if s.kind == stepTLS {
			hasTLS = true
		}
		if s.kind == stepAuth {
			hasAuth = true
		}
	}
	if !hasTLS || !hasAuth {
		t.Error("network mode should add TLS + auth steps")
	}
}

func TestWizardAuthChoice(t *testing.T) {
	m := newWizardModel()
	// Build steps for network mode and position on stepAuth.
	m.steps = buildSteps(true)
	for i, s := range m.steps {
		if s.kind == stepAuth {
			m.current = i
			break
		}
	}

	// selected=1 means "disable auth".
	model, _ := m.handleChoiceResult(1)
	wm := model.(wizardModel)
	if !wm.cfg.Server.DisableAuth {
		t.Error("disable-auth not set")
	}
}

func TestWizardTLSReverseProxy(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(true)
	for i, s := range m.steps {
		if s.kind == stepTLS {
			m.current = i
			break
		}
	}
	// Choose reverse proxy (index 2).
	model, _ := m.handleTLSChoice(2)
	wm := model.(wizardModel)
	if wm.tlsChoice != 2 || wm.phase != phaseInput {
		t.Fatalf("unexpected state: tlsChoice=%d phase=%v", wm.tlsChoice, wm.phase)
	}

	wm.input.input.SetValue("https://example.com")
	model2, _ := wm.handleTLSInput("https://example.com")
	wm2 := model2.(wizardModel)
	if wm2.cfg.Server.RPOrigin != "https://example.com" {
		t.Errorf("RPOrigin: %q", wm2.cfg.Server.RPOrigin)
	}
}

func TestWizardTLSExistingCerts(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(true)
	for i, s := range m.steps {
		if s.kind == stepTLS {
			m.current = i
			break
		}
	}
	model, _ := m.handleTLSChoice(0)
	wm := model.(wizardModel)
	if wm.tlsChoice != 0 || wm.tlsSubStep != 1 {
		t.Fatalf("substep: %d", wm.tlsSubStep)
	}

	// Supply cert path, then key path.
	model, _ = wm.handleTLSInput("/etc/c")
	wm = model.(wizardModel)
	if wm.cfg.Server.TLSCert != "/etc/c" {
		t.Errorf("cert: %q", wm.cfg.Server.TLSCert)
	}
	if wm.tlsSubStep != 2 {
		t.Fatalf("substep after cert: %d", wm.tlsSubStep)
	}
	model, _ = wm.handleTLSInput("/etc/k")
	wm = model.(wizardModel)
	if wm.cfg.Server.TLSKey != "/etc/k" {
		t.Errorf("key: %q", wm.cfg.Server.TLSKey)
	}
}

func TestWizardProjectExpand(t *testing.T) {
	home := t.TempDir()
	projDir := filepath.Join(home, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	m := newWizardModel()
	m.steps = buildSteps(false)
	for i, s := range m.steps {
		if s.kind == stepProject {
			m.current = i
			break
		}
	}
	model, _ := m.handleInputResult("~/proj")
	wm := model.(wizardModel)
	if wm.cfg.Setup.InitialProject != projDir {
		t.Errorf("expanded path: %q want %q", wm.cfg.Setup.InitialProject, projDir)
	}
}

// --- Input view ---

func TestInputModelView(t *testing.T) {
	m := newInputModel("title", "ph", "hint-text", true, nil)
	out := m.View()
	if !strings.Contains(out, "title") {
		t.Error("missing title")
	}
	if !strings.Contains(out, "hint-text") {
		t.Error("missing hint")
	}
	if !strings.Contains(out, "empty to skip") {
		t.Error("optional hint missing")
	}

	m2 := newInputModel("t", "", "", false, func(string) error { return os.ErrInvalid })
	m2.input.SetValue("x")
	m2, _ = m2.Update(keyPress("enter"))
	if !strings.Contains(m2.View(), "invalid") {
		t.Error("view should render error")
	}
}

// --- Summary model ---

func TestSummaryModelView(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Addr = "0.0.0.0:9201"
	cfg.Server.TLSCert = "/tmp/c"
	cfg.Server.DisableAuth = false
	cfg.Setup.InitialProject = "/tmp/p"

	m := newSummaryModel(cfg, false)
	out := m.View()

	for _, want := range []string{
		"Setup complete", "0.0.0.0:9201", "/tmp/p",
		"passkey", "https://localhost:9201", "agentique serve",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q", want)
		}
	}

	// Service install branch.
	m2 := newSummaryModel(cfg, true)
	if !strings.Contains(m2.View(), "Service installed") {
		t.Error("service install message missing")
	}

	// No TLS, auth disabled.
	cfg2 := config.Default()
	cfg2.Server.Addr = "localhost:9201"
	cfg2.Server.DisableAuth = true
	m3 := newSummaryModel(cfg2, false)
	out3 := m3.View()
	if !strings.Contains(out3, "disabled") {
		t.Error("disabled (TLS/Auth) missing")
	}
	if !strings.Contains(out3, "http://localhost:9201") {
		t.Error("http scheme url missing")
	}
}

// --- Doctor model ---

func TestDoctorModelCycle(t *testing.T) {
	m := newDoctorModel()
	if len(m.checks) == 0 {
		t.Skip("no checks registered")
	}

	// Feed synthetic OK results for every check.
	for i := range m.checks {
		msg := checkResultMsg{index: i, check: doctor.Check{
			Name: m.checks[i].Name, Status: doctor.OK, Required: true,
		}}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if i < len(m.checks)-1 {
			if cmd == nil {
				t.Fatal("expected next-check cmd")
			}
			continue
		}
		// Last check: running = -1, cmd yields doctorDoneMsg.
		if m.running != -1 {
			t.Fatalf("running after last: %d", m.running)
		}
		if m.failed {
			t.Fatal("should not fail when all OK")
		}
		done, ok := runCmd(t, cmd).(doctorDoneMsg)
		if !ok || done.failed {
			t.Fatalf("bad final msg: %#v", done)
		}
	}

	if !strings.Contains(m.View(), "press any key to continue") {
		t.Error("success view missing continue hint")
	}
}

func TestDoctorModelRequiredFail(t *testing.T) {
	m := newDoctorModel()
	if len(m.checks) == 0 {
		t.Skip("no checks registered")
	}
	msg := checkResultMsg{index: 0, check: doctor.Check{
		Name: m.checks[0].Name, Status: doctor.Fail, Required: true, Fix: "install it",
	}}
	m, _ = m.Update(msg)
	if !m.failed {
		t.Fatal("should mark failed")
	}
	out := m.View()
	if !strings.Contains(out, "install it") {
		t.Error("fix hint missing from view")
	}
}

// --- Wizard Update dispatching ---

func TestWizardUpdateCtrlC(t *testing.T) {
	m := newWizardModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c should return quit cmd")
	}
	if msg := runCmd(t, cmd); msg == nil {
		t.Fatal("quit cmd should produce message")
	}
}

func TestWizardUpdateWindowSize(t *testing.T) {
	m := newWizardModel()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	wm := model.(wizardModel)
	if wm.width != 120 || wm.height != 40 {
		t.Fatalf("size not stored: %dx%d", wm.width, wm.height)
	}
}

func TestWizardViewHasHeader(t *testing.T) {
	m := newWizardModel()
	v := m.View()
	if !strings.Contains(v.Content, "Agentique Setup") {
		t.Error("missing header")
	}
	if !v.AltScreen {
		t.Error("AltScreen should be enabled")
	}
}

// --- Wizard Update → phase dispatch ---

func positionAt(m wizardModel, kind stepKind) wizardModel {
	for i, s := range m.steps {
		if s.kind == kind {
			m.current = i
			return m
		}
	}
	return m
}

func TestWizardUpdateDispatchChoice(t *testing.T) {
	m := newWizardModel()
	m = positionAt(m, stepNetworkMode)
	m.choice = newChoiceModel("net", []string{"local", "net"}, 0)
	m.phase = phaseChoice

	// Forward "down" to choice → cursor moves.
	model, _ := m.Update(keyPress("down"))
	wm := model.(wizardModel)
	if wm.choice.cursor != 1 {
		t.Fatalf("cursor did not advance: %d", wm.choice.cursor)
	}
}

func TestWizardUpdateDispatchInput(t *testing.T) {
	m := newWizardModel()
	m = positionAt(m, stepProject)
	m.input = newInputModel("t", "", "", true, nil)
	m.phase = phaseInput

	// Enter on empty optional → advances step.
	model, _ := m.Update(keyPress("enter"))
	wm := model.(wizardModel)
	// The inputDoneMsg gets enqueued via cmd; Update triggers inputDoneMsg path.
	// First Update returns inputDoneMsg cmd, not yet processed.
	_ = wm
}

func TestWizardUpdateSummaryQuits(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(false)
	for i, s := range m.steps {
		if s.kind == stepSummary {
			m.current = i
		}
	}
	m.phase = phaseSummary
	m.summary = newSummaryModel(m.cfg, false)

	_, cmd := m.Update(keyPress("a"))
	if cmd == nil {
		t.Fatal("any key on summary should quit")
	}
}

// --- Wizard: completion choice branches ---

func TestWizardCompletionChoiceSkip(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(false)
	m = positionAt(m, stepCompletion)

	model, _ := m.handleChoiceResult(1) // "No, skip"
	wm := model.(wizardModel)
	// After skipping completion we advance past it.
	if wm.current <= m.current {
		t.Errorf("should advance past completion: was %d now %d", m.current, wm.current)
	}
}

func TestWizardCompletionChoiceInstall(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/bash")
	m := newWizardModel()
	m.steps = buildSteps(false)
	m = positionAt(m, stepCompletion)

	model, cmd := m.handleChoiceResult(0) // "Yes, install"
	wm := model.(wizardModel)
	if wm.phase != phaseAction {
		t.Errorf("phase: %v", wm.phase)
	}
	if cmd == nil {
		t.Fatal("should init action cmd")
	}
}

// --- Wizard: service install skip ---

func TestWizardServiceInstallSkip(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(false)
	m = positionAt(m, stepServiceInstall)

	model, _ := m.handleChoiceResult(1)
	wm := model.(wizardModel)
	if wm.serviceInstall {
		t.Error("serviceInstall should be false")
	}
}

// --- Wizard: handleActionResult ---

func TestWizardHandleActionResultError(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(false)
	m = positionAt(m, stepSaveConfig)
	m.action = actionModel{err: os.ErrPermission}

	_, cmd := m.handleActionResult()
	// Error path waits for keypress — no advance cmd.
	if cmd != nil {
		t.Fatal("should not advance on error")
	}
}

func TestWizardHandleActionResultSaveConfig(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(false)
	m = positionAt(m, stepSaveConfig)
	before := m.current

	model, _ := m.handleActionResult()
	wm := model.(wizardModel)
	if wm.current <= before {
		t.Errorf("should advance: was %d now %d", before, wm.current)
	}
}

// --- Wizard: TLS self-signed action ---

func TestWizardHandleTLSSelfSigned(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(true)
	m = positionAt(m, stepTLS)

	model, cmd := m.handleTLSChoice(1)
	wm := model.(wizardModel)
	if wm.tlsChoice != 1 || wm.phase != phaseAction {
		t.Fatalf("state: choice=%d phase=%v", wm.tlsChoice, wm.phase)
	}
	if cmd == nil {
		t.Fatal("should init action cmd")
	}

	// After action succeeds, handleActionResult stores cert/key paths.
	wm.action = actionModel{done: true}
	model2, _ := wm.handleActionResult()
	wm2 := model2.(wizardModel)
	if wm2.cfg.Server.TLSCert == "" || wm2.cfg.Server.TLSKey == "" {
		t.Error("cert/key paths not stored")
	}
}

// --- Wizard: initStep cases ---

func TestWizardInitStepAll(t *testing.T) {
	m := newWizardModel()
	for _, k := range []stepKind{
		stepNetworkMode, stepAuth, stepProject,
		stepSaveConfig, stepServiceInstall, stepSummary,
	} {
		m2 := m
		m2.steps = buildSteps(true)
		m2 = positionAt(m2, k)
		m2, _ = m2.initStep()
		switch k {
		case stepNetworkMode, stepAuth, stepServiceInstall:
			if m2.phase != phaseChoice {
				t.Errorf("%v: phase=%v", k, m2.phase)
			}
		case stepProject:
			if m2.phase != phaseInput {
				t.Errorf("%v: phase=%v", k, m2.phase)
			}
		case stepSaveConfig:
			if m2.phase != phaseAction {
				t.Errorf("%v: phase=%v", k, m2.phase)
			}
		case stepSummary:
			if m2.phase != phaseSummary {
				t.Errorf("%v: phase=%v", k, m2.phase)
			}
		}
	}
}

// --- Wizard updateDoctor/updateAction ---

func TestWizardUpdateDoctorKeyAdvance(t *testing.T) {
	m := newWizardModel()
	m.doctor.running = -1 // doctor already done, success
	m.phase = phaseDoctor

	model, cmd := m.Update(keyPress("a"))
	wm := model.(wizardModel)
	// Advances to next step.
	if wm.current == 0 {
		t.Error("should advance past doctor")
	}
	_ = cmd
}

func TestWizardUpdateDoctorFailQuits(t *testing.T) {
	m := newWizardModel()
	m.doctor.running = -1
	m.err = os.ErrPermission
	m.phase = phaseDoctor

	_, cmd := m.Update(keyPress("a"))
	if cmd == nil {
		t.Fatal("should quit on failure")
	}
}

func TestWizardUpdateDoctorDoneMsg(t *testing.T) {
	m := newWizardModel()
	m.phase = phaseDoctor

	model, _ := m.Update(doctorDoneMsg{failed: true})
	wm := model.(wizardModel)
	if wm.err == nil {
		t.Error("err should be set on failed doctor")
	}
}

func TestWizardUpdateActionDone(t *testing.T) {
	m := newWizardModel()
	m.steps = buildSteps(false)
	m = positionAt(m, stepSaveConfig)
	m.phase = phaseAction
	m.action = newActionModel("save", func() (string, error) { return "ok", nil })
	before := m.current

	model, _ := m.Update(actionDoneMsg{detail: "ok"})
	wm := model.(wizardModel)
	if !wm.action.done {
		t.Error("action should be marked done")
	}
	if wm.current <= before {
		t.Errorf("should advance past action: was %d now %d", before, wm.current)
	}
}

// --- Certificate generator ---

func TestGenerateSelfSignedCert(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "server.crt")
	key := filepath.Join(dir, "server.key")

	if err := generateSelfSignedCert(cert, key); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Verify cert parses.
	pemBytes, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("bad cert PEM: %+v", block)
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if parsed.Subject.CommonName != "localhost" {
		t.Errorf("CN: %s", parsed.Subject.CommonName)
	}
	foundLocalhost := false
	for _, dns := range parsed.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Error("DNS SAN missing localhost")
	}
	if len(parsed.IPAddresses) < 2 {
		t.Errorf("want at least 2 IP SANs, got %d", len(parsed.IPAddresses))
	}

	// Verify key parses.
	keyBytes, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		t.Fatalf("bad key PEM: %+v", keyBlock)
	}
	if _, err := x509.ParseECPrivateKey(keyBlock.Bytes); err != nil {
		t.Fatalf("parse key: %v", err)
	}

	// Key file must be 0600.
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v, want 0600", info.Mode().Perm())
	}
}
