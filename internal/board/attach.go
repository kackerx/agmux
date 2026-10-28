package board

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/store"
)

// A background session (claude --bg) has no tmux pane. Asked for with enter, i or tab, it gets one:
// `claude attach <id>` in a window of its own session, which the store then finds by its start
// command. From there the preview, pass-through, cursor and jump are the same as for any pane.

const bgTmuxSession = "agmux-bg"

// attachedMsg: the window for a background session was opened (or couldn't be); action is what to
// do now that it has a pane.
type attachedMsg struct {
	id, pane, action string
	err              error
}

// attach opens the window off the UI goroutine.
func (m Model) attach(s store.Session, action string) tea.Cmd {
	r, w, h := m.run, previewInner(previewW(m.w)), m.previewLinesN()
	return func() tea.Msg {
		pane, err := attachWindow(r, s, w, h)
		if err == nil && action == "handoff" { // typed at once, claude is not reading yet: the Enter is lost
			waitDrawn(r, pane)
		}
		return attachedMsg{id: s.ID, pane: pane, action: action, err: err}
	}
}

// drawnWait is how long a new window may take to show anything (claude attach does in about 0.2 s),
// polled every drawnPoll.
var drawnWait, drawnPoll = 5 * time.Second, 100 * time.Millisecond

// waitDrawn returns when the pane has something on its screen, or after drawnWait.
func waitDrawn(r Runner, pane string) {
	for end := time.Now().Add(drawnWait); ; time.Sleep(drawnPoll) {
		if out, err := r.Tmux("capture-pane", "-p", "-t", pane); err == nil && strings.TrimSpace(out) != "" {
			return
		}
		if !time.Now().Before(end) {
			return
		}
	}
}

// tmuxText is text for the -n and -c of new-window / new-session: tmux expands formats in them
// (#S, #T; a directory with a # in it would silently start the window in $HOME), and ## is a #.
func tmuxText(s string) string { return strings.ReplaceAll(s, "#", "##") }

// attachWindow runs `claude attach <id>` in a new window of the agmux-bg session (made if it isn't
// there) and returns its pane. -d: the user's client stays where it is. A session with no client
// has a window w × h cells (what the preview shows) to start with, and a window follows the client
// that comes (not set by resize-window, which would keep it that size); new windows take the
// session's.
func attachWindow(r Runner, s store.Session, w, h int) (string, error) {
	var args []string
	if _, err := r.Tmux("has-session", "-t", "="+bgTmuxSession); err == nil {
		args = []string{"new-window", "-t", "=" + bgTmuxSession + ":"}
	} else {
		args = []string{"new-session", "-s", bgTmuxSession, "-x", strconv.Itoa(w), "-y", strconv.Itoa(h)}
	}
	args = append(args, "-d")
	if s.Name != "" {
		args = append(args, "-n", tmuxText(s.Name))
	}
	if s.CWD != "" {
		args = append(args, "-c", tmuxText(s.CWD))
	}
	out, err := r.Tmux(append(args, "-P", "-F", "#{pane_id}", "--", "claude", "attach", s.ID)...)
	if err != nil {
		return "", fmt.Errorf("attach %s: %w", s.Name, err)
	}
	pane := strings.TrimSpace(out)
	if pane == "" {
		return "", fmt.Errorf("attach %s: tmux gave no pane", s.Name)
	}
	return pane, nil
}

// attachedSrc is the source with the panes just opened laid over it: the store learns of one at its
// next poll, and until then the board must already show it (the action that asked is going on).
// An entry goes when the store has the pane, or after attachedGrace without it (the window may have
// closed again, and the poll will not say so for a pane it never listed).
type attachedSrc struct {
	Source
	now   func() time.Time // nil = time.Now
	mu    sync.Mutex
	panes map[string]attachedPane
}

type attachedPane struct {
	pane string
	at   time.Time
}

const attachedGrace = 10 * time.Second

// hinter is a Source that has something to tell the user that is no error (the store's PiHint).
type hinter interface{ Hint() string }

// Hint passes the wrapped source's on.
func (a *attachedSrc) Hint() string {
	if h, ok := a.Source.(hinter); ok {
		return h.Hint()
	}
	return ""
}

func (a *attachedSrc) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *attachedSrc) add(id, pane string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panes == nil {
		a.panes = map[string]attachedPane{}
	}
	a.panes[id] = attachedPane{pane, a.clock()}
}

func (a *attachedSrc) Sessions() []store.Session {
	ss := a.Source.Sessions()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.panes) == 0 {
		return ss
	}
	var out []store.Session // a copy, made when there is something to change: ss is the store's
	for i, s := range ss {
		e, ok := a.panes[s.ID]
		if !ok {
			continue
		}
		if s.TmuxPane != "" || a.clock().Sub(e.at) > attachedGrace {
			delete(a.panes, s.ID)
			continue
		}
		if out == nil {
			out = slices.Clone(ss)
		}
		out[i].TmuxPane, out[i].TmuxSession = e.pane, bgTmuxSession
	}
	if out != nil {
		return out
	}
	return ss
}
