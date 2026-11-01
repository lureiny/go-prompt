package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Numbers are parsed at the parameter's own size: float32 overflow (either
// sign) is an error rather than ±Inf, and the float32 range edges are fine.
func TestFloat32Range(t *testing.T) {
	var got float32
	c := mustCommand(t, "f", func(x float32) { got = x }, Flag("x", ""))
	for _, bad := range []string{"-1e100", "1e100"} {
		if err := c.Run("f -x " + bad); err == nil || !strings.Contains(err.Error(), "out of range for float32") {
			t.Errorf("-x %s: err = %v, want out of range", bad, err)
		}
	}
	for _, ok := range []string{"-1.5", "3.4028234663852886e38", "-3.4028234663852886e38"} {
		if err := c.Run("f -x " + ok); err != nil {
			t.Errorf("-x %s: %v", ok, err)
		}
	}
	if err := c.Run("f -x -1.5"); err != nil || got != -1.5 {
		t.Fatalf("got %v, %v", got, err)
	}
}

// With a small terminal height, the rendered view must not exceed it even when
// suggestNum is large (input line must stay visible).
func TestSuggestionsClampedToTerminalHeight(t *testing.T) {
	m := New(WithoutHistory(), WithMaxSuggestions(20))
	for i, name := range []string{"aa", "ab", "ac", "ad", "ae", "af", "ag", "ah"} {
		_ = i
		m.RawCommand(name, "", func(s string) {})
	}
	var mm tea.Model = m
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 80, Height: 5})
	mm = typeString(mm, "a") // matches all handlers
	v := mm.View().Content
	lines := strings.Count(v, "\n") + 1
	if lines > 5 {
		t.Fatalf("view has %d lines, exceeds terminal height 5:\n%s", lines, v)
	}
}

func TestEffectiveSuggestNum(t *testing.T) {
	m := New(WithoutHistory(), WithMaxSuggestions(10))
	m.helpLine = true
	m.termHeight = 0
	if got := m.effectiveSuggestNum(); got != 10 {
		t.Fatalf("no height known: got %d, want 10 (unchanged)", got)
	}
	m.termHeight = 5 // 5 - 1 input - 1 help = 3
	if got := m.effectiveSuggestNum(); got != 3 {
		t.Fatalf("height 5 with help: got %d, want 3", got)
	}
	m.helpLine = false
	m.termHeight = 5 // 5 - 1 input = 4
	if got := m.effectiveSuggestNum(); got != 4 {
		t.Fatalf("height 5 no help: got %d, want 4", got)
	}
	m.termHeight = 1 // clamp to at least 1
	if got := m.effectiveSuggestNum(); got != 1 {
		t.Fatalf("tiny height: got %d, want 1", got)
	}
}

// completeUTF8Len must return a length that never cuts a multibyte rune.
func TestCompleteUTF8Len(t *testing.T) {
	full := []byte("查") // E6 9F A5 (3 bytes)
	if got := completeUTF8Len(full); got != 3 {
		t.Errorf("complete rune: got %d, want 3", got)
	}
	if got := completeUTF8Len(full[:2]); got != 0 {
		t.Errorf("2/3 of a rune: got %d, want 0", got)
	}
	if got := completeUTF8Len(full[:1]); got != 0 {
		t.Errorf("1/3 of a rune: got %d, want 0", got)
	}
	mixed := append([]byte("ab"), full...) // "ab查"
	if got := completeUTF8Len(mixed); got != len(mixed) {
		t.Errorf("ab查: got %d, want %d", got, len(mixed))
	}
	if got := completeUTF8Len(mixed[:3]); got != 2 { // "ab" + first byte of 查
		t.Errorf("ab + partial rune: got %d, want 2", got)
	}
	if got := completeUTF8Len([]byte{}); got != 0 {
		t.Errorf("empty: got %d, want 0", got)
	}
	if got := completeUTF8Len([]byte("abc")); got != 3 {
		t.Errorf("ascii: got %d, want 3", got)
	}
}

// Help text no longer contains the old "choise" typo.
func TestHelpTextSpelling(t *testing.T) {
	v := helpLineText
	if strings.Contains(v, "choise") {
		t.Fatalf("help text still contains typo 'choise': %q", v)
	}
	if !strings.Contains(v, "complete") {
		t.Fatalf("help text should mention completion: %q", v)
	}
}

// lipgloss v2 only parses #RGB/#RRGGBB; an 8-digit #RRGGBBAA silently becomes
// "no color". Guard the default suggestion styles against that regression.
func TestDefaultStyleColorsParse(t *testing.T) {
	ds := DefaultStyles()
	for name, st := range map[string]lipgloss.Style{"suggestion": ds.Suggestion, "selected": ds.Selected} {
		if _, ok := st.GetBackground().(lipgloss.NoColor); ok {
			t.Errorf("%s style background did not parse", name)
		}
		if _, ok := st.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("%s style foreground did not parse", name)
		}
	}
}
