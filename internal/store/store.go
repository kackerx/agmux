// Package store holds the sessions agmux shows: ccmux polled every 2s,
// merged with the HUD state file and the jsonl model fallback.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/xdg"
)

type Subagent = ccmux.Subagent

type Session struct {
	ID, Name, AgentType, CWD, GroupKey, Branch string
	Status, Attention, PendingTool, TmuxPane   string
	Done                                       bool // ccmux's "done": idle, no subagent running, attentionState set
	AgentsWorking                              bool // Status is "working" only because subagents are
	Model, Effort                              string
	Agent                                      string // the agent definition (claude --agent <name>) from the transcript's agent-setting line; "" if none
	ContextPct                                 int    // <0 = unknown
	Subagents                                  []Subagent
	Shells                                     int // always 0 in S1
	AvatarID                                   string
	AvatarState                                string // what an image avatar does: one of avatar.States, from the fields above
	LastActivity                               time.Time
}

// AvatarKey is what avatar bindings are keyed by: the name, which survives restarts and
// handoffs (the ID doesn't); the ID only when there is no name.
func (s Session) AvatarKey() string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

type Store interface {
	Sessions() []Session
}

// groupKey decides which column a session lands in; change only this to regroup.
func groupKey(s Session) string { return s.CWD }

const PollInterval = 2 * time.Second

// Poller is the in-memory Store.
type Poller struct {
	Show     func() ([]ccmux.Session, error)
	StateDir string          // where HUD files <nativeSessionId>.json live
	Avatars  avatar.Bindings // avatars.json; nil = none

	AvatarBind    map[string]string // config.toml's [avatars.bind]
	AvatarDefault string            // config.toml's [avatars] default; "" = hash

	refreshMu sync.Mutex        // serializes Refresh; guards models
	models    map[string]string // session ID → last model found in its jsonl
	agents    map[string]string // session ID → agent definition, once known (it never changes)

	avatarErr error // avatars.json could not be read, as of the last poll or bind

	mu       sync.Mutex
	sessions []Session
	err      error
}

func NewPoller() (*Poller, error) {
	dir, err := xdg.StateDir()
	if err != nil {
		return nil, err
	}
	// a corrupt avatars.json means no bindings (hash defaults); the error shows on the board, it never exits
	b, err := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	return &Poller{Show: ccmux.Show, StateDir: dir, Avatars: b, avatarErr: err}, nil
}

func (p *Poller) Sessions() []Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions
}

// Err is the last poll's error (the previous sessions are kept when it fails),
// else the avatars.json error.
func (p *Poller) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	return p.avatarErr
}

// SetAvatar binds s's key to sprite id in avatars.json; the sessions show it right away.
// A file that can't be read is not overwritten (Set says why).
func (p *Poller) SetAvatar(s Session, id string) error {
	p.refreshMu.Lock() // a poll in flight read the old binding; it must not land after this
	defer p.refreshMu.Unlock()
	err := p.Avatars.Set(s.AvatarKey(), id)
	aerr := p.Avatars.Reload() // avatarErr is about reading the file; a failed write is err's alone
	p.mu.Lock()
	defer p.mu.Unlock()
	p.avatarErr = aerr
	if err != nil {
		return err
	}
	ss := slices.Clone(p.sessions) // the board may still hold the old slice
	for i := range ss {
		if ss[i].AvatarKey() == s.AvatarKey() {
			ss[i].AvatarID = id
		}
	}
	p.sessions = ss
	return nil
}

// Refresh polls ccmux once.
func (p *Poller) Refresh() error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	var aerr error
	if p.Avatars != nil { // another board, or a hand edit, may have changed avatars.json
		aerr = p.Avatars.Reload()
	}
	raw, err := p.Show()
	var ss []Session
	if err == nil {
		ss = make([]Session, len(raw))
		for i, r := range raw {
			ss[i] = p.convert(r)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err, p.avatarErr = err, aerr
	if err == nil {
		p.sessions = ss
	}
	return err
}

// Run refreshes every PollInterval until ctx is done.
func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Refresh()
		}
	}
}

func (p *Poller) convert(r ccmux.Session) Session {
	s := Session{
		ID: r.ID, Name: r.Summary, AgentType: r.AgentType, CWD: r.CWD, Branch: r.GitBranch,
		Status: r.Status, Attention: r.AttentionType, PendingTool: r.PendingTool, TmuxPane: r.TmuxPane,
		ContextPct: -1, Subagents: r.Subagents, LastActivity: r.LastActivityAt,
	}
	if s.Name == "" {
		s.Name = r.Project
	}
	// ccmux 1.4.3 getEffectiveStatus: an idle session with a running subagent counts as working
	if s.Status == "idle" {
		for _, a := range r.Subagents {
			if a.Status == "working" || a.Status == "waiting" {
				s.Status, s.AgentsWorking = "working", true
				break
			}
		}
	}
	s.Done = s.Status == "idle" && r.AttentionState != ""
	s.AvatarState = avatarState(s)
	s.GroupKey = groupKey(s)
	s.AvatarID = avatar.For(p.Avatars, p.AvatarBind, p.AvatarDefault, s.AvatarKey())
	hudModel := ""
	if r.NativeSessionID != "" {
		s.ContextPct, hudModel, s.Effort = readHUD(filepath.Join(p.StateDir, r.NativeSessionID+".json"))
	}
	if r.LogPath != "" {
		s.Model = lastModel(r.LogPath)
		s.Agent = p.agent(s.ID, r.LogPath)
	}
	// ~8% of transcripts end in a big attachment / long line with no assistant entry in the tail
	if p.models == nil {
		p.models = map[string]string{}
	}
	if s.Model == "" {
		s.Model = p.models[s.ID]
	} else {
		p.models[s.ID] = s.Model
	}
	if hudModel != "" { // Claude Code's own name for it beats the jsonl model ID
		s.Model = hudModel
	}
	return s
}

