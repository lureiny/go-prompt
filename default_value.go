package prompt

import "github.com/charmbracelet/lipgloss"

var (
	defaultForceStyle = lipgloss.NewStyle().
				Bold(true).
				Italic(true).
				Faint(true).
				Blink(true).
				ColorWhitespace(true).
				Underline(true).
				Reverse(true)

	defaultRunCmdDeply int64 = 20

	defaultPrintCmd      bool   = true
	defaultSuggestNum    int    = 3
	defaultSuggestPrefix string = "-"
	defaultPrefix        string = ">>>"
)
