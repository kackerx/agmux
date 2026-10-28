// Package xdg resolves agmux's state directory.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
)

// StateDir is $XDG_STATE_HOME/agmux, defaulting to ~/.local/state/agmux.
// A relative $XDG_STATE_HOME is ignored, as the XDG spec says.
func StateDir() (string, error) {
	if d := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "agmux"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the state directory: %w; set $HOME or an absolute $XDG_STATE_HOME", err)
	}
	return filepath.Join(home, ".local", "state", "agmux"), nil
}

// ConfigDir is $XDG_CONFIG_HOME/agmux, defaulting to ~/.config/agmux.
func ConfigDir() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "agmux"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the config directory: %w; set $HOME or an absolute $XDG_CONFIG_HOME", err)
	}
	return filepath.Join(home, ".config", "agmux"), nil
}

// CacheDir is $XDG_CACHE_HOME/agmux, defaulting to ~/.cache/agmux.
func CacheDir() (string, error) {
	if d := os.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "agmux"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the cache directory: %w; set $HOME or an absolute $XDG_CACHE_HOME", err)
	}
	return filepath.Join(home, ".cache", "agmux"), nil
}
