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
	"sync/atomic"
	"unsafe"
)

// HTTPClient wraps the SDK's ZetaSdk_HttpClient. Each instance is bound to a
// parent Client and shares its mutex — the SDK serializes everything routed
// through one Client. Closing the parent Client before this HTTPClient leaves
// the underlying pointer dangling; close children first.
type HTTPClient struct {
	parent  *Client
	ptr     unsafe.Pointer // *C.ZetaSdk_HttpClient
	closed  *atomic.Bool
	cleanup runtime.Cleanup
}

// NewHTTPClient constructs an HTTPClient bound to client. The returned wrapper
// owns the SDK's ZetaSdk_HttpClient pointer; Close releases it.
func NewHTTPClient(client *Client) (*HTTPClient, error) {
	client.Lock()
	defer client.Unlock()
	ptr := C.ZetaSdk_buildHttpClient(client.Ptr())
	if ptr == nil {
		return nil, errors.New("sdk: ZetaSdk_buildHttpClient returned NULL")
	}
	hc := &HTTPClient{
		parent: client,
		ptr:    ptr,
		closed: new(atomic.Bool),
	}
	hc.cleanup = runtime.AddCleanup(hc, httpClientCleanup, httpClientCleanupArgs{
		closed: hc.closed,
		ptr:    ptr,
	})
	return hc, nil
}

// Close releases the SDK's HTTPClient handle. Idempotent. Must run BEFORE the
// parent Client's Close, otherwise the underlying pointer is already dangling.
func (h *HTTPClient) Close() {
	if h.closed.Swap(true) {
		return
	}
	h.cleanup.Stop()
	h.parent.Lock()
	defer h.parent.Unlock()
	freeHTTPClient(h.ptr)
	h.ptr = nil
}

type httpClientCleanupArgs struct {
	closed *atomic.Bool
	ptr    unsafe.Pointer
}

func httpClientCleanup(args httpClientCleanupArgs) {
	if args.closed.Swap(true) {
		return
	}
	log.Printf("zeta: HTTPClient GC'd without Close — releasing late; this is a caller bug")
	freeHTTPClient(args.ptr)
}

func freeHTTPClient(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	C.ZetaSdk_clearHttpClient(ptr)
	if hook := TestCleanupHook; hook != nil {
		hook()
	}
}

// Get / Post / Put / Delete / Head / Options dispatch to the matching SDK
// HTTP method. Each builds a ZetaSdk_HttpRequest, holds the parent Client's
// mutex for the duration of the SDK call, and wraps the returned response.
//
// body may be nil; for methods without a body (GET, HEAD, DELETE, OPTIONS)
// the SDK ignores it. URL is the resource path appended to the resource
// origin configured at BuildClient time.

func (h *HTTPClient) Get(url string, headers map[string][]string) (*HttpResponse, error) {
	return h.do(methodGet, url, headers, nil)
}

func (h *HTTPClient) Post(url string, headers map[string][]string, body []byte) (*HttpResponse, error) {
	return h.do(methodPost, url, headers, body)
}

func (h *HTTPClient) Put(url string, headers map[string][]string, body []byte) (*HttpResponse, error) {
	return h.do(methodPut, url, headers, body)
}

func (h *HTTPClient) Delete(url string, headers map[string][]string) (*HttpResponse, error) {
	return h.do(methodDelete, url, headers, nil)
}

func (h *HTTPClient) Head(url string, headers map[string][]string) (*HttpResponse, error) {
	return h.do(methodHead, url, headers, nil)
}

func (h *HTTPClient) Options(url string, headers map[string][]string) (*HttpResponse, error) {
	return h.do(methodOptions, url, headers, nil)
}

func (h *HTTPClient) Patch(url string, headers map[string][]string, body []byte) (*HttpResponse, error) {
	return h.do(methodPatch, url, headers, body)
}

type httpMethod int

const (
	methodGet httpMethod = iota
	methodPost
	methodPut
	methodDelete
	methodHead
	methodOptions
	methodPatch
)

func (h *HTTPClient) do(m httpMethod, url string, headers map[string][]string, body []byte) (*HttpResponse, error) {
	if h.closed.Load() {
		return nil, errors.New("sdk: HTTPClient is closed")
	}
	req, freeReq := buildHttpRequest(url, headers, body)
	defer freeReq()

	h.parent.Lock()
	var resp unsafe.Pointer
	switch m {
	case methodGet:
		resp = C.ZetaHttpClient_get(h.ptr, unsafe.Pointer(req))
	case methodPost:
		resp = C.ZetaHttpClient_post(h.ptr, unsafe.Pointer(req))
	case methodPut:
		resp = C.ZetaHttpClient_put(h.ptr, unsafe.Pointer(req))
	case methodDelete:
		resp = C.ZetaHttpClient_delete(h.ptr, unsafe.Pointer(req))
	case methodHead:
		resp = C.ZetaHttpClient_head(h.ptr, unsafe.Pointer(req))
	case methodOptions:
		resp = C.ZetaHttpClient_options(h.ptr, unsafe.Pointer(req))
	case methodPatch:
		resp = C.ZetaHttpClient_patch(h.ptr, unsafe.Pointer(req))
	}
	h.parent.Unlock()

	if resp == nil {
		return nil, errors.New("sdk: HTTP call returned NULL response")
	}
	return WrapHttpResponse(resp), nil
}

// buildHttpRequest allocates a C ZetaSdk_HttpRequest populated with url,
// headers, and body. Returns the request pointer plus a free func that
// releases the request struct and every CString it owns.
func buildHttpRequest(url string, headers map[string][]string, body []byte) (*C.ZetaSdk_HttpRequest, func()) {
	cURL := C.CString(url)
	var cBody *C.char
	if body != nil {
		cBody = C.CString(string(body))
	}
	headerArr, headerCount, freeHeaders := buildHeaders(headers)

	req := (*C.ZetaSdk_HttpRequest)(C.malloc(C.sizeof_ZetaSdk_HttpRequest))
	req.url = cURL
	req.body = cBody
	req.headers = headerArr
	req.headersCount = headerCount

	free := func() {
		C.free(unsafe.Pointer(req))
		C.free(unsafe.Pointer(cURL))
		if cBody != nil {
			C.free(unsafe.Pointer(cBody))
		}
		freeHeaders()
	}
	return req, free
}
