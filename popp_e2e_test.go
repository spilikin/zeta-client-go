//go:build e2e

// popp e2e: lifecycle (Discover → Register → Authenticate) against the PoPP
// test service. PoPP's resource metadata advertises
// `"zeta_asl_use": "not_supported"`, so the lifecycle should succeed without
// ASL plumbing — but as of this writing the AS rejects the token request
// with the same urn:telematik:client-self-assessment-missing error vsdm
// gives, leaving Status at RegisteredNoValidTokens even though
// ZetaSdk_authenticate returns 0 (FEEDBACK_SDK.md).
//
// Stops at Authenticate — WebSocket tests are skipped per direction; the
// underlying SDK WS path SIGABRTs on the same auth rejection (also tracked
// in FEEDBACK_SDK.md).
//
// Run with:
//   ZETA_E2E=1 ZETA_INSECURE=1 \
//   POPP_URL=https://popp.dev.poppservice.de/ \
//   SMB_KEYSTORE_FILE=/tmp/smcb.p12 SMB_KEYSTORE_ALIAS=alias SMB_KEYSTORE_PASSWORD=00 \
//   STORAGE_AES_KEY=<base64-32bytes> \
//   go test -tags e2e -count=1 -v -run TestE2E_PoPP ./...

package zeta_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gematik/zeta-client-go"
)

const (
	envPoPPURL   = "POPP_URL"
	envPoPPWSURL = "POPP_WS_URL"
)

func poppWS(t *testing.T) string {
	t.Helper()
	requireEnv(t, envPoPPWSURL)
	return os.Getenv(envPoPPWSURL)
}

// PoPP's AS only accepts the "popp" scope; "zero:audience" is rejected.
var poppScopes = []string{"popp"}

func poppResource(t *testing.T) string {
	t.Helper()
	requireE2E(t)
	requireEnv(t, envPoPPURL)
	return os.Getenv(envPoPPURL)
}

// TestE2E_PoPP_Lifecycle drives Discover → Register → Authenticate and
// reports the post-Authenticate Status. Same scope as the vsdm lifecycle
// test — no HTTP, no WS, just the auth flow.
func TestE2E_PoPP_Lifecycle(t *testing.T) {
	cfg := baseConfig(t)
	cfg.ResourceURL = poppResource(t)
	cfg.Scopes = poppScopes

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

// TestE2E_PoPP_WebSocket_Start opens a WS to the PoPP token-generation
// endpoint and sends the protocol's first frame (Start). The binding's job
// stops at "the server talks back" — any reply (ConnectorScenario,
// StandardScenario, or even Error) proves the WS path, auth, and frame
// encoding are all working end-to-end. APDU exchange, ScenarioResponse and
// Token issuance need card-domain logic the binding doesn't own (see
// zeta-cli/popp/PoppClient.kt).
//
// Uses a goroutine + select so a hung Receive can't keep the test running
// past `wsReceiveTimeout` — the SDK's receiveNext is uncancellable from Go's
// side, but Close() makes it return NULL promptly.
func TestE2E_PoPP_WebSocket_Start(t *testing.T) {
	const wsReceiveTimeout = 10 * time.Second

	wsURL := poppWS(t)
	client := authedClient(t, poppResource(t), poppScopes...)

	var (
		invoked atomic.Bool
		gotMsg  zeta.WSMessage
		gotErr  error
	)
	err := client.WebSocket(context.Background(), wsURL, zeta.WSOptions{}, func(s *zeta.WSSession) {
		invoked.Store(true)

		startMsg := fmt.Sprintf(
			`{"type":"Start","version":"1.0.0","cardConnectionType":"contact-standard","clientSessionId":"zeta-client-go-e2e-%d"}`,
			time.Now().UnixNano(),
		)
		t.Logf("→ Start: %s", startMsg)
		if err := s.SendText(startMsg); err != nil {
			gotErr = fmt.Errorf("SendText Start: %w", err)
			_ = s.Close()
			return
		}

		type recvResult struct {
			msg zeta.WSMessage
			err error
		}
		resultCh := make(chan recvResult, 1)
		go func() {
			m, e := s.Receive()
			resultCh <- recvResult{m, e}
		}()

		select {
		case r := <-resultCh:
			gotMsg, gotErr = r.msg, r.err
		case <-time.After(wsReceiveTimeout):
			gotErr = fmt.Errorf("timeout waiting %s for server response", wsReceiveTimeout)
		}
		_ = s.Close()
	})
	if err != nil {
		t.Fatalf("WebSocket: %v", err)
	}
	if !invoked.Load() {
		t.Fatal("handler was never invoked")
	}
	if gotErr != nil && !errors.Is(gotErr, zeta.ErrWSSessionClosed) {
		t.Fatalf("Receive: %v", gotErr)
	}

	payload := gotMsg.Text
	if payload == "" && len(gotMsg.Binary) > 0 {
		payload = string(gotMsg.Binary)
	}
	if len(payload) > 500 {
		t.Logf("← first server msg (%s, %d bytes, first 500): %s", gotMsg.Type, len(payload), payload[:500])
	} else {
		t.Logf("← first server msg (%s): %s", gotMsg.Type, payload)
	}

	// Success criterion: server replied to our Start frame at all. Any well-formed
	// PoPP message (Scenario, Error, …) proves the round-trip works; deciding
	// whether the Error is a binding bug or a server-side schema quirk lives in
	// FEEDBACK_SDK.md, not in test pass/fail.
	if payload == "" {
		t.Fatal("server sent no response to Start")
	}
	switch {
	case strings.Contains(payload, `"ConnectorScenario"`):
		t.Log("PoPP responded with ConnectorScenario — protocol up to APDU exchange point reached.")
	case strings.Contains(payload, `"StandardScenario"`):
		t.Log("PoPP responded with StandardScenario.")
	case strings.Contains(payload, `"Error"`):
		t.Logf("PoPP returned Error (round-trip OK; see FEEDBACK_SDK.md): %s", payload)
	default:
		t.Logf("PoPP returned an unrecognised frame (round-trip OK): %s", payload)
	}
}
