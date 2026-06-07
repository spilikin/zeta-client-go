// geta is a thin CLI over the zeta-client-go binding, mirroring the
// p12-auth lifecycle subset of ../zeta-cli. Storage lives at
// $XDG_CONFIG_HOME/telematik/zeta/<profile>.native.storage.json — the
// `.native` infix segregates geta's profile from zeta-cli's. Both tools
// use the SDK's flat-string Storage interface but the Kotlin/Native and
// Kotlin/JVM targets store EC keys in incompatible formats under the same
// PEM labels; sharing one file corrupts the reader. See FEEDBACK_SDK.md.
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
	"strconv"
	"strings"
	"time"

	zeta "github.com/gematik/zeta-client-go"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println("geta " + version)
		return
	case "help", "-h", "--help":
		usage(os.Stdout)
		return
	case "discover":
		err = runLifecycle(args, cmd, func(ctx context.Context, c *zeta.Client) error { return c.Discover(ctx) })
	case "register":
		err = runLifecycle(args, cmd, func(ctx context.Context, c *zeta.Client) error { return c.Register(ctx) })
	case "authenticate":
		err = runLifecycle(args, cmd, func(ctx context.Context, c *zeta.Client) error { return c.Authenticate(ctx) })
	case "logout":
		err = runLifecycle(args, cmd, func(ctx context.Context, c *zeta.Client) error {
			if err := c.Logout(ctx); err != nil {
				return err
			}
			fmt.Println("logout: ok")
			return nil
		})
	case "login":
		err = runLogin(args)
	case "status":
		err = runStatus(args)
	case "forget":
		err = runForget(args)
	case "http":
		err = runHTTP(args)
	case "ws":
		err = runWS(args)
	case "popp":
		err = runPopp(args)
	default:
		fmt.Fprintf(os.Stderr, "geta: unknown command %q\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "geta: %v\n", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `Usage: geta <command> [flags] [args]

Commands:
  version              Print version.
  discover URL         Fetch protected-resource + AS metadata; persist to profile.
  register URL         Run Dynamic Client Registration against the discovered AS.
  authenticate URL     Exchange p12 credentials for access + refresh tokens.
  login URL            Idempotent discover + register + authenticate.
  status URL           Report cached state for this resource.
  logout URL           Revoke tokens server-side and clear local token state.
  forget URL           logout + wipe the profile's storage file for this resource.
  http [flags] URL     Send an authenticated HTTP request (curl-style).
  ws [flags] WS_URL    Open a WebSocket; stdin lines → text frames, server text frames → stdout.
  popp kartos [flags]  Retrieve a PoPP token via the Standard / kartos-simulator flow.

Common flags (most commands accept these):
  --profile NAME       Storage profile (default: "default"; env ZETA_PROFILE).
  --p12-file PATH      PKCS#12 keystore file.
  --p12-alias NAME     Key entry alias (default "alias"; env ZETA_P12_ALIAS).
  --p12-password PWD   Keystore password (default "00"; env ZETA_P12_PASSWORD).
  --scope SCOPE        OAuth scope; repeatable. Default: zero:audience.
  --insecure           Disable TLS verification (test endpoints only).
  --verbose            Include SDK DEBUG/INFO log lines (default true).
  --proxy URL          Outbound HTTP proxy, e.g. http://user:pass@host:3128 (env: ZETA_PROXY).

The 'http' command additionally accepts:
  -X METHOD            HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS). Default GET.
  -H "key: value"      Request header; repeatable.
  -d BODY              Request body for methods that accept one.
  -i                   Include HTTP status + response headers in output (default off).
  -p, --popp-token T   PoPP header value (env: ZETA_POPP_TOKEN).

The 'ws' command additionally accepts:
  -H "key: value"      Upgrade-request header; repeatable.
  --popp-token TOKEN   Sent as the 'PoPP' upgrade header (env: ZETA_POPP_TOKEN).

