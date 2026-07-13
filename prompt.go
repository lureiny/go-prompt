package prompt

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PromptModelInitFunc func(*PromptModel) error

type PromptModel struct {
	prefix          string
	handlerInfos    map[string]*HandlerInfo
	defaultCallback HandlerCallback
	historys        []string
	historyBuffers  []string
	historyIndex    int

	historyBufferPos int
	// historyOuts    []string // save history out info

	printCmd       bool
	ignoreEmptyCmd bool

	textInput textinput.Model

	matchSuggests []Suggest
	suggestIndex  int
	suggestNum    int

	termHeight int // last known terminal height, 0 until a WindowSizeMsg arrives

	initCmds       []tea.Cmd
	programOptions []tea.ProgramOption

	initFuncs []PromptModelInitFunc

	forceStyle lipgloss.Style
	baseStyle  lipgloss.Style

	runCmdMark  bool
	runCmdDeply int64 // ms

	outFile     string
	filterAscii bool // filter invisible characters in ascii

	exit bool

	withHelpMsg bool

	printRunTime bool

	// history
	saveHistory        bool
	historyFile        string
	historyChan        chan string
	readyToSaveHistory atomic.Bool
	historyDone        chan struct{} // closed when the history-writer goroutine exits

	mutex sync.Mutex
}

type Prompt struct {
	*PromptModel
}

func NewPrompt(opts ...PromptModelOption) *Prompt {
	p := &Prompt{
		NewPromptModel(opts...),
	}
	return p
}

func NewPromptModel(opts ...PromptModelOption) *PromptModel {
	model := &PromptModel{
		handlerInfos:   map[string]*HandlerInfo{},
		prefix:         defaultPrefix,
		historyBuffers: make([]string, 1),
		suggestIndex:   -1,
		suggestNum:     defaultSuggestNum,
		forceStyle:     defaultForceStyle,
		baseStyle:      defaultBaseStyle,

		runCmdDeply: defaultRunCmdDeply,
		printCmd:    defaultPrintCmd,

		withHelpMsg:  true,
		printRunTime: true,

		saveHistory: true,
		historyFile: defaultHistoryFile,
		historyChan: make(chan string, 1000),

		initFuncs: []PromptModelInitFunc{initTextModel, loadHistory, startSaveHistory},
	}
	for _, opt := range opts {
		opt(model)
	}
	model.init()
	return model
}

func initTextModel(m *PromptModel) error {
	m.textInput = textinput.New()
	m.textInput.Focus()
	m.textInput.Prompt = m.prefix
	return nil
}

func (m *PromptModel) init() {
	for _, initFunc := range m.initFuncs {
		if err := initFunc(m); err != nil {
			panic(err)
		}
	}
}

// RegisterHandler registers handler under name. Invalid configuration (nil or
// non-func handler, mismatched suggest count, unsupported param type, duplicate
// name, invalid suggest text) is a setup-time programmer error and panics, like
// regexp.MustCompile. Register all handlers before calling Run.
func (m *PromptModel) RegisterHandler(handler Handler, name string, opts ...HandlerInfoOption) {
	handlerInfos := NewHandlerInfo(name, handler, opts...)
	m.RegisterHandlerInfos(handlerInfos)
}

func (m *PromptModel) RegisterHandlerInfos(handlerInfos ...*HandlerInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, handlerInfo := range handlerInfos {
		if err := handlerInfo.CheckAndInitHandler(); err != nil {
			panic(err)
		}
		if handlerInfo.Callback == nil {
			handlerInfo.Callback = m.defaultCallback
		}
		if _, ok := m.handlerInfos[handlerInfo.Name]; ok {
			panic(fmt.Errorf("handler[%s] has been registered", handlerInfo.Name))
		}
		m.handlerInfos[handlerInfo.Name] = handlerInfo
	}
}

