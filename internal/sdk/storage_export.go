package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"unsafe"
)

//export go_storage_put
func go_storage_put(ctx unsafe.Pointer, key *C.char, value *C.char, cb C.ZetaSdk_VoidCallback, cbCtx unsafe.Pointer) {
	s := cgo.Handle(uintptr(ctx)).Value().(Storage)
	_ = s.Put(C.GoString(key), C.GoString(value))
	C.zeta_invoke_void_cb(cb, cbCtx)
}

//export go_storage_get
func go_storage_get(ctx unsafe.Pointer, key *C.char, cb C.ZetaSdk_StringCallback, cbCtx unsafe.Pointer) {
	s := cgo.Handle(uintptr(ctx)).Value().(Storage)
	value, err := s.Get(C.GoString(key))
	if errors.Is(err, ErrNotFound) || err != nil {
		C.zeta_invoke_string_cb(cb, cbCtx, nil)
		return
	}
	cValue := C.CString(value)
	defer C.free(unsafe.Pointer(cValue))
	C.zeta_invoke_string_cb(cb, cbCtx, cValue)
}

//export go_storage_remove
func go_storage_remove(ctx unsafe.Pointer, key *C.char, cb C.ZetaSdk_VoidCallback, cbCtx unsafe.Pointer) {
	s := cgo.Handle(uintptr(ctx)).Value().(Storage)
	_ = s.Remove(C.GoString(key))
	C.zeta_invoke_void_cb(cb, cbCtx)
}

//export go_storage_clear
func go_storage_clear(ctx unsafe.Pointer, cb C.ZetaSdk_VoidCallback, cbCtx unsafe.Pointer) {
	s := cgo.Handle(uintptr(ctx)).Value().(Storage)
	_ = s.Clear()
	C.zeta_invoke_void_cb(cb, cbCtx)
}
