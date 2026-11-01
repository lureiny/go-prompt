package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// An Option configures a Prompt; pass options to New.
type Option func(*Prompt)

// WithPrefix sets the input prompt (default ">>> ").
func WithPrefix(prefix string) Option {
	return func(p *Prompt) { p.prefix = prefix }
}

// WithMaxSuggestions sets how many suggestions are shown at once (default 3).
func WithMaxSuggestions(n int) Option {
	return func(p *Prompt) { p.maxSuggestions = max(n, 1) }
}

// WithSuggestOnEmpty controls whether the command list is shown while the
// input line is empty (default true).
func WithSuggestOnEmpty(show bool) Option {
	return func(p *Prompt) { p.suggestOnEmpty = show }
}

// WithHelpLine controls the key-hint line under the input (default true).
func WithHelpLine(show bool) Option {
	return func(p *Prompt) { p.helpLine = show }
}

// WithEcho controls whether a submitted line is printed (as prefix + line)
// before its command runs (default true).
func WithEcho(echo bool) Option {
	return func(p *Prompt) { p.echo = echo }
}

// WithEchoTime adds a timestamp to the echoed line (default false).
func WithEchoTime(show bool) Option {
	return func(p *Prompt) { p.echoTime = show }
}

// Styles are the colors of the suggestion list and the help line.
type Styles struct {
	Suggestion lipgloss.Style // an unselected suggestion
	Selected   lipgloss.Style // the suggestion picked with tab
	HelpLine   lipgloss.Style
}

// DefaultStyles returns the default Styles.
func DefaultStyles() Styles {
	base := lipgloss.NewStyle().Bold(true).Faint(true).Reverse(true).
		Background(lipgloss.Color("#00B3FF")).Foreground(lipgloss.Color("#00B3FF"))
	return Styles{
		Suggestion: base,
		Selected:   base.Underline(true).Italic(true).Background(lipgloss.Color("#9EA9AE")),
		HelpLine:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
	}
}

// WithStyles sets the styles; start from DefaultStyles to change only some.
func WithStyles(s Styles) Option {
	return func(p *Prompt) { p.styles = s }
}

// WithResultHandler sets what happens with command functions' return values
// (except a final error). The default prints them on one line; nil discards
// them. A command's OnResult option takes precedence.
func WithResultHandler(f func(results []any)) Option {
	return func(p *Prompt) { p.resultHandler = f }
}

// WithoutBuiltinCommands removes the built-in "help" and "exit" commands.
// (A command you register with the same name replaces a built-in anyway.)
func WithoutBuiltinCommands() Option {
	return func(p *Prompt) { p.builtins = map[string]*command{} }
}

// WithLogFile tees everything written to stdout and stderr while the prompt
// runs — command output and the prompt itself — to the file at path
// (appending). Output stays correctly ordered on the terminal.
func WithLogFile(path string) Option {
	return func(p *Prompt) { p.outFile = path }
}

// WithPlainLog strips colors and other terminal escape sequences from the log
// file written by WithLogFile.
func WithPlainLog() Option {
	return func(p *Prompt) { p.plainLog = true }
}

// WithHistoryFile sets where command history is kept. By default it is
// <user config dir>/<app name>/history, e.g. ~/.config/mytool/history.
func WithHistoryFile(path string) Option {
	return func(p *Prompt) { p.historyFile, p.saveHistory = path, true }
}

// WithAppName sets the directory name used for the default history file
// (default: the executable's name).
func WithAppName(name string) Option {
	return func(p *Prompt) { p.appName = name }
}

// WithoutHistory disables loading and saving command history.
func WithoutHistory() Option {
	return func(p *Prompt) { p.saveHistory = false }
}

// WithProgramOptions adds Bubble Tea program options. Repeated uses
// accumulate.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(p *Prompt) { p.programOptions = append(p.programOptions, opts...) }
}

// WithInitCmds adds Bubble Tea commands to run at startup. Repeated uses
// accumulate.
func WithInitCmds(cmds ...tea.Cmd) Option {
	return func(p *Prompt) { p.initCmds = append(p.initCmds, cmds...) }
}

// defaultHistoryPath is <user config dir>/<app>/history, or "" if there is
// no config dir.
func defaultHistoryPath(app string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	if app == "" {
		exe := filepath.Base(os.Args[0])
		app = strings.TrimSuffix(exe, filepath.Ext(exe))
	}
	if app == "" || app == "." {
		app = "go-prompt"
	}
	return filepath.Join(dir, app, "history")
}

// loadHistory reads the history file. History is best-effort: a missing or
// unreadable file starts an empty history.
func (m *Prompt) loadHistory() {
	if m.historyFile == "" {
		return
	}
	data, err := os.ReadFile(m.historyFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "prompt: can't read history %s: %v\n", m.historyFile, err)
		}
		return
	}
	m.historys = nil
	for _, h := range strings.Split(string(data), "\n") {
		h = strings.TrimRight(h, "\r") // tolerate CRLF files
		// format: "<timeFormat>: <cmd>"
		if len(h) <= len(timeFormat)+2 {
			continue
		}
		if _, err := time.Parse(timeFormat, h[:len(timeFormat)]); err != nil {
			continue
		}
		m.historys = append(m.historys, h[len(timeFormat)+2:])
	}
	m.historyBuffers = make([]string, len(m.historys)+1)
	copy(m.historyBuffers, m.historys)
	m.historyIndex = len(m.historys)
}

