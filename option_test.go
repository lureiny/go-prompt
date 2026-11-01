package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// P2: WithOutFile must forward only the bytes written, never a NUL-padded
// fixed-size buffer, to the log file (and console).
func TestRedirectStdoutNoNulPadding(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.log")

	m := New(WithoutHistory())
	m.outFile = outPath

	origStdout, origStderr := os.Stdout, os.Stderr
	if err := m.redirectStdout(); err != nil {
		os.Stdout, os.Stderr = origStdout, origStderr
		t.Fatalf("redirectStdout: %v", err)
	}
	writer := os.Stdout
	fmt.Fprint(os.Stdout, "hi")
	// Closing the pipe writer flushes the write and signals EOF, so the reader
	// goroutine drains, closes the channels, and the file goroutine closes the file.
	writer.Close()
	os.Stdout, os.Stderr = origStdout, origStderr

	var data []byte
	for i := 0; i < 250; i++ {
		data, _ = os.ReadFile(outPath)
		if len(data) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	if string(data) != "hi" {
		t.Fatalf("out file = %q (len %d), want exactly %q", data, len(data), "hi")
	}
	for _, b := range data {
		if b == 0 {
			t.Fatalf("out file contains NUL padding: %q", data)
		}
	}
}

// With WithOutFile, bubbletea must be handed an output that still reports the
// console's fd (v2 reads terminal size/resize/colors from it) while writing
// into the stdout pipe, so TUI frames stay ordered with handler output.
func TestConsoleTeeReportsConsoleFdAndWritesToPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	console, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer console.Close()

	c := &consoleTee{console: console, w: w}
	if c.Fd() != console.Fd() {
		t.Fatalf("Fd = %d, want console fd %d", c.Fd(), console.Fd())
	}
	if _, err := c.Write([]byte("帧")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, _ := r.Read(buf)
	if string(buf[:n]) != "帧" {
		t.Fatalf("pipe got %q, want 帧", buf[:n])
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Stat(); err != nil {
		t.Fatalf("Close must not close the console: %v", err)
	}
}

// restoreStdout (run by Prompt.Run after the TUI stops) must hand stdout and
// stderr back and synchronously flush everything already written, so the
// final bytes — e.g. bubbletea's terminal-restore sequences — aren't lost
// when the process exits right after Run returns.
func TestRestoreStdoutFlushesSynchronously(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "out.log")
	m := New(WithoutHistory())
	m.outFile = outPath

	origStdout, origStderr := os.Stdout, os.Stderr
	if err := m.redirectStdout(); err != nil {
		os.Stdout, os.Stderr = origStdout, origStderr
		t.Fatalf("redirectStdout: %v", err)
	}
	fmt.Fprint(os.Stdout, "最后的输出\x1b[?25h")
	m.restoreStdout()

	if os.Stdout != origStdout || os.Stderr != origStderr {
		os.Stdout, os.Stderr = origStdout, origStderr
		t.Fatal("restoreStdout did not restore os.Stdout/os.Stderr")
	}
	// no polling: the data must already be on disk when restoreStdout returns
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "最后的输出\x1b[?25h" {
		t.Fatalf("out file = %q, want all bytes flushed", data)
	}
}
