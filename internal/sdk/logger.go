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

// Log level constants — mirror ZetaSdk_LogLevel.
const (
	LogLevelDebug = 0
	LogLevelInfo  = 1
	LogLevelWarn  = 2
	LogLevelError = 3
)

// Logger is the contract a Go-side log sink must satisfy. Implementations MUST
// be safe for concurrent calls — the SDK invokes the log vtable from internal
// coroutine threads. Level is one of LogLevelDebug..LogLevelError.
type Logger interface {
	Log(level int, tag, message string)
}

// LoggerHandle owns a C-allocated ZetaSdk_LogVTable and the cgo.Handle into
// the Go Logger. Lifecycle mirrors StorageHandle: explicit Close +
// runtime.AddCleanup safety net + heap-allocated *atomic.Bool gate.
type LoggerHandle struct {
	closed  *atomic.Bool
	handle  cgo.Handle
	vtable  *C.ZetaSdk_LogVTable
	cleanup runtime.Cleanup
}

// NewLoggerHandle wraps l into a ZetaSdk_LogVTable suitable for passing as
// ZetaSdk_BuildConfig.logVTable.
func NewLoggerHandle(l Logger) *LoggerHandle {
	closed := new(atomic.Bool)
	h := cgo.NewHandle(l)
	vt := (*C.ZetaSdk_LogVTable)(C.malloc(C.sizeof_ZetaSdk_LogVTable))
	C.zeta_log_vtable_init(vt, C.uintptr_t(h))
	lh := &LoggerHandle{closed: closed, handle: h, vtable: vt}
	lh.cleanup = runtime.AddCleanup(lh, loggerHandleCleanup, loggerCleanupArgs{
		closed: closed,
		handle: h,
		vtable: vt,
	})
	return lh
}

// VTable returns the C vtable pointer. Pass into ZetaSdk_BuildConfig.logVTable.
func (lh *LoggerHandle) VTable() unsafe.Pointer {
	return unsafe.Pointer(lh.vtable)
}

// Close releases the C vtable and the cgo.Handle. Idempotent. The SDK MUST
// NOT dispatch log lines after Close returns.
func (lh *LoggerHandle) Close() {
	if lh.closed.Swap(true) {
		return
	}
	lh.cleanup.Stop()
	freeLogger(lh.vtable, lh.handle)
}

type loggerCleanupArgs struct {
	closed *atomic.Bool
	handle cgo.Handle
	vtable *C.ZetaSdk_LogVTable
}

func loggerHandleCleanup(args loggerCleanupArgs) {
	if args.closed.Swap(true) {
		return
	}
	log.Printf("zeta: LoggerHandle GC'd without Close — releasing late; this is a caller bug")
	freeLogger(args.vtable, args.handle)
}

func freeLogger(vt *C.ZetaSdk_LogVTable, h cgo.Handle) {
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

// parseLevel maps the SDK's level string to a Logger.Log level int.
// Unknown strings map to LogLevelInfo as a safe default.
func parseLevel(s string) int {
	switch s {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}
