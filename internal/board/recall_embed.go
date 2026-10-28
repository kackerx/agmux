package board

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/recall"
	"github.com/kackerx/agmux/internal/store"
)

// recallIndex is what the board's recall screen is made of: the index, opened the first time it is
// asked for (and kept: the next time only brings it up to date), and what the user chose to resume in
// place, for Run to hand to the caller when the board ends.
type recallIndex struct {
	ix   *recall.Index
	open func() (*recall.Index, error)
	res  *RecallResult
}

// resumeWindowMsg is the answer of the tmux window made to resume a closed session in.
type resumeWindowMsg struct {
	pane string
	err  error
}

// openRecall puts the recall screen where the board is: over the sessions that run now (the board's
// own table: nothing is polled again), in the directory's project.
func (m Model) openRecall() (tea.Model, tea.Cmd) {
	if m.w == 0 {
		return m, nil
	}
	if m.rx.ix == nil {
		ix, err := m.rx.open()
		if err != nil {
			return m.say(cRed, err.Error()), nil
		}
		m.rx.ix = ix
	}
	cwd, _ := os.Getwd()
	rm := newRecallModel(m.rx.ix, m.cfg, m.src.Sessions(), m.rx.ix.ProjectOf(cwd), "")
	rm.embedded, rm.w, rm.h, rm.building = true, m.w, m.h, m.resuming
	m.rc = &rm
	return m, rm.Init()
}

// recallUpdate gives the keys, and the answers the recall screen asked for, to it; the rest (the
// board keeps polling and drawing underneath) goes on as before. handled says the message was its.
func (m Model) recallUpdate(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.rc.w, m.rc.h = msg.Width, msg.Height // the board takes it too
		return m, nil, false
	case tea.KeyMsg, tea.MouseMsg, recallTickMsg, recallIndexedMsg, recallFoundMsg, recallEditedMsg:
		tm, cmd := m.rc.Update(msg)
		rm := tm.(recallModel)
		switch {
		case rm.leave:
			m.rc = nil
		case rm.pick != nil:
			pick := rm.pick
			rm.pick = nil
			m.rc = &rm
			tm, c := m.picked(pick)
			return tm, tea.Batch(cmd, c), true
		default:
			m.rc = &rm
		}
		return m, cmd, true
	}
	return m, nil, false
}

// floating: the board is shown through a popup (display-popup has no pane of its own; --stay keeps
// the board in a session of its own that a popup shows), so the screen the user looks at is not
// the board's terminal: a session resumed from here gets a window of its own.
func (m Model) floating() bool {
	return m.getenv("TMUX") != "" && (m.stay || m.getenv("TMUX_PANE") == "")
}

// picked is enter on a session in the recall screen. One that runs is jumped to, as the board's
// enter does. A closed one is resumed where the user is: in this very terminal when the board runs
// in an ordinary pane (the board ends and Run hands the command over), else in a new tmux window,
// started in the session's directory, that the client then switches to.
func (m Model) picked(p *recallPick) (tea.Model, tea.Cmd) {
	if p.live != nil {
		if p.live.TmuxPane == "" && !p.live.Background { // nowhere to jump: said where the screen is, not on the board it would drop to
			m.rc.msg = fg(cMuted, " "+i18n.T("recall.running_nopane", p.name))
			return m, nil
		}
		m.rc = nil
		return m.jump(*p.live)
	}
	if !m.floating() {
		m.rx.res = p.resume
		return m, tea.Quit
	}
	r, getenv, res, name := m.run, m.getenv, *p.resume, p.name
	m.rc.building = true
	m.resuming = true
	return m, func() tea.Msg {
		pane, err := newResumeWindow(r, getenv, res, name)
		return resumeWindowMsg{pane, err}
	}
}

// resumed: the window is there, go to it (the board's own jump: a popup closes, --stay keeps the
// board); or say why it is not.
func (m Model) resumed(msg resumeWindowMsg) (tea.Model, tea.Cmd) {
	m.resuming = false // kept on the board: the screen that asked may be gone, another one open
	if m.rc != nil {
		m.rc.building = false
	}
	if msg.err != nil {
		if m.rc != nil {
			m.rc.msg = fg(cRed, " "+firstLine(msg.err.Error()))
		} else {
			m = m.say(cRed, firstLine(msg.err.Error()))
		}
		return m, nil
	}
	m.rc = nil
	return m.jump(store.Session{TmuxPane: msg.pane})
}

// newResumeWindow runs the resume command in a new window of the user's tmux session (the one their
// client is in), without taking the focus, and returns its pane.
func newResumeWindow(r Runner, getenv func(string) string, res RecallResult, name string) (string, error) {
	pc, err := chooseClient(r, getenv)
	if err != nil {
		return "", err
	}
	args := []string{"new-window", "-d", "-P", "-F", "#{pane_id}"}
	session := ""
	for _, c := range pc.clients {
		if c.name == pc.name {
			session = c.session
		}
	}
	if session == "" { // without a target the window would be made in the board's own (hidden) session
		return "", fmt.Errorf("new-window: tmux did not say which session the client %s is in", pc.name)
	}
	args = append(args, "-t", "="+session+":")
	args = append(args, "-c", tmuxText(res.Dir))
	if name != "" {
		args = append(args, "-n", tmuxText(name))
	}
	out, err := r.Tmux(append(append(args, "--"), res.Argv...)...)
	if err != nil {
		return "", fmt.Errorf("new-window: %w", err)
	}
	if pane := strings.TrimSpace(out); pane != "" {
		return pane, nil
	}
	return "", fmt.Errorf("new-window: tmux did not say which pane")
}
