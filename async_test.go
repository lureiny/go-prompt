package prompt

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// startCmd types line, presses enter and dispatches the RunCmdMsg, returning
// the background command that runs the handler (not yet executed).
func startCmd(t *testing.T, m *Prompt, line string) tea.Cmd {
	t.Helper()
	m.Update(textKey(line))
	_, enter := m.Update(key(tea.KeyEnter))
	_, bg := m.Update(enter()) // RunCmdMsg -> background cmd
	if bg == nil || !m.runCmdMark {
		t.Fatal("expected a running background command")
	}
	return bg
}

func ctrl(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Mod: tea.ModCtrl} }

func TestCtrlCInterruptsContextHandler(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false), WithEcho(false))
	started := make(chan struct{})
	var n int
	m.Command("等待", "", func(ctx context.Context, times int) error {
		n = times
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, Flag("次数", ""))

	bg := startCmd(t, m, "等待 -次数 3")
	done := make(chan tea.Msg)
	go func() { done <- bg() }()
	<-started

	// the event loop is not blocked: other keys are ignored, ctrl+c cancels
	m.Update(textKey("x"))
	if m.textInput.Value() != "" {
		t.Fatalf("typing while running changed the input to %q", m.textInput.Value())
	}
	m.Update(ctrl('c'))
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctrl+c did not cancel the handler's context")
	}
	_, follow := m.Update(msg)
	if m.runCmdMark {
		t.Fatal("still marked running after the command finished")
	}
	if s := printedText(follow); !strings.Contains(s, "interrupted") {
		t.Fatalf("printed %q, want an interrupted message", s)
	}
	if n != 3 {
		t.Fatalf("flag after the context param = %d, want 3", n)
	}
}

func TestCtrlCOnPlainHandlerExplains(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false), WithEcho(false))
	release := make(chan struct{})
	m.RawCommand("慢", "", func(s string) { <-release })

	bg := startCmd(t, m, "慢")
	done := make(chan tea.Msg)
	go func() { done <- bg() }()

	_, hint := m.Update(ctrl('c'))
	if s := printedText(hint); !strings.Contains(s, "can't be interrupted") {
		t.Fatalf("hint = %q", s)
	}
	close(release)
	m.Update(<-done)
	if m.runCmdMark {
		t.Fatal("still running after the handler returned")
	}
}

func TestCtrlDQuitsWhileRunning(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false), WithEcho(false))
	m.RawCommand("w", "", func(ctx context.Context, s string) { <-ctx.Done() })
	bg := startCmd(t, m, "w")
	done := make(chan tea.Msg)
	go func() { done <- bg() }()
	_, quit := m.Update(ctrl('d'))
	if _, ok := quit().(tea.QuitMsg); !ok || !m.exit {
		t.Fatal("ctrl+d while running must quit")
	}
	select {
	case <-done: // the context was cancelled too
	case <-time.After(2 * time.Second):
		t.Fatal("ctrl+d did not cancel the running handler")
	}
}

func TestContextHandlerSignatures(t *testing.T) {
	var got string
	h := mustRaw(t, "r", func(ctx context.Context, s string) { got = s })
	if err := h.Run("r 原样 参数"); err != nil || got != "原样 参数" {
		t.Fatalf("raw ctx handler: err=%v got=%q", err, got)
	}
	f := mustCommand(t, "f", func(ctx context.Context, a int) {}, Flag("a", ""))
	if u := f.usage(); !strings.Contains(u, "-a int") || strings.Contains(u, "Context") {
		t.Fatalf("usage = %q", u)
	}
	// suggest count excludes the context param
	_, badErr := testCommand("b", "", func(ctx context.Context, a int) {}, false, Flag("ctx", ""), Flag("a", ""))
	if err := badErr; err == nil {
		t.Fatal("a Suggest for the context param must be rejected")
	}
}
