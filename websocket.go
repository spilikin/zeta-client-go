package zeta

import (
	"context"
	"errors"

	"github.com/gematik/zeta-client-go/internal/sdk"
)

// WSMessageType classifies a received WebSocket message.
type WSMessageType int

// WSMessageType discriminators mirror the SDK's tagged-union variants.
const (
	WSText   WSMessageType = WSMessageType(sdk.WSText)
	WSBinary WSMessageType = WSMessageType(sdk.WSBinary)
	WSClose  WSMessageType = WSMessageType(sdk.WSClose)
)

func (t WSMessageType) String() string {
	switch t {
	case WSText:
		return "Text"
	case WSBinary:
		return "Binary"
	case WSClose:
		return "Close"
	}
	return "Unknown"
}

// WSMessage is a copy of a single received WebSocket frame. The SDK-allocated
// underlying message has already been freed by the time you see this struct
// — no Close required.
type WSMessage struct {
	Type   WSMessageType
	Text   string // populated when Type == WSText
	Binary []byte // populated when Type == WSBinary
}

// WSSession is the live WebSocket session passed to a Client.WebSocket
// handler. Valid ONLY inside the handler invocation that received it; methods
// on a session captured outside the handler return ErrWSSessionClosed.
type WSSession struct {
	inner *sdk.WSSession
}

// SendText sends a UTF-8 text frame.
func (s *WSSession) SendText(text string) error { return s.inner.SendText(text) }

// SendBinary sends a binary frame.
func (s *WSSession) SendBinary(data []byte) error { return s.inner.SendBinary(data) }

// Receive blocks until the next message arrives. A returned WSMessage with
// Type == WSClose signals the session is being terminated by the server —
// stop calling Receive at that point.
func (s *WSSession) Receive() (WSMessage, error) {
	m, err := s.inner.Receive()
	if err != nil {
		return WSMessage{}, err
	}
	return WSMessage{
		Type:   WSMessageType(m.Type),
		Text:   m.Text,
		Binary: m.Binary,
	}, nil
}

// Close requests a graceful server-side close.
func (s *WSSession) Close() error { return s.inner.Close() }

// WSOptions configures a WebSocket connection.
type WSOptions struct {
	// Headers added to the WebSocket upgrade request. Typically used for
	// per-message tokens (e.g. PoPP) the resource server expects on the
	// upgrade.
	Headers map[string][]string
}

// Errors returned by Client.WebSocket and WSSession methods.
var (
	// ErrWSSessionClosed is returned by WSSession methods called after the
	// handler that produced the session has returned.
	ErrWSSessionClosed = sdk.ErrWSSessionClosed

	// ErrWSReceiveFailed is returned when the SDK's receiveNext call returns
	// NULL — typically a transport-level failure (see SDK logs for detail).
	ErrWSReceiveFailed = sdk.ErrWSReceiveFailed

	// ErrWSHandlerNotInvoked is returned by Client.WebSocket if the SDK
	// completed without ever invoking the handler — usually means the
	// connection was never established (see SDK logs for detail).
	ErrWSHandlerNotInvoked = sdk.ErrWSHandlerNotInvoked
)

// WebSocket establishes a session against url and synchronously invokes
// handler once the connection is established. The handler's WSSession is
// valid only for the duration of the handler — do not stash it in long-lived
// state; calls after the handler returns fail with ErrWSSessionClosed.
//
// Only one WebSocket call may be active per process at a time. Concurrent
// callers serialize on an internal mutex; this is an SDK ABI constraint, not
// a binding choice (see FEEDBACK_SDK.md).
//
// ctx is checked before the SDK call but not honoured during it (the SDK call
// is synchronous and not cancellable).
func (c *Client) WebSocket(ctx context.Context, url string, opts WSOptions, handler func(*WSSession)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handler == nil {
		return errors.New("zeta: WebSocket handler is required")
	}
	return c.inner.WebSocket(url, opts.Headers, func(inner *sdk.WSSession) {
		handler(&WSSession{inner: inner})
	})
}
