// Package sdktest drives the production sdk package's storage VTable directly
// from Go test code, without involving a live ZETA SDK client. It is a test
// scaffold — never import it from production code.
package sdktest

/*
#include <stdlib.h>
#include "sdktest_glue.h"
*/
import "C"

import (
	"runtime/cgo"
	"sync/atomic"
	"unsafe"

	"github.com/gematik/zeta-client-go/internal/sdk"
)

// Completion records a single SDK callback invocation. Fired tracks how many
// times the callback was dispatched — must be exactly 1 for a well-behaved
// vtable function. Value/OK are populated by the string callback path.
type Completion struct {
	Fired atomic.Int32
	Value string
	OK    bool
	done  chan struct{}
}

func newCompletion() *Completion {
	return &Completion{done: make(chan struct{})}
}

func (c *Completion) callback(v string, ok bool) {
	if c.Fired.Add(1) == 1 {
		c.Value = v
		c.OK = ok
		close(c.done)
	}
}

func vtableOf(sh *sdk.StorageHandle) *C.ZetaSdk_StorageVTable {
	return (*C.ZetaSdk_StorageVTable)(sh.VTable())
}

// InvokePut dispatches vtable.put with a fresh completion callback and blocks
// until the callback fires. Returns the completion for assertions.
func InvokePut(sh *sdk.StorageHandle, key, value string) *Completion {
	c := newCompletion()
	h := cgo.NewHandle(c.callback)
	defer h.Delete()
	ck, cv := C.CString(key), C.CString(value)
	defer C.free(unsafe.Pointer(ck))
	defer C.free(unsafe.Pointer(cv))
	C.test_call_put(vtableOf(sh), ck, cv, C.uintptr_t(h))
	<-c.done
	return c
}

// InvokeGet dispatches vtable.get and blocks for completion.
func InvokeGet(sh *sdk.StorageHandle, key string) *Completion {
	c := newCompletion()
	h := cgo.NewHandle(c.callback)
	defer h.Delete()
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	C.test_call_get(vtableOf(sh), ck, C.uintptr_t(h))
	<-c.done
	return c
}

// InvokeRemove dispatches vtable.remove and blocks for completion.
func InvokeRemove(sh *sdk.StorageHandle, key string) *Completion {
	c := newCompletion()
	h := cgo.NewHandle(c.callback)
	defer h.Delete()
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	C.test_call_remove(vtableOf(sh), ck, C.uintptr_t(h))
	<-c.done
	return c
}

// InvokeClear dispatches vtable.clear and blocks for completion.
func InvokeClear(sh *sdk.StorageHandle) *Completion {
	c := newCompletion()
	h := cgo.NewHandle(c.callback)
	defer h.Delete()
	C.test_call_clear(vtableOf(sh), C.uintptr_t(h))
	<-c.done
	return c
}

// BytesCompletion records the result of an SMC-B byte-returning callback.
// Bytes is the payload (or nil if the SDK callback was invoked with size=0,
// which the binding uses for the error path).
type BytesCompletion struct {
	Fired atomic.Int32
	Bytes []byte
	done  chan struct{}
}

func newBytesCompletion() *BytesCompletion {
	return &BytesCompletion{done: make(chan struct{})}
}

func (c *BytesCompletion) callback(b []byte) {
	if c.Fired.Add(1) == 1 {
		c.Bytes = b
		close(c.done)
	}
}

// InvokeReadCertificate dispatches the SMC-B vtable's readCertificate and
// blocks for completion.
func InvokeReadCertificate(sh *sdk.SmcbHandle) *BytesCompletion {
	vt := (*C.ZetaSdk_SmcbVTable)(sh.VTable())
	c := newBytesCompletion()
	h := cgo.NewHandle(c.callback)
	defer h.Delete()
	C.test_call_read_certificate(vt, C.uintptr_t(h))
	<-c.done
	return c
}

// InvokeExternalAuthenticate dispatches the SMC-B vtable's externalAuthenticate
// and blocks for completion.
func InvokeExternalAuthenticate(sh *sdk.SmcbHandle, challenge string) *BytesCompletion {
	vt := (*C.ZetaSdk_SmcbVTable)(sh.VTable())
	c := newBytesCompletion()
	h := cgo.NewHandle(c.callback)
	defer h.Delete()
	cChallenge := C.CString(challenge)
	defer C.free(unsafe.Pointer(cChallenge))
	C.test_call_external_authenticate(vt, cChallenge, C.uintptr_t(h))
	<-c.done
	return c
}

// InvokeLog dispatches the log vtable's callback once with the given level,
// tag, and message. Synchronous — the Go Logger's Log method runs before
// InvokeLog returns. Pass empty strings for null fields.
func InvokeLog(lh *sdk.LoggerHandle, level, tag, message string) {
	vt := (*C.ZetaSdk_LogVTable)(lh.VTable())
	cLevel := C.CString(level)
	defer C.free(unsafe.Pointer(cLevel))
	var cTag *C.char
	if tag != "" {
		cTag = C.CString(tag)
		defer C.free(unsafe.Pointer(cTag))
	}
	cMsg := C.CString(message)
	defer C.free(unsafe.Pointer(cMsg))
	C.test_call_log(vt, cLevel, cTag, cMsg)
}
