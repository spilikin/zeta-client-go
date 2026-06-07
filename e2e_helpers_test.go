//go:build e2e

// Shared helpers for the e2e suites. Gating is per-suite — see vsdm_e2e_test.go
// and popp_e2e_test.go for the env vars each demands.

package zeta_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gematik/zeta-client-go"
)

// debugLogger captures every SDK log line. The Go-side Logger vtable is
// wired with logLevel = ZETA_LOG_LEVEL_DEBUG so the SDK forwards everything;
// stderr lets `go test -v` interleave it with the test output and lets bash
// redirection grep for what we care about.
type debugLogger struct{}

func (debugLogger) Log(level zeta.LogLevel, tag, message string) {
	lvl := levelName(level)
	if tag == "" {
		fmt.Fprintf(os.Stderr, "[sdk %s] %s\n", lvl, message)
		return
	}
	fmt.Fprintf(os.Stderr, "[sdk %s %s] %s\n", lvl, tag, message)
}

func levelName(l zeta.LogLevel) string {
	switch l {
	case zeta.LogLevelDebug:
		return "DEBUG"
	case zeta.LogLevelInfo:
		return "INFO"
	case zeta.LogLevelWarn:
		return "WARN"
	case zeta.LogLevelError:
		return "ERROR"
	}
	return fmt.Sprintf("L%d", int(l))
}

// Env vars shared across all e2e suites.
const (
	envE2E             = "ZETA_E2E"
	envKeystoreFile    = "SMB_KEYSTORE_FILE"
	envKeystoreAlias   = "SMB_KEYSTORE_ALIAS"
	envKeystorePass    = "SMB_KEYSTORE_PASSWORD"
	envStorageAESKey   = "STORAGE_AES_KEY"
	envRequiredRoleOID = "REQUIRED_ROLE_OID"
	envInsecure        = "ZETA_INSECURE"
)

// requireE2E skips the test unless ZETA_E2E=1.
func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv(envE2E) != "1" {
		t.Skipf("set %s=1 to run e2e tests", envE2E)
	}
}

// requireEnv skips the test if any named env var is empty. Use in suite-level
// helpers to bail before constructing a Client.
func requireEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if os.Getenv(k) == "" {
			t.Skipf("e2e: env var %s not set", k)
		}
	}
}

// baseConfig returns a Config populated with the shared p12 + storage env
// vars. Caller fills ResourceURL, Scopes, and any other suite-specific fields.
func baseConfig(t *testing.T) zeta.Config {
	t.Helper()
	requireEnv(t, envKeystoreFile, envKeystoreAlias, envKeystorePass, envStorageAESKey)
	return zeta.Config{
		ProductID:      "zeta-client-go-e2e",
		ProductVersion: "0.0.1",
		ClientName:     "go-e2e",
		Auth: zeta.KeystoreAuth{
			File:     os.Getenv(envKeystoreFile),
			Alias:    os.Getenv(envKeystoreAlias),
			Password: os.Getenv(envKeystorePass),
		},
		StorageAESKey:         os.Getenv(envStorageAESKey),
		RequiredRoleOID:       os.Getenv(envRequiredRoleOID),
		ASLProd:               false,
		InsecureSkipTLSVerify: os.Getenv(envInsecure) == "1",
		Logger:                debugLogger{},
	}
}

// authedClient builds a Client against resourceURL with the given scopes,
// runs the full lifecycle, and registers t.Cleanup for client.Close. Returns
// the live, authenticated client. Fails the test if any step errors.
//
// Each test that wants an authenticated client calls this; if the lifecycle
// fails the test surfaces the failure rather than proceeding into the dead
// part of the SDK surface. Scopes are server-specific — pass what the AS
// supports (vsdm uses zero:audience, popp uses popp).
func authedClient(t *testing.T, resourceURL string, scopes ...string) *zeta.Client {
	t.Helper()
	cfg := baseConfig(t)
	cfg.ResourceURL = resourceURL
	cfg.Scopes = scopes
	client, err := zeta.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if err := client.Discover(ctx); err != nil {
		t.Fatalf("Discover %s: %v", resourceURL, err)
	}
	if err := client.Register(ctx); err != nil {
		t.Fatalf("Register %s: %v", resourceURL, err)
	}
	if err := client.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate %s: %v", resourceURL, err)
	}
	return client
}
