package board

import (
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/store"
)

type staticSrc []store.Session

func (s staticSrc) Sessions() []store.Session { return s }
func (s staticSrc) Err() error                { return nil }

type fakeRunner struct {
	calls   [][]string
	err     error
	script  string            // what Interactive runs (sh -c); default: true
	tmuxOut map[string]string // tmux subcommand → stdout; list-clients defaults to one client /dev/c
}

func (f *fakeRunner) Tmux(args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{"tmux"}, args...))
	if out, ok := f.tmuxOut[args[0]]; ok {
		return out, f.err
	}
	if args[0] == "list-clients" {
		return "1\t/dev/c\tmain\n", f.err
	}
	return "", f.err
}

var listClients = []string{"tmux", "list-clients", "-F", clientsFormat}

// switched is what enter on a card with pane runs by default: pick the one client, switch it.
func switched(pane string) [][]string {
	return [][]string{listClients, {"tmux", "switch-client", "-c", "/dev/c", "-t", pane}}
}

func sent(pane, text string) [][]string {
	return [][]string{{"tmux", "send-keys", "-t", pane, "-l", "--", text}, {"tmux", "send-keys", "-t", pane, "Enter"}}
}

func (f *fakeRunner) Run(args ...string) error {
	f.calls = append(f.calls, args)
	return f.err
}

func (f *fakeRunner) Interactive(args ...string) *exec.Cmd {
	f.calls = append(f.calls, append([]string{"(interactive)"}, args...))
	if f.script == "" {
		return exec.Command("true")
	}
	return exec.Command("/bin/sh", "-c", f.script)
}

