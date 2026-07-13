# go-prompt - 基于Bubble Tea的快速构建命令行工具框架

go-prompt是一个用Go语言编写的开源框架，旨在帮助用户轻松创建强大的命令行工具。它的灵感来自于 [c-bata/go-prompt](https://github.com/c-bata/go-prompt) 仓库，但它是基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 项目构建的，允许您构建更加交互式和可扩展的命令行工具。

## 特点

- 注册Handler：go-prompt支持用户注册多个Handler，每个Handler代表一个函数，用于处理特定的命令行操作。
- 自动注册回调方法：利用Go语言的系统库flag，go-prompt自动注册回调方法，使得处理命令行参数变得简单。
- 支持多种基本命令行操作：go-prompt支持常见的命令行快捷键，如Ctrl+A（切换到行首输入）、Ctrl+E（切换到行尾输入）、Tab键（向下切换建议），以及其他操作如Ctrl+D（退出程序）和Ctrl+C（清空当前行）。

## 使用方法
初始化Prompt对象：使用prompt.NewPrompt来初始化Prompt对象，您可以根据需要配置提示符和建议的数量等选项。

注册Handler：使用Prompt对象的RegisterHandler方法来注册不同的Handler，每个Handler代表一个函数，用于处理特定的命令。可以为每个Handler提供自定义的提示（Suggest）以帮助用户输入。

运行应用：最后，通过调用Run方法启动应用。

## 示例代码

以下是一个示例代码，演示了如何使用go-prompt创建一个命令行工具：

完整可运行示例见 [`example/main.go`](example/main.go)：

```go
package main

import (
	"fmt"

	prompt "github.com/lureiny/go-prompt"
)

func main() {
	m := prompt.NewPrompt(
		prompt.WithPromptPrefixOption(">>> "),
		prompt.WithSuggestNum(4),
		prompt.WithOutFile("out.log"),
		prompt.WithHelpMsg(),
	)
	// WithoutFlagSet：handler 签名必须是 func(string)，收到的是去掉命令名后的参数串。
	m.RegisterHandler(hello, "hello",
		prompt.WithoutFlagSet(),
		prompt.WithHandlerHelpMsg("say hello to someone"),
	)
	// 默认（flag 模式）：Suggest 数量必须等于参数个数，每个参数变成一个 flag。
	m.RegisterHandler(calc, "calc",
		prompt.WithSuggests([]prompt.Suggest{
			{Text: "a", Default: int16(10000), Description: "a"},
			{Text: "b", Description: "b"},
		}))
	// WithExitAfterRun：运行后退出程序。
	m.RegisterHandler(prompt.DefaultExitFunc, "exit", prompt.WithExitAfterRun(true))

	if err := m.Run(); err != nil {
		panic(err)
	}
}

// WithoutFlagSet handler：输入 "hello world" 时 people == "world"（命令名已被去掉）。
func hello(people string) {
	fmt.Println("hello", people)
}

func calc(a, b int16) {
	fmt.Printf("a + b = %d + %d = %d\n", a, b, a+b)
}
```

这个示例展示了如何用 go-prompt 创建交互式命令行工具：定义不同的 Handler 处理命令，用 Suggest 提供补全提示。

> 说明：`HandlerInfo` 的导出字段在近期修复中改名为 `HandlerReflectType`、`FlagSet`（原 `HandlerReflecType`、`FlagsSet`，均为拼写修正）。若你直接以结构体字面量构造 `HandlerInfo`，升级时需同步改名。

## 贡献
如果您有任何建议、改进或发现了问题，请随时提交Issue或创建Pull Request。我们欢迎您的贡献！