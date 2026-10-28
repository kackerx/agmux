package board

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/recall"
	"github.com/kackerx/agmux/internal/store"
)

// RecallResult is what the user chose to do on leaving recall: run Argv in Dir (the terminal is
// theirs again by then).
type RecallResult struct {
	Dir  string
	Argv []string
}

// recallModel is `agmux recall`: a query on top, the sessions that match on the left, what matched
// in the one under the cursor on the right.
type recallModel struct {
	ix         *recall.Index
	cfg        *config.Config
	live       []store.Session // the sessions that run now (one look at them, not polled)
	project    string          // the project of the directory agmux was started in: "this project"
	progress   *recallProgress
	indexing   bool // the index is being built (or brought up to date)
	gen        int  // of the index when results were asked for
	qgen       int  // the number (recallSeq) of the last search asked for: the answers of older ones are dropped
	searching  bool // a search is out: the list is the one of the query before
	building   bool // embedded: the window of the session picked is being made; enter waits (a second one would resume it twice)
	now        func() time.Time
	lookPath   func(string) (string, error)
	statDir    func(string) error
	copyToClip func(string) error
	editor     func() string

	embedded bool        // inside the board: leaving, resuming and jumping are the board's to do (leave, pick)
	leave    bool        // embedded: the user is done, back to the board
	pick     *recallPick // embedded: what enter chose, for the board to act on

	query    string
	results  []recall.Result
	sel, top int
	sources  []string        // those that have a session here
	unticked map[string]bool // the sources the panel has unticked
	scopeCWD bool            // scope: this project, not all history
	panel    bool
	pcur     int      // the row of the panel the cursor is on
	pvScroll int      // lines the preview is scrolled down
	pvCache  *pvCache // the content of the preview of the session under the cursor, until it or the query changes
	w, h     int
	msg      string
	res      *RecallResult
}

// recallPick is what enter chose in a recall screen inside the board: a session that runs (to jump
// to) or one that is closed and can be resumed (the command, where, and what to call its window).
type recallPick struct {
	live   *store.Session
	resume *RecallResult
	name   string
}

type recallProgress struct{ done, total atomic.Int64 }

type pvCache struct {
	key   string
	lines []string
}

type recallTickMsg struct{}

// recallSeq numbers the searches of every screen, so the answer to one of a screen that was left is
// not taken for the answer of the one opened after it.
var recallSeq atomic.Int64

// recallFoundMsg is what a search found; qgen is the number of the query it was for.
type recallFoundMsg struct {
	qgen    int
	keep    bool
	results []recall.Result
}
type recallIndexedMsg struct{ err error }
type recallEditedMsg struct{ err error }

// NewRecall is the recall screen over ix. live are the sessions that run now (they get a dot and
// are not resumed), project the project of the current directory. The index is brought up to date
// when the program starts (Init), and the screen shows what is there as it is built.
func NewRecall(ix *recall.Index, cfg *config.Config, live []store.Session, project, query string) tea.Model {
	return newRecallModel(ix, cfg, live, project, query)
}

func newRecallModel(ix *recall.Index, cfg *config.Config, live []store.Session, project, query string) recallModel {
	m := recallModel{ix: ix, cfg: cfg, live: live, project: project, query: query, progress: &recallProgress{}, pvCache: &pvCache{}, indexing: true,
		now: time.Now, lookPath: exec.LookPath, copyToClip: copyToClipboard, unticked: map[string]bool{},
		statDir: func(p string) error { _, err := os.Stat(p); return err },
		editor: func() string {
			if e := os.Getenv("EDITOR"); e != "" {
				return e
			}
			return "vi"
		}}
	ix.Progress = func(done, total int) { m.progress.done.Store(int64(done)); m.progress.total.Store(int64(total)) }
	return m
}

// RecallOutcome is what the user chose in a recall screen that has ended: nil if they just left.
func RecallOutcome(m tea.Model) *RecallResult {
	switch r := m.(type) {
	case recallModel:
		return r.res
	case Model: // the board: what a recall screen inside it chose to resume in place
		return r.rx.res
	}
	return nil
}

