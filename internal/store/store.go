// Package store holds the sessions agmux shows: ccmux polled every 2s,
// merged with the HUD state file and the jsonl model fallback.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/pi"
	"github.com/kackerx/agmux/internal/xdg"
)

// Subagent is a subagent a session runs (the board draws them; no source tells of them now).
type Subagent struct {
	AgentID     string
	Status      string
	PendingTool string
}

type Session struct {
	ID, Name, AgentType, CWD, GroupKey, Branch string
	Status, Attention, PendingTool, TmuxPane   string
	Background                                 bool // a background session (claude --bg): ccmux tracks it without a tmux pane
	Done                                       bool // ccmux's "done": idle, no subagent running, attentionState set, and not seen on the board
	AgentsWorking                              bool // Status is "working" only because subagents are
	Model, Effort                              string
	Project, TmuxSession                       string // ccmux's project (worktrees under their main repo); the tmux session the pane is in, "" with no pane
	MainRepoRoot                               string // the main repo of a worktree, "" outside git
	PID                                        int    // the agent's process, 0 if not known (restart and kill signal it)
	SessionID, Transcript                      string // what the agent itself calls the session, and its jsonl: to resume it ("" if not known)
	GroupLabel                                 string // what the board's current grouping calls this card's group ("" = the cwd as a path); set by the board, not the poller
	Agent                                      string // the agent definition (claude --agent <name>) from the transcript's agent-setting line; "" if none
	ContextPct                                 int    // <0 = unknown
	Subagents                                  []Subagent
	Shells                                     int // always 0 in S1
	AvatarID                                   string
	AvatarState                                string // what an image avatar does: one of avatar.States, from the fields above
	LastActivity                               time.Time
	StatusChangedAt                            time.Time // when Status last changed; with the ID it names one done, which is what "seen" is kept for
	Pinned                                     bool      // pinned to the top of its group (pins.json)
	PinnedAt                                   time.Time // when; pinned cards sort by it, the first one pinned first
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
	Sources  []Source        // the agents' lists of live sessions, merged
	StateDir string          // where HUD files <nativeSessionId>.json live
	Avatars  avatar.Bindings // avatars.json; nil = none

	AvatarBind    map[string]string // config.toml's [avatars.bind]
	AvatarDefault string            // config.toml's [avatars] default; "" = hash

	Seen        *avatar.FileBindings   // seen.json: session ID → seenKey of the done the board showed it at; nil = nothing is seen
	Now         func() time.Time       // nil = time.Now; tests set it
	Pins        *avatar.FileBindings   // pins.json: session name (AvatarKey) → when it was pinned (RFC 3339); nil = nothing is pinned
	Procs       func() (procs, error)  // the process table, for the pane of a process; nil = no climbing
	PiInstalled func() bool            // whether the pi extension is installed, for the hint; nil = no hint
	Panes       func() (string, error) // tmux's pane listing (see TmuxPanes), for the panes of the sessions: their tmux session, and where background sessions are attached; nil = none

	hint       string                // PiHint while there is a pi that the extension does not tell of (mu)
	hintNow    string                // the same as of the last look (refreshMu)
	hintAt     time.Time             // when that was last looked at
	refreshMu  sync.Mutex            // serializes Refresh; guards models
	pinGone    map[string]time.Time  // pinned name → when no session had it any more (see prunePins)
	models     map[string]string     // session ID → last model found in its jsonl
	heads      map[string]string     // transcript path → agent definition from its head (the head never changes), once known
	titleBytes int64                 // and the bytes they read
	titleScans int                   // transcripts scanned for a title, the tests count them
	titles     map[string]titleState // session ID → its custom title and how far its transcript was scanned for one
	worked     map[string]bool       // session ID → seen not idle by this poller, or its status changed since: only such a session can be done
	firstAt    map[string]time.Time  // session ID → its StatusChangedAt when this poller first saw it
	repos      map[string]repoInfo   // cwd → what git says of it, asked once
	branches   map[string]branchInfo // cwd → the branch git said, and when

	avatarErr, seenErr, pinsErr error // avatars.json / seen.json / pins.json could not be read, as of the last poll or bind

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
	seen, serr := avatar.LoadBindings(filepath.Join(dir, "seen.json")) // likewise: a corrupt one means nothing is seen
	pins, perr := avatar.LoadBindings(filepath.Join(dir, "pins.json"))
	return &Poller{Sources: []Source{NewClaude(dir), NewPi()}, StateDir: dir, Avatars: b, avatarErr: err, Seen: seen, seenErr: serr, Pins: pins, pinsErr: perr, Panes: TmuxPanes, Procs: psProcs, PiInstalled: pi.Installed}, nil
}

