package store

import (
	"path"
	"strings"

	"github.com/kackerx/agmux/internal/execx"
)

// A background session has no pane of its own. The board gives it one by running `claude attach
// <id>` in a tmux window; which pane that is, is read off tmux on every poll (not kept in a file:
// a restarted or a second board finds the same).

// TmuxPanes lists every pane as "<pane id>\t<its session>\t<1 if it is dead>\t<the command it was started with>"
// lines.
func TmuxPanes() (string, error) {
	out, err := execx.Run("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{session_name}\t#{pane_dead}\t#{pane_start_command}")
	return string(out), err
}

// attachedPane is where `claude attach <id>` runs: the pane and the tmux session it is in.
type attachedPane struct{ pane, session string }

// attachPanes maps a session ID to the first pane that was started with `claude attach <id>`.
func attachPanes(listing string) map[string]attachedPane {
	panes := map[string]attachedPane{}
	unquote := strings.NewReplacer(`"`, "", `'`, "")
	for _, line := range strings.Split(listing, "\n") {
		pane, rest, ok := strings.Cut(line, "\t")
		session, rest, ok2 := strings.Cut(rest, "\t") // a session name has no tab; the command may
		dead, cmd, ok3 := strings.Cut(rest, "\t")
		if !ok || !ok2 || !ok3 || pane == "" || dead == "1" { // a dead pane (remain-on-exit) is a window nobody can use
			continue
		}
		f := strings.Fields(unquote.Replace(cmd))
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "attach" && path.Base(f[i-1]) == "claude" {
				if _, seen := panes[f[i+1]]; !seen {
					panes[f[i+1]] = attachedPane{pane, session}
				}
			}
		}
	}
	return panes
}

// paneSessions maps each pane to the tmux session it is in.
func paneSessions(listing string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(listing, "\n") {
		if pane, rest, ok := strings.Cut(line, "\t"); ok && pane != "" {
			m[pane], _, _ = strings.Cut(rest, "\t")
		}
	}
	return m
}

// fillPanes takes from tmux what the sessions' own records get wrong or lack: the tmux session
// of a pane (a record names it as it was when the process started), and for a background
// session that has no pane the one `claude attach` runs in, if any. Not being able to ask is the
// same as there being nothing to find.
func (p *Poller) fillPanes(ss []Session) {
	if p.Panes == nil {
		return
	}
	need := false
	for _, s := range ss {
		need = need || s.TmuxPane != "" || s.Background
	}
	if !need {
		return
	}
	out, err := p.Panes()
	if err != nil {
		return
	}
	attached, in := attachPanes(out), paneSessions(out)
	for i := range ss {
		switch {
		case ss[i].TmuxPane != "":
			if name, ok := in[ss[i].TmuxPane]; ok {
				ss[i].TmuxSession = name
			}
		case ss[i].Background:
			ss[i].TmuxPane, ss[i].TmuxSession = attached[ss[i].ID].pane, attached[ss[i].ID].session
		}
	}
}
