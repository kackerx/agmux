// Package board is the full-screen session board (`agmux board`).
package board

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/execx"
	"github.com/kackerx/agmux/internal/store"
)

// Source is what the board reads: the store plus its last poll error.
type Source interface {
	store.Store
	Err() error
}

// Runner runs ccmux and tmux; tests swap in one that records them.
type Runner interface {
	Run(args ...string) error             // ccmux; error carries its stderr
	Interactive(args ...string) *exec.Cmd // ccmux, given the terminal via tea.ExecProcess
	Tmux(args ...string) (string, error)  // tmux from $PATH, reaching the server through the inherited $TMUX
}

type realRunner struct{}

func (realRunner) Run(args ...string) error {
	_, err := ccmux.Run(args...)
	return err
}

func (realRunner) Interactive(args ...string) *exec.Cmd { return exec.Command("ccmux", args...) }

func (realRunner) Tmux(args ...string) (string, error) {
	out, err := execx.Run("tmux", args...)
	return string(out), err
}

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
	getenv func(string) string // AGMUX_CLIENT, TMUX_PANE
	w, h   int
	frame  int
	focus  string // session ID; follows the session across polls
	msg    string // one line above the footer: ccmux stderr, hints
	mode   mode
	target store.Session   // what the open x / s prompt acts on, fixed when it opened
	input  []rune          // the `s` line being typed
	folded map[string]bool // group keys folded with f; this run only

	preview   bool       // P: show the focused pane on the right
	pv        previewMsg // the latest capture
	capturing bool       // a capture is in flight; don't stack them
	pvFocus   bool       // tab: keys go to the previewed pane
	scroll    int        // lines the preview is scrolled back with the wheel
	keys      *serial    // forwarded keys reach the pane in the order they were typed
}

func New(src Source, run Runner) Model {
	return Model{src: src, run: run, now: time.Now, getenv: os.Getenv, folded: map[string]bool{}, keys: newSerial()}
}

// Run polls p in the background and runs the board until the user quits.
func Run(p *store.Poller) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	_, err := tea.NewProgram(New(p, realRunner{}), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

type frameMsg struct{}

// doneMsg reports a finished ccmux or tmux command.
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
		cmd := m.capture() // the preview refreshes on the 500ms frame; called first: it marks m.capturing
		return m, tea.Batch(nextFrame(), cmd)
	case previewMsg:
		m.pv, m.capturing = msg, false
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
	// the preview has the keys: everything but tab goes to the pane, ctrl+c included
	// (Claude Code interrupts with it); quitting means tab back first
	if m.pvFocus && m.previewOn() {
		if _, _, s, ok := m.current(); ok && s.TmuxPane != "" {
			if k.Type == tea.KeyTab {
				m.pvFocus = false
				return m, nil
			}
			cmd := m.forward(k)
			return m, cmd
		}
	}
	m.pvFocus = false
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
		if m.w == 0 { // no layout before the first WindowSizeMsg
			return m, nil
		}
		// ponytail: lays the board out again per key press; fine for tens of sessions.
		_, rects := layout(gs, m.cardsW(), m.focus, m.frame, m.now(), m.folded)
		if id := nearest(rects, m.focus, d); id != "" {
			m.focus, m.msg = id, ""
		}
		cmd := m.capture() // the preview follows the focus right away
		return m, cmd
	}
	switch k.String() {
	case "tab":
		if m.previewOn() && ok && s.TmuxPane != "" {
			m.pvFocus, m.msg = true, ""
		}
		return m, nil
	case "P":
		if m.w < previewMinTerm {
			return m.say(cMuted, "终端不足 100 列，开不了预览"), nil
		}
		m.preview, m.msg = !m.preview, ""
		cmd := m.capture()
		return m, cmd
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

// nearest finds where the focus moves from rect id in direction d ("" = at the edge, stay).
//   - j/k: among the rects entirely below / above that overlap it horizontally, the closest,
//     ties going to the most overlap; across groups and bands.
//   - h/l: only inside its own band, in the closest column on that side: the rect overlapping it
//     most vertically, or when none does (a folded title sits on the title row; columns differ in
//     height) the vertically closest. No column there in this band: stay.
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
	best, bestKey := "", []int(nil)
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
		switch {
		case r.id == id || gap < 0:
			continue
		case d[0] == 0 && ov <= 0: // j/k: must overlap
			continue
		case d[0] != 0 && r.band != cur.band: // h/l: stay in the band
			continue
		}
		// nearest first; then -ov: overlapping ones (negative, more is smaller) beat the rest,
		// which rank by vertical distance
		if key := []int{gap, -ov}; best == "" || slices.Compare(key, bestKey) < 0 {
			best, bestKey = r.id, key
		}
	}
	return best
}

// enterDelay separates the text from its Enter: an Enter that lands in the same read as the text
// can be taken as part of a paste and not submit (ccmux waits the same).
var enterDelay = 150 * time.Millisecond

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
	r, getenv := m.run, m.getenv
	return m, func() tea.Msg { return doneMsg{err: switchTo(r, getenv, s.TmuxPane), quit: true} }
}

