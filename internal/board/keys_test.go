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
	_, rects, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false, nil)
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
	_, before, _ := view(sessions, m.w, m.h, "", 0, now, "", false, nil)

	m, _ = press(t, m, "f") // fold /a (focus was on a1)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "▸ /a (2)") || strings.Contains(v, " one ") || strings.Contains(v, " two ") || !strings.Contains(v, "▾ /b (1)") {
		t.Fatalf("folded /a: title ▸, its cards gone, /b untouched:\n%s", v)
	}
	if m.focus != headerID("/a") {
		t.Fatalf("focus %q, want the folded title", m.focus)
	}
	_, after, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false, m.folded)
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
	if nearest(nil, "x", directions["j"]) != "" || nearest([]rect{{"a", 0, 0, 10, 5}}, "zz", directions["j"]) != "" {
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
