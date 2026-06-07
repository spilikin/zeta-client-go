package zeta

import (
	"context"
	"errors"

	"github.com/gematik/zeta-client-go/internal/sdk"
)

// HTTPClient performs authenticated HTTP requests through the SDK. Derive
// from Client.HTTPClient. Close before the parent Client.
type HTTPClient struct {
	inner *sdk.HTTPClient
}

// Close releases the SDK's HTTPClient handle. Idempotent. MUST be called
// before the parent Client's Close.
func (h *HTTPClient) Close() { h.inner.Close() }

// Response is a snapshot of an HTTP response from the SDK. The body and
// header copies are pure Go — no Close required.
type Response struct {
	Status  int
	Body    []byte
	Headers map[string][]string
}

// ErrTransport wraps a transport-level error reported by the SDK (the
// response's error field is non-empty and Status is 0).
var ErrTransport = errors.New("zeta: transport error")

// Get sends an authenticated GET to url, returning the response snapshot.
// ctx is accepted for forward-compatibility but is not currently honoured by
// the synchronous SDK call.
func (h *HTTPClient) Get(ctx context.Context, url string, headers map[string][]string) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Get(url, headers) })
}

// Post sends an authenticated POST with body.
func (h *HTTPClient) Post(ctx context.Context, url string, headers map[string][]string, body []byte) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Post(url, headers, body) })
}

// Put sends an authenticated PUT with body.
func (h *HTTPClient) Put(ctx context.Context, url string, headers map[string][]string, body []byte) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Put(url, headers, body) })
}

// Delete sends an authenticated DELETE.
func (h *HTTPClient) Delete(ctx context.Context, url string, headers map[string][]string) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Delete(url, headers) })
}

// Head sends an authenticated HEAD.
func (h *HTTPClient) Head(ctx context.Context, url string, headers map[string][]string) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Head(url, headers) })
}

// Options sends an authenticated OPTIONS.
func (h *HTTPClient) Options(ctx context.Context, url string, headers map[string][]string) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Options(url, headers) })
}

// Patch sends an authenticated PATCH with body.
func (h *HTTPClient) Patch(ctx context.Context, url string, headers map[string][]string, body []byte) (*Response, error) {
	return h.exec(ctx, func() (*sdk.HttpResponse, error) { return h.inner.Patch(url, headers, body) })
}

func (h *HTTPClient) exec(ctx context.Context, call func() (*sdk.HttpResponse, error)) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := call()
	if err != nil {
		return nil, errors.Join(ErrTransport, err)
	}
	defer raw.Close()
	if e := raw.Error(); e != "" {
		return nil, errors.Join(ErrTransport, errors.New(e))
	}
	return &Response{
		Status:  raw.Status(),
		Body:    raw.Body(),
		Headers: raw.Headers(),
	}, nil
}
