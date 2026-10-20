package store

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Claude is the Source of Claude Code's own register of live sessions: one <pid>.json per
// process in <config dir>/sessions (2.1.296 and later), and for a background session the job's
// state in <config dir>/jobs/<job id>/state.json. Files that do not parse are left out one by one.
type Claude struct {
	Dir      string // Claude Code's config dir
	HUDDir   string // where `agmux statusline` saves the payloads (<session id>.json), for the transcript path
	Alive    func(pid int) bool
	StartsOf func(pids []int) (map[int]string, error) // pid → the start time ps says; nil = not checked
}

// NewClaude is the Source of the config dir ($CLAUDE_CONFIG_DIR, else ~/.claude) and the statusline files in hudDir.
func NewClaude(hudDir string) *Claude {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return &Claude{Dir: dir, HUDDir: hudDir, Alive: alive, StartsOf: psStarts}
}

// entry is what the register says of a process; times are Unix milliseconds.
type entry struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Kind            string `json:"kind"` // interactive | bg
	Tmux            string `json:"tmux"`
	Name            string `json:"name"`
	Status          string `json:"status"` // busy | waiting | idle | shell
	WaitingFor      string `json:"waitingFor"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	ProcStart       string `json:"procStart"`
	Spare           bool   `json:"spare"` // a warmed-up process of the daemon, no session yet
	JobID           string `json:"jobId"`
	NameSource      string `json:"nameSource"` // user | peer | derived
}

func (c *Claude) Sessions() ([]Raw, error) {
	files, _ := filepath.Glob(filepath.Join(c.Dir, "sessions", "*.json")) // not <pid>.<hash>.key
	var es []entry
	for _, f := range files {
		var e entry
		if data, err := os.ReadFile(f); err != nil || json.Unmarshal(data, &e) != nil || e.PID <= 0 || e.Spare {
			continue
		}
		if c.Alive != nil && !c.Alive(e.PID) {
			continue
		}
		es = append(es, e)
	}
	if c.StartsOf != nil && len(es) > 0 { // a pid that was used again by another process is no session
		pids := make([]int, len(es))
		for i, e := range es {
			pids[i] = e.PID
		}
		if starts, err := c.StartsOf(pids); err == nil { // ps failing is no reason to show nothing
			kept := es[:0]
			for _, e := range es {
				if got, ok := starts[e.PID]; ok && (e.ProcStart == "" || strings.Join(strings.Fields(got), " ") == strings.Join(strings.Fields(e.ProcStart), " ")) {
					kept = append(kept, e)
				}
			}
			es = kept
		}
	}
	raws := make([]Raw, 0, len(es))
	for _, e := range es {
		raws = append(raws, c.raw(e))
	}
	return raws, nil
}

func (c *Claude) raw(e entry) Raw {
	r := Raw{
		ID: e.SessionID, AgentType: "claude", SessionID: e.SessionID, PID: e.PID, CWD: e.CWD, Name: e.Name, NameDerived: e.NameSource == "derived",
		Status:          map[string]string{"busy": "working", "waiting": "waiting"}[e.Status],
		StatusChangedAt: time.UnixMilli(e.StatusUpdatedAt), LastActivity: time.UnixMilli(e.UpdatedAt),
	}
	if r.Status == "" { // idle, shell (a background shell is running: not the session), what a later version adds
		r.Status = "idle"
	}
	if r.Status == "waiting" {
		r.Attention = "question"
		if strings.Contains(e.WaitingFor, "permission") {
			r.Attention = "permission"
		}
	}
	r.Pane, r.TmuxSession = tmuxField(e.Tmux)
	if e.Kind == "bg" && e.JobID != "" {
		r.Background, r.ID = true, e.JobID
		c.jobState(&r)
	}
	if r.Transcript == "" {
		r.Transcript = c.transcript(r.SessionID, r.CWD)
	}
	return r
}

// tmuxField reads the register's tmux field, "<session name>:<window id>.<pane id>" ("agmux # main:@14.%37":
// the session is named "agmux # main"): the pane and the session, short. The session is as it was
// when the process started; the Poller asks tmux for the one the pane is in now.
func tmuxField(s string) (pane, session string) {
	i := strings.LastIndex(s, ".%")
	if i < 0 {
		return "", ""
	}
	pane, s = s[i+1:], s[:i]
	if j := strings.LastIndex(s, ":@"); j >= 0 {
		s = s[:j]
	}
	return pane, shortSession(s)
}

// jobState reads what the daemon keeps of a background job, as ccmux does: tempo blocked is
// waiting (with a question, or what it needs: permission), done / stopped / failed is idle, the
// rest is working; the job's name and transcript beat the register's.
func (c *Claude) jobState(r *Raw) {
	data, err := os.ReadFile(filepath.Join(c.Dir, "jobs", r.ID, "state.json"))
	if err != nil {
		return
	}
	var st struct {
		State, Tempo, Name, LinkScanPath string
		Questions                        []json.RawMessage
		Needs                            json.RawMessage
	}
	if json.Unmarshal(data, &st) != nil {
		return
	}
	switch {
	case st.Tempo == "blocked":
		r.Status, r.Attention = "waiting", "question"
		if len(st.Questions) == 0 && len(st.Needs) > 0 && string(st.Needs) != "null" && string(st.Needs) != `""` && string(st.Needs) != "[]" && string(st.Needs) != "{}" {
			r.Attention = "permission"
		}
	case st.State == "done" || st.State == "stopped" || st.State == "failed":
		r.Status, r.Attention = "idle", ""
	default:
		r.Status, r.Attention = "working", ""
	}
	if st.Name != "" {
		r.Name, r.NameDerived = st.Name, false
	}
	r.Transcript = st.LinkScanPath
}

var notAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// transcript is the path the statusline saved for the session, else where Claude Code keeps it:
// projects/<cwd with every other character a "-">/<session id>.jsonl; "" if there is no such file.
func (c *Claude) transcript(id, cwd string) string {
	if id == "" {
		return ""
	}
	var hud struct {
		TranscriptPath string `json:"transcript_path"`
	}
	if data, err := os.ReadFile(filepath.Join(c.HUDDir, id+".json")); err == nil && json.Unmarshal(data, &hud) == nil && hud.TranscriptPath != "" {
		if _, err := os.Stat(hud.TranscriptPath); err == nil {
			return hud.TranscriptPath
		}
	}
	p := filepath.Join(c.Dir, "projects", notAlnum.ReplaceAllString(cwd, "-"), id+".jsonl")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// alive is kill(pid, 0): the process is there (EPERM: it is, and is not ours).
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// psStarts is the start time of each pid as ps prints it ("Tue Oct 20 07:34:09 2026"), the form
// the register's procStart has; one ps for all. A pid that is gone is not in the answer.
func psStarts(pids []int) (map[int]string, error) {
	ids := make([]string, len(pids))
	for i, p := range pids {
		ids[i] = strconv.Itoa(p)
	}
	cmd := exec.Command("ps", "-o", "pid=,lstart=", "-p", strings.Join(ids, ","))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC") // the register's procStart is in English and UTC, as ps prints it so
	out, err := cmd.Output()
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) { // ps exits 1 when none of the pids is there (with one it exits 0), with nothing printed
		return nil, err
	}
	m := map[int]string{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		if p, err := strconv.Atoi(f[0]); err == nil {
			m[p] = strings.Join(f[1:], " ")
		}
	}
	return m, nil
}
