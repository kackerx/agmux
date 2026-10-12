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
