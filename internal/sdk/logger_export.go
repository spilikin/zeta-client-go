package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

//export go_log_callback
func go_log_callback(ctx unsafe.Pointer, level *C.char, tag *C.char, message *C.char) {
	l, ok := cgo.Handle(uintptr(ctx)).Value().(Logger)
	if !ok || l == nil {
		return
	}
	levelStr := ""
	if level != nil {
		levelStr = C.GoString(level)
	}
	tagStr := ""
	if tag != nil {
		tagStr = C.GoString(tag)
	}
	msgStr := ""
	if message != nil {
		msgStr = C.GoString(message)
	}
	l.Log(parseLevel(levelStr), tagStr, msgStr)
}
