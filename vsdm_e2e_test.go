//go:build e2e

// vsdm e2e: lifecycle (Discover → Register → Authenticate) against a
// ZETA-protected resource that requires ASL attestation. Stops at
// Authenticate — anything past it (HTTP request, WS) triggers the
// FEEDBACK_SDK-tracked SDK abort and isn't worth running until Path 3 (ASL)
// lands.
//
// Run with:
//   ZETA_E2E=1 VSDM_URL=https://vsdm-dev.tk.de \
//   SMB_KEYSTORE_FILE=/tmp/smcb.p12 SMB_KEYSTORE_ALIAS=alias SMB_KEYSTORE_PASSWORD=00 \
//   STORAGE_AES_KEY=<base64-32bytes> \
//   go test -tags e2e -count=1 -v -run TestE2E_VSDM ./...

package zeta_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gematik/zeta-client-go"
)

const envVSDMURL = "VSDM_URL"

var vsdmScopes = []string{"zero:audience"}

func vsdmURL(t *testing.T) string {
	t.Helper()
	requireE2E(t)
	requireEnv(t, envVSDMURL)
	return os.Getenv(envVSDMURL)
}

// TestE2E_VSDM_Lifecycle drives Discover → Register → Authenticate and
// reports the post-Authenticate Status. Doesn't go further — see
// FEEDBACK_SDK.md for the ASL / SIGABRT issues that block past-auth ops.
func TestE2E_VSDM_Lifecycle(t *testing.T) {
	url := vsdmURL(t)
	cfg := baseConfig(t)
	cfg.ResourceURL = url
	cfg.Scopes = vsdmScopes

	client, err := zeta.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	pre, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("initial Status: %v", err)
	}
	t.Logf("initial status: %s", pre)

	if err := client.Discover(ctx); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Log("discover ok")

	if err := client.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Log("register ok")

	if err := client.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	t.Log("authenticate ok")

	post, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("post-auth Status: %v", err)
	}
	t.Logf("post-auth status: %s", post)
	if post != zeta.StatusHasAccessAndRefreshToken {
		t.Errorf("post-auth status = %s, want %s — Authenticate likely returned 0 despite token-exchange failure; see SDK logs", post, zeta.StatusHasAccessAndRefreshToken)
	}
}

// TestE2E_VSDM_HTTPRequest authenticates, then performs an HTTP GET through
// the SDK's HTTPClient (which adds the bearer token + DPoP automatically).
// VSDM's resource server demands a PoPP token in the PoPP header; tests are
// skipped if POPP_TOKEN env var isn't set.
func TestE2E_VSDM_HTTPRequest(t *testing.T) {
	requireEnv(t, "POPP_TOKEN")
	popp := os.Getenv("POPP_TOKEN")

	client := authedClient(t, vsdmURL(t), vsdmScopes...)
	httpClient, err := client.HTTPClient()
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	defer httpClient.Close()

	headers := map[string][]string{
		"PoPP":          {popp},
		"Accept":        {"application/fhir+json; application/json; application/octet-stream"},
		"If-None-Match": {`"0000000000000000000000000000000000000000000000000000000000000000"`},
	}
	ctx := context.Background()
	for _, path := range []string{"vsdservice/v1/vsdmbundle?profileVersion=1.0"} {
		t.Run("GET_"+path, func(t *testing.T) {
			resp, err := httpClient.Get(ctx, path, headers)
			if err != nil {
				t.Fatalf("GET %q: %v", path, err)
			}
			body := resp.Body
			if len(body) > 300 {
				body = body[:300]
			}
			t.Logf("GET %q -> %d (body[:300]=%q)", path, resp.Status, string(body))
			if resp.Status == 401 || resp.Status == 403 {
				t.Errorf("GET %q returned %d — bearer token not accepted", path, resp.Status)
			}
		})
	}
}

// TestE2E_VSDM_DiscoverErrorWrapsSentinel guards the errors.Is contract:
// Discover against an unreachable host must surface ErrDiscoverFailed.
func TestE2E_VSDM_DiscoverErrorWrapsSentinel(t *testing.T) {
	requireE2E(t)
	cfg := baseConfig(t)
	cfg.ResourceURL = "https://invalid-host.example.invalid/"
	cfg.Scopes = vsdmScopes

	client, err := zeta.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	err = client.Discover(context.Background())
	if err == nil {
		t.Fatal("expected Discover against unreachable host to fail")
	}
	if !errors.Is(err, zeta.ErrDiscoverFailed) {
		t.Errorf("expected errors.Is(err, ErrDiscoverFailed), got %v", err)
	}
}
