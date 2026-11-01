package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type level int

// level implements flag.Value.
func (l *level) String() string { return fmt.Sprint(int(*l)) }
func (l *level) Set(s string) error {
	switch s {
	case "low":
		*l = 1
	case "high":
		*l = 2
	default:
		return fmt.Errorf("want low or high")
	}
	return nil
}

// upper implements encoding.TextUnmarshaler.
type upper string

func (u *upper) UnmarshalText(b []byte) error {
	*u = upper(strings.ToUpper(string(b)))
	return nil
}

type greetArgs struct {
	People   string        `help:"who" default:"world"`
	Times    int           `flag:"n" default:"1"`
	Shout    bool          `help:"loud"`
	MaxWait  time.Duration `default:"1s"`
	Level    level
	Name     upper
	Skipped  string `flag:"-"`
	internal int
}

func TestStructArgs(t *testing.T) {
	var got greetArgs
	c := mustCommand(t, "greet", func(a greetArgs) { got = a })

	if err := c.Run("greet"); err != nil {
		t.Fatal(err)
	}
	if got.People != "world" || got.Times != 1 || got.Shout || got.MaxWait != time.Second {
		t.Fatalf("defaults = %+v", got)
	}
	if err := c.Run(`greet -people "张 三" -n 3 -shout -max-wait 2m -level high -name bob`); err != nil {
		t.Fatal(err)
	}
	want := greetArgs{People: "张 三", Times: 3, Shout: true, MaxWait: 2 * time.Minute, Level: 2, Name: "BOB"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// fresh defaults every run
	if err := c.Run("greet"); err != nil || got.People != "world" || got.Shout {
		t.Fatalf("values leaked into the next run: %+v (%v)", got, err)
	}
	// flag:"-" and unexported fields aren't flags
	if err := c.Run("greet -skipped x"); err == nil {
		t.Fatal("-skipped must not be a flag")
	}
	if err := c.Run("greet -level medium"); err == nil || !strings.Contains(err.Error(), "want low or high") {
		t.Fatalf("flag.Value error not reported: %v", err)
	}
}

func TestStructPointerArgsWithContext(t *testing.T) {
	var got *greetArgs
	c := mustCommand(t, "g", func(ctx context.Context, a *greetArgs) error {
		got = a
		return ctx.Err()
	})
	if err := c.Run("g -n 2"); err != nil || got == nil || got.Times != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestStructArgsErrors(t *testing.T) {
	type badType struct{ M map[string]int }
	if _, err := testCommand("c", "", func(badType) {}, false); err == nil ||
		!strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("unsupported field type: err = %v", err)
	}
	type badDefault struct {
		N int `default:"abc"`
	}
	if _, err := testCommand("c", "", func(badDefault) {}, false); err == nil {
		t.Error("an unparsable default tag must be rejected")
	}
	type dup struct {
		A int `flag:"x"`
		B int `flag:"x"`
	}
	if _, err := testCommand("c", "", func(dup) {}, false); err == nil {
		t.Error("duplicate flag names must be rejected")
	}
}

// time.Time is a struct but implements TextUnmarshaler: it's one value, not
// a struct of flags.
func TestTextUnmarshalerStructIsAValue(t *testing.T) {
	var got time.Time
	c := mustCommand(t, "at", func(when time.Time) { got = when }, Flag("when", ""))
	if err := c.Run("at -when 2026-11-01T10:00:00Z"); err != nil || got.Hour() != 10 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSignatureErrorsAreHelpful(t *testing.T) {
	_, err := testCommand("calc", "", func(a, b int) {}, false, Flag("a", ""))
	if err == nil || !strings.Contains(err.Error(), "2 parameter(s) (int, int) but 1 Flag option(s)") {
		t.Errorf("count mismatch: %v", err)
	}
	if _, err := testCommand("v", "", func(xs ...int) {}, false); err == nil {
		t.Error("variadic functions must be rejected")
	}
	if _, err := testCommand("r", "", func(s string) {}, true, Flag("x", "")); err == nil {
		t.Error("RawCommand with Flag options must be rejected")
	}
	if _, err := testCommand("r", "", func(n int) {}, true); err == nil {
		t.Error("RawCommand needs func(string)")
	}
	if _, err := testCommand("-x", "", func() {}, false); err == nil {
		t.Error("a name starting with '-' must be rejected")
	}
}

func TestBuiltinCommands(t *testing.T) {
	m := New(WithoutHistory())
	m.Command("calc", "add", func(a int) {}, Flag("a", ""))
	if m.lookup("help") == nil || m.lookup("exit") == nil {
		t.Fatal("help and exit should be built in")
	}
	out := captureStdout(t, func() { m.runCmd("help") })
	for _, want := range []string{"calc", "add", "exit", "help"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
	if m.runCmd("exit"); !m.exit {
		t.Error("exit should quit")
	}

	// a user command replaces the built-in of the same name
	m2 := New(WithoutHistory())
	called := false
	m2.Command("exit", "my exit", func() { called = true })
	m2.runCmd("exit")
	if !called || m2.exit {
		t.Errorf("user exit: called=%v quit=%v", called, m2.exit)
	}

	m3 := New(WithoutHistory(), WithoutBuiltinCommands())
	if m3.lookup("help") != nil || m3.lookup("exit") != nil {
		t.Error("WithoutBuiltinCommands should remove help and exit")
	}
}

func TestResultHandling(t *testing.T) {
	m := New(WithoutHistory())
	m.Command("add", "", func(a, b int) int { return a + b }, Flag("a", ""), Flag("b", ""))
	if out := captureStdout(t, func() { m.runCmd("add -a 1 -b 2") }); out != "3\n" {
		t.Errorf("default result printing = %q, want 3", out)
	}

	var got []any
	m2 := New(WithoutHistory(), WithResultHandler(func(r []any) { got = r }))
	m2.Command("pair", "", func() (string, int) { return "x", 1 })
	m2.runCmd("pair")
	if len(got) != 2 || got[0] != "x" || got[1] != 1 {
		t.Errorf("WithResultHandler got %v", got)
	}

	m3 := New(WithoutHistory(), WithResultHandler(nil))
	m3.Command("one", "", func() int { return 1 })
	if out := captureStdout(t, func() { m3.runCmd("one") }); out != "" {
		t.Errorf("WithResultHandler(nil) printed %q", out)
	}
}

func TestNewHasNoSideEffects(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "sub", "history")
	if err := os.MkdirAll(filepath.Dir(hist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hist, []byte("2026-01-02 03:04:05.000: old cmd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(WithHistoryFile(hist), WithLogFile(filepath.Join(t.TempDir(), "never.log")))
	if len(m.historys) != 0 || m.historyDone != nil || m.restoreStdout != nil {
		t.Fatal("New must not load history, start goroutines or redirect stdout")
	}
	if err := m.start(); err != nil {
		t.Fatal(err)
	}
	m.stop()
	if len(m.historys) != 1 || m.historys[0] != "old cmd" {
		t.Fatalf("start should load history, got %q", m.historys)
	}
}

func TestDefaultHistoryInUserConfigDir(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg) // os.UserConfigDir on Linux
	t.Setenv("HOME", t.TempDir())
	m := startedPrompt(t, WithAppName("mytool"))
	want := filepath.Join(cfg, "mytool", "history")
	if m.historyFile != want {
		t.Fatalf("history file = %q, want %q", m.historyFile, want)
	}
	for i := 0; i < 250 && !m.readyToSaveHistory.Load(); i++ {
		time.Sleep(2 * time.Millisecond)
	}
	m.saveHistoryEntry("hello")
	m.stop()
	data, err := os.ReadFile(want)
	if err != nil || !strings.Contains(string(data), ": hello") {
		t.Fatalf("history not written to %s: %q, %v", want, data, err)
	}
}

func TestCompleterOption(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	var seen string
	m.RawCommand("open", "", func(string) {}, Completer(func(input string) []Suggestion {
		seen = input
		return []Suggestion{{Text: "b.txt"}, {Text: "a.txt"}}
	}))
	typeAndRender(m, "open ")
	if seen != "open " || len(m.matchSuggests) != 2 || m.matchSuggests[0].Text != "a.txt" {
		t.Fatalf("seen=%q suggestions=%+v", seen, m.matchSuggests)
	}
}

func TestKebabCase(t *testing.T) {
	for in, want := range map[string]string{
		"A": "a", "MaxCount": "max-count", "UserID": "user-id", "HTTPServer": "http-server",
		"V2Name": "v2-name", "名字": "名字",
	} {
		if got := kebabCase(in); got != want {
			t.Errorf("kebabCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnknownCommandMessage(t *testing.T) {
	m := New(WithoutHistory())
	if s := printedText(m.runCmd("nope -x")); !strings.Contains(s, `unknown command "nope"`) {
		t.Fatalf("got %q", s)
	}
}
