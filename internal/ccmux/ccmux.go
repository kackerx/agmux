// Package ccmux execs the ccmux CLI; agmux never reimplements what ccmux does.
package ccmux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Session is one entry of `ccmux show --json`, only the fields agmux reads.
// JSON nulls decode to "" / zero time.
type Session struct {
	ID               string     `json:"id"`
	Summary          string     `json:"summary"`
	Project          string     `json:"project"`
	AgentType        string     `json:"agentType"`
	CWD              string     `json:"cwd"`
	MainRepoRoot     string     `json:"mainRepoRoot"`
	GitBranch        string     `json:"gitBranch"`
	Status           string     `json:"status"`
	AttentionType    string     `json:"attentionType"`
	PendingTool      string     `json:"pendingTool"`
	TmuxPane         string     `json:"tmuxPane"`
	TrackingMode     string     `json:"trackingMode"`
	Subagents        []Subagent `json:"subagents"`
	NativeSessionID  string     `json:"nativeSessionId"`
	LogPath          string     `json:"logPath"`
	LastActivityAt   time.Time  `json:"lastActivityAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	BackgroundDetail string     `json:"backgroundDetail"`
}

type Subagent struct {
	AgentID     string `json:"agentId"`
	Status      string `json:"status"`
	PendingTool string `json:"pendingTool"`
}

const timeout = 10 * time.Second

// Run execs `ccmux args...` and returns stdout. On failure the error carries
// ccmux's stderr so callers can show it as-is.
func Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ccmux", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil, fmt.Errorf("ccmux not found in $PATH: install ccmux and make sure `ccmux show --json` works")
	case ctx.Err() != nil:
		return nil, fmt.Errorf("ccmux %s: timed out after %s", strings.Join(args, " "), timeout)
	case err != nil:
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ccmux %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// Show runs `ccmux show --json`.
func Show() ([]Session, error) {
	out, err := Run("show", "--json")
	if err != nil {
		return nil, err
	}
	return Parse(out)
}

func Parse(data []byte) ([]Session, error) {
	var ss []Session
	if err := json.Unmarshal(data, &ss); err != nil {
		return nil, fmt.Errorf("parse `ccmux show --json` output: %w (is ccmux up to date? check `ccmux --version`)", err)
	}
	return ss, nil
}
