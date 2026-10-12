// Package ccmux execs the ccmux CLI; agmux never reimplements what ccmux does.
package ccmux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/kackerx/agmux/internal/execx"
)

// Session is one entry of `ccmux show --json`, only the fields agmux reads.
// JSON nulls decode to "" / zero time.
type Session struct {
	ID              string     `json:"id"`
	Summary         string     `json:"summary"`
	Project         string     `json:"project"`
	AgentType       string     `json:"agentType"`
	CWD             string     `json:"cwd"`
	MainRepoRoot    string     `json:"mainRepoRoot"`
	GitBranch       string     `json:"gitBranch"`
	Status          string     `json:"status"`
	AttentionType   string     `json:"attentionType"`
	AttentionState  string     `json:"attentionState"` // "unread" / "read" / null
	PendingTool     string     `json:"pendingTool"`
	TmuxPane        string     `json:"tmuxPane"`
	TrackingMode    string     `json:"trackingMode"`
	Subagents       []Subagent `json:"subagents"`
	NativeSessionID string     `json:"nativeSessionId"`
	LogPath         string     `json:"logPath"`
	LastActivityAt  time.Time  `json:"lastActivityAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type Subagent struct {
	AgentID     string `json:"agentId"`
	Status      string `json:"status"`
	PendingTool string `json:"pendingTool"`
}

// Run execs `ccmux args...` and returns stdout. On failure the error carries
// ccmux's stderr so callers can show it as-is.
func Run(args ...string) ([]byte, error) {
	out, err := execx.Run("ccmux", args...)
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("ccmux not found in $PATH: install ccmux and make sure `ccmux show --json` works")
	}
	return out, err
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
