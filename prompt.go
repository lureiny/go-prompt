// Package prompt builds interactive command-line REPLs on Bubble Tea.
//
//	p := prompt.New(prompt.WithPrefix(">>> "))
//	p.Command("calc", "add two numbers", func(a, b int) int { return a + b },
//		prompt.Flag("a", "first number"), prompt.Flag("b", "second number"))
//	if err := p.Run(); err != nil { ... }
//
// Typing "calc -a 1 -b 2" prints 3. Commands get tab completion for their
// names and flags, "-h" usage, history (↑/↓, saved across runs), and "help"
// and "exit" built-ins.
package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	timeFormat = "2006-01-02 15:04:05.000" // history line prefix: "<time>: <cmd>"

	defaultPrefix         = ">>> "
	defaultMaxSuggestions = 3
	// runDelay lets the blank "running" frame render before a command's
	// output starts.
	runDelay = 20 * time.Millisecond

	helpLineText = "tab/shift+tab: complete · ↑↓: history · ctrl+c: clear/interrupt · ctrl+d: exit"
)

// Prompt is an interactive command prompt. Create it with New, add commands
// with Command and RawCommand, then call Run. It implements tea.Model.
type Prompt struct {
	prefix   string
	commands map[string]*command
	builtins map[string]*command // help, exit; user commands take precedence

	historys       []string // committed command lines, oldest first
	historyBuffers []string // per-entry edit buffers; len(historys)+1, last is the live line
	historyIndex   int
	// cursor position as a byte offset into historyBuffers[historyIndex]
	historyBufferPos int

	textInput textinput.Model

	matchSuggests  []Suggestion
	suggestIndex   int
	maxSuggestions int
	suggestOnEmpty bool

	termHeight int // last known terminal height, 0 until a WindowSizeMsg arrives

	initCmds       []tea.Cmd
	programOptions []tea.ProgramOption

	styles   Styles
	helpLine bool
	echo     bool
	echoTime bool

	resultHandler func(results []any)

	// set while a command runs in the background (between runCmdMsg and
	// cmdDoneMsg); only touched from Update
	runCmdMark bool
	cancelRun  context.CancelFunc
	running    *command

	exit bool

	// log file (WithLogFile)
	outFile       string
	plainLog      bool
	programOutput io.Writer // bubbletea output override while logging
	restoreStdout func()    // undoes the stdout redirect, flushing the pipe

	// history
	saveHistory        bool
	historyFile        string // "" = default location, resolved by start
	appName            string
	historyChan        chan string
	readyToSaveHistory atomic.Bool
	historyDone        chan struct{} // closed when the history writer exits

	started bool
}

// New creates a prompt. It has no side effects: history is loaded, and the
// log file opened, when Run starts.
func New(opts ...Option) *Prompt {
	p := &Prompt{
		prefix:         defaultPrefix,
		commands:       map[string]*command{},
		historyBuffers: make([]string, 1),
		suggestIndex:   -1,
		maxSuggestions: defaultMaxSuggestions,
		suggestOnEmpty: true,
		styles:         DefaultStyles(),
		helpLine:       true,
		echo:           true,
		resultHandler:  printResults,
		saveHistory:    true,
		historyChan:    make(chan string, 1000),
	}
	p.builtins = p.builtinCommands()
	for _, opt := range opts {
		opt(p)
	}
	p.textInput = textinput.New()
	p.textInput.Focus()
	p.textInput.Prompt = p.prefix
	return p
}

// printResults is the default result handler: it prints the command
// function's return values on one line.
func printResults(results []any) { fmt.Println(results...) }

// Command registers a command whose arguments are flags (-name value). fn is
// a function of one of these forms, optionally with a leading
// context.Context (cancelled when the user presses ctrl+c while it runs):
//
//	func()
//	func(args Args)          // or *Args: struct fields are the flags
//	func(a T1, b T2, ...)    // one Flag(...) option per parameter, in order
//
// Struct fields are configured with tags:
//
//	type Args struct {
//		Name  string        `help:"who to greet" default:"world"`
//		Times int           `flag:"n" help:"repeat count" default:"1"`
//		Wait  time.Duration `default:"1s"`
//	}
//
// The flag name defaults to the field name in kebab-case; flag:"-" skips a
// field. Supported types are strings, bools, all int/uint/float sizes,
// time.Duration, and types implementing flag.Value or
// encoding.TextUnmarshaler.
//
// fn may return any values. A final error is reported as a failure; other
// results go to the result handler (printed by default, see
// WithResultHandler and OnResult).
//
// Invalid registrations (bad signature, duplicate name, ...) are programmer
// errors and panic, so register all commands before Run.
func (p *Prompt) Command(name, help string, fn any, opts ...CommandOption) {
	p.add(name, help, fn, false, opts)
}

