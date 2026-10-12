// Package execx runs the external tools agmux drives (ccmux, tmux) with a timeout and errors
// that carry the tool's own stderr, ready for the board's message line.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const timeout = 10 * time.Second

// Run execs `bin args...` and returns stdout. On failure the error carries bin's stderr.
func Run(bin string, args ...string) ([]byte, error) { return RunIn(nil, bin, args...) }

// RunIn is Run with stdin (nil = none).
func RunIn(stdin []byte, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.WaitDelay = time.Second // don't hang on children that keep the pipes open after a timeout kill
	err := cmd.Run()
	line := bin + " " + strings.Join(args, " ")
	switch {
	case errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success():
		// exited 0 but left a background child holding stdout; what it printed is complete
		return stdout.Bytes(), nil
	case errors.Is(err, exec.ErrNotFound):
		return nil, notFound(bin)
	case err != nil && ctx.Err() != nil:
		return nil, fmt.Errorf("%s: timed out after %s", line, timeout)
	case err != nil:
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", line, msg)
	}
	return stdout.Bytes(), nil
}

// notFound reads "<bin> not found in $PATH" and still matches exec.ErrNotFound.
type notFound string

func (e notFound) Error() string { return string(e) + " not found in $PATH" }
func (notFound) Unwrap() error   { return exec.ErrNotFound }