func (m *PromptModel) runCmd(cmd string) tea.Cmd {
	if len(strings.ReplaceAll(cmd, " ", "")) == 0 {
		return nil
	}
	cmdWithTime := fmt.Sprintf("%s: %s", time.Now().Local().Format(timeFormat), cmd)
	if m.readyToSaveHistory.Load() {
		m.historyChan <- cmdWithTime + "\n"
	}

	handlerName := strings.Split(cmd, " ")[0]
	handler, ok := m.handlerInfos[handlerName]
	if !ok {
		return tea.Printf("can't find handler[%s]", handlerName)
	}
	if err := handler.Run(cmd); err != nil {
		return tea.Printf("run cmd of handler[%s] fail, err: %v", handlerName, err)
	}
	if handler.ExitAfterRun {
		m.exit = true
		return tea.Quit
	}
	return nil
}

func (m *PromptModel) Init() tea.Cmd {
	return tea.Batch(m.initCmds...)
}

func (m *PromptModel) getCurrentCmdString() string {
	// Split only on the ASCII space, matching the suggestion and flag-parse paths
	// (which use strings.Split on " "). strings.Fields would additionally split on
	// Unicode whitespace such as the full-width space U+3000 that CJK IMEs emit in
	// full-width mode, silently rewriting it to an ASCII space and corrupting the
	// argument/flag value and the saved history. Treat U+3000 as a normal
	// character (like a shell does), so tokenization is consistent everywhere.
	fields := strings.FieldsFunc(m.historyBuffers[m.historyIndex], func(r rune) bool {
		return r == ' '
	})
	return strings.Join(fields, " ")
}

// syncBufferPos records the cursor position as a byte offset into the current
// input buffer. bubbles reports the cursor as a rune index, but the suggestion
// and completion logic slices the buffer by byte, so we convert here.
func (m *PromptModel) syncBufferPos() {
	m.historyBufferPos = runeIndexToByteOffset(m.textInput.Value(), m.textInput.Position())
}

func (m *PromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd = nil
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "ctrl+d":
			m.exit = true
			return m, tea.Quit
		case "ctrl+c":
			m.historyBuffers[m.historyIndex] = ""
			m.textInput.SetValue("")
			m.syncBufferPos()
			return m, nil
		case "ctrl+l":
			return m, tea.ClearScreen
		case "enter":
			m.historyBuffers[m.historyIndex] = m.textInput.Value()
			cmdString := m.getCurrentCmdString()
			if len(strings.ReplaceAll(cmdString, " ", "")) > 0 &&
				(len(m.historys) == 0 || cmdString != m.historys[len(m.historys)-1]) {
				m.historys = append(m.historys, cmdString)
			}
			m.historyIndex = len(m.historys)
			m.historyBuffers = make([]string, len(m.historys)+1)
			copy(m.historyBuffers, m.historys)
			if m.printCmd {
				// 覆盖刷新
				fmt.Println(m.prefix + cmdString)
			}
			// reset text input
			m.textInput.SetValue(m.historyBuffers[m.historyIndex])
			m.textInput.SetCursor(utf8.RuneCountInString(m.historyBuffers[m.historyIndex]))
			m.syncBufferPos()
			m.suggestIndex = -1
			m.runCmdMark = true
			return m, runCmd(cmdString)
		case "up", "down", "ctrl+p", "ctrl+n":
			switch msg.String() {
			case "up", "ctrl+p":
				m.historyIndex = max(0, m.historyIndex-1)
			case "down", "ctrl+n":
				m.historyIndex = min(m.historyIndex+1, len(m.historys))
			}
			m.textInput.SetValue(m.historyBuffers[m.historyIndex])
			m.textInput.SetCursor(utf8.RuneCountInString(m.historyBuffers[m.historyIndex]))
			m.syncBufferPos()
			m.suggestIndex = -1
			return m, nil
		case "tab", "shift+tab":
			if len(m.matchSuggests) == 0 {
				return m, nil
			}
			switch keypress {
			case "tab":
				m.suggestIndex = (m.suggestIndex + 1) % len(m.matchSuggests)
			case "shift+tab":
				if m.suggestIndex <= 0 {
					m.suggestIndex = len(m.matchSuggests) - 1
				} else {
					m.suggestIndex = (m.suggestIndex - 1) % len(m.matchSuggests)
				}
			}
			// Show the completed command, but keep historyBuffers[historyIndex]
			// and historyBufferPos anchored to the user's typed prefix. That way
			// updateSuggentList keeps regenerating the SAME candidate list from the
			// prefix, so repeated tab cycles through all matches instead of
			// collapsing onto the first one. The enter/default handlers re-sync the
			// buffer from the shown value when the user commits or keeps typing.
			newCmd, newPos := replaceScope(m.historyBuffers[m.historyIndex],
				m.matchSuggests[m.suggestIndex].Text, m.historyBufferPos)
			m.textInput.SetValue(newCmd)
			m.textInput.SetCursor(byteOffsetToRuneIndex(newCmd, newPos))
			return m, nil
		default:
			// input any key, need make sure text input buffer and current buffer is same
			// reset suggest index
			m.suggestIndex = -1
			m.textInput, cmd = m.textInput.Update(msg)
			m.historyBuffers[m.historyIndex] = m.textInput.Value()
			m.syncBufferPos()
			return m, cmd
		}
	case RunCmdMsg:
		cmdWithTime := fmt.Sprintf("%s: %s", time.Now().Local().Format(timeFormat), msg.cmd)
		if !m.printCmd && m.printRunTime {
			fmt.Println(cmdWithTime)
		}
		time.Sleep(time.Duration(m.runCmdDeply * int64(time.Millisecond)))
		cmd := m.runCmd(msg.cmd)
		m.runCmdMark = false
		return m, cmd
	case tea.WindowSizeMsg:
		// use display width (cells), not byte length, so a multibyte prefix sizes correctly
		m.textInput.Width = msg.Width - lipgloss.Width(m.prefix) - 1 // 防止显示不全。 -1是为了显示force光标
		m.termHeight = msg.Height
		return m, nil
	}
	return m, nil
}

