package sdk

/*
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"sync/atomic"
	"unsafe"
)

//export go_ws_handler
func go_ws_handler(session unsafe.Pointer) {
	h := wsActiveHandler
	if h == nil {
		// Defensive: SDK called the handler with no active registration.
		// Shouldn't happen — Client.WebSocket sets the handler before the
		// SDK call and clears it after. If we get here something is wrong;
		// returning leaves the SDK to clean up the session.
		return
	}
	s := &WSSession{
		ptr:    session,
		closed: new(atomic.Bool),
	}
	h(s)
	// The session pointer is SDK-managed; we just stop using it. Flip the
	// closed flag so any goroutine that captured s and outlived the handler
	// sees a clean error from subsequent method calls.
	s.closed.Store(true)
}
