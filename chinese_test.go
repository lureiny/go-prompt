package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// A real IME commits a whole phrase as ONE KeyMsg carrying multiple runes
// (not one rune at a time). This is the primary real-world Chinese-input path.
func TestIMEBatchMultiRuneInput(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("查询状态", "", func(s string) {})

	m.Update(textKey("查询"))
	if v := m.textInput.Value(); v != "查询" {
		t.Fatalf("batch input value = %q, want 查询", v)
	}
	if m.historyBufferPos != len("查询") {
		t.Fatalf("historyBufferPos = %d, want %d (byte len)", m.historyBufferPos, len("查询"))
	}
	_ = m.View()
	if !strings.Contains(m.View().Content, "查询状态") {
		t.Fatalf("batch-typed 查询 should suggest 查询状态")
	}
}

// Inserting a multi-rune batch when the cursor is in the MIDDLE of a CJK line
// (the hardest case for the rune/byte offset handling).
func TestIMEBatchMidLineInsert(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("打印", "", func(s string) {})

	m.Update(textKey("打印日志"))
	m.Update(key(tea.KeyLeft)) // cursor before 志
	m.Update(key(tea.KeyLeft)) // cursor before 日
	m.Update(textKey("测试"))
	if v := m.textInput.Value(); v != "打印测试日志" {
		t.Fatalf("mid-line CJK insert = %q, want 打印测试日志", v)
	}
	if m.historyBufferPos > len(m.textInput.Value()) {
		t.Fatalf("historyBufferPos %d out of range", m.historyBufferPos)
	}
	_ = m.View() // must not panic
}

// Chinese command content must survive a history save -> load round-trip.
func TestChineseHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	hist := filepath.Join(dir, "zh.history")

	m1 := startedPrompt(t, WithHistoryFile(hist))
	for i := 0; i < 250 && !m1.readyToSaveHistory.Load(); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	m1.historyChan <- "2026-01-02 03:04:05.000: 查询状态 全部\n"
	m1.closeHistory()

	m2 := startedPrompt(t, WithHistoryFile(hist))
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
	h, hErr := testCommand("查询", "", func(s string) { got = s }, true)
	if err := hErr; err != nil {
		t.Fatal(err)
	}
	if err := h.Run("查询 张三 的状态"); err != nil {
		t.Fatal(err)
	}
	if got != "张三 的状态" {
		t.Fatalf("handler got %q, want 张三 的状态", got)
	}
}

// The full-width space U+3000 (emitted by CJK IMEs in full-width mode) is a
// token separator, like the ASCII space: committed commands use ASCII spaces.
func TestFullWidthSpaceIsSeparator(t *testing.T) {
	m := New(WithoutHistory())
	m.historyBuffers[m.historyIndex] = "echo　你好　　世界　" // e.g. from an old history file
	if got := m.getCurrentCmdString(); got != "echo 你好 世界" {
		t.Fatalf("getCurrentCmdString = %q, want 'echo 你好 世界'", got)
	}
	// ASCII whitespace is still normalized (collapsed + trimmed).
	m.historyBuffers[m.historyIndex] = "  echo   hi  "
	if got := m.getCurrentCmdString(); got != "echo hi" {
		t.Fatalf("ASCII normalization = %q, want 'echo hi'", got)
	}
}

// Typing with the IME in full-width mode: U+3000 between handler name, flag and
// value must still dispatch the handler with the right flag value.
func TestFullWidthSpaceTypedReachesHandler(t *testing.T) {
	var got string
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("查询", "", func(名字 string) { got = 名字 }, Flag("名字", ""))

	var mm tea.Model = m
	for _, r := range "查询　-名字　你好" {
		mm, _ = mm.Update(textKey(string(r)))
	}
	if v := m.textInput.Value(); v != "查询 -名字 你好" {
		t.Fatalf("input = %q, want full-width spaces normalized", v)
	}
	if m.historyBufferPos != len(m.textInput.Value()) {
		t.Fatalf("historyBufferPos = %d, want byte len %d", m.historyBufferPos, len(m.textInput.Value()))
	}
	mm, teaCmd := mm.Update(key(tea.KeyEnter))
	runAll(mm, teaCmd)
	if got != "你好" {
		t.Fatalf("handler got %q, want 你好", got)
	}
}

