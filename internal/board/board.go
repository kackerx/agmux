// Package board is the full-screen session board (`agmux board`).
package board

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/execx"
	"github.com/kackerx/agmux/internal/fuzzy"
	"github.com/kackerx/agmux/internal/store"
)

// Source is what the board reads: the store plus its last poll error; and where avatars are bound.
type Source interface {
	store.Store
	Err() error
	SetAvatar(s store.Session, id string) error
}

// Runner runs ccmux and tmux; tests swap in one that records them.
type Runner interface {
	Run(args ...string) error                  // ccmux; error carries its stderr
	Interactive(args ...string) *exec.Cmd      // ccmux, given the terminal via tea.ExecProcess
	Tmux(args ...string) (string, error)       // tmux from $PATH, reaching the server through the inherited $TMUX
	TmuxIn(stdin string, args ...string) error // tmux with stdin (load-buffer)
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

func (realRunner) TmuxIn(stdin string, args ...string) error {
	_, err := execx.RunIn([]byte(stdin), "tmux", args...)
	return err
}

const frameEvery = 500 * time.Millisecond

type mode int

const (
	browsing mode = iota
	confirmKill
	pickAvatar // the avatar list is open; it has the keys
	flashing   // s: every card wears a label; pressing one moves the focus there
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
	target store.Session   // what the open x prompt / avatar list acts on, fixed when it opened
	pick   int             // the avatar list's cursor, an index into avatar.Sprites
	folded map[string]bool // group keys folded with f; this run only

	query      string // the / filter; "" = every card
	searching  bool   // the filter's input box has the keys
	searchFrom string // the focus when / was pressed: esc puts it back

	labels map[string]string // flash label → card (or folded title) ID, fixed when s was pressed

	preview   bool       // p: show the focused pane on the right
	pv        previewMsg // the latest capture
	capturing bool       // a capture is in flight; don't stack them
	pvFocus   bool       // keys go to the locked pane (see passThrough)
	pvID      string     // the session pass-through was entered on
	pvPane    string     // and its pane then; keys never go anywhere else
	scroll    int        // lines the preview is scrolled back with the wheel
	pvLines   int        // lines in the last capture, the most scroll can show
	keys      *serial    // forwarded keys reach the pane in the order they were typed

	cfg    *config.Config
	keymap map[string]string // key (as KeyMsg.String() spells it) → action
}

func New(src Source, run Runner) Model {
	cfg := config.Default()
	return Model{src: src, run: run, now: time.Now, getenv: os.Getenv, folded: map[string]bool{}, keys: newSerial(),
		cfg: cfg, keymap: cfg.KeyMap()}
}

// Run polls p in the background and runs the board until the user quits.
func Run(p *store.Poller, cfg *config.Config) error {
	useConfig(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	m := New(p, realRunner{})
	m.cfg, m.keymap = cfg, cfg.KeyMap()
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
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
		if m.pvFocus && !m.previewOn() { // not "session is gone": it isn't
			m.pvFocus = false
			m = m.say(cMuted, "preview closed: terminal too narrow")
		}
	case frameMsg:
		m.frame++
		cmd := m.capture() // the preview refreshes on the 500ms frame; called first: it marks m.capturing
		return m, tea.Batch(nextFrame(), cmd)
	case previewMsg:
		m.pv, m.capturing = msg, false
		if msg.sendErr { // the key didn't reach the pane: say so where errors go
			m.msg = fg(cRed, " "+firstLine(msg.err.Error()))
		} else if msg.err == nil {
			m.pvLines = len(tail(msg.text, 1<<30))
		}
		// the focus moved while this capture was in flight: get the pane it is on now
		if _, _, s, ok := m.current(); ok && s.TmuxPane != "" && s.TmuxPane != msg.pane {
			cmd := m.capture()
			return m, cmd
		}
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
	case tea.KeyMsg, tea.MouseMsg:
		// whatever moved the focus (hjkl, a click, unfolding), the preview follows right away
		before := m.focus
		var tm tea.Model
		var cmd tea.Cmd
		if k, ok := msg.(tea.KeyMsg); ok {
			tm, cmd = m.key(k)
		} else {
			tm, cmd = m.mouse(msg.(tea.MouseMsg))
		}
		nm, ok := tm.(Model)
		if !ok || nm.focus == before {
			return tm, cmd
		}
		nm.scroll = 0 // a new card starts at the bottom
		capture := nm.capture()
		return nm, tea.Batch(cmd, capture)
	}
	return m, nil
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// items lists, per group, what the focus can rest on: the title of a folded group, else its cards.
func (m Model) items(gs []group) [][]string {
	out := make([][]string, len(gs))
	for i, g := range gs {
		if m.foldedNow()[g.key] {
			out[i] = []string{headerID(g.key)}
			continue
		}
		for _, s := range g.sessions {
			out[i] = append(out[i], s.ID)
		}
	}
	return out
}

// foldedNow is the folding in effect: none under a filter (a group with a hit shows its cards),
// what f set otherwise; the filter never changes what f set.
func (m Model) foldedNow() map[string]bool {
	if m.query != "" {
		return nil
	}
	return m.folded
}

// shown is what the / filter lets through (every session without one), best match first, and
// where each card matched. The card pass-through is locked on stays, matching or not.
func (m Model) shown() ([]store.Session, map[string]hit) {
	ss := m.src.Sessions()
	if m.query == "" {
		return ss, nil
	}
	// fed in layout order, so equal scores keep it (the poll's own order is nobody's)
	var laid []store.Session
	for _, g := range groups(ss) {
		laid = append(laid, g.sessions...)
	}
	ss = laid
	items := make([]string, len(ss))
	for i, s := range ss {
		items[i] = s.Name + " " + s.Branch + " " + tildePath(s.CWD)
	}
	var out []store.Session
	hits := map[string]hit{}
	for _, mt := range fuzzy.Filter(m.query, items) {
		s := ss[mt.Index]
		out = append(out, s)
		// split the offsets into the three fields (each joined by one space)
		n, b := len([]rune(s.Name)), len([]rune(s.Branch))
		var h hit
		for _, p := range mt.Pos {
			switch {
			case p < n:
				h.name = append(h.name, p)
			case p > n && p <= n+b:
				h.branch = append(h.branch, p-n-1)
			case p > n+b+1:
				h.dir = append(h.dir, p-n-b-2)
			}
		}
		hits[s.ID] = h
	}
	if m.pvFocus && !slices.ContainsFunc(out, func(s store.Session) bool { return s.ID == m.pvID }) {
		if i := slices.IndexFunc(ss, func(s store.Session) bool { return s.ID == m.pvID }); i >= 0 {
			out = append(out, ss[i])
		}
	}
	return out, hits
}

// current resolves focus against the shown sessions, falling back to the first item.
// s is the focused session; ok is false when there is none (nothing to show, or a folded title).
func (m *Model) current() (gs []group, gi int, s store.Session, ok bool) {
	ss, _ := m.shown()
	gs = groups(ss)
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
		m.scroll = 0 // the focused card is gone (or there was none): a new card starts at the bottom
		if len(gs) == 0 {
			m.focus = ""
			return gs, 0, s, false
		}
		gi, ci = 0, 0
		m.focus = items[0][0]
	}
	if m.foldedNow()[gs[gi].key] {
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

// passThrough: keys go to the pane — the preview is on, the focus is still on the session
// pass-through was entered on (the preview shows whoever has the focus), and that session still
// exists with the same pane. Keys, the footer and the border all ask this one question.
func (m Model) passThrough() bool {
	if !m.pvFocus || !m.previewOn() || m.focus != m.pvID {
		return false
	}
	for _, s := range m.src.Sessions() {
		if s.ID == m.pvID {
			return s.TmuxPane != "" && s.TmuxPane == m.pvPane
		}
	}
	return false
}

// lock enters pass-through on session s.
func (m *Model) lock(s store.Session) {
	m.pvFocus, m.pvID, m.pvPane, m.msg = true, s.ID, s.TmuxPane, ""
	m.searching = false // a click on the preview while typing a filter: the keys go to the pane now
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// the preview has the keys: everything but leave_preview's keys (tab) goes to the locked
	// pane, tab and ctrl+c included (Claude Code completes and interrupts with them)
	if m.pvFocus {
		if m.cfg.Is("leave_preview", k.String()) || !m.passThrough() && k.Type == tea.KeyEsc {
			m.pvFocus = false
			return m, nil
		}
		if !m.passThrough() {
			// the session ended or its pane changed: locked out. Swallow everything until the user
			// takes the keys back, or the rest of what they were typing would run as board commands
			return m, nil
		}
		cmd := m.forward(k)
		return m, cmd
	}
	if m.searching {
		return m.searchKey(k), nil
	}
	if k.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// bubbletea v1 hands keys read in one burst ("ljd") over as one message; outside the
	// text prompt each rune is its own key, and their commands run in typed order.
	if k.Type == tea.KeyRunes && len(k.Runes) > 1 && !k.Paste {
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
		if !m.cfg.Is("confirm", k.String()) {
			return m, nil
		}
		return m.kill(m.target)
	case flashing: // a label moves the focus and ends it, esc just ends it; other keys do nothing
		id, ok := m.labels[k.String()]
		gs, _, _, _ := m.current()
		// a card gone since s (a poll) is no target: the key is ignored like any non-label
		if ok && slices.ContainsFunc(m.items(gs), func(ids []string) bool { return slices.Contains(ids, id) }) {
			m.mode, m.focus, m.msg = browsing, id, ""
		} else if k.Type == tea.KeyEsc {
			m.mode = browsing
		}
		return m, nil
	case pickAvatar: // other keys do nothing while the list is open
		switch m.keymap[k.String()] {
		case "up":
			m.pick = max(m.pick-1, 0)
		case "down":
			m.pick = min(m.pick+1, len(avatar.Sprites)-1)
		case "quit":
			m.mode = browsing
		case "jump": // off the UI goroutine: it writes a file and may wait for a poll
			m.mode = browsing
			src, s, sp := m.src, m.target, avatar.Sprites[m.pick]
			return m, func() tea.Msg {
				ok := s.Name + " wears " + sp.Name
				if s.Name == "" {
					ok = "bound to session id (name missing)"
				}
				return doneMsg{ok: ok, err: src.SetAvatar(s, sp.ID)}
			}
		}
		return m, nil
	}
	if k.String() == "esc" && m.query != "" { // a kept filter goes first (alt+esc, two quick escs, doesn't)
		if ss, _ := m.shown(); len(ss) > 0 {
			m.current() // the focus stays on the card it is on now, not on one the filter had hidden
		} // with no match there is no such card: the focus is where it was
		m.query = ""
		m.focus = m.visibleFocus()
		return m, nil
	}
	return m.do(m.keymap[k.String()])
}

// visibleFocus is the focus once the filter is gone: a card that f had folded away is not there to
// hold it, so the focus goes to its group's title.
func (m Model) visibleFocus() string {
	for _, s := range m.src.Sessions() {
		if s.ID == m.focus && m.folded[s.GroupKey] {
			return headerID(s.GroupKey)
		}
	}
	return m.focus
}

// searchKey edits the / filter, which applies as it is typed. enter keeps it and puts the focus
// on the first card it shows; esc / ctrl+c drop it, the focus back where it was.
func (m Model) searchKey(k tea.KeyMsg) Model {
	switch k.Type {
	case tea.KeyEnter: // onto the best match; with no query or no match, the focus stays
		m.searching = false
		if ss, _ := m.shown(); m.query != "" && len(ss) > 0 {
			m.focus = ss[0].ID
		}
	case tea.KeyEsc, tea.KeyCtrlC:
		m.searching, m.query, m.focus = false, "", m.searchFrom
		m.focus = m.visibleFocus()
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		if k.Alt { // alt+e is a key, not the text "e"
			break
		}
		// a paste may carry \r, \t, ESC sequences: they'd tear the top bar
		m.query += strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) { // Cf: U+202E and friends
				return ' '
			}
			return r
		}, string(k.Runes))
	}
	return m
}

