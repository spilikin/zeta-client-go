package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
