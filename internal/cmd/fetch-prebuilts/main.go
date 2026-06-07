// Command fetch-prebuilts downloads platform-specific SDK archives from the
// upstream zeta-sdk GitHub Releases page and extracts them into
// internal/sdk/prebuilt/<goos>_<goarch>/ where cgo can link against them.
//
// It is invoked via `go generate ./...` from the binding repo. Consumers run it
// once after `go get` and then on every version bump.
//
// Configuration (resolved in this order):
//
//   - ZETA_SDK_MANIFEST_URL env var — full URL override for the tarball; useful
//     for air-gapped mirrors. When set, REPO + VERSION below are ignored for URL
//     construction but still used for the tarball filename pattern.
//   - ZETA_SDK_REPO env var — overrides the upstream-repo path (e.g.
//     "spilikin/zeta-sdk"). When unset, falls back to the REPO file.
//   - internal/sdk/prebuilt/REPO — committed upstream-repo default
//     (e.g. "gematik/zeta-sdk").
//   - internal/sdk/prebuilt/VERSION — pinned upstream release tag (e.g. "v0.0.1").
//   - internal/sdk/prebuilt/CHECKSUMS.txt — BSD-style SHA256 lines, one per
//     per-target tarball. Verified before extraction; mismatch is fatal.
//
// The tool is pure Go (no cgo) so it builds before any SDK archive is present.
package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultRepo = "gematik/zeta-sdk"
	prebuiltDir = "internal/sdk/prebuilt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fetch-prebuilts: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := findRepoRoot()
	if err != nil {
		return fmt.Errorf("locate repo root: %w", err)
	}

	repo, err := resolveRepo(root)
	if err != nil {
		return fmt.Errorf("resolve upstream repo: %w", err)
	}
	version, err := readTrimmed(filepath.Join(root, prebuiltDir, "VERSION"))
	if err != nil {
		return fmt.Errorf("read VERSION: %w", err)
	}

	target := targetName()
	tarball := fmt.Sprintf("libzeta_sdk-%s-%s.tar.gz", target, version)

	url, err := resolveURL(repo, version, tarball)
	if err != nil {
		return err
	}

	wantSHA, err := lookupChecksum(filepath.Join(root, prebuiltDir, "CHECKSUMS.txt"), tarball)
	if err != nil {
		return fmt.Errorf("lookup checksum for %s: %w", tarball, err)
	}

	destDir := filepath.Join(root, prebuiltDir, strings.ReplaceAll(target, "-", "_"))
	statePath := filepath.Join(destDir, ".fetched")
	if upToDate(statePath, wantSHA) {
		fmt.Printf("fetch-prebuilts: %s already up-to-date (sha %s…)\n", target, wantSHA[:12])
		return nil
	}

	fmt.Printf("fetch-prebuilts: downloading %s\n", url)
	if envOverride := os.Getenv("ZETA_SDK_REPO"); envOverride != "" {
		fmt.Printf("fetch-prebuilts: (using ZETA_SDK_REPO=%s — non-default upstream)\n", envOverride)
	}
	if manifestOverride := os.Getenv("ZETA_SDK_MANIFEST_URL"); manifestOverride != "" {
		fmt.Printf("fetch-prebuilts: (using ZETA_SDK_MANIFEST_URL — non-default URL)\n")
	}

	body, gotSHA, err := download(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if gotSHA != wantSHA {
		return fmt.Errorf("checksum mismatch for %s\n  expected: %s\n  got:      %s", tarball, wantSHA, gotSHA)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}
	if err := extractTarGz(body, destDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if err := os.WriteFile(statePath, []byte(wantSHA+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", statePath, err)
	}

	fmt.Printf("fetch-prebuilts: extracted into %s (sha %s…)\n", destDir, wantSHA[:12])
	return nil
}

// findRepoRoot walks up from the current directory looking for go.mod.
// go generate runs us from internal/sdk/prebuilt; the binding root is two
// levels up. We don't hardcode "../.." in case the tool gets invoked from
// elsewhere (e.g. `go run ./internal/cmd/fetch-prebuilts` from the repo root).
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s", cwd)
		}
		dir = parent
	}
}

func resolveRepo(root string) (string, error) {
	if env := os.Getenv("ZETA_SDK_REPO"); env != "" {
		return env, nil
	}
	repoPath := filepath.Join(root, prebuiltDir, "REPO")
	repo, err := readTrimmed(repoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultRepo, nil
		}
		return "", err
	}
	if repo == "" {
		return defaultRepo, nil
	}
	return repo, nil
}

func resolveURL(repo, version, tarball string) (string, error) {
	if override := os.Getenv("ZETA_SDK_MANIFEST_URL"); override != "" {
		// Caller-supplied base URL pattern; substitute {version} / {tarball} placeholders.
		url := override
		url = strings.ReplaceAll(url, "{version}", version)
		url = strings.ReplaceAll(url, "{tarball}", tarball)
		return url, nil
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, tarball), nil
}

// targetName returns the per-target identifier used in tarball filenames.
// Format: <goos>-<goarch> (e.g. "darwin-arm64", "windows-amd64").
func targetName() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

func readTrimmed(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// Drop comment lines (starting with #) and trim each remaining line.
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line, nil
	}
	return "", nil
}

// lookupChecksum reads a BSD-format SHA256 file (`<sha>  <filename>`) and
// returns the SHA for the given tarball filename. Lines starting with # are
// treated as comments.
func lookupChecksum(path, tarball string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// BSD shasum format is "<sha>  <filename>" but some tools emit other
		// orderings; accept either as long as one of the two fields equals tarball.
		var sha, name string
		switch {
		case fields[len(fields)-1] == tarball:
			name = tarball
			sha = fields[0]
		case fields[0] == tarball:
			name = tarball
			sha = fields[len(fields)-1]
		}
		if name != "" && len(sha) == 64 {
			return sha, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no entry for %s in %s — has a release for this target+version been published?", tarball, path)
}

func upToDate(statePath, wantSHA string) bool {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == wantSHA
}

func download(url string) ([]byte, string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	h := sha256.New()
	body, err := io.ReadAll(io.TeeReader(resp.Body, h))
	if err != nil {
		return nil, "", err
	}
	return body, hex.EncodeToString(h.Sum(nil)), nil
}

func extractTarGz(body []byte, destDir string) error {
	gz, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		// Reject paths that try to escape the dest dir (zip-slip).
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, string(filepath.Separator)) {
			return fmt.Errorf("tar entry escapes dest dir: %q", hdr.Name)
		}
		target := filepath.Join(destDir, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Skip — current tarball spec does not include links; if upstream
			// starts shipping them, decide on policy then (resolve, or copy target).
			continue
		default:
			// Other tar types (char/block/fifo) have no place in an artefact tarball.
			continue
		}
	}
}
