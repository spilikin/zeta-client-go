package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"log"
	"runtime"
	"runtime/cgo"
	"sync/atomic"
	"unsafe"
)

// SmcbHandle owns a C-allocated ZetaSdk_SmcbVTable and the cgo.Handle into
// the Go SMCBConnector. Same lifecycle pattern as StorageHandle and
// LoggerHandle: explicit Close + runtime.AddCleanup safety net + heap-
// allocated *atomic.Bool gate.
type SmcbHandle struct {
	closed  *atomic.Bool
	handle  cgo.Handle
	vtable  *C.ZetaSdk_SmcbVTable
	cleanup runtime.Cleanup
}

// NewSmcbHandle wraps c into a ZetaSdk_SmcbVTable suitable for assigning to
// ZetaSdk_SmcbConfig.customSmcb. While the vtable is in use the SDK ignores
// the SOAP-side fields on SmcbConfig.
func NewSmcbHandle(c SMCBConnector) *SmcbHandle {
	closed := new(atomic.Bool)
	h := cgo.NewHandle(c)
	vt := (*C.ZetaSdk_SmcbVTable)(C.malloc(C.sizeof_ZetaSdk_SmcbVTable))
	C.zeta_smcb_vtable_init(vt, C.uintptr_t(h))
	sh := &SmcbHandle{closed: closed, handle: h, vtable: vt}
	sh.cleanup = runtime.AddCleanup(sh, smcbHandleCleanup, smcbCleanupArgs{
		closed: closed,
		handle: h,
		vtable: vt,
	})
	return sh
}

// VTable returns the C vtable pointer. Pass into SmcbConfig.customSmcb.
func (sh *SmcbHandle) VTable() unsafe.Pointer { return unsafe.Pointer(sh.vtable) }

// Close releases the C vtable and the cgo.Handle. Idempotent. The SDK MUST
// NOT dispatch into this vtable after Close returns.
func (sh *SmcbHandle) Close() {
	if sh.closed.Swap(true) {
		return
	}
	sh.cleanup.Stop()
	freeSmcb(sh.vtable, sh.handle)
}

type smcbCleanupArgs struct {
	closed *atomic.Bool
	handle cgo.Handle
	vtable *C.ZetaSdk_SmcbVTable
}

func smcbHandleCleanup(args smcbCleanupArgs) {
	if args.closed.Swap(true) {
		return
	}
	log.Printf("zeta: SmcbHandle GC'd without Close — releasing late; this is a caller bug")
	freeSmcb(args.vtable, args.handle)
}

func freeSmcb(vt *C.ZetaSdk_SmcbVTable, h cgo.Handle) {
	if vt != nil {
		C.free(unsafe.Pointer(vt))
	}
	if h != 0 {
		h.Delete()
	}
	if hook := TestCleanupHook; hook != nil {
		hook()
	}
}