func (p *Poller) Sessions() []Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions
}

// What the board says when a pi runs that it can't see: the extension is not installed, or is and
// the pi was started before it was.
const (
	PiHint        = "pi: run agmux setup --write"
	PiRestartHint = "pi: restart the running pi sessions to show them"
)

// piHintEvery is how often the process table is asked about pi when no extension is there to tell.
const piHintEvery = 30 * time.Second

// checkPiHint sets the hint when a process called pi runs and no pi session is known: the one that
// installs the extension, or the one that says the running pis have to be started again.
func (p *Poller) checkPiHint(raw []Raw) {
	if p.Procs == nil || p.PiInstalled == nil {
		return
	}
	for _, r := range raw {
		if r.AgentType == "pi" {
			p.hintNow = ""
			return
		}
	}
	if now := p.now(); !p.hintAt.IsZero() && now.Sub(p.hintAt) < piHintEvery {
		return
	} else {
		p.hintAt = now
	}
	p.hintNow = ""
	if t, err := p.Procs(); err == nil {
		for _, c := range t.comm {
			if filepath.Base(c) == "pi" {
				p.hintNow = PiHint
				if p.PiInstalled() {
					p.hintNow = PiRestartHint
				}
				return
			}
		}
	}
}

// Hint is a line for the user that is not an error, "" when there is none.
func (p *Poller) Hint() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hint
}

// Err is the last poll's error (the previous sessions are kept when it fails),
// else the avatars.json error, else the seen.json one, else the pins.json one.
func (p *Poller) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	if p.avatarErr != nil {
		return p.avatarErr
	}
	if p.seenErr != nil {
		return p.seenErr
	}
	return p.pinsErr
}