Storage:
  Profile state is persisted to $XDG_CONFIG_HOME/telematik/zeta/<profile>.native.storage.json.
  The .native infix segregates from the JVM-format ../zeta-cli profiles — see FEEDBACK_SDK.md.`)
}

// --- common args -------------------------------------------------------------

type commonFlags struct {
	profile     string
	p12File     string
	p12Alias    string
	p12Password string
	scopes      stringSlice
	insecure    bool
	verbose     bool
	proxy       string
}

func registerCommonFlags(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.profile, "profile", envDefault("ZETA_PROFILE", "default"), "Storage profile name")
	fs.StringVar(&c.p12File, "p12-file", "", "PKCS#12 keystore file")
	fs.StringVar(&c.p12Alias, "p12-alias", envDefault("ZETA_P12_ALIAS", "alias"), "Alias inside the PKCS#12 keystore")
	fs.StringVar(&c.p12Password, "p12-password", envDefault("ZETA_P12_PASSWORD", "00"), "Keystore password (env ZETA_P12_PASSWORD)")
	fs.Var(&c.scopes, "scope", "OAuth scope (repeatable); default zero:audience")
	fs.BoolVar(&c.insecure, "insecure", false, "Disable TLS verification (test endpoints only)")
	fs.BoolVar(&c.verbose, "verbose", true, "Include SDK DEBUG/INFO log lines (default true; pass --verbose=false to quiet)")
	fs.StringVar(&c.proxy, "proxy", os.Getenv("ZETA_PROXY"), "Outbound HTTP proxy URL, e.g. http://user:pass@host:3128 (env: ZETA_PROXY)")
	return c
}

// parseProxy returns nil, nil when the proxy flag is empty. Otherwise it
// parses an `http://user:pass@host:port` URL into a zeta.ProxyConfig.
func (c *commonFlags) parseProxy() (*zeta.ProxyConfig, error) {
	if c.proxy == "" {
		return nil, nil
	}
	u, err := url.Parse(c.proxy)
	if err != nil {
		return nil, fmt.Errorf("--proxy: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("--proxy %q: host required", c.proxy)
	}
	host, portStr := u.Hostname(), u.Port()
	if host == "" {
		return nil, fmt.Errorf("--proxy %q: host required", c.proxy)
	}
	port := 8080
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("--proxy %q: invalid port %q", c.proxy, portStr)
		}
		port = p
	}
	pc := &zeta.ProxyConfig{Host: host, Port: port}
	if u.User != nil {
		pc.Username = u.User.Username()
		pc.Password, _ = u.User.Password()
	}
	return pc, nil
}

