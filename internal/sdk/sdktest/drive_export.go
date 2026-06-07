package sdktest

/*
#include <stdlib.h>
#include "sdktest_glue.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

//export go_test_void_complete
func go_test_void_complete(cbCtx unsafe.Pointer) {
	fn := cgo.Handle(uintptr(cbCtx)).Value().(func(string, bool))
	fn("", false)
}

//export go_test_string_complete
func go_test_string_complete(cbCtx unsafe.Pointer, value *C.char) {
	fn := cgo.Handle(uintptr(cbCtx)).Value().(func(string, bool))
	if value == nil {
		fn("", false)
	} else {
		fn(C.GoString(value), true)
	}
}

//export go_test_bytes_complete
func go_test_bytes_complete(cbCtx unsafe.Pointer, data *C.uint8_t, size C.int) {
	fn := cgo.Handle(uintptr(cbCtx)).Value().(func([]byte))
	if data == nil || size <= 0 {
		fn(nil)
		return
	}
	// C.GoBytes copies into Go memory — safe to drop the C-side pointer after.
	fn(C.GoBytes(unsafe.Pointer(data), size))
}
