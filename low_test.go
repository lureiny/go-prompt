package prompt

import (
	"math"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A large-magnitude negative float64 must be rejected for a float32 param,
// symmetric with the positive-overflow check (no silent -Inf).
func TestFloat32NegativeOverflowPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on negative float32 overflow")
		}
	}()
	float64ToFloatx(float64(-1e100), reflect.TypeOf(float32(0)))
}

func TestFloat32InRangeOK(t *testing.T) {
	got := float64ToFloatx(float64(-1.5), reflect.TypeOf(float32(0)))
	if got.(float32) != float32(-1.5) {
		t.Fatalf("got %v, want -1.5", got)
	}
	// boundary values must not overflow
	_ = float64ToFloatx(float64(math.MaxFloat32), reflect.TypeOf(float32(0)))
	_ = float64ToFloatx(float64(-math.MaxFloat32), reflect.TypeOf(float32(0)))
}

// With a small terminal height, the rendered view must not exceed it even when
// suggestNum is large (input line must stay visible).
func TestSuggestionsClampedToTerminalHeight(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithSuggestNum(20))
	for i, name := range []string{"aa", "ab", "ac", "ad", "ae", "af", "ag", "ah"} {
		_ = i
		m.RegisterHandler(func(s string) {}, name, WithoutFlagSet())
	}
	var mm tea.Model = m
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 80, Height: 5})
	mm = typeString(mm, "a") // matches all handlers
	v := mm.View()
	lines := strings.Count(v, "\n") + 1
	if lines > 5 {
		t.Fatalf("view has %d lines, exceeds terminal height 5:\n%s", lines, v)
	}
}

func TestEffectiveSuggestNum(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithSuggestNum(10))
	m.withHelpMsg = true
	m.termHeight = 0
	if got := m.effectiveSuggestNum(); got != 10 {
		t.Fatalf("no height known: got %d, want 10 (unchanged)", got)
	}
	m.termHeight = 5 // 5 - 1 input - 1 help = 3
	if got := m.effectiveSuggestNum(); got != 3 {
		t.Fatalf("height 5 with help: got %d, want 3", got)
	}
	m.withHelpMsg = false
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

// IsBoolSuggest must recognize a bool flag whether or not '=' is present.
func TestIsBoolSuggestValuelessFlag(t *testing.T) {
	suggests := []Suggest{{Text: "verbose", Default: false}, {Text: "name", Default: ""}}
	if !IsBoolSuggest(suggests, "-verbose", "-") {
		t.Error("value-less -verbose should be recognized as bool")
	}
	if !IsBoolSuggest(suggests, "-verbose=true", "-") {
		t.Error("-verbose=true should be recognized as bool")
	}
	if IsBoolSuggest(suggests, "-name", "-") {
		t.Error("string flag -name should not be bool")
	}
	if IsBoolSuggest(suggests, "name", "-") {
		t.Error("a token without the prefix should return false")
	}
}

// Help text no longer contains the old "choise" typo.
func TestHelpTextSpelling(t *testing.T) {
	v := HelpView()
	if strings.Contains(v, "choise") {
		t.Fatalf("help text still contains typo 'choise': %q", v)
	}
	if !strings.Contains(v, "choose") {
		t.Fatalf("help text should say 'choose': %q", v)
	}
}
