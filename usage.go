package prompt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// isUsage reports whether err is a help request and returns the usage text.
func isUsage(err error) (string, bool) {
	var u *usageError
	if errors.As(err, &u) {
		return u.usage, true
	}
	return "", false
}

// usage describes how to call the command; it is what "<name> -h" prints.
func (c *command) usage() string {
	var b strings.Builder
	switch {
	case c.raw:
		fmt.Fprintf(&b, "usage: %s <args>", c.name)
	case len(c.flags) == 0:
		fmt.Fprintf(&b, "usage: %s", c.name)
	default:
		fmt.Fprintf(&b, "usage: %s [flags]", c.name)
	}
	if c.help != "" {
		b.WriteString("\n  " + c.help)
	}
	width := 0
	for _, fd := range c.flags {
		width = max(width, lipgloss.Width(fd.usageName()))
	}
	for _, fd := range c.flags {
		name := fd.usageName()
		b.WriteString("\n  " + name)
		desc := fd.help
		if d, ok := fd.defaultText(); ok {
			if desc != "" {
				desc += " "
			}
			desc += "(default: " + d + ")"
		}
		if desc != "" {
			b.WriteString(strings.Repeat(" ", width-lipgloss.Width(name)+2) + desc)
		}
	}
	return b.String()
}

func (fd *flagDef) usageName() string { return "-" + fd.name + " " + fd.typeName() }

// builtinCommands are available unless WithoutBuiltinCommands is used; a
// user command with the same name takes precedence.
func (p *Prompt) builtinCommands() map[string]*command {
	help, err := newCommand("help", "list commands, or show the usage of one", func(arg string) {
		fmt.Println(p.helpText(arg))
	}, true, nil)
	if err != nil {
		panic(err)
	}
	exit, err := newCommand("exit", "quit", func(context.Context, string) {}, true, []CommandOption{Exit()})
	if err != nil {
		panic(err)
	}
	return map[string]*command{help.name: help, exit.name: exit}
}

// helpText renders the built-in help command: "help" lists every command,
// "help <name>" shows that command's usage.
func (p *Prompt) helpText(arg string) string {
	if arg = strings.TrimSpace(arg); arg != "" {
		c := p.lookup(arg)
		if c == nil {
			return fmt.Sprintf("unknown command %q", arg)
		}
		return c.usage()
	}
	names := p.commandNames()
	sort.Strings(names)
	width := 0
	for _, n := range names {
		width = max(width, lipgloss.Width(n))
	}
	var b strings.Builder
	b.WriteString(`commands ("help <command>" or "<command> -h" for details):`)
	for _, n := range names {
		b.WriteString("\n  " + n)
		if h := p.lookup(n).help; h != "" {
			b.WriteString(strings.Repeat(" ", width-lipgloss.Width(n)+2) + h)
		}
	}
	return b.String()
}