// two groups: /a has a1 (pane) and a2 (background), /b has b1.
var sessions = staticSrc{
	{ID: "a1", Name: "one", GroupKey: "/a", TmuxPane: "%1", Status: "idle", ContextPct: -1},
	{ID: "a2", Name: "two", GroupKey: "/a", Status: "idle", ContextPct: -1},
	{ID: "b1", Name: "three", GroupKey: "/b", TmuxPane: "%3", Status: "idle", ContextPct: -1},
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// run executes cmd like the runtime would, in order for Batch / Sequence, feeding doneMsgs back.
func run(m *Model, cmd tea.Cmd) (quit bool) {
	if cmd == nil {
		return false
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.QuitMsg:
		return true
	case doneMsg:
		tm, next := m.Update(msg)
		*m = tm.(Model)
		return run(m, next)
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice { // tea.BatchMsg or the unexported sequenceMsg
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok && run(m, c) {
				quit = true
			}
		}
	}
	return quit // anything else (ExecProcess, ticks) is not run here
}

// press feeds keys, runs every resulting command once (not tea.Quit / ticks), and reports whether it quit.
func press(t *testing.T, m Model, keys ...string) (Model, bool) {
	t.Helper()
	quit := false
	for _, k := range keys {
		tm, cmd := m.Update(key(k))
		m = tm.(Model)
		quit = run(&m, cmd) || quit
	}
	return m, quit
}

func newModel(r *fakeRunner) Model {
	m := New(sessions, r)
	m.getenv = func(string) string { return "" } // never the test process's own $TMUX_PANE / $AGMUX_CLIENT
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return tm.(Model)
}

func TestKeys(t *testing.T) {
	cases := []struct {
		name  string
		keys  []string
		calls [][]string
		quit  bool
	}{
		{"enter switches and quits", []string{"enter"}, switched("%1"), true},
		{"l moves to next column", []string{"l", "enter"}, switched("%3"), true},
		{"l then h comes back", []string{"l", "h", "r"}, [][]string{{"restart", "a1"}}, false},
		{"j/k within column", []string{"j", "j", "r", "k", "r"}, [][]string{{"restart", "a2"}, {"restart", "a1"}}, false},
		{"H sends /handoff", []string{"H"}, sent("%1", "/handoff"), false},
		{"d reviews interactively", []string{"d"}, [][]string{{"(interactive)", "review", "a1"}}, false},
		{"x y kills", []string{"x", "y"}, [][]string{{"kill", "a1"}}, false},
		{"x other cancels", []string{"x", "n", "x", "q"}, nil, false},
		{"s types and sends", []string{"s", "hi", " ", "你好", "enter"}, sent("%1", "hi 你好"), false},
		{"s esc cancels", []string{"s", "hi", "esc"}, nil, false},
		{"s empty cancels", []string{"s", " ", "enter"}, nil, false},
		{"q in s mode is text", []string{"s", "q", "enter"}, sent("%1", "q"), false},
		{"background: enter / H / s refused", []string{"j", "enter", "H", "s", "x", "y"}, [][]string{{"kill", "a2"}}, false},
		{"q quits", []string{"q"}, nil, true},
		{"arrow keys move", []string{"down", "r", "up", "right", "r"}, [][]string{{"restart", "a2"}, {"restart", "b1"}}, false},
		{"up comes back", []string{"down", "up", "r"}, [][]string{{"restart", "a1"}}, false},
		{"text starting with - stays text", []string{"s", "--help", "enter"}, sent("%1", "--help"), false},
		{"burst of keys = one by one", []string{"lr"}, [][]string{{"restart", "b1"}}, false},
		{"unfolding focuses the group's first card", []string{"l", "f", "f", "r"}, [][]string{{"restart", "b1"}}, false},
		{"burst keeps typed order", []string{"Hr"}, append(sent("%1", "/handoff"), []string{"restart", "a1"}), false},
		{"ctrl+c quits from the prompt", []string{"s", "hi", "ctrl+c"}, nil, true},
		{"pasted newlines become spaces", []string{"s", "a\nb", "enter"}, sent("%1", "a b"), false},
		{"burst into the prompt", []string{"shi", "enter"}, sent("%1", "hi"), false},
	}
	for _, c := range cases {
		r := &fakeRunner{}
		_, quit := press(t, newModel(r), c.keys...)
		if !reflect.DeepEqual(r.calls, c.calls) || quit != c.quit {
			t.Errorf("%s: calls %q quit %v, want %q quit %v", c.name, r.calls, quit, c.calls, c.quit)
		}
	}
}

func TestErrorsGoToMessageLine(t *testing.T) {
	r := &fakeRunner{err: errors.New("ccmux restart a1: session not found\nmore")}
	m, quit := press(t, newModel(r), "r")
	if quit || !strings.Contains(m.View(), "ccmux restart a1: session not found") || strings.Contains(m.View(), "more") {
		t.Errorf("want the first stderr line in the message line")
	}
	r.err = errors.New("tmux switch-client -c /dev/c -t %1: can't find pane: %1")
	if _, quit := press(t, newModel(r), "enter"); quit {
		t.Error("a failed switch must not quit")
	}
	if m, _ := press(t, newModel(&fakeRunner{}), "j"); !strings.Contains(m.View(), noPane) {
		t.Error("focusing a background session shows the no-pane hint")
	}
}

func TestMouse(t *testing.T) {
	r := &fakeRunner{}
	m := newModel(r)
	_, rects, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false, nil, m.w, nil)
	var b1 rect
	for _, rc := range rects {
		if rc.id == "b1" {
			b1 = rc
		}
	}
	click := tea.MouseMsg{X: b1.x + 3, Y: 1 + b1.y + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	tm, cmd := m.Update(click)
	if m = tm.(Model); m.focus != "b1" || cmd != nil {
		t.Fatalf("first click should focus b1, focus=%q", m.focus)
	}
	_, cmd = m.Update(click)
	if cmd == nil {
		t.Fatal("second click should jump")
	}
	cmd()
	if !reflect.DeepEqual(r.calls, switched("%3")) {
		t.Errorf("calls %q", r.calls)
	}
	tm, _ = m.Update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if tm.(Model).focus != "b1" {
		t.Error("clicking the top bar changes nothing")
	}
}

type mutSrc struct{ ss []store.Session }

func (s *mutSrc) Sessions() []store.Session { return s.ss }
func (s *mutSrc) Err() error                { return nil }

// The x / s prompts act on the session they named, never on whatever focus falls back to.
func TestPromptTargetIsPinned(t *testing.T) {
	for _, keys := range [][]string{{"x", "y"}, {"s", "hi", "enter"}} {
		src := &mutSrc{ss: sessions}
		r := &fakeRunner{}
		m, _ := press(t, newModel(r), "l") // focus b1
		m.src = src
		m, _ = press(t, m, keys[0])
		src.ss = sessions[:2] // b1 disappears; focus would fall back to a1
		m, _ = press(t, m, keys[1:]...)
		if len(r.calls) != 0 || !strings.Contains(m.View(), "three 已经不在了") {
			t.Errorf("%v: calls %q, want none and a 'gone' message", keys, r.calls)
		}
	}
}

func TestPromptSurvivesResults(t *testing.T) {
	r := &fakeRunner{}
	m := newModel(r)
	tm, cmd := m.Update(key("r")) // restart runs async
	m = tm.(Model)
	m, _ = press(t, m, "x")
	tm, _ = m.Update(cmd()) // its result lands while the kill prompt is open
	if !strings.Contains(tm.(Model).View(), "y 确认") {
		t.Error("a finished command must not hide the open prompt")
	}
}

func TestPasteIsNotKeys(t *testing.T) {
	r := &fakeRunner{}
	tm, cmd := newModel(r).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("xyHr"), Paste: true})
	m := tm.(Model)
	run(&m, cmd)
	if len(r.calls) != 0 {
		t.Errorf("a paste outside the prompt must not run commands: %q", r.calls)
	}
}

