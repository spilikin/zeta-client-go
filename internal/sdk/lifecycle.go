package sdk

/*
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"errors"
	"fmt"
)

// ErrClientClosed is returned by any lifecycle/HTTP method invoked after Close.
var ErrClientClosed = errors.New("sdk: client is closed")

// Discover drives the SDK's discover flow (fetches resource + AS metadata,
// persists via the configured Storage). Holds the Client's mutex.
//
// The SDK collapses every failure into a single non-zero return — configure
// a Logger to capture the underlying error detail.
func (c *Client) Discover() error {
	return c.runLifecycle(opDiscover)
}

// Register runs Dynamic Client Registration against the discovered AS.
func (c *Client) Register() error {
	return c.runLifecycle(opRegister)
}

// Authenticate exchanges the SM-B/SMC-B credentials for an access + refresh token.
func (c *Client) Authenticate() error {
	return c.runLifecycle(opAuthenticate)
}

// Logout revokes tokens server-side and clears local token state. Local
// registration state (DCR response) is preserved — use Forget at the public
// layer for full reset.
func (c *Client) Logout() error {
	return c.runLifecycle(opLogout)
}

// Status returns the SDK's view of the client's progress through the
// lifecycle. The raw int matches the SDK's SdkStatus enum: 0 = NOT_REGISTERED,
// 1 = REGISTERED_NO_VALID_TOKENS, 2 = HAS_REFRESH_TOKEN, 3 = HAS_ACCESS_AND_REFRESH_TOKEN.
// Returns -1 mapped to a non-nil error when the SDK cannot determine status.
func (c *Client) Status() (int, error) {
	if c.closed.Load() {
		return 0, ErrClientClosed
	}
	c.mu.Lock()
	rc := C.ZetaSdk_status(c.bundle.client)
	c.mu.Unlock()
	r := int(rc)
	if r < 0 || r > 3 {
		return 0, errors.New("sdk: status unavailable")
	}
	return r, nil
}

type lifecycleOp int

const (
	opDiscover lifecycleOp = iota
	opRegister
	opAuthenticate
	opLogout
)

func (op lifecycleOp) name() string {
	switch op {
	case opDiscover:
		return "discover"
	case opRegister:
		return "register"
	case opAuthenticate:
		return "authenticate"
	case opLogout:
		return "logout"
	}
	return "unknown"
}

func (c *Client) runLifecycle(op lifecycleOp) error {
	if c.closed.Load() {
		return ErrClientClosed
	}
	c.mu.Lock()
	var rc C.int
	switch op {
	case opDiscover:
		rc = C.ZetaSdk_discover(c.bundle.client)
	case opRegister:
		rc = C.ZetaSdk_register(c.bundle.client)
	case opAuthenticate:
		rc = C.ZetaSdk_authenticate(c.bundle.client)
	case opLogout:
		rc = C.ZetaSdk_logout(c.bundle.client)
	}
	c.mu.Unlock()
	if rc != 0 {
		return fmt.Errorf("sdk: %s failed (rc=%d)", op.name(), int(rc))
	}
	return nil
}
