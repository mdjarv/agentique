package main

import "charm.land/lipgloss/v2"

var (
	colorAccent  = lipgloss.Color("75")
	colorPass    = lipgloss.Color("76")
	colorFail    = lipgloss.Color("196")
	colorWarn    = lipgloss.Color("214")
	colorDim     = lipgloss.Color("240")
	colorText    = lipgloss.Color("252")
	colorSurface = lipgloss.Color("236")

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			MarginBottom(1)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	styleDim = lipgloss.NewStyle().
			Foreground(colorDim)

	stylePass = lipgloss.NewStyle().
			Foreground(colorPass)

	styleFail = lipgloss.NewStyle().
			Foreground(colorFail)

	styleWarn = lipgloss.NewStyle().
			Foreground(colorWarn)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleKey = lipgloss.NewStyle().
			Foreground(colorDim).
			Width(16)

	styleVal = lipgloss.NewStyle().
			Foreground(colorText)

	styleSummaryBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	styleError = lipgloss.NewStyle().
			Foreground(colorFail).
			Bold(true)
)