func (m recallModel) Init() tea.Cmd {
	ix := m.ix
	return tea.Batch(func() tea.Msg { return recallIndexedMsg{ix.Update()} }, recallTick())
}

// RunRecall runs the recall screen until the user leaves it; what they chose to resume, if anything,
// is returned for the caller to run once the terminal is theirs again.
func RunRecall(ix *recall.Index, cfg *config.Config, live []store.Session, project, query string) (*RecallResult, error) {
	useConfig(cfg)
	m, err := tea.NewProgram(NewRecall(ix, cfg, live, project, query), tea.WithAltScreen()).Run()
	return RecallOutcome(m), err
}

func recallTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return recallTickMsg{} })
}

func (m recallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case recallTickMsg: // what the index has so far, while it is built
		var cmd tea.Cmd
		if m.ix.Gen() != m.gen && !m.searching { // not over one that is out: a slow search would never be shown
			m, cmd = m.search(true)
		}
		if m.indexing {
			cmd = tea.Batch(cmd, recallTick())
		}
		return m, cmd
	case recallIndexedMsg:
		m.indexing = false
		m, cmd := m.search(true)
		if msg.err != nil {
			m.msg = fg(cRed, " "+i18n.T("recall.index_error", firstLine(msg.err.Error())))
		}
		return m, cmd
	case recallFoundMsg:
		if msg.qgen == m.qgen { // an answer to a query that was typed over is not shown
			m = m.show(msg.results, msg.keep)
		}
	case recallEditedMsg:
		if msg.err != nil {
			m.msg = fg(cRed, " "+firstLine(msg.err.Error()))
		}
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

// filter is what the panel lets through.
func (m recallModel) filter() recall.Filter {
	var f recall.Filter
	if len(m.unticked) > 0 {
		f.Sources = map[string]bool{}
		for _, s := range m.sources {
			f.Sources[s] = !m.unticked[s]
		}
	}
	if m.scopeCWD {
		f.Project = m.project
	}
	return f
}

// search asks for the results again, off the update loop (found comes back as a recallFoundMsg);
// keep: the cursor then stays on the session it is on, if it is still there.
func (m recallModel) search(keep bool) (recallModel, tea.Cmd) {
	m.gen = m.ix.Gen()
	m.sources = m.sources[:0:0]
	for _, e := range m.ix.Entries() {
		if !slices.Contains(m.sources, e.Source) {
			m.sources = append(m.sources, e.Source)
		}
	}
	slices.Sort(m.sources)
	m.qgen = int(recallSeq.Add(1))
	m.searching = true
	ix, query, f, qgen := m.ix, m.query, m.filter(), m.qgen
	return m, func() tea.Msg { return recallFoundMsg{qgen, keep, ix.SearchIn(query, f)} }
}

// show puts the results found where the cursor can be on them.
func (m recallModel) show(results []recall.Result, keep bool) recallModel {
	var path string
	if r, ok := m.selected(); ok {
		path = r.Path
	}
	was := m.pvScroll
	m.results, m.searching = results, false
	m.sel = 0
	if i := slices.IndexFunc(m.results, func(r recall.Result) bool { return r.Path == path }); keep && path != "" && i >= 0 {
		m.sel = i
		m.pvScroll = was // the same session: its preview stays where the user scrolled it
	} else {
		m.pvScroll = 0
	}
	return m.visible()
}

func (m recallModel) selected() (recall.Result, bool) {
	if m.sel < 0 || m.sel >= len(m.results) {
		return recall.Result{}, false
	}
	return m.results[m.sel], true
}

// visible scrolls the list so the cursor is in it.
func (m recallModel) visible() recallModel {
	rows := m.listRows()
	m.sel = min(max(m.sel, 0), max(len(m.results)-1, 0))
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+rows {
		m.top = m.sel - rows + 1
	}
	m.top = min(max(m.top, 0), max(len(m.results)-rows, 0))
	return m
}

func (m recallModel) listRows() int { return max(m.h-3, 1) }

func (m recallModel) previewOn() bool { return m.w >= previewMinTerm }

// listW is the columns the list gets.
func (m recallModel) listW() int {
	if m.previewOn() {
		return m.w - previewW(m.w)
	}
	return m.w
}

func (m recallModel) move(d int) recallModel {
	m.sel += d
	m.pvScroll = 0
	return m.visible()
}

// esc: the panel closes, then the query clears, then the screen ends.
func (m recallModel) back() (tea.Model, tea.Cmd) {
	switch {
	case m.panel:
		m.panel = false
	case m.query != "":
		m.query = ""
		return m.search(false)
	case m.embedded:
		m.leave = true
	default:
		return m, tea.Quit
	}
	return m, nil
}

func (m recallModel) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if k.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	m.msg = ""
	if s == "esc" || m.cfg.IsCtl("quit", s) {
		return m.back()
	}
	// the key that opened this screen from the board closes it: a character only when nothing is typed
	if m.embedded && (m.cfg.IsCtl("recall", s) || m.query == "" && !m.panel && m.cfg.Is("recall", s)) {
		m.leave = true
		return m, nil
	}
	if m.panel {
		return m.panelKey(s)
	}
	// the list on the screen is not the query's yet: not the sessions to act on; and a window is
	// being made for the one picked: not a second one for it
	resume := s == "enter" || m.cfg.IsCtl("jump", s)
	if m.building && resume || m.searching && (resume || m.cfg.IsCtl("recall_copy", s) || m.cfg.IsCtl("recall_open", s)) {
		return m, nil
	}
	switch {
	case s == "enter" || m.cfg.IsCtl("jump", s):
		return m.enter()
	case m.cfg.IsCtl("down", s):
		return m.move(1), nil
	case m.cfg.IsCtl("up", s):
		return m.move(-1), nil
	case m.cfg.IsCtl("page_down", s):
		return m.move(m.listRows() / 2), nil
	case m.cfg.IsCtl("page_up", s):
		return m.move(-m.listRows() / 2), nil
	case m.cfg.IsCtl("preview_down", s):
		if r, ok := m.selected(); ok && m.previewOn() {
			m.pvScroll = min(m.pvScroll+3, m.previewScrollMax(r))
		}
		return m, nil
	case m.cfg.IsCtl("preview_up", s):
		m.pvScroll = max(m.pvScroll-3, 0)
		return m, nil
	case m.cfg.IsCtl("recall_filter", s):
		m.panel, m.pcur = true, 0
		return m, nil
	case m.cfg.IsCtl("recall_scope", s):
		m.scopeCWD = !m.scopeCWD
		return m.search(true)
	case m.cfg.IsCtl("recall_copy", s):
		return m.copy(), nil
	case m.cfg.IsCtl("recall_open", s):
		return m.open()
	case k.Type == tea.KeyBackspace || k.Type == tea.KeyCtrlH:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			return m.search(false)
		}
	case (k.Type == tea.KeyRunes || k.Type == tea.KeySpace) && !k.Alt:
		// a paste may carry \r, \t, ESC sequences: they'd tear the top bar
		m.query += strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return ' '
			}
			return r
		}, string(k.Runes))
		return m.search(false)
	}
	return m, nil
}

