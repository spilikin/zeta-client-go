# zeta-client-go

Go bindings for the [ZETA Client SDK](https://github.com/gematik/zeta-sdk) — a Kotlin Multiplatform library shipped as a Kotlin/Native shared library with a C ABI. Includes a CLI (`geta`) that mirrors the p12-auth lifecycle subset of [`zeta-cli`](https://github.com/gematik/zeta-cli).

Status: experimental. Targets macOS (arm64); Linux + Windows (amd64) static builds are scaffolded.

This project was developed with the assistance of AI-based coding tools.

## 15-minute quick start

Zero-to-working binding in three phases.

### 1. Prerequisites (2 min)

- Go 1.26 or newer (`go version`)
- A supported host: macOS arm64, Linux amd64, or Windows amd64
- Internet access for `go get` and the one-time SDK archive download
- (Optional, for actual ZETA-protected calls) An SMC-B p12 keystore and access to a resource server

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

Save as `main.go`:

```go
package main

import (
    "fmt"
    "log"

    zeta "github.com/gematik/zeta-client-go"
)

func main() {
    cfg := zeta.Config{
        ResourceURL:    "https://your-resource-server.example.com",
        ProductID:      "demo",
        ProductVersion: "0.1.0",
        ClientName:     "demo-client",
        Auth: &zeta.KeystoreAuth{
            File:     "/path/to/smcb.p12",
            Alias:    "alias",
            Password: "00",
        },
        StorageAESKey: "<base64-encoded 32-byte AES key>",
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

Seeing **binding linked OK; client built** confirms cgo linked the SDK successfully. From here, swap in real credentials and call `client.Discover(ctx)`, `client.Register(ctx)`, `client.Authenticate(ctx)`, then construct an `HTTPClient` and make ZETA-protected requests. See the `geta` CLI source under `cmd/geta/` for a complete worked example.

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
