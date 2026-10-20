package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kackerx/agmux/internal/pi"
)

// Pi is the Source of the pi coding agent's sessions: the files of the agmux extension (internal/pi)
// in <pi dir>/agmux-sessions, one per running pi with a UI. Only the processes that are there count;
// the file of one that was killed stays, and its pid may be someone else's by now.
type Pi struct {
	Dir      string
	Alive    func(pid int) bool
	StartsOf func(pids []int) (map[int]string, error)
}

// NewPi is the Source of the sessions directory the extension writes to.
func NewPi() *Pi {
	dir, _ := pi.SessionsDir() // no home: no files, no sessions
	return &Pi{Dir: dir, Alive: alive, StartsOf: psStarts}
}

type piEntry struct {
	PID         int    `json:"pid"`
	SessionID   string `json:"sessionId"`
	CWD         string `json:"cwd"`
	Transcript  string `json:"transcript"`
	Name        string `json:"name"`
	Status      string `json:"status"` // idle | working | waiting
	PendingTool string `json:"pendingTool"`
	StatusAt    int64  `json:"statusAt"` // Unix milliseconds
	UpdatedAt   int64  `json:"updatedAt"`
}

func (c *Pi) Sessions() ([]Raw, error) {
	if c.Dir == "" {
		return nil, nil
	}
	files, _ := filepath.Glob(filepath.Join(c.Dir, "*.json"))
	var es []piEntry
	file := map[int]string{} // pid → its file
	for _, f := range files {
		var e piEntry
		if data, err := os.ReadFile(f); err != nil || json.Unmarshal(data, &e) != nil || e.PID <= 0 || e.SessionID == "" {
			continue
		}
		if c.Alive == nil || c.Alive(e.PID) {
			es = append(es, e)
			file[e.PID] = f
		} else {
			c.forget(f)
		}
	}
	c.sweep()
	if c.StartsOf != nil && len(es) > 0 {
		pids := make([]int, len(es))
		for i, e := range es {
			pids[i] = e.PID
		}
		if starts, err := c.StartsOf(pids); err == nil {
			kept := es[:0]
			for _, e := range es {
				// a process that started after the file was last written is not the one that wrote it (the pid was used again)
				at, ok := parseStart(starts[e.PID])
				switch {
				case ok && !at.After(time.UnixMilli(e.UpdatedAt)):
					kept = append(kept, e)
				case ok: // a reused pid: the file is of one that is gone. Not told by ps, not said: next poll
					c.forget(file[e.PID])
				}
			}
			es = kept
		}
	}
	raws := make([]Raw, 0, len(es))
	for _, e := range es {
		r := Raw{
			ID: e.SessionID, AgentType: "pi", SessionID: e.SessionID, PID: e.PID, CWD: e.CWD, Name: e.Name,
			Status: e.Status, PendingTool: e.PendingTool, Transcript: e.Transcript, // the Poller does not read it: not Claude Code's format
			StatusChangedAt: time.UnixMilli(e.StatusAt), LastActivity: time.UnixMilli(e.UpdatedAt),
		}
		if e.StatusAt == 0 {
			r.StatusChangedAt = r.LastActivity
		}
		switch r.Status {
		case "working", "waiting", "idle":
		default:
			r.Status = "idle"
		}
		if r.Status == "waiting" {
			r.Attention = "question"
		}
		raws = append(raws, r)
	}
	return raws, nil
}

// parseStart reads a start time as psStarts gives it (English, UTC).
func parseStart(s string) (time.Time, bool) {
	t, err := time.Parse("Mon Jan _2 15:04:05 2006", strings.Join(strings.Fields(s), " "))
	return t, err == nil
}

// forget removes the file of a pi that is gone: it is the extension's own, and nothing else will.
func (c *Pi) forget(f string) { os.Remove(f) }

// sweep removes what a pi that is gone left of a write that was not finished.
func (c *Pi) sweep() {
	left, _ := filepath.Glob(filepath.Join(c.Dir, "*.json.tmp.*"))
	for _, t := range left {
		pid, err := strconv.Atoi(strings.SplitN(filepath.Base(t), ".", 2)[0])
		if err == nil && pid > 0 && c.Alive != nil && !c.Alive(pid) {
			os.Remove(t)
		}
	}
}
