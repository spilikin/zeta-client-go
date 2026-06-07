package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
	"unsafe"
)

//export go_smcb_read_certificate
func go_smcb_read_certificate(ctx unsafe.Pointer, cb C.ZetaSdk_BytesCallback, cbCtx unsafe.Pointer) {
	c, ok := cgo.Handle(uintptr(ctx)).Value().(SMCBConnector)
	if !ok || c == nil {
		C.zeta_invoke_bytes_cb(cb, cbCtx, nil, 0)
		return
	}
	der, err := c.ReadCertificate()
	if err != nil || len(der) == 0 {
		C.zeta_invoke_bytes_cb(cb, cbCtx, nil, 0)
		return
	}
	C.zeta_invoke_bytes_cb(cb, cbCtx, (*C.uint8_t)(unsafe.Pointer(&der[0])), C.int(len(der)))
	runtime.KeepAlive(der)
}

//export go_smcb_external_authenticate
func go_smcb_external_authenticate(ctx unsafe.Pointer, challenge *C.char, cb C.ZetaSdk_BytesCallback, cbCtx unsafe.Pointer) {
	c, ok := cgo.Handle(uintptr(ctx)).Value().(SMCBConnector)
	if !ok || c == nil {
		C.zeta_invoke_bytes_cb(cb, cbCtx, nil, 0)
		return
	}
	chall := ""
	if challenge != nil {
		chall = C.GoString(challenge)
	}
	sig, err := c.ExternalAuthenticate(chall)
	if err != nil || len(sig) == 0 {
		C.zeta_invoke_bytes_cb(cb, cbCtx, nil, 0)
		return
	}
	C.zeta_invoke_bytes_cb(cb, cbCtx, (*C.uint8_t)(unsafe.Pointer(&sig[0])), C.int(len(sig)))
	runtime.KeepAlive(sig)
}