// do runs a configured action on the focused card.
func (m Model) do(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "quit":
		return m, tea.Quit
	case "search": // even when the filter shows nothing: / edits it
		m.searching, m.searchFrom, m.msg = true, m.focus, ""
		return m, nil
	case "next_match", "prev_match": // around the cards a kept filter shows, in layout order
		if m.query == "" {
			return m, nil
		}
		ss, _ := m.shown()
		var order []string
		for _, g := range groups(ss) {
			for _, s := range g.sessions {
				order = append(order, s.ID)
			}
		}
		if len(order) == 0 {
			return m, nil
		}
		step := 1
		if action == "prev_match" {
			step = -1
		}
		switch i := slices.Index(order, m.focus); {
		case i >= 0:
			m.focus = order[(i+step+len(order))%len(order)]
		case step > 0: // on a card the filter hides: the first match, or the last going back
			m.focus = order[0]
		default:
			m.focus = order[len(order)-1]
		}
		m.msg = ""
		return m, nil
	}
	gs, gi, s, ok := m.current()
	if len(gs) == 0 {
		return m, nil
	}
	key := gs[gi].key
	if d, ok := directions[action]; ok {
		if m.w == 0 { // no layout before the first WindowSizeMsg
			return m, nil
		}
		// ponytail: lays the board out again per key press; fine for tens of sessions.
		_, rects := layout(gs, m.cardsW(), m.focus, m.frame, m.now(), m.foldedNow(), nil)
		if id := nearest(rects, m.focus, d); id != "" {
			m.focus, m.msg = id, ""
		}
		return m, nil
	}
	switch action {
	case "flash":
		if m.w == 0 {
			return m, nil
		}
		// only what is on screen: a card whose top border is in the window
		_, rects := layout(gs, m.cardsW(), m.focus, m.frame, m.now(), m.foldedNow(), nil)
		bodyH := max(m.h-3, 1)
		off := scrollOff(rects, m.focus, bodyH)
		rects = slices.DeleteFunc(rects, func(r rect) bool { return r.y < off || r.y >= off+bodyH })
		m.mode, m.labels, m.msg = flashing, flashLabels(rects, m.focus, m.cardsW()), ""
		return m, nil
	case "preview":
		if m.w < previewMinTerm && !m.preview { // too narrow to open; an open one still closes
			return m.say(cMuted, "the preview needs a terminal at least 100 columns wide"), nil
		}
		m.preview, m.msg = !m.preview, ""
		cmd := m.capture()
		return m, cmd
	case "fold": // fold / unfold the focused group; the focus stays on it
		if m.query != "" { // every group is open under a filter: folding would show nothing
			return m.say(cMuted, "clear the filter to fold"), nil
		}
		m.folded[key] = !m.folded[key]
		m.focus, m.msg = m.items(gs)[gi][0], ""
		return m, nil
	case "jump":
		if !ok { // a folded title: unfold it
			m.folded[key] = false
			m.focus, m.msg = m.items(gs)[gi][0], ""
			return m, nil
		}
		return m.jump(s)
	}
	if !ok { // the rest act on a session; a folded title has none
		if action == "avatar" {
			return m.say(cMuted, "fold a group? pick a card first"), nil
		}
		return m, nil
	}
	switch action {
	case "handoff":
		return m.send(s, "/handoff", "sent /handoff → "+s.Name)
	case "focus_preview", "reply": // open the preview if needed and give it the keys; typing is the reply
		if s.TmuxPane == "" {
			return m, nil // msgLine already shows noPane
		}
		if m.w < previewMinTerm {
			return m.say(cMuted, "the preview needs a terminal at least 100 columns wide"), nil
		}
		m.preview = true
		m.lock(s)
		cmd := m.capture()
		return m, cmd
	case "kill":
		m.mode, m.target, m.msg = confirmKill, s, ""
		return m, nil
	case "avatar":
		m.mode, m.target, m.msg, m.pick = pickAvatar, s, "", 0
		for i, sp := range avatar.Sprites { // on the one it wears: enter right away changes nothing
			if sp.ID == s.AvatarID {
				m.pick = i
			}
		}
		return m, nil
	case "restart":
		m.msg = fg(cMuted, " restarting "+s.Name+"…")
		return m, m.ccmux("restarted "+s.Name, "restart", s.ID)
	case "review":
		return m, tea.ExecProcess(m.review(s))
	}
	return m, nil
}

