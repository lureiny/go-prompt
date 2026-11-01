package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lureiny/go-prompt"
)

// GreetArgs shows struct-based flags: names, help and defaults come from
// field tags (the name defaults to the field name in kebab-case).
type GreetArgs struct {
	People string `help:"who to greet" default:"world"`
	Times  int    `flag:"n" help:"how many times" default:"1"`
	Shout  bool   `help:"use upper case"`
}

func main() {
	p := prompt.New(
		prompt.WithPrefix(">>> "),
		prompt.WithMaxSuggestions(4),
		prompt.WithLogFile("out.log"),
		prompt.WithAppName("go-prompt-example"), // history: ~/.config/go-prompt-example/history
	)

	// greet -people "张 三" -n 2 -shout
	p.Command("greet", "say hello to someone", greet)

	// Plain parameters: one prompt.Flag per parameter, in order.
	// calc -a 1 -b 2 prints the returned value: 3
	p.Command("calc", "add two numbers", func(a, b int16) int16 { return a + b },
		prompt.Flag("a", "first number").Default(10000),
		prompt.Flag("b", "second number"))

	// A leading context.Context makes the command interruptible with ctrl+c.
	p.Command("sleep", "sleep for a while (ctrl+c interrupts)", sleep,
		prompt.Flag("for", "how long").Default("5s"))

	// RawCommand gets the rest of the line as-is.
	p.RawCommand("echo", "print the arguments unchanged", func(args string) {
		fmt.Println(args)
	})

	// "help", "help <command>", "<command> -h" and "exit" are built in.
	if err := p.Run(); err != nil {
		fmt.Println("error:", err)
	}
}

func greet(args GreetArgs) {
	msg := fmt.Sprintf("hello %s", args.People)
	if args.Shout {
		msg = fmt.Sprintf("HELLO %s!", args.People)
	}
	for i := 0; i < args.Times; i++ {
		fmt.Println(msg)
	}
}

// sleep returns ctx.Err() when interrupted; a returned error is shown as a
// failure (context.Canceled as "interrupted").
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		fmt.Println("slept", d)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
