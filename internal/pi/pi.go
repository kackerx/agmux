// Package pi is agmux's side of the pi coding agent: the extension that makes a running pi session
// known to the board, and where pi keeps things.
package pi

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kackerx/agmux/internal/i18n"
)

//go:embed agmux.js
var files embed.FS

// Extension is the source of the extension `agmux setup --write` installs.
var Extension = func() []byte { b, _ := files.ReadFile("agmux.js"); return b }()

// Dir is pi's agent directory: $PI_CODING_AGENT_DIR, else ~/.pi/agent.
func Dir() (string, error) {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate pi's directory: %w; set $HOME or $PI_CODING_AGENT_DIR", err)
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

// SessionsDir is where the extension keeps a file per running session.
func SessionsDir() (string, error) {
	d, err := Dir()
	return filepath.Join(d, "agmux-sessions"), err
}

// ExtensionPath is where pi finds the extension.
func ExtensionPath() (string, error) {
	d, err := Dir()
	return filepath.Join(d, "extensions", "agmux.js"), err
}

// Installed says whether the extension file is there.
func Installed() bool {
	p, err := ExtensionPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Setup is the pi part of `agmux setup [--write]`: it installs the extension into pi's extensions
// directory, or says what it would do. A file that is there and not the same is left alone: it may
// be edited, or another version.
func Setup(write bool, stdout, stderr io.Writer) int {
	dir, err := Dir()
	if err != nil {
		fmt.Fprintf(stderr, "agmux setup: %v\n", err)
		return 1
	}
	if _, err := os.Stat(dir); err != nil {
		fmt.Fprintln(stdout, i18n.T("setup.pi_not_found", dir))
		return 0
	}
	path, _ := ExtensionPath()
	cur, err := os.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(cur, Extension):
		fmt.Fprintln(stdout, i18n.T("setup.pi_present", path))
		return 0
	case err == nil:
		fmt.Fprintln(stdout, i18n.T("setup.pi_differs", path))
		return 0
	case !os.IsNotExist(err):
		fmt.Fprintf(stderr, "agmux setup: %v\n", err)
		return 1
	}
	if !write {
		fmt.Fprintln(stdout, i18n.T("setup.pi_would", bytes.Count(Extension, []byte("\n")), path))
		return 0
	}
	if err := install(path); err != nil {
		fmt.Fprintf(stderr, "agmux setup: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, i18n.T("setup.pi_installed", path))
	return 0
}

// install writes the extension atomically: a pi starting now never reads half of it.
func install(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "agmux-*.js.tmp") // not *.js: pi would load it
	if err != nil {
		return err
	}
	_, err = tmp.Write(Extension)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
