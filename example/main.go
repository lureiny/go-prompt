package main

import (
	"fmt"
	"reflect"

	"github.com/lureiny/go-prompt"
)

func main() {
	m := prompt.NewPrompt(
		prompt.WithPromptPrefixOption(">>> "),
		prompt.WithSuggestNum(4),
		prompt.WithDefaultHandlerCallback(helloCallback),
		prompt.WithOutFile("out.log"),
		prompt.WithHelpMsg(),
	)
	m.RegisterHandler(hello, "hello",
		prompt.WithoutFlagSet(),
		prompt.WithSuggests([]prompt.Suggest{
			{Text: "people", Default: "world", Description: "say hello"},
		}),
		prompt.WithHandlerHelpMsg("say hello to someone"),
	)
	m.RegisterHandler(hello2, "hello2",
		prompt.WithSuggests([]prompt.Suggest{
			{Text: "people", Default: "world", Description: "say hello"},
		}))
	m.RegisterHandler(hello1, "hello1",
		prompt.WithSuggests([]prompt.Suggest{
			{Text: "people", Default: "world", Description: "say hello"},
		}))
	m.RegisterHandler(calc, "calc",
		prompt.WithSuggests([]prompt.Suggest{
			{Text: "a", Default: 10, Description: "a"},
			{Text: "b", Description: "b"},
		}))
	m.RegisterHandler(boolTest, "boolTest",
		prompt.WithSuggests([]prompt.Suggest{
			{Text: "b", Description: "b"},
			{Text: "name", Description: "every thing", Default: "bool"},
			{Text: "c", Description: "every thing", Default: true},
			{Text: "d", Description: "every thing", Default: true},
		}))

	m.RegisterHandler(prompt.DefaultExitFunc, "exit", prompt.WithExitAfterRun(true))

	if err := m.Run(); err != nil {
		panic(err)
	}
}

func hello(people string) string {
	fmt.Println("hello", people)
	return "just say hello " + people
}

func helloCallback(results []reflect.Value) {
	if len(results) == 0 {
		return
	}
	for _, r := range results {
		if r.Kind() == reflect.String {
			fmt.Println(r.String())
		}
	}

}

func hello1(people string) {
	fmt.Println("hello1", people)
}

func hello2(people string) {
	fmt.Println("hello2", people)
}

func calc(a, b int) string {
	fmt.Printf("a + b = %d + %d = %d\n", a, b, a+b)
	return "just calc " + fmt.Sprintf("a + b = %d + %d = %d", a, b, a+b)
}

func boolTest(b bool, name string, c, d bool) {
	fmt.Println(b, name, c, d)
}