// panelRows is what the panel holds: a row for each source, then the two scopes.
func (m recallModel) panelRows() int { return len(m.sources) + 2 }

func (m recallModel) panelKey(s string) (tea.Model, tea.Cmd) {
	c := m.cfg
	switch {
	case c.Is("down", s):
		m.pcur = min(m.pcur+1, m.panelRows()-1)
	case c.Is("up", s):
		m.pcur = max(m.pcur-1, 0)
	case c.IsCtl("recall_filter", s):
		m.panel = false
	case c.IsCtl("recall_scope", s):
		m.scopeCWD = !m.scopeCWD
		return m.search(true)
	case c.Is("recall_toggle", s) || s == "enter" || c.IsCtl("jump", s):
		if m.pcur < len(m.sources) {
			src := m.sources[m.pcur]
			if m.unticked[src] {
				delete(m.unticked, src)
			} else {
				m.unticked = cloneSet(m.unticked)
				m.unticked[src] = true
			}
		} else {
			m.scopeCWD = m.pcur == len(m.sources)+1
		}
		return m.search(true)
	case c.Is("recall_all", s):
		m.unticked = map[string]bool{}
		return m.search(true)
	case c.Is("recall_none", s):
		m.unticked = map[string]bool{}
		for _, src := range m.sources {
			m.unticked[src] = true
		}
		return m.search(true)
	}
	return m, nil
}

