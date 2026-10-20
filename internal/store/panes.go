package store

import (
	"path"
	"strings"

	"github.com/kackerx/agmux/internal/execx"
)

// A background session has no pane of its own. The board gives it one by running `claude attach
// <id>` in a tmux window; which pane that is, is read off tmux on every poll (not kept in a file:
// a restarted or a second board finds the same).

// TmuxPanes lists every pane as "<pane id>\t<its session>\t<1 if it is dead>\t<the command it was started with>\t<its title>"
// lines (a line of the first four is as good: the title is for the name of a card).
func TmuxPanes() (string, error) {
	out, err := execx.Run("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{session_name}\t#{pane_dead}\t#{pane_start_command}\t#{pane_title}")
	return string(out), err
}

// shortSession is a tmux session name as the board knows it: up to the first " # " (a session
// named "agmux # main" is "agmux", every session of the repo one group).
func shortSession(name string) string {
	short, _, _ := strings.Cut(name, " # ")
	return short
}

// paneLine is one line of the TmuxPanes listing; ok is false for one that is too short. The command
// may hold a tab: with five or more fields the title is the last and the command what lies between.
func paneLine(line string) (pane, session string, dead bool, cmd, title string, ok bool) {
	f := strings.Split(line, "\t")
	if len(f) < 4 || f[0] == "" {
		return
	}
	cmd = strings.Join(f[3:], "\t")
	if len(f) >= 5 {
		cmd, title = strings.Join(f[3:len(f)-1], "\t"), f[len(f)-1]
	}
	return f[0], f[1], f[2] == "1", cmd, title, true
}

// attachedPane is where `claude attach <id>` runs: the pane and the tmux session it is in.
type attachedPane struct{ pane, session string }

// attachPanes maps a session ID to the first pane that was started with `claude attach <id>`.
func attachPanes(listing string) map[string]attachedPane {
	panes := map[string]attachedPane{}
	unquote := strings.NewReplacer(`"`, "", `'`, "")
	for _, line := range strings.Split(listing, "\n") {
		pane, session, dead, cmd, _, ok := paneLine(line)
		if !ok || dead { // a dead pane (remain-on-exit) is a window nobody can use
			continue
		}
		f := strings.Fields(unquote.Replace(cmd))
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "attach" && path.Base(f[i-1]) == "claude" {
				if _, seen := panes[f[i+1]]; !seen {
					panes[f[i+1]] = attachedPane{pane, shortSession(session)}
				}
			}
		}
	}
	return panes
}

// paneListing is what tmux says of the panes.
type paneListing struct {
	session  map[string]string // pane → its session (short)
	title    map[string]string // pane → its title
	attached map[string]attachedPane
}

// paneListing asks tmux when a session has a pane or is a background one (for the pane `claude
// attach` runs in): once per poll. Not being able to ask is the same as there being nothing.
func (p *Poller) paneListing(raw []Raw) paneListing {
	var pl paneListing
	need := false
	for _, r := range raw {
		need = need || r.Pane != "" || r.Background
	}
	if p.Panes == nil || !need {
		return pl
	}
	out, err := p.Panes()
	if err != nil {
		return pl
	}
	pl = paneListing{session: map[string]string{}, title: map[string]string{}, attached: attachPanes(out)}
	for _, line := range strings.Split(out, "\n") {
		if pane, session, _, _, title, ok := paneLine(line); ok {
			pl.session[pane], pl.title[pane] = shortSession(session), title
		}
	}
	return pl
}

// fillPanes takes from tmux what the sessions' own records get wrong or lack: the tmux session
// of a pane (a record names it as it was when the process started), and for a background
// session that has no pane the one `claude attach` runs in, if any.
func fillPanes(ss []Session, pl paneListing) {
	for i := range ss {
		switch {
		case ss[i].TmuxPane != "":
			if name, ok := pl.session[ss[i].TmuxPane]; ok {
				ss[i].TmuxSession = name
			}
		case ss[i].Background:
			ss[i].TmuxPane, ss[i].TmuxSession = pl.attached[ss[i].ID].pane, pl.attached[ss[i].ID].session
		}
	}
}