func TestReview(t *testing.T) {
	enableMouse := fmt.Sprintf("%T", tea.EnableMouseCellMotion())
	for _, c := range []struct {
		script, wantMsg string
	}{
		{"echo progress >&2; echo 'hunk: no changes to review' >&2; exit 1", "ccmux review: hunk: no changes to review"},
		{"exit 2", "exit status 2"},
		{"echo fine >&2", "review 结束：one"},
	} {
		m := newModel(&fakeRunner{script: c.script})
		cmd, done := m.review(sessions[0])
		msg := done(cmd.Run()) // what ExecProcess does once the terminal is back
		tm, next := m.Update(msg)
		if v := ansi.Strip(tm.(Model).View()); !strings.Contains(v, c.wantMsg) {
			t.Errorf("%q: message line missing %q", c.script, c.wantMsg)
		}
		if next == nil || fmt.Sprintf("%T", next()) != enableMouse {
			t.Errorf("%q: coming back from review must turn the mouse back on", c.script)
		}
	}
}

type errSrc struct {
	staticSrc
	err error
}

func (s errSrc) Err() error { return s.err }

func TestPollErrorOnBoard(t *testing.T) {
	m := New(errSrc{sessions, errors.New("ccmux show --json: boom\n  at x\n  at y")}, &fakeRunner{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	v := tm.(Model).View()
	if n := len(strings.Split(v, "\n")); n != 30 || !strings.Contains(v, "ccmux show --json: boom") {
		t.Errorf("poll error: %d lines (want 30), shown=%v", n, strings.Contains(v, "boom"))
	}
}

// A multi-line poll error must not swallow what follows it on the message line.
func TestPollErrorKeepsBackgroundHint(t *testing.T) {
	m := New(errSrc{sessions, errors.New("ccmux show --json: boom\n  at x")}, &fakeRunner{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	m, _ = press(t, tm.(Model), "j") // a2: background
	if v := ansi.Strip(m.View()); !strings.Contains(v, "boom") || !strings.Contains(v, noPane) || strings.Contains(v, "at x") {
		t.Errorf("message line should be the error's first line plus the hint: %q", strings.Split(v, "\n")[28])
	}
}

func TestBackgroundHintOnce(t *testing.T) {
	for _, k := range []string{"enter", "H", "s"} {
		m, _ := press(t, newModel(&fakeRunner{}), "j", k) // a2 is the background session
		if n := strings.Count(ansi.Strip(m.View()), noPane); n != 1 {
			t.Errorf("%s on a background session: hint shown %d times, want 1", k, n)
		}
	}
}

func TestFold(t *testing.T) {
	r := &fakeRunner{}
	m := newModel(r)
	_, before, _ := view(sessions, m.w, m.h, "", 0, now, "", false, nil, m.w, nil)

	m, _ = press(t, m, "f") // fold /a (focus was on a1)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "▸ /a (2)") || strings.Contains(v, " one ") || strings.Contains(v, " two ") || !strings.Contains(v, "▾ /b (1)") {
		t.Fatalf("folded /a: title ▸, its cards gone, /b untouched:\n%s", v)
	}
	if m.focus != headerID("/a") {
		t.Fatalf("focus %q, want the folded title", m.focus)
	}
	_, after, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false, m.folded, m.w, nil)
	for _, b := range before {
		for _, a := range after {
			if a.id == "b1" && b.id == "b1" && a != b {
				t.Errorf("other groups must not move: b1 %+v → %+v", b, a)
			}
		}
	}

	m, _ = press(t, m, "r", "x", "y", "H", "s", "j") // session keys do nothing on a title
	if len(r.calls) != 0 || m.mode != browsing {
		t.Fatalf("keys on a folded title ran %q", r.calls)
	}
	m, _ = press(t, m, "l", "r", "h") // the title is a stop for h/l like a card
	m, _ = press(t, m, "enter", "r")  // enter unfolds; focus lands on its first card
	if !reflect.DeepEqual(r.calls, [][]string{{"restart", "b1"}, {"restart", "a1"}}) {
		t.Errorf("calls %q", r.calls)
	}
	m, _ = press(t, m, "f", "f") // f folds and unfolds
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▾ /a (2)") || !strings.Contains(v, " one ") {
		t.Errorf("f twice should leave /a open:\n%s", v)
	}
	if !strings.Contains(ansi.Strip(m.View()), "f 折叠") {
		t.Error("footer lists f")
	}
}

func TestFoldMouse(t *testing.T) {
	m, _ := press(t, newModel(&fakeRunner{}), "f", "l")                                              // /a folded, focus on b1
	click := tea.MouseMsg{X: 1, Y: 1 + 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft} // /a's title row
	tm, _ := m.Update(click)
	m = tm.(Model)
	if m.focus != headerID("/a") {
		t.Fatalf("first click focuses the title, focus %q", m.focus)
	}
	tm, _ = m.Update(click)
	if m = tm.(Model); m.folded["/a"] || m.focus != "a1" {
		t.Errorf("second click unfolds: folded=%v focus=%q", m.folded["/a"], m.focus)
	}
}

// Spatial hjkl over the laid-out rectangles: 2 columns × 2 bands at 120 columns
// (band 1: /a with 3 cards, /b with 2; band 2: /c with 1, under /a).
func TestSpatialNav(t *testing.T) {
	ss := staticSrc{}
	for _, s := range []struct{ id, group string }{{"a1", "/a"}, {"a2", "/a"}, {"a3", "/a"}, {"b1", "/b"}, {"b2", "/b"}, {"c1", "/c"}} {
		ss = append(ss, store.Session{ID: s.id, Name: s.id, GroupKey: s.group, TmuxPane: "%1", Status: "idle", ContextPct: -1})
	}
	start := func(focus string, keys ...string) string {
		m := New(ss, &fakeRunner{})
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
		m = tm.(Model)
		m.focus = focus
		m, _ = press(t, m, keys...)
		return m.focus
	}
	for _, c := range []struct {
		from string
		keys []string
		want string
	}{
		{"a3", []string{"j"}, "c1"}, // down across bands
		{"c1", []string{"k"}, "a3"}, // and back
		{"c1", []string{"j"}, "c1"}, // bottom edge: stay
		{"a1", []string{"k"}, "a1"}, // top edge
		{"a1", []string{"h"}, "a1"}, // left edge
		{"b2", []string{"h"}, "a2"}, // the left card overlapping most
		{"b1", []string{"h"}, "a1"},
		{"a2", []string{"l"}, "b2"},
		{"a3", []string{"l"}, "b2"},                // nothing level with a3: the closest one over there
		{"b2", []string{"j"}, "b2"},                // nothing below in the right column
		{"a1", []string{"j", "j", "j", "j"}, "c1"}, // walks the whole left column, then stops
		{"a1", []string{"down", "right", "left"}, "a2"},
	} {
		if got := start(c.from, c.keys...); got != c.want {
			t.Errorf("from %s %v: focus %s, want %s", c.from, c.keys, got, c.want)
		}
	}
	// a folded title is a one-row stop: reachable sideways and back
	m := New(ss, &fakeRunner{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = tm.(Model)
	m.focus = "b1"
	m, _ = press(t, m, "f", "h") // fold /b, focus on its title, then left
	if m.focus != "a1" {
		t.Errorf("h from the folded /b title: %s, want a1", m.focus)
	}
	if m, _ = press(t, m, "l"); m.focus != headerID("/b") {
		t.Errorf("l back to the folded title: %q", m.focus)
	}
}

func TestNearestEmpty(t *testing.T) {
	if nearest(nil, "x", directions["j"]) != "" || nearest([]rect{{"a", 0, 0, 10, 5, 0}}, "zz", directions["j"]) != "" {
		t.Error("unknown focus or no rects: stay")
	}
}

// Which client enter switches, and whether agmux's own session is detached afterwards.
func TestSwitchTo(t *testing.T) {
	const pane = "%7"
	for _, c := range []struct {
		name    string
		env     map[string]string
		clients string // list-clients output: activity \t name \t session
		want    [][]string
		err     string
	}{
		{"AGMUX_CLIENT wins over activity", map[string]string{"AGMUX_CLIENT": "/dev/x"},
			"5\t/dev/a\tmain\n9\t/dev/b\tother\n",
			[][]string{listClients, {"tmux", "switch-client", "-c", "/dev/x", "-t", pane}}, ""},
		{"direct popup: most recent client", nil,
			"5\t/dev/a\tmain\n9\t/dev/b\tother\n3\t/dev/c\tmain\n",
			[][]string{listClients, {"tmux", "switch-client", "-c", "/dev/b", "-t", pane}}, ""},
		{"nested fl-* float: skip own session, then detach it", map[string]string{"TMUX_PANE": "%2"},
			"5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n",
			[][]string{
				{"tmux", "display-message", "-p", "-t", "%2", "#{session_name}"}, listClients,
				{"tmux", "switch-client", "-c", "/dev/outer", "-t", pane},
				{"tmux", "detach-client", "-s", "fl-1"},
			}, ""},
		{"in a pane no other client shows: no detach", map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/outer"},
			"5\t/dev/outer\tmain\n",
			[][]string{
				{"tmux", "display-message", "-p", "-t", "%2", "#{session_name}"}, listClients,
				{"tmux", "switch-client", "-c", "/dev/outer", "-t", pane},
			}, ""},
		{"ordinary pane: only own-session clients → most recent of them, no detach", map[string]string{"TMUX_PANE": "%2"},
			"4\t/dev/old\tfl-1\n9\t/dev/mine\tfl-1\n",
			[][]string{
				{"tmux", "display-message", "-p", "-t", "%2", "#{session_name}"}, listClients,
				{"tmux", "switch-client", "-c", "/dev/mine", "-t", pane},
			}, ""},
		{"no client at all: error, nothing switched", map[string]string{"TMUX_PANE": "%2"},
			"",
			[][]string{{"tmux", "display-message", "-p", "-t", "%2", "#{session_name}"}, listClients}, "AGMUX_CLIENT"},
	} {
		r := &fakeRunner{tmuxOut: map[string]string{"list-clients": c.clients, "display-message": "fl-1\n"}}
		err := switchTo(r, func(k string) string { return c.env[k] }, pane)
		if !reflect.DeepEqual(r.calls, c.want) || (err == nil) != (c.err == "") || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s:\n calls %q\n want  %q\n err %v", c.name, r.calls, c.want, err)
		}
	}
}

// No client to switch: the reason shows on the board and agmux keeps running.
func TestSwitchNoClientStays(t *testing.T) {
	r := &fakeRunner{tmuxOut: map[string]string{"list-clients": "", "display-message": "fl-1\n"}}
	m := newModel(r)
	m.getenv = func(k string) string { return map[string]string{"TMUX_PANE": "%2"}[k] }
	m, quit := press(t, m, "enter")
	if quit || !strings.Contains(ansi.Strip(m.View()), "AGMUX_CLIENT") {
		t.Errorf("quit=%v, want the AGMUX_CLIENT hint on the message line", quit)
	}
}

// h/l stays in its band and takes the closest column there, even when that column is a folded title.
func TestSpatialNavBands(t *testing.T) {
	ss := staticSrc{}
	for _, s := range []struct{ id, group string }{
		{"a1", "/a"}, {"a2", "/a"}, {"a3", "/a"}, {"b1", "/b"}, {"b2", "/b"}, {"c1", "/c"}, {"d1", "/d"}, {"e1", "/e"},
	} {
		ss = append(ss, store.Session{ID: s.id, Name: s.id, GroupKey: s.group, TmuxPane: "%1", Status: "idle", ContextPct: -1})
	}
	nav := func(w int, fold bool, from string, keys ...string) string {
		m := New(ss, &fakeRunner{})
		tm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 80})
		m = tm.(Model)
		if fold {
			m.folded["/b"] = true
		}
		m.focus = from
		m, _ = press(t, m, keys...)
		return m.focus
	}
	b := headerID("/b")
	for _, c := range []struct {
		w          int
		fold       bool
		from, keys string
		want       string
	}{
		{200, true, "a1", "l", b},     // 4 columns: the folded title next door, not c1 further on
		{200, true, "c1", "h", b},     // and from the other side
		{200, true, b, "l", "c1"},     // off the title again
		{200, false, "d1", "l", "d1"}, // band's right edge
		{200, false, "e1", "l", "e1"}, // band 2 has only e1: h/l stay
		{200, false, "e1", "h", "e1"},
		{120, true, "a3", "l", b},     // 2 columns: stays in band 1, never d1 in band 2
		{120, false, "a3", "l", "b2"}, // nothing level with a3: the vertically closest in that column
		{120, false, "e1", "h", "e1"}, // band 3, alone
		{120, false, "c1", "l", "d1"},
		{120, false, "a3", "j", "c1"}, // j/k still cross bands
	} {
		if got := nav(c.w, c.fold, c.from, c.keys); got != c.want {
			t.Errorf("%d cols fold=%v: %s %s → %q, want %q", c.w, c.fold, c.from, c.keys, got, c.want)
		}
	}
}

