// Package xdg resolves agmux's state directory.
package xdg

import (
	"os"
	"path/filepath"
)

// StateDir is $XDG_STATE_HOME/agmux, defaulting to ~/.local/state/agmux.
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "agmux")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "agmux")
}
