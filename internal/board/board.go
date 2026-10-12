// Package board is the full-screen session board (`agmux board`).
package board

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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
	target store.Session   // what the open x / s prompt acts on, fixed when it opened
	input  []rune          // the `s` line being typed
	folded map[string]bool // group keys folded with f; this run only
}

func New(src Source, run Runner) Model {
	return Model{src: src, run: run, now: time.Now, folded: map[string]bool{}}
}

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
	ok      string // message on success
	err     error
	quit    bool // leave the board on success (switch)
	retrack bool // came back from ExecProcess: bubbletea v1 leaves the mouse off, turn it back on
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
		if msg.retrack {
			return m, tea.EnableMouseCellMotion
		}
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return m, nil
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// items lists, per group, what the focus can rest on: the title of a folded group, else its cards.
func (m Model) items(gs []group) [][]string {
	out := make([][]string, len(gs))
	for i, g := range gs {
		if m.folded[g.key] {
			out[i] = []string{headerID(g.key)}
			continue
		}
		for _, s := range g.sessions {
			out[i] = append(out[i], s.ID)
		}
	}
	return out
}

// current resolves focus against the live sessions, falling back to the first item.
// s is the focused session; ok is false when there is none (nothing to show, or a folded title).
func (m *Model) current() (gs []group, gi int, s store.Session, ok bool) {
	gs = groups(m.src.Sessions())
	items := m.items(gs)
	gi, ci := -1, -1
	for g := range items {
		for c, id := range items[g] {
			if id == m.focus {
				gi, ci = g, c
			}
		}
	}
	if gi < 0 {
		if len(gs) == 0 {
			m.focus = ""
			return gs, 0, s, false
		}
		gi, ci = 0, 0
		m.focus = items[0][0]
	}
	if m.folded[gs[gi].key] {
		return gs, gi, s, false
	}
	return gs, gi, gs[gi].sessions[ci], true
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
	gs, gi, s, ok := m.current()
	if len(gs) == 0 {
		return m, nil
	}
	key := gs[gi].key
	if d, ok := directions[k.String()]; ok {
		// ponytail: lays the board out again per key press; fine for tens of sessions.
		_, rects := layout(gs, m.w, m.focus, m.frame, m.now(), m.folded)
		if id := nearest(rects, m.focus, d); id != "" {
			m.focus, m.msg = id, ""
		}
		return m, nil
	}
	switch k.String() {
	case "f": // fold / unfold the focused group; the focus stays on it
		m.folded[key] = !m.folded[key]
		m.focus, m.msg = m.items(gs)[gi][0], ""
		return m, nil
	case "enter":
		if !ok { // a folded title: unfold it
			m.folded[key] = false
			m.focus, m.msg = m.items(gs)[gi][0], ""
			return m, nil
		}
		return m.jump(s)
	}
	if !ok { // the rest act on a session; a folded title has none
		return m, nil
	}
	switch k.String() {
	case "H":
		return m.send(s, "/handoff", "已发送 /handoff → "+s.Name)
	case "s":
		if s.TmuxPane == "" {
			return m, nil // msgLine already shows noPane
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
		return m, tea.ExecProcess(m.review(s))
	}
	return m, nil
}

// directions maps the movement keys to a unit step on screen.
var directions = map[string][2]int{
	"j": {0, 1}, "down": {0, 1}, "k": {0, -1}, "up": {0, -1},
	"l": {1, 0}, "right": {1, 0}, "h": {-1, 0}, "left": {-1, 0},
}

// nearest finds where the focus moves from rect id in direction d ("" = at the edge, stay):
// among the rects entirely on that side, the closest one that overlaps it across the direction of
// travel, ties going to the most overlap. Sideways (h/l) a card may overlap nothing there (a folded
// title sits on the title row, columns differ in height); then the nearest column's vertically
// closest one is taken, so h/l never strands the focus. Up/down stays strict.
func nearest(rects []rect, id string, d [2]int) string {
	var cur rect
	for _, r := range rects {
		if r.id == id {
			cur = r
		}
	}
	if cur.id == "" {
		return ""
	}
	// overlap of two intervals; negative = the gap between them
	overlap := func(a1, a2, b1, b2 int) int { return min(a2, b2) - max(a1, b1) }
	best, bestKey := "", [4]int{}
	for _, r := range rects {
		var gap, ov int
		switch d {
		case [2]int{0, 1}:
			gap, ov = r.y-(cur.y+cur.h), overlap(cur.x, cur.x+cur.w, r.x, r.x+r.w)
		case [2]int{0, -1}:
			gap, ov = cur.y-(r.y+r.h), overlap(cur.x, cur.x+cur.w, r.x, r.x+r.w)
		case [2]int{1, 0}:
			gap, ov = r.x-(cur.x+cur.w), overlap(cur.y, cur.y+cur.h, r.y, r.y+r.h)
		case [2]int{-1, 0}:
			gap, ov = cur.x-(r.x+r.w), overlap(cur.y, cur.y+cur.h, r.y, r.y+r.h)
		}
		if r.id == id || gap < 0 || ov <= 0 && d[0] == 0 {
			continue
		}
		miss := 0 // 1 = no overlap (sideways fallback): always loses to an overlapping one
		if ov <= 0 {
			miss = 1
		}
		key := [4]int{miss, gap, max(-ov, 0), -ov}
		if best == "" || less(key, bestKey) {
			best, bestKey = r.id, key
		}
	}
	return best
}

func less(a, b [4]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

const noPane = "后台会话没有 tmux pane：不能跳转 / 发送"

func (m Model) say(c lipgloss.Color, text string) Model {
	m.msg = fg(c, " "+text)
	return m
}

// review hands the terminal to `ccmux review`. Its stderr still reaches the terminal, and a
// copy is kept so a failure shows ccmux's last word instead of a bare "exit status 1".
func (m Model) review(s store.Session) (*exec.Cmd, tea.ExecCallback) {
	c := m.run.Interactive("review", s.ID)
	var buf bytes.Buffer
	c.Stderr = io.MultiWriter(os.Stderr, &buf)
	return c, func(err error) tea.Msg {
		if err != nil {
			if last := lastLine(buf.String()); last != "" {
				err = errors.New("ccmux review: " + last)
			}
		}
		return doneMsg{ok: "review 结束：" + s.Name, err: err, retrack: true}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// ccmux runs `ccmux args...` off the UI goroutine and reports back with a doneMsg.
func (m Model) ccmux(ok string, args ...string) tea.Cmd {
	r := m.run
	return func() tea.Msg { return doneMsg{ok: ok, err: r.Run(args...)} }
}

// jump switches tmux to the session and leaves the board (the popup must close to show it).
func (m Model) jump(s store.Session) (tea.Model, tea.Cmd) {
	if s.TmuxPane == "" {
		return m, nil // msgLine already shows noPane
	}
	r := m.run
	return m, func() tea.Msg { return doneMsg{err: r.Run("switch", s.ID), quit: true} }
}

func (m Model) send(s store.Session, text, ok string) (tea.Model, tea.Cmd) {
	switch {
	case s.TmuxPane == "":
		return m, nil // msgLine already shows noPane
	case !m.alive(s.ID):
		return m.say(cRed, s.Name+" 已经不在了，没有发送"), nil
	}
	return m, m.ccmux(ok, "send", s.ID, "--", text) // ccmux reads text starting with - as options
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
	// ponytail: re-lays out the board per click to find the card; fine for tens of sessions.
	_, rects, off := view(m.src.Sessions(), m.w, m.h, m.focus, m.frame, m.now(), "", false, m.folded)
	y := e.Y - 1 + off // row 0 is the top bar
	for _, r := range rects {
		if e.X >= r.x && e.X < r.x+r.w && y >= r.y && y < r.y+r.h {
			if r.id != m.focus {
				m.focus, m.msg = r.id, ""
				return m, nil
			}
			return m.key(tea.KeyMsg{Type: tea.KeyEnter}) // second click = enter: jump, or unfold a title
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
		parts = append(parts, fg(cRed, " "+firstLine(err.Error()))) // or the hint after it is cut off
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
	_, _, focused, ok := m.current()
	out, _, _ := view(m.src.Sessions(), m.w, m.h, m.focus, m.frame, m.now(), m.msgLine(focused), ok && focused.TmuxPane == "", m.folded)
	return out
}
