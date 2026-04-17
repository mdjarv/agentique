package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// choiceDoneMsg is sent when the user confirms a selection.
type choiceDoneMsg struct{ selected int }

type choiceModel struct {
	title    string
	options  []string
	hints    []string // optional per-option hint text
	cursor   int
	selected int // -1 until confirmed
}

func newChoiceModel(title string, options []string, defaultIdx int) choiceModel {
	return choiceModel{
		title:    title,
		options:  options,
		cursor:   defaultIdx,
		selected: -1,
	}
}

func (m choiceModel) Init() tea.Cmd { return nil }

func (m choiceModel) Update(msg tea.Msg) (choiceModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.cursor
			return m, func() tea.Msg { return choiceDoneMsg{m.cursor} }
		}
	}
	return m, nil
}

func (m choiceModel) View() string {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(styleSubtitle.Render(m.title))
		b.WriteString("\n\n")
	}
	for i, opt := range m.options {
		cursor := "  "
		style := styleDim
		if i == m.cursor {
			cursor = styleSelected.Render("> ")
			style = styleSelected
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(opt)))
		if i < len(m.hints) && m.hints[i] != "" && i == m.cursor {
			b.WriteString(fmt.Sprintf("    %s\n", styleDim.Render(m.hints[i])))
		}
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render("↑/↓ navigate • enter select"))
	return b.String()
}