func TestNearestTies(t *testing.T) {
	cur := rect{"c", 10, 0, 10, 5, 0}
	rs := []rect{cur, {"less", 0, 6, 12, 3, 0}, {"more", 14, 6, 10, 3, 0}, {"far", 10, 20, 10, 3, 0}}
	if got := nearest(rs, "c", directions["j"]); got != "more" {
		t.Errorf("j: same distance, most overlap wins: got %s", got)
	}
	side := []rect{cur, {"low", 30, 4, 10, 5, 0}, {"level", 30, 1, 10, 3, 0}, {"farcol", 50, 0, 10, 5, 0}, {"otherband", 22, 0, 5, 5, 1}}
	if got := nearest(side, "c", directions["l"]); got != "level" {
		t.Errorf("l: closest column, most vertical overlap: got %s", got)
	}
	apart := []rect{cur, {"below", 30, 9, 10, 3, 0}, {"further", 30, 20, 10, 3, 0}}
	if got := nearest(apart, "c", directions["l"]); got != "below" {
		t.Errorf("l with no overlap: vertically closest: got %s", got)
	}
}

// Keys before the first WindowSizeMsg (e.g. stdout redirected) must not crash on a zero-width layout.
func TestMoveBeforeSize(t *testing.T) {
	m := New(sessions, &fakeRunner{})
	for _, k := range []string{"j", "k", "h", "l"} {
		tm, _ := m.Update(key(k))
		m = tm.(Model)
	}
}

