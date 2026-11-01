package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sortSuggestions must preserve every element even when two share the same Text.
func TestSortSuggestionsKeepsDuplicateText(t *testing.T) {
	in := []Suggestion{
		{Text: "b", Description: "first"},
		{Text: "a", Description: "second"},
		{Text: "a", Description: "third"},
	}
	out := sortSuggestions(in)
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
	m := startedPrompt(t, WithHistoryFile(hist))

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

// An invalid flag name must yield a clean registration error, not a panic
// from inside the flag package.
func TestInvalidFlagNameIsError(t *testing.T) {
	for _, bad := range []string{"-x", "a=b", "a b", ""} {
		_, err := testCommand("h", "", func(s string) {}, false, Flag(bad, ""))
		if err == nil {
			t.Errorf("flag name %q should be rejected", bad)
		}
	}
	if _, err := testCommand("h", "", func(s string) {}, false, Flag("ok", "")); err != nil {
		t.Errorf("valid flag name rejected: %v", err)
	}
}

// A nil or non-func command function is a registration error, not a panic
// later at run time.
func TestNilOrNonFuncCommandIsError(t *testing.T) {
	for _, fn := range []any{nil, 42, "f", (func())(nil)} {
		if _, err := testCommand("bad", "", fn, false); err == nil {
			t.Errorf("fn %#v should be rejected", fn)
		}
	}
}

// History written by one model must be reloadable by the next (round-trip).
func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	hist := filepath.Join(dir, "rt.history")

	m1 := startedPrompt(t, WithHistoryFile(hist))
	for i := 0; i < 250 && !m1.readyToSaveHistory.Load(); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	m1.historyChan <- "2026-01-02 03:04:05.000: mycmd arg\n"
	m1.closeHistory()

	// A fresh model (saving disabled so it doesn't spawn a writer) still loads.
	m2 := startedPrompt(t, WithHistoryFile(hist))
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
	m := New(WithoutHistory())
	m.closeHistory() // historyDone is nil -> must not panic or block
	m.closeHistory() // double call also safe
}

// A RawCommand must not be offered flag-style param completions.
func TestRawCommandNoFlagSuggestions(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("hello", "", func(s string) {})

	typeAndRender(m, "hello ") // trailing space -> param position
	if len(m.matchSuggests) != 0 {
		t.Fatalf("RawCommand should offer no flag suggestions, got %+v", m.matchSuggests)
	}
}

// Registering the same command name twice is a setup error and panics.
func TestDuplicateCommandPanics(t *testing.T) {
	m := New(WithoutHistory())
	m.Command("a", "", func() {})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("registering a duplicate command should panic")
		}
	}()
	m.Command("a", "", func() {})
}
