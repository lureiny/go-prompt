package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SortSuggest must preserve every element even when two share the same Text.
func TestSortSuggestKeepsDuplicateText(t *testing.T) {
	in := []Suggest{
		{Text: "b", Description: "first"},
		{Text: "a", Description: "second"},
		{Text: "a", Description: "third"},
	}
	out := SortSuggest(in)
	if len(out) != 3 {
		t.Fatalf("got %d suggests, want 3 (none dropped)", len(out))
	}
	if out[0].Text != "a" || out[1].Text != "a" || out[2].Text != "b" {
		t.Fatalf("unexpected order: %+v", out)
	}
	// both "a" descriptions must survive
	descs := out[0].Description + "|" + out[1].Description
	if !strings.Contains(descs, "second") || !strings.Contains(descs, "third") {
		t.Fatalf("a duplicate-Text entry was lost: %q", descs)
	}
}

// History entries buffered before exit must be flushed by closeHistory.
func TestHistoryFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	hist := filepath.Join(dir, "cmd.history")
	m := NewPromptModel(WithHistoryFile(hist))

	for i := 0; i < 250 && !m.readyToSaveHistory.Load(); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if !m.readyToSaveHistory.Load() {
		t.Fatal("history writer never became ready")
	}
	m.historyChan <- "2026-01-01 00:00:00.000: alpha\n"
	m.historyChan <- "2026-01-01 00:00:01.000: beta\n"

	m.closeHistory() // must drain + close before returning

	data, err := os.ReadFile(hist)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(string(data), "alpha") || !strings.Contains(string(data), "beta") {
		t.Fatalf("history not fully flushed: %q", data)
	}
}

// An invalid Suggest.Text must yield a clean registration error, not a panic
// from inside the flag package.
func TestInvalidSuggestTextIsError(t *testing.T) {
	for _, bad := range []string{"-x", "a=b", "a b", ""} {
		h := NewHandlerInfo("h", func(s string) {}, WithSuggests([]Suggest{{Text: bad}}))
		if err := h.CheckAndInitHandler(); err == nil {
			t.Errorf("Suggest.Text %q should be rejected", bad)
		}
	}
	h := NewHandlerInfo("h", func(s string) {}, WithSuggests([]Suggest{{Text: "ok"}}))
	if err := h.CheckAndInitHandler(); err != nil {
		t.Errorf("valid Suggest.Text rejected: %v", err)
	}
}

// A struct-literal HandlerInfo with a nil func must return an error, not panic.
func TestCheckAndInitHandlerNilFunc(t *testing.T) {
	h := &HandlerInfo{Name: "bad"} // Handler and HandlerReflectType are nil
	if err := h.CheckAndInitHandler(); err == nil {
		t.Fatal("expected error for nil handler func")
	}
	// literal with a valid func but no reflect type should recover and pass
	h2 := &HandlerInfo{Name: "ok", Handler: func(s string) {}, UseFlagSet: false}
	if err := h2.CheckAndInitHandler(); err != nil {
		t.Fatalf("literal handler with valid func should pass, got: %v", err)
	}
}

// History written by one model must be reloadable by the next (round-trip).
func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	hist := filepath.Join(dir, "rt.history")

	m1 := NewPromptModel(WithHistoryFile(hist))
	for i := 0; i < 250 && !m1.readyToSaveHistory.Load(); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	m1.historyChan <- "2026-01-02 03:04:05.000: mycmd arg\n"
	m1.closeHistory()

	// A fresh model (saving disabled so it doesn't spawn a writer) still loads.
	m2 := NewPromptModel(WithHistoryFile(hist), WithOutSaveHistory())
	found := false
	for _, h := range m2.historys {
		if h == "mycmd arg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("history not reloaded; historys=%v", m2.historys)
	}
}

// closeHistory on a history-disabled model is a safe no-op, and idempotent.
func TestCloseHistoryDisabledNoop(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory())
	m.closeHistory() // historyDone is nil -> must not panic or block
	m.closeHistory() // double call also safe
}

// A WithoutFlagSet handler must not be offered flag-style param completions.
func TestWithoutFlagSetNoParamSuggestions(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithOutHelpMsg())
	m.RegisterHandler(func(s string) {}, "hello", WithoutFlagSet(),
		WithSuggests([]Suggest{{Text: "people", Default: "world"}}))

	typeAndRender(m, "hello ") // trailing space -> param position
	if len(m.matchSuggests) != 0 {
		t.Fatalf("WithoutFlagSet handler should offer no param suggestions, got %+v", m.matchSuggests)
	}
}

// SetHandlers must validate handlers instead of installing broken ones.
func TestSetHandlersPanicsOnInvalid(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory())
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetHandlers should panic on an invalid handler")
		}
	}()
	m.SetHandlers(map[string]*HandlerInfo{"bad": {Name: "bad"}})
}
