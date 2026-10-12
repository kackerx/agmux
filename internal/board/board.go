// Package board is the full-screen session board (`agmux board`).
package board

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/store"
)

// Source is what the board reads: the store plus its last poll error.
type Source interface {
	store.Store
	Err() error
}

// Runner runs ccmux subcommands; tests swap in one that records them.
type Runner interface {
	Run(args ...string) error             // error carries ccmux's stderr
	Interactive(args ...string) *exec.Cmd // gets the terminal via tea.ExecProcess
}

type ccmuxRunner struct{}

func (ccmuxRunner) Run(args ...string) error {
	_, err := ccmux.Run(args...)
	return err
}

func (ccmuxRunner) Interactive(args ...string) *exec.Cmd { return exec.Command("ccmux", args...) }

const frameEvery = 500 * time.Millisecond

type mode int

const (
	browsing mode = iota
	confirmKill
	typing
)

type Model struct {
	src    Source
	run    Runner
	now    func() time.Time
	w, h   int
	frame  int
	focus  string // session ID; follows the session across polls
	msg    string // one line above the footer: ccmux stderr, hints
	mode   mode
	target store.Session // what the open x / s prompt acts on, fixed when it opened
	input  []rune        // the `s` line being typed
}

func New(src Source, run Runner) Model { return Model{src: src, run: run, now: time.Now} }

