package prompt

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// P1: WithoutFlagSet handlers must actually run and receive the argument string
// (the command line with the leading handler name stripped).
func TestWithoutFlagSetHandlerReceivesArgs(t *testing.T) {
	var got string
	called := false
	h := NewHandlerInfo("echo", func(s string) {
		called = true
		got = s
	}, WithoutFlagSet())
	if err := h.CheckAndInitHandler(); err != nil {
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
	h := NewHandlerInfo("calc", func(a, b int) { sum = a + b },
		WithSuggests([]Suggest{{Text: "a"}, {Text: "b"}}))
	if err := h.CheckAndInitHandler(); err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if err := h.Run("calc -a=2 -b=3"); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if sum != 5 {
		t.Fatalf("sum = %d, want 5", sum)
	}
}

// P1: IsBoolSuggest must not panic when a Suggest.Default is nil.
func TestIsBoolSuggestNilDefaultNoPanic(t *testing.T) {
	suggests := []Suggest{{Text: "people", Description: "who"}} // Default is nil
	if IsBoolSuggest(suggests, "-people=x", "-") {
		t.Fatal("nil Default should not be treated as bool")
	}
	// positive case still works
	boolSuggests := []Suggest{{Text: "flag", Default: true}}
	if !IsBoolSuggest(boolSuggests, "-flag=true", "-") {
		t.Fatal("bool Default should be detected")
	}
}

// P1: typing an unregistered-value flag on a WithoutFlagSet handler must not crash the TUI.
func TestTypingBoolFlagDoesNotPanicView(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory())
	m.RegisterHandler(func(s string) {}, "hello", WithoutFlagSet(),
		WithSuggests([]Suggest{{Text: "people", Description: "who"}}))
	var mm tea.Model = m
	for _, r := range "hello -people=x" {
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_ = mm.View() // must not panic
}