func wide(r *fakeRunner, w, h int) Model {
	m := New(sessions, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return tm.(Model)
}

func TestPreviewToggle(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 99, 40), "P")
	if m.preview || !strings.Contains(ansi.Strip(m.View()), "不足 100 列") {
		t.Fatal("under 100 columns P refuses and says why")
	}

	m = wide(r, 232, 50)
	tm, cmd := m.Update(key("P"))
	m = tm.(Model)
	if !m.previewOn() || m.cardsW() != 232-92 {
		t.Fatalf("preview on: cards %d wide, want 140", m.cardsW())
	}
	if n, _ := columns(m.cardsW()); n != 3 { // 140 columns at the 40-column minimum
		t.Errorf("cards beside the preview: %d columns", n)
	}
	if cmd == nil {
		t.Fatal("opening the preview captures right away")
	}
	msg := cmd()
	want := []string{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-44"} // 50 rows - top/msg/footer - title/box
	if got := r.calls[len(r.calls)-1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("capture: %q, want %q", got, want)
	}
	if tm, cmd = m.Update(frameMsg{}); cmd != nil { // a capture is in flight: the frame tick must not stack another
		m = tm.(Model)
		before := len(r.calls)
		runAll(cmd)
		if len(r.calls) != before {
			t.Errorf("a second capture started while one was in flight: %q", r.calls[before:])
		}
	}

	var text strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&text, "\x1b[32mline %d\x1b[0m\n", i)
	}
	text.WriteString("\n\n   \n") // blank rows under the cursor
	pm := msg.(previewMsg)
	pm.text = text.String()
	tm, _ = m.Update(pm)
	m = tm.(Model)
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) != 50 {
		t.Fatalf("%d lines, want 50", len(lines))
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > 232 {
			t.Errorf("line %d is %d wide", i, ansi.StringWidth(l))
		}
	}
	plain := ansi.Strip(v)
	if !strings.Contains(plain, "one · %1 · ⏸ idle") || !strings.Contains(plain, "line 200") || !strings.Contains(plain, "line 157") ||
		strings.Contains(plain, "line 156") || !strings.Contains(v, "\x1b[32mline 200") {
		t.Errorf("preview should show the title and the last 44 lines with their colors")
	}
	if !strings.Contains(lines[46], "line 200") { // 0 top bar, 1 title, 2 box top, 3..46 content, 47 box bottom
		t.Errorf("the last line sits at the bottom of the box (blank rows trimmed): %q", ansi.Strip(lines[46]))
	}
	for _, l := range lines[3:47] { // every content row closes its colors before the border
		if !strings.Contains(l, "\x1b[0m "+fg(cDim, "│")) {
			t.Fatalf("pane colors may bleed into the border: %q", l)
		}
	}

	m, _ = press(t, m, "P")
	if m.previewOn() || m.cardsW() != 232 || strings.Contains(ansi.Strip(m.View()), "line 200") {
		t.Error("P again closes it and the cards get the full width back")
	}
}

