package prompt

import (
	"sort"
)

const (
	SuggestOfParam = iota
	SuggestOfHandler
)

type Suggest struct {
	Text        string
	Description string
	Default     interface{}

	SuggestType int
}

func SortSuggest(suggests []Suggest) []Suggest {
	suggestTextMap := make(map[string]Suggest)
	suggestTexts := make([]string, len(suggests))
	for index, s := range suggests {
		suggestTextMap[s.Text] = s
		suggestTexts[index] = s.Text
	}
	sort.Strings(suggestTexts)
	for index, text := range suggestTexts {
		suggests[index] = suggestTextMap[text]
	}
	return suggests
}
