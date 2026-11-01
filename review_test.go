package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Regression tests for issues found in the code review.

// Tokens left over after flag parsing must be an error, not silently dropped.
func TestLeftoverArgsAreAnError(t *testing.T) {
	var a, b int
	h := mustCommand(t, "add", func(x, y int) { a, b = x, y }, Flag("a", ""), Flag("b", ""))
	for _, line := range []string{"add -a 1 2 -b 3", "add -a 1 extra"} {
		if err := h.Run(line); err == nil || !strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("Run(%q) err = %v, want unexpected argument error", line, err)
		}
	}
	// a double space (possible when calling Run directly) must not stop parsing
	if err := h.Run("add  -a 5   -b 6"); err != nil || a != 5 || b != 6 {
		t.Fatalf("double-space Run: err=%v a=%d b=%d, want 5 6", err, a, b)
	}

	var v bool
	hb := mustCommand(t, "t", func(x bool) { v = x }, Flag("v", ""))
	if err := hb.Run("t -v false"); err == nil {
		t.Fatal(`"-v false" for a bool flag must error (needs -v=false)`)
	}
	if err := hb.Run("t -v=false"); err != nil || v {
		t.Fatalf("-v=false: err=%v v=%v", err, v)
	}
}

// After "-flag=value" the next flag must still be suggested.
func TestSuggestAfterFlagEqualsValue(t *testing.T) {
	h := mustCommand(t, "c", func(name string, num int) {}, Flag("name", ""), Flag("num", ""))
	for _, in := range []string{"c -name=x ", "c -name=x -"} {
		got := h.completeFlags(in)
		if len(got) == 0 {
			t.Errorf("suggestions for %q are empty", in)
		}
	}
}

func TestHandlerNameWithSpaceRejected(t *testing.T) {
	if _, err := testCommand("show all", "", func(s string) {}, true); err == nil {
		t.Fatal("a handler name with a space can never be dispatched and must be rejected")
	}
}

// Leading spaces must not turn flag completion into handler-name completion.
func TestLeadingSpaceKeepsFlagSuggestions(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("calc", "", func(a int) {}, Flag("a", ""))
	typeAndRender(m, " calc -")
	if len(m.matchSuggests) != 1 || m.matchSuggests[0].Text != "-a" {
		t.Fatalf("suggestions = %+v, want [-a]", m.matchSuggests)
	}
}

func TestCtrlCResetsSuggestIndex(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	for _, n := range []string{"aa", "ab", "ac"} {
		m.RawCommand(n, "", func(s string) {})
	}
	typeAndRender(m, "a")
	press(m, tea.KeyTab)
	press(m, tea.KeyTab)
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.suggestIndex != -1 {
		t.Fatalf("suggestIndex after ctrl+c = %d, want -1", m.suggestIndex)
	}
}

func TestLoadHistoryCRLF(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "h")
	if err := os.WriteFile(hist, []byte("2026-01-02 03:04:05.000: calc -a 1\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := startedPrompt(t, WithHistoryFile(hist))
	defer m.closeHistory()
	if len(m.historys) != 1 || m.historys[0] != "calc -a 1" {
		t.Fatalf("historys = %q, want [calc -a 1]", m.historys)
	}
}

func TestDefaultNumericConversion(t *testing.T) {
	ok := []struct {
		def  interface{}
		fn   interface{}
		want interface{}
	}{
		{5, func(int64) {}, int64(5)},
		{5, func(uint8) {}, uint8(5)},
		{int64(-3), func(int8) {}, int8(-3)},
		{1.5, func(float32) {}, float32(1.5)},
		{7, func(float64) {}, float64(7)},
		{2.0, func(int) {}, 2},
		{0.1, func(float32) {}, float32(0.1)}, // float precision loss is fine
		{"5", func(int) {}, 5},                // command-line syntax
		{"0x10", func(uint8) {}, uint8(16)},
		{"1m30s", func(time.Duration) {}, 90 * time.Second},
		{time.Second, func(time.Duration) {}, time.Second},
	}
	for _, c := range ok {
		h, hErr := testCommand("c", "", c.fn, false, Flag("x", "").Default(c.def))
		if err := hErr; err != nil {
			t.Errorf("Default %v (%T) for %T: %v", c.def, c.def, c.fn, err)
			continue
		}
		if got := h.flags[0].def.Interface(); got != c.want {
			t.Errorf("Default %v -> %v (%T), want %v (%T)", c.def, got, got, c.want, c.want)
		}
	}
	bad := []struct {
		def interface{}
		fn  interface{}
	}{
		{300, func(int8) {}},
		{-1, func(uint) {}},
		{1.5, func(int) {}},
		{1e40, func(float32) {}}, // overflows float32
		{"abc", func(int) {}},    // a string default must parse
		{5, func(string) {}},
		{1, func(bool) {}},
	}
	for _, c := range bad {
		if _, err := testCommand("c", "", c.fn, false, Flag("x", "").Default(c.def)); err == nil {
			t.Errorf("Default %v (%T) for %T must be rejected", c.def, c.def, c.fn)
		}
	}
	// the converted default is what the handler receives
	var got int64
	h := mustCommand(t, "c", func(x int64) { got = x }, Flag("x", "").Default(42))
	if err := h.Run("c"); err != nil || got != 42 {
		t.Fatalf("Run: err=%v got=%d", err, got)
	}
}

func TestUsedFlagsNotSuggestedAgain(t *testing.T) {
	h := mustCommand(t, "c", func(a, b string, v bool) {}, Flag("a", ""), Flag("b", ""), Flag("v", ""))
	texts := func(in string) []string {
		got := h.completeFlags(in)
		var out []string
		for _, s := range got {
			out = append(out, s.Text)
		}
		return out
	}
	if got := texts("c -a x "); strings.Join(got, ",") != "-b,-v" {
		t.Errorf(`"c -a x " -> %v, want [-b -v]`, got)
	}
	if got := texts("c -a=x --v -"); strings.Join(got, ",") != "-b" {
		t.Errorf(`"c -a=x --v -" -> %v, want [-b]`, got)
	}
	// the word being typed itself doesn't count as used
	if got := texts("c -a"); strings.Join(got, ",") != "-a" {
		t.Errorf(`"c -a" -> %v, want [-a]`, got)
	}
}

func TestProgramAndInitOptionsAccumulate(t *testing.T) {
	m := New(WithoutHistory(),
		WithProgramOptions(tea.WithFPS(30)), WithProgramOptions(tea.WithFPS(60)),
		WithInitCmds(nil), WithInitCmds(nil, nil))
	if len(m.programOptions) != 2 || len(m.initCmds) != 3 {
		t.Fatalf("programOptions=%d initCmds=%d, want 2 and 3", len(m.programOptions), len(m.initCmds))
	}
}
