package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"errors"
	"log"
	"runtime"
	"runtime/cgo"
	"sync/atomic"
	"unsafe"
)

// ErrNotFound is returned by Storage.Get for missing keys. The cgo glue
// translates it into a NULL value passed to the SDK's string callback.
var ErrNotFound = errors.New("sdk: storage key not found")

// Storage is the contract a Go-side persistence backend must satisfy.
// Implementations MUST be safe for concurrent calls from arbitrary goroutines:
// the SDK invokes the underlying vtable from its internal Kotlin coroutine
// threads, and the cgo glue dispatches them on whichever thread the SDK uses.
type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Remove(key string) error
	Clear() error
}

// TestCleanupHook is invoked by every StorageHandle cleanup path (explicit
// Close OR runtime.AddCleanup safety net), exactly once per wrapper, after
// freeing. Test-only observation point — production code must leave it nil.
var TestCleanupHook func()

// StorageHandle owns a C-allocated ZetaSdk_StorageVTable and the cgo.Handle
// that points back into the Go Storage. The vtable pointer must outlive every
// SDK call that might dispatch into it — only call Close after the SDK client
// holding this customStorage has been torn down.
//
// Lifecycle: explicit Close is the contract. A runtime.AddCleanup is registered
// as a safety net that fires if the wrapper is GC'd before Close — it frees
// the C memory and logs a warning. The safety net is "release-late + tell
// somebody"; it does not absolve callers of the discipline.
type StorageHandle struct {
	closed  *atomic.Bool
	handle  cgo.Handle
	vtable  *C.ZetaSdk_StorageVTable
	cleanup runtime.Cleanup
}

// NewStorageHandle wraps s into a ZetaSdk_StorageVTable suitable for passing
// as ZetaSdk_StorageConfig.customStorage.
func NewStorageHandle(s Storage) *StorageHandle {
	closed := new(atomic.Bool)
	h := cgo.NewHandle(s)
	vt := (*C.ZetaSdk_StorageVTable)(C.malloc(C.sizeof_ZetaSdk_StorageVTable))
	C.zeta_storage_vtable_init(vt, C.uintptr_t(h))
	sh := &StorageHandle{closed: closed, handle: h, vtable: vt}
	sh.cleanup = runtime.AddCleanup(sh, storageHandleCleanup, storageCleanupArgs{
		closed: closed,
		handle: h,
		vtable: vt,
	})
	return sh
}

// VTable returns the C vtable pointer. Pass into ZetaSdk_StorageConfig.customStorage.
func (sh *StorageHandle) VTable() unsafe.Pointer {
	return unsafe.Pointer(sh.vtable)
}

// Close releases the C vtable and the cgo.Handle. Idempotent: subsequent calls
// are no-ops. The SDK MUST NOT dispatch into this vtable after Close returns;
// doing so will dereference freed memory and crash.
func (sh *StorageHandle) Close() {
	if sh.closed.Swap(true) {
		return
	}
	sh.cleanup.Stop()
	freeStorage(sh.vtable, sh.handle)
}

type storageCleanupArgs struct {
	closed *atomic.Bool
	handle cgo.Handle
	vtable *C.ZetaSdk_StorageVTable
}

func storageHandleCleanup(args storageCleanupArgs) {
	if args.closed.Swap(true) {
		return
	}
	log.Printf("zeta: StorageHandle GC'd without Close — releasing late; this is a caller bug")
	freeStorage(args.vtable, args.handle)
}

func freeStorage(vt *C.ZetaSdk_StorageVTable, h cgo.Handle) {
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
