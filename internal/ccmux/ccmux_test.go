package ccmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCcmux puts a shell script named ccmux first on $PATH.
func fakeCcmux(t *testing.T, script string) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ccmux"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestRun(t *testing.T) {
	fakeCcmux(t, `echo "out:$*"`)
	if out, err := Run("switch", "x"); err != nil || string(out) != "out:switch x\n" {
		t.Errorf("ok: %q %v", out, err)
	}

	fakeCcmux(t, "echo 'session x not found' >&2; exit 3")
	if _, err := Run("kill", "x"); err == nil || err.Error() != "ccmux kill x: session x not found" {
		t.Errorf("stderr: %v", err)
	}

	fakeCcmux(t, "exit 4")
	if _, err := Run("kill", "x"); err == nil || err.Error() != "ccmux kill x: exit status 4" {
		t.Errorf("empty stderr: %v", err)
	}

	fakeCcmux(t, "echo '[]'; (/bin/sleep 6 &)")
	start := time.Now()
	if out, err := Run("show", "--json"); err != nil || string(out) != "[]\n" || time.Since(start) > 3*time.Second {
		t.Errorf("child holding stdout after exit 0: %q %v after %s", out, err, time.Since(start))
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Run("show"); err == nil || !strings.HasPrefix(err.Error(), "ccmux not found in $PATH") {
		t.Errorf("missing: %v", err)
	}
}
