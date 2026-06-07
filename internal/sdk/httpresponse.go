package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"log"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// HttpResponse wraps an SDK-allocated ZetaSdk_HttpResponse. The pointer must
// be released via ZetaHttpResponse_destroy exactly once; Close handles it,
// runtime.AddCleanup is the safety net.
//
// In normal use callers don't see HttpResponse — the public layer snapshots
// the fields and Close()s inline. This wrapper exists so that pattern is
// uniform and the safety net catches any forgotten Close.
type HttpResponse struct {
	closed  *atomic.Bool
	resp    *C.ZetaSdk_HttpResponse
	cleanup runtime.Cleanup
}

// WrapHttpResponse takes ownership of resp. Pass the pointer the SDK returned
// from ZetaHttpClient_get/post/etc. The wrapper destroys it via
// ZetaHttpResponse_destroy when Close (or the safety net) runs.
func WrapHttpResponse(resp unsafe.Pointer) *HttpResponse {
	closed := new(atomic.Bool)
	hr := &HttpResponse{closed: closed, resp: (*C.ZetaSdk_HttpResponse)(resp)}
	hr.cleanup = runtime.AddCleanup(hr, httpResponseCleanup, httpResponseCleanupArgs{
		closed: closed,
		resp:   hr.resp,
	})
	return hr
}

// Status returns the HTTP status code. 0 indicates a transport-level error;
// inspect Error for details.
func (hr *HttpResponse) Status() int {
	if hr.resp == nil {
		return 0
	}
	return int(hr.resp.status)
}

// Body returns the response body as a copy. Empty slice for NULL body.
func (hr *HttpResponse) Body() []byte {
	if hr.resp == nil || hr.resp.body == nil {
		return nil
	}
	return []byte(C.GoString(hr.resp.body))
}

// Error returns the transport-level error string if the SDK reported one,
// "" otherwise.
func (hr *HttpResponse) Error() string {
	if hr.resp == nil || hr.resp.error == nil {
		return ""
	}
	return C.GoString(hr.resp.error)
}

// Headers returns a copy of the response headers.
func (hr *HttpResponse) Headers() map[string][]string {
	if hr.resp == nil || hr.resp.headers == nil || hr.resp.headersCount == 0 {
		return nil
	}
	count := int(hr.resp.headersCount)
	slice := unsafe.Slice(hr.resp.headers, count)
	out := make(map[string][]string, count)
	for i := range count {
		k := C.GoString(slice[i].key)
		v := C.GoString(slice[i].value)
		out[k] = append(out[k], v)
	}
	return out
}

// Close destroys the SDK-allocated response. Idempotent. The SDK MUST NOT be
// used to reference this response after Close returns.
func (hr *HttpResponse) Close() {
	if hr.closed.Swap(true) {
		return
	}
	hr.cleanup.Stop()
	freeHttpResponse(hr.resp)
}

type httpResponseCleanupArgs struct {
	closed *atomic.Bool
	resp   *C.ZetaSdk_HttpResponse
}

func httpResponseCleanup(args httpResponseCleanupArgs) {
	if args.closed.Swap(true) {
		return
	}
	log.Printf("zeta: HttpResponse GC'd without Close — releasing late; this is a caller bug")
	freeHttpResponse(args.resp)
}

func freeHttpResponse(p *C.ZetaSdk_HttpResponse) {
	if p == nil {
		return
	}
	C.ZetaHttpResponse_destroy(unsafe.Pointer(p))
	if hook := TestCleanupHook; hook != nil {
		hook()
	}
}
