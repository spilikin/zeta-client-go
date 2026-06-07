// Package prebuilt is a scaffolding directory for platform-specific SDK archives
// fetched at build time via `go generate`. The directory itself holds no Go code
// at runtime — cgo files in the parent package reference the artefacts extracted
// here into per-target subdirectories (darwin_arm64/, linux_amd64/, windows_amd64/).
//
// After `go get`, run `go generate ./...` once to populate this directory.
//
// See DISTRIBUTION_STRATEGY.md (Section A.1) for the full mechanism, including
// the ZETA_SDK_REPO env-var override for testing against personal forks.
package prebuilt

//go:generate go run ../../cmd/fetch-prebuilts