// runAll runs cmd once, and each command of a batch once (ticks just sleep and return).
func runAll(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			runAll(c)
		}
	}
}

func TestPreviewFollowsFocus(t *testing.T) {
	r := &fakeRunner{}
	m := wide(r, 232, 50)
	m, _ = press(t, m, "P") // capture %1 (result not fed back, so not "in flight" for the next tests)
	m.capturing = false
	tm, cmd := m.Update(key("l"))
	m = tm.(Model)
	if cmd == nil || !reflect.DeepEqual(cmd().(previewMsg).pane, "%3") {
		t.Fatal("moving the focus captures the new card's pane")
	}
	m.capturing = false
	tm, cmd = m.Update(key("h"))
	m = tm.(Model)
	m.capturing = false
	tm, cmd = m.Update(key("j")) // a2: background, no pane
	m = tm.(Model)
	if cmd != nil {
		if pm, ok := cmd().(previewMsg); ok {
			t.Fatalf("no capture without a pane, got %q", pm.pane)
		}
	}
	if !strings.Contains(ansi.Strip(m.View()), "没有 pane") {
		t.Error("a background session's preview says it has no pane")
	}
	m, _ = press(t, m, "f") // focus on the folded /a title
	if !strings.Contains(ansi.Strip(m.View()), "没有 pane") {
		t.Error("a folded title's preview says no pane")
	}
	// clicks hit the cards as laid out beside the preview (3 columns of 45 at 232), not full width
	m2, _ := press(t, wide(&fakeRunner{}, 232, 50), "P")
	tm, _ = m2.Update(tea.MouseMsg{X: 50, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if tm.(Model).focus != "b1" {
		t.Errorf("click at x=50 beside the preview: focus %q, want b1", tm.(Model).focus)
	}
	// a click inside the preview is not a card
	tm, _ = m.Update(tea.MouseMsg{X: 200, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if tm.(Model).focus != m.focus {
		t.Error("clicking the preview moved the focus")
	}
}

// The preview refreshes on the 500ms frame tick (the cards keep their 2s poll).
func TestPreviewRefreshesOnFrame(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "P")
	m.capturing = false // the opening capture came back
	before := len(r.calls)
	tm, cmd := m.Update(frameMsg{})
	runAll(cmd)
	if len(r.calls) != before+1 || r.calls[before][1] != "capture-pane" || !tm.(Model).capturing {
		t.Errorf("a frame tick with the preview open captures again: %q", r.calls[before:])
	}
	m.preview = false
	before = len(r.calls)
	_, cmd = m.Update(frameMsg{})
	runAll(cmd)
	if len(r.calls) != before {
		t.Errorf("no captures with the preview closed: %q", r.calls[before:])
	}
}

func TestSendKeysArgs(t *testing.T) {
	for _, c := range []struct {
		k    tea.KeyMsg
		want []string // after "send-keys -t %1"
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("你好 world")}, []string{"-l", "--", "你好 world"}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-x\nmore"), Paste: true}, []string{"-l", "--", "-x\nmore"}},
		{tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, []string{"-l", "--", " "}},
		{tea.KeyMsg{Type: tea.KeyEnter}, []string{"Enter"}},
		{tea.KeyMsg{Type: tea.KeyEsc}, []string{"Escape"}},
		{tea.KeyMsg{Type: tea.KeyCtrlC}, []string{"C-c"}},
		{tea.KeyMsg{Type: tea.KeyCtrlX}, []string{"C-x"}},
		{tea.KeyMsg{Type: tea.KeyUp}, []string{"Up"}},
		{tea.KeyMsg{Type: tea.KeyLeft}, []string{"Left"}},
		{tea.KeyMsg{Type: tea.KeyShiftTab}, []string{"BTab"}},
		{tea.KeyMsg{Type: tea.KeyBackspace}, []string{"BSpace"}},
		{tea.KeyMsg{Type: tea.KeyDelete}, []string{"DC"}},
		{tea.KeyMsg{Type: tea.KeyPgUp}, []string{"PPage"}},
		{tea.KeyMsg{Type: tea.KeyF5}, []string{"F5"}},
		{tea.KeyMsg{Type: tea.KeyCtrlLeft}, []string{"C-Left"}},
		{tea.KeyMsg{Type: tea.KeyCtrlShiftUp}, []string{"C-S-Up"}},
		{tea.KeyMsg{Type: tea.KeyUp, Alt: true}, []string{"M-Up"}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, []string{"M-x"}},
		{tea.KeyMsg{Type: tea.KeyCtrlAt}, []string{"C-Space"}},
	} {
		got := sendKeysArgs("%1", c.k)
		if want := append([]string{"send-keys", "-t", "%1"}, c.want...); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q, want %q", c.k.String(), got, want)
		}
	}
}

// tab hands the keys to the previewed pane: everything but tab is forwarded, ctrl+c and q included,
// each followed by a capture; tab gives them back.
func TestPreviewPassThrough(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "tab") // preview closed: tab does nothing
	if m.pvFocus {
		t.Fatal("tab with the preview closed")
	}
	m, _ = press(t, m, "P", "tab")
	if !m.pvFocus {
		t.Fatal("tab should focus the preview")
	}
	v := m.View()
	focusEsc, _, _ := strings.Cut(fg(cFocus, "╭"), "╭")
	if !strings.Contains(v, focusEsc+"╭") || !strings.HasSuffix(ansi.Strip(v), "按键直通 pane · tab 回到卡片") {
		t.Error("focused preview: #ff8ebe border and the pass-through footer")
	}
	r.calls = nil
	m.scroll = 6
	m, quit := press(t, m, "你好 world", "enter", "ctrl+c", "q", "esc")
	if quit {
		t.Fatal("ctrl+c / q / esc go to the pane while it has the keys")
	}
	capture := []string{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-44"}
	var want [][]string
	for _, k := range [][]string{{"-l", "--", "你好 world"}, {"Enter"}, {"C-c"}, {"-l", "--", "q"}, {"Escape"}} {
		want = append(want, append([]string{"tmux", "send-keys", "-t", "%1"}, k...), capture)
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("forwarded:\n %q\nwant\n %q", r.calls, want)
	}
	if m.scroll != 0 {
		t.Error("a forwarded key scrolls the preview back to the bottom")
	}
	m, quit = press(t, m, "tab", "q")
	if m.pvFocus || !quit {
		t.Error("tab gives the keys back to the board, then q quits")
	}

	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "P", "j", "tab") // a2: no pane
	if m.pvFocus {
		t.Error("no pane, nothing to type into")
	}
}

