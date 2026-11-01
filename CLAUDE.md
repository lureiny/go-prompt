# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

go-prompt is a Go library (package `prompt`, module `github.com/lureiny/go-prompt`) for building interactive command-line REPL tools, inspired by c-bata/go-prompt but built on Bubble Tea **v2** (`charm.land/bubbletea/v2`, with `charm.land/bubbles/v2` and `charm.land/lipgloss/v2`). The library source lives at the repository root; `example/main.go` is a runnable demo. The README and many code comments are in Chinese.

Public API (keep it small): `New(opts ...Option) *Prompt`; `(*Prompt).Command(name, help, fn, opts ...CommandOption)`, `RawCommand(...)`, `Run()`; command options `Flag(name, help).Default(v)`, `Exit()`, `Completer(f)`, `OnResult(f)`; `With…` prompt options (option.go); `Styles`/`DefaultStyles`; `Suggestion`. Everything else is unexported. The project is pre-1.0 (`v0.0.x` tags), and the old `RegisterHandler`/`Suggest`/`HandlerInfo` API was removed deliberately — don't reintroduce compatibility shims.

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- Test: `go test ./...` (add `-race` where cgo is available; `-run TestName` for a single test)
- Run the demo (interactive TUI, requires a real terminal): `go run ./example`

Tests drive `Update`/`View` directly with synthetic `tea.Msg` values — no TTY needed. Helpers live in `keys_test.go`: `textKey(s)` / `key(code)` build `tea.KeyPressMsg`s; `runAll(m, cmd)` runs a command chain to completion (enter → `runCmdMsg` → background run → `cmdDoneMsg`); `mustCommand`/`mustRaw`/`testCommand` build a `*command` directly; `(*command).Run(line)` runs one line; `startedPrompt(t, opts...)` is `New` plus `start()` (history/log side effects) with cleanup. `View()` returns a `tea.View` — read `.Content`. Test files by concern: `api_test.go` (registration forms, struct args, builtins, results, history location), `handler_test.go` / `flagleak_test.go` (running commands, fresh defaults), `tokenize_test.go` (quoting), `usage_test.go` (-h/help), `async_test.go` (background runs, ctrl+c/ctrl+d), `unicode_test.go` / `chinese_test.go` (multibyte/CJK input), `option_test.go` (stdout redirect), `review_test.go`, `medium_test.go`, `low_test.go` (assorted regressions).

Requires Go 1.26+ (pulled in by bubbletea v2.1.x).

## Architecture

`Prompt` (prompt.go) is the single model type and implements `tea.Model` (`View` wraps the string built by `render()` in a `tea.View`). Key input arrives as `tea.KeyPressMsg`; bracketed paste arrives separately as `tea.PasteMsg` and must also be synced into `historyBuffers`, or pasted text never reaches the command.

**Lifecycle** — `New` applies options and has **no side effects**. `Run` calls `start()` (resolve the history path, `loadHistory`, `startSaveHistory`, `redirectStdout` for `WithLogFile`) and defers `stop()` (`closeHistory` flushes the writer goroutine; `restoreStdout` drains the pipe). Errors from `start` are returned by `Run`.

**Commands** (command.go) — `newCommand` validates the function at registration (programmer errors panic in `Prompt.add`): optional leading `context.Context` (`takesCtx`), then either
- one struct (or `*struct`) parameter that isn't itself a bindable value type: exported fields become flags (`bindStruct`; tags `flag`, `help`, `default`; names default to `kebabCase(field)`), or
- plain parameters, one `Flag(...)` option each, in order (`flagSpecs`; `Flag(...).Default(v)` goes through `defaultValue`: same type, lossless `convertNumber`, or a string parsed with command-line syntax).

`RawCommand` functions are `func(string)` / `func(ctx, string)` and get the argument string (line minus the name, trimmed); they take no `Flag`s.
Bindable types (`bindable`): string, bool, all int/uint/float sizes (parsed at their own bit size, so overflow is a parse error), `time.Duration`, and anything whose pointer implements `flag.Value` or `encoding.TextUnmarshaler` (checked first, so `time.Time` is a value, not a struct of flags). Each run (`parseArgs`) builds a fresh `flag.FlagSet` over freshly allocated values set to the defaults, using the `flagValue` adapter (`IsBoolFlag` for bools), so values never leak between runs. Leftover positional words and unterminated quotes are errors; `-h`/`-help` returns a `usageError` with `command.usage()` (usage.go), printed as-is.
`run` calls the function (panics are recovered into errors); a final `error` result is the command's error; other results go to the command's `OnResult` or the prompt's `resultHandler` (default `printResults`), **only on success**.