func (c *commonFlags) validate() error {
	var missing []string
	if c.p12File == "" {
		missing = append(missing, "--p12-file")
	}
	if c.p12Alias == "" {
		missing = append(missing, "--p12-alias")
	}
	if c.p12Password == "" {
		missing = append(missing, "--p12-password (or env ZETA_P12_PASSWORD)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("p12 auth requires: %s", strings.Join(missing, ", "))
	}
	if _, err := os.Stat(c.p12File); err != nil {
		return fmt.Errorf("--p12-file %s: %w", c.p12File, err)
	}
	if _, err := c.parseProxy(); err != nil {
		return err
	}
	return nil
}

func (c *commonFlags) buildConfig(resourceURL string) zeta.Config {
	scopes := []string(c.scopes)
	if len(scopes) == 0 {
		scopes = []string{"zero:audience"}
	}
	proxy, _ := c.parseProxy() // validation already ran; ignore here
	return zeta.Config{
		ResourceURL:    resourceURL,
		ProductID:      "zeta-client-go",
		ProductVersion: version,
		ClientName:     "zeta-cli-go",
		Auth: zeta.KeystoreAuth{
			File:     c.p12File,
			Alias:    c.p12Alias,
			Password: c.p12Password,
		},
		Scopes:                scopes,
		InsecureSkipTLSVerify: c.insecure,
		Proxy:                 proxy,
		Logger:                newSlogLogger(c.verbose),
	}
}

// slogLogger routes SDK log lines through log/slog so output is uniform and
// trailing newlines (the SDK emits messages with embedded \n) don't produce
// blank lines.
type slogLogger struct{ l *slog.Logger }

func newSlogLogger(verbose bool) zeta.Logger {
	lvl := slog.LevelWarn
	if verbose {
		lvl = slog.LevelDebug
	}
	sink := interceptStderr()
	h := slog.NewTextHandler(sink, &slog.HandlerOptions{Level: lvl})
	logger := slog.New(h)
	slog.SetDefault(logger)
	pumpStderr(logger)
	return &slogLogger{l: logger}
}

func (s *slogLogger) Log(level zeta.LogLevel, tag, message string) {
	msg := strings.TrimRight(message, "\r\n")
	attr := slog.String("tag", tag)
	switch level {
	case zeta.LogLevelDebug:
		s.l.Debug(msg, attr)
	case zeta.LogLevelInfo:
		s.l.Info(msg, attr)
	case zeta.LogLevelWarn:
		s.l.Warn(msg, attr)
	default:
		s.l.Error(msg, attr)
	}
}

// withClient parses common flags, the trailing positional URL, opens the
// profile storage, builds a Client wired to that storage, and invokes fn.
// Ensures Client.Close runs.
func withClient(fs *flag.FlagSet, args []string, fn func(ctx context.Context, c *zeta.Client, resourceURL string) error) error {
	cf := registerCommonFlags(fs)
	if err := fs.Parse(reorderFlagsFirst(fs, args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("URL argument is required")
	}
	resourceURL := fs.Arg(0)
	if err := cf.validate(); err != nil {
		return err
	}
	storage, err := zeta.OpenFileStorage(profilePath(cf.profile))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	cfg := cf.buildConfig(resourceURL)
	cfg.Storage = storage
	client, err := zeta.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}
	defer client.Close()
	return fn(context.Background(), client, resourceURL)
}

// reorderFlagsFirst lets users write `geta cmd URL --flag VAL` as well as the
// stdlib-required `geta cmd --flag VAL URL`. stdlib `flag.Parse` stops at the
// first non-flag token, so without this the URL eats everything after it.
func reorderFlagsFirst(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if strings.Contains(a, "=") {
			continue
		}
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

// --- commands ----------------------------------------------------------------

func runLifecycle(args []string, name string, op func(context.Context, *zeta.Client) error) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return withClient(fs, args, func(ctx context.Context, c *zeta.Client, _ string) error {
		return op(ctx, c)
	})
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	return withClient(fs, args, func(ctx context.Context, c *zeta.Client, _ string) error {
		start, _ := c.Status(ctx)
		fmt.Printf("status before: %s\n", start)
		if err := c.Discover(ctx); err != nil {
			return err
		}
		fmt.Println("discover: ok")
		if err := c.Register(ctx); err != nil {
			return err
		}
		fmt.Println("register: ok")
		if err := c.Authenticate(ctx); err != nil {
			return err
		}
		fmt.Println("authenticate: ok")
		end, _ := c.Status(ctx)
		fmt.Printf("status after: %s\n", end)
		return nil
	})
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	return withClient(fs, args, func(ctx context.Context, c *zeta.Client, resourceURL string) error {
		s, err := c.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("resource: %s\nprofile:  %s\nstatus:   %s\n", resourceURL, fs.Lookup("profile").Value, s)
		return nil
	})
}

func runForget(args []string) error {
	fs := flag.NewFlagSet("forget", flag.ContinueOnError)
	return withClient(fs, args, func(ctx context.Context, c *zeta.Client, _ string) error {
		if err := c.Forget(ctx); err != nil {
			return err
		}
		fmt.Println("forget: ok")
		return nil
	})
}

func runHTTP(args []string) error {
	fs := flag.NewFlagSet("http", flag.ContinueOnError)
	var method, body, poppToken string
	var headers stringSlice
	var include bool
	fs.StringVar(&method, "X", "GET", "HTTP method")
	fs.Var(&headers, "H", "Request header in 'key: value' form; repeatable")
	fs.StringVar(&body, "d", "", "Request body")
	fs.BoolVar(&include, "i", false, "Include HTTP status line + response headers in output")
	fs.StringVar(&poppToken, "popp-token", os.Getenv("ZETA_POPP_TOKEN"), "PoPP header value (env: ZETA_POPP_TOKEN)")
	fs.StringVar(&poppToken, "p", os.Getenv("ZETA_POPP_TOKEN"), "shorthand for --popp-token")
	cf := registerCommonFlags(fs)
	if err := fs.Parse(reorderFlagsFirst(fs, args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("URL argument is required")
	}
	rawURL := fs.Arg(0)
	if err := cf.validate(); err != nil {
		return err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid URL: %s", rawURL)
	}
	origin := parsed.Scheme + "://" + parsed.Host + "/"
	requestPath := strings.TrimPrefix(parsed.Path, "/")
	if parsed.RawQuery != "" {
		requestPath += "?" + parsed.RawQuery
	}

	storage, err := zeta.OpenFileStorage(profilePath(cf.profile))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	cfg := cf.buildConfig(origin)
	cfg.Storage = storage
	client, err := zeta.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}
	defer client.Close()

	httpClient, err := client.HTTPClient()
	if err != nil {
		return fmt.Errorf("build http client: %w", err)
	}
	defer httpClient.Close()

	parsedHeaders, err := parseHeaders(headers)
	if err != nil {
		return err
	}
	if poppToken != "" {
		parsedHeaders["PoPP"] = []string{poppToken}
	}
	ctx := context.Background()
	var resp *zeta.Response
	switch strings.ToUpper(method) {
	case "GET":
		resp, err = httpClient.Get(ctx, requestPath, parsedHeaders)
	case "HEAD":
		resp, err = httpClient.Head(ctx, requestPath, parsedHeaders)
	case "OPTIONS":
		resp, err = httpClient.Options(ctx, requestPath, parsedHeaders)
	case "DELETE":
		resp, err = httpClient.Delete(ctx, requestPath, parsedHeaders)
	case "POST":
		resp, err = httpClient.Post(ctx, requestPath, parsedHeaders, []byte(body))
	case "PUT":
		resp, err = httpClient.Put(ctx, requestPath, parsedHeaders, []byte(body))
	case "PATCH":
		resp, err = httpClient.Patch(ctx, requestPath, parsedHeaders, []byte(body))
	default:
		return fmt.Errorf("unsupported method: %s", method)
	}
	if err != nil {
		return err
	}
	if include {
		fmt.Printf("HTTP/1.1 %d\n", resp.Status)
		for k, vs := range resp.Headers {
			for _, v := range vs {
				fmt.Printf("%s: %s\n", k, v)
			}
		}
		fmt.Println()
	}
	if len(resp.Body) > 0 {
		_, _ = os.Stdout.Write(resp.Body)
		if resp.Body[len(resp.Body)-1] != '\n' {
			fmt.Println()
		}
	}
	if resp.Status >= 400 {
		return fmt.Errorf("HTTP %d", resp.Status)
	}
	return nil
}

// runWS mirrors zeta-cli's `ws` command: stdin JSON lines → text frames,
// server text frames → stdout. Skips blank/non-JSON lines with a WARN like
// zeta-cli does. Closing stdin (Ctrl-D) triggers a graceful WS close; an
// incoming Close frame or transport error exits the program.
func runWS(args []string) error {
	fs := flag.NewFlagSet("ws", flag.ContinueOnError)
	var poppToken string
	var headers stringSlice
	fs.Var(&headers, "H", "Upgrade-request header in 'key: value' form; repeatable")
	fs.StringVar(&poppToken, "popp-token", os.Getenv("ZETA_POPP_TOKEN"), "PoPP header value (env: ZETA_POPP_TOKEN)")
	cf := registerCommonFlags(fs)
	if err := fs.Parse(reorderFlagsFirst(fs, args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("WebSocket URL argument is required")
	}
	wsURL := fs.Arg(0)
	if err := cf.validate(); err != nil {
		return err
	}
	parsed, err := url.Parse(wsURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid URL: %s", wsURL)
	}
	scheme := "https"
	if parsed.Scheme == "ws" {
		scheme = "http"
	}
	origin := scheme + "://" + parsed.Host + "/"

	hdrs, err := parseHeaders(headers)
	if err != nil {
		return err
	}
	if poppToken != "" {
		hdrs["PoPP"] = []string{poppToken}
	}

	storage, err := zeta.OpenFileStorage(profilePath(cf.profile))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	cfg := cf.buildConfig(origin)
	cfg.Storage = storage
	client, err := zeta.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}
	defer client.Close()

	return client.WebSocket(context.Background(), wsURL, zeta.WSOptions{Headers: hdrs}, func(s *zeta.WSSession) {
		fmt.Fprintln(os.Stderr, "ws: connected; type JSON, one per line. Ctrl-D to close.")
		runWSRelay(s)
	})
}

// runWSRelay is a request-response relay: one stdin line is sent as a text
// frame, then we block on Receive (with a timeout) for the server's reply,
// print it, repeat. zeta-cli's full-duplex coroutine pattern doesn't
// translate cleanly because the SDK's WSSession serializes send and receive
// behind one mutex (internal/sdk/websocket.go) — a recv goroutine holds
// the lock while waiting for unsolicited server data and starves the
// sender. See FEEDBACK_SDK.md.
func runWSRelay(s *zeta.WSSession) {
	const recvTimeout = 5 * time.Second
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			fmt.Fprintf(os.Stderr, "ws: skipping non-JSON input\n")
			continue
		}
		if err := s.SendText(line); err != nil {
			fmt.Fprintf(os.Stderr, "ws: send: %v\n", err)
			return
		}

		type recvResult struct {
			msg zeta.WSMessage
			err error
		}
		ch := make(chan recvResult, 1)
		go func() { m, e := s.Receive(); ch <- recvResult{m, e} }()

		select {
		case r := <-ch:
			if r.err != nil {
				if !errors.Is(r.err, zeta.ErrWSSessionClosed) {
					fmt.Fprintf(os.Stderr, "ws: receive: %v\n", r.err)
				}
				return
			}
			payload := r.msg.Text
			if payload == "" && len(r.msg.Binary) > 0 {
				fmt.Fprintf(os.Stderr, "ws: skipping %d-byte binary frame\n", len(r.msg.Binary))
				continue
			}
			fmt.Println(payload)
		case <-time.After(recvTimeout):
			fmt.Fprintf(os.Stderr, "ws: no server response within %s; closing\n", recvTimeout)
			_ = s.Close()
			return
		}
	}
	_ = s.Close()
}

// --- helpers -----------------------------------------------------------------

// stringSlice is a repeatable string flag: -scope=a -scope=b → []string{"a", "b"}.
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseHeaders turns "key: value" entries into a map[string][]string.
func parseHeaders(raw []string) (map[string][]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(raw))
	for _, h := range raw {
		before, after, ok := strings.Cut(h, ":")
		if !ok {
			return nil, fmt.Errorf("invalid header %q: expected 'key: value'", h)
		}
		key := strings.TrimSpace(before)
		val := strings.TrimSpace(after)
		if key == "" {
			return nil, fmt.Errorf("invalid header %q: empty key", h)
		}
		out[key] = append(out[key], val)
	}
	return out, nil
}
