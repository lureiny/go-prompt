package prompt

import (
	"fmt"
	"math"
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

func floatxToFloat64(val interface{}) float64 {
	switch val.(type) {
	case float64:
		return val.(float64)
	case float32:
		return float64(val.(float32))
	default:
		panic("val is not float")
	}
}

func intxToInt64(val interface{}) int64 {
	switch val.(type) {
	case int:
		return int64(val.(int))
	case int8:
		return int64(val.(int8))
	case int16:
		return int64(val.(int16))
	case int32:
		return int64(val.(int32))
	case int64:
		return val.(int64)
	default:
		panic("val is not int")
	}
}

func uintxToUint64(val interface{}) uint64 {
	switch val.(type) {
	case uint:
		return uint64(val.(uint))
	case uint8:
		return uint64(val.(uint8))
	case uint16:
		return uint64(val.(uint16))
	case uint32:
		return uint64(val.(uint32))
	case uint64:
		return val.(uint64)
	default:
		panic("val is not uint")
	}
}

const rangeOutString = "invalid value '%v' for %v param"

func int64ToIntx(src interface{}, dst reflect.Type) interface{} {
	var srcData int64 = 0
	switch src.(type) {
	case *int64:
		p := src.(*int64)
		srcData = *p
	case int64:
		srcData = src.(int64)
	default:
		panic("src is not int64 or *int64")
	}

	switch dst.String() {
	case "int":
		if srcData < math.MinInt || srcData > math.MaxInt {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return int(srcData)
	case "int8":
		if srcData < math.MinInt8 || srcData > math.MaxInt8 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return int8(srcData)
	case "int16":
		if srcData < math.MinInt16 || srcData > math.MaxInt16 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return int16(srcData)
	case "int32":
		if srcData < math.MinInt32 || srcData > math.MaxInt32 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return int32(srcData)
	case "int64":
		return srcData
	default:
		panic("dst is not int")
	}
}

func uint64ToUintx(src interface{}, dst reflect.Type) interface{} {
	var srcData uint64 = 0
	switch src.(type) {
	case *uint64:
		p64 := src.(*uint64)
		srcData = *p64
	case uint64:
		srcData = src.(uint64)
	default:
		panic("src is not uint64 or *uint64")
	}
	switch dst.String() {
	case "uint":
		if srcData > math.MaxUint {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return uint(srcData)
	case "uint8":
		if srcData > math.MaxUint8 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return uint8(srcData)
	case "uint16":
		if srcData > math.MaxUint16 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return uint16(srcData)
	case "uint32":
		if srcData > math.MaxUint32 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return uint32(srcData)
	case "uint64":
		return srcData
	default:
		panic("dst is not uint")
	}
}

func float64ToFloatx(src interface{}, dst reflect.Type) interface{} {
	var srcData float64 = 0
	switch src.(type) {
	case *float64:
		p64 := src.(*float64)
		srcData = *p64
	case float64:
		srcData = src.(float64)
	default:
		panic("src is not float64 or *float64")
	}

	switch dst.String() {
	case "float32":
		if srcData > math.MaxFloat32 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return float32(srcData)
	case "float64":
		if srcData > math.MaxFloat64 {
			panic(fmt.Sprintf(rangeOutString, srcData, dst.String()))
		}
		return srcData
	default:
		panic("dst is not float")
	}
}

func convertParam(src interface{}, dst reflect.Type) reflect.Value {
	switch src.(type) {
	case *int64, int64:
		return reflect.ValueOf(int64ToIntx(src, dst))
	case *float64, float64:
		return reflect.ValueOf(float64ToFloatx(src, dst))
	case *uint64, uint64:
		return reflect.ValueOf(uint64ToUintx(src, dst))
	default:
		return reflect.ValueOf(src).Elem()
	}
}
