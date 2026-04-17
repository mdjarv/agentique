package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/service"
)

// wizardPhase tracks what sub-model is currently active.
type wizardPhase int

const (
	phaseDoctor wizardPhase = iota
	phaseChoice
	phaseInput
	phaseAction
	phaseSummary
	phaseDone
)

type wizardModel struct {
	steps   []step
	current int
	cfg     *config.Config
	width   int
	height  int

	networkMode    bool
	serviceInstall bool

	// Active sub-models (only one active at a time).
	phase   wizardPhase
	doctor  doctorModel
	choice  choiceModel
	input   inputModel
	action  actionModel
	summary summaryModel

	// TLS sub-step state (step 3 has branching paths).
	tlsChoice  int // 0=existing, 1=self-signed, 2=reverse-proxy
	tlsSubStep int // 0=main choice, 1+=follow-up inputs/actions

	// Accumulated action results for multi-action steps.
	actions []actionModel

	err error
}

func newWizardModel() wizardModel {
	m := wizardModel{
		steps: buildSteps(false),
		cfg:   config.Default(),
	}
	m.doctor = newDoctorModel()
	m.phase = phaseDoctor
	return m
}

func (m wizardModel) Init() tea.Cmd {
	return m.doctor.Init()
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	switch m.phase {
	case phaseDoctor:
		return m.updateDoctor(msg)
	case phaseChoice:
		return m.updateChoice(msg)
	case phaseInput:
		return m.updateInput(msg)
	case phaseAction:
		return m.updateAction(msg)
	case phaseSummary:
		return m.updateSummary(msg)
	}
	return m, nil
}

