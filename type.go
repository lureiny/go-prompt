package prompt

import (
	"reflect"
)

var supportTypeMap = map[string]reflect.Type{}

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

func RegisterType(i interface{}) {
	supportTypeMap[reflect.TypeOf(i).String()] = reflect.TypeOf(i)
}

func isSupportType(name string) (reflect.Type, bool) {
	t, ok := supportTypeMap[name]
	return t, ok
}
