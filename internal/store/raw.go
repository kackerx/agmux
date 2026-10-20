package store

import "time"

// Raw is one live session as a source found it, whatever the agent: the Poller turns it into a
// Session (the model, the context, the avatar, done, the pins come from the Poller's own files).
type Raw struct {
	ID, AgentType, SessionID string // SessionID names the statusline file and the transcript; ID is what the board and the user call it (a background job's id)
	PID                      int
	CWD                      string
	Pane, TmuxSession        string // "" with no pane; the Poller replaces TmuxSession by what tmux says the pane is in
	Name                     string
	NameDerived              bool   // Name was made up by the agent (not given by the user or a peer): a title the session was given beats it
	Status                   string // working | idle | waiting
	Attention                string // permission | question | plan_approval | ""
	StatusChangedAt          time.Time
	LastActivity             time.Time
	Transcript               string // path of the session's jsonl, "" if unknown
	Background               bool
	PendingTool              string // the source knows it (else the Poller reads it off the transcript)
	Branch                   string // likewise
	Project, MainRepoRoot    string // likewise: the Poller asks git about CWD
}

// Source lists the live sessions of one kind of agent. The Poller merges every source's list; a
// source that fails is a failed poll (the sessions of the last good one stay).
type Source interface {
	Sessions() ([]Raw, error)
}

// SourceFunc is a Source that is a function; the tests use it.
type SourceFunc func() ([]Raw, error)

func (f SourceFunc) Sessions() ([]Raw, error) { return f() }