// RawCommand registers a command that receives its arguments unparsed: fn is
// func(args string) or func(ctx context.Context, args string), and args is
// the line after the command name, trimmed. Use it for free-form input.
func (p *Prompt) RawCommand(name, help string, fn any, opts ...CommandOption) {
	p.add(name, help, fn, true, opts)
}

func (p *Prompt) add(name, help string, fn any, raw bool, opts []CommandOption) {
	c, err := newCommand(name, help, fn, raw, opts)
	if err != nil {
		panic("prompt: " + err.Error())
	}
	if _, dup := p.commands[name]; dup {
		panic(fmt.Sprintf("prompt: command[%s] is already registered", name))
	}
	p.commands[name] = c
}

// lookup returns the command called name, or nil.
func (p *Prompt) lookup(name string) *command {
	if c, ok := p.commands[name]; ok {
		return c
	}
	return p.builtins[name]
}

// commandNames lists user and built-in command names (unsorted).
func (p *Prompt) commandNames() []string {
	names := make([]string, 0, len(p.commands)+len(p.builtins))
	for n := range p.commands {
		names = append(names, n)
	}
	for n := range p.builtins {
		if _, shadowed := p.commands[n]; !shadowed {
			names = append(names, n)
		}
	}
	return names
}

// Run starts the prompt and blocks until the user exits. It loads and saves
// history, applies WithLogFile, and restores the terminal and stdout before
// returning.
func (p *Prompt) Run() error {
	if err := p.start(); err != nil {
		return err
	}
	defer p.stop()
	opts := p.programOptions
	if p.programOutput != nil {
		// put ours first so a user-supplied tea.WithOutput still wins
		opts = append([]tea.ProgramOption{tea.WithOutput(p.programOutput)}, opts...)
	}
	_, err := tea.NewProgram(p, opts...).Run()
	return err
}

// start performs Run's side effects: loading history, starting the history
// writer, and redirecting stdout for WithLogFile.
func (p *Prompt) start() error {
	if p.started {
		return nil
	}
	p.started = true
	if p.saveHistory {
		if p.historyFile == "" {
			p.historyFile = defaultHistoryPath(p.appName)
		}
		p.loadHistory()
		p.startSaveHistory()
	}
	if p.outFile != "" {
		if err := p.redirectStdout(); err != nil {
			p.stop()
			return err
		}
	}
	return nil
}

// stop undoes start: it flushes history and the log, and restores stdout.
func (p *Prompt) stop() {
	p.closeHistory()
	if p.restoreStdout != nil {
		p.restoreStdout()
		p.restoreStdout = nil
	}
}

// closeHistory closes the history channel and waits for the writer goroutine
// to flush remaining entries. It is a no-op when history saving is off.
func (p *Prompt) closeHistory() {
	if p.historyDone == nil {
		return
	}
	close(p.historyChan)
	<-p.historyDone
	p.historyDone = nil
}

// cmdResult is the outcome of running one command line.
type cmdResult struct {
	name string
	cmd  *command // nil if no command has that name
	err  error
}

// execCmd runs one command line and reports the outcome without touching the
// model, so it is safe to call from the background goroutine of a tea.Cmd.
func (p *Prompt) execCmd(ctx context.Context, line string) cmdResult {
	r := cmdResult{}
	if tokens := tokenize(line); len(tokens) > 0 {
		r.name = tokens[0].Text
	}
	r.cmd = p.lookup(r.name)
	if r.cmd != nil {
		r.err = r.cmd.run(ctx, line, p.resultHandler)
	}
	return r
}

// finishCmd applies a command's outcome to the model (called from Update)
// and returns what to print, or tea.Quit for an Exit command.
func (p *Prompt) finishCmd(r cmdResult) tea.Cmd {
	switch {
	case r.cmd == nil:
		return tea.Printf(`unknown command %q (type "help" to list commands)`, r.name)
	case r.err != nil:
		if usage, ok := isUsage(r.err); ok {
			return tea.Println(usage)
		}
		if errors.Is(r.err, context.Canceled) {
			return tea.Printf("%s: interrupted", r.name)
		}
		return tea.Printf("%s: %v", r.name, r.err)
	case r.cmd.exitAfter:
		p.exit = true
		return tea.Quit
	}
	return nil
}

