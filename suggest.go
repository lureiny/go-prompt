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
	// Sort in place by Text. A previous map-based implementation silently
	// dropped/duplicated entries whose Text collided; sorting the slice
	// directly preserves every element.
	sort.SliceStable(suggests, func(i, j int) bool {
		return suggests[i].Text < suggests[j].Text
	})
	return suggests
}
