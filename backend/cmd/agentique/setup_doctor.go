package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdjarv/agentique/backend/internal/doctor"
)

// doctorDoneMsg is sent when all checks complete.
type doctorDoneMsg struct{ failed bool }

// checkResultMsg carries a single completed check result.
type checkResultMsg struct {
	index int
	check doctor.Check
}

type doctorModel struct {
	checks  []doctor.CheckFunc
	results []doctor.Check
	running int // index of currently running check, -1 when done
	spinner spinner.Model
	failed  bool
}

func newDoctorModel() doctorModel {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	s.Style = styleSelected
	checks := doctor.AllChecks()
	return doctorModel{
		checks:  checks,
		results: make([]doctor.Check, len(checks)),
		running: 0,
		spinner: s,
	}
}

func (m doctorModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runCheck(0))
}

func (m doctorModel) runCheck(idx int) tea.Cmd {
	if idx >= len(m.checks) {
		return nil
	}
	fn := m.checks[idx]
	return func() tea.Msg {
		return checkResultMsg{index: idx, check: fn.Run()}
	}
}

func (m doctorModel) Update(msg tea.Msg) (doctorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case checkResultMsg:
		m.results[msg.index] = msg.check
		if msg.check.Required && msg.check.Status == doctor.Fail {
			m.failed = true
		}
		next := msg.index + 1
		if next >= len(m.checks) {
			m.running = -1
			return m, func() tea.Msg { return doctorDoneMsg{m.failed} }
		}
		m.running = next
		return m, m.runCheck(next)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m doctorModel) View() string {
	var b strings.Builder
	b.WriteString(styleSubtitle.Render("Checking dependencies"))
	b.WriteString("\n\n")

	for i, cf := range m.checks {
		if i < m.running || m.running == -1 {
			// Completed check.
			r := m.results[i]
			icon := stylePass.Render("✓")
			if r.Status == doctor.Warn {
				icon = styleWarn.Render("!")
			} else if r.Status == doctor.Fail {
				icon = styleFail.Render("✗")
			}
			req := ""
			if !r.Required {
				req = styleDim.Render(" (optional)")
			}
			fmt.Fprintf(&b, "  %s  %-14s %s%s\n", icon, r.Name, r.Message, req)
			if r.Fix != "" && r.Status != doctor.OK {
				fmt.Fprintf(&b, "     %-14s %s\n", "", styleDim.Render(r.Fix))
			}
		} else if i == m.running {
			// Currently running.
			fmt.Fprintf(&b, "  %s  %s\n", m.spinner.View(), cf.Name)
		} else {
			// Pending.
			fmt.Fprintf(&b, "  %s  %s\n", styleDim.Render("·"), styleDim.Render(cf.Name))
		}
	}

	if m.running == -1 {
		b.WriteString("\n")
		if m.failed {
			b.WriteString(styleError.Render("Fix the issues above before continuing."))
			b.WriteString("\n")
			b.WriteString(styleDim.Render("press any key to exit"))
		} else {
			b.WriteString(styleDim.Render("press any key to continue"))
		}
	}

	return b.String()
}