func (m *PromptModel) View() string {
	if m.runCmdMark || m.exit {
		return m.prefix
	}
	m.updateSuggentList()
	s := m.textInput.View()
	if m.SuggestView() != "" {
		s += "\n" + m.SuggestView()
	}
	if m.withHelpMsg {
		s += "\n" + HelpView()
	}
	return s
}

func (m *PromptModel) SuggestView() string {
	suggestViews := []string{}
	start, end := m.getSuggestScope()
	forceSuggestIndex := -1
	width := -1
	for index := start; index < end; index++ {
		if index == m.suggestIndex {
			forceSuggestIndex = index - start
		}
		suggestView := getSuggestView(m.matchSuggests[index])
		suggestViews = append(suggestViews, suggestView)
		// display width (cells), so CJK suggestions are not over-padded
		width = max(lipgloss.Width(suggestView), width)
	}
	m.forceStyle = m.forceStyle.Width(width)
	m.baseStyle = m.baseStyle.Width(width)
	for index, suggestView := range suggestViews {
		if index == forceSuggestIndex {
			suggestViews[index] = m.forceStyle.Render(suggestView)
		} else {
			suggestViews[index] = m.baseStyle.Render(suggestView)
		}
	}
	return strings.Join(suggestViews, "\n")
}

func getSuggestView(s Suggest) string {
	if s.SuggestType == SuggestOfHandler {
		if s.Description != "" {
			return fmt.Sprintf("%s: %s", s.Text, s.Description)
		}
		return s.Text
	}
	return fmt.Sprintf("%s, default: %v, description: %s", s.Text, s.Default, s.Description)
}

func (m *Prompt) Run() error {
	_, err := tea.NewProgram(m, m.programOptions...).Run()
	// The bubbletea loop has stopped, so no more history entries will be sent.
	// Close the channel and wait for the writer to drain any buffered entries
	// before returning, so commands typed right before exit are not lost.
	m.closeHistory()
	return err
}

// closeHistory closes the history channel and waits for the writer goroutine to
// flush remaining entries. It is a no-op when history saving is disabled.
func (m *PromptModel) closeHistory() {
	if m.historyDone == nil {
		return
	}
	close(m.historyChan)
	<-m.historyDone
	m.historyDone = nil
}

func replaceScope(cmdString, newString string, pos int) (string, int) {
	subIndex := 0
	for index := 0; index < pos; index++ {
		if cmdString[index] == ' ' {
			subIndex++
		}
	}
	cmds := strings.Split(cmdString, " ")
	cmds[subIndex] = newString
	newPos := 0
	for index := 0; index < subIndex; index++ {
		newPos += len(cmds[index]) + 1
	}
	newPos += len(cmds[subIndex])
	return strings.Join(cmds, " "), newPos
}