// switchTo points the user's tmux client at pane. Inside a popup tmux has no current client
// (a bare switch-client moves whichever was active last, and in a nested fl-* float that is the
// float's own client), so the client is chosen explicitly: $AGMUX_CLIENT, else the most recently
// active client not attached to agmux's own session (or, when that leaves none, agmux runs in an
// ordinary pane: the most recent client of its own session).
const clientsFormat = "#{client_activity}\t#{client_name}\t#{client_session}"

func switchTo(r Runner, getenv func(string) string, pane string) error {
	own := ""
	if p := getenv("TMUX_PANE"); p != "" {
		// a stale TMUX_PANE (e.g. left in the server's global environment) just means "no own session"
		if out, err := r.Tmux("display-message", "-p", "-t", p, "#{session_name}"); err == nil {
			own = strings.TrimSuffix(out, "\n") // session names may end in spaces
		}
	}
	out, err := r.Tmux("list-clients", "-F", clientsFormat)
	if err != nil {
		return err
	}
	type cl struct {
		activity      int
		name, session string
	}
	var clients []cl
	for l := range strings.Lines(out) {
		f := strings.SplitN(strings.TrimRight(l, "\n"), "\t", 3)
		if len(f) == 3 {
			n, _ := strconv.Atoi(f[0])
			clients = append(clients, cl{n, f[1], f[2]})
		}
	}
	exact := getenv("AGMUX_CLIENT")
	client := exact
	if client == "" {
		best := -1
		for _, c := range clients {
			if c.session != own && c.activity > best {
				client, best = c.name, c.activity
			}
		}
	}
	if client == "" { // agmux in an ordinary pane: the user's own client is the one showing it
		best := -1
		for _, c := range clients {
			if c.activity > best {
				client, best = c.name, c.activity
			}
		}
	}
	if client == "" {
		return errors.New("没有可切换的 tmux client：设 AGMUX_CLIENT=<client>（tmux list-clients 里的名字）")
	}
	if _, err := r.Tmux("switch-client", "-c", client, "-t", pane); err != nil {
		return err
	}
	// Close the nested float only when the launcher said exactly which client is the user's: a
	// guessed client might be the float's own. Detach the other clients of agmux's session one by
	// one (-s would also kick the user's client off when the target pane is in that same session).
	if exact == "" || own == "" {
		return nil
	}
	for _, c := range clients {
		if c.session == own && c.name != client {
			if _, err := r.Tmux("detach-client", "-t", c.name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m Model) send(s store.Session, text, ok string) (tea.Model, tea.Cmd) {
	switch {
	case s.TmuxPane == "":
		return m, nil // msgLine already shows noPane
	case !m.alive(s.ID):
		return m.say(cRed, s.Name+" 已经不在了，没有发送"), nil
	}
	r, pane := m.run, s.TmuxPane
	return m, func() tea.Msg {
		_, err := r.Tmux("send-keys", "-t", pane, "-l", "--", text) // -l: literal, -- : text may start with -
		if err == nil {
			time.Sleep(enterDelay)
			_, err = r.Tmux("send-keys", "-t", pane, "Enter")
		}
		return doneMsg{ok: ok, err: err}
	}
}

func (m Model) kill(s store.Session) (tea.Model, tea.Cmd) {
	if !m.alive(s.ID) {
		return m.say(cMuted, s.Name+" 已经不在了"), nil
	}
	return m, m.ccmux("已杀掉 "+s.Name, "kill", s.ID)
}

// mouse: click a card to focus it, click the focused card again to jump to it. Over the preview
// the wheel scrolls it back locally and a click gives it the keys.
func (m Model) mouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.previewOn() && e.X >= m.cardsW() && e.Action == tea.MouseActionPress && m.mode == browsing {
		switch e.Button {
		case tea.MouseButtonWheelUp:
			m.scroll = min(m.scroll+3, maxScroll)
		case tea.MouseButtonWheelDown:
			m.scroll = max(m.scroll-3, 0)
		case tea.MouseButtonLeft:
			if _, _, s, ok := m.current(); ok && s.TmuxPane != "" {
				m.pvFocus = true
			}
			return m, nil
		default:
			return m, nil
		}
		cmd := m.capture()
		return m, cmd
	}
	if m.mode != browsing || e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft || e.Y < 1 || e.Y > m.h-3 {
		return m, nil
	}
	m.current()
	// ponytail: re-lays out the board per click to find the card; fine for tens of sessions.
	_, rects, off := view(m.src.Sessions(), m.w, m.h, m.focus, m.frame, m.now(), "", false, m.folded, m.cardsW(), nil)
	y := e.Y - 1 + off // row 0 is the top bar
	for _, r := range rects {
		if e.X >= r.x && e.X < r.x+r.w && y >= r.y && y < r.y+r.h {
			m.pvFocus = false
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
	var right []string
	if m.previewOn() {
		right = previewLines(focused, ok, m.pv, m.w-m.cardsW(), max(m.h-3, 1), m.scroll, m.pvFocus)
	}
	out, _, _ := view(m.src.Sessions(), m.w, m.h, m.focus, m.frame, m.now(), m.msgLine(focused), ok && focused.TmuxPane == "", m.folded, m.cardsW(), right)
	if m.pvFocus && m.previewOn() { // the footer is always the last line
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " 按键直通 pane · tab 回到卡片")
	}
	return out
}
