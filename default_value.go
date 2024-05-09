package prompt

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	defaultBaseStyle = lipgloss.NewStyle().
				Bold(true).
				Faint(true).
				Blink(true).
				Reverse(true).
				Background(lipgloss.Color("#00B3FF73")).
				Foreground(lipgloss.Color("#00B3FFFF"))

	defaultForceStyle = defaultBaseStyle.Copy().
				Underline(true).
				Italic(true).
				Background(lipgloss.Color("#9EA9AEFF"))

	defaultRunCmdDeply int64 = 20

	defaultPrintCmd      bool   = true
	defaultSuggestNum    int    = 3
	defaultSuggestPrefix string = "-"
	defaultPrefix        string = ">>>"

	defaultHistoryFile string = ".prompt.history"
)

var (
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render
)

var (
	DefaultExitFunc = func() {}
)
