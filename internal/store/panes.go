package store

import (
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/kackerx/agmux/internal/execx"
)

// A background session has no pane of its own. The board gives it one by running `claude attach
// <id>` in a tmux window; which pane that is, is read off tmux on every poll (not kept in a file:
// a restarted or a second board finds the same).

// TmuxPanes lists every pane as "<pane id>\t<its session>\t<1 if it is dead>\t<the command it was started with>\t<its title>\t<its pid>"
// lines (a line of the first four, or five, is as good: the title is for the name of a card, the
// pid for finding the pane of a process).
func TmuxPanes() (string, error) {
	out, err := execx.Run("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{session_name}\t#{pane_dead}\t#{pane_start_command}\t#{pane_title}\t#{pane_pid}")
	return string(out), err
}

// shortSession is a tmux session name as the board knows it: up to the first " # " (a session
// named "agmux # main" is "agmux", every session of the repo one group).
func shortSession(name string) string {
	short, _, _ := strings.Cut(name, " # ")
	return short
}

// paneLine is one line of the TmuxPanes listing; ok is false for one that is too short. The command
// may hold a tab: with five fields the last is the title, with six or more the last is the pid, the
// one before the title, and the command is what lies between.
func paneLine(line string) (pane, session string, dead bool, cmd, title string, pid int, ok bool) {
	f := strings.Split(line, "\t")
	if len(f) < 4 || f[0] == "" {
		return
	}
	n := len(f)
	if n >= 6 {
		pid, _ = strconv.Atoi(f[n-1])
		n--
	}
	cmd = strings.Join(f[3:n], "\t")
	if n >= 5 {
		cmd, title = strings.Join(f[3:n-1], "\t"), f[n-1]
	}
	return f[0], f[1], f[2] == "1", cmd, title, pid, true
}

// attachedPane is where `claude attach <id>` runs: the pane and the tmux session it is in.
type attachedPane struct{ pane, session string }

// attachPanes maps a session ID to the first pane that was started with `claude attach <id>`.
func attachPanes(listing string) map[string]attachedPane {
	panes := map[string]attachedPane{}
	unquote := strings.NewReplacer(`"`, "", `'`, "")
	for _, line := range strings.Split(listing, "\n") {
		pane, session, dead, cmd, _, _, ok := paneLine(line)
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
	byPID    map[int]string    // pane's process → pane
	attached map[string]attachedPane
}

// paneListing asks tmux when a session has a pane or is a background one (for the pane `claude
// attach` runs in): once per poll. Not being able to ask is the same as there being nothing.
func (p *Poller) paneListing(raw []Raw) paneListing {
	var pl paneListing
	need := false
	for _, r := range raw {
		need = need || r.Pane != "" || r.Background || r.AgentType == "pi" // a pi's pid is looked up in the panes
	}
	if p.Panes == nil || !need {
		return pl
	}
	out, err := p.Panes()
	if err != nil {
		return pl
	}
	pl = paneListing{session: map[string]string{}, title: map[string]string{}, byPID: map[int]string{}, attached: attachPanes(out)}
	for _, line := range strings.Split(out, "\n") {
		if pane, session, _, _, title, pid, ok := paneLine(line); ok {
			pl.session[pane], pl.title[pane] = shortSession(session), title
			if pid > 0 {
				pl.byPID[pid] = pane
			}
		}
	}
	return pl
}

// procs is the process table: the parent of each process, and the name (comm) of each.
type procs struct {
	parent map[int]int
	comm   map[int]string
}

// psProcs reads the process table with one ps.
func psProcs() (procs, error) {
	cmd := exec.Command("ps", "-axo", "pid=,ppid=,comm=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return procs{}, err
	}
	t := procs{parent: map[int]int{}, comm: map[int]string{}}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			t.parent[pid], t.comm[pid] = ppid, strings.Join(f[2:], " ")
		}
	}
	return t, nil
}

// placeByPID gives the pi sessions (the extension does not know the pane; Claude Code's register
// does) the pane their process runs under: the first ancestor that is a pane's process.
func (p *Poller) placeByPID(raw []Raw, pl paneListing) {
	if len(pl.byPID) == 0 || p.Procs == nil {
		return
	}
	var t procs
	for i := range raw {
		if raw[i].AgentType != "pi" || raw[i].Pane != "" || raw[i].PID <= 0 {
			continue
		}
		if t.parent == nil {
			var err error
			if t, err = p.Procs(); err != nil {
				return
			}
		}
		for pid, hops := raw[i].PID, 0; pid > 1 && hops < 64; pid, hops = t.parent[pid], hops+1 {
			if pane, ok := pl.byPID[pid]; ok {
				raw[i].Pane = pane
				break
			}
		}
	}
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
