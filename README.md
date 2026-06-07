# zeta-client-go

Go bindings for the [ZETA Client SDK](https://github.com/gematik/zeta-sdk) — a Kotlin Multiplatform library shipped as a Kotlin/Native shared library with a C ABI. Includes a CLI (`geta`) that mirrors the p12-auth lifecycle subset of [`zeta-cli`](https://github.com/gematik/zeta-cli).

Status: experimental. Targets macOS (arm64). Linux + Windows cross-builds wired in `Justfile` but currently disabled.

This project was developed with the assistance of AI-based coding tools.

See [`CLAUDE.md`](CLAUDE.md) for design constraints, [`BACKLOG.md`](BACKLOG.md) for roadmap, and [`FEEDBACK_SDK.md`](FEEDBACK_SDK.md) for tracked upstream SDK bugs.

## Install

### As a Go library consumer

```sh
go get github.com/gematik/zeta-client-go@latest
go generate github.com/gematik/zeta-client-go/...   # downloads + verifies platform SDK archives
go build ./...                                       # cgo links the archives
```

The `go generate` step downloads the prebuilt SDK archive for your platform from the upstream `zeta-sdk` GitHub Releases page, verifies the SHA256 against `internal/sdk/prebuilt/CHECKSUMS.txt` (committed to this repo as the supply-chain anchor), and extracts it into the per-target subdir cgo expects. Re-running is cheap — it skips download when files are already current.

To test against a personal fork (pre-official-release validation):
```sh
ZETA_SDK_REPO=spilikin/zeta-sdk go generate github.com/gematik/zeta-client-go/...
```

Environment overrides:
- `ZETA_SDK_REPO` — overrides the upstream-repo path. Default: read from `internal/sdk/prebuilt/REPO`.
- `ZETA_SDK_MANIFEST_URL` — full URL override (with `{version}` and `{tarball}` placeholders) for air-gapped mirrors.

See [DISTRIBUTION_STRATEGY.md](DISTRIBUTION_STRATEGY.md) §A.1 for the full mechanism (fetcher design, supply-chain model, spilikin→gematik promotion path).

### As a binding developer (working in this repo)

Two link modes; pick by preference.

**Shared link (default)** — binary + dylib pair, smaller binary, dylib reusable across consumers:
```sh
just install            # builds geta + libzeta_sdk.dylib, drops both into $GOPATH/bin
```
The pair is relocatable (`@loader_path` rpath); `just dist` produces a tarball.

**Static link** (`-tags static`) — single self-contained binary, no `.dylib` to ship:
```sh
just sdk-macos-static-arm64   # one-time: builds libzeta_sdk.a (~39 MB)
just install-static           # builds + installs ~26 MB self-contained geta
```
Per-platform cgo flags live in `internal/sdk/cgo_*_static.go`. Linux + Windows static targets are wired but unverified — first build on each platform will surface any missing `-l…` system libs at link time.


## Quickstart: VSDM round-trip

End-to-end roundtrip: mint a PoPP token via `kartos`, authenticate to a ZETA-protected VSDM service, and pull a FHIR `vsdmbundle`.

Prerequisites:
- `/tmp/smcb.p12` — SMC-B test card keystore (alias `alias`, password `00` — both are geta defaults).
- `../EGK_TK_1b.xml` — kartos card image for a Standard-scenario eGK.
- `kartos` on `PATH` (from [github.com/gematik/kartos](https://github.com/gematik/kartos)).

```sh
export ZETA_POPP_TOKEN=$(geta popp kartos \
  --p12-file /tmp/smcb.p12 \
  --image ../EGK_TK_1b.xml \
  --verbose=false -insecure | tail -1)

geta login --p12-file /tmp/smcb.p12 --insecure https://vsdm-dev.tk.de

geta http --p12-file /tmp/smcb.p12 --insecure \
  -H 'Accept: application/fhir+json' \
  -H 'If-None-Match: "0000000000000000000000000000000000000000000000000000000000000000"' \
  --verbose=false \
  'https://vsdm-dev.tk.de/vsdservice/v1/vsdmbundle?profileVersion=1.0' | jq
```

Notes:
- `--insecure` skips TLS revocation (`OCSP signature verification failed` against dev servers is a tracked upstream bug — see [`FEEDBACK_SDK.md`](FEEDBACK_SDK.md)).
- `If-None-Match` with 64 zeros tells the server to return the full VSD bundle even if the eGK hasn't changed.
- `profileVersion=1.0` is required by VSDM 2.0.
- `geta http` picks up `ZETA_POPP_TOKEN` automatically as the `PoPP` request header. Pass `-p TOKEN` (or `--popp-token TOKEN`) to override.

## Commands

| Command | Purpose |
|---|---|
| `geta login URL` | Idempotent discover + register + authenticate. |
| `geta status URL` | Report cached state for a resource (`NotRegistered` / `RegisteredNoValidTokens` / `HasRefreshToken` / `HasAccessAndRefreshToken`). |
| `geta logout URL` | Revoke tokens server-side and clear local token state. |
| `geta forget URL` | logout + wipe the profile's storage file. |
| `geta http [flags] URL` | Send an authenticated HTTP request (curl-style: `-X`, `-H`, `-d`, `-i`, `-p`/`--popp-token`). |
| `geta ws [flags] WS_URL` | Open a WebSocket; stdin lines → text frames, server text frames → stdout. |
| `geta popp kartos --image PATH` | Drive PoPP's Standard scenario via a `kartos` simulator; print the resulting JWT. |

Common flags (`--p12-file`, `--p12-alias`, `--p12-password`, `--profile`, `--scope`, `--insecure`, `--verbose`) work across all commands. Run `geta` with no args for the full reference.

## Storage layout

Profile state lives at:
```
$XDG_CONFIG_HOME/telematik/zeta/<profile>.native.storage.json
```

The `.native` infix segregates `geta`'s files from `zeta-cli`'s (`<profile>.storage.json`) — the Kotlin/Native and Kotlin/JVM targets store EC keys in incompatible formats under the same PEM labels. See [`FEEDBACK_SDK.md`](FEEDBACK_SDK.md). Sharing a file across the two tools causes `OPENSSL: invalid encoding` crashes.

## Building from source

```sh
just                    # list recipes
just build-geta         # build the relocatable binary + dylib pair into bin/
just install            # = build-geta + copy into $GOPATH/bin
just dist               # tarball into dist/
just sdk-linux          # rebuild the upstream Linux SDK shared lib in the cross-build container
just clean              # rm bin/ dist/
```

Tests:
```sh
go test ./...                                  # unit + integration
ZETA_E2E=1 ZETA_INSECURE=1 \
  POPP_URL=https://popp.dev.poppservice.de/ \
  VSDM_URL=https://vsdm-dev.tk.de \
  SMB_KEYSTORE_FILE=/tmp/smcb.p12 \
  STORAGE_AES_KEY=<base64-32bytes> \
  go test -tags e2e -count=1 -v -run TestE2E ./...
```
