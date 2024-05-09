package prompt

import (
	"bytes"
	"fmt"
	"io"
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

func WithSetTermValue() PromptModelOption {
	return func(pm *PromptModel) {
		os.Setenv("TERM", "xterm-256color")
	}
}

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
	out := io.MultiWriter(consoleOut, outputFile)

	reader, writer, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = writer
	os.Stderr = writer

	consoleChan := make(chan []byte)
	outFileChan := make(chan []byte)

	go func() {
		defer consoleOut.Close()
		for {
			consoleOut.Write(<-consoleChan)
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
		for {
			buffer := make([]byte, 1024)
			_, err := reader.Read(buffer[:])
			if err != nil {
				if err != io.EOF {
					out.Write([]byte("read out put fail, err: " + err.Error()))
				}
				// no output, wait 1ms
				time.Sleep(time.Millisecond)
				continue
			}
			consoleChan <- buffer
			outFileChan <- buffer
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
		return err
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
	go func(ch <-chan string) {
		outputFile, err := os.OpenFile(m.historyFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			fmt.Printf("open out file[%s] fail, err: %v\n", m.outFile, err)
			return
		}
		defer outputFile.Close()
		m.readyToSaveHistory = true
		defer func() {
			m.readyToSaveHistory = false
		}()
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
