package main

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// inputDoneMsg is sent when the user confirms text input.
type inputDoneMsg struct{ value string }

type inputModel struct {
	title    string
	hint     string
	input    textinput.Model
	validate func(string) error
	optional bool
	done     bool
	err      error
}

func newInputModel(title, placeholder, hint string, optional bool, validate func(string) error) inputModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = "  "
	ti.Focus()
	return inputModel{
		title:    title,
		hint:     hint,
		input:    ti,
		validate: validate,
		optional: optional,
	}
}

func (m inputModel) Init() tea.Cmd {
	return m.input.Focus()
}

func (m inputModel) Update(msg tea.Msg) (inputModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			if val == "" && m.optional {
				m.done = true
				return m, func() tea.Msg { return inputDoneMsg{""} }
			}
			if m.validate != nil {
				if err := m.validate(val); err != nil {
					m.err = err
					return m, nil
				}
			}
			m.done = true
			return m, func() tea.Msg { return inputDoneMsg{val} }
		}
		// Clear error on any other key press.
		m.err = nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(styleSubtitle.Render(m.title))
		b.WriteString("\n\n")
	}
	b.WriteString(m.input.View())
	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(styleError.Render("  " + m.err.Error()))
		b.WriteString("\n")
	}
	if m.hint != "" {
		b.WriteString(styleDim.Render("  " + m.hint))
		b.WriteString("\n")
	}
	hint := "enter confirm"
	if m.optional {
		hint += " • empty to skip"
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render(hint))
	return b.String()
}
