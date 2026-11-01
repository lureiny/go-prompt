# go-prompt - 基于 Bubble Tea 的交互式命令行框架

go-prompt 用来快速搭建交互式命令行工具（REPL）：把普通的 Go 函数注册成命令，自动获得参数解析、Tab 补全、`-h` 帮助、历史记录和中断支持。灵感来自 [c-bata/go-prompt](https://github.com/c-bata/go-prompt)，基于 [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) 构建，需要 Go 1.26+。

## 快速开始

```go
package main

import (
	"fmt"

	"github.com/lureiny/go-prompt"
)

func main() {
	p := prompt.New()
	p.Command("calc", "两数相加", func(a, b int) int { return a + b },
		prompt.Flag("a", "第一个数"),
		prompt.Flag("b", "第二个数").Default(1))
	if err := p.Run(); err != nil {
		fmt.Println(err)
	}
}
```

运行后输入 `calc -a 2 -b 3`，会打印返回值 `5`。`calc -h` 显示参数说明；`help` 列出所有命令；`exit` 或 ctrl+d 退出。

## 注册命令

### 结构体参数（参数较多时推荐）

```go
type GreetArgs struct {
	People string        `help:"对谁说" default:"world"`
	Times  int           `flag:"n" help:"重复次数" default:"1"`
	Shout  bool          `help:"大写"`
	Wait   time.Duration `default:"1s"`
}

p.Command("greet", "打招呼", func(args GreetArgs) { ... })
```

- 每个导出字段就是一个参数，参数名默认是字段名的 kebab-case 形式（`MaxCount` → `-max-count`）。
- `flag:"名字"` 可以改名，`flag:"-"` 表示跳过该字段；`help` 是说明，`default` 是默认值，用命令行语法书写。
- 也可以写成 `*GreetArgs`。

### 普通参数

每个参数对应一个 `prompt.Flag`，顺序和函数参数一致：

```go
p.Command("calc", "两数相加", calc,
	prompt.Flag("a", "第一个数").Default(10000), // 数值会无损转换成参数类型
	prompt.Flag("b", "第二个数"))
```

### 原样接收参数

```go
p.RawCommand("echo", "原样输出", func(args string) { fmt.Println(args) })
```

函数收到的是去掉命令名后的整段文本。

### 函数签名约定

- **第一个参数可以是 `context.Context`**：命令在后台运行，ctrl+c 会取消这个 ctx。没有 ctx 参数的命令无法中断。
- **返回值**：最后一个返回值如果是 `error`，非 nil 时显示为失败（`context.Canceled` 显示为 interrupted）；其余返回值默认用 `fmt.Println` 打印。可以用 `prompt.WithResultHandler` 统一处理，或用 `prompt.OnResult` 为单个命令单独处理。
- **支持的参数类型**：string、bool、所有 int/uint/float、`time.Duration`，以及实现了 `flag.Value` 或 `encoding.TextUnmarshaler` 的类型（例如 `time.Time`）。数值按声明类型解析，溢出会报错。
- **命令选项**：`prompt.Exit()` 表示命令成功后退出程序；`prompt.Completer(f)` 用来自定义参数补全。

注册时如果配置有误（签名不对、`Flag` 数量不匹配、命令重名等），会直接 panic 并给出明确的说明，所以请在 `Run` 之前注册好所有命令。

## 命令行用法

- **参数写法**：`calc -a 1`、`calc -a=1`、`calc --a 1` 都可以；bool 参数写 `-v` 或 `-v=false`（`-v false` 会报错）。
- **带空格的值用引号**：`greet -people "张 三"`、`-people='张 三'`、`-people=张\ 三`。多余的参数会报错，不会被静默丢弃。
- **中文输入**：输入法全角模式下的空格（U+3000）会被当作普通空格。
- **快捷键**：`tab`/`shift+tab` 选择补全，`↑↓` 浏览历史，`ctrl+c` 清空输入或中断正在运行的命令，`ctrl+d` 退出，`ctrl+l` 清屏。

## 选项

| 选项 | 作用 |
|---|---|
| `WithPrefix(">>> ")` | 输入提示符 |
| `WithMaxSuggestions(n)` | 同时显示的补全条数（默认 3） |
| `WithSuggestOnEmpty(bool)` | 输入为空时是否列出命令（默认显示） |
| `WithHelpLine(bool)` | 底部快捷键提示（默认显示） |
| `WithEcho(bool)` / `WithEchoTime(bool)` | 回车后是否回显命令 / 是否带时间戳 |
| `WithStyles(s)` | 补全列表样式，可从 `DefaultStyles()` 开始修改 |
| `WithLogFile(path)` / `WithPlainLog()` | 把输出同时写入日志文件 / 日志中去掉颜色等转义序列 |
| `WithHistoryFile(path)` / `WithAppName(name)` / `WithoutHistory()` | 历史文件位置；默认是 `<用户配置目录>/<程序名>/history`（Linux 下为 `~/.config/<程序名>/history`） |
| `WithResultHandler(f)` | 统一处理返回值，`nil` 表示不打印 |
| `WithoutBuiltinCommands()` | 去掉内置的 `help`/`exit`（同名的自定义命令本来就会覆盖内置命令） |
| `WithProgramOptions(...)` / `WithInitCmds(...)` | 透传给 Bubble Tea，多次调用会累加 |

`New` 本身没有副作用：读取历史、写日志、重定向 stdout 都在 `Run` 里进行，出错时由 `Run` 返回 error。

完整示例见 [example/main.go](example/main.go)（`go run ./example`）。

## 从旧版本升级

这一版重新设计了接口，**旧接口没有保留**：

| 旧 | 新 |
|---|---|
| `NewPrompt(...)` / `NewPromptModel(...)` | `New(...)` |
| `RegisterHandler(fn, name, WithSuggests([]Suggest{...}), WithHandlerHelpMsg(h))` | `Command(name, h, fn, Flag(...)...)`，或使用结构体参数 |
| `WithoutFlagSet()` | `RawCommand(name, help, fn)` |
| `WithExitAfterRun(true)` | `Exit()`；`exit` 命令已经内置 |
| `WithCallback` / `WithDefaultHandlerCallback`（`[]reflect.Value`） | `OnResult` / `WithResultHandler`（`[]any`） |
| `WithGetSuggestMethod` | `Completer` |
| `RegisterType` + `WithFlagSetInitFunc` | 让类型实现 `flag.Value` 或 `encoding.TextUnmarshaler` |
| `WithPromptPrefixOption` / `WithSuggestNum` / `WithOutFile` / `WithFilterAscii` | `WithPrefix` / `WithMaxSuggestions` / `WithLogFile` / `WithPlainLog` |
| `WithOutHelpMsg` / `WithoutPrintCmd` / `WithIgnoreEmptyCmd(true)` / `WithOutSaveHistory` | `WithHelpLine(false)` / `WithEcho(false)` / `WithSuggestOnEmpty(false)` / `WithoutHistory()` |
| `WithForceStyle` / `WithBaseStyle` | `WithStyles(Styles{...})` |
| 默认历史文件 `./.prompt.history` | `<用户配置目录>/<程序名>/history`；想沿用旧位置可写 `WithHistoryFile(".prompt.history")` |

依赖也升级到了 Bubble Tea v2（`charm.land/...`）。如果你用到了 `WithProgramOptions`、`WithInitCmds` 或 `Styles`，需要改用 v2 的类型。lipgloss v2 的 `Color` 只接受 `#RGB`/`#RRGGBB`，`#RRGGBBAA` 会变成无颜色。

## 贡献
如果您有任何建议、改进或发现了问题，请随时提交Issue或创建Pull Request。我们欢迎您的贡献！
