package prompt

import (
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type Handler interface{} // func

type HandlerCallback func([]reflect.Value)

type FlagSetInitFunc func(funcType reflect.Type) (*flag.FlagSet, []interface{}, error)

type HandlerInfo struct {
	Handler           Handler
	HandlerReflecType reflect.Type
	Callback          HandlerCallback

	Suggests         []Suggest // key = suggest text
	SuggestPrefix    string
	GetSuggestMethod GetSuggestFunc

	Name     string
	FlagsSet *flag.FlagSet
	Params   []interface{}

	HelpMsg string

	UseFlagSet          bool // use flag set to parse param
	FlagSetInitFuncImpl FlagSetInitFunc

	ExitAfterRun bool
}

func NewHandlerInfo(name string, handler Handler, opts ...HandlerInfoOption) *HandlerInfo {
	h := &HandlerInfo{
		Handler:           handler,
		HandlerReflecType: reflect.TypeOf(handler),

		Suggests:      make([]Suggest, 0),
		SuggestPrefix: defaultSuggestPrefix,

		Name:   name,
		Params: make([]interface{}, 0),

		UseFlagSet:          true,
		FlagSetInitFuncImpl: nil,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *HandlerInfo) InitParamsAndFlagSet() error {
	if !h.UseFlagSet {
		return nil
	}
	var err error = nil
	if h.FlagSetInitFuncImpl != nil {
		h.FlagsSet, h.Params, err = h.FlagSetInitFuncImpl(h.HandlerReflecType)
		return err
	}
	h.FlagsSet = flag.NewFlagSet(h.Name, flag.ContinueOnError)
	h.Params = make([]interface{}, 0)
	numIn := h.HandlerReflecType.NumIn()
	for i := 0; i < numIn; i++ {
		v := h.HandlerReflecType.In(i)
		if _, ok := isSupportType(v.String()); !ok {
			return fmt.Errorf("handler[%s] the %d param of type[%s] is not support, need register first",
				h.Name, i, v.String())
		}
		if err := initIterfaceParams(&h.Suggests[i], v, h); err != nil {
			return fmt.Errorf("init handler[%s] the %d in param fail, err: %v", h.Name, i, err)
		}
	}
	return nil
}

func (h *HandlerInfo) Run(cmd string) error {
	fn := reflect.ValueOf(h.Handler)

	args := []reflect.Value{}
	if h.UseFlagSet {
		// parse param
		if err := h.FlagsSet.Parse(strings.Split(cmd, " ")[1:]); err != nil {
			return fmt.Errorf("can't parse handler[%s] args", h.Name)
		}
		for _, param := range h.Params {
			args = append(args, reflect.ValueOf(param).Elem())
		}
	} else {
		args = append(args, reflect.ValueOf(cmd))
	}

	results := fn.Call(args)
	if h.Callback != nil {
		h.Callback(results)
	}
	h.InitParamsAndFlagSet()
	return nil
}

func (h *HandlerInfo) CheckAndInitHandler() error {
	if h.Name == "" {
		return fmt.Errorf("handler name can't be empty")
	}
	if h.HandlerReflecType.Kind() != reflect.Func {
		return fmt.Errorf("handler[%s] is not func", h.Name)
	}
	if errMsg := checkSuggest(h.Suggests); errMsg != "" {
		return fmt.Errorf("check suggest of handler[%s] fail, err: %s", h.Name, errMsg)
	}
	if !h.UseFlagSet {
		if h.HandlerReflecType.NumIn() != 1 || h.HandlerReflecType.In(0).Kind() != reflect.String {
			return fmt.Errorf("handler[%s] not use flagset, should have 1 in param which type is string",
				h.Name)
		}
		return nil
	}
	if h.HandlerReflecType.NumIn() != len(h.Suggests) {
		return fmt.Errorf("handler[%s] has %d in param, but suggest num is %d",
			h.Name, h.HandlerReflecType.NumIn(), len(h.Suggests))
	}
	if err := h.InitParamsAndFlagSet(); err != nil {
		return err
	}
	return nil
}

func checkSuggest(suggests []Suggest) (errMsg string) {
	errMsg = ""
	tempMap := map[string]bool{}
	for _, suggest := range suggests {
		if _, ok := tempMap[suggest.Text]; !ok {
			tempMap[suggest.Text] = true
		} else {
			errMsg += fmt.Sprintf("suggest[%s] is duplicate|", suggest.Text)
		}
	}
	return
}

func initIterfaceParams(suggest *Suggest, valueType reflect.Type, h *HandlerInfo) error {
	if isNil(suggest.Default) {
		defaultValue, err := getDefaultValue(valueType)
		if err != nil {
			return fmt.Errorf("getDefaultValue fail, err: %v", err)
		}
		suggest.Default = defaultValue
	} else {
		defaultType := reflect.TypeOf(suggest.Default)
		if defaultType.String() != valueType.String() {
			return fmt.Errorf("default value type[%s] is defferrnt with in param type[%s]",
				defaultType.String(), valueType.String())
		}
	}
	var param interface{} = nil
	switch valueType.String() {
	case "string":
		param = h.FlagsSet.String(suggest.Text, suggest.Default.(string), suggest.Description)
	case "int":
		param = h.FlagsSet.Int(suggest.Text, suggest.Default.(int), suggest.Description)
	case "int64":
		param = h.FlagsSet.Int64(suggest.Text, suggest.Default.(int64), suggest.Description)
	case "float64":
		param = h.FlagsSet.Float64(suggest.Text, suggest.Default.(float64), suggest.Description)
	case "bool":
		param = h.FlagsSet.Bool(suggest.Text, suggest.Default.(bool), suggest.Description)
	case "uint":
		param = h.FlagsSet.Uint(suggest.Text, suggest.Default.(uint), suggest.Description)
	case "uint64":
		param = h.FlagsSet.Uint64(suggest.Text, suggest.Default.(uint64), suggest.Description)
	default:
		return fmt.Errorf("value type[%s] is not support by flag set", valueType.String())
	}

	h.Params = append(h.Params, param)
	return nil
}

func isNil(i interface{}) bool {
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Slice:
		return v.IsNil()
	default:
		return false || i == nil
	}
}

func getDefaultValue(valueType reflect.Type) (interface{}, error) {
	zeroValue := reflect.Zero(valueType)
	defaultValue := fmt.Sprintf("%v", zeroValue)
	switch valueType.String() {
	case "string":
		return defaultValue, nil
	case "int":
		return strconv.Atoi(defaultValue)
	case "int64":
		return strconv.ParseInt(defaultValue, 10, 64)
	case "float64":
		return strconv.ParseFloat(defaultValue, 64)
	case "bool":
		return defaultValue == "true", nil
	case "uint":
		defaultUint, err := strconv.ParseUint(defaultValue, 10, 16)
		return uint(defaultUint), err
	case "uint64":
		return strconv.ParseUint(defaultValue, 10, 64)
	default:
		return nil, fmt.Errorf("value type[%s] is not support", valueType.String())
	}
}