**Built-ins** (usage.go) — `help` and `exit` live in `Prompt.builtins`; `lookup` checks user `commands` first, so a user command with the same name shadows a built-in. `WithoutBuiltinCommands` empties the map.

**Running commands** (prompt.go `Update`) — enter snapshots the input into history and returns a `runCmdMsg`. Handling it saves history and returns a background `tea.Cmd` that sleeps `runDelay` and calls `execCmd` (which must not mutate the model); the result comes back as `cmdDoneMsg` and `finishCmd` applies it in `Update` (prints errors/usage, sets `exit` and returns `tea.Quit` for `Exit()` commands). `runCmdMark` is true for the whole run: `View` is blank and keys go to `updateWhileRunning` (ctrl+c cancels the context, ctrl+d quits, everything else is ignored). `runCmd` is the synchronous equivalent, used by tests.

**Tokenizing** — tokenize.go is the single shell-style word splitter (`"…"`, `'…'`, backslash escapes, concatenation; tokens carry raw byte spans and an `Open` flag for an unterminated quote). It is used by `parseArgs`, `getCurrentCmdString` (re-joins raw spans, so quotes are kept in history), command-name dispatch, `updateSuggestions`, `completeFlags` and `replaceScope`. Don't reintroduce `strings.Split(line, " ")`.

**Rune vs byte positions** — bubbles `textinput` reports/accepts the cursor as a *rune* index, but the completion logic slices the input buffer by *byte*. `historyBufferPos` is a byte offset; `syncBufferPos` converts from the textinput's rune index via `runeIndexToByteOffset`, and `SetCursor` calls convert back. Keep this invariant when touching the `Update` key handlers, or multibyte (Chinese) input desyncs. The full-width space U+3000 (CJK IME full-width mode) is a token separator: `normalizeFullWidthSpace` rewrites it to an ASCII space after every textinput update (key and paste paths), and `getCurrentCmdString` also splits on it. Display widths use `lipgloss.Width` (cells), not `len` (bytes).

**History navigation and completion** — up/down move `historyIndex` over `historys`, with per-entry edit buffers in `historyBuffers` (always one longer than `historys`; the last slot is the live line). `updateSuggestions` runs inside `render` on every frame: command names (`completeNames`, fuzzy `isMatch`) while typing the first word, otherwise the command's `Completer`, nothing for raw commands, or `completeFlags` (nothing inside a quote or while a non-bool flag's value is pending; flags already on the line aren't re-offered). Tab/shift+tab cycle `matchSuggests` and splice the choice into the word at the cursor (`replaceScope`) while keeping the buffer anchored to the typed prefix.

**History persistence** (option.go) — lines are appended to the history file as `"2006-01-02 15:04:05.000: <line>"` through the buffered `historyChan` by a writer goroutine. The default file is `<os.UserConfigDir()>/<app>/history` (`defaultHistoryPath`; app = `WithAppName` or the executable name); the directory is created on demand. A missing/corrupt file is skipped with a warning on stderr.

## Design characteristics / gotchas

- **Commands run in a background goroutine, one at a time.** Only commands taking a `context.Context` can be interrupted with ctrl+c.
- **Command stdout ordering** — commands print via `fmt.Println`. `WithLogFile` routes stdout/stderr through a pipe (serialized, teed to the log file). Bubble Tea's output is then a `consoleTee` (option.go): `Fd()` is the real console (v2 reads terminal size, SIGWINCH and color profile from the output fd — a bare pipe breaks rendering) while `Write` goes into the same pipe. `restoreStdout` drains the pipe synchronously so Bubble Tea's terminal-restore sequences aren't lost on exit.
- **Registration panics on invalid configuration** (bad signature, `Flag` count mismatch, unsupported type, duplicate name, invalid flag name) with a message explaining the fix — register everything before `Run`; the event loop reads the command maps without locking.
- lipgloss v2 `Color` only parses `#RGB`/`#RRGGBB`; `#RRGGBBAA` silently becomes no color (`TestDefaultStyleColorsParse` guards the defaults).
