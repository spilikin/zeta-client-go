// PoPP commands mirror the equivalents in ../zeta-cli/cli/.../popp/.
//
// Today only `popp kartos` is wired — drives the PoPP service through the
// Standard scenario using a `kartos` smartcard simulator child process. The
// connector flow (signed scenarios from a real Konnektor) can be added later
// alongside this file.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	zeta "github.com/gematik/zeta-client-go"
)

const defaultPoppServiceURL = "wss://popp.dev.poppservice.de/popp/practitioner/api/v1/token-generation-ehc"

func runPopp(args []string) error {
	if len(args) < 1 {
		return errors.New("popp: subcommand required (try: kartos)")
	}
	switch args[0] {
	case "kartos":
		return runPoppKartos(args[1:])
	default:
		return fmt.Errorf("popp: unknown subcommand %q (try: kartos)", args[0])
	}
}

func runPoppKartos(args []string) error {
	fs := flag.NewFlagSet("popp kartos", flag.ContinueOnError)
	var image, kartosBin, serviceURL string
	fs.StringVar(&image, "image", "", "XML card image to load into the kartos smartcard simulator (required)")
	fs.StringVar(&image, "i", "", "shorthand for --image")
	fs.StringVar(&kartosBin, "kartos-bin", envDefault("ZETA_KARTOS_BIN", "kartos"), "kartos executable (env: ZETA_KARTOS_BIN)")
	fs.StringVar(&serviceURL, "service-url", envDefault("ZETA_POPP_SERVICE_URL", defaultPoppServiceURL), "popp service WebSocket URL (env: ZETA_POPP_SERVICE_URL)")
	cf := registerCommonFlags(fs)
	if err := fs.Parse(reorderFlagsFirst(fs, args)); err != nil {
		return err
	}
	if image == "" {
		return errors.New("--image PATH is required")
	}
	if _, err := os.Stat(image); err != nil {
		return fmt.Errorf("--image %s: %w", image, err)
	}
	// Force the PoPP-only scope; the AS rejects anything else.
	cf.scopes = stringSlice{"popp"}
	if err := cf.validate(); err != nil {
		return err
	}
	parsed, err := url.Parse(serviceURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid --service-url: %s", serviceURL)
	}
	resourceOrigin := "https://" + parsed.Host + "/"

	storage, err := openStorage(profilePath(cf.profile))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	cfg := cf.buildConfig(resourceOrigin)
	cfg.Storage = storage
	client, err := zeta.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}
	defer client.Close()

	kartos, err := spawnKartos(kartosBin, image)
	if err != nil {
		return err
	}
	defer func() { _ = kartos.Close() }()

	var token string
	wsErr := client.WebSocket(context.Background(), serviceURL, zeta.WSOptions{}, func(s *zeta.WSSession) {
		token, err = runPoppStandard(s, kartos)
	})
	if err != nil {
		return err
	}
	if wsErr != nil {
		return wsErr
	}
	if token == "" {
		return errors.New("popp: websocket closed without a Token frame")
	}
	fmt.Println(token)
	return nil
}

// runPoppStandard drives the Standard scenario: Start → StandardScenario →
// (kartos exchange) → ScenarioResponse → ... → Token. Mirrors zeta-cli's
// PoppClient.runStandardScenario.
func runPoppStandard(s *zeta.WSSession, kartos *kartosProcess) (string, error) {
	clientSessionID := fmt.Sprintf("geta-popp-kartos-%d", time.Now().UnixNano())
	startMsg := poppStartMessage{
		Type:               "Start",
		Version:            "1.0.0",
		CardConnectionType: "contact-standard",
		ClientSessionID:    clientSessionID,
	}
	if err := sendJSON(s, startMsg); err != nil {
		return "", fmt.Errorf("send Start: %w", err)
	}
	for {
		raw, err := recvWithTimeout(s, 15*time.Second)
		if err != nil {
			return "", err
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			return "", fmt.Errorf("parse server frame: %w (raw=%s)", err, raw)
		}
		switch probe.Type {
		case "StandardScenario":
			var msg poppStandardScenario
			if err := json.Unmarshal([]byte(raw), &msg); err != nil {
				return "", fmt.Errorf("parse StandardScenario: %w", err)
			}
			responses := make([]string, 0, len(msg.Steps))
			for _, step := range msg.Steps {
				resp, err := kartos.exchange(step.CommandAPDU)
				if err != nil {
					return "", fmt.Errorf("kartos exchange for %s: %w", step.CommandAPDU, err)
				}
				responses = append(responses, resp)
			}
			if err := sendJSON(s, poppScenarioResponse{Type: "ScenarioResponse", Steps: responses}); err != nil {
				return "", fmt.Errorf("send ScenarioResponse: %w", err)
			}
		case "Token":
			var msg poppToken
			if err := json.Unmarshal([]byte(raw), &msg); err != nil {
				return "", fmt.Errorf("parse Token: %w", err)
			}
			return msg.Token, nil
		case "Error":
			var msg poppError
			_ = json.Unmarshal([]byte(raw), &msg)
			return "", fmt.Errorf("popp server error: %s: %s", msg.ErrorCode, msg.ErrorDetail)
		case "ConnectorScenario":
			return "", errors.New("popp server picked Connector flow but kartos drives Standard only")
		default:
			return "", fmt.Errorf("unexpected server frame type %q: %s", probe.Type, raw)
		}
	}
}

