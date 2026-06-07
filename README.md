# zeta-client-go

Go bindings for the [ZETA Client SDK](https://github.com/gematik/zeta-sdk) — a Kotlin Multiplatform library shipped as a Kotlin/Native shared library with a C ABI. Includes a CLI (`geta`) that mirrors the p12-auth lifecycle subset of [`zeta-cli`](https://github.com/gematik/zeta-cli).

Status: experimental. Targets macOS (arm64); Linux + Windows (amd64) static builds are scaffolded.

This project was developed with the assistance of AI-based coding tools.

## 15-minute quick start

Zero-to-working binding in three phases. Verified end-to-end on macOS arm64; the same flow applies to Linux amd64 and Windows amd64 with the platform-appropriate C toolchain.

### 1. Prerequisites (2 min)

- **Go 1.26 or newer** (`go version`).
- **A supported host**: macOS arm64, Linux amd64, or Windows amd64.
- **A C compiler** — required by cgo. The binding does not work without one.
  - **macOS**: Xcode Command Line Tools (`xcode-select --install`, then verify with `cc --version`).
  - **Linux**: `gcc` or `clang` (`apt-get install build-essential`, `dnf install gcc`, etc.).
  - **Windows**: `mingw-w64` (cgo requires a GCC-style toolchain; MSVC is not supported).
- **Internet access** for `go get` and the one-time SDK archive download.
- (Optional, for actual ZETA-protected calls) an SMC-B p12 keystore and a reachable ZETA-protected resource server.

### 2. Install + fetch SDK archives (3 min)

```sh
mkdir zeta-demo && cd zeta-demo
go mod init demo
go get github.com/gematik/zeta-client-go@latest
go generate github.com/gematik/zeta-client-go/...
```

The `go generate` step downloads the prebuilt SDK archive for your platform (~12–13 MB) from GitHub Releases, verifies its SHA256 against the committed `internal/sdk/prebuilt/CHECKSUMS.txt`, and extracts it into the per-target subdirectory cgo will link against. Re-running is idempotent.

Environment overrides (for forks, mirrors, air-gapped builds):

- `ZETA_SDK_REPO` — overrides the upstream-repo path. Default: read from `internal/sdk/prebuilt/REPO` inside the module.
- `ZETA_SDK_MANIFEST_URL` — full URL pattern override (supports `{version}` and `{tarball}` placeholders) for mirrors.

### 3. First program (10 min)

A `zeta.Storage` is **required** to construct a client. The binding never falls back to a default implicit storage on the Go side. Save the following as `main.go` — it includes a minimal file-backed `zeta.Storage` (plaintext JSON next to the running program — wrap it with your own encryption layer for production):

```go
package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "io/fs"
    "log"
    "os"
    "sync"

    zeta "github.com/gematik/zeta-client-go"
)

// fileStorage is a minimal zeta.Storage that persists state as a flat JSON
// map at `path`. Plaintext at rest — wrap with your own encryption layer
// for production.
type fileStorage struct {
    mu   sync.Mutex
    path string
    kv   map[string]string
}

func openStorage(path string) (*fileStorage, error) {
    s := &fileStorage{path: path, kv: map[string]string{}}
    b, err := os.ReadFile(path)
    if errors.Is(err, fs.ErrNotExist) || len(b) == 0 {
        return s, nil
    }
    if err != nil {
        return nil, err
    }
    return s, json.Unmarshal(b, &s.kv)
}

func (s *fileStorage) write() error {
    b, err := json.MarshalIndent(s.kv, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(s.path, b, 0o600)
}

func (s *fileStorage) Put(k, v string) error {
    s.mu.Lock(); defer s.mu.Unlock()
    s.kv[k] = v; return s.write()
}
func (s *fileStorage) Get(k string) (string, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    if v, ok := s.kv[k]; ok { return v, nil }
    return "", zeta.ErrNotFound
}
func (s *fileStorage) Remove(k string) error {
    s.mu.Lock(); defer s.mu.Unlock()
    delete(s.kv, k); return s.write()
}
func (s *fileStorage) Clear() error {
    s.mu.Lock(); defer s.mu.Unlock()
    s.kv = map[string]string{}; return s.write()
}

func main() {
    storage, err := openStorage("./demo.storage.json")
    if err != nil {
        log.Fatal(err)
    }

    cfg := zeta.Config{
        ResourceURL:    "https://popp.dev.poppservice.de/",
        ProductID:      "demo",
        ProductVersion: "0.1.0",
        ClientName:     "demo-client",
        Auth: zeta.KeystoreAuth{
            File:     "/path/to/smcb.p12",
            Alias:    "alias",
            Password: "00",
        },
        Storage: storage,
        Scopes:  []string{"popp"},
    }
    if err := cfg.Validate(); err != nil {
        log.Fatal("config invalid: ", err)
    }
    client, err := zeta.NewClient(cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    fmt.Println("binding linked OK; client built")
}
```

```sh
go build -o demo . && ./demo
```

Seeing **binding linked OK; client built** confirms cgo linked the SDK, the Storage implementation satisfies the binding's interface, and `NewClient` constructed a live client. The storage file (`demo.storage.json`) is created on first `Put()` — which happens once you start calling `client.Discover(ctx)`, `client.Register(ctx)`, `client.Authenticate(ctx)`. From there, construct an `HTTPClient` to make ZETA-protected requests. See the `geta` CLI source under `cmd/geta/` for a complete worked example.

### About at-rest encryption

The SDK passes **plaintext** state (access/refresh tokens, registration data) to any `zeta.Storage` implementation. The `fileStorage` above persists it as plaintext JSON. For production deployments, wrap your storage with your own AES-GCM (or equivalent) encryption layer between `Put`/`Get` and the persistent backend. The SDK's own per-platform encrypted backends are not reachable when supplying a custom `Storage`.

## The `geta` CLI

Build the bundled CLI from source:

```sh
go install github.com/gematik/zeta-client-go/cmd/geta@latest
```

| Command | Purpose |
|---|---|
| `geta login URL` | Idempotent discover + register + authenticate against URL. |
| `geta status URL` | Report cached state (`NotRegistered` / `RegisteredNoValidTokens` / `HasRefreshToken` / `HasAccessAndRefreshToken`). |
| `geta logout URL` | Revoke tokens server-side and clear local token state. |
| `geta forget URL` | logout + wipe the profile's storage file. |
| `geta http [flags] URL` | Send an authenticated HTTP request (curl-style: `-X`, `-H`, `-d`, `-i`, `-p`/`--popp-token`). |
| `geta ws [flags] WS_URL` | Open a WebSocket; stdin lines → text frames, server text frames → stdout. |
| `geta popp kartos --image PATH` | Drive PoPP's Standard scenario via a [`kartos`](https://github.com/gematik/kartos) simulator; print the resulting JWT. |

Common flags (`--p12-file`, `--p12-alias`, `--p12-password`, `--profile`, `--scope`, `--insecure`, `--verbose`) work across all commands. Run `geta` with no args for the full reference.

### PoPP token via kartos

A minimal demo using the public PoPP service and a kartos card image:

```sh
geta popp kartos \
  --p12-file /tmp/smcb.p12 \
  --image /path/to/EGK_TK_1b.xml \
  --verbose=false --insecure
```

This drives the PoPP Standard scenario against `popp.dev.poppservice.de` and prints the resulting PoPP token JWT.

## Storage layout

Profile state lives at:

```
$XDG_CONFIG_HOME/telematik/zeta/<profile>.native.storage.json
```

The `.native` infix segregates `geta`'s files from the JVM-based `zeta-cli`'s files (`<profile>.storage.json`) — the Kotlin/Native and Kotlin/JVM targets store EC keys in incompatible formats under the same PEM labels. Sharing a file across the two tools causes `OPENSSL: invalid encoding` crashes.

## License

Apache-2.0 — see [LICENSE](LICENSE).
