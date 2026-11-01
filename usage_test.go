package prompt

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDashHPrintsUsage(t *testing.T) {
	called := false
	h, err := testCommand("calc", "add numbers", func(a int16, 名字 string, v bool) { called = true }, false,
		Flag("a", "first").Default(10000), Flag("名字", "who"), Flag("v", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"-h", "-help", "--help"} {
		err := h.Run("calc " + flag)
		usage, ok := isUsage(err)
		if !ok {
			t.Fatalf("%s: err = %v, want usage", flag, err)
		}
		for _, want := range []string{"usage: calc [flags]", "add numbers", "-a int16", "first (default: 10000)", "-名字 string", "who", "-v bool"} {
			if !strings.Contains(usage, want) {
				t.Errorf("%s usage missing %q:\n%s", flag, want, usage)
			}
		}
	}
	if called {
		t.Fatal("-h must not run the handler")
	}
}

func TestDashHThroughPromptIsNotAFailure(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("calc", "", func(a int) {}, Flag("a", ""))
	cmd := m.runCmd("calc -h")
	if cmd == nil {
		t.Fatal("want a print command")
	}
	if s := strings.TrimSpace(printedText(cmd)); !strings.HasPrefix(s, "usage: calc") {
		t.Fatalf("printed %q, want the usage text (not a failure message)", s)
	}
}

func TestHelpHandler(t *testing.T) {
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("calc", "calculate", func(a int) {}, Flag("a", "num"))

	list := captureStdout(t, func() { m.runCmd("help") })
	for _, want := range []string{"calc", "calculate", "help"} {
		if !strings.Contains(list, want) {
			t.Errorf("help list missing %q:\n%s", want, list)
		}
	}
	one := captureStdout(t, func() { m.runCmd("help calc") })
	if !strings.Contains(one, "-a int") || !strings.Contains(one, "num") {
		t.Errorf("help calc = %q", one)
	}
}

// printedText runs a tea.Println/Printf command and returns its text. The
// message type is unexported, so read it via %+v ("{messageBody:...}").
func printedText(cmd tea.Cmd) string {
	s := fmt.Sprintf("%+v", cmd())
	s = strings.TrimPrefix(s, "{messageBody:")
	return strings.TrimSuffix(s, "}")
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = orig
	w.Close()
	var b bytes.Buffer
	io.Copy(&b, r)
	return b.String()
}
