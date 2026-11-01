package prompt

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// P1: WithoutFlagSet handlers must actually run and receive the argument string
// (the command line with the leading handler name stripped).
func TestWithoutFlagSetHandlerReceivesArgs(t *testing.T) {
	var got string
	called := false
	h, hErr := testCommand("echo", "", func(s string) {
		called = true
		got = s
	}, true)
	if err := hErr; err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if err := h.Run("echo hello world"); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was never called")
	}
	if got != "hello world" {
		t.Fatalf("handler got %q, want the args %q (handler name stripped)", got, "hello world")
	}
	// no args -> empty string, still runs
	got, called = "x", false
	if err := h.Run("echo"); err != nil {
		t.Fatalf("run (no args) returned error: %v", err)
	}
	if !called || got != "" {
		t.Fatalf("no-arg run: called=%v got=%q, want called=true got=\"\"", called, got)
	}
}

// P1 regression: a normal flag-set handler still parses and runs correctly.
func TestFlagSetHandlerRuns(t *testing.T) {
	var sum int
	h, hErr := testCommand("calc", "", func(a, b int) { sum = a + b }, false, Flag("a", ""), Flag("b", ""))
	if err := hErr; err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if err := h.Run("calc -a=2 -b=3"); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if sum != 5 {
		t.Fatalf("sum = %d, want 5", sum)
	}
}

// A bool flag doesn't take a separate value, so the next flag is still
// suggested after it; a string flag does, so nothing is suggested while its
// value is being typed.
func TestBoolFlagNeedsNoValueForCompletion(t *testing.T) {
	c := mustCommand(t, "c", func(people string, flag bool) {}, Flag("people", "who"), Flag("flag", ""))
	if got := c.completeFlags("c -flag "); len(got) != 1 || got[0].Text != "-people" {
		t.Fatalf("after a bool flag got %+v, want [-people]", got)
	}
	if got := c.completeFlags("c -people "); len(got) != 0 {
		t.Fatalf("while typing a string flag's value got %+v, want none", got)
	}
	if got := c.completeFlags("c -people=x "); len(got) != 1 || got[0].Text != "-flag" {
		t.Fatalf("after -people=x got %+v, want [-flag]", got)
	}
}

// Typing flag-like text for a RawCommand must not crash the TUI.
func TestTypingBoolFlagDoesNotPanicView(t *testing.T) {
	m := New(WithoutHistory())
	m.RawCommand("hello", "", func(s string) {})
	var mm tea.Model = m
	for _, r := range "hello -people=x" {
		mm, _ = mm.Update(textKey(string(r)))
	}
	_ = mm.View() // must not panic
}

// A function returning a non-nil error as its last result makes the command
// fail with that error, and OnResult isn't called; on success OnResult gets
// the other results.
func TestHandlerReturnedErrorIsReported(t *testing.T) {
	var seen int
	h, hErr := testCommand("删除", "", func(名字 string) (int, error) {
		if 名字 == "" {
			return 0, fmt.Errorf("名字不能为空")
		}
		return 1, nil
	}, false, Flag("名字", ""), OnResult(func(r []any) { seen = len(r) }))
	if err := hErr; err != nil {
		t.Fatal(err)
	}
	if err := h.Run("删除"); err == nil || err.Error() != "名字不能为空" {
		t.Fatalf("Run err = %v, want 名字不能为空", err)
	}
	if seen != 0 {
		t.Fatalf("OnResult called on failure with %d results", seen)
	}
	if err := h.Run("删除 -名字 张三"); err != nil {
		t.Fatalf("Run err = %v, want nil", err)
	}
	if seen != 1 {
		t.Fatalf("OnResult saw %d results, want 1 (the error is not a result)", seen)
	}
}
