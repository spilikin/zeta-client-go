package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"slices"
	"unsafe"
)

// cAllocator collects every C.malloc / C.CString allocation made while
// constructing C structs. Call Free to release them all at once.
type cAllocator struct {
	frees []func()
}

func (a *cAllocator) string(s string) *C.char {
	if s == "" {
		// Caller may still want a non-NULL empty string; if NULL is acceptable
		// for the field, the caller should skip setting it instead.
		c := C.CString("")
		a.frees = append(a.frees, func() { C.free(unsafe.Pointer(c)) })
		return c
	}
	c := C.CString(s)
	a.frees = append(a.frees, func() { C.free(unsafe.Pointer(c)) })
	return c
}

func (a *cAllocator) malloc(size C.size_t) unsafe.Pointer {
	p := C.malloc(size)
	a.frees = append(a.frees, func() { C.free(p) })
	return p
}

func (a *cAllocator) calloc(n, size C.size_t) unsafe.Pointer {
	p := C.calloc(n, size)
	a.frees = append(a.frees, func() { C.free(p) })
	return p
}

// Free releases everything the allocator has handed out. Safe to call once.
func (a *cAllocator) Free() {
	for _, f := range slices.Backward(a.frees) {
		f()
	}
	a.frees = nil
}

// buildHeaders allocates a C array of ZetaSdk_HttpHeader from a Go map.
// Returns the C array pointer, count, and a free func that releases the array
// plus every CString inside it. For empty/nil input returns (nil, 0, no-op).
func buildHeaders(h map[string][]string) (*C.ZetaSdk_HttpHeader, C.int, func()) {
	count := 0
	for _, vs := range h {
		count += len(vs)
	}
	if count == 0 {
		return nil, 0, func() {}
	}
	arr := (*C.ZetaSdk_HttpHeader)(C.malloc(C.size_t(count) * C.sizeof_ZetaSdk_HttpHeader))
	slice := unsafe.Slice(arr, count)
	cStrings := make([]*C.char, 0, count*2)
	i := 0
	for k, vs := range h {
		for _, v := range vs {
			ck := C.CString(k)
			cv := C.CString(v)
			cStrings = append(cStrings, ck, cv)
			slice[i].key = ck
			slice[i].value = cv
			i++
		}
	}
	free := func() {
		for _, s := range cStrings {
			C.free(unsafe.Pointer(s))
		}
		C.free(unsafe.Pointer(arr))
	}
	return arr, C.int(count), free
}