// saveHistoryEntry queues line for the history file.
func (p *Prompt) saveHistoryEntry(line string) {
	if p.readyToSaveHistory.Load() {
		p.historyChan <- fmt.Sprintf("%s: %s\n", time.Now().Local().Format(timeFormat), line)
	}
}

// runCmd runs line synchronously (saving it to history) and returns the
// follow-up command. The interactive loop runs commands asynchronously via
// runCmdMsg instead; this is the blocking equivalent.
func (p *Prompt) runCmd(line string) tea.Cmd {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	p.saveHistoryEntry(line)
	return p.finishCmd(p.execCmd(context.Background(), line))
}

// Init implements tea.Model.
func (p *Prompt) Init() tea.Cmd {
	return tea.Batch(p.initCmds...)
}

// getCurrentCmdString normalizes the committed line: words (as tokenize sees
// them, so quoted spaces stay inside their word) joined by single spaces.
// Full-width spaces are separators too; typed input is already normalized by
// normalizeFullWidthSpace, this covers e.g. history from older versions.
func (p *Prompt) getCurrentCmdString() string {
	line := strings.ReplaceAll(p.historyBuffers[p.historyIndex], string(fullWidthSpace), " ")
	tokens := tokenize(line)
	words := make([]string, len(tokens))
	for i, t := range tokens {
		words[i] = line[t.Start:t.End]
	}
	return strings.Join(words, " ")
}

// fullWidthSpace is U+3000, which CJK IMEs emit for the space bar in
// full-width mode.
const fullWidthSpace = '\u3000'

// normalizeFullWidthSpace rewrites full-width spaces in the text input to
// ASCII spaces, so an IME left in full-width mode still separates the command
// name, flags and values. Both are a single rune, so the rune cursor position
// is unchanged; callers re-derive the byte offset via syncBufferPos.
func (p *Prompt) normalizeFullWidthSpace() {
	v := p.textInput.Value()
	if !strings.ContainsRune(v, fullWidthSpace) {
		return
	}
	pos := p.textInput.Position()
	p.textInput.SetValue(strings.ReplaceAll(v, string(fullWidthSpace), " "))
	p.textInput.SetCursor(pos)
}

// syncBufferPos records the cursor position as a byte offset into the current
// input buffer. bubbles reports the cursor as a rune index, but the suggestion
// and completion logic slices the buffer by byte, so we convert here.
func (p *Prompt) syncBufferPos() {
	p.historyBufferPos = runeIndexToByteOffset(p.textInput.Value(), p.textInput.Position())
}

// runCmdMsg asks Update to run a committed command line.
type runCmdMsg struct{ line string }

// cmdDoneMsg reports that a background command finished.
type cmdDoneMsg struct{ result cmdResult }