// directions maps the movement actions to a unit step on screen.
var directions = map[string][2]int{"down": {0, 1}, "up": {0, -1}, "right": {1, 0}, "left": {-1, 0}}

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

const noPane = "background session, no tmux pane: can't jump or send"

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
		return doneMsg{ok: "review finished: " + s.Name, err: err, retrack: true}
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
// ordinary pane: the most recent client of its own session). AGMUX_CLIENT comes from agmux's
// environment, else from its tmux session's environment.
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
	if exact == "" && own != "" {
		// fl-* floats attach to a long-lived session whose shell started long ago, so the launcher
		// can't reach agmux's process environment; it sets the session's environment instead
		// hidden (-h): a plain session variable would be inherited by panes opened later and then,
		// as their process variable, win with a stale client
		if out, err := r.Tmux("show-environment", "-h", "-t", own, "AGMUX_CLIENT"); err == nil {
			if v, ok := strings.CutPrefix(strings.TrimSuffix(out, "\n"), "AGMUX_CLIENT="); ok { // "-AGMUX_CLIENT" = unset
				exact = v
			}
		}
	}
	// a named client must be attached and not inside agmux's own session (a launcher reached from
	// another float may pass that float's inner client; tty names get reused): otherwise guess
	valid := false
	for _, c := range clients {
		valid = valid || c.name == exact && c.session != own
	}
	if !valid {
		exact = ""
	}
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
		return errors.New("no tmux client to switch: set AGMUX_CLIENT=<client> (a name from tmux list-clients)")
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
		return m.say(cRed, s.Name+" is gone; not sent"), nil
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
		return m.say(cMuted, s.Name+" is gone"), nil
	}
	return m, m.ccmux("killed "+s.Name, "kill", s.ID)
}

