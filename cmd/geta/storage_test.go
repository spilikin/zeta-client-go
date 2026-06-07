package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zeta "github.com/gematik/zeta-client-go"
)

func newStorage(t *testing.T) *jsonFileStorage {
	t.Helper()
	s, err := openStorage(filepath.Join(t.TempDir(), "default.storage.json"))
	if err != nil {
		t.Fatalf("openStorage: %v", err)
	}
	return s
}

func TestStorage_PutGetRoundtrip(t *testing.T) {
	s := newStorage(t)
	if err := s.Put("k1", "v1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v, err := s.Get("k1")
	if err != nil || v != "v1" {
		t.Fatalf("get returned %q err %v", v, err)
	}
}

func TestStorage_GetMissingReturnsErrNotFound(t *testing.T) {
	s := newStorage(t)
	_, err := s.Get("nope")
	if !errors.Is(err, zeta.ErrNotFound) {
		t.Fatalf("expected zeta.ErrNotFound, got %v", err)
	}
}

func TestStorage_PersistAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.storage.json")
	a, _ := openStorage(path)
	_ = a.Put("k", "persistent")
	b, err := openStorage(path)
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
func TestStorage_OnDiskFormatMatchesZetaCli(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default.storage.json")
	s, _ := openStorage(path)
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
	// Pretty-printed with 2-space indent — zeta-cli uses prettyPrintIndent = "  ".
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 4 {
		t.Errorf("expected pretty-printed output (≥4 lines), got %d:\n%s", len(lines), raw)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "  ") {
			t.Errorf("indent must be exactly 2 spaces; saw: %q", line)
		}
	}
}

func TestStorage_RemoveThenGet(t *testing.T) {
	s := newStorage(t)
	_ = s.Put("k", "v")
	if err := s.Remove("k"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := s.Get("k"); !errors.Is(err, zeta.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after remove, got %v", err)
	}
}

func TestStorage_Clear(t *testing.T) {
	s := newStorage(t)
	_ = s.Put("a", "1")
	_ = s.Put("b", "2")
	if err := s.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := s.Get("a"); !errors.Is(err, zeta.ErrNotFound) {
		t.Errorf("after clear, a should be missing; got %v", err)
	}
}

func TestStorage_SatisfiesZetaStorage(_ *testing.T) {
	var _ zeta.Storage = (*jsonFileStorage)(nil)
}

func TestProfilePath_SegregatesNativeFromZetaCli(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/fake-xdg")
	got := profilePath("myprof")
	want := "/tmp/fake-xdg/telematik/zeta/myprof.native.storage.json"
	if got != want {
		t.Errorf("profilePath(%q) = %q, want %q", "myprof", got, want)
	}
}

func TestXdgConfigHome_FallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := xdgConfigHome()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config")
	if got != want {
		t.Errorf("xdgConfigHome() = %q, want %q", got, want)
	}
}
