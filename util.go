package prompt

import (
	"reflect"
	"strings"
)

func IsMatch(input, suggest string) bool {
	index := 0
	input = strings.ToLower(input)
	suggest = strings.ToLower(suggest)

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
	splitedInput := strings.Split(input, " ")
	// filter extra spaces
	inputs := []string{}
	for _, s := range splitedInput {
		if len(s) > 0 {
			inputs = append(inputs, s)
		}
	}

	isInputLast := len(input) == 0 || input[len(input)-1] != ' '
	if len(input) == 0 || !isInputLast {
		inputs = append(inputs, "") // 添加空字符串表示当前在等待输入一个新的参数, inputs的最后一个一定是当前在输入的值
	}

	matchSuggests := make([]Suggest, 0)
	// input custom param, not need suggest
	notInputHandler := len(inputs) > 1
	isInputParamValue := notInputHandler &&
		(IsBoolSuggest(h.Suggests, inputs[len(inputs)-1], h.SuggestPrefix) ||
			IsInputNotBoolValue(inputs, h.SuggestPrefix, h.Suggests))

	// 正在输入参数值, 此时不反回suggest
	if isInputParamValue {
		return matchSuggests, nil
	}
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

const helpMsg = "ctrl+d: exit; tab, shift+tab choise suggest; ↑↓ choise history cmd"

func HelpView() string {
	return helpStyle(helpMsg)
}

func IsBoolSuggest(suggests []Suggest, input, suggestPrefix string) bool {
	for _, s := range suggests {
		prefix := suggestPrefix + s.Text + "="
		if strings.Contains(input, prefix) {
			return reflect.TypeOf(s.Default).String() == reflect.TypeOf(true).String()
		}
	}
	return false
}

func IsSuggest(input, suggestPrefix string) bool {
	return strings.HasPrefix(input, suggestPrefix)
}

func IsInputNotBoolValue(inputs []string, suggestPrefix string, suggests []Suggest) bool {
	inputNum := len(inputs)
	if inputNum < 2 {
		return false
	}

	return IsSuggest(inputs[inputNum-2], suggestPrefix) && !IsBoolSuggest(suggests, inputs[inputNum-2], suggestPrefix)
}