// Update implements tea.Model.
func (p *Prompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if p.runCmdMark {
			return p, nil // a command is running; input is ignored
		}
		// bubbletea v2 delivers bracketed paste as its own message. Forward
		// it to the text input AND re-sync the command buffer; otherwise
		// pasted text is shown but never reaches the command.
		p.suggestIndex = -1
		p.textInput, cmd = p.textInput.Update(msg)
		p.normalizeFullWidthSpace()
		p.historyBuffers[p.historyIndex] = p.textInput.Value()
		p.syncBufferPos()
		return p, cmd
	case tea.KeyPressMsg:
		if p.runCmdMark {
			return p.updateWhileRunning(msg)
		}
		switch keypress := msg.String(); keypress {
		case "ctrl+d":
			p.exit = true
			return p, tea.Quit
		case "ctrl+c":
			p.suggestIndex = -1
			p.historyBuffers[p.historyIndex] = ""
			p.textInput.SetValue("")
			p.syncBufferPos()
			return p, nil
		case "ctrl+l":
			return p, tea.ClearScreen
		case "enter":
			p.historyBuffers[p.historyIndex] = p.textInput.Value()
			line := p.getCurrentCmdString()
			if line != "" && (len(p.historys) == 0 || line != p.historys[len(p.historys)-1]) {
				p.historys = append(p.historys, line)
			}
			p.historyIndex = len(p.historys)
			p.historyBuffers = make([]string, len(p.historys)+1)
			copy(p.historyBuffers, p.historys)
			if p.echo {
				echo := p.prefix + line
				if p.echoTime {
					echo = time.Now().Local().Format(timeFormat) + " " + echo
				}
				fmt.Println(echo)
			}
			p.textInput.SetValue("")
			p.syncBufferPos()
			p.suggestIndex = -1
			p.runCmdMark = true
			return p, func() tea.Msg { return runCmdMsg{line: line} }
		case "up", "down", "ctrl+p", "ctrl+n":
			switch keypress {
			case "up", "ctrl+p":
				p.historyIndex = max(0, p.historyIndex-1)
			case "down", "ctrl+n":
				p.historyIndex = min(p.historyIndex+1, len(p.historys))
			}
			p.textInput.SetValue(p.historyBuffers[p.historyIndex])
			p.textInput.SetCursor(utf8.RuneCountInString(p.historyBuffers[p.historyIndex]))
			p.syncBufferPos()
			p.suggestIndex = -1
			return p, nil
		case "tab", "shift+tab":
			if len(p.matchSuggests) == 0 {
				return p, nil
			}
			if keypress == "tab" {
				p.suggestIndex = (p.suggestIndex + 1) % len(p.matchSuggests)
			} else if p.suggestIndex <= 0 {
				p.suggestIndex = len(p.matchSuggests) - 1
			} else {
				p.suggestIndex--
			}
			// Show the completed line, but keep historyBuffers[historyIndex]
			// and historyBufferPos anchored to the typed prefix, so the same
			// candidate list keeps being generated and repeated tabs cycle
			// through all matches. enter/typing re-sync from the shown value.
			newLine, newPos := replaceScope(p.historyBuffers[p.historyIndex],
				p.matchSuggests[p.suggestIndex].Text, p.historyBufferPos)
			p.textInput.SetValue(newLine)
			p.textInput.SetCursor(byteOffsetToRuneIndex(newLine, newPos))
			return p, nil
		default:
			p.suggestIndex = -1
			p.textInput, cmd = p.textInput.Update(msg)
			p.normalizeFullWidthSpace()
			p.historyBuffers[p.historyIndex] = p.textInput.Value()
			p.syncBufferPos()
			return p, cmd
		}
	case runCmdMsg:
		if strings.TrimSpace(msg.line) == "" {
			p.runCmdMark = false
			return p, nil
		}
		p.saveHistoryEntry(msg.line)
		// Run the command in the background so the event loop stays
		// responsive: ctrl+c cancels a command that takes a context.Context,
		// ctrl+d quits. View stays blank (runCmdMark) until cmdDoneMsg, so
		// command output isn't mixed with the input line.
		ctx, cancel := context.WithCancel(context.Background())
		p.cancelRun = cancel
		p.running = p.lookupLine(msg.line)
		line := msg.line
		return p, func() tea.Msg {
			time.Sleep(runDelay)
			return cmdDoneMsg{p.execCmd(ctx, line)}
		}
	case cmdDoneMsg:
		if p.cancelRun != nil {
			p.cancelRun()
		}
		p.cancelRun, p.running = nil, nil
		p.runCmdMark = false
		return p, p.finishCmd(msg.result)
	case tea.WindowSizeMsg:
		// display width (cells), not bytes, so a multibyte prefix sizes
		// correctly; -1 leaves room for the cursor
		p.textInput.SetWidth(msg.Width - lipgloss.Width(p.prefix) - 1)
		p.termHeight = msg.Height
		return p, nil
	}
	return p, nil
}

// lookupLine returns the command line would dispatch to, or nil.
func (p *Prompt) lookupLine(line string) *command {
	if tokens := tokenize(line); len(tokens) > 0 {
		return p.lookup(tokens[0].Text)
	}
	return nil
}

// updateWhileRunning handles keys while a command runs: ctrl+c interrupts it
// (if it takes a context.Context), ctrl+d quits; other keys are ignored.
func (p *Prompt) updateWhileRunning(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if p.running != nil && !p.running.takesCtx {
			return p, tea.Printf("%s can't be interrupted (its function doesn't take a context.Context); "+
				"waiting for it to finish, ctrl+d quits", p.running.name)
		}
		if p.cancelRun != nil {
			p.cancelRun()
		}
	case "ctrl+d":
		if p.cancelRun != nil {
			p.cancelRun()
		}
		p.exit = true
		return p, tea.Quit
	}
	return p, nil
}

