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

	m := NewPromptModel(WithOutSaveHistory())
	m.outFile = outPath

	origStdout, origStderr := os.Stdout, os.Stderr
	if err := redirectStdout(m); err != nil {
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
