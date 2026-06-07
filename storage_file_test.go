package zeta_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zeta "github.com/gematik/zeta-client-go"
)

func newFileStorage(t *testing.T) *zeta.FileStorage {
	t.Helper()
	s, err := zeta.OpenFileStorage(filepath.Join(t.TempDir(), "default.storage.json"))
	if err != nil {
		t.Fatalf("OpenFileStorage: %v", err)
	}
	return s
}

func TestFileStorage_PutGetRoundtrip(t *testing.T) {
	s := newFileStorage(t)
	if err := s.Put("k1", "v1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v, err := s.Get("k1")
	if err != nil || v != "v1" {
		t.Fatalf("get returned %q err %v", v, err)
	}
}

func TestFileStorage_GetMissingReturnsErrNotFound(t *testing.T) {
	s := newFileStorage(t)
	_, err := s.Get("nope")
	if !errors.Is(err, zeta.ErrNotFound) {
		t.Fatalf("expected zeta.ErrNotFound, got %v", err)
	}
}

func TestFileStorage_PersistAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.storage.json")
	a, _ := zeta.OpenFileStorage(path)
	_ = a.Put("k", "persistent")
	b, err := zeta.OpenFileStorage(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	v, err := b.Get("k")
	if err != nil || v != "persistent" {
		t.Fatalf("get returned %q err %v", v, err)
	}
}

// On-disk format must match zeta-cli's JsonFileStorage so the same file is
// interoperable: flat {"key": "value"} JSON, 2-space indent, pretty-printed.
func TestFileStorage_OnDiskFormatMatchesZetaCli(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.storage.json")
	s, _ := zeta.OpenFileStorage(path)
	_ = s.Put("alpha", "1")
	_ = s.Put("beta", "two")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("file is not flat string-string JSON: %v\ncontents: %s", err, raw)
	}
	if decoded["alpha"] != "1" || decoded["beta"] != "two" || len(decoded) != 2 {
		t.Errorf("on-disk JSON wrong: %+v", decoded)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 4 {
		t.Errorf("expected pretty-printed output (>=4 lines), got %d:\n%s", len(lines), raw)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "  ") {
			t.Errorf("indent must be exactly 2 spaces; saw: %q", line)
		}
	}
}

func TestFileStorage_RemoveThenGet(t *testing.T) {
	s := newFileStorage(t)
	_ = s.Put("k", "v")
	if err := s.Remove("k"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := s.Get("k"); !errors.Is(err, zeta.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after remove, got %v", err)
	}
}

func TestFileStorage_Clear(t *testing.T) {
	s := newFileStorage(t)
	_ = s.Put("a", "1")
	_ = s.Put("b", "2")
	if err := s.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := s.Get("a"); !errors.Is(err, zeta.ErrNotFound) {
		t.Errorf("after clear, a should be missing; got %v", err)
	}
}

func TestFileStorage_SatisfiesZetaStorage(_ *testing.T) {
	var _ zeta.Storage = (*zeta.FileStorage)(nil)
}

func TestFileStorage_Path(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.storage.json")
	s, _ := zeta.OpenFileStorage(path)
	if s.Path() != path {
		t.Errorf("Path() = %q, want %q", s.Path(), path)
	}
}
