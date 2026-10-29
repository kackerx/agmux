// Package board is the full-screen session board (`agmux board`).
package board

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
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
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/execx"
	"github.com/kackerx/agmux/internal/fuzzy"
	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/recall"
	"github.com/kackerx/agmux/internal/store"
)

// Source is what the board reads: the store plus its last poll error; and where avatars are bound.
type Source interface {
	store.Store
	Err() error
	SetAvatar(s store.Session, id string) error
	MarkSeen(s store.Session) error                     // the user saw s's done: it is idle from now on
	TogglePin(s store.Session) (pinned bool, err error) // pin s to the top of its group, or unpin it; which it did
}

// Runner does what the board does outside itself: tmux, and r / x / d on a session (control.go);
// tests swap in one that records them.
type Runner interface {
	Restart(s store.Session) error             // r: ends the agent and starts it again where it was; blocks while it exits
	Kill(s store.Session) error                // x
	Review(s store.Session) (*exec.Cmd, error) // d: the command that shows the diff, given the terminal via tea.ExecProcess
	Tmux(args ...string) (string, error)       // tmux from $PATH, reaching the server through the inherited $TMUX
	TmuxIn(stdin string, args ...string) error // tmux with stdin (load-buffer)
}

type realRunner struct{}

func (r realRunner) Restart(s store.Session) error { return realControl(r.Tmux).restart(s) }
func (r realRunner) Kill(s store.Session) error    { return realControl(r.Tmux).kill(s) }
func (r realRunner) Review(s store.Session) (*exec.Cmd, error) {
	return realControl(r.Tmux).review(s)
}

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
	flashing   // s: type to find text on the board, press a hit's label to put the focus there
)

type Model struct {
	src     Source
	run     Runner
	now     func() time.Time
	getenv  func(string) string // AGMUX_CLIENT, TMUX_PANE
	w, h    int
	frame   int
	focus   string // session ID; follows the session across polls
	msg     string // one line above the footer: what a command said, errors, hints
	mode    mode
	target  store.Session   // what the open x prompt / avatar list acts on, fixed when it opened
	pick    int             // the avatar grid's cursor, an index into avatar.Listing()
	folded  map[string]bool // group keys folded with f; this run only; cleared when the grouping changes
	groupBy string          // how the cards are grouped now (g changes it); "" = [layout] group_by
	stay    bool            // --stay: jump never ends the board, quit only hides a popup (see hideBoard)

	query      string // the / filter; "" = every card
	qpos       int    // the caret in it, in runes (editQuery)
	searching  bool   // the filter's input box has the keys
	searchFrom string // the focus when / was pressed: esc puts it back

	flash struct {
		pat   string          // what was typed
		ms    []flashMatch    // its hits as the last read of the board left them
		freed map[string]bool // labels of hits whose cards have gone since: pressing one is ignored, no new hit gets one
	}

	preview   bool       // p: show the focused pane on the right
	pv        previewMsg // the latest capture
	capturing bool       // a capture is in flight; don't stack them
	pvFocus   bool       // keys go to the locked pane (see passThrough)
	pvID      string     // the session pass-through was entered on
	pvPane    string     // and its pane then; keys never go anywhere else
	scroll    int        // lines the preview is scrolled back with the wheel
	pvLines   int        // lines in the last capture, the most scroll can show
	keys      *serial    // forwarded keys reach the pane in the order they were typed
	scrolled  bool       // the cards were scrolled by the wheel or page keys, not by the focus: top is the offset
	top       int        // lines scrolled off the top of the cards while scrolled; the focus leaving the view ends it

	attached *attachedSrc // the panes opened for background sessions; src with them laid over it
	curOut   *cursorOut   // where the terminal cursor is put (nil: nowhere, as in the tests)
	curOn    bool         // the terminal cursor is shown

	cfg    *config.Config
	keymap map[string]string // key (as KeyMsg.String() spells it) → action

	rc       *recallModel // the recall screen, while it is on (T77); nil: the board
	resuming bool         // a window is being made for a session resumed from it (kept here: the screen may be left meanwhile)
	rx       *recallIndex // what the recall screen is made of and what it asked for; shared by the copies of the Model
}

func New(src Source, run Runner) Model {
	cfg := config.Default()
	a := &attachedSrc{Source: src}
	return Model{src: a, attached: a, run: run, now: time.Now, getenv: os.Getenv, folded: map[string]bool{}, keys: newSerial(),
		cfg: cfg, keymap: cfg.KeyMap(), rx: &recallIndex{open: recall.Default}}
}

// Run polls p in the background and runs the board until the user quits. img is DetectImages's
// answer: when it says pictures, they are uploaded first.
func Run(p *store.Poller, cfg *config.Config, img ImageMode, stay bool) (*RecallResult, error) {
	var stayNote string
	if stay && os.Getenv("TMUX") == "" {
		stay, stayNote = false, "--stay only works inside tmux"
	}
	stayFooter = false // true once a popup is seen on the board's session
	useConfig(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	m := New(p, realRunner{})
	m.cfg, m.keymap, m.stay = cfg, cfg.KeyMap(), stay
	if img.On { // before the screen is taken over: bubbletea must not interleave with the upload
		note, err := uploadImages(img, realRunner{})
		if err != nil {
			return nil, err
		}
		img.Note = note
		useImages = true
	}
	lines := slices.Clone(avatar.Problems())
	if img.Note != "" {
		lines = append(lines, img.Note)
	}
	if stayNote != "" {
		lines = append(lines, stayNote)
	}
	if len(lines) > 0 {
		m = m.say(cRed, strings.Join(lines, "; "))
	}
	m.curOut = &cursorOut{File: os.Stdout}
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithOutput(m.curOut)).Run()
	if err != nil && img.On && os.Getenv("TMUX") == "" { // the upload switched screens: bubbletea may not have switched back
		io.WriteString(os.Stdout, "\x1b[?1049l")
	}
	return RecallOutcome(final), err
}

