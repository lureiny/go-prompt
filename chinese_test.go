package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A real IME commits a whole phrase as ONE KeyMsg carrying multiple runes
// (not one rune at a time). This is the primary real-world Chinese-input path.
func TestIMEBatchMultiRuneInput(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithOutHelpMsg())
	m.RegisterHandler(func(s string) {}, "查询状态", WithoutFlagSet())

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("查询")})
	if v := m.textInput.Value(); v != "查询" {
		t.Fatalf("batch input value = %q, want 查询", v)
	}
	if m.historyBufferPos != len("查询") {
		t.Fatalf("historyBufferPos = %d, want %d (byte len)", m.historyBufferPos, len("查询"))
	}
	m.View()
	if !strings.Contains(m.View(), "查询状态") {
		t.Fatalf("batch-typed 查询 should suggest 查询状态")
	}
}

// Inserting a multi-rune batch when the cursor is in the MIDDLE of a CJK line
// (the hardest case for the rune/byte offset handling).
func TestIMEBatchMidLineInsert(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithOutHelpMsg())
	m.RegisterHandler(func(s string) {}, "打印", WithoutFlagSet())

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("打印日志")})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // cursor before 志
	m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // cursor before 日
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("测试")})
	if v := m.textInput.Value(); v != "打印测试日志" {
		t.Fatalf("mid-line CJK insert = %q, want 打印测试日志", v)
	}
	if m.historyBufferPos > len(m.textInput.Value()) {
		t.Fatalf("historyBufferPos %d out of range", m.historyBufferPos)
	}
	m.View() // must not panic
}

// Chinese command content must survive a history save -> load round-trip.
func TestChineseHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	hist := filepath.Join(dir, "zh.history")

	m1 := NewPromptModel(WithHistoryFile(hist))
	for i := 0; i < 250 && !m1.readyToSaveHistory.Load(); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	m1.historyChan <- "2026-01-02 03:04:05.000: 查询状态 全部\n"
	m1.closeHistory()

	m2 := NewPromptModel(WithHistoryFile(hist), WithOutSaveHistory())
	found := false
	for _, h := range m2.historys {
		if h == "查询状态 全部" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Chinese history not reloaded intact; got %v", m2.historys)
	}
}

// A WithoutFlagSet handler must receive Chinese args (name stripped, content intact).
func TestChineseArgsToHandler(t *testing.T) {
	var got string
	h := NewHandlerInfo("查询", func(s string) { got = s }, WithoutFlagSet())
	if err := h.CheckAndInitHandler(); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("查询 张三 的状态"); err != nil {
		t.Fatal(err)
	}
	if got != "张三 的状态" {
		t.Fatalf("handler got %q, want 张三 的状态", got)
	}
}

// The full-width space U+3000 (emitted by CJK IMEs in full-width mode) must be
// treated as a normal character, not a token separator that gets rewritten to an
// ASCII space (which would corrupt args/flag values and history).
func TestFullWidthSpacePreserved(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory())
	m.historyBuffers[m.historyIndex] = "echo　你好　世界" // U+3000 separators
	if got := m.getCurrentCmdString(); got != "echo　你好　世界" {
		t.Fatalf("getCurrentCmdString = %q, want U+3000 preserved", got)
	}
	// ASCII whitespace is still normalized (collapsed + trimmed).
	m.historyBuffers[m.historyIndex] = "  echo   hi  "
	if got := m.getCurrentCmdString(); got != "echo hi" {
		t.Fatalf("ASCII normalization = %q, want 'echo hi'", got)
	}
}

// A flag value containing a full-width space must not be truncated at that space.
func TestFullWidthSpaceFlagValueNotTruncated(t *testing.T) {
	var got string
	m := NewPromptModel(WithOutSaveHistory(), WithOutHelpMsg())
	m.RegisterHandler(func(名字 string) { got = 名字 }, "查询",
		WithSuggests([]Suggest{{Text: "名字"}}))

	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("查询 -名字=你好　世界")})
	mm, teaCmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm.Update(teaCmd()) // execute the RunCmdMsg the enter handler produced
	if got != "你好　世界" {
		t.Fatalf("flag value = %q, want 你好　世界 (U+3000 not truncated)", got)
	}
}

// A value-less bool flag (CJK-named) must not suppress the next flag's suggestions.
func TestBoolFlagDoesNotSuppressNextSuggestion(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithOutHelpMsg())
	m.RegisterHandler(func(详细 bool, 名字 string) {}, "查询",
		WithSuggests([]Suggest{{Text: "详细", Default: false}, {Text: "名字"}}))

	typeAndRender(m, "查询 -详细 -")
	if len(m.matchSuggests) == 0 {
		t.Fatal("a value-less bool flag should not suppress the next flag's suggestions")
	}
}

// With WithFilterAscii, CJK output larger than the pipe read buffer must not be
// corrupted (a rune straddling the 1024-byte boundary must survive intact).
func TestRedirectFilterAsciiCJKNotCorrupted(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.log")
	m := NewPromptModel(WithOutSaveHistory())
	m.outFile = outPath
	m.filterAscii = true

	origStdout, origStderr := os.Stdout, os.Stderr
	if err := redirectStdout(m); err != nil {
		os.Stdout, os.Stderr = origStdout, origStderr
		t.Fatalf("redirect: %v", err)
	}
	writer := os.Stdout
	payload := strings.Repeat("查", 400) // 1200 bytes: a rune straddles byte 1024
	fmt.Fprint(os.Stdout, payload)
	writer.Close()
	os.Stdout, os.Stderr = origStdout, origStderr

	var data []byte
	for i := 0; i < 400; i++ {
		data, _ = os.ReadFile(outPath)
		if len(data) >= len(payload) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if string(data) != payload {
		t.Fatalf("CJK log corrupted: %d/%d bytes, has U+FFFD=%v",
			len(data), len(payload), strings.Contains(string(data), "�"))
	}
}

// Up/down history navigation onto a CJK entry keeps the byte offset consistent.
func TestChineseHistoryNavConsistency(t *testing.T) {
	m := NewPromptModel(WithOutSaveHistory(), WithOutHelpMsg())
	m.RegisterHandler(func(s string) {}, "查询", WithoutFlagSet())

	// enter a CJK command to push it into history
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("查询 全部")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(RunCmdMsg{cmd: "查询 全部"})
	// navigate back up onto the CJK history entry
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if v := m.textInput.Value(); v != "查询 全部" {
		t.Fatalf("history nav value = %q, want 查询 全部", v)
	}
	if m.historyBufferPos != len("查询 全部") {
		t.Fatalf("historyBufferPos = %d, want %d", m.historyBufferPos, len("查询 全部"))
	}
	m.View() // must not panic
}