func sendJSON(s *zeta.WSSession, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.SendText(string(b))
}

func recvWithTimeout(s *zeta.WSSession, d time.Duration) (string, error) {
	type recvResult struct {
		msg zeta.WSMessage
		err error
	}
	ch := make(chan recvResult, 1)
	go func() { m, e := s.Receive(); ch <- recvResult{m, e} }()
	select {
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		if r.msg.Text == "" && len(r.msg.Binary) > 0 {
			return string(r.msg.Binary), nil
		}
		return r.msg.Text, nil
	case <-time.After(d):
		_ = s.Close()
		return "", fmt.Errorf("popp: no server frame within %s", d)
	}
}

// --- PoPP wire types (subset of zeta-cli's PoppMessages.kt) ------------------

type poppStartMessage struct {
	Type               string `json:"type"`
	Version            string `json:"version"`
	CardConnectionType string `json:"cardConnectionType"`
	ClientSessionID    string `json:"clientSessionId"`
}

type poppStandardScenario struct {
	Type            string             `json:"type"`
	Version         string             `json:"version"`
	ClientSessionID string             `json:"clientSessionId"`
	SequenceCounter int                `json:"sequenceCounter"`
	TimeSpan        int                `json:"timeSpan"`
	Steps           []poppScenarioStep `json:"steps"`
}

type poppScenarioStep struct {
	CommandAPDU         string   `json:"commandApdu"`
	ExpectedStatusWords []string `json:"expectedStatusWords"`
}

type poppScenarioResponse struct {
	Type  string   `json:"type"`
	Steps []string `json:"steps"`
}

type poppToken struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type poppError struct {
	Type        string `json:"type"`
	ErrorCode   string `json:"errorCode"`
	ErrorDetail string `json:"errorDetail"`
}

// --- kartos subprocess -------------------------------------------------------

type kartosProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func spawnKartos(executable, image string) (*kartosProcess, error) {
	cmd := exec.Command(executable, "pipe", image)
	// Go 1.19+ stores exec.ErrDot on cmd.Err when the binary was resolved
	// from the current directory rather than $PATH, and cmd.Start refuses
	// to run it. The user named this binary explicitly via --kartos-bin
	// (default "kartos") — accepting that input IS consent to run it.
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("kartos stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("kartos stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("kartos stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w (is it on PATH?)", executable, err)
	}
	// Route kartos stderr through slog at Debug so its noise (e.g. the
	// "GET DATA tag not registered" warning) only surfaces with --verbose.
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			slog.Default().Debug(sc.Text(), slog.String("source", "kartos"))
		}
	}()
	return &kartosProcess{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

func (k *kartosProcess) exchange(commandAPDUHex string) (string, error) {
	if _, err := io.WriteString(k.stdin, commandAPDUHex+"\n"); err != nil {
		return "", fmt.Errorf("kartos stdin write (%s): %w", k.stateDescription(), err)
	}
	line, err := k.stdout.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("kartos stdout read (%s): %w", k.stateDescription(), err)
	}
	return strings.TrimSpace(line), nil
}

func (k *kartosProcess) stateDescription() string {
	if k.cmd.ProcessState == nil {
		return "still running"
	}
	return fmt.Sprintf("exit code: %d", k.cmd.ProcessState.ExitCode())
}

func (k *kartosProcess) Close() error {
	_ = k.stdin.Close()
	return k.cmd.Wait()
}
