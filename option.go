package prompt

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// -----------------------------------------------------------------------------------------------------------------
// -                                         PromptModelOption                                                     -
// -----------------------------------------------------------------------------------------------------------------

type PromptModelOption func(*PromptModel)

func WithPromptPrefixOption(prefix string) PromptModelOption {
	return func(p *PromptModel) {
		p.prefix = prefix
	}
}

func WithPrintCmd() PromptModelOption {
	return func(pm *PromptModel) {
		pm.printCmd = true
	}
}

func WithoutPrintCmd() PromptModelOption {
	return func(pm *PromptModel) {
		pm.printCmd = false
	}
}

func WithSuggestNum(num int) PromptModelOption {
	return func(pm *PromptModel) {
		pm.suggestNum = num
	}
}

func WithInitCmds(initCmds ...tea.Cmd) PromptModelOption {
	return func(pm *PromptModel) {
		pm.initCmds = initCmds
	}
}

func WithProgramOptions(programOptions ...tea.ProgramOption) PromptModelOption {
	return func(pm *PromptModel) {
		pm.programOptions = programOptions
	}
}

func WithForceStyle(style lipgloss.Style) PromptModelOption {
	return func(pm *PromptModel) {
		pm.forceStyle = style
	}
}

func WithBaseStyle(style lipgloss.Style) PromptModelOption {
	return func(pm *PromptModel) {
		pm.baseStyle = style
	}
}

func WithIgnoreEmptyCmd(ignore bool) PromptModelOption {
	return func(pm *PromptModel) {
		pm.ignoreEmptyCmd = ignore
	}
}

func WithDefaultHandlerCallback(callback HandlerCallback) PromptModelOption {
	return func(pm *PromptModel) {
		pm.defaultCallback = callback
	}
}

// WithSetTermValue sets the process TERM environment variable to
// "xterm-256color" so lipgloss/termenv detect a 256-color profile. Note this
// mutates the whole process environment, not just this prompt.
func WithSetTermValue() PromptModelOption {
	return func(pm *PromptModel) {
		os.Setenv("TERM", "xterm-256color")
	}
}

// WithOutFile tees stdout/stderr to outFile (the "Out" here means output, unlike
// the WithOut*/Without* options which negate a feature).
func WithOutFile(outFile string) PromptModelOption {
	return func(pm *PromptModel) {
		pm.outFile = outFile
		pm.initFuncs = append(pm.initFuncs, redirectStdout)
	}
}

// WithFilterAscii filter invisible characters in ascii
func WithFilterAscii() PromptModelOption {
	return func(pm *PromptModel) {
		pm.filterAscii = true
	}
}

func redirectStdout(m *PromptModel) error {
	outputFile, err := os.OpenFile(m.outFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("open out file[%s] fail, err: %v", m.outFile, err)
	}

	consoleOut := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe fail, err: %v", err)
	}
	os.Stdout = writer
	os.Stderr = writer

	consoleChan := make(chan []byte)
	outFileChan := make(chan []byte)

	go func() {
		// Do NOT close consoleOut: it is the process's original os.Stdout, which
		// the library did not open and must not close (doing so would break any
		// later writes to fd 1 after the prompt shuts down).
		for buffer := range consoleChan {
			consoleOut.Write(buffer)
		}
	}()

	go func() {
		defer outputFile.Close()
		for buffer := range outFileChan {
			if m.filterAscii {
				buffer = filterAscii(buffer)
			}
			outputFile.Write(buffer)
		}
	}()

	go func() {
		defer reader.Close()
		defer close(consoleChan)
		defer close(outFileChan)
		buf := make([]byte, 1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				// Forward only the bytes actually read (not the whole 1024-byte
				// buffer, which would append NUL padding). Copy because buf is
				// reused on the next iteration and both consumers read concurrently.
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				consoleChan <- chunk
				outFileChan <- chunk
			}
			if err != nil {
				// EOF or a closed pipe on shutdown: stop instead of busy-looping.
				return
			}
		}
	}()
	return nil
}