func TestPreviewWheelAndClicks(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "P")
	m.capturing = false
	wheel := func(m Model, x int, b tea.MouseButton) (Model, tea.Cmd) {
		tm, cmd := m.Update(tea.MouseMsg{X: x, Y: 10, Action: tea.MouseActionPress, Button: b})
		return tm.(Model), cmd
	}
	m, cmd := wheel(m, 200, tea.MouseButtonWheelUp)
	if m.scroll != 3 || cmd == nil {
		t.Fatalf("wheel up over the preview: scroll %d", m.scroll)
	}
	if pm := cmd().(previewMsg); r.calls[len(r.calls)-1][7] != "-47" {
		t.Errorf("scrolled capture asks for 44+3 lines: %q (%v)", r.calls[len(r.calls)-1], pm.pane)
	}
	var text strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&text, "line %d\n", i)
	}
	tm, _ := m.Update(previewMsg{pane: "%1", text: text.String()})
	m = tm.(Model)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "line 97") || strings.Contains(v, "line 98") || !strings.Contains(v, "line 54") {
		t.Error("scrolled 3 back: lines 54..97 show")
	}
	m.capturing = false
	m, _ = wheel(m, 200, tea.MouseButtonWheelDown)
	m.capturing = false
	m, _ = wheel(m, 200, tea.MouseButtonWheelDown)
	if m.scroll != 0 {
		t.Errorf("wheel down stops at the bottom: %d", m.scroll)
	}
	if m, _ = wheel(m, 10, tea.MouseButtonWheelUp); m.scroll != 0 {
		t.Error("the wheel over the cards doesn't scroll the preview")
	}
	tm, _ = m.Update(tea.MouseMsg{X: 200, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m = tm.(Model); !m.pvFocus {
		t.Error("a click in the preview gives it the keys")
	}
	tm, _ = m.Update(tea.MouseMsg{X: 50, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m = tm.(Model); m.pvFocus || m.focus != "b1" {
		t.Errorf("a click on a card takes the keys back: pvFocus=%v focus=%s", m.pvFocus, m.focus)
	}
}
