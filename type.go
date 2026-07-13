package prompt

import (
	"reflect"
	"sync"
)

var (
	supportTypeMu  sync.RWMutex
	supportTypeMap = map[string]reflect.Type{}
)

func init() {
	initSupportTypes := []interface{}{
		"0",                                           // string
		true,                                          // bool
		int(0), int8(0), int16(0), int32(0), int64(0), // int
		uint(0), uint8(0), uint16(0), uint32(0), uint64(0), // uint
		float32(0), float64(0), // float
	}
	for _, initSupportType := range initSupportTypes {
		RegisterType(initSupportType)
	}
}

// RegisterType marks the dynamic type of i as an accepted handler parameter type
// for the default flag-based parameter binding's type gate.
//
// Note: the built-in flag binding only knows how to parse the built-in scalar
// types (string, bool, the int/uint families, and float32/64). Registering a
// genuinely custom type is therefore not sufficient on its own — you must also
// supply a custom binding via WithFlagSetInitFunc, which bypasses the built-in
// parameter parsing entirely. Safe for concurrent use.
func RegisterType(i interface{}) {
	supportTypeMu.Lock()
	defer supportTypeMu.Unlock()
	supportTypeMap[reflect.TypeOf(i).String()] = reflect.TypeOf(i)
}

func isSupportType(name string) (reflect.Type, bool) {
	supportTypeMu.RLock()
	defer supportTypeMu.RUnlock()
	t, ok := supportTypeMap[name]
	return t, ok
}