func filterAscii(buffer []byte) []byte {
	return bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == '\r' || r == '\b' || unicode.IsPrint(r) {
			return r
		}
		return -1
	}, buffer)
}

func WithHelpMsg() PromptModelOption {
	return func(pm *PromptModel) {
		pm.withHelpMsg = true
	}
}

func WithOutHelpMsg() PromptModelOption {
	return func(pm *PromptModel) {
		pm.withHelpMsg = false
	}
}

func WithOutPrintRunTime() PromptModelOption {
	return func(pm *PromptModel) {
		pm.printRunTime = false
	}
}

func WithHistoryFile(file string) PromptModelOption {
	return func(pm *PromptModel) {
		if file == "" {
			file = defaultHistoryFile
		}
		pm.historyFile = file
	}
}

func WithOutSaveHistory() PromptModelOption {
	return func(pm *PromptModel) {
		pm.saveHistory = false
	}
}

func WithSaveHistory() PromptModelOption {
	return func(pm *PromptModel) {
		pm.saveHistory = true
	}
}

func loadHistory(m *PromptModel) error {
	data, err := os.ReadFile(m.historyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// History is best-effort: a corrupt or unreadable file must not crash
		// construction. Warn and start with empty history instead.
		fmt.Printf("load history file[%s] fail, err: %v\n", m.historyFile, err)
		return nil
	}
	m.historyBuffers = make([]string, 0) // 清空历史记录
	historys := strings.Split(string(data), "\n")
	// history format: "{time like timeFormat}: {cmd}"
	spaceLen := 2
	for _, h := range historys {
		if len(h) <= len(timeFormat)+spaceLen {
			continue
		}

		if _, err := time.Parse(timeFormat, h[:len(timeFormat)]); err != nil {
			continue
		}
		m.historyBuffers = append(m.historyBuffers, h[len(timeFormat)+spaceLen:])
		m.historys = append(m.historys, h[len(timeFormat)+spaceLen:])
	}

	m.historyBuffers = append(m.historyBuffers, "")
	m.historyIndex = len(m.historyBuffers) - 1
	return nil
}

func startSaveHistory(m *PromptModel) error {
	if !m.saveHistory || m.historyFile == "" {
		return nil
	}
	m.historyDone = make(chan struct{})
	go func(ch <-chan string) {
		// Signal completion so closeHistory can wait for the final flush.
		defer close(m.historyDone)
		outputFile, err := os.OpenFile(m.historyFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			fmt.Printf("open history file[%s] fail, err: %v\n", m.historyFile, err)
			// Drain until the channel is closed so no sender ever blocks.
			for range ch {
			}
			return
		}
		defer outputFile.Close()
		m.readyToSaveHistory.Store(true)
		defer m.readyToSaveHistory.Store(false)
		for cmd := range ch {
			outputFile.Write([]byte(cmd))
		}
	}(m.historyChan)
	return nil
}

// -----------------------------------------------------------------------------------------------------------------
// -                                        HandlerInfoOption                                                     -
// -----------------------------------------------------------------------------------------------------------------

type HandlerInfoOption func(h *HandlerInfo)

func WithCallback(callback HandlerCallback) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.Callback = callback
	}
}

func WithSuggests(suggests []Suggest) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.Suggests = suggests
	}
}

func WithSuggestPrefix(prefix string) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.SuggestPrefix = prefix
	}
}

func WithoutFlagSet() HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.UseFlagSet = false
	}
}

func WithGetSuggestMethod(f GetSuggestFunc) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.GetSuggestMethod = f
	}
}

func WithHandlerHelpMsg(helpMsg string) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.HelpMsg = helpMsg
	}
}

func WithExitAfterRun(isExitAfterRun bool) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.ExitAfterRun = isExitAfterRun
	}
}

func WithFlagSetInitFunc(f FlagSetInitFunc) HandlerInfoOption {
	return func(h *HandlerInfo) {
		h.FlagSetInitFuncImpl = f
	}
}