func cloneSet(s map[string]bool) map[string]bool {
	out := make(map[string]bool, len(s)+1)
	for k, v := range s {
		out[k] = v
	}
	return out
}

// liveOf is the running session that is r, if it is one.
func (m recallModel) liveOf(e *recall.Entry) (store.Session, bool) {
	for _, s := range m.live {
		if e.ID != "" && s.SessionID == e.ID || s.Transcript != "" && s.Transcript == e.Path {
			return s, true
		}
	}
	return store.Session{}, false
}

// enter resumes the session in the terminal that is here: the screen ends, and the caller runs the
// command in the session's directory. A session that runs is not resumed; a directory that is gone
// has nowhere to resume it.
func (m recallModel) enter() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	if s, ok := m.liveOf(r.Entry); ok {
		if m.embedded {
			m.pick = &recallPick{live: &s, name: r.Title()}
			return m, nil
		}
		if s.TmuxPane == "" {
			m.msg = fg(cMuted, " "+i18n.T("recall.running_nopane", r.Title()))
		} else {
			m.msg = fg(cMuted, " "+i18n.T("recall.running", r.Title(), s.TmuxPane))
		}
		return m, nil
	}
	argv := m.ix.Resume(r.Entry)
	if len(argv) == 0 {
		return m, nil
	}
	if r.CWD == "" || m.statDir(r.CWD) != nil {
		m.msg = fg(cRed, " "+i18n.T("recall.gone", tildePath(r.CWD)))
		return m, nil
	}
	if _, err := m.lookPath(argv[0]); err != nil {
		m.msg = fg(cRed, " "+i18n.T("recall.no_cmd", argv[0]))
		return m, nil
	}
	m.res = &RecallResult{Dir: r.CWD, Argv: argv}
	if m.embedded {
		m.pick = &recallPick{resume: m.res, name: r.Title()}
		return m, nil
	}
	return m, tea.Quit
}

// resumeLine is the command that gets back into the session from anywhere.
func (m recallModel) resumeLine(r recall.Result) string {
	words := make([]string, len(m.ix.Resume(r.Entry)))
	for i, a := range m.ix.Resume(r.Entry) {
		words[i] = shellQuote(a)
	}
	return "cd '" + strings.ReplaceAll(r.CWD, "'", `'\''`) + "' && " + strings.Join(words, " ")
}

func (m recallModel) copy() recallModel {
	r, ok := m.selected()
	if !ok {
		return m
	}
	line := m.resumeLine(r)
	switch err := m.copyToClip(line); {
	case errors.Is(err, errNoClipboard):
		m.msg = fg(cRed, " "+i18n.T("recall.no_clip"))
	case err != nil:
		m.msg = fg(cRed, " "+i18n.T("recall.copy_failed", firstLine(err.Error())))
	default:
		m.msg = fg(cMuted, " "+i18n.T("recall.copied", line))
	}
	return m
}

// open hands the terminal to $EDITOR on the transcript.
func (m recallModel) open() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m, tea.ExecProcess(editorCmd(m.editor(), r.Path), func(err error) tea.Msg { return recallEditedMsg{err} })
}

// editorCmd is the editor (a command line: it may have arguments) on the file, whatever the file's name has in it.
func editorCmd(editor, path string) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
}

var errNoClipboard = errors.New("no clipboard tool")

// copyToClipboard puts text on the system clipboard with pbcopy, xclip or wl-copy, whichever is there.
func copyToClipboard(text string) error {
	for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v %s", c[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return errNoClipboard
}