func (m wizardModel) View() tea.View {
	var b strings.Builder

	// Header with step progress.
	stepNum := m.current + 1
	total := len(m.steps)
	header := fmt.Sprintf("Agentique Setup  %s",
		styleDim.Render(fmt.Sprintf("[%d/%d]", stepNum, total)))
	b.WriteString(styleTitle.Render(header))
	b.WriteString("\n")

	// Render completed actions if present.
	for _, a := range m.actions {
		b.WriteString(a.View())
		b.WriteString("\n")
	}

	switch m.phase {
	case phaseDoctor:
		b.WriteString(m.doctor.View())
	case phaseChoice:
		b.WriteString(m.choice.View())
	case phaseInput:
		b.WriteString(m.input.View())
	case phaseAction:
		b.WriteString(m.action.View())
	case phaseSummary:
		b.WriteString(m.summary.View())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// --- Step initialization ---

func (m wizardModel) initStep() (wizardModel, tea.Cmd) {
	if m.current >= len(m.steps) {
		return m, tea.Quit
	}
	s := m.steps[m.current]
	switch s.kind {
	case stepDoctor:
		m.doctor = newDoctorModel()
		m.phase = phaseDoctor
		return m, m.doctor.Init()

	case stepNetworkMode:
		m.choice = newChoiceModel(s.title, []string{
			"Localhost only (recommended for single user)",
			"Over the network (LAN, Tailscale, etc.)",
		}, 0)
		m.choice.hints = []string{
			"Auth disabled, binds to 127.0.0.1",
			"Binds to 0.0.0.0, enables passkey auth",
		}
		m.phase = phaseChoice
		return m, nil

	case stepTLS:
		m.tlsSubStep = 0
		m.actions = nil
		m.choice = newChoiceModel(s.title, []string{
			"I have TLS certificates",
			"Generate self-signed certificates",
			"I'll use a reverse proxy (nginx, caddy, etc.)",
		}, 2)
		m.phase = phaseChoice
		return m, nil

	case stepAuth:
		m.choice = newChoiceModel(s.title, []string{
			"Yes, enable passkey authentication (recommended)",
			"No, disable authentication (trusted network only)",
		}, 0)
		m.phase = phaseChoice
		return m, nil

	case stepProject:
		m.input = newInputModel(
			s.title,
			"~/projects/my-app",
			"Leave empty to skip. Path will be validated.",
			true,
			validateProjectPath,
		)
		m.phase = phaseInput
		return m, m.input.Init()

	case stepSaveConfig:
		m.actions = nil
		configPath := config.Path()
		m.action = newActionModel(
			fmt.Sprintf("Saving config to %s", configPath),
			func() (string, error) {
				if err := config.Save(m.cfg, configPath); err != nil {
					return "", fmt.Errorf("save config: %w", err)
				}
				return configPath, nil
			},
		)
		m.phase = phaseAction
		return m, m.action.Init()

	case stepCompletion:
		shell := detectShell()
		if shell == "" {
			// Unknown shell, skip.
			m.current++
			return m.initStep()
		}
		m.choice = newChoiceModel(s.title, []string{
			fmt.Sprintf("Yes, install for %s", shell),
			"No, skip",
		}, 0)
		m.phase = phaseChoice
		return m, nil

	case stepServiceInstall:
		m.choice = newChoiceModel(s.title, []string{
			"Yes, install as system service (auto-starts on login)",
			"No, I'll run it manually",
		}, 0)
		m.phase = phaseChoice
		return m, nil

	case stepSummary:
		m.summary = newSummaryModel(m.cfg, m.serviceInstall)
		m.phase = phaseSummary
		return m, nil
	}
	return m, nil
}

// advance moves to the next step and returns the model + init command.
func (m wizardModel) advance() (tea.Model, tea.Cmd) {
	m.current++
	m.actions = nil
	m, cmd := m.initStep()
	return m, cmd
}

// --- Phase-specific update handlers ---

func (m wizardModel) updateDoctor(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case doctorDoneMsg:
		if msg.failed {
			m.err = fmt.Errorf("required checks failed")
		}
		// Wait for keypress to continue or exit.
		return m, nil

	case tea.KeyMsg:
		if m.doctor.running == -1 {
			if m.err != nil {
				return m, tea.Quit
			}
			return m.advance()
		}

	default:
		var cmd tea.Cmd
		m.doctor, cmd = m.doctor.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.doctor, cmd = m.doctor.Update(msg)
	return m, cmd
}

func (m wizardModel) updateChoice(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case choiceDoneMsg:
		return m.handleChoiceResult(msg.selected)
	default:
		var cmd tea.Cmd
		m.choice, cmd = m.choice.Update(msg)
		return m, cmd
	}
}

func (m wizardModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case inputDoneMsg:
		return m.handleInputResult(msg.value)
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m wizardModel) updateAction(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case actionDoneMsg:
		m.action.done = true
		m.action.err = msg.err
		m.action.detail = msg.detail
		return m.handleActionResult()
	default:
		var cmd tea.Cmd
		m.action, cmd = m.action.Update(msg)
		return m, cmd
	}
}

func (m wizardModel) updateSummary(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return m, tea.Quit
	}
	return m, nil
}

// --- Result handlers ---

func (m wizardModel) handleChoiceResult(selected int) (tea.Model, tea.Cmd) {
	s := m.steps[m.current]

	switch s.kind {
	case stepNetworkMode:
		m.networkMode = selected == 1
		if m.networkMode {
			m.cfg.Server.Addr = "0.0.0.0:9201"
		} else {
			m.cfg.Server.Addr = "localhost:9201"
			m.cfg.Server.DisableAuth = true
		}
		// Rebuild steps based on network mode choice.
		m.steps = buildSteps(m.networkMode)
		// Find our new position (stepNetworkMode is always index 1).
		m.current = 1
		return m.advance()

	case stepTLS:
		return m.handleTLSChoice(selected)

	case stepAuth:
		m.cfg.Server.DisableAuth = selected == 1
		return m.advance()

	case stepCompletion:
		if selected == 1 {
			return m.advance()
		}
		shell := detectShell()
		m.action = newActionModel(
			fmt.Sprintf("Installing %s completions", shell),
			func() (string, error) {
				return installCompletion(shell)
			},
		)
		m.phase = phaseAction
		return m, m.action.Init()

	case stepServiceInstall:
		if selected == 1 {
			// Skip service install.
			m.serviceInstall = false
			return m.advance()
		}
		// Run service install action.
		m.serviceInstall = true
		m.action = newActionModel("Installing system service", func() (string, error) {
			if err := service.Install(); err != nil {
				return "", err
			}
			st, _ := service.GetStatus()
			if st.Running {
				return fmt.Sprintf("PID: %d", st.PID), nil
			}
			return "installed", nil
		})
		m.phase = phaseAction
		return m, m.action.Init()
	}

	return m.advance()
}

