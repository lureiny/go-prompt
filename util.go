package prompt

import "strings"

func IsMatch(input, suggest string) bool {
	index := 0

	for _, c := range input {
		if c == '-' {
			continue
		}

		if index == len(suggest) {
			return false
		}

		i := strings.IndexByte(suggest[index:], byte(c)) // 使用 strings.IndexByte 查找字符
		if i == -1 {
			return false
		}
		index += i + 1 // 更新 index
	}

	return true
}

type GetSuggestFunc func(h *HandlerInfo, input string) ([]Suggest, error)

func DefaultGetHandlerSuggests(h *HandlerInfo, input string) ([]Suggest, error) {
	inputs := strings.Split(input, " ")
	if len(inputs) == 0 {
		inputs = append(inputs, "")
	}
	matchSuggests := make([]Suggest, 0)
	for _, s := range h.Suggests {
		if IsMatch(inputs[len(inputs)-1], s.Text) {
			newSuggest := Suggest{
				Text:        h.SuggestPrefix + s.Text,
				Description: s.Description,
				Default:     s.Default,
			}
			matchSuggests = append(matchSuggests, newSuggest)
		}
	}
	return matchSuggests, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