type frameMsg struct{}

// popupMsg answers whether a popup (a tmux client) is attached to the board's session now (--stay).
type popupMsg struct{ yes bool }

// doneMsg reports a finished action (restart, kill, tmux).
type doneMsg struct {
	ok      string // message on success
	err     error
	quit    bool // leave the board on success (switch)
	retrack bool // came back from ExecProcess: bubbletea v1 leaves the mouse off, turn it back on
}

func nextFrame() tea.Cmd { return tea.Tick(frameEvery, func(time.Time) tea.Msg { return frameMsg{} }) }

func (m Model) Init() tea.Cmd { return tea.Batch(nextFrame(), m.askPopup()) }

// askPopup, under --stay, asks tmux whether q has a popup to hide, by the very rule q uses
// (popupClients of the chosen client): the footer says "q hide" then, "q quit" otherwise. A tmux
// that does not answer leaves the footer as it was.
func (m Model) askPopup() tea.Cmd {
	if !m.stay || m.getenv("TMUX_PANE") == "" {
		return nil
	}
	r, getenv := m.run, m.getenv
	return func() tea.Msg {
		pc, err := chooseClient(r, getenv)
		switch {
		case errors.Is(err, errNoClient): // q ends the board then, as it does without a popup
			return popupMsg{false}
		case err != nil:
			return nil
		}
		return popupMsg{len(popupClients(pc)) > 0}
	}
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.rc != nil {
		if tm, cmd, handled := m.recallUpdate(msg); handled {
			return tm, cmd
		}
	}
	switch msg := msg.(type) {
	case resumeWindowMsg:
		return m.resumed(msg)
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.pvFocus && !m.previewOn() { // not "session is gone": it isn't
			m.pvFocus = false
			m = m.say(cMuted, i18n.T("msg.preview_closed"))
		}
		if m.mode == flashing { // the hits were read off the old size
			m.flashFollow()
		}
	case frameMsg:
		m.frame++
		m.dropLostLock()
		if was := m.focus; was != "" { // a poll took the focused card: the focus falls back to the first, and is in view
			if m.current(); m.focus != was {
				m.followHidden()
			}
		}
		if m.mode == flashing { // polls move cards: the hits follow, as View draws them
			m.flashFollow()
		}
		cmd := m.capture() // the preview refreshes on the 500ms frame; called first: it marks m.capturing
		var popup tea.Cmd
		if m.frame%2 == 0 { // a popup comes and goes; once a second is soon enough for a footer
			popup = m.askPopup()
		}
		return m, tea.Batch(nextFrame(), cmd, popup)
	case popupMsg:
		if stayFooter != msg.yes {
			stayFooter = msg.yes
			footerKeys = footerFor(m.cfg) // the "q hide" / "q quit" text is part of the key line
		}
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
	case attachedMsg:
		if msg.err != nil {
			m.msg = fg(cRed, " "+firstLine(msg.err.Error()))
			return m, nil
		}
		if m.attached != nil {
			m.attached.add(msg.id, msg.pane)
		}
		// the focus may have moved on meanwhile; and a card that still has no pane would ask again
		if _, _, s, ok := m.current(); ok && s.ID == msg.id && s.TmuxPane != "" {
			return m.do(msg.action)
		}
		return m, nil
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
		m.dropLostLock()
		// whatever moved the focus (hjkl, a click, unfolding), the preview follows right away
		before := m.focus
		shown := m // the focus the screen shows: current() puts it on the first card when the focused one is gone, or none was set yet
		shown.current()
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
		// only what the event moved counts, not the fallback shown was made for
		if nm.focus != shown.focus {
			nm.followHidden()
		}
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

// groupMode is how the cards are grouped now: what g chose, else [layout] group_by.
func (m Model) groupMode() string {
	if m.groupBy != "" {
		return m.groupBy
	}
	return m.cfg.GroupBy
}

// shown is what the / filter lets through (every session without one), best match first, and
// where each card matched. The card pass-through is locked on stays, matching or not.
func (m Model) shown() ([]store.Session, map[string]hit) {
	ss := regroup(m.src.Sessions(), m.groupMode())
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

// markSeen records that the user saw s's done, off the UI goroutine: it writes a file.
func (m Model) markSeen(s store.Session) tea.Cmd {
	if !s.Done {
		return nil
	}
	src := m.src
	return func() tea.Msg {
		if err := src.MarkSeen(s); err != nil {
			return doneMsg{err: err}
		}
		return nil
	}
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
		if len(gs) == 0 { // nothing shown (a filter without a hit, or no sessions): the focus is kept for esc to give back
			return gs, 0, s, false
		}
		m.scroll = 0 // the focused card is gone (or there was none): a new card starts at the bottom
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

// dropLostLock ends a pass-through lock whose target (session and pane) is not in the poll any
// more: pvID is forgotten, so that passThrough stays false, however the focus moves or the session
// comes back, until the user takes the keys back (tab / esc) and locks again. Asked on every frame
// and key, where the lock is used.
func (m *Model) dropLostLock() {
	if !m.pvFocus || m.pvID == "" {
		return
	}
	for _, s := range m.src.Sessions() {
		if s.ID == m.pvID && s.TmuxPane == m.pvPane {
			return
		}
	}
	m.pvID = ""
}

// lock enters pass-through on session s.
func (m *Model) lock(s store.Session) {
	m.pvFocus, m.pvID, m.pvPane, m.msg = true, s.ID, s.TmuxPane, ""
	m.searching = false // a click on the preview while typing a filter: the keys go to the pane now
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.pvFocus { // the pass-through sends what came, as it came (see mouseTail)
		var ok bool
		if k, ok = withoutMouseTail(k); !ok {
			return m, nil
		}
	}
	// the preview has the keys: everything but leave_preview's keys (tab) goes to the locked
	// pane, ctrl+c and shift+tab included (Claude Code interrupts and cycles modes with them)
	if m.pvFocus {
		if m.cfg.Is("leave_preview", k.String()) || !m.passThrough() && (k.String() == "esc" || m.cfg.IsCtl("quit", k.String())) {
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
	case flashing: // text narrows the hits, a hit's label moves the focus there and ends it
		switch {
		case k.String() == "esc" || m.cfg.IsCtl("quit", k.String()):
			m.mode = browsing
		case k.Type == tea.KeyBackspace || k.Type == tea.KeyCtrlH:
			if r := []rune(m.flash.pat); len(r) > 0 {
				m.flash.pat = string(r[:len(r)-1])
				m.flashScan()
			}
		case (k.Type == tea.KeyRunes || k.Type == tea.KeySpace) && !k.Alt && !k.Paste && len(k.Runes) == 1 && unicode.IsPrint(k.Runes[0]):
			m.flashFollow() // the board as it is now: what a label is pressed on
			label := string(k.Runes)
			if i := slices.IndexFunc(m.flash.ms, func(x flashMatch) bool { return x.label == label }); i >= 0 {
				m.mode, m.focus, m.msg = browsing, m.flash.ms[i].id, ""
			} else if !m.flash.freed[label] {
				m.flash.pat += label // not a label: more of the pattern
				m.flashScan()
			} // else the label of a hit whose card has gone since it was drawn: ignored, still flashing
		}
		return m, nil
	case pickAvatar: // other keys do nothing while the list is open
		switch m.keymap[k.String()] {
		case "left":
			m.pick = max(m.pick-1, 0)
		case "right":
			m.pick = min(m.pick+1, len(avatar.Listing())-1)
		case "up":
			if m.pick >= gridCols {
				m.pick -= gridCols
			}
		case "down":
			if n := len(avatar.Listing()); m.pick+gridCols < n {
				m.pick += gridCols
			} else if m.pick/gridCols < (n-1)/gridCols { // the row below is shorter: its last one
				m.pick = n - 1
			}
		case "quit", "avatar": // the key that opened the list closes it too
			m.mode = browsing
		case "jump": // off the UI goroutine: it writes a file and may wait for a poll
			m.mode = browsing
			src, s, id := m.src, m.target, avatar.Listing()[m.pick]
			return m, func() tea.Msg {
				ok := i18n.T("msg.wears", s.Name, avatarName(id))
				if s.Name == "" {
					ok = i18n.T("msg.bound_id")
				}
				return doneMsg{ok: ok, err: src.SetAvatar(s, id)}
			}
		}
		return m, nil
	}
	if m.query != "" && m.cfg.Is("clear_filter", k.String()) { // a kept filter goes first (alt+esc, two quick escs, doesn't)
		if ss, _ := m.shown(); len(ss) > 0 {
			m.current() // the focus stays on the card it is on now, not on one the filter had hidden
		} // with no match there is no such card: the focus is where it was
		m.query = ""
		m.focus = m.visibleFocus()
		return m, nil
	}
	return m.do(m.keymap[k.String()])
}

// flashRead reads the flash pattern off the board as it is now (see flashScan in view.go); prev are
// the hits drawn before, whose labels stay where they can.
func (m *Model) flashRead(prev []flashMatch) []flashMatch {
	gs, _, _, _ := m.current()
	rects, off, _, bodyH := m.window()
	return flashScan(gs, rects, m.focus, m.flash.pat, off, bodyH, m.cardsW(), m.now(), prev, m.flash.freed)
}

// flashScan: the pattern changed, so the hits are read anew with new labels, and what was freed is forgotten.
func (m *Model) flashScan() {
	m.flash.freed = nil
	m.flash.ms = m.flashRead(nil)
}

// flashFollow reads the hits again for a board that moved under them (a poll, a resize), keeping
// the labels. The labels of hits whose cards are gone from it are noted as freed: they were drawn,
// so pressing one is not typing, and no new hit is given one until the pattern changes.
func (m *Model) flashFollow() {
	old := m.flash.ms
	freed := maps.Clone(m.flash.freed) // the copy View makes must not write into the model's
	m.flash.freed = freed
	m.flash.ms = m.flashRead(old)
	for _, o := range old {
		if o.label != "" && !slices.ContainsFunc(m.flash.ms, func(n flashMatch) bool { return n.key() == o.key() }) {
			if freed == nil {
				freed = map[string]bool{}
			}
			freed[o.label] = true
		}
	}
	m.flash.freed = freed
}

// visibleFocus is the focus once the filter is gone: a card that f had folded away is not there to
// hold it, so the focus goes to its group's title.
func (m Model) visibleFocus() string {
	for _, s := range regroup(m.src.Sessions(), m.groupMode()) {
		if s.ID == m.focus && m.folded[s.GroupKey] {
			return headerID(s.GroupKey)
		}
	}
	return m.focus
}

// searchKey edits the / filter, which applies as it is typed. enter keeps it and puts the focus
// on the first card it shows; esc / ctrl+c drop it, the focus back where it was.
func (m Model) searchKey(k tea.KeyMsg) Model {
	switch s := k.String(); { // esc and enter first: a config that binds them to the other action does not take them away
	case k.Type == tea.KeyCtrlC || s == "esc":
		m.searching, m.query, m.focus = false, "", m.searchFrom
		m.focus = m.visibleFocus()
	case s == "enter" || m.cfg.IsCtl("jump", s): // onto the best match; with no query or no match, the focus stays
		m.searching = false
		if ss, _ := m.shown(); m.query != "" && len(ss) > 0 {
			m.focus = ss[0].ID
		}
	case m.cfg.IsCtl("quit", s):
		m.searching, m.query, m.focus = false, "", m.searchFrom
		m.focus = m.visibleFocus()
	default:
		m.query, m.qpos, _ = editQuery(m.query, m.qpos, k)
	}
	return m
}

// window lays the board out and gives the cards, the offset in effect, the most it can scroll
// and the body's height.
func (m *Model) window() (rects []rect, off, last, bodyH int) {
	gs, _, _, _ := m.current()
	body, rects := layout(gs, m.cardsW(), m.focus, m.frame, m.now(), m.foldedNow(), nil)
	bodyH = max(m.h-3, 1)
	return rects, scrollOff(rects, m.focus, bodyH, len(body), m.pin()), max(len(body)-bodyH, 0), bodyH
}

// pin is the offset the wheel or the page keys set, -1 when the view follows the focus.
func (m Model) pin() int {
	if m.scrolled {
		return m.top
	}
	return -1
}

// slide scrolls the cards without moving the focus: dir -1 up, 1 down; three lines, or half a body for a page.
func (m Model) slide(dir int, page bool) Model {
	_, off, last, bodyH := m.window()
	n := 3
	if page {
		n = max(bodyH/2, 1)
	}
	m.top, m.scrolled = min(max(off+dir*n, 0), last), true
	return m
}

// followHidden goes back to following the focus once it is not fully in view.
func (m *Model) followHidden() {
	if !m.scrolled {
		return
	}
	rects, off, _, bodyH := m.window()
	for _, r := range rects {
		if r.id == m.focus && (r.y < off || r.y+r.h > off+bodyH) {
			m.scrolled = false
		}
	}
}

// do runs a configured action on the focused card.
func (m Model) do(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "exit":
		return m, tea.Quit
	case "quit":
		if m.stay {
			r, getenv := m.run, m.getenv
			return m, func() tea.Msg { // off the UI goroutine: it asks tmux
				hidden, err := hideBoard(r, getenv)
				return doneMsg{err: err, quit: !hidden && err == nil}
			}
		}
		return m, tea.Quit
	case "preview_up", "preview_down": // what the wheel does over the preview
		if !m.previewOn() {
			return m, nil
		}
		m = m.scrollPreview(action == "preview_up")
		cmd := m.capture()
		return m, cmd
	case "recall":
		return m.openRecall()
	case "search": // even when the filter shows nothing: / edits it
		m.searching, m.searchFrom, m.msg, m.qpos = true, m.focus, "", len([]rune(m.query))
		return m, nil
	case "group", "group_cwd", "group_project", "group_session", "group_state": // the next grouping, or the one named; every group open in it, the focus where it was
		next := strings.TrimPrefix(action, "group_")
		if action == "group" {
			next = config.GroupModes[(slices.Index(config.GroupModes, m.groupMode())+1)%len(config.GroupModes)]
		} else if next == m.groupMode() {
			return m, nil // already there
		}
		if strings.HasPrefix(m.focus, "\x00") { // on a folded title: that group's first card (the title goes with the fold)
			for _, g := range groups(regroup(m.src.Sessions(), m.groupMode())) {
				if headerID(g.key) == m.focus {
					m.focus = g.sessions[0].ID
				}
			}
		}
		m.groupBy, m.folded, m.scrolled, m.msg = next, map[string]bool{}, false, ""
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
		m.current() // from the card the screen shows the focus on, as j / k do
		m.focus = order[(slices.Index(order, m.focus)+step+len(order))%len(order)]
		m.msg = ""
		return m, nil
	}
	gs, gi, s, ok := m.current()
	if len(gs) == 0 {
		return m, nil
	}
	key := gs[gi].key
	if action == "page_down" || action == "page_up" {
		if m.w == 0 { // no layout before the first WindowSizeMsg
			return m, nil
		}
		dir := 1
		if action == "page_up" {
			dir = -1
		}
		return m.slide(dir, true), nil
	}
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
		m.mode, m.msg = flashing, ""
		m.flash.pat, m.flash.ms, m.flash.freed = "", nil, nil // nothing is labelled until a character is typed
		return m, nil
	case "preview":
		if m.w < previewMinTerm && !m.preview { // too narrow to open; an open one still closes
			return m.say(cMuted, i18n.T("msg.preview_narrow")), nil
		}
		m.preview, m.msg = !m.preview, ""
		cmd := m.capture()
		if m.preview && ok { // a preview opened on a done card is a look at it
			cmd = tea.Batch(cmd, m.markSeen(s))
		}
		return m, cmd
	case "fold": // fold / unfold the focused group; the focus stays on it
		if m.query != "" { // every group is open under a filter: folding would show nothing
			return m.say(cMuted, i18n.T("msg.fold_filter")), nil
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
			return m.say(cMuted, i18n.T("msg.pick_card")), nil
		}
		return m, nil
	}
	switch action {
	case "handoff":
		if s.TmuxPane == "" && s.Background { // a window first, as for enter, i and tab
			return m, m.attach(s, action)
		}
		return m.send(s, "/handoff", i18n.T("msg.sent_handoff", s.Name))
	case "focus_preview", "reply": // open the preview if needed and give it the keys; typing is the reply
		if s.TmuxPane == "" {
			if s.Background {
				return m, m.attach(s, action)
			}
			return m, nil // msgLine already shows noPane
		}
		if m.w < previewMinTerm {
			return m.say(cMuted, i18n.T("msg.preview_narrow")), nil
		}
		m.preview = true
		m.lock(s)
		cmd := m.capture()
		return m, tea.Batch(cmd, m.markSeen(s))
	case "kill":
		m.mode, m.target, m.msg = confirmKill, s, ""
		return m, nil
	case "avatar":
		m.mode, m.target, m.msg, m.pick = pickAvatar, s, "", 0
		for i, id := range avatar.Listing() { // on the one it wears: enter right away changes nothing
			if id == s.AvatarID {
				m.pick = i
			}
		}
		return m, nil
	case "mark_read": // what the preview and enter do to a done card, without the jump
		if !s.Done {
			return m, nil
		}
		src := m.src // the message comes with the write, so the footer and the card change in one frame
		return m, func() tea.Msg { return doneMsg{ok: i18n.T("msg.read", s.Name), err: src.MarkSeen(s)} }
	case "pin": // off the UI goroutine: it writes a file
		src := m.src
		return m, func() tea.Msg {
			on, err := src.TogglePin(s) // what is pinned now, not what this key press saw
			verb := "msg.unpinned"
			if on {
				verb = "msg.pinned"
			}
			return doneMsg{ok: i18n.T(verb, s.Name), err: err}
		}
	case "restart":
		m.msg = fg(cMuted, " "+i18n.T("msg.restarting", s.Name))
		return m, m.act(i18n.T("msg.restarted", s.Name), func() error { return m.run.Restart(s) })
	case "review":
		c, err := m.run.Review(s)
		if err != nil {
			return m, func() tea.Msg { return doneMsg{err: err} }
		}
		return m, tea.ExecProcess(m.review(c, s))
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
// can be taken as part of a paste and not submit (a short wait is what other tools do too).
var enterDelay = 150 * time.Millisecond

func noPane() string { return i18n.T("note.no_pane") }

// bgJump and bgReply are the keys bgNote names; useConfig sets them from [keys].
var bgJump, bgReply = "enter", "i"

// bgNote is said of a background session, which gets a pane when it is asked for (T60).
func bgNote() string {
	var keys []string
	for _, k := range []string{bgJump, bgReply} {
		if k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return i18n.T("note.bg_plain")
	}
	return i18n.T("note.bg", strings.Join(keys, i18n.T("list.or")))
}

func (m Model) say(c lipgloss.Color, text string) Model {
	m.msg = fg(c, " "+text)
	return m
}

// review hands the terminal to the command that shows the diff. Its stderr still reaches the
// terminal, and a copy is kept so a failure shows its last word instead of a bare "exit status 1".
func (m Model) review(c *exec.Cmd, s store.Session) (*exec.Cmd, tea.ExecCallback) {
	c.Stdout = os.Stdout // left nil, bubbletea gives it the program's output: cursorOut is not a file, so the child would get a pipe
	var buf bytes.Buffer
	c.Stderr = io.MultiWriter(os.Stderr, &buf)
	return c, func(err error) tea.Msg {
		if err != nil {
			if last := lastLine(buf.String()); last != "" {
				err = errors.New("review: " + last)
			}
		}
		return doneMsg{ok: i18n.T("msg.review_done", s.Name), err: err, retrack: true}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// act runs f off the UI goroutine and reports back with a doneMsg.
func (m Model) act(ok string, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{ok: ok, err: f()} }
}

// jump switches tmux to the session and leaves the board (the popup must close to show it); under
// --stay the board stays, and only a popup closes.
func (m Model) jump(s store.Session) (tea.Model, tea.Cmd) {
	if s.TmuxPane == "" {
		if s.Background {
			return m, m.attach(s, "jump")
		}
		return m, nil // msgLine already shows noPane
	}
	r, getenv, stay, src := m.run, m.getenv, m.stay, m.src
	return m, func() tea.Msg {
		var done doneMsg
		if stay {
			done = doneMsg{err: switchAndHide(r, getenv, s.TmuxPane)} // the board stays, in a popup's session or an ordinary pane
		} else {
			done = doneMsg{err: switchTo(r, getenv, s.TmuxPane), quit: true}
		}
		if s.Done { // after the switch: it may wait for a poll in flight
			src.MarkSeen(s) // an error here has nowhere to show: the board is on its way out (or the popup closed)
		}
		return done
	}
}

// clientsFormat: the termname and the tty are asked here, not by display-message -c, which in
// tmux 3.8 ignores -c without -t and answers for the most recently active client.
const clientsFormat = "#{client_activity}\t#{client_name}\t#{client_session}\t#{client_termname}\t#{client_tty}"

type tmuxClient struct {
	activity      int
	name, session string
	termname, tty string // "" when the line had no such field
}

// pickedClient is chooseClient's answer: the client, whether the launcher named it exactly, agmux's
// own session ("" in an ordinary pane) and every client.
type pickedClient struct {
	name    string
	exact   bool
	own     string
	clients []tmuxClient
}

var errNoClient = errors.New("no tmux client to switch: set AGMUX_CLIENT=<client> (a name from tmux list-clients)")

// chooseClient picks the user's tmux client by the rules in switchTo's comment.
func chooseClient(r Runner, getenv func(string) string) (pickedClient, error) {
	own := ""
	if p := getenv("TMUX_PANE"); p != "" {
		// a stale TMUX_PANE (e.g. left in the server's global environment) just means "no own session"
		if out, err := r.Tmux("display-message", "-p", "-t", p, "#{session_name}"); err == nil {
			own = strings.TrimSuffix(out, "\n") // session names may end in spaces
		}
	}
	out, err := r.Tmux("list-clients", "-F", clientsFormat)
	if err != nil {
		return pickedClient{}, err
	}
	var clients []tmuxClient
	for l := range strings.Lines(out) {
		f := strings.SplitN(strings.TrimRight(l, "\n"), "\t", 5)
		if len(f) >= 3 {
			n, _ := strconv.Atoi(f[0])
			c := tmuxClient{activity: n, name: f[1], session: f[2]}
			if len(f) == 5 {
				c.termname, c.tty = f[3], f[4]
			}
			clients = append(clients, c)
		}
	}
	exact := getenv("AGMUX_CLIENT")
	if exact == "" && own != "" {
		// fl-* floats attach to a long-lived session whose shell started long ago, so the launcher
		// can't reach agmux's process environment; it sets the session's environment instead
		// hidden (-h): a plain session variable would be inherited by panes opened later and then,
		// as their process variable, win with a stale client
		// A launcher that set it plain (no -h) is read too, when no hidden one is there.
		for _, flags := range [][]string{{"-h"}, nil} {
			out, err := r.Tmux(append(append([]string{"show-environment"}, flags...), "-t", own, "AGMUX_CLIENT")...)
			out = strings.TrimSuffix(out, "\n")
			if v, ok := strings.CutPrefix(out, "AGMUX_CLIENT="); err == nil && ok {
				exact = v
				break
			}
			if err == nil && strings.HasPrefix(out, "-AGMUX_CLIENT") { // unset on purpose: the plain one is not asked
				break
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
		return pickedClient{}, errNoClient
	}
	return pickedClient{client, exact != "", own, clients}, nil
}

// switchTo points the user's tmux client at pane. Inside a popup tmux has no current client
// (a bare switch-client moves whichever was active last, and in a nested fl-* float that is the
// float's own client), so the client is chosen explicitly: $AGMUX_CLIENT, else the most recently
// active client not attached to agmux's own session (or, when that leaves none, agmux runs in an
// ordinary pane: the most recent client of its own session). AGMUX_CLIENT comes from agmux's
// environment, else from its tmux session's environment.
func switchTo(r Runner, getenv func(string) string, pane string) error {
	pc, err := chooseClient(r, getenv)
	if err != nil {
		return err
	}
	client, exact, own, clients := pc.name, pc.exact, pc.own, pc.clients
	if _, err := r.Tmux("switch-client", "-c", client, "-t", pane); err != nil {
		return err
	}
	// Close the nested float only when the launcher said exactly which client is the user's: a
	// guessed client might be the float's own. Detach the other clients of agmux's session one by
	// one (-s would also kick the user's client off when the target pane is in that same session).
	if !exact || own == "" {
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

// popupClients are the clients attached to agmux's own session that are a tmux themselves: what
// a popup running `tmux attach` is (the board runs in a session of its own, the popup only shows
// it). A client of that session that is not a tmux is the user's own
// terminal (the board was started in an ordinary pane): never detached.
func popupClients(pc pickedClient) []tmuxClient {
	var out []tmuxClient
	for _, c := range pc.clients {
		if pc.own != "" && c.session == pc.own && nestedTerm(c.termname) && c.name != pc.name {
			out = append(out, c)
		}
	}
	return out
}

// detachAll detaches those clients; hidden says there were some.
func detachAll(r Runner, cs []tmuxClient) (hidden bool, err error) {
	for _, c := range cs {
		if _, err := r.Tmux("detach-client", "-t", c.name); err != nil {
			return false, err
		}
	}
	return len(cs) > 0, nil
}

// hideBoard is quit under --stay: a popup closes, the board keeps running in its session.
// hidden is false when there is no popup client to detach (the board is in an ordinary pane): the
// caller then ends the board as quit always did.
func hideBoard(r Runner, getenv func(string) string) (hidden bool, err error) {
	pc, err := chooseClient(r, getenv)
	if errors.Is(err, errNoClient) {
		return false, nil // no client at all: nothing to hide from
	}
	if err != nil {
		return false, err // tmux can't say who is attached: the board stays, and says why
	}
	return detachAll(r, popupClients(pc))
}

// switchAndHide is jump under --stay: the user's client goes to the pane and a popup, if there is
// one, closes; the board stays either way (nothing is detached when it runs in an ordinary pane).
func switchAndHide(r Runner, getenv func(string) string, pane string) error {
	pc, err := chooseClient(r, getenv)
	if err != nil {
		return err
	}
	popups := popupClients(pc)
	if _, err := r.Tmux("switch-client", "-c", pc.name, "-t", pane); err != nil {
		return err
	}
	_, err = detachAll(r, popups)
	return err
}

func (m Model) send(s store.Session, text, ok string) (tea.Model, tea.Cmd) {
	switch {
	case s.TmuxPane == "":
		return m, nil // msgLine already shows noPane
	case !m.alive(s.ID):
		return m.say(cRed, i18n.T("msg.gone_not_sent", s.Name)), nil
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
		return m.say(cMuted, i18n.T("msg.gone", s.Name)), nil
	}
	return m, m.act(i18n.T("msg.killed", s.Name), func() error { return m.run.Kill(s) })
}

// scrollPreview scrolls the preview three lines back (up) or forward: up to the oldest line
// captured, no further.
func (m Model) scrollPreview(up bool) Model {
	if up {
		m.scroll = min(m.scroll+3, max(m.pvLines-m.previewLinesN(), 0))
	} else {
		m.scroll = max(m.scroll-3, 0)
	}
	return m
}

// mouse: click a card to focus it, click the focused card again to jump to it. Over the preview
// the wheel scrolls it back locally and a click gives it the keys.
func (m Model) mouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.previewOn() && e.X >= m.cardsW() && e.Action == tea.MouseActionPress && m.mode == browsing {
		switch e.Button {
		case tea.MouseButtonWheelUp:
			m = m.scrollPreview(true)
		case tea.MouseButtonWheelDown:
			m = m.scrollPreview(false)
		case tea.MouseButtonLeft: // only the box counts, not the title row
			if _, _, s, ok := m.current(); ok && s.TmuxPane != "" && e.Y >= 2 && e.Y <= 3+m.previewLinesN() {
				m.lock(s)
				return m, m.markSeen(s)
			}
			return m, nil
		default:
			return m, nil
		}
		cmd := m.capture()
		return m, cmd
	}
	if m.mode == browsing && m.w > 0 && e.Action == tea.MouseActionPress && (e.Button == tea.MouseButtonWheelUp || e.Button == tea.MouseButtonWheelDown) {
		dir := 1
		if e.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		return m.slide(dir, false), nil
	}
	if m.mode != browsing || e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft || e.Y < 1 || e.Y > m.h-3 {
		return m, nil
	}
	m.current()
	// ponytail: re-lays out the board per click to find the card; fine for tens of sessions.
	ss, _ := m.shown()
	_, rects, off := view(ss, m.w, m.h, m.focus, m.frame, m.now(), "", false, m.foldedNow(), m.cardsW(), nil, nil, m.pin(), m.groupMode())
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
		return fg(cRed, " "+i18n.T("prompt.kill", m.target.Name, m.cfg.First("confirm")))
	case pickAvatar: // what the cursor is on
		id := avatar.Listing()[m.pick]
		if _, isSprite := avatar.ByID(id); isSprite && !isSet(id) {
			return fg(cFocus, " "+id+"  "+avatarName(id))
		}
		return fg(cFocus, " "+id)
	}
	var parts []string
	if m.msg != "" {
		parts = append(parts, m.msg)
	}
	if err := m.src.Err(); err != nil {
		parts = append(parts, fg(cRed, " "+firstLine(err.Error()))) // or the hint after it is cut off
	}
	if focused.ID != "" && focused.TmuxPane == "" {
		note := noPane()
		if focused.Background {
			note = bgNote()
		}
		parts = append(parts, fg(cMuted, " "+note))
	}
	if h, ok := m.src.(hinter); ok && h.Hint() != "" { // last: it is there for good, and the line cuts from the right
		parts = append(parts, fg(cMuted, " "+h.Hint()))
	}
	return strings.Join(parts, fg(cMuted, " ·"))
}

// typingHint is the footer of the search prompt: the keys that keep the filter and drop it (never
// esc for the one or enter for the other: they are fixed).
func (m Model) typingHint() string {
	return i18n.T("search.keep", m.cfg.FirstCtl("jump", "enter", "esc")) + " · " + i18n.T("search.drop", m.cfg.FirstCtl("quit", "esc", "enter"))
}

func (m Model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.rc != nil {
		view, x, y, ok := m.rc.frame()
		if m.curOut != nil {
			if ok {
				m.curOut.place(x, y)
			} else {
				m.curOut.place(-1, -1)
			}
		}
		return view
	}
	_, _, focused, ok := m.current()
	var right []string
	cx, cy := -1, -1
	if m.previewOn() {
		right, cx, cy = m.pvBox()
	}
	ss, hits := m.shown()
	// the count is read once: the text and the cursor must agree on its width
	total := len(m.src.Sessions())
	if x, y, ok := m.searchCursor(len(ss), total); ok { // typing the filter: the cursor is the caret (the keys are not the pane's)
		cx, cy = x, y
	}
	if m.curOut != nil {
		m.curOut.place(cx, cy)
	}
	out, rects, off := view(ss, m.w, m.h, m.focus, m.frame, m.now(), m.msgLine(focused), ok && focused.TmuxPane == "" && !focused.Background, m.foldedNow(), m.cardsW(), right, hits, m.pin(), m.groupMode())
	if m.searching || m.query != "" { // the top bar is the filter: what it is and how many it shows
		lines := strings.SplitN(out, "\n", 3)
		count := fg(cMuted, m.searchCount(len(ss), total))
		lines[0] = spread(fg(cFocus, " / ")+queryView(m.query, m.qpos, m.searching, m.searchRoom(len(ss), total)), count, m.w, "…")
		if len(ss) == 0 && len(lines) > 2 {
			lines[1] = fg(cMuted, " "+i18n.T("search.no_match"))
		}
		out = strings.Join(lines, "\n")
	}
	if m.mode == flashing && m.h >= 4 { // the board dims; each hit is a match, its label on the cell after it
		lines := strings.Split(out, "\n")
		for i := 1; i < len(lines)-2; i++ {
			lines[i] = fg(cMuted, ansi.Strip(lines[i]))
		}
		follow := m // the cards may have moved since the last frame: draw where they are now
		follow.flashFollow()
		for _, h := range follow.flash.ms { // read off the window as it is: always between the bars
			lines[1+h.y] = put(lines[1+h.y], h.x, matched(h.text))
			if h.label != "" {
				lines[1+h.y] = put(lines[1+h.y], h.x+h.w, flashLabel(h.label))
			}
		}
		lines[len(lines)-2] = fg(cFocus, " "+m.cfg.First("flash")+" ") + m.flash.pat + "▏" // what has been typed
		out = strings.Join(lines, "\n")
	}
	if m.query != "" { // a kept filter: what moves around it (the states below replace the whole line)
		var hints []string // whichever keys there are
		switch n, p := m.cfg.First("next_match"), m.cfg.First("prev_match"); {
		case n != "" && p != "":
			hints = append(hints, i18n.T("search.next_prev", n, p))
		case n != "":
			hints = append(hints, i18n.T("search.next", n))
		case p != "":
			hints = append(hints, i18n.T("search.prev", p))
		}
		if c := m.cfg.First("clear_filter"); c != "" {
			hints = append(hints, i18n.T("search.clear", c))
		}
		hint := strings.Join(hints, " · ")
		last := strings.LastIndexByte(out, '\n') + 1
		out = out[:last] + ansi.Truncate(fg(cFocus, " "+hint)+fg(cMuted, " ·")+footer(ok && focused.TmuxPane == "" && !focused.Background, m.w, m.groupMode()), m.w, "…")
	}
	switch { // the footer is always the last line
	case m.mode == flashing:
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " "+i18n.T("flash.hint")+" · "+m.cfg.FirstCtl("quit", "esc", "enter"))
	case m.searching:
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " "+m.typingHint())
	case m.mode == pickAvatar:
		k := m.cfg.First
		var hint []string // actions the config left with no key aren't offered
		if k("left") != "" && k("down") != "" && k("up") != "" && k("right") != "" {
			hint = append(hint, i18n.T("picker.move", k("left")+"/"+k("down")+"/"+k("up")+"/"+k("right")))
		}
		for _, a := range [][2]string{{"jump", "picker.bind"}, {"quit", "picker.cancel"}} {
			if k(a[0]) != "" {
				hint = append(hint, i18n.T(a[1], k(a[0])))
			}
		}
		x, y, rows := 0, 1, 1 // the grid under the card it is for (above if it doesn't fit)
		for _, r := range rects {
			if r.id == m.target.ID {
				x = min(r.x, max(m.w-gridW, 0))
				y, rows = gridPlace(1+r.y-off, r.h, m.h, gridTotal())
			}
		}
		out = overlayAt(out, avatarGrid(m.target, m.pick, rows), x, y, m.w)
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " "+strings.Join(hint, " · "))
	case m.passThrough():
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cFocus, " "+i18n.T("pass.keys", m.cfg.First("leave_preview")))
	case m.pvFocus: // locked out: the target is gone, keys are being swallowed
		out = out[:strings.LastIndexByte(out, '\n')+1] + fg(cRed, " "+i18n.T("pass.gone", m.cfg.First("leave_preview")))
	}
	return out
}
