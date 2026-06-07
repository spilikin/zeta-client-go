package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

// WSMessageType mirrors ZetaSdk_WsMessageType: WS_TEXT=0, WS_BINARY=1, WS_CLOSE=2.
type WSMessageType int

const (
	WSText   WSMessageType = 0
	WSBinary WSMessageType = 1
	WSClose  WSMessageType = 2
)

// WSMessage is a decoded copy of an SDK-allocated ZetaSdk_WSMessage. The
// underlying C message is destroyed before WSMessage is returned to the
// caller; users don't see the C-side lifecycle.
type WSMessage struct {
	Type   WSMessageType
	Text   string // populated when Type == WSText
	Binary []byte // populated when Type == WSBinary
}

// WSSession wraps an SDK-managed WebSocket session. Valid only inside the
// handler invocation that received it; methods called after the handler
// returns produce ErrWSSessionClosed.
type WSSession struct {
	ptr    unsafe.Pointer // ZetaSdk_WSSession*; SDK-owned, no Close from our side
	closed *atomic.Bool
	mu     sync.Mutex // serializes send/receive on a single session
}

// Errors returned by WebSocket methods.
var (
	ErrWSSessionClosed     = errors.New("sdk: websocket session is closed (handler has returned)")
	ErrWSReceiveFailed     = errors.New("sdk: websocket receiveNext returned NULL")
	ErrWSHandlerNotInvoked = errors.New("sdk: websocket handler was not invoked (connection not established)")
)

// SendText sends a UTF-8 text frame.
func (s *WSSession) SendText(text string) error {
	if s.closed.Load() {
		return ErrWSSessionClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(text) == 0 {
		C.ZetaSdk_WSSession_sendText(s.ptr, nil, 0)
		return nil
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	C.ZetaSdk_WSSession_sendText(s.ptr, unsafe.Pointer(cText), C.int(len(text)))
	return nil
}

// SendBinary sends a binary frame.
func (s *WSSession) SendBinary(data []byte) error {
	if s.closed.Load() {
		return ErrWSSessionClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(data) == 0 {
		C.ZetaSdk_WSSession_sendBinary(s.ptr, nil, 0)
		return nil
	}
	C.ZetaSdk_WSSession_sendBinary(s.ptr, unsafe.Pointer(&data[0]), C.int(len(data)))
	return nil
}

// Receive blocks until the SDK delivers the next message. The returned
// WSMessage holds copies of the C-side payload; the underlying SDK message is
// destroyed before Receive returns. Type == WSClose signals the session is
// being terminated by the server.
func (s *WSSession) Receive() (WSMessage, error) {
	if s.closed.Load() {
		return WSMessage{}, ErrWSSessionClosed
	}
	s.mu.Lock()
	cMsg := C.ZetaSdk_WSSession_receiveNext(s.ptr)
	s.mu.Unlock()
	if cMsg == nil {
		return WSMessage{}, ErrWSReceiveFailed
	}
	defer C.ZetaSdk_WSMessage_destroy(cMsg)
	msgPtr := (*C.ZetaSdk_WSMessage)(cMsg)

	msg := WSMessage{Type: WSMessageType(C.zeta_wsmsg_type(msgPtr))}
	switch msg.Type {
	case WSText:
		var size C.int
		ptr := C.zeta_wsmsg_text(msgPtr, &size)
		if ptr != nil && size > 0 {
			msg.Text = C.GoStringN(ptr, size)
		}
	case WSBinary:
		var size C.int
		ptr := C.zeta_wsmsg_binary(msgPtr, &size)
		if ptr != nil && size > 0 {
			msg.Binary = C.GoBytes(unsafe.Pointer(ptr), size)
		}
	}
	return msg, nil
}

// Close requests a graceful server-side close of the session.
func (s *WSSession) Close() error {
	if s.closed.Load() {
		return ErrWSSessionClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	C.ZetaSdk_WSSession_close(s.ptr)
	return nil
}

// --- handler bridging ---------------------------------------------------------
//
// ZetaSdk_WSHandler has no ctx parameter, so we can't pass a per-call closure
// through it. We serialize WebSocket calls process-wide via wsCallMu and pass
// the user closure through the package-level wsActiveHandler variable. See
// FEEDBACK_SDK.md — adding ctx to the SDK signature would lift this.

var (
	wsCallMu        sync.Mutex
	wsActiveHandler func(*WSSession)
)

// WebSocket establishes a session against url and invokes handler synchronously
// once the connection is up. The handler's WSSession is valid only for the
// handler's lifetime; methods called against it after the handler returns
// produce ErrWSSessionClosed.
//
// Only one WebSocket call may be active per process at a time (SDK ABI
// limitation — see FEEDBACK_SDK.md). Concurrent callers serialize on a
// package mutex.
func (c *Client) WebSocket(url string, headers map[string][]string, handler func(*WSSession)) error {
	if c.closed.Load() {
		return ErrClientClosed
	}
	if handler == nil {
		return errors.New("sdk: WebSocket handler is required")
	}

	wsCallMu.Lock()
	defer wsCallMu.Unlock()

	var invoked atomic.Bool
	wsActiveHandler = func(s *WSSession) {
		invoked.Store(true)
		handler(s)
	}
	defer func() { wsActiveHandler = nil }()

	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	headerArr, headerCount, freeHeaders := buildHeaders(headers)
	defer freeHeaders()

	c.mu.Lock()
	C.zeta_ws_invoke(c.bundle.client, cURL, C.int(len(url)), unsafe.Pointer(headerArr), headerCount)
	c.mu.Unlock()

	if !invoked.Load() {
		return ErrWSHandlerNotInvoked
	}
	return nil
}