// View implements tea.Model.
func (p *Prompt) View() tea.View {
	return tea.NewView(p.render())
}

// render builds the screen content.
func (p *Prompt) render() string {
	if p.runCmdMark || p.exit {
		return p.prefix
	}
	p.updateSuggestions()
	s := p.textInput.View()
	if sv := p.suggestView(); sv != "" {
		s += "\n" + sv
	}
	if p.helpLine {
		s += "\n" + p.styles.HelpLine.Render(helpLineText)
	}
	return s
}

func (p *Prompt) suggestView() string {
	start, end := p.getSuggestScope()
	if start >= end {
		return ""
	}
	textWidth := 0
	for i := start; i < end; i++ {
		textWidth = max(textWidth, lipgloss.Width(p.matchSuggests[i].Text))
	}
	lines := make([]string, 0, end-start)
	width := 0
	for i := start; i < end; i++ {
		s := p.matchSuggests[i]
		line := s.Text
		if s.Description != "" {
			line += strings.Repeat(" ", textWidth-lipgloss.Width(s.Text)+2) + s.Description
		}
		lines = append(lines, line)
		width = max(width, lipgloss.Width(line)) // cells, so CJK isn't over-padded
	}
	for i, line := range lines {
		style := p.styles.Suggestion
		if start+i == p.suggestIndex {
			style = p.styles.Selected
		}
		lines[i] = style.Width(width).Render(line)
	}
	return strings.Join(lines, "\n")
}

// replaceScope replaces the word under (or just before) byte offset pos in
// line with word and returns the new line and the byte offset just past the
// inserted text. If pos is in the whitespace between words (or at a trailing
// space), word is inserted there.
func replaceScope(line, word string, pos int) (string, int) {
	for _, t := range tokenize(line) {
		if t.Start <= pos && pos <= t.End {
			return line[:t.Start] + word + line[t.End:], t.Start + len(word)
		}
	}
	return line[:pos] + word + line[pos:], pos + len(word)
}

// effectiveSuggestNum caps the number of visible suggestions so the input
// line, suggestions and help line fit the terminal height.
func (p *Prompt) effectiveSuggestNum() int {
	if p.termHeight <= 0 {
		return p.maxSuggestions
	}
	avail := p.termHeight - 1 // input line
	if p.helpLine {
		avail--
	}
	return min(p.maxSuggestions, max(avail, 1))
}

// getSuggestScope returns the visible window [start, end) of matchSuggests,
// keeping the selected suggestion in view.
func (p *Prompt) getSuggestScope() (start, end int) {
	n := p.effectiveSuggestNum()
	start = max(p.suggestIndex, 0)
	if start+n > len(p.matchSuggests)-1 {
		start = max(len(p.matchSuggests)-n, 0)
	}
	return start, min(start+n, len(p.matchSuggests))
}

// updateSuggestions recomputes matchSuggests for the text before the cursor.
func (p *Prompt) updateSuggestions() {
	buf := p.historyBuffers[p.historyIndex]
	pos := min(p.historyBufferPos, len(buf)) // defensive clamp
	// leading spaces don't change which word is the command name
	input := strings.TrimLeft(buf[:pos], " ")
	if !p.suggestOnEmpty && input == "" {
		p.matchSuggests = nil
		return
	}
	tokens := tokenize(input)
	name := ""
	if len(tokens) > 0 {
		name = tokens[0].Text
	}
	// still typing the first word: no word yet, or one word reaching the cursor
	typingName := len(tokens) == 0 || (len(tokens) == 1 && tokens[0].End == len(input))
	c := p.lookup(name)
	switch {
	case c == nil || typingName:
		p.matchSuggests = p.completeNames(name)
	case c.completer != nil:
		p.matchSuggests = sortSuggestions(c.completer(input))
	case c.raw:
		p.matchSuggests = nil // free-form arguments: nothing to suggest
	default:
		p.matchSuggests = c.completeFlags(input)
	}
	if p.suggestIndex >= len(p.matchSuggests) {
		p.suggestIndex = -1
	}
}

// completeNames suggests command names matching input.
func (p *Prompt) completeNames(input string) []Suggestion {
	names := p.commandNames()
	sort.Strings(names)
	var out []Suggestion
	for _, n := range names {
		if isMatch(input, n) {
			out = append(out, Suggestion{Text: n, Description: p.lookup(n).help})
		}
	}
	return out
}
