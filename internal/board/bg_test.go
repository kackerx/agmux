package board

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/store"
)

// ---- T59: a background session is marked ----

func bgSession(mod func(*store.Session)) store.Session {
	s := store.Session{ID: "b1", Name: "bg-probe", GroupKey: "/x", Status: "idle", Background: true, ContextPct: -1}
	if mod != nil {
		mod(&s)
	}
	return s
}

// bg sits at the right end of the branch line, after the agent if there is one; the branch gives
// way to it; an ordinary card has none.
func TestBackgroundTagOnCard(t *testing.T) {
	line := func(s store.Session, w int) string { return ansi.Strip(line2(s, w, nil)) }
	if got := line(bgSession(func(s *store.Session) { s.Branch = "main" }), 30); got != " main"+strings.Repeat(" ", 30-len(" main")-2)+"bg" && !strings.HasSuffix(got, " bg") {
		t.Errorf("%q", got)
	}
	if got := line(bgSession(func(s *store.Session) { s.Branch, s.Agent = "main", "orchestrator" }), 40); !strings.HasSuffix(got, "orchestrator bg") || ansi.StringWidth(got) != 40 {
		t.Errorf("with an agent: %q (%d wide)", got, ansi.StringWidth(got))
	}
	long := bgSession(func(s *store.Session) { s.Branch = "feature/a-very-long-branch-name-that-cannot-fit" })
	if got := line(long, 20); !strings.HasSuffix(got, " bg") || ansi.StringWidth(got) != 20 {
		t.Errorf("a branch that gives way: %q", got)
	}
	plain := bgSession(func(s *store.Session) { s.Background, s.Branch = false, "main" })
	if got := line(plain, 30); strings.Contains(got, "bg") {
		t.Errorf("an ordinary card: %q", got)
	}
	if !strings.HasSuffix(line2(long, 20, nil), fg(cMuted, "bg")) {
		t.Errorf("bg is muted: %q", line2(long, 20, nil))
	}
}

// The preview title says bg where the pane is (bg · %id once it has one); without a pane the
// message line says what Enter does; an ordinary session without a pane keeps the old message.
func TestBackgroundPreviewAndMessage(t *testing.T) {
	title := func(s store.Session) string {
		return ansi.Strip(previewLines(s, true, previewMsg{}, 100, 12, 0, false)[0])
	}
	if got := title(bgSession(nil)); !strings.Contains(got, "bg-probe · bg · ") || strings.Contains(got, "%") {
		t.Errorf("no pane: %q", got)
	}
	if got := title(bgSession(func(s *store.Session) { s.TmuxPane = "%7" })); !strings.Contains(got, "bg-probe · bg · %7 · ") {
		t.Errorf("with a pane: %q", got)
	}
	if got := title(bgSession(func(s *store.Session) { s.Background, s.TmuxPane = false, "%7" })); strings.Contains(got, "· bg ·") || !strings.Contains(got, "· %7 ·") {
		t.Errorf("ordinary: %q", got)
	}
	m := psized(staticSrc{bgSession(nil)}, 120, 30)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "background session · enter or i attaches it") || strings.Contains(v, "can't jump") {
		t.Errorf("message line:\n%s", v)
	}
	m = psized(staticSrc{bgSession(func(s *store.Session) { s.Background = false })}, 120, 30)
	if v := ansi.Strip(m.View()); !strings.Contains(v, noPane()) || strings.Contains(v, "attaches it") {
		t.Errorf("a session that is not a background one: %s", v)
	}
}

// ---- T60: enter, i and tab give a background session a pane by attaching it in a tmux window ----

func attachRunner(inSession bool) *fakeRunner {
	r := &fakeRunner{tmuxOut: map[string]string{"new-session": "%9\n", "new-window": "%9\n"}, tmuxFail: map[string]error{}}
	if !inSession {
		r.tmuxFail["has-session"] = errors.New("can't find session: agmux-bg")
	}
	return r
}

