package main

import (
	"os"
	"path/filepath"
)

// xdgConfigHome returns $XDG_CONFIG_HOME if set and non-empty, else ~/.config.
// macOS deliberately follows the Linux convention so the same files work on
// both platforms — matches zeta-cli's xdgConfigHome().
func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config"
	}
	return filepath.Join(home, ".config")
}

// zetaConfigDir returns $XDG_CONFIG_HOME/telematik/zeta. Created on demand by
// storage writers, not here.
func zetaConfigDir() string {
	return filepath.Join(xdgConfigHome(), "telematik", "zeta")
}

// profilePath returns the path to <profile>.native.storage.json under
// zetaConfigDir. The `.native` infix segregates geta's storage from the
// Kotlin/JVM zeta-cli's `<profile>.storage.json`: the Kotlin/Native target
// stores client/DPoP EC keys in raw SEC1 byte form, the JVM target stores
// them in PKCS#8/SKPI DER, and neither side detects the other's format
// (both use the same `PRIVATE KEY`/`PUBLIC KEY` PEM labels). Sharing the
// same file across tools triggers `EC_R_INVALID_ENCODING` on the reader —
// see FEEDBACK_SDK.md. Renaming sidesteps the collision until the SDK
// unifies the encoding.
func profilePath(profile string) string {
	return filepath.Join(zetaConfigDir(), profile+".native.storage.json")
}