// effectiveSuggestNum caps the number of visible suggestions so the input line,
// suggestions, and help line together fit within the terminal height. It returns
// suggestNum unchanged until a WindowSizeMsg has reported the height.
func (m *PromptModel) effectiveSuggestNum() int {
	if m.termHeight <= 0 {
		return m.suggestNum
	}
	avail := m.termHeight - 1 // input line
	if m.withHelpMsg {
		avail-- // help line
	}
	if avail < 1 {
		// degenerate tiny terminal: still show one suggestion
		avail = 1
	}
	return min(m.suggestNum, avail)
}

// getSuggestScope >= start; < end
func (m *PromptModel) getSuggestScope() (start, end int) {
	suggestNum := m.effectiveSuggestNum()
	start = m.suggestIndex
	for ; start < len(m.matchSuggests); start++ {
		if start >= 0 && start < len(m.matchSuggests) {
			break
		}
	}
	if start+suggestNum > len(m.matchSuggests)-1 {
		start = max(len(m.matchSuggests)-suggestNum, 0)
	}

	end = min(start+suggestNum, len(m.matchSuggests))
	return
}

// SetHandlers replaces the whole handler set. It validates each handler like
// registration does. Call it during setup, before Run: the bubbletea loop reads
// the handler map without locking, so replacing it while the program is running
// would race those reads.
func (m *PromptModel) SetHandlers(handlers map[string]*HandlerInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	// Validate and initialize each handler the same way registration does, so
	// SetHandlers can't install handlers that would panic at run time.
	for _, handlerInfo := range handlers {
		if err := handlerInfo.CheckAndInitHandler(); err != nil {
			panic(err)
		}
		if handlerInfo.Callback == nil {
			handlerInfo.Callback = m.defaultCallback
		}
	}
	m.handlerInfos = handlers
}

func (m *PromptModel) updateSuggentList() {
	buf := m.historyBuffers[m.historyIndex]
	// defensive clamp: never slice past the buffer even if position desyncs
	pos := m.historyBufferPos
	if pos > len(buf) {
		pos = len(buf)
	}
	cmd := buf[:pos]
	if m.ignoreEmptyCmd && cmd == "" {
		m.matchSuggests = make([]Suggest, 0)
		return
	}
	cmds := strings.Split(cmd, " ")
	if len(cmds) == 0 {
		cmds = append(cmds, "")
	}
	handler, ok := m.handlerInfos[cmds[0]]
	if !ok || len(cmds) == 1 {
		m.genHandlerSuggests(cmds[0])
	} else if !handler.UseFlagSet {
		// WithoutFlagSet handlers take a raw argument string, not flags, so
		// offering "-flag" param completions for them is meaningless.
		m.matchSuggests = make([]Suggest, 0)
		m.suggestIndex = -1
	} else {
		var matchSuggests []Suggest
		var err error
		var getHandlerSuggests GetSuggestFunc = DefaultGetHandlerSuggests
		if handler.GetSuggestMethod != nil {
			getHandlerSuggests = handler.GetSuggestMethod
		}
		matchSuggests, err = getHandlerSuggests(handler, buf[:pos])
		if err != nil || matchSuggests == nil {
			m.matchSuggests = make([]Suggest, 0)
			m.suggestIndex = -1
			return
		}
		m.matchSuggests = matchSuggests
	}

	m.matchSuggests = SortSuggest(m.matchSuggests)
}

func (m *PromptModel) genHandlerSuggests(input string) {
	m.matchSuggests = make([]Suggest, 0)
	for handlerName, h := range m.handlerInfos {
		if IsMatch(input, handlerName) {
			m.matchSuggests = append(m.matchSuggests, Suggest{
				Text:        handlerName,
				SuggestType: SuggestOfHandler,
				Description: h.HelpMsg,
			})
		}
	}
}

type RunCmdMsg struct {
	cmd string
}

func runCmd(cmd string) tea.Cmd {
	return func() tea.Msg {
		return RunCmdMsg{
			cmd: cmd,
		}
	}
}
