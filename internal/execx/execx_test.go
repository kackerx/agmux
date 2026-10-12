package execx

import (
	"errors"
	"os/exec"
	"testing"
)

func TestNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Run("tmux", "list-clients")
	if err == nil || err.Error() != "tmux not found in $PATH" || !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestRunInFeedsStdin(t *testing.T) {
	if out, err := RunIn([]byte("x"), "cat"); err != nil || string(out) != "x" {
		t.Errorf("%q %v", out, err)
	}
}