// avatarState is what an image avatar shows: hi when the session needs you, then for a working one
// think (no tool pending, or only subagents), read / write / tool by the pending tool, then success
// when it is done, else idle.
func avatarState(s Session) string {
	switch {
	case s.Attention != "" || s.Status == "waiting":
		return "hi"
	case s.Status == "working":
		switch s.PendingTool {
		case "":
			return "think"
		case "Read", "Grep", "Glob", "WebFetch", "WebSearch":
			return "read"
		case "Write", "Edit", "MultiEdit", "NotebookEdit":
			return "write"
		}
		return "tool"
	case s.Done:
		return "success"
	}
	return "idle"
}

// readHUD reads what `agmux statusline` saved from Claude Code's status line payload:
// context_window.used_percentage (truncated like claude-hud, 29.6 → 29), model.display_name and
// effort.level. Missing or unparsable file = unknown (-1, "", ""), never an error.
func readHUD(path string) (pct int, model, effort string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1, "", ""
	}
	var hud struct {
		ContextWindow struct {
			UsedPercentage *float64 `json:"used_percentage"`
		} `json:"context_window"`
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Effort struct {
			Level string `json:"level"`
		} `json:"effort"`
	}
	// a wrongly typed field (effort: "high") leaves the rest decoded: only broken JSON is unknown
	if err := json.Unmarshal(data, &hud); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return -1, "", ""
		}
	}
	pct = -1
	if hud.ContextWindow.UsedPercentage != nil {
		pct = int(*hud.ContextWindow.UsedPercentage)
	}
	return pct, hud.Model.DisplayName, hud.Effort.Level
}

const tailBytes = 64 << 10

// lastModel returns message.model of the last assistant entry in the final
// 64KB of a jsonl transcript (files reach 16MB, so never read it whole).
// ponytail: an assistant line longer than 64KB is missed; widen tailBytes if models show blank.
func lastModel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	// one extra byte: if the cut lands exactly on a line start, the dropped "partial" first line is empty
	off := max(fi.Size()-tailBytes-1, 0)
	buf, err := io.ReadAll(io.NewSectionReader(f, off, fi.Size()-off))
	if err != nil {
		return ""
	}
	lines := bytes.Split(buf, []byte("\n"))
	if off > 0 {
		lines = lines[1:] // first line is cut mid-way
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"type":"assistant"`)) {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		// the substring can also appear inside tool output; check the real top-level type.
		// "<synthetic>" marks Claude Code's locally made messages (API errors etc.), not a model.
		if json.Unmarshal(lines[i], &e) == nil && e.Type == "assistant" && e.Message.Model != "" && e.Message.Model != "<synthetic>" {
			return e.Message.Model
		}
	}
	return ""
}

// agent is the session's agent definition. It is read once: Claude Code writes the agent-setting
// line first (and again on resume, with the same value), so a name found is cached, and so is
// "none" once the transcript is longer than what is read: the line would be in it by then.
func (p *Poller) agent(id, path string) string {
	if a, ok := p.agents[id]; ok {
		return a
	}
	a, final := agentSetting(path)
	if final {
		if p.agents == nil {
			p.agents = map[string]string{}
		}
		p.agents[id] = a
	}
	return a
}

const headBytes = 64 << 10

// agentSetting returns agentSetting of the first {"type":"agent-setting",...} line in the first
// 64KB of a jsonl transcript. final says whether asking again could change the answer: no, when it
// was found or when the transcript is longer than what was read (a later line is not looked for).
// ponytail: a transcript whose agent-setting line is not in its first 64KB shows no agent.
func agentSetting(path string) (agent string, final bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, headBytes))
	if err != nil {
		return "", false
	}
	for _, l := range bytes.Split(buf, []byte("\n")) { // a line cut by the 64KB limit does not parse: skipped
		if !bytes.Contains(l, []byte(`"agent-setting"`)) {
			continue
		}
		var e struct {
			Type         string `json:"type"`
			AgentSetting string `json:"agentSetting"`
		}
		if json.Unmarshal(l, &e) == nil && e.Type == "agent-setting" && e.AgentSetting != "" {
			return e.AgentSetting, true
		}
	}
	return "", len(buf) == headBytes
}
