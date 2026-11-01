package prompt

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// isMatch reports whether input is an in-order subsequence of candidate
// (case-insensitive), ignoring '-' in input. It compares whole runes, so
// multibyte (e.g. Chinese) names match correctly.
func isMatch(input, candidate string) bool {
	rs := []rune(strings.ToLower(candidate))
	i := 0
	for _, c := range strings.ToLower(input) {
		if c == '-' {
			continue
		}
		found := false
		for i < len(rs) {
			r := rs[i]
			i++
			if r == c {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sortSuggestions(s []Suggestion) []Suggestion {
	sort.SliceStable(s, func(i, j int) bool { return s[i].Text < s[j].Text })
	return s
}

// flagName extracts the flag name from a word like "-name", "--name" or
// "-name=value".
func flagName(word string) (string, bool) {
	if !strings.HasPrefix(word, "-") {
		return "", false
	}
	name := strings.TrimPrefix(word[1:], "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	return name, name != ""
}

func (c *command) flagByName(name string) *flagDef {
	for _, fd := range c.flags {
		if fd.name == name {
			return fd
		}
	}
	return nil
}

// completeFlags returns the flag suggestions for input, the command line up
// to the cursor. Nothing is suggested while a flag value is being typed
// (inside a quote, or right after a non-bool flag without "=value"), and
// flags already on the line aren't offered again.
func (c *command) completeFlags(input string) []Suggestion {
	tokens := tokenize(input)
	if endsInOpenQuote(tokens) {
		return nil
	}
	words := tokenTexts(tokens)
	// the cursor is past the last word (after a space): a new word starts
	if len(tokens) == 0 || tokens[len(tokens)-1].End < len(input) {
		words = append(words, "")
	}
	if len(words) < 2 {
		return nil // still typing the command name
	}
	current := words[len(words)-1]
	prev := words[len(words)-2]

	// typing "-flag=..." or a word that isn't a flag: a value
	if strings.Contains(current, "=") || (current != "" && !strings.HasPrefix(current, "-")) {
		return nil
	}
	// the previous word is a non-bool flag still waiting for its value
	if len(words) > 2 && !strings.Contains(prev, "=") {
		if name, ok := flagName(prev); ok {
			if fd := c.flagByName(name); fd != nil && !fd.isBool {
				return nil
			}
		}
	}
	used := map[string]bool{}
	for _, w := range words[1 : len(words)-1] {
		if name, ok := flagName(w); ok {
			used[name] = true
		}
	}

	var out []Suggestion
	for _, fd := range c.flags {
		if used[fd.name] || !isMatch(current, fd.name) {
			continue
		}
		out = append(out, Suggestion{Text: "-" + fd.name, Description: fd.describe()})
	}
	return sortSuggestions(out)
}

// describe is the flag's help line: "<type> help (default: x)".
func (fd *flagDef) describe() string {
	parts := []string{fd.typeName()}
	if fd.help != "" {
		parts = append(parts, fd.help)
	}
	if d, ok := fd.defaultText(); ok {
		parts = append(parts, "(default: "+d+")")
	}
	return strings.Join(parts, " ")
}

func (fd *flagDef) typeName() string { return fd.typ.String() }

// defaultText renders a non-zero default for display.
func (fd *flagDef) defaultText() (string, bool) {
	if !fd.def.IsValid() || fd.def.IsZero() {
		return "", false
	}
	v := fd.def.Interface()
	if s, ok := v.(string); ok && (s == "" || strings.ContainsAny(s, " \"'")) {
		return fmt.Sprintf("%q", s), true
	}
	if st, ok := v.(fmt.Stringer); ok && fd.def.Kind() != reflect.String {
		return st.String(), true
	}
	return fmt.Sprint(v), true
}