func attachBoard(r *fakeRunner, extra ...store.Session) Model {
	bg := bgSession(func(s *store.Session) { s.CWD = "/w/x" })
	m := New(staticSrc(append([]store.Session{bg}, extra...)), r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	return tm.(Model)
}

// the session's windows: first one made with new-session, later ones with new-window
func newSession(pane string) []string {
	return []string{"tmux", "new-session", "-s", "agmux-bg", "-x", "87", "-y", "44", "-d", "-n", "bg-probe", "-c", "/w/x", "-P", "-F", "#{pane_id}", "--", "claude", "attach", "b1"}
}

func newWindow() []string {
	return []string{"tmux", "new-window", "-t", "=agmux-bg:", "-d", "-n", "bg-probe", "-c", "/w/x", "-P", "-F", "#{pane_id}", "--", "claude", "attach", "b1"}
}

func callsNamed(r *fakeRunner, names ...string) [][]string {
	var out [][]string
	for _, c := range r.calls {
		if len(c) > 1 && slices.Contains(names, c[1]) {
			out = append(out, c)
		}
	}
	return out
}

// Enter on a background card with no pane opens the window (in a new session, or in the one that is
// there), then does what Enter does with a pane: switches to it.
func TestAttachOnEnter(t *testing.T) {
	for name, c := range map[string]struct {
		inSession bool
		want      []string
	}{"no session yet": {false, newSession("%9")}, "session there": {true, newWindow()}} {
		r := attachRunner(c.inSession)
		m := attachBoard(r)
		press(t, m, "enter")
		if got := callsNamed(r, "new-session", "new-window"); !reflect.DeepEqual(got, [][]string{c.want}) {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
		if got := callsNamed(r, "switch-client"); !reflect.DeepEqual(got, [][]string{{"tmux", "switch-client", "-c", "/dev/c", "-t", "%9"}}) {
			t.Errorf("%s: switched %q", name, got)
		}
		if first := r.calls[0]; first[1] != "has-session" || first[2] != "-t" || first[3] != "=agmux-bg" {
			t.Errorf("%s: asked first %q", name, first)
		}
	}
}

// i and tab take the preview and the keys, on the new pane.
func TestAttachOnReplyAndTab(t *testing.T) {
	for _, k := range []string{"i", "tab"} {
		r := attachRunner(true)
		m, _ := press(t, attachBoard(r), k)
		if !m.preview || !m.passThrough() || m.pvPane != "%9" || m.pvID != "b1" {
			t.Errorf("%s: preview %v, pass-through %v on %q", k, m.preview, m.passThrough(), m.pvPane)
		}
		if got := callsNamed(r, "new-window"); len(got) != 1 {
			t.Errorf("%s: windows opened %q", k, got)
		}
		if got := callsNamed(r, "switch-client"); len(got) != 0 {
			t.Errorf("%s switched: %q", k, got)
		}
	}
}

// p only looks: no window, and the preview says what Enter would do. A session that has a pane, or
// that is not a background one, is not attached.
func TestAttachNotAskedFor(t *testing.T) {
	r := attachRunner(true)
	m, _ := press(t, attachBoard(r), "p")
	if got := callsNamed(r, "new-window", "new-session", "has-session"); len(got) != 0 {
		t.Errorf("p opened a window: %q", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "│ background session · enter or i attaches it") {
		t.Errorf("the preview says nothing:\n%s", v)
	}
	for name, mod := range map[string]func(*store.Session){
		"has a pane":     func(s *store.Session) { s.TmuxPane = "%5" },
		"not background": func(s *store.Session) { s.Background = false },
	} {
		for _, k := range []string{"enter", "i", "tab"} {
			r := attachRunner(true)
			m := New(staticSrc{bgSession(func(s *store.Session) { s.CWD = "/w/x"; mod(s) })}, r)
			m.getenv = func(string) string { return "" }
			tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
			press(t, tm.(Model), k)
			if got := callsNamed(r, "new-window", "new-session"); len(got) != 0 {
				t.Errorf("%s, %s: opened %q", name, k, got)
			}
		}
	}
}

// A window that can't be opened is said, once, in red: nothing is switched to, nothing is locked.
func TestAttachFails(t *testing.T) {
	r := attachRunner(true)
	r.tmuxFail["new-window"] = errors.New("tmux: no space for new window\nsecond line")
	m, _ := press(t, attachBoard(r), "tab")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "attach bg-probe: tmux: no space for new window") || strings.Contains(v, "second line") || m.pvFocus || m.preview {
		t.Errorf("preview %v, locked %v:\n%s", m.preview, m.pvFocus, v)
	}
	r = attachRunner(true)
	r.tmuxOut["new-window"] = "\n" // tmux said nothing
	if m, _ = press(t, attachBoard(r), "enter"); !strings.Contains(ansi.Strip(m.View()), "tmux gave no pane") {
		t.Error("no pane id: not said")
	}
}

// If the focus has moved by the time the window is open, the pane is kept for the card but nothing
// is done to the one that has the focus now.
func TestAttachFocusMoved(t *testing.T) {
	r := attachRunner(true)
	other := store.Session{ID: "z9", Name: "zzz", GroupKey: "/x", TmuxPane: "%2", Status: "idle", ContextPct: -1}
	m := attachBoard(r, other)
	m.focus = "z9"
	tm, _ := m.Update(attachedMsg{id: "b1", pane: "%9", action: "focus_preview"})
	m = tm.(Model)
	if m.preview || m.pvFocus {
		t.Error("tab went to the card that has the focus now")
	}
	for _, s := range m.src.Sessions() {
		if s.ID == "b1" && s.TmuxPane != "%9" {
			t.Errorf("the pane is not on the card: %q", s.TmuxPane)
		}
	}
}

// The pane is laid over the store's answer until the store has it, or ten seconds: then the store's
// word is the only one.
func TestAttachedSrcOverlay(t *testing.T) {
	inner := &mutSrc{ss: []store.Session{bgSession(nil), {ID: "o", Name: "o"}}}
	now := time.Unix(1000, 0)
	a := &attachedSrc{Source: inner, now: func() time.Time { return now }}
	a.add("b1", "%9")
	pane := func() string { return a.Sessions()[0].TmuxPane }
	if pane() != "%9" || a.Sessions()[1].TmuxPane != "" {
		t.Fatalf("laid over: %q", pane())
	}
	if inner.ss[0].TmuxPane != "" || inner.ss[0].TmuxSession != "" {
		t.Error("the store's own slice was written to")
	}
	if a.Sessions()[0].TmuxSession != "agmux-bg" {
		t.Errorf("the session of the pane: %q", a.Sessions()[0].TmuxSession)
	}
	now = now.Add(9 * time.Second)
	if pane() != "%9" {
		t.Error("still there at 9 s")
	}
	inner.ss[0].TmuxPane = "%11" // the store has found it
	if pane() != "%11" {
		t.Errorf("the store's pane wins: %q", pane())
	}
	inner.ss[0].TmuxPane = "" // and the window has closed since: the overlay must not bring it back
	if pane() != "" {
		t.Errorf("a closed window came back: %q", pane())
	}
	a.add("b1", "%12")
	now = now.Add(11 * time.Second)
	if pane() != "" {
		t.Errorf("after 11 s: %q", pane())
	}
}

// The keys that attach are lit on a background card without a pane; handoff is not one of them.
func TestAttachFooterKeys(t *testing.T) {
	trueColor(t)
	lit := func(s store.Session) (jump, handoff bool) {
		v := psized(staticSrc{s}, 240, 30).View()
		return !strings.Contains(v, fg(cOff, "enter jump")), !strings.Contains(v, fg(cOff, "H handoff"))
	}
	if j, _ := lit(bgSession(nil)); !j {
		t.Error("enter is dimmed on a background card")
	}
	if j, _ := lit(bgSession(func(s *store.Session) { s.Background = false })); j {
		t.Error("enter is lit on a card with no pane that is not a background one")
	}
}

// H on a background card with no pane opens the window first, then sends /handoff to it, as it does
// to any pane.
func TestAttachOnHandoff(t *testing.T) {
	r := attachRunner(true)
	r.tmuxOut["capture-pane"] = "▐▛███▛█   Claude Code\n"
	press(t, attachBoard(r), "H")
	if got := callsNamed(r, "new-window"); len(got) != 1 {
		t.Errorf("windows opened %q", got)
	}
	if got := callsNamed(r, "send-keys"); !reflect.DeepEqual(got, sent("%9", "/handoff")) {
		t.Errorf("sent %q", got)
	}
	// nothing is typed before the pane has drawn anything
	if all := callsNamed(r, "new-window", "capture-pane", "send-keys"); all[0][1] != "new-window" || all[1][1] != "capture-pane" || all[2][1] != "send-keys" {
		t.Errorf("order: %q", all)
	}
	// it waits, and gives up after drawnWait (the keys go anyway: the window may just be a quiet one)
	drawnWait, drawnPoll = 30*time.Millisecond, 10*time.Millisecond
	defer func() { drawnWait, drawnPoll = 5*time.Second, 100*time.Millisecond }()
	r = attachRunner(true) // capture-pane answers nothing
	start := time.Now()
	press(t, attachBoard(r), "H")
	if n := len(callsNamed(r, "capture-pane")); n < 2 || time.Since(start) < 30*time.Millisecond || len(callsNamed(r, "send-keys")) != 2 {
		t.Errorf("a blank pane: %d polls in %v, %d keys sent", n, time.Since(start), len(callsNamed(r, "send-keys")))
	}
	// the other keys do not wait
	r = attachRunner(true)
	press(t, attachBoard(r), "i")
	if n := len(callsNamed(r, "capture-pane")); n != 0 { // (the preview's own capture is a display-message)
		t.Errorf("i polled the pane %d times", n)
	}
}

// The agent gives way to bg, not the other way round: " bg" stays whole at any width, the agent
// is cut to what is left, and on a card too narrow for both only bg shows.
func TestBackgroundTagKeepsToWidth(t *testing.T) {
	trueColor(t)
	for _, w := range []int{20, 11, 10, 9, 5, 4, 3, 2} {
		s := bgSession(func(s *store.Session) { s.Agent = "orchestrator" })
		raw := line2(s, w, nil)
		got := ansi.Strip(raw)
		if ansi.StringWidth(got) > w || !strings.HasSuffix(got, "bg") || !strings.HasSuffix(raw, fg(cMuted, "bg")) {
			t.Errorf("w=%d: %q (%d wide)", w, got, ansi.StringWidth(got))
		}
		if w >= 6 && !strings.Contains(got, " bg") {
			t.Errorf("w=%d: no space before bg: %q", w, got)
		}
	}
	if got := ansi.Strip(line2(bgSession(func(s *store.Session) { s.Agent = "ab" }), 20, nil)); !strings.HasSuffix(got, "ab bg") {
		t.Errorf("a short agent is whole: %q", got)
	}
}

// With a filter kept (the line below the cards is the filter's hint plus the footer), the keys
// that attach are lit on a background card without a pane and dim on any other card without one.
func TestAttachFooterKeysUnderFilter(t *testing.T) {
	trueColor(t)
	view := func(s store.Session) string {
		m := psized(staticSrc{s}, 240, 30)
		m, _ = press(t, m, "/", "b", "enter")
		if m.query != "b" || m.searching {
			t.Fatalf("filter %q, searching %v", m.query, m.searching)
		}
		return m.View()
	}
	v := view(bgSession(nil))
	if strings.Contains(v, fg(cOff, "enter jump")) || strings.Contains(v, fg(cOff, "tab focus preview")) || strings.Contains(v, fg(cOff, "i reply")) || !strings.Contains(v, "esc clear") {
		t.Errorf("a background card under a filter has its keys dimmed:\n%s", ansi.Strip(v))
	}
	v = view(bgSession(func(s *store.Session) { s.Background = false }))
	if !strings.Contains(v, fg(cOff, "enter jump")) {
		t.Errorf("a card with no pane that is not a background one keeps them dim:\n%s", ansi.Strip(v))
	}
}

// What stops a card that still has no pane after the window opened from asking again, which
// would open windows without end: a model with nowhere to keep the pane takes the message and does
// nothing.
func TestAttachedMsgWithoutPaneDoesNotAskAgain(t *testing.T) {
	r := attachRunner(true)
	m := attachBoard(r)
	m.attached = nil // the pane is not kept, so the card still has none
	tm, cmd := m.Update(attachedMsg{id: "b1", pane: "%9", action: "focus_preview"})
	if cmd != nil || tm.(Model).preview || len(r.calls) != 0 {
		t.Errorf("cmd %v, preview %v, calls %q", cmd != nil, tm.(Model).preview, r.calls)
	}
}

// In the session grouping a background session goes with the foreground sessions in its directory
// (else in its main repo): the tmux session with the most of them, then the latest activity. It is
// the same before and after the attach, which puts its pane in agmux-bg. With none it keeps what it
// had: its pane's session, or (no pane).
func TestBgGroupFollowsForeground(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fg := func(id, cwd, root, tmux string, ago time.Duration) store.Session {
		return store.Session{ID: id, CWD: cwd, MainRepoRoot: root, TmuxSession: tmux, TmuxPane: "%" + id, LastActivity: t0.Add(-ago)}
	}
	bg := func(id, cwd, root, tmux string) store.Session {
		s := store.Session{ID: id, CWD: cwd, MainRepoRoot: root, Background: true, TmuxSession: tmux}
		if tmux != "" {
			s.TmuxPane = "%" + id
		}
		return s
	}
	key := func(ss []store.Session, id string) string {
		for _, s := range regroup(ss, "session") {
			if s.ID == id {
				return s.GroupKey
			}
		}
		t.Fatalf("no %s", id)
		return ""
	}
	none := "\U0010FFFF" + noPaneGroup
	w := "/w/shopcart"
	for _, c := range []struct {
		name string
		ss   []store.Session
		want string
	}{
		{"two foreground, no pane", []store.Session{fg("a", w, "", "blessmuse", 0), fg("b", w, "", "blessmuse", 0), bg("x", w, "", "")}, "blessmuse"},
		{"attached: its pane is in agmux-bg", []store.Session{fg("a", w, "", "blessmuse", 0), bg("x", w, "", "agmux-bg")}, "blessmuse"},
		{"the most foreground sessions", []store.Session{fg("a", w, "", "one", 0), fg("b", w, "", "two", time.Hour), fg("c", w, "", "two", time.Hour), bg("x", w, "", "")}, "two"},
		{"a tie: the latest activity of the session", []store.Session{fg("a", w, "", "one", time.Hour), fg("b", w, "", "two", time.Minute), bg("x", w, "", "")}, "two"},
		{"equal in both: by name", []store.Session{fg("a", w, "", "two", 0), fg("b", w, "", "one", 0), bg("x", w, "", "")}, "one"},
		{"sessions without a pane do not count", []store.Session{{ID: "a", CWD: w}, {ID: "b", CWD: w}, fg("c", w, "", "one", 0), bg("x", w, "", "")}, "one"},
		{"a worktree: the main repo", []store.Session{fg("a", "/w/other", "/w/main", "blessmuse", 0), bg("x", w, "/w/main", "")}, "blessmuse"},
		{"the directory before the main repo", []store.Session{fg("a", w, "/w/main", "one", time.Hour), fg("b", "/w/o1", "/w/main", "two", 0), fg("c", "/w/o2", "/w/main", "two", 0), bg("x", w, "/w/main", "")}, "one"},
		{"no main repo is no match", []store.Session{fg("a", "/w/other", "", "one", 0), bg("x", w, "", "")}, none},
		{"another directory, no pane", []store.Session{fg("a", "/w/other", "/w/o", "one", 0), bg("x", w, "/w/main", "")}, none},
		{"another directory, a pane", []store.Session{fg("a", "/w/other", "", "one", 0), bg("x", w, "", "agmux-bg")}, "agmux-bg"},
		{"a foreground session without a pane is no home", []store.Session{{ID: "a", CWD: w}, bg("x", w, "", "")}, none},
		{"another background session is no home", []store.Session{bg("y", w, "", "agmux-bg"), bg("x", w, "", "")}, none},
	} {
		if got := key(c.ss, "x"); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	ss := []store.Session{fg("a", w, "", "blessmuse", 0), bg("x", w, "", "")}
	if got := key(ss, "a"); got != "blessmuse" {
		t.Errorf("a foreground session: %q", got)
	}
	for _, mode := range []string{"cwd", "project", "state"} { // only the session grouping moves it
		for i, s := range regroup(ss, mode) {
			if want := regroup([]store.Session{ss[i]}, mode)[0].GroupKey; s.GroupKey != want {
				t.Errorf("%s: %s is in %q alone and %q with the others", mode, s.ID, want, s.GroupKey)
			}
		}
	}
}