// Run polls p in the background and runs the board until the user quits.
func Run(p *store.Poller) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	_, err := tea.NewProgram(New(p, ccmuxRunner{}), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

type frameMsg struct{}

// doneMsg reports a finished ccmux command.
type doneMsg struct {
	ok   string // message on success
	err  error
	quit bool // leave the board on success (switch)
}

func nextFrame() tea.Cmd { return tea.Tick(frameEvery, func(time.Time) tea.Msg { return frameMsg{} }) }

func (m Model) Init() tea.Cmd { return nextFrame() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case frameMsg:
		m.frame++
		return m, nextFrame()
	case doneMsg:
		switch {
		case msg.err != nil:
			m.msg = fg(cRed, " "+firstLine(msg.err.Error()))
		case msg.quit:
			return m, tea.Quit
		default:
			m.msg = fg(cMuted, " "+msg.ok)
		}
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return m, nil
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// current resolves focus against the live sessions, falling back to the first card.
func (m *Model) current() (gs []group, gi, ci int, ok bool) {
	gs = groups(m.src.Sessions())
	for gi, g := range gs {
		for ci, s := range g.sessions {
			if s.ID == m.focus {
				return gs, gi, ci, true
			}
		}
	}
	if len(gs) == 0 {
		m.focus = ""
		return gs, 0, 0, false
	}
	m.focus = gs[0].sessions[0].ID
	return gs, 0, 0, true
}

// alive reports whether a session is still in the latest poll.
func (m Model) alive(id string) bool {
	for _, s := range m.src.Sessions() {
		if s.ID == id {
			return true
		}
	}
	return false
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// bubbletea v1 hands keys read in one burst ("ljd") over as one message; outside the
	// text prompt each rune is its own key, and their commands run in typed order.
	if m.mode != typing && k.Type == tea.KeyRunes && len(k.Runes) > 1 && !k.Paste {
		var tm tea.Model = m
		cmds := make([]tea.Cmd, len(k.Runes))
		for i, r := range k.Runes {
			tm, cmds[i] = tm.(Model).key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		return tm, tea.Sequence(cmds...)
	}
	switch m.mode {
	case confirmKill:
		m.mode = browsing
		if k.String() != "y" {
			return m, nil
		}
		return m.kill(m.target)
	case typing:
		switch k.Type {
		case tea.KeyEnter:
			m.mode = browsing
			text := strings.TrimSpace(string(m.input))
			m.input = nil
			if text == "" {
				return m, nil
			}
			return m.send(m.target, text, "已发送 → "+m.target.Name)
		case tea.KeyEsc:
			m.mode, m.input = browsing, nil
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			for _, r := range k.Runes { // a pasted newline would submit a half-written prompt
				if r == '\n' || r == '\r' || r == '\t' {
					r = ' '
				}
				m.input = append(m.input, r)
			}
		}
		return m, nil
	}

	if k.String() == "q" || k.Type == tea.KeyEsc {
		return m, tea.Quit
	}
	gs, gi, ci, ok := m.current()
	if !ok {
		return m, nil
	}
	s := gs[gi].sessions[ci]
	switch k.String() {
	case "j", "down":
		ci = min(ci+1, len(gs[gi].sessions)-1)
	case "k", "up":
		ci = max(ci-1, 0)
	case "l", "right":
		gi = min(gi+1, len(gs)-1)
		ci = min(ci, len(gs[gi].sessions)-1)
	case "h", "left":
		gi = max(gi-1, 0)
		ci = min(ci, len(gs[gi].sessions)-1)
	case "enter":
		return m.jump(s)
	case "H":
		return m.send(s, "/handoff", "已发送 /handoff → "+s.Name)
	case "s":
		if s.TmuxPane == "" {
			return m.say(cMuted, noPane), nil
		}
		m.mode, m.target, m.msg = typing, s, ""
		return m, nil
	case "x":
		m.mode, m.target, m.msg = confirmKill, s, ""
		return m, nil
	case "r":
		m.msg = fg(cMuted, " 重启 "+s.Name+"…")
		return m, m.ccmux("已重启 "+s.Name, "restart", s.ID)
	case "d":
		return m, tea.ExecProcess(m.run.Interactive("review", s.ID), func(err error) tea.Msg {
			return doneMsg{ok: "review 结束：" + s.Name, err: err}
		})
	default:
		return m, nil
	}
	m.focus, m.msg = gs[gi].sessions[ci].ID, ""
	return m, nil
}

const noPane = "后台会话没有 tmux pane：不能跳转 / 发送"

func (m Model) say(c lipgloss.Color, text string) Model {
	m.msg = fg(c, " "+text)
	return m
}

// ccmux runs `ccmux args...` off the UI goroutine and reports back with a doneMsg.
func (m Model) ccmux(ok string, args ...string) tea.Cmd {
	r := m.run
	return func() tea.Msg { return doneMsg{ok: ok, err: r.Run(args...)} }
}

// jump switches tmux to the session and leaves the board (the popup must close to show it).
func (m Model) jump(s store.Session) (tea.Model, tea.Cmd) {
	if s.TmuxPane == "" {
		return m.say(cMuted, noPane), nil
	}
	r := m.run
	return m, func() tea.Msg { return doneMsg{err: r.Run("switch", s.ID), quit: true} }
}

func (m Model) send(s store.Session, text, ok string) (tea.Model, tea.Cmd) {
	switch {
	case s.TmuxPane == "":
		return m.say(cMuted, noPane), nil
	case !m.alive(s.ID):
		return m.say(cRed, s.Name+" 已经不在了，没有发送"), nil
	}
	return m, m.ccmux(ok, "send", s.ID, text)
}

func (m Model) kill(s store.Session) (tea.Model, tea.Cmd) {
	if !m.alive(s.ID) {
		return m.say(cMuted, s.Name+" 已经不在了"), nil
	}
	return m, m.ccmux("已杀掉 "+s.Name, "kill", s.ID)
}

// mouse: click a card to focus it, click the focused card again to jump to it.
func (m Model) mouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != browsing || e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft || e.Y < 1 || e.Y > m.h-3 {
		return m, nil
	}
	m.current()
	ss := m.src.Sessions()
	// ponytail: re-lays out the board per click to find the card; fine for tens of sessions.
	_, rects, off := view(ss, m.w, m.h, m.focus, m.frame, m.now(), "", false)
	y := e.Y - 1 + off // row 0 is the top bar
	for _, r := range rects {
		if e.X >= r.x && e.X < r.x+r.w && y >= r.y && y < r.y+r.h {
			if r.id != m.focus {
				m.focus, m.msg = r.id, ""
				return m, nil
			}
			for _, s := range ss {
				if s.ID == r.id {
					return m.jump(s)
				}
			}
		}
	}
	return m, nil
}

// msgLine is the line above the footer: an open prompt, else the last result,
// poll / avatars errors and the background hint.
func (m Model) msgLine(focused store.Session) string {
	switch m.mode {
	case confirmKill:
		return fg(cRed, " 杀掉 "+m.target.Name+"？y 确认，其它键取消")
	case typing:
		p := " 发送到 " + m.target.Name + ": " + string(m.input) + "▏"
		if w := ansi.StringWidth(p); w > m.w { // keep the end (and the cursor) in view
			p = "…" + ansi.TruncateLeft(p, w-m.w+1, "")
		}
		return p
	}
	var parts []string
	if m.msg != "" {
		parts = append(parts, m.msg)
	}
	if err := m.src.Err(); err != nil {
		parts = append(parts, fg(cRed, " "+err.Error()))
	}
	if focused.ID != "" && focused.TmuxPane == "" {
		parts = append(parts, fg(cMuted, " "+noPane))
	}
	return strings.Join(parts, fg(cMuted, " ·"))
}

func (m Model) View() string {
	if m.w == 0 {
		return ""
	}
	gs, gi, ci, ok := m.current()
	var focused store.Session
	if ok {
		focused = gs[gi].sessions[ci]
	}
	out, _, _ := view(m.src.Sessions(), m.w, m.h, m.focus, m.frame, m.now(), m.msgLine(focused), ok && focused.TmuxPane == "")
	return out
}
