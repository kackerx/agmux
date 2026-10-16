package board

import (
	"fmt"
	"slices"
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
	r := m.run
	return func() tea.Msg {
		pane, err := attachWindow(r, s)
		return attachedMsg{id: s.ID, pane: pane, action: action, err: err}
	}
}

// attachWindow runs `claude attach <id>` in a new window of the agmux-bg session (made if it isn't
// there) and returns its pane. -d: the user's client stays where it is.
func attachWindow(r Runner, s store.Session) (string, error) {
	var args []string
	if _, err := r.Tmux("has-session", "-t", "="+bgTmuxSession); err == nil {
		args = []string{"new-window", "-t", "=" + bgTmuxSession + ":"}
	} else {
		args = []string{"new-session", "-s", bgTmuxSession}
	}
	args = append(args, "-d")
	if s.Name != "" {
		args = append(args, "-n", s.Name)
	}
	if s.CWD != "" {
		args = append(args, "-c", s.CWD)
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
		out[i].TmuxPane = e.pane
	}
	if out != nil {
		return out
	}
	return ss
}
