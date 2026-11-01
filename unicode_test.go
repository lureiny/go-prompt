package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func typeString(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(textKey(string(r)))
	}
	return m
}

// P3: IsMatch must compare whole runes so multibyte names match.
func TestIsMatchUnicode(t *testing.T) {
	cases := []struct {
		input, suggest string
		want           bool
	}{
		{"查", "查询状态", true},
		{"查询", "查询状态", true},
		{"查态", "查询状态", true},   // subsequence
		{"xyz", "查询状态", false}, // no false positive from byte truncation
		{"c", "check", true},
		{"ck", "check", true},
		{"cx", "check", false},
		{"", "anything", true},
	}
	for _, c := range cases {
		if got := isMatch(c.input, c.suggest); got != c.want {
			t.Errorf("isMatch(%q, %q) = %v, want %v", c.input, c.suggest, got, c.want)
		}
	}
}

// P3: typing a Chinese prefix must surface the matching handler suggestion.
func TestChineseHandlerSuggestion(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("查询状态", "", func(s string) {})
	m.RawCommand("check", "", func(s string) {})

	var mm tea.Model = m
	mm = typeString(mm, "查")
	v := mm.View().Content
	if !strings.Contains(v, "查询状态") {
		t.Fatalf("typing '查' should suggest '查询状态'; view:\n%s", v)
	}
}

// P3: rune<->byte position helpers.
func TestPositionHelpers(t *testing.T) {
	s := "a查b" // bytes: 'a'(1) '查'(3) 'b'(1) = 5 bytes, 3 runes
	if got := runeIndexToByteOffset(s, 0); got != 0 {
		t.Errorf("runeIndexToByteOffset rune0 = %d, want 0", got)
	}
	if got := runeIndexToByteOffset(s, 1); got != 1 {
		t.Errorf("runeIndexToByteOffset rune1 = %d, want 1", got)
	}
	if got := runeIndexToByteOffset(s, 2); got != 4 {
		t.Errorf("runeIndexToByteOffset rune2 = %d, want 4", got)
	}
	if got := runeIndexToByteOffset(s, 3); got != 5 {
		t.Errorf("runeIndexToByteOffset rune3 = %d, want 5", got)
	}
	if got := byteOffsetToRuneIndex(s, 4); got != 2 {
		t.Errorf("byteOffsetToRuneIndex byte4 = %d, want 2", got)
	}
	if got := byteOffsetToRuneIndex(s, 5); got != 3 {
		t.Errorf("byteOffsetToRuneIndex byte5 = %d, want 3", got)
	}
	// round-trip
	for i := 0; i <= 3; i++ {
		if got := byteOffsetToRuneIndex(s, runeIndexToByteOffset(s, i)); got != i {
			t.Errorf("round-trip rune %d = %d", i, got)
		}
	}
}

// typeAndRender types s then calls View() (which populates matchSuggests via
// updateSuggentList) so a subsequent Tab has candidates to work with.
func typeAndRender(m *Prompt, s string) {
	for _, r := range s {
		m.Update(textKey(string(r)))
	}
	_ = m.View()
}

func press(m *Prompt, k rune) { m.Update(key(k)) }

// P3: tab must actually complete a multibyte handler name, and repeated tab
// must cycle through all matches (regression guard for the buffer-anchor fix).
func TestTabCompletionAndCyclingMultibyte(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("查询历史", "", func(a string) {})
	m.RawCommand("查询状态", "", func(a string) {})

	typeAndRender(m, "查询") // prefix matches both handlers
	if len(m.matchSuggests) != 2 {
		t.Fatalf("expected 2 candidates before tab, got %d: %+v", len(m.matchSuggests), m.matchSuggests)
	}

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		press(m, tea.KeyTab)
		v := m.textInput.Value()
		seen[v] = true
		// buffer stays anchored to the typed prefix so the candidate list persists
		if m.historyBuffers[m.historyIndex] != "查询" {
			t.Fatalf("after tab %d, buffer should stay anchored to '查询', got %q",
				i, m.historyBuffers[m.historyIndex])
		}
		_ = m.View() // re-render as bubbletea would between key presses
		if len(m.matchSuggests) != 2 {
			t.Fatalf("after tab %d, candidate list collapsed to %d (cycling broken)",
				i, len(m.matchSuggests))
		}
	}
	if !seen["查询历史"] || !seen["查询状态"] {
		t.Fatalf("tab did not cycle through both candidates; saw %v", seen)
	}
	// a third tab must cycle back to the first candidate, not get stuck
	press(m, tea.KeyTab)
	if v := m.textInput.Value(); v != "查询历史" && v != "查询状态" {
		t.Fatalf("third tab produced unexpected value %q", v)
	}
}