// seenKey names one done: the status changed at that time. A session that works and is done again has a new one.
func seenKey(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// MarkSeen records that the user saw s's done on the board: from now on it is idle, until its status changes.
func (p *Poller) MarkSeen(s Session) error {
	if p.Seen == nil || s.StatusChangedAt.IsZero() { // no time, no telling this done from the next: it stays until ccmux clears it
		return nil
	}
	p.refreshMu.Lock() // a poll in flight was built without it; it must not land after this
	defer p.refreshMu.Unlock()
	err := p.Seen.Set(s.ID, seenKey(s.StatusChangedAt))
	serr := p.Seen.Reload()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seenErr = serr
	if err != nil {
		return err
	}
	ss := slices.Clone(p.sessions) // the board may still hold the old slice
	for i := range ss {
		if ss[i].ID == s.ID && ss[i].Done && ss[i].StatusChangedAt.Equal(s.StatusChangedAt) {
			ss[i].Done = false
			ss[i].AvatarState = avatarState(ss[i])
		}
	}
	p.sessions = ss
	return nil
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

// TogglePin pins s to the top of its group, or takes it off when it is pinned, and says which it
// did. What is pinned is what pins.json holds now, not what the caller saw: two presses in a row
// act on the same snapshot, and another board may have changed the file. Pins are kept by name
// (AvatarKey), like avatar bindings: they hold under every grouping, across restarts of the board,
// and across a session's restart or handoff, which give it a new ID.
func (p *Poller) TogglePin(s Session) (pinned bool, err error) {
	if p.Pins == nil {
		return false, nil
	}
	p.refreshMu.Lock() // as MarkSeen: a poll in flight was built without it
	defer p.refreshMu.Unlock()
	key, at := s.AvatarKey(), p.now().UTC()
	if err = p.Pins.Reload(); err == nil {
		if _, on := p.Pins.Get(key); on {
			err = p.Pins.Prune(func(k string) bool { return k != key })
		} else if err = p.Pins.Set(key, at.Format(time.RFC3339Nano)); err == nil {
			pinned = true
		}
	}
	perr := p.Pins.Reload()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pinsErr = perr
	if err != nil {
		return false, err
	}
	ss := slices.Clone(p.sessions)
	for i := range ss {
		if ss[i].AvatarKey() == key { // every session of that name
			ss[i].Pinned, ss[i].PinnedAt = pinned, time.Time{}
			if pinned {
				ss[i].PinnedAt = at
			}
		}
	}
	p.sessions = ss
	return pinned, nil
}

// pinGrace is how long no session may have a pinned name before the pin goes: ccmux restart
// leaves the session out of ccmux's list for a few seconds, a daemon just started answers with
// none.
const pinGrace = 60 * time.Second

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// prunePins drops the pins of names that no session in ss has worn for pinGrace running; the time
// a name was first missing is kept in memory, and a name that comes back starts over.
func (p *Poller) prunePins(ss []Session) error {
	live := map[string]bool{}
	for _, s := range ss {
		live[s.AvatarKey()] = true
	}
	now := p.now()
	if p.pinGone == nil {
		p.pinGone = map[string]time.Time{}
	}
	for k := range live {
		delete(p.pinGone, k)
	}
	err := p.Pins.Prune(func(k string) bool { // may be asked twice about a key: it must not restart the clock
		if live[k] {
			return true
		}
		if _, ok := p.pinGone[k]; !ok {
			p.pinGone[k] = now
		}
		return now.Sub(p.pinGone[k]) <= pinGrace
	})
	for k, since := range p.pinGone {
		if now.Sub(since) > pinGrace {
			delete(p.pinGone, k)
		}
	}
	return err
}

// pinnedAt is when the session was pinned (ok false: it is not). A time that does not parse still
// counts as pinned, before every other.
func (p *Poller) pinnedAt(key string) (t time.Time, ok bool) {
	if p.Pins == nil {
		return t, false
	}
	v, ok := p.Pins.Get(key)
	t, _ = time.Parse(time.RFC3339Nano, v)
	return t, ok
}

// Refresh polls the sources once.
func (p *Poller) Refresh() error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	var aerr, serr, perr error
	if p.Avatars != nil { // another board, or a hand edit, may have changed avatars.json
		aerr = p.Avatars.Reload()
	}
	if p.Seen != nil {
		serr = p.Seen.Reload()
	}
	if p.Pins != nil {
		perr = p.Pins.Reload()
	}
	var raw []Raw
	var err error
	for _, src := range p.Sources {
		var rs []Raw
		if rs, err = src.Sessions(); err != nil {
			break
		}
		raw = append(raw, rs...)
	}
	var ss []Session
	if err == nil {
		live := map[string]bool{}
		if p.worked == nil {
			p.worked = map[string]bool{}
		}
		if p.firstAt == nil {
			p.firstAt = map[string]time.Time{}
		}
		for _, r := range raw {
			live[r.ID] = true
			if _, ok := p.firstAt[r.ID]; !ok {
				p.firstAt[r.ID] = r.StatusChangedAt
			}
			// worked: seen busy, or its status changed since it was first seen (a short answer between two polls)
			p.worked[r.ID] = p.worked[r.ID] || r.Status != "idle" || r.StatusChangedAt.After(p.firstAt[r.ID])
		}
		maps.DeleteFunc(p.worked, func(id string, _ bool) bool { return !live[id] })
		maps.DeleteFunc(p.firstAt, func(id string, _ time.Time) bool { return !live[id] })
		if p.Seen != nil { // a session that is gone is not "seen" any more
			if perr := p.Seen.Prune(func(id string) bool { return live[id] }); serr == nil {
				serr = perr
			}
		}
		ss = make([]Session, len(raw))
		pl := p.paneListing(raw)
		p.placeByPID(raw, pl)
		for i, r := range raw {
			ss[i] = p.convert(r, pl)
		}
		fillPanes(ss, pl)
		p.checkPiHint(raw)
		if p.Pins != nil {
			if e := p.prunePins(ss); perr == nil {
				perr = e
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err, p.avatarErr, p.seenErr, p.pinsErr = err, aerr, serr, perr
	if err == nil {
		p.hint = p.hintNow
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

func (p *Poller) convert(r Raw, pl paneListing) Session {
	s := Session{
		ID: r.ID, AgentType: r.AgentType, CWD: r.CWD, Branch: r.Branch,
		Status: r.Status, Attention: r.Attention, PendingTool: r.PendingTool, TmuxPane: r.Pane,
		ContextPct: -1, LastActivity: r.LastActivity, Project: r.Project, MainRepoRoot: r.MainRepoRoot, PID: r.PID, SessionID: r.SessionID, Transcript: r.Transcript,
		TmuxSession: r.TmuxSession, Background: r.Background,
	}
	if r.AgentType == "pi" {
		r.Transcript = "" // not Claude Code's format: the extension says all there is, and the Session keeps the path to resume it by
	}
	var tail [][]byte // the end of the transcript: the model, the agent, the branch and the title are looked for in it
	if r.Transcript != "" {
		tail = tailLines(r.Transcript)
	}
	if s.Project == "" {
		s.Project, s.MainRepoRoot = p.repo(r.CWD)
	}
	if s.Branch == "" {
		s.Branch = branchIn(tail)
	}
	if s.Branch == "" {
		s.Branch = p.gitBranch(r.CWD)
	}
	if s.PendingTool == "" && s.Status != "idle" {
		s.PendingTool = pendingIn(tail)
	}
	if s.Status == "waiting" && s.PendingTool == "ExitPlanMode" { // a plan to approve, not a question
		s.Attention = "plan_approval"
	}
	s.Name = p.name(r, pl, tail, s.Project)
	s.StatusChangedAt = r.StatusChangedAt
	s.Done = s.Status == "idle" && p.worked[r.ID] && !r.StatusChangedAt.IsZero() && !p.seen(r)
	s.PinnedAt, s.Pinned = p.pinnedAt(s.AvatarKey())
	s.AvatarState = avatarState(s)
	s.GroupKey = groupKey(s)
	s.AvatarID = avatar.For(p.Avatars, p.AvatarBind, p.AvatarDefault, s.AvatarKey())
	hudModel := ""
	if r.SessionID != "" {
		s.ContextPct, hudModel, s.Effort = readHUD(filepath.Join(p.StateDir, r.SessionID+".json"))
	}
	if r.Transcript != "" {
		s.Model = modelIn(tail)
		s.Agent = p.agent(r.Transcript, tail)
	}
	if a := recordedAgent(r.SessionID); a != "" { // the SessionStart hook's record beats the transcript, which a /clear leaves without the agent
		s.Agent = a
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

// name is what the card is called: the name the user (or a peer) gave it in the register, else the
// title the session was given (the last custom-title of the transcript), else the summary in its
// pane's title (the text after Claude Code's spinner), else the name Claude made up for it, else the project.
func (p *Poller) name(r Raw, pl paneListing, tail [][]byte, project string) string {
	if r.Name != "" && !r.NameDerived {
		return r.Name
	}
	if r.Transcript != "" {
		if t := p.title(r.ID, r.Transcript, tail); t != "" {
			return t
		}
	}
	if t := paneSummary(pl.title[r.Pane]); t != "" && r.AgentType != "pi" { // pi's title is "π - <dir>", not a summary
		return t
	}
	if r.Name != "" {
		return r.Name
	}
	return project
}

// paneSummary is a pane title without Claude Code's status mark in front (✳ idle, a braille spinner
// working); "Claude Code", which it is before the session has a summary, is none.
func paneSummary(title string) string {
	t := strings.TrimSpace(strings.TrimLeftFunc(strings.TrimSpace(title), func(r rune) bool { return r == '✳' || r >= 0x2800 && r <= 0x28FF || unicode.IsSpace(r) }))
	if t == "Claude Code" {
		return ""
	}
	return t
}

// seen: the user already saw this very done on the board.
func (p *Poller) seen(r Raw) bool {
	if p.Seen == nil {
		return false
	}
	k, ok := p.Seen.Get(r.ID)
	return ok && k == seenKey(r.StatusChangedAt)
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

// tailLines are the lines of the final 64KB of a jsonl transcript (files reach 16MB, so never read
// them whole): the first, cut line left out. nil when it cannot be read.
func tailLines(path string) [][]byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	// one extra byte: if the cut lands exactly on a line start, the dropped "partial" first line is empty
	off := max(fi.Size()-tailBytes-1, 0)
	buf, err := io.ReadAll(io.NewSectionReader(f, off, fi.Size()-off))
	if err != nil {
		return nil
	}
	lines := bytes.Split(buf, []byte("\n"))
	if off > 0 {
		lines = lines[1:] // first line is cut mid-way
	}
	return lines
}

// lastModel returns message.model of the last assistant entry in the final 64KB of a transcript.
// ponytail: an assistant line longer than 64KB is missed; widen tailBytes if models show blank.
func lastModel(path string) string { return modelIn(tailLines(path)) }

func modelIn(lines [][]byte) string {
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

// recordedAgent is the agent role a SessionStart hook wrote for the session to
// ${XDG_STATE_HOME:-~/.local/state}/claude-agent/<session id>: one line, "-" for none. "" when there
// is no such file or no role in it.
func recordedAgent(sessionID string) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "claude-agent", sessionID))
	if a := strings.TrimSpace(string(data)); err == nil && a != "-" {
		return a
	}
	return ""
}

// agent is the session's agent definition: the last agent-setting line in the tail that
// lastModel reads anyway (Claude Code writes the line again on every resume and when the agent is
// changed, so the last one is the current one, and a session first resumed with --agent long
// after it started has it near the end); else the first one in the head of the transcript. The
// head never changes (the file only grows), so what it says is kept per path; the tail is read
// on every poll and not kept.
func (p *Poller) agent(path string, tail [][]byte) string {
	if a := agentIn(tail); a != "" {
		return a
	}
	if a, ok := p.heads[path]; ok {
		return a
	}
	a, final := agentSetting(path)
	if final {
		if p.heads == nil {
			p.heads = map[string]string{}
		}
		p.heads[path] = a
	}
	return a
}

// agentIn is the agentSetting of the last agent-setting line among lines, "" if none.
func agentIn(lines [][]byte) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if a := parseAgent(lines[i]); a != "" {
			return a
		}
	}
	return ""
}

func parseAgent(line []byte) string {
	if !bytes.Contains(line, []byte(`"agent-setting"`)) {
		return ""
	}
	var e struct {
		Type         string `json:"type"`
		AgentSetting string `json:"agentSetting"`
	}
	if json.Unmarshal(line, &e) == nil && e.Type == "agent-setting" {
		return e.AgentSetting
	}
	return ""
}

const headBytes = 64 << 10

// agentSetting returns agentSetting of the first agent-setting line in the first 64KB of a
// transcript. final says whether asking again could change the answer: no, when it was found or
// when the transcript is longer than what was read.
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
		if a := parseAgent(l); a != "" {
			return a, true
		}
	}
	return "", len(buf) == headBytes
}