// Normalizing a full-width space inserted mid-line must keep the cursor (and
// the byte offset used for suggestions) right after the inserted space.
func TestFullWidthSpaceMidLineCursor(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Update(textKey("查询你好"))
	m.Update(key(tea.KeyLeft))
	m.Update(key(tea.KeyLeft)) // cursor before 你
	m.Update(textKey("　"))
	if v := m.textInput.Value(); v != "查询 你好" {
		t.Fatalf("input = %q, want 查询 你好", v)
	}
	if p := m.textInput.Position(); p != 3 {
		t.Fatalf("rune cursor = %d, want 3", p)
	}
	if m.historyBufferPos != len("查询 ") {
		t.Fatalf("historyBufferPos = %d, want %d", m.historyBufferPos, len("查询 "))
	}
}

// Pasted text containing full-width spaces is normalized the same way.
func TestFullWidthSpacePasted(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Update(tea.PasteMsg{Content: "查询　-名字　你好"})
	if v := m.historyBuffers[m.historyIndex]; v != "查询 -名字 你好" {
		t.Fatalf("buffer = %q, want full-width spaces normalized", v)
	}
}

// A value-less bool flag (CJK-named) must not suppress the next flag's suggestions.
func TestBoolFlagDoesNotSuppressNextSuggestion(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("查询", "", func(详细 bool, 名字 string) {}, Flag("详细", "").Default(false), Flag("名字", ""))

	typeAndRender(m, "查询 -详细 -")
	if len(m.matchSuggests) == 0 {
		t.Fatal("a value-less bool flag should not suppress the next flag's suggestions")
	}
}

// With WithPlainLog, CJK output larger than the pipe read buffer must not be
// corrupted (a rune straddling the 1024-byte boundary must survive intact).
func TestRedirectPlainLogCJKNotCorrupted(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.log")
	m := New(WithoutHistory())
	m.outFile = outPath
	m.plainLog = true

	origStdout, origStderr := os.Stdout, os.Stderr
	if err := m.redirectStdout(); err != nil {
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
	m := New(WithoutHistory(), WithHelpLine(false))
	m.RawCommand("查询", "", func(s string) {})

	// enter a CJK command to push it into history
	m.Update(textKey("查询 全部"))
	_, enterCmd := m.Update(key(tea.KeyEnter))
	runAll(m, enterCmd) // run the command to completion
	// navigate back up onto the CJK history entry
	m.Update(key(tea.KeyUp))
	if v := m.textInput.Value(); v != "查询 全部" {
		t.Fatalf("history nav value = %q, want 查询 全部", v)
	}
	if m.historyBufferPos != len("查询 全部") {
		t.Fatalf("historyBufferPos = %d, want %d", m.historyBufferPos, len("查询 全部"))
	}
	_ = m.View() // must not panic
}

// bubbletea v2 delivers bracketed paste as a tea.PasteMsg, not a key press.
// Pasted Chinese must land in the command buffer and reach the handler.
func TestPastedChineseReachesHandler(t *testing.T) {
	var got string
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("查询", "", func(名字 string) { got = 名字 }, Flag("名字", ""))

	var mm tea.Model = m
	mm, _ = mm.Update(textKey("查询 -名字 "))
	mm, _ = mm.Update(tea.PasteMsg{Content: "粘贴的中文"})
	if m.historyBuffers[m.historyIndex] != "查询 -名字 粘贴的中文" {
		t.Fatalf("buffer after paste = %q", m.historyBuffers[m.historyIndex])
	}
	if m.historyBufferPos != len(m.historyBuffers[m.historyIndex]) {
		t.Fatalf("historyBufferPos = %d, want byte len %d", m.historyBufferPos, len(m.historyBuffers[m.historyIndex]))
	}
	mm, teaCmd := mm.Update(key(tea.KeyEnter))
	runAll(mm, teaCmd)
	if got != "粘贴的中文" {
		t.Fatalf("handler got %q, want 粘贴的中文", got)
	}
}
