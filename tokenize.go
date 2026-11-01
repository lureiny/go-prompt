package prompt

import "strings"

// token is one shell-style word of a command line.
type token struct {
	Text  string // the word with quotes removed and escapes resolved
	Start int    // byte offset of the word's first byte in the raw line
	End   int    // byte offset just past the word in the raw line
	Open  bool   // the word ends inside an unterminated quote
}

// tokenize splits a command line into words the way a POSIX shell would, so
// values containing spaces can be passed as -name "张 三" or -name='张 三':
//
//   - words are separated by runs of ASCII spaces (full-width spaces are
//     normalized to ASCII ones while typing, see normalizeFullWidthSpace);
//   - '...' quotes everything literally;
//   - "..." quotes everything except \" and \\, which are escapes;
//   - outside quotes a backslash escapes the next character (e.g. "\ ");
//   - quoted and unquoted parts concatenate: -name="张 三" is one word.
//
// An unterminated quote extends to the end of the line and sets Open on the
// last token; callers decide whether that is an error (running) or simply
// "still typing" (suggestions).
func tokenize(line string) []token {
	var (
		tokens  []token
		buf     strings.Builder
		inWord  bool
		start   int
		quote   byte // 0, '\'' or '"'
		escaped bool
	)
	begin := func(i int) {
		if !inWord {
			inWord, start = true, i
			buf.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case escaped:
			buf.WriteByte(c)
			escaped = false
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				buf.WriteByte(c)
			}
		case quote == '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\'):
				escaped = true
			default:
				buf.WriteByte(c)
			}
		case c == ' ':
			if inWord {
				tokens = append(tokens, token{Text: buf.String(), Start: start, End: i})
				inWord = false
			}
		case c == '\'' || c == '"':
			begin(i)
			quote = c
		case c == '\\':
			begin(i)
			if i+1 < len(line) {
				escaped = true
			} else {
				buf.WriteByte(c) // trailing lone backslash is literal
			}
		default:
			begin(i)
			buf.WriteByte(c)
		}
	}
	if inWord {
		tokens = append(tokens, token{Text: buf.String(), Start: start, End: len(line), Open: quote != 0})
	}
	return tokens
}

// tokenTexts returns the unquoted text of each token.
func tokenTexts(tokens []token) []string {
	texts := make([]string, len(tokens))
	for i, t := range tokens {
		texts[i] = t.Text
	}
	return texts
}

// endsInOpenQuote reports whether line ends inside an unterminated quote.
func endsInOpenQuote(tokens []token) bool {
	return len(tokens) > 0 && tokens[len(tokens)-1].Open
}
