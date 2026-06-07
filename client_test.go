package zeta_test

import (
	"errors"
	"testing"

	"github.com/gematik/zeta-client-go"
)

func TestNewClient_SurfacesValidationErrors(t *testing.T) {
	_, err := zeta.NewClient(zeta.Config{}) // every required field missing
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	for _, want := range []error{
		zeta.ErrMissingResourceURL,
		zeta.ErrMissingProductID,
		zeta.ErrMissingAuth,
		zeta.ErrEmptyScopes,
	} {
		if !errors.Is(err, want) {
			t.Errorf("expected aggregated error to include %v, got %v", want, err)
		}
	}
}
