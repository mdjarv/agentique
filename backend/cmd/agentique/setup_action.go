package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// actionDoneMsg is sent when the async operation completes.
type actionDoneMsg struct {
	err    error
	detail string
}

type actionModel struct {
	title   string
	spinner spinner.Model
	done    bool
	err     error
	detail  string
	run     func() (string, error) // returns detail string and error
}

func newActionModel(title string, run func() (string, error)) actionModel {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	s.Style = styleSelected
	return actionModel{
		title:   title,
		spinner: s,
		run:     run,
	}
}

func (m actionModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.execute())
}

func (m actionModel) execute() tea.Cmd {
	run := m.run
	return func() tea.Msg {
		detail, err := run()
		return actionDoneMsg{err: err, detail: detail}
	}
}

func (m actionModel) Update(msg tea.Msg) (actionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case actionDoneMsg:
		m.done = true
		m.err = msg.err
		m.detail = msg.detail
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m actionModel) View() string {
	var b strings.Builder
	if !m.done {
		fmt.Fprintf(&b, "  %s  %s", m.spinner.View(), m.title)
	} else if m.err != nil {
		fmt.Fprintf(&b, "  %s  %s\n", styleFail.Render("✗"), m.title)
		fmt.Fprintf(&b, "     %s", styleError.Render(m.err.Error()))
	} else {
		fmt.Fprintf(&b, "  %s  %s", stylePass.Render("✓"), m.title)
		if m.detail != "" {
			fmt.Fprintf(&b, "\n     %s", styleDim.Render(m.detail))
		}
	}
	return b.String()
}