// mouse: click a card to focus it, click the focused card again to jump to it. Over the preview
// the wheel scrolls it back locally and a click gives it the keys.
func (m Model) mouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.previewOn() && e.X >= m.cardsW() && e.Action == tea.MouseActionPress && m.mode == browsing {
		switch e.Button {
		case tea.MouseButtonWheelUp: // up to the oldest line captured, no further
			m.scroll = min(m.scroll+3, max(m.pvLines-m.previewLinesN(), 0))
		case tea.MouseButtonWheelDown:
			m.scroll = max(m.scroll-3, 0)
		case tea.MouseButtonLeft: // only the box counts, not the title row
			if _, _, s, ok := m.current(); ok && s.TmuxPane != "" && e.Y >= 2 && e.Y <= 3+m.previewLinesN() {
				m.lock(s)
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
	ss, _ := m.shown()
	_, rects, off := view(ss, m.w, m.h, m.focus, m.frame, m.now(), "", false, m.foldedNow(), m.cardsW(), nil, nil)
	y := e.Y - 1 + off // row 0 is the top bar
	for _, r := range rects {
		if e.X >= r.x && e.X < r.x+r.w && y >= r.y && y < r.y+r.h {
			m.pvFocus = false
			if r.id != m.focus {
				m.focus, m.msg = r.id, ""
				return m, nil
			}
			return m.do("jump") // second click: jump, or unfold a title
		}
	}
	return m, nil
}

// msgLine is the line above the footer: an open prompt, else the last result,
// poll / avatars errors and the background hint.
func (m Model) msgLine(focused store.Session) string {
	switch m.mode {
	case confirmKill:
		return fg(cRed, " kill "+m.target.Name+"? "+m.cfg.First("confirm")+" to confirm, any other key cancels")
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
		right = previewLines(focused, ok, m.pv, m.w-m.cardsW(), max(m.h-3, 1), m.scroll, m.passThrough())
	}
	ss, hits := m.shown()
	out, rects, off := view(ss, m.w, m.h, m.focus, m.frame, m.now(), m.msgLine(focused), ok && focused.TmuxPane == "", m.foldedNow(), m.cardsW(), right, hits)
	if m.searching || m.query != "" { // the top bar is the filter: what it is and how many it shows
		cursor := ""
		if m.searching {
			cursor = "▏"
		}
		lines := strings.SplitN(out, "\n", 3)
		lines[0] = spread(fg(cFocus, " / ")+m.query+cursor, fg(cMuted, fmt.Sprintf("%d/%d ", len(ss), len(m.src.Sessions()))), m.w)
		if len(ss) == 0 && len(lines) > 2 {
			lines[1] = fg(cMuted, " no match")
		}
		out = strings.Join(lines, "\n")
	}
	if m.mode == flashing { // the board dims, every card shows its label on its top border
		lines := strings.Split(out, "\n")
		for i := 1; i < len(lines)-2; i++ {
			lines[i] = fg(cMuted, ansi.Strip(lines[i]))
		}
		for label, id := range m.labels {
			for _, r := range rects {
				if y := 1 + r.y - off; r.id == id && y >= 1 && y < len(lines)-2 {
					lines[y] = put(lines[y], r.x+1, flashLabel(label))
				}
			}
		}
		out = strings.Join(lines, "\n")
	}
	if m.query != "" { // a kept filter: what moves around it (the states below replace the whole line)
		hint := "esc clear"
		if n, p := m.cfg.First("next_match"), m.cfg.First("prev_match"); n != "" && p != "" {
			hint = n + "/" + p + " next/prev · " + hint
		}
		last := strings.LastIndexByte(out, '\n') + 1
		out = out[:last] + ansi.Truncate(fg(cFocus, " "+hint)+fg(cMuted, " ·")+footer(ok && focused.TmuxPane == "", m.w), m.w, "…")
	}
	switch { // the footer is always the last line
	case m.mode == flashing:
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " jump: press a label · esc cancel")
	case m.searching:
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " enter keep the filter · esc clear it")
	case m.mode == pickAvatar:
		k := m.cfg.First
		var hint []string // actions the config left with no key aren't offered
		if k("down") != "" && k("up") != "" {
			hint = append(hint, k("down")+"/"+k("up")+" move")
		}
		for _, a := range [][2]string{{"jump", " bind"}, {"quit", " cancel"}} {
			if k(a[0]) != "" {
				hint = append(hint, k(a[0])+a[1])
			}
		}
		out = overlay(out, avatarList(m.target, m.pick), m.w)
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " "+strings.Join(hint, " · "))
	case m.passThrough():
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " keys go to the pane · "+m.cfg.First("leave_preview")+" back to the cards")
	case m.pvFocus: // locked out: the target is gone, keys are being swallowed
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cRed, " session is gone · "+m.cfg.First("leave_preview")+" back to the cards")
	}
	return out
}
