package prompt

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		open bool
	}{
		{`a  b`, []string{"a", "b"}, false},
		{`  a b  `, []string{"a", "b"}, false},
		{`-name "张 三"`, []string{"-name", "张 三"}, false},
		{`-name='张 三'`, []string{"-name=张 三"}, false},
		{`-name="张 三"x`, []string{"-name=张 三x"}, false},
		{`"a \"q\" \\ b"`, []string{`a "q" \ b`}, false},
		{`'a \" b'`, []string{`a \" b`}, false},
		{`a\ b c`, []string{"a b", "c"}, false},
		{`""`, []string{""}, false},
		{`-name "张 三`, []string{"-name", "张 三"}, true},
		{`end\`, []string{`end\`}, false},
	}
	for _, c := range cases {
		toks := tokenize(c.in)
		if got := tokenTexts(toks); !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %q, want %q", c.in, got, c.want)
		}
		if endsInOpenQuote(toks) != c.open {
			t.Errorf("tokenize(%q) open = %v, want %v", c.in, !c.open, c.open)
		}
	}
	// spans point at the raw text
	toks := tokenize(`x  "a b" c`)
	if toks[1].Start != 3 || toks[1].End != 8 {
		t.Fatalf("span = [%d,%d), want [3,8)", toks[1].Start, toks[1].End)
	}
}

func TestQuotedValueReachesHandler(t *testing.T) {
	var name, city string
	m := New(WithoutHistory(), WithHelpLine(false))
	m.Command("查询", "", func(n, c string) { name, city = n, c }, Flag("名字", ""), Flag("城市", ""))

	var mm tea.Model = m
	mm, _ = mm.Update(textKey(`查询  -名字 "张  三" -城市='北 京'`))
	mm, teaCmd := mm.Update(key(tea.KeyEnter))
	runAll(mm, teaCmd)
	if name != "张  三" || city != "北 京" {
		t.Fatalf("got name=%q city=%q", name, city)
	}
	// history keeps the quotes and the inner spacing, collapses the rest
	if h := m.historys[len(m.historys)-1]; h != `查询 -名字 "张  三" -城市='北 京'` {
		t.Fatalf("history = %q", h)
	}
}

func TestUnterminatedQuoteIsAnError(t *testing.T) {
	h := mustCommand(t, "c", func(s string) {}, Flag("s", ""))
	if err := h.Run(`c -s "abc`); err == nil {
		t.Fatal("unterminated quote must be an error")
	}
}

func TestNoSuggestionsInsideQuote(t *testing.T) {
	h := mustCommand(t, "c", func(a, b string) {}, Flag("a", ""), Flag("b", ""))
	if got := h.completeFlags(`c -a "x -`); len(got) != 0 {
		t.Fatalf("inside a quote got suggestions %+v", got)
	}
	if got := h.completeFlags(`c -a "x y" -`); len(got) == 0 {
		t.Fatal("after a closed quote the next flag must be suggested")
	}
}

func TestReplaceScopeRespectsQuotes(t *testing.T) {
	line := `c -a "x y" -`
	got, pos := replaceScope(line, "-b", len(line))
	if got != `c -a "x y" -b` || pos != len(got) {
		t.Fatalf("replaceScope = %q, %d", got, pos)
	}
	got, pos = replaceScope("c ", "-a", 2)
	if got != "c -a" || pos != 4 {
		t.Fatalf("replaceScope at gap = %q, %d", got, pos)
	}
}
