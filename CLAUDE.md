# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

go-prompt is a Go library (package `prompt`, module `github.com/lureiny/go-prompt`) for building interactive command-line REPL tools, inspired by c-bata/go-prompt but built on charmbracelet/bubbletea. The library source lives at the repository root; `example/main.go` is a runnable demo. The README and many code comments are in Chinese.

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- Test: `go test ./...` (add `-race` for the history/stdout-redirect goroutine tests; `-run TestName` for a single test)
- Run the demo (interactive TUI, requires a real terminal): `go run ./example`

Tests are split by concern: `handler_test.go` / `flagleak_test.go` (handler execution + flag state), `unicode_test.go` (multibyte/rune handling), `option_test.go` (stdout redirect), `medium_test.go`, `low_test.go`. They drive `Update`/`View` directly with synthetic `tea.Msg` values — no TTY needed.

Requires Go 1.24+ (pulled in by bubbletea v1.3.x).

## Architecture

The library follows bubbletea's Elm architecture: `PromptModel` (prompt.go) implements `tea.Model` with `Init`/`Update`/`View`.

**Startup pipeline** — `NewPrompt(opts...)` applies functional options (`PromptModelOption`, option.go), then runs `initFuncs` in order: `initTextModel`, `loadHistory`, `startSaveHistory`, plus `redirectStdout` if `WithOutFile` was used. Init failures panic. `Prompt.Run()` then starts the bubbletea program.

**Handler system (the core mechanism)** — handlers are arbitrary Go funcs registered via `RegisterHandler(fn, name, opts...)`. handler.go uses reflection to:

1. Validate at registration (`CheckAndInitHandler`): in the default flag mode, the number of `Suggest` entries must equal the func's parameter count, and every param type must exist in `supportTypeMap` (type.go).
2. Build a stdlib `flag.FlagSet` per handler (`InitParamsAndFlagSet`): each param becomes a flag named after the corresponding `Suggest.Text`. Flag values are stored as the widest type of each family (int64/uint64/float64/string/bool).
3. Execute (`HandlerInfo.Run`): the FlagSet is rebuilt with fresh defaults at the **start** of every run (so values from a previous run — including one that failed to parse or panicked — never leak) → `FlagSet.Parse` on everything after the handler name → `convertParam` (util.go) narrows int64/uint64/float64 back to the declared param type, panicking on range overflow (the panic is recovered inside `Run` and returned as an error) → `reflect.Call` → optional `HandlerCallback` receives the `[]reflect.Value` results.

`WithoutFlagSet()` opts a handler out of flag parsing; such a handler must have the signature `func(string)` and receives the **entire raw command line** (including the handler name). `WithExitAfterRun(true)` quits the program after the handler runs (see the `exit` handler in example/main.go).

**Rune vs byte positions** — bubbles `textinput` reports/accepts the cursor as a *rune* index, but the suggestion/completion logic slices the input buffer by *byte*. `historyBufferPos` is kept as a byte offset; `syncBufferPos` converts from the textinput's rune index via `runeIndexToByteOffset`, and `SetCursor` calls convert back. Keep this invariant when touching the `Update` key handlers, or multibyte (Chinese) input desyncs. Display widths use `lipgloss.Width` (cells), not `len` (bytes).

**Update loop key handling** (prompt.go `Update`) — enter snapshots the input into history and dispatches a `RunCmdMsg`; the handler actually runs on the next Update cycle so the view can redraw first (`runCmdMark` blanks `View` during that window). Up/down navigate `historys` via `historyIndex`, with per-entry edit buffers kept in `historyBuffers` (always one element longer than `historys` — the last slot is the live input line). Tab/shift+tab cycle `matchSuggests` and splice the selected suggestion into the word at the cursor (`replaceScope`).

**Suggestions** — two kinds (suggest.go): `SuggestOfHandler` (registered handler names, offered while typing the first word) and `SuggestOfParam` (a handler's flags, offered with the `-` prefix). Matching uses `IsMatch` (util.go), a subsequence-style fuzzy match. `updateSuggentList` recomputes the list inside `View` on every render; a handler can override suggestion generation with `WithGetSuggestMethod`.

**History persistence** — commands append to `.prompt.history` (configurable via `WithHistoryFile`, disable with `WithOutSaveHistory`) prefixed with a `"2006-01-02 15:04:05.000: "` timestamp. `loadHistory` parses that exact format on startup (a corrupt/unreadable file warns and is skipped rather than crashing construction); `startSaveHistory` writes asynchronously through the buffered `historyChan` in a goroutine. `Prompt.Run()` calls `closeHistory()` after the bubbletea loop stops to flush buffered entries — so drive the prompt via `NewPrompt(...).Run()`; hand-rolling the bubbletea loop around a bare `PromptModel` leaks that goroutine and loses the final flush.

**Custom param types** — `RegisterType` (type.go) registers a type for the default binding's type gate, but the built-in flag binding only handles the built-in scalar types, so for a genuinely custom type you must also supply `WithFlagSetInitFunc` (which bypasses the built-in binding entirely).

## Design characteristics / gotchas

- **Handlers run synchronously inside the `Update` loop** (after a `RunCmdMsg` and a `runCmdDeply`-ms sleep). A long-running handler blocks the event loop, so keystrokes (including ctrl+c) aren't processed until it returns.
- **Handler stdout ordering** — handlers print via `fmt.Println`. Use `WithOutFile` to route stdout/stderr through a pipe (serialized, teed to a log file); without it, direct prints can interleave with the bubbletea renderer.
- **`RegisterHandler`/`SetHandlers` panic on invalid configuration** (duplicate name, wrong param/suggest count, invalid flag-name in `Suggest.Text`, etc.) — treat these as setup-time programmer errors and register everything before `Run`. `SetHandlers` is setup-only: the event loop reads the handler map without locking.