// startSaveHistory starts the goroutine appending entries to the history
// file; closeHistory stops it.
func (m *Prompt) startSaveHistory() {
	if m.historyFile == "" {
		return
	}
	m.historyDone = make(chan struct{})
	go func(ch <-chan string) {
		defer close(m.historyDone) // lets closeHistory wait for the final flush
		_ = os.MkdirAll(filepath.Dir(m.historyFile), 0o755)
		f, err := os.OpenFile(m.historyFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "prompt: can't open history %s: %v\n", m.historyFile, err)
			for range ch { // drain so no sender blocks
			}
			return
		}
		defer f.Close()
		m.readyToSaveHistory.Store(true)
		defer m.readyToSaveHistory.Store(false)
		for line := range ch {
			f.WriteString(line)
		}
	}(m.historyChan)
}

func (m *Prompt) redirectStdout() error {
	outputFile, err := os.OpenFile(m.outFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	consoleOut := os.Stdout
	consoleErr := os.Stderr

	reader, writer, err := os.Pipe()
	if err != nil {
		outputFile.Close()
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	os.Stdout = writer
	os.Stderr = writer
	// bubbletea v2 reads the terminal size, resize events and color profile
	// from its output's file descriptor. Give it one that reports the real
	// console fd but writes into the same pipe, so the TUI keeps working and
	// its frames stay ordered with (and teed alongside) command output.
	m.programOutput = &consoleTee{console: consoleOut, w: writer}

	consoleChan := make(chan []byte)
	outFileChan := make(chan []byte)
	var drained sync.WaitGroup // both consumers finished writing
	drained.Add(2)

	// restoreStdout runs once the bubbletea loop has stopped: it hands stdout
	// and stderr back to the console, closes the pipe and waits until every
	// byte already written has reached the console and the log. Without this,
	// returning from Run (and exiting the process) could drop the tail of the
	// output — notably bubbletea's terminal-restore sequences, leaving the
	// cursor hidden and keyboard enhancements enabled in the user's shell.
	m.restoreStdout = func() {
		os.Stdout = consoleOut
		os.Stderr = consoleErr
		writer.Close()
		drained.Wait()
	}

	go func() {
		defer drained.Done()
		// Do NOT close consoleOut: it is the process's original os.Stdout, which
		// the library did not open and must not close (doing so would break any
		// later writes to fd 1 after the prompt shuts down).
		for buffer := range consoleChan {
			consoleOut.Write(buffer)
		}
	}()

	go func() {
		defer drained.Done()
		defer outputFile.Close()
		for buffer := range outFileChan {
			if m.plainLog {
				buffer = plainText(buffer)
			}
			outputFile.Write(buffer)
		}
	}()

	go func() {
		defer reader.Close()
		defer close(consoleChan)
		defer close(outFileChan)
		buf := make([]byte, 1024)
		var carry []byte // trailing bytes of an incomplete UTF-8 rune held back
		send := func(b []byte) {
			if len(b) == 0 {
				return
			}
			// Copy because buf is reused next iteration and both consumers read
			// the slice concurrently.
			chunk := make([]byte, len(b))
			copy(chunk, b)
			consoleChan <- chunk
			outFileChan <- chunk
		}
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				// Forward only the bytes actually read (not the whole 1024-byte
				// buffer, which would append NUL padding), and split on a rune
				// boundary so a multibyte (e.g. CJK) rune straddling two reads is
				// not decoded as two halves — otherwise filterAscii's per-chunk
				// bytes.Map would rewrite each half as U+FFFD.
				data := make([]byte, 0, len(carry)+n)
				data = append(data, carry...)
				data = append(data, buf[:n]...)
				keep := completeUTF8Len(data)
				send(data[:keep])
				carry = append([]byte(nil), data[keep:]...)
			}
			if err != nil {
				// EOF or a closed pipe on shutdown: flush any trailing bytes and
				// stop instead of busy-looping.
				send(carry)
				return
			}
		}
	}()
	return nil
}

// consoleTee is the bubbletea output used with WithLogFile: Fd (and Read)
// refer to the real console so bubbletea can treat it as a TTY, while Write
// goes to the stdout pipe. Close is a no-op; the console is not ours to close.
type consoleTee struct {
	console *os.File
	w       *os.File
}

func (c *consoleTee) Write(b []byte) (int, error) { return c.w.Write(b) }
func (c *consoleTee) Read(b []byte) (int, error)  { return c.console.Read(b) }
func (c *consoleTee) Close() error                { return nil }
func (c *consoleTee) Fd() uintptr                 { return c.console.Fd() }

// plainText strips ANSI escape sequences and other non-printable characters
// (best effort: a sequence split across two pipe reads may leave a fragment).
func plainText(b []byte) []byte {
	return bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || unicode.IsPrint(r) {
			return r
		}
		return -1
	}, []byte(ansi.Strip(string(b))))
}
