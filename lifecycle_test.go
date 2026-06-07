package zeta_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gematik/zeta-client-go"
)

func TestStatus_String(t *testing.T) {
	cases := []struct {
		status zeta.Status
		want   string
	}{
		{zeta.StatusNotRegistered, "NotRegistered"},
		{zeta.StatusRegisteredNoValidTokens, "RegisteredNoValidTokens"},
		{zeta.StatusHasRefreshToken, "HasRefreshToken"},
		{zeta.StatusHasAccessAndRefreshToken, "HasAccessAndRefreshToken"},
		{zeta.Status(99), "Status(99)"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(c.status), got, c.want)
		}
	}
}

// The SDK's SdkStatus enum (from ZetaSdkHelpers.kt) maps:
//
//	NOT_REGISTERED                  → 0
//	REGISTERED_NO_VALID_TOKENS      → 1
//	HAS_REFRESH_TOKEN               → 2
//	HAS_ACCESS_AND_REFRESH_TOKEN    → 3
//
// Drift here would silently report the wrong state to users.
func TestStatus_EnumValuesMatchSDK(t *testing.T) {
	cases := []struct {
		status zeta.Status
		want   int
	}{
		{zeta.StatusNotRegistered, 0},
		{zeta.StatusRegisteredNoValidTokens, 1},
		{zeta.StatusHasRefreshToken, 2},
		{zeta.StatusHasAccessAndRefreshToken, 3},
	}
	for _, c := range cases {
		if int(c.status) != c.want {
			t.Errorf("%s = %d, want %d", c.status, int(c.status), c.want)
		}
	}
}

// Sentinels must be distinct values (not the same nil-error, not duplicated)
// and each must nudge users toward configuring a Logger so they can see why
// the SDK collapsed the failure into a single rc.
func TestLifecycleSentinels(t *testing.T) {
	sentinels := []error{
		zeta.ErrDiscoverFailed,
		zeta.ErrRegisterFailed,
		zeta.ErrAuthenticateFailed,
		zeta.ErrLogoutFailed,
		zeta.ErrStatusUnavailable,
	}
	seen := map[string]bool{}
	for i, e := range sentinels {
		if e == nil {
			t.Errorf("sentinel %d is nil", i)
			continue
		}
		if seen[e.Error()] {
			t.Errorf("sentinel %d duplicates message %q", i, e.Error())
		}
		seen[e.Error()] = true
		if !strings.Contains(e.Error(), "Logger") {
			t.Errorf("sentinel %q should mention Logger to nudge users toward observability", e)
		}
	}
}

// Document the errors.Is + errors.Join pattern callers should use.
func TestLifecycleSentinels_ErrorsIsWrapped(t *testing.T) {
	innerCause := errors.New("rc=-1; some sdk detail")
	joined := errors.Join(zeta.ErrDiscoverFailed, innerCause)

	if !errors.Is(joined, zeta.ErrDiscoverFailed) {
		t.Errorf("errors.Is(joined, ErrDiscoverFailed) = false, want true")
	}
	if !errors.Is(joined, innerCause) {
		t.Errorf("errors.Is(joined, innerCause) = false, want true")
	}
	wrapped := fmt.Errorf("at step X: %w", zeta.ErrAuthenticateFailed)
	if !errors.Is(wrapped, zeta.ErrAuthenticateFailed) {
		t.Errorf("errors.Is(wrapped, ErrAuthenticateFailed) = false, want true")
	}
}