// P3: ASCII tab cycling with param suggestions of a flag-set handler.
func TestTabCyclingASCIIParams(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("calc", "", func(apple, apricot, banana int) {}, Flag("apple", ""), Flag("apricot", ""), Flag("banana", ""))

	typeAndRender(m, "calc -a") // -a matches apple & apricot (subsequence), not banana
	got := len(m.matchSuggests)
	if got < 2 {
		t.Fatalf("expected >=2 param candidates for '-a', got %d: %+v", got, m.matchSuggests)
	}
	values := map[string]bool{}
	for i := 0; i < got+1; i++ { // one extra to prove it cycles back
		press(m, tea.KeyTab)
		values[m.textInput.Value()] = true
		_ = m.View()
		if len(m.matchSuggests) != got {
			t.Fatalf("param candidate list collapsed from %d to %d after tab %d",
				got, len(m.matchSuggests), i)
		}
	}
	if len(values) < got {
		t.Fatalf("tab reached only %d of %d distinct completions: %v", len(values), got, values)
	}
}

// P3: moving the cursor into the middle of a CJK line keeps historyBufferPos a
// valid byte offset, and suggestions scope against the correct prefix.
func TestMidLineCJKCursor(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("查询", "", func(s string) {})

	typeAndRender(m, "查询") // value "查询", cursor at end
	if m.historyBufferPos != 6 {
		t.Fatalf("after typing '查询', historyBufferPos = %d, want 6 (byte len)", m.historyBufferPos)
	}
	press(m, tea.KeyLeft) // cursor between 查 and 询 (rune 1, byte 3)
	if m.historyBufferPos != 3 {
		t.Fatalf("after KeyLeft, historyBufferPos = %d, want byte offset 3", m.historyBufferPos)
	}
	_ = m.View() // updateSuggentList slices "查询"[:3] == "查" — must not panic
	if got := m.historyBuffers[m.historyIndex][:m.historyBufferPos]; got != "查" {
		t.Fatalf("prefix slice = %q, want %q", got, "查")
	}
}

// P3: backspace over a multibyte rune deletes the whole rune and keeps the
// byte offset valid.
func TestBackspaceOverMultibyte(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("查询", "", func(s string) {})

	typeAndRender(m, "查询")
	press(m, tea.KeyBackspace) // remove 询
	if v := m.textInput.Value(); v != "查" {
		t.Fatalf("after backspace, value = %q, want %q", v, "查")
	}
	if m.historyBufferPos != len("查") {
		t.Fatalf("historyBufferPos = %d, want %d", m.historyBufferPos, len("查"))
	}
	_ = m.View() // must not panic
}

// P3: after typing Chinese then navigating history, no panic and suggestions
// still recompute against a byte-correct slice.
func TestChineseHistoryNavNoPanic(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("打印", "", func(s string) {})

	var mm tea.Model = m
	mm = typeString(mm, "打印")
	mm, enterCmd := mm.Update(key(tea.KeyEnter)) // push history
	mm = runAll(mm, enterCmd)                    // run the command to completion
	mm, _ = mm.Update(key(tea.KeyUp))            // navigate back into history
	_ = mm.View().Content                        // must not panic
}
