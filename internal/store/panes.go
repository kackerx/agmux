package store

import (
	"path"
	"strings"

	"github.com/kackerx/agmux/internal/execx"
)

// A background session has no pane of its own. The board gives it one by running `claude attach
// <id>` in a tmux window; which pane that is, is read off tmux on every poll (not kept in a file:
// a restarted or a second board finds the same).

// TmuxPanes lists every pane as "<pane id>\t<the command it was started with>" lines.
func TmuxPanes() (string, error) {
	out, err := execx.Run("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_start_command}")
	return string(out), err
}

// attachPanes maps a session ID to the first pane that was started with `claude attach <id>`.
func attachPanes(listing string) map[string]string {
	panes := map[string]string{}
	unquote := strings.NewReplacer(`"`, "", `'`, "")
	for _, line := range strings.Split(listing, "\n") {
		pane, cmd, ok := strings.Cut(line, "\t")
		if !ok || pane == "" {
			continue
		}
		f := strings.Fields(unquote.Replace(cmd))
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "attach" && path.Base(f[i-1]) == "claude" {
				if _, seen := panes[f[i+1]]; !seen {
					panes[f[i+1]] = pane
				}
			}
		}
	}
	return panes
}

// fillAttached gives the background sessions that have no pane the one `claude attach` runs in, if
// any. tmux is asked only when there is such a session, and not being able to is the same as
// there being none.
func (p *Poller) fillAttached(ss []Session) {
	if p.Panes == nil {
		return
	}
	need := false
	for _, s := range ss {
		need = need || s.Background && s.TmuxPane == ""
	}
	if !need {
		return
	}
	out, err := p.Panes()
	if err != nil {
		return
	}
	panes := attachPanes(out)
	for i := range ss {
		if ss[i].Background && ss[i].TmuxPane == "" {
			ss[i].TmuxPane = panes[ss[i].ID]
		}
	}
}