func (m wizardModel) handleTLSChoice(selected int) (tea.Model, tea.Cmd) {
	m.tlsChoice = selected
	m.tlsSubStep = 1

	switch selected {
	case 0: // Existing certs — ask for cert path.
		m.input = newInputModel(
			"Certificate file path",
			"/path/to/server.crt",
			"",
			false,
			validateFilePath,
		)
		m.phase = phaseInput
		return m, m.input.Init()

	case 1: // Self-signed — generate certs.
		certDir := filepath.Join(paths.DataDir(), "certs")
		certPath := filepath.Join(certDir, "server.crt")
		keyPath := filepath.Join(certDir, "server.key")
		m.action = newActionModel("Generating self-signed certificate", func() (string, error) {
			if err := generateSelfSignedCert(certPath, keyPath); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s (valid 365 days)", certDir), nil
		})
		m.phase = phaseAction
		return m, m.action.Init()

	case 2: // Reverse proxy — ask for origin URL.
		m.input = newInputModel(
			"What URL will users access?",
			"https://agentique.example.com",
			"Used for WebAuthn origin verification",
			true,
			nil,
		)
		m.phase = phaseInput
		return m, m.input.Init()
	}

	return m, nil
}

func (m wizardModel) handleInputResult(value string) (tea.Model, tea.Cmd) {
	s := m.steps[m.current]

	switch s.kind {
	case stepTLS:
		return m.handleTLSInput(value)

	case stepProject:
		if value != "" {
			// Expand ~ if present.
			if strings.HasPrefix(value, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					value = filepath.Join(home, value[2:])
				}
			}
			absPath, err := filepath.Abs(value)
			if err == nil {
				value = absPath
			}
			m.cfg.Setup.InitialProject = value
		}
		return m.advance()
	}

	return m.advance()
}

func (m wizardModel) handleTLSInput(value string) (tea.Model, tea.Cmd) {
	switch m.tlsChoice {
	case 0: // Existing certs.
		if m.tlsSubStep == 1 {
			// Got cert path, now ask for key path.
			m.cfg.Server.TLSCert = value
			m.tlsSubStep = 2
			m.input = newInputModel(
				"Key file path",
				"/path/to/server.key",
				"",
				false,
				validateFilePath,
			)
			m.phase = phaseInput
			return m, m.input.Init()
		}
		// Got key path, done with TLS.
		m.cfg.Server.TLSKey = value
		return m.advance()

	case 2: // Reverse proxy.
		if value != "" {
			m.cfg.Server.RPOrigin = value
		}
		return m.advance()
	}

	return m.advance()
}

func (m wizardModel) handleActionResult() (tea.Model, tea.Cmd) {
	s := m.steps[m.current]

	if m.action.err != nil {
		// Show error, wait for keypress to continue.
		return m, nil
	}

	switch s.kind {
	case stepTLS:
		if m.tlsChoice == 1 {
			// Self-signed cert generated — store paths.
			certDir := filepath.Join(paths.DataDir(), "certs")
			m.cfg.Server.TLSCert = filepath.Join(certDir, "server.crt")
			m.cfg.Server.TLSKey = filepath.Join(certDir, "server.key")
		}
		return m.advance()

	case stepSaveConfig:
		return m.advance()

	case stepCompletion:
		return m.advance()

	case stepServiceInstall:
		return m.advance()
	}

	return m.advance()
}

// --- Validators ---

func validateProjectPath(value string) error {
	path := value
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("directory not found: %s", absPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", absPath)
	}
	return nil
}

func validateFilePath(value string) error {
	if _, err := os.Stat(value); err != nil {
		return fmt.Errorf("file not found: %s", value)
	}
	return nil
}
