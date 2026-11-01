package prompt

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// textKey builds the key-press message bubbletea v2 emits for typed (or
// IME-committed) text.
func textKey(s string) tea.KeyPressMsg {
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

// key builds a key-press message for a special key such as tea.KeyEnter.
func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// runAll executes cmd and feeds every resulting message back into m until no
// command is left, like the bubbletea loop would (e.g. enter -> RunCmdMsg ->
// handler finished). Batches are expanded; it gives up after a bounded number
// of steps so a ticking command can't hang a test.
func runAll(m tea.Model, cmd tea.Cmd) tea.Model {
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 100; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		queue = append(queue, next)
	}
	return m
}

// testCommand builds a command like Prompt.Command/RawCommand would,
// returning the validation error instead of panicking.
func testCommand(name, help string, fn any, raw bool, opts ...CommandOption) (*command, error) {
	return newCommand(name, help, fn, raw, opts)
}

func mustCommand(t *testing.T, name string, fn any, opts ...CommandOption) *command {
	t.Helper()
	c, err := newCommand(name, "", fn, false, opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustRaw(t *testing.T, name string, fn any, opts ...CommandOption) *command {
	t.Helper()
	c, err := newCommand(name, "", fn, true, opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Run runs line with a background context and no result handler.
func (c *command) Run(line string) error { return c.run(context.Background(), line, nil) }

// startedPrompt is New plus the side effects of Run (history load/save, log
// redirect), undone when the test ends.
func startedPrompt(t *testing.T, opts ...Option) *Prompt {
	t.Helper()
	p := New(opts...)
	if err := p.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.stop)
	return p
}
