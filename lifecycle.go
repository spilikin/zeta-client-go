package zeta

import (
	"context"
	"errors"
	"fmt"
)

// Status mirrors the SDK's SdkStatus enum — the client's progress through the
// register → authenticate → token-refresh lifecycle.
type Status int

const (
	// StatusNotRegistered — the client has not yet completed DCR with the AS.
	StatusNotRegistered Status = 0
	// StatusRegisteredNoValidTokens — DCR is done but no usable tokens are cached.
	StatusRegisteredNoValidTokens Status = 1
	// StatusHasRefreshToken — a refresh token is cached; an access token can be obtained without re-auth.
	StatusHasRefreshToken Status = 2
	// StatusHasAccessAndRefreshToken — fully authenticated, ready for HTTP calls.
	StatusHasAccessAndRefreshToken Status = 3
)

func (s Status) String() string {
	switch s {
	case StatusNotRegistered:
		return "NotRegistered"
	case StatusRegisteredNoValidTokens:
		return "RegisteredNoValidTokens"
	case StatusHasRefreshToken:
		return "HasRefreshToken"
	case StatusHasAccessAndRefreshToken:
		return "HasAccessAndRefreshToken"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Lifecycle sentinel errors. The SDK returns "failed" as an opaque -1 for each
// of these calls — the underlying detail is written to the configured Logger.
// Wire cfg.Logger to see WHY a call failed.
var (
	ErrDiscoverFailed     = errors.New("zeta: discover failed (configure Logger to see SDK detail)")
	ErrRegisterFailed     = errors.New("zeta: register failed (configure Logger to see SDK detail)")
	ErrAuthenticateFailed = errors.New("zeta: authenticate failed (configure Logger to see SDK detail)")
	ErrLogoutFailed       = errors.New("zeta: logout failed (configure Logger to see SDK detail)")
	ErrStatusUnavailable  = errors.New("zeta: status unavailable (configure Logger to see SDK detail)")
)

// Discover runs the ZETA discover flow against cfg.ResourceURL — fetches the
// protected-resource metadata and the authorization-server metadata, persists
// them via the configured Storage. Must succeed before Register.
//
// ctx is checked for cancellation before the SDK call; the SDK call itself is
// synchronous and not cancellable.
func (c *Client) Discover(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.inner.Discover(); err != nil {
		return errors.Join(ErrDiscoverFailed, err)
	}
	return nil
}

// Register completes Dynamic Client Registration against the discovered AS.
// Discover must have succeeded first; the registration response is persisted
// via the configured Storage.
func (c *Client) Register(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.inner.Register(); err != nil {
		return errors.Join(ErrRegisterFailed, err)
	}
	return nil
}

// Authenticate exchanges the configured Auth (KeystoreAuth p12, ConnectorAuth
// SOAP, or CustomConnectorAuth vtable) for an access + refresh token pair.
// Register must have succeeded first.
func (c *Client) Authenticate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.inner.Authenticate(); err != nil {
		return errors.Join(ErrAuthenticateFailed, err)
	}
	return nil
}

// Logout revokes the cached tokens server-side and clears local token state.
// Registration state is preserved — a subsequent Authenticate will get fresh
// tokens without re-registering. For a full reset, use Forget.
func (c *Client) Logout(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.inner.Logout(); err != nil {
		return errors.Join(ErrLogoutFailed, err)
	}
	return nil
}

// Status reports the client's current progress through the lifecycle. Cheap;
// reads cached state via the configured Storage.
func (c *Client) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	raw, err := c.inner.Status()
	if err != nil {
		return 0, errors.Join(ErrStatusUnavailable, err)
	}
	return Status(raw), nil
}

// Forget revokes tokens server-side AND wipes local storage state — full
// reset, as if the client had never been registered. Equivalent to Logout +
// Storage.Clear when a custom Storage is configured.
//
// When cfg.Storage is nil (using the SDK's platform-default backend), Forget
// can only call Logout — the SDK's default storage retains registration state
// that this binding cannot wipe through the current C ABI. For true "forget"
// semantics in that case, delete the SDK's storage location out-of-band, or
// supply a custom Storage.
//
// Errors from both steps are joined so callers see every failure with
// errors.Is.
func (c *Client) Forget(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var errs []error
	if err := c.inner.Logout(); err != nil {
		errs = append(errs, ErrLogoutFailed, err)
	}
	if c.storage != nil {
		if err := c.storage.Clear(); err != nil {
			errs = append(errs, fmt.Errorf("zeta: storage clear failed: %w", err))
		}
	}
	return errors.Join(errs...)
}
