# zeta-client-go

Go bindings for the [ZETA Client SDK](https://github.com/gematik/zeta-sdk) — a Kotlin Multiplatform library shipped as a Kotlin/Native shared library with a C ABI. Includes a CLI (`geta`) covering the p12-auth lifecycle.

Status: experimental. Targets macOS (arm64); Linux + Windows (amd64) static builds are scaffolded.

This project was developed with the assistance of AI-based coding tools.

## Quick start

Zero-to-working binding in three phases. Verified end-to-end on macOS arm64; the same flow applies to Linux amd64 and Windows amd64 with the platform-appropriate C toolchain.

### 1. Prerequisites

- **Go 1.26 or newer** (`go version`).
- **A supported host**: macOS arm64, Linux amd64, or Windows amd64.
- **A C compiler** — required by cgo. The binding does not work without one.
  - **macOS**: Xcode Command Line Tools (`xcode-select --install`, then verify with `cc --version`).
  - **Linux**: `gcc` or `clang` (`apt-get install build-essential`, `dnf install gcc`, etc.).
  - **Windows**: `mingw-w64` (cgo requires a GCC-style toolchain; MSVC is not supported).
- **Internet access** for `go get` and the one-time SDK archive download.
- (Optional, for actual ZETA-protected calls) an SMC-B p12 keystore and a reachable ZETA-protected resource server.

### 2. Install + fetch SDK archives

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

### 3. First program: full login

A `zeta.Storage` is **required** to construct a client. The binding ships `zeta.FileStorage`, a flat-JSON file-backed implementation suitable for development and small production deployments. Save the following as `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    zeta "github.com/gematik/zeta-client-go"
)

func main() {
    storage, err := zeta.OpenFileStorage("./demo.storage.json")
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

    // Login = idempotent discover + register + authenticate.
    ctx := context.Background()
    if err := client.Discover(ctx); err != nil {
        log.Fatal("discover: ", err)
    }
    fmt.Println("discover: ok")
    if err := client.Register(ctx); err != nil {
        log.Fatal("register: ", err)
    }
    fmt.Println("register: ok")
    if err := client.Authenticate(ctx); err != nil {
        log.Fatal("authenticate: ", err)
    }
    fmt.Println("authenticate: ok")

    status, _ := client.Status(ctx)
    fmt.Println("status:", status)
}
```

```sh
go build -o demo . && ./demo
```

A successful run prints `discover: ok` → `register: ok` → `authenticate: ok` → `status: HasAccessAndRefreshToken`. State (registration keys, tokens) is persisted in `./demo.storage.json` next to the binary — subsequent runs reuse the existing registration. From here, construct an `HTTPClient` from the same `*zeta.Client` and make ZETA-protected requests; see `cmd/geta/` for a complete worked example.

With placeholder credentials (the `/path/to/smcb.p12` above), `discover` succeeds (it's pure metadata fetch), `register` succeeds and creates a real registration on the dev server, then `authenticate` fails with `No such file or directory` because the keystore path doesn't exist — proof the full lifecycle is wired without requiring real credentials to validate the install.

> The underlying SDK currently emits verbose TLS handshake / curl debug lines to stdout during HTTP calls. They're not produced by your code and they're tracked for an upstream fix; expect them as background noise interleaved with your `discover: ok` / `register: ok` lines for now.

### About at-rest encryption

The SDK passes **plaintext** state (access/refresh tokens, registration data) to any `zeta.Storage` implementation, and `zeta.FileStorage` persists it as plaintext JSON. For production deployments, wrap your `Storage` with your own AES-GCM (or equivalent) encryption layer between `Put`/`Get` and the persistent backend. The SDK's own per-platform encrypted backends are not reachable when supplying a custom `Storage`.

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

The `.native` infix segregates `geta`'s files from any JVM-based ZETA client's storage at `<profile>.storage.json` — the Kotlin/Native and Kotlin/JVM targets store EC keys in incompatible formats under the same PEM labels, and sharing a file across the two would trigger `OPENSSL: invalid encoding` crashes.

## License

Apache-2.0 — see [LICENSE](LICENSE).
