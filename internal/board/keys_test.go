package board

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

type staticSrc []store.Session

func (s staticSrc) Sessions() []store.Session           { return s }
func (s staticSrc) Err() error                          { return nil }
func (staticSrc) SetAvatar(store.Session, string) error { return nil }

type fakeRunner struct {
	mu       sync.Mutex // forwarded keys call Tmux from the serial worker
	calls    [][]string
	at       []time.Time // when each call was made
	err      error
	script   string            // what Interactive runs (sh -c); default: true
	tmuxOut  map[string]string // tmux subcommand → stdout; list-clients defaults to one client /dev/c
	tmuxFail map[string]error  // tmux subcommand → error
}

func init() { enterDelay = 0 } // TestSendWaitsBeforeEnter sets its own

func (f *fakeRunner) Tmux(args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{"tmux"}, args...))
	f.at = append(f.at, time.Now())
	if err := f.tmuxFail[args[0]]; err != nil {
		return "", err
	}
	if out, ok := f.tmuxOut[args[0]]; ok {
		return out, f.err
	}
	if args[0] == "list-clients" {
		return "1\t/dev/c\tmain\n", f.err
	}
	return "", f.err
}

var listClients = []string{"tmux", "list-clients", "-F", clientsFormat}

// captureCall is the one tmux call that reads a pane for the preview: its cursor, then its last n
// lines. The ";" is an argv of its own, which tmux takes as the end of the first command.
func captureCall(pane string, n int) []string {
	return []string{"tmux", "display-message", "-p", "-t", pane, "#{cursor_x} #{cursor_y} #{cursor_flag} #{cursor_shape} #{pane_height}",
		";", "capture-pane", "-p", "-e", "-t", pane, "-S", fmt.Sprintf("-%d", n)}
}

func isCapture(call []string) bool {
	return len(call) > 7 && call[1] == "display-message" && call[7] == "capture-pane"
}

// switched is what enter on a card with pane runs by default: pick the one client, switch it.
func switched(pane string) [][]string {
	return [][]string{listClients, {"tmux", "switch-client", "-c", "/dev/c", "-t", pane}}
}

func sent(pane, text string) [][]string {
	return [][]string{{"tmux", "send-keys", "-t", pane, "-l", "--", text}, {"tmux", "send-keys", "-t", pane, "Enter"}}
}

func (f *fakeRunner) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func (f *fakeRunner) TmuxIn(stdin string, args ...string) error {
	_, err := f.Tmux(append(args, "<stdin:"+stdin+">")...)
	return err
}

func (f *fakeRunner) Run(args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+]":
		return tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
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
		{"background: enter / H / i refused", []string{"j", "enter", "H", "i", "x", "y"}, [][]string{{"kill", "a2"}}, false},
		{"q quits", []string{"q"}, nil, true},
		{"arrow keys move", []string{"down", "r", "up", "right", "r"}, [][]string{{"restart", "a2"}, {"restart", "b1"}}, false},
		{"up comes back", []string{"down", "up", "r"}, [][]string{{"restart", "a1"}}, false},
		{"burst of keys = one by one", []string{"lr"}, [][]string{{"restart", "b1"}}, false},
		{"unfolding focuses the group's first card", []string{"l", "f", "f", "r"}, [][]string{{"restart", "b1"}}, false},
		{"burst keeps typed order", []string{"Hr"}, append(sent("%1", "/handoff"), []string{"restart", "a1"}), false},
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
	_, rects, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false, nil, m.w, nil, nil, -1)
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

func (s *mutSrc) Sessions() []store.Session           { return s.ss }
func (s *mutSrc) Err() error                          { return nil }
func (*mutSrc) SetAvatar(store.Session, string) error { return nil }

// The x / s prompts act on the session they named, never on whatever focus falls back to.
func TestPromptTargetIsPinned(t *testing.T) {
	for _, keys := range [][]string{{"x", "y"}} {
		src := &mutSrc{ss: sessions}
		r := &fakeRunner{}
		m, _ := press(t, newModel(r), "l") // focus b1
		m.src = src
		m, _ = press(t, m, keys[0])
		src.ss = sessions[:2] // b1 disappears; focus would fall back to a1
		m, _ = press(t, m, keys[1:]...)
		if len(r.calls) != 0 || !strings.Contains(m.View(), "three is gone") {
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
	if !strings.Contains(tm.(Model).View(), "y to confirm") {
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
		{"echo fine >&2", "review finished: one"},
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
	for _, k := range []string{"enter", "H", "i"} {
		m, _ := press(t, newModel(&fakeRunner{}), "j", k) // a2 is the background session
		if n := strings.Count(ansi.Strip(m.View()), noPane); n != 1 {
			t.Errorf("%s on a background session: hint shown %d times, want 1", k, n)
		}
	}
}

func TestFold(t *testing.T) {
	r := &fakeRunner{}
	m := newModel(r)
	_, before, _ := view(sessions, m.w, m.h, "", 0, now, "", false, nil, m.w, nil, nil, -1)

	m, _ = press(t, m, "f") // fold /a (focus was on a1)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "▸ /a (2)") || strings.Contains(v, " one ") || strings.Contains(v, " two ") || !strings.Contains(v, "▾ /b (1)") {
		t.Fatalf("folded /a: title ▸, its cards gone, /b untouched:\n%s", v)
	}
	if m.focus != headerID("/a") {
		t.Fatalf("focus %q, want the folded title", m.focus)
	}
	_, after, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false, m.folded, m.w, nil, nil, -1)
	for _, b := range before {
		for _, a := range after {
			if a.id == "b1" && b.id == "b1" && a != b {
				t.Errorf("other groups must not move: b1 %+v → %+v", b, a)
			}
		}
	}

	m, _ = press(t, m, "r", "x", "y", "H", "i", "j") // session keys do nothing on a title
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
	if !strings.Contains(ansi.Strip(m.View()), "f fold") {
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
	if nearest(nil, "x", directions["down"]) != "" || nearest([]rect{{"a", 0, 0, 10, 5, 0}}, "zz", directions["down"]) != "" {
		t.Error("unknown focus or no rects: stay")
	}
}

// Which client enter switches, and which clients are detached afterwards.
func TestSwitchTo(t *testing.T) {
	const pane = "%7"
	display := []string{"tmux", "display-message", "-p", "-t", "%2", "#{session_name}"}
	sw := func(c string) []string { return []string{"tmux", "switch-client", "-c", c, "-t", pane} }
	detach := func(c string) []string { return []string{"tmux", "detach-client", "-t", c} }
	showEnv := []string{"tmux", "show-environment", "-h", "-t", "fl-1", "AGMUX_CLIENT"}
	for _, c := range []struct {
		name    string
		env     map[string]string
		own     string // display-message output
		clients string // list-clients output: activity \t name \t session
		want    [][]string
		err     string
	}{
		{"AGMUX_CLIENT wins over activity", map[string]string{"AGMUX_CLIENT": "/dev/a"}, "",
			"5\t/dev/a\tmain\n9\t/dev/b\tother\n",
			[][]string{listClients, sw("/dev/a")}, ""},
		{"AGMUX_CLIENT not attached (stale): guess, no detach", map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/gone"}, "fl-1\n",
			"5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n",
			[][]string{display, listClients, sw("/dev/outer")}, ""},
		{"AGMUX_CLIENT is a client inside own session (another float's inner one): guess, no detach", map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/nested"}, "fl-1\n",
			"5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n",
			[][]string{display, listClients, sw("/dev/outer")}, ""},
		{"direct popup: most recent client", nil, "",
			"5\t/dev/a\tmain\n9\t/dev/b\tother\n3\t/dev/c\tmain\n",
			[][]string{listClients, sw("/dev/b")}, ""},
		{"nested float without AGMUX_CLIENT: switch the outer one, detach nothing", map[string]string{"TMUX_PANE": "%2"}, "fl-1\n",
			"5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n",
			[][]string{display, listClients, showEnv, sw("/dev/outer")}, ""},
		{"nested float with AGMUX_CLIENT: detach each other client of own session", map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/outer"}, "fl-1\n",
			"5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n4\t/dev/nested2\tfl-1\n",
			[][]string{display, listClients, sw("/dev/outer"), detach("/dev/nested"), detach("/dev/nested2")}, ""},
		{"session names with spaces", map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/outer"}, "my float\n",
			"5\t/dev/outer\tmy main\n9\t/dev/nested\tmy float\n",
			[][]string{display, listClients, sw("/dev/outer"), detach("/dev/nested")}, ""},
		{"a session name ending in a space is not trimmed", map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/outer"}, "fl \n",
			"5\t/dev/outer\tfl\n9\t/dev/nested\tfl \n",
			[][]string{display, listClients, sw("/dev/outer"), detach("/dev/nested")}, ""},
		{"ordinary pane: only own-session clients → most recent of them, no detach", map[string]string{"TMUX_PANE": "%2"}, "fl-1\n",
			"4\t/dev/old\tfl-1\n9\t/dev/mine\tfl-1\n",
			[][]string{display, listClients, showEnv, sw("/dev/mine")}, ""},
		{"no client at all: error, nothing switched", map[string]string{"TMUX_PANE": "%2"}, "fl-1\n",
			"",
			[][]string{display, listClients, showEnv}, "AGMUX_CLIENT"},
	} {
		r := &fakeRunner{tmuxOut: map[string]string{"list-clients": c.clients, "display-message": c.own}}
		err := switchTo(r, func(k string) string { return c.env[k] }, pane)
		if !reflect.DeepEqual(r.calls, c.want) || (err == nil) != (c.err == "") || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s:\n calls %q\n want  %q\n err %v", c.name, r.calls, c.want, err)
		}
	}
}

// AGMUX_CLIENT from the float session's environment (set by the launcher with set-environment):
// used like the process variable, which still wins when both are set.
func TestSwitchSessionEnv(t *testing.T) {
	const pane = "%7"
	clients := "5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n3\t/dev/old\tother\n"
	display := []string{"tmux", "display-message", "-p", "-t", "%2", "#{session_name}"}
	showEnv := []string{"tmux", "show-environment", "-h", "-t", "fl-1", "AGMUX_CLIENT"}
	for _, c := range []struct {
		name    string
		env     map[string]string
		sessEnv string
		envErr  error
		want    [][]string
	}{
		{"session env names the client: use it, detach the nested one", nil, "AGMUX_CLIENT=/dev/old\n", nil,
			[][]string{display, listClients, showEnv, {"tmux", "switch-client", "-c", "/dev/old", "-t", pane}, {"tmux", "detach-client", "-t", "/dev/nested"}}},
		{"-AGMUX_CLIENT (removed): heuristic, no detach", nil, "-AGMUX_CLIENT\n", nil,
			[][]string{display, listClients, showEnv, {"tmux", "switch-client", "-c", "/dev/outer", "-t", pane}}},
		{"show-environment fails: heuristic", nil, "", errors.New("unknown variable: AGMUX_CLIENT"),
			[][]string{display, listClients, showEnv, {"tmux", "switch-client", "-c", "/dev/outer", "-t", pane}}},
		{"unexpected output is not a client", nil, "something else\n", nil,
			[][]string{display, listClients, showEnv, {"tmux", "switch-client", "-c", "/dev/outer", "-t", pane}}},
		{"session env names a client in own session: heuristic, no detach", nil, "AGMUX_CLIENT=/dev/nested\n", nil,
			[][]string{display, listClients, showEnv, {"tmux", "switch-client", "-c", "/dev/outer", "-t", pane}}},
		{"session env names a detached client: heuristic, no detach", nil, "AGMUX_CLIENT=/dev/gone\n", nil,
			[][]string{display, listClients, showEnv, {"tmux", "switch-client", "-c", "/dev/outer", "-t", pane}}},
		{"process env wins: session env not even read", map[string]string{"AGMUX_CLIENT": "/dev/outer"}, "AGMUX_CLIENT=/dev/old\n", nil,
			[][]string{display, listClients, {"tmux", "switch-client", "-c", "/dev/outer", "-t", pane}, {"tmux", "detach-client", "-t", "/dev/nested"}}},
	} {
		env := map[string]string{"TMUX_PANE": "%2"}
		for k, v := range c.env {
			env[k] = v
		}
		r := &fakeRunner{tmuxOut: map[string]string{"list-clients": clients, "display-message": "fl-1\n", "show-environment": c.sessEnv}}
		if c.envErr != nil {
			r.tmuxFail = map[string]error{"show-environment": c.envErr}
		}
		if err := switchTo(r, func(k string) string { return env[k] }, pane); err != nil || !reflect.DeepEqual(r.calls, c.want) {
			t.Errorf("%s:\n calls %q\n want  %q\n err %v", c.name, r.calls, c.want, err)
		}
	}
}

// A TMUX_PANE that no longer exists (left in the server's global environment) is not an error.
func TestSwitchStaleTmuxPane(t *testing.T) {
	r := &fakeRunner{tmuxFail: map[string]error{"display-message": errors.New("can't find pane: %2")},
		tmuxOut: map[string]string{"list-clients": "5\t/dev/a\tmain\n"}}
	if err := switchTo(r, func(k string) string { return map[string]string{"TMUX_PANE": "%2"}[k] }, "%7"); err != nil ||
		!reflect.DeepEqual(r.calls[len(r.calls)-1], []string{"tmux", "switch-client", "-c", "/dev/a", "-t", "%7"}) {
		t.Errorf("err %v, calls %q", err, r.calls)
	}
}

// A failing tmux step stops the chain: no detach after a failed switch, no Enter after failed text.
func TestTmuxFailuresStopTheChain(t *testing.T) {
	r := &fakeRunner{tmuxFail: map[string]error{"switch-client": errors.New("tmux switch-client: no such client")},
		tmuxOut: map[string]string{"list-clients": "5\t/dev/outer\tmain\n9\t/dev/nested\tfl-1\n", "display-message": "fl-1\n"}}
	m := newModel(r)
	m.getenv = func(k string) string { return map[string]string{"TMUX_PANE": "%2", "AGMUX_CLIENT": "/dev/outer"}[k] }
	m, quit := press(t, m, "enter")
	if quit || r.calls[len(r.calls)-1][1] != "switch-client" || !strings.Contains(ansi.Strip(m.View()), "no such client") {
		t.Errorf("failed switch: quit=%v calls %q", quit, r.calls)
	}

	r = &fakeRunner{tmuxFail: map[string]error{"send-keys": errors.New("tmux send-keys: can't find pane")}}
	m, _ = press(t, newModel(r), "H")
	if len(r.calls) != 1 || !strings.Contains(ansi.Strip(m.View()), "can't find pane") {
		t.Errorf("failed text must not be followed by Enter: %q", r.calls)
	}
}

func TestSendWaitsBeforeEnter(t *testing.T) {
	enterDelay = 120 * time.Millisecond
	defer func() { enterDelay = 0 }()
	r := &fakeRunner{}
	press(t, newModel(r), "H")
	if len(r.at) != 2 || r.at[1].Sub(r.at[0]) < 120*time.Millisecond {
		t.Errorf("Enter must come ≥120ms after the text: %v", r.calls)
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
	if got := nearest(rs, "c", directions["down"]); got != "more" {
		t.Errorf("j: same distance, most overlap wins: got %s", got)
	}
	side := []rect{cur, {"low", 30, 4, 10, 5, 0}, {"level", 30, 1, 10, 3, 0}, {"farcol", 50, 0, 10, 5, 0}, {"otherband", 22, 0, 5, 5, 1}}
	if got := nearest(side, "c", directions["right"]); got != "level" {
		t.Errorf("l: closest column, most vertical overlap: got %s", got)
	}
	apart := []rect{cur, {"below", 30, 9, 10, 3, 0}, {"further", 30, 20, 10, 3, 0}}
	if got := nearest(apart, "c", directions["right"]); got != "below" {
		t.Errorf("l with no overlap: vertically closest: got %s", got)
	}
}

// Keys before the first WindowSizeMsg (e.g. stdout redirected) must not crash on a zero-width layout.
func TestMoveBeforeSize(t *testing.T) {
	m := New(sessions, &fakeRunner{})
	for _, k := range []string{"j", "k", "h", "l", "s"} {
		tm, _ := m.Update(key(k))
		m = tm.(Model)
	}
	if m.mode == flashing {
		t.Error("no labels before the first size: there is no layout")
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
	m, _ := press(t, wide(r, 99, 40), "p")
	if m.preview || !strings.Contains(ansi.Strip(m.View()), "at least 100 columns") {
		t.Fatal("under 100 columns p refuses and says why")
	}

	m = wide(r, 232, 50)
	tm, cmd := m.Update(key("p"))
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
	want := captureCall("%1", 44) // 50 rows - top/msg/footer - title/box
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

	m, _ = press(t, m, "p")
	if m.previewOn() || m.cardsW() != 232 || strings.Contains(ansi.Strip(m.View()), "line 200") {
		t.Error("p again closes it and the cards get the full width back")
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
	m, _ = press(t, m, "p") // capture %1 (result not fed back, so not "in flight" for the next tests)
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
	if !strings.Contains(ansi.Strip(m.View()), "no pane") {
		t.Error("a background session's preview says it has no pane")
	}
	m, _ = press(t, m, "f") // focus on the folded /a title
	if !strings.Contains(ansi.Strip(m.View()), "no pane") {
		t.Error("a folded title's preview says no pane")
	}
	// clicks hit the cards as laid out beside the preview (3 columns of 45 at 232), not full width
	m2, _ := press(t, wide(&fakeRunner{}, 232, 50), "p")
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
	m, _ := press(t, wide(r, 232, 50), "p")
	m.capturing = false // the opening capture came back
	before := len(r.calls)
	tm, cmd := m.Update(frameMsg{})
	runAll(cmd)
	if len(r.calls) != before+1 || !isCapture(r.calls[before]) || !tm.(Model).capturing {
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

// tab opens the preview if it is closed and hands it the keys: everything but tab is forwarded,
// ctrl+c, q and shift+tab included, each followed by a capture; tab gives them back and the
// preview stays.
func TestPreviewPassThrough(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "tab") // preview closed: tab opens it and goes in
	if !m.pvFocus || !m.previewOn() {
		t.Fatal("tab with the preview closed opens it and focuses it")
	}
	v := m.View()
	focusEsc, _, _ := strings.Cut(fg(cFocus, "╭"), "╭")
	if !strings.Contains(v, focusEsc+"╭") || !strings.HasSuffix(ansi.Strip(v), "keys go to the pane · tab back to the cards") {
		t.Error("focused preview: #ff8ebe border and the pass-through footer")
	}
	r.calls = nil
	m.scroll = 6
	m, quit := press(t, m, "你好 world", "enter", "ctrl+c", "q", "esc", "shift+tab", "ctrl+]")
	if quit || !m.pvFocus {
		t.Fatal("ctrl+c / q / esc / shift+tab / ctrl+] go to the pane while it has the keys")
	}
	capture := captureCall("%1", 44)
	var want [][]string
	for _, k := range [][]string{{"-l", "--", "你好 world"}, {"Enter"}, {"C-c"}, {"-l", "--", "q"}, {"Escape"}, {"BTab"}, {"C-]"}} {
		want = append(want, append([]string{"tmux", "send-keys", "-t", "%1"}, k...), capture)
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("forwarded:\n %q\nwant\n %q", r.calls, want)
	}
	if m.scroll != 0 {
		t.Error("a forwarded key scrolls the preview back to the bottom")
	}
	m, _ = press(t, m, "tab")
	if m.pvFocus || !m.previewOn() || strings.Contains(ansi.Strip(m.View()), "keys go to the pane") {
		t.Error("tab gives the keys back to the board; the preview stays open")
	}
	if m, quit = press(t, m, "q"); !quit {
		t.Error("then q quits")
	}
	if m, _ = press(t, wide(&fakeRunner{}, 232, 50), "tab", "tab", "p"); m.preview || m.pvFocus {
		t.Error("p closes the preview")
	}
	m, _ = press(t, wide(&fakeRunner{}, 99, 40), "tab")
	if m.preview || m.pvFocus || !strings.Contains(ansi.Strip(m.View()), "at least 100 columns") {
		t.Error("tab on a narrow terminal says why it can't open the preview")
	}

	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "j", "tab") // a2: no pane
	if m.pvFocus || m.preview {
		t.Error("no pane, nothing to type into: tab doesn't even open the preview")
	}
}

func TestPreviewWheelAndClicks(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "p")
	var hist strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&hist, "line %d\n", i)
	}
	tm0, _ := m.Update(previewMsg{pane: "%1", text: hist.String()}) // 100 lines captured: room to scroll
	m = tm0.(Model)
	m.capturing = false
	wheel := func(m Model, x int, b tea.MouseButton) (Model, tea.Cmd) {
		tm, cmd := m.Update(tea.MouseMsg{X: x, Y: 10, Action: tea.MouseActionPress, Button: b})
		return tm.(Model), cmd
	}
	m, cmd := wheel(m, 200, tea.MouseButtonWheelUp)
	if m.scroll != 3 || cmd == nil {
		t.Fatalf("wheel up over the preview: scroll %d", m.scroll)
	}
	if pm := cmd().(previewMsg); r.calls[len(r.calls)-1][len(r.calls[len(r.calls)-1])-1] != "-47" {
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

// s = reply: open the preview if needed and hand it the keys; what is typed next goes to the pane.
func TestSReplies(t *testing.T) {
	r := &fakeRunner{}
	m := wide(r, 232, 50)
	m, _ = press(t, m, "j", "k") // focus on a1 first: a focus change would capture on its own
	tm, cmd := m.Update(key("i"))
	m = tm.(Model)
	if !m.previewOn() || !m.pvFocus || cmd == nil {
		t.Fatalf("s: preview %v, keys %v", m.previewOn(), m.pvFocus)
	}
	if msg, ok := cmd().(previewMsg); !ok || msg.pane != "%1" { // the opening capture
		t.Fatalf("i captures the pane it opened on: %#v", msg)
	}
	m.capturing = false
	r.calls = nil
	m, quit := press(t, m, "hi", "q", "enter")
	want := [][]string{{"tmux", "send-keys", "-t", "%1", "-l", "--", "hi"}, {"tmux", "send-keys", "-t", "%1", "-l", "--", "q"}, {"tmux", "send-keys", "-t", "%1", "Enter"}}
	var sends [][]string
	for _, c := range r.calls {
		if c[1] == "send-keys" {
			sends = append(sends, c)
		}
	}
	if quit || !reflect.DeepEqual(sends, want) {
		t.Errorf("after s, keys go to the pane: quit=%v %q", quit, sends)
	}

	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "p", "i") // already open: s only takes the keys
	if !m.previewOn() || !m.pvFocus {
		t.Error("s with the preview open focuses it")
	}
	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "j", "i") // a2: no pane
	if m.preview || m.pvFocus {
		t.Error("s on a session without a pane does nothing")
	}
	m, _ = press(t, wide(&fakeRunner{}, 99, 40), "i")
	if m.preview || !strings.Contains(ansi.Strip(m.View()), "at least 100 columns") {
		t.Error("s on a narrow terminal says why it can't open the preview")
	}
	if !strings.Contains(ansi.Strip(wide(&fakeRunner{}, 232, 50).View()), "tab focus preview · i reply · H handoff") {
		t.Error("footer order: … tab focus preview · i reply · H handoff …")
	}
}

// A burst typed into the preview reaches the pane in order even though bubbletea runs each
// key's command on its own goroutine.
func TestForwardedKeysKeepOrder(t *testing.T) {
	for round := range 20 {
		r := &fakeRunner{}
		m, _ := press(t, wide(r, 232, 50), "p", "tab")
		r.mu.Lock()
		r.calls = nil
		r.mu.Unlock()
		var cmds []tea.Cmd
		for _, k := range []tea.KeyMsg{
			{Type: tea.KeyRunes, Runes: []rune("a")}, {Type: tea.KeySpace, Runes: []rune(" ")}, {Type: tea.KeyRunes, Runes: []rune("b")},
			{Type: tea.KeySpace, Runes: []rune(" ")}, {Type: tea.KeyRunes, Runes: []rune("-")}, {Type: tea.KeyRunes, Runes: []rune("修一下")},
			{Type: tea.KeyRunes, Runes: []rune("c")}, {Type: tea.KeyEnter},
		} {
			tm, cmd := m.Update(k)
			m = tm.(Model)
			cmds = append(cmds, cmd)
		}
		var wg sync.WaitGroup
		for i := len(cmds) - 1; i >= 0; i-- { // run them like bubbletea: concurrently, in any order
			wg.Add(1)
			go func(c tea.Cmd) { defer wg.Done(); c() }(cmds[i])
		}
		wg.Wait()
		var got []string
		for _, c := range r.calls {
			if c[1] == "send-keys" {
				got = append(got, c[len(c)-1])
			}
		}
		if want := []string{"a", " ", "b", " ", "-", "修一下", "c", "Enter"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: keys reached the pane as %q", round, got)
		}
	}
}

// T11 review: the edges that mutations showed untested.
func TestPreviewEdges(t *testing.T) {
	// a tiny terminal must not crash the preview
	for h := 5; h >= 1; h-- {
		m, _ := press(t, wide(&fakeRunner{}, 120, 20), "p")
		m.pv = previewMsg{pane: "%1", text: "a\nb\nc\n"}
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: h})
		_ = tm.(Model).View()
	}

	// a capture coming back frees the next frame's capture
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "p")
	tm, _ := m.Update(previewMsg{pane: "%1", text: "x"})
	m = tm.(Model)
	before := len(r.calls)
	tm, cmd := m.Update(frameMsg{})
	runAll(cmd)
	if len(r.calls) != before+1 {
		t.Error("after a capture returns, the next frame captures again")
	}

	// widths: at least 60 for the preview, opens at 100, closes below
	if m := wide(&fakeRunner{}, 100, 40); func() bool { m, _ = press(t, m, "p"); return !m.previewOn() || m.cardsW() != 40 }() {
		t.Errorf("100 columns: preview on, cards %d (want 40)", m.cardsW())
	}
	m, _ = press(t, wide(&fakeRunner{}, 100, 40), "p")
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 99, Height: 40})
	if m = tm.(Model); m.previewOn() || m.cardsW() != 99 {
		t.Errorf("99 columns: preview off, cards %d", m.cardsW())
	}
	if m, _ = press(t, m, "p"); m.preview {
		t.Error("too narrow with the preview still set: P turns it off")
	}

	// stale content for another pane never shows; a capture error does
	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "p")
	m.pv = previewMsg{pane: "%3", text: "stale-from-b1"}
	if strings.Contains(ansi.Strip(m.View()), "stale-from-b1") {
		t.Error("content captured for another pane must not show")
	}
	m.pv = previewMsg{pane: "%1", err: errors.New("tmux capture-pane: can't find pane %1")}
	if !strings.Contains(ansi.Strip(m.View()), "can't find pane %1") {
		t.Error("a capture error shows in the preview")
	}
	// tabs become spaces, the box keeps its width
	for _, l := range previewLines(sessions[0], true, previewMsg{pane: "%1", text: "a\tb"}, 60, 10, 0, false) {
		if strings.Contains(l, "\t") || ansi.StringWidth(l) != 60 {
			t.Errorf("preview line %q (width %d)", l, ansi.StringWidth(l))
		}
	}
}

// hjkl beside the open preview move over the narrowed layout: with 4 groups at 232 columns the
// cards get 3 columns, so l l l from a1 stops on the third group instead of reaching the fourth.
func TestPreviewNavUsesCardWidth(t *testing.T) {
	ss := staticSrc{}
	for _, g := range []string{"/a", "/b", "/c", "/d"} {
		ss = append(ss, store.Session{ID: g[1:] + "1", Name: g[1:] + "1", GroupKey: g, TmuxPane: "%1", Status: "idle", ContextPct: -1})
	}
	m := New(ss, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m, _ = press(t, tm.(Model), "p", "l", "l", "l")
	if m.focus != "c1" {
		t.Errorf("focus %s, want c1 (d1 is in the next band beside the preview)", m.focus)
	}
}

// Every focus change captures at once: a click, unfolding, and a capture that came back for a
// pane that is no longer focused.
func TestPreviewFollowsEveryFocusChange(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "p")
	m.capturing = false
	tm, cmd := m.Update(tea.MouseMsg{X: 50, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}) // click b1
	m = tm.(Model)
	runAll(cmd)
	if last := r.calls[len(r.calls)-1]; m.focus != "b1" || !isCapture(last) || last[4] != "%3" {
		t.Errorf("a click captures the new card's pane: %q", last)
	}
	m.capturing = false
	m, _ = press(t, m, "h", "f") // fold /a: focus on its title
	m.capturing = false
	before := len(r.calls)
	tm, cmd = m.Update(key("enter")) // unfold: focus lands on a1
	m = tm.(Model)
	runAll(cmd)
	if len(r.calls) == before || r.calls[len(r.calls)-1][4] != "%1" {
		t.Errorf("unfolding moves the focus to a1 and captures it: %q", r.calls[before:])
	}
	// a capture for b1 arrives after the focus moved to a1: capture a1 straight away
	m.capturing = true
	tm, cmd = m.Update(previewMsg{pane: "%3", text: "old"})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("a capture for a pane no longer focused triggers a new one")
	}
	if pm := cmd().(previewMsg); pm.pane != "%1" {
		t.Errorf("recaptured %s, want %%1", pm.pane)
	}
}

// Pass-through is locked to the session and pane it was entered on. If that session ends or its
// pane changes, the next key is swallowed and the board takes the keys back — it never types into
// whatever pane now sits where the session was.
func TestPassThroughLock(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor) // it tells the borders apart by color
	defer lipgloss.SetColorProfile(old)
	for _, c := range []struct {
		name  string
		after []store.Session
	}{
		{"pane changed", []store.Session{{ID: "a1", Name: "one", GroupKey: "/a", TmuxPane: "%9", Status: "idle", ContextPct: -1}, sessions[2]}},
		{"session gone", []store.Session{sessions[1], sessions[2]}},
		{"gone, another session now in its pane", []store.Session{{ID: "decoy", Name: "decoy", GroupKey: "/a", TmuxPane: "%1", Status: "idle", ContextPct: -1}, sessions[2]}},
		{"pane gone", []store.Session{{ID: "a1", Name: "one", GroupKey: "/a", Status: "idle", ContextPct: -1}, sessions[2]}},
	} {
		src := &mutSrc{ss: sessions}
		r := &fakeRunner{}
		m := New(src, r)
		m.getenv = func(string) string { return "" }
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
		m, _ = press(t, tm.(Model), "i") // locked on a1 / %1
		src.ss = c.after
		r.calls = nil
		raw := m.View()
		focusEsc, _, _ := strings.Cut(fg(cFocus, "╭"), "╭")
		if strings.Contains(ansi.Strip(raw), "keys go to the pane") || strings.Contains(raw, focusEsc+"╭") {
			t.Errorf("%s: the screen still shows pass-through (footer or pink preview border)", c.name)
		}
		// a burst arrives as several messages ("rm", " ", "-rf", " ", "build"): all swallowed
		var quit bool
		for _, k := range []tea.KeyMsg{key("rm"), {Type: tea.KeySpace, Runes: []rune(" ")}, key("-rf"), {Type: tea.KeySpace, Runes: []rune(" ")}, key("build"), key("x"), key("y"), key("q")} {
			tm, cmd := m.Update(k)
			m = tm.(Model)
			if q := run(&m, cmd); q {
				quit = true
			}
		}
		for _, call := range r.calls {
			if call[1] == "send-keys" || call[1] == "paste-buffer" {
				t.Errorf("%s: typed into a pane: %q", c.name, call)
			}
		}
		r.mu.Lock()
		if n := len(r.calls); n != 0 && r.calls[0][0] != "tmux" {
			t.Errorf("%s: board commands ran while locked out: %q", c.name, r.calls)
		}
		for _, call := range r.calls {
			if call[0] != "tmux" || !isCapture(call) {
				t.Errorf("%s: only captures may run while locked out, got %q", c.name, call)
			}
		}
		r.mu.Unlock()
		if quit || !strings.Contains(ansi.Strip(m.View()), "session is gone · tab back to the cards") {
			t.Errorf("%s: locked out with a red footer (quit=%v)", c.name, quit)
		}
		if m, _ = press(t, m, "tab"); m.pvFocus {
			t.Errorf("%s: tab takes the keys back", c.name)
		}
		if _, quit = press(t, m, "q"); !quit {
			t.Errorf("%s: then the board has the keys again", c.name)
		}
	}
}

// esc also leaves the locked-out state (but is forwarded while the target is alive).
func TestLockedOutEsc(t *testing.T) {
	src := &mutSrc{ss: sessions}
	m := New(src, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m, _ = press(t, tm.(Model), "i")
	src.ss = sessions[1:]
	if m, _ = press(t, m, "esc"); m.pvFocus {
		t.Error("esc leaves the locked-out state")
	}
}

// The preview shows whoever has the focus: once the focus is off the session pass-through was
// entered on, keys go nowhere (locked out) until tab / esc.
func TestForwardGoesToLockedPane(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "i") // locked on a1 / %1
	m.focus = "b1"                          // however the focus moved
	r.calls = nil
	m, _ = press(t, m, "x")
	if m.passThrough() || !m.pvFocus || len(r.calls) != 0 || m.mode != browsing {
		t.Errorf("locked out: passThrough %v pvFocus %v calls %q", m.passThrough(), m.pvFocus, r.calls)
	}
}

// sentKeys lists the panes keys and pastes went to.
func sentKeys(r *fakeRunner) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if c[1] == "send-keys" || c[1] == "paste-buffer" {
			out = append(out, c[len(c)-2]+" "+c[len(c)-1])
		}
	}
	return out
}

// The target misses one poll (ccmux restarting returns nothing): the focus falls back to the first
// card. When it is back, the preview shows that first card, so keys must not go to the target.
func TestLockedOutAfterTargetReappears(t *testing.T) {
	all := append([]store.Session(nil), sessions...)
	src := &mutSrc{ss: all}
	r := &fakeRunner{}
	m := New(src, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m, _ = press(t, tm.(Model), "l", "i") // locked on b1 / %3
	m.capturing = false
	src.ss = all[:2]
	tm, _ = m.Update(frameMsg{})
	m = tm.(Model)
	src.ss = all
	r.reset() // a frame's capture may still be running on the key queue
	m, _ = press(t, m, "hi")
	v := m.View()
	if m.focus == m.pvID || m.passThrough() || len(sentKeys(r)) != 0 || !strings.Contains(ansi.Strip(v), "session is gone") {
		t.Errorf("focus %s pvID %s, keys → %q", m.focus, m.pvID, sentKeys(r))
	}
	if m, _ = press(t, m, "tab"); m.pvFocus {
		t.Error("tab takes the keys back")
	}
}

// The target moves into a folded group (same pane, still alive): the focus leaves it, keys stop.
func TestLockedOutWhenTargetFolds(t *testing.T) {
	all := append([]store.Session(nil), sessions...)
	src := &mutSrc{ss: all}
	r := &fakeRunner{}
	m := New(src, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m, _ = press(t, tm.(Model), "f", "l", "i") // fold /a, lock on b1
	moved := append([]store.Session(nil), all...)
	moved[2].GroupKey = "/a"
	src.ss = moved
	m.capturing = false
	tm, _ = m.Update(frameMsg{})
	m = tm.(Model)
	r.reset() // a frame's capture may still be running on the key queue
	m, _ = press(t, m, "hi")
	if m.passThrough() || len(sentKeys(r)) != 0 {
		t.Errorf("focus %q, keys → %q", m.focus, sentKeys(r))
	}
}

// An empty paste is nothing: no tmux at all (load-buffer with no input keeps the old buffer).
func TestEmptyPasteIgnored(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "i")
	r.reset() // a frame's capture may still be running on the key queue
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true})
	if m = tm.(Model); cmd != nil || len(r.calls) != 0 || strings.Contains(ansi.Strip(m.View()), "buffer") {
		t.Errorf("calls %q", r.calls)
	}
}

// A failed paste-buffer deletes the buffer: -d only deletes after pasting, and the text would
// otherwise wait there for the next paste into another pane.
func TestFailedPasteDeletesBuffer(t *testing.T) {
	r := &fakeRunner{tmuxFail: map[string]error{"paste-buffer": errors.New("can't find pane: %1")}}
	m, _ := press(t, wide(r, 232, 50), "i")
	r.reset() // a frame's capture may still be running on the key queue
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("secret token"), Paste: true})
	tm, _ = tm.(Model).Update(cmd())
	want := [][]string{
		{"tmux", "load-buffer", "-b", "agmux", "-", "<stdin:secret token>"},
		{"tmux", "paste-buffer", "-p", "-d", "-b", "agmux", "-t", "%1"},
		{"tmux", "delete-buffer", "-b", "agmux"},
	}
	if !reflect.DeepEqual(r.calls, want) || !strings.Contains(ansi.Strip(tm.(Model).View()), "can't find pane: %1") {
		t.Errorf("calls\n %q\nwant\n %q", r.calls, want)
	}
}

// The terminal shrinks under 100 columns: the preview closes and pass-through ends, saying why.
func TestNarrowEndsPassThrough(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "i")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 50})
	m = tm.(Model)
	v := ansi.Strip(m.View())
	if m.pvFocus || !strings.Contains(v, "preview closed: terminal too narrow") || strings.Contains(v, "session is gone") {
		t.Errorf("pvFocus %v\n%s", m.pvFocus, v)
	}
	r.reset() // a frame's capture may still be running on the key queue
	if m, _ = press(t, m, "x"); m.mode != confirmKill || len(r.calls) != 0 {
		t.Error("keys are the board's again")
	}
}

// A poll drops the focused card: the focus falls back and the new card starts at the bottom.
func TestScrollResetsWhenPollMovesFocus(t *testing.T) {
	all := append([]store.Session(nil), sessions...)
	src := &mutSrc{ss: all}
	r := &fakeRunner{}
	m := New(src, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m, _ = press(t, tm.(Model), "p", "l") // preview, focus b1
	m.capturing, m.scroll = false, 9
	src.ss = all[:2]
	r.reset() // a frame's capture may still be running on the key queue
	tm, cmd := m.Update(frameMsg{})
	m = tm.(Model)
	runAll(cmd)
	if m.scroll != 0 || !slices.ContainsFunc(r.calls, func(c []string) bool { return isCapture(c) && c[len(c)-1] == "-44" }) {
		t.Errorf("scroll %d, calls %q", m.scroll, r.calls)
	}
}

// The preview key is p now; P does nothing.
func TestCapitalPIsNotPreview(t *testing.T) {
	if m, _ := press(t, wide(&fakeRunner{}, 232, 50), "P"); m.preview {
		t.Error("P opened the preview")
	}
}

func TestSendKeysEscaping(t *testing.T) {
	for _, c := range []struct {
		k    tea.KeyMsg
		want []string
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x;;")}, []string{"-l", "--", "x", `\;`, `\;`}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";")}, []string{"-l", "--", `\;`}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a;b")}, []string{"-l", "--", "a;b"}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(";"), Alt: true}, []string{`M-\;`}},
		{tea.KeyMsg{Type: tea.KeyCtrlAt}, []string{"C-Space"}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@"), Alt: true}, []string{"M-@"}},
	} {
		if got, want := sendKeysArgs("%1", c.k), append([]string{"send-keys", "-t", "%1"}, c.want...); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q, want %q", c.k.String(), got, want)
		}
	}
	for _, k := range []tea.KeyType{tea.KeyF13, tea.KeyF20} {
		if got := sendKeysArgs("%1", tea.KeyMsg{Type: k}); got != nil {
			t.Errorf("tmux has no %s: %q", tea.KeyMsg{Type: k}.String(), got)
		}
	}
}

// A paste goes in as one bracketed paste through a tmux buffer, not line by line.
func TestPastePassThrough(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "i")
	r.calls = nil
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line 1\rline 2;"), Paste: true})
	cmd()
	want := [][]string{
		{"tmux", "load-buffer", "-b", "agmux", "-", "<stdin:line 1\rline 2;>"},
		{"tmux", "paste-buffer", "-p", "-d", "-b", "agmux", "-t", "%1"},
		captureCall("%1", 44),
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("paste:\n %q\nwant\n %q", r.calls, want)
	}
}

func TestPassThroughFailuresAndMouse(t *testing.T) {
	// a key that fails to reach the pane says so on the message line
	r := &fakeRunner{tmuxFail: map[string]error{"send-keys": errors.New("tmux send-keys: can't find pane %1")}}
	m, _ := press(t, wide(r, 232, 50), "i")
	tm, cmd := m.Update(key("x"))
	tm, _ = tm.(Model).Update(cmd())
	if m = tm.(Model); !strings.Contains(ansi.Strip(m.msg), "can't find pane %1") {
		t.Error("a failed forward shows on the message line")
	}
	// pvFocus left set while the preview is closed: no pass-through footer
	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "i")
	m.preview = false
	if strings.Contains(ansi.Strip(m.View()), "keys go to the pane") {
		t.Error("no pass-through footer with the preview closed")
	}
	// the preview title row is not the box; a session without a pane can't be entered by click
	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "p")
	click := func(m Model, x, y int) Model {
		tm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		return tm.(Model)
	}
	if click(m, 200, 1).pvFocus {
		t.Error("a click on the preview's title row doesn't enter pass-through")
	}
	if !click(m, 200, 10).pvFocus {
		t.Error("a click in the box does")
	}
	if mm, _ := press(t, m, "j"); click(mm, 200, 10).pvFocus {
		t.Error("no pane (a2): a click in the preview does nothing")
	}
	// while the kill prompt is open, the mouse does nothing
	mm, _ := press(t, m, "x")
	mm.pvLines = 100
	if got := click(mm, 200, 10); got.pvFocus {
		t.Error("click ignored during the kill prompt")
	}
	tm, _ = mm.Update(tea.MouseMsg{X: 200, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if tm.(Model).scroll != 0 {
		t.Error("wheel ignored during the kill prompt")
	}
}

// The wheel stops at the oldest captured line and a new card starts at the bottom.
func TestScrollLimit(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "p")
	var hist strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&hist, "line %d\n", i)
	}
	tm, _ := m.Update(previewMsg{pane: "%1", text: hist.String()})
	m = tm.(Model)
	for range 40 {
		m.capturing = true // keep the wheel from capturing; only the bound matters here
		tm, _ = m.Update(tea.MouseMsg{X: 200, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
		m = tm.(Model)
	}
	if m.scroll != 100-44 {
		t.Errorf("scroll %d, want 56 (100 captured − 44 visible)", m.scroll)
	}
	if m, _ = press(t, m, "l"); m.scroll != 0 {
		t.Errorf("a new card starts at the bottom: scroll %d", m.scroll)
	}
}

// A capture queued before a key comes back before that key's own capture.
func TestCaptureQueuedWithKeys(t *testing.T) {
	for range 20 {
		r := &fakeRunner{}
		m, _ := press(t, wide(r, 232, 50), "i")
		m.capturing = false
		r.mu.Lock()
		r.calls = nil
		r.mu.Unlock()
		tm, frame := m.Update(frameMsg{})
		m = tm.(Model)
		_, fwd := m.Update(key("x"))
		var wg sync.WaitGroup
		for _, c := range []tea.Cmd{fwd, frame} {
			wg.Add(1)
			go func(c tea.Cmd) { defer wg.Done(); runAll(c) }(c)
		}
		wg.Wait()
		var order []string
		for _, c := range r.calls {
			order = append(order, c[1])
		}
		if want := []string{"display-message", "send-keys", "display-message"}; !reflect.DeepEqual(order, want) {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// Review leftovers: p captures on its own, s opens at exactly 100 columns, a scrolled preview on a
// tiny terminal doesn't crash.
func TestPreviewOpenEdges(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "l") // focus resolved first, so p's own capture is what counts
	if _, cmd := m.Update(key("p")); cmd == nil {
		t.Error("p captures right away")
	}
	if m, _ := press(t, wide(&fakeRunner{}, 100, 40), "i"); !m.previewOn() || !m.pvFocus {
		t.Error("s opens the preview at exactly 100 columns")
	}
	for h := 5; h >= 1; h-- {
		m, _ := press(t, wide(&fakeRunner{}, 120, 20), "p")
		m.pv, m.scroll = previewMsg{pane: "%1", text: "a\nb\nc\nd\ne\nf\n"}, 3
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: h})
		_ = tm.(Model).View()
	}
}

func withConfig(t *testing.T, toml string) Model {
	t.Helper()
	c, err := config.Parse("cfg", []byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	m := New(sessions, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	m.cfg, m.keymap = c, c.KeyMap()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	return tm.(Model)
}

// Key bindings come from the config: n moves down instead of j, and the preview only keeps
// focus_preview's key for itself.
func TestConfiguredKeys(t *testing.T) {
	m, _ := press(t, withConfig(t, "[keys]\ndown = [\"n\"]\n"), "j")
	if m.focus != "a1" {
		t.Errorf("j unbound: focus %s, want a1", m.focus)
	}
	if m, _ = press(t, m, "n"); m.focus != "a2" {
		t.Errorf("n moves down: focus %s", m.focus)
	}

	r := &fakeRunner{}
	m = withConfig(t, "[keys]\nfocus_preview = [\"ctrl+o\"]\nleave_preview = [\"ctrl+g\"]\n")
	m.run = r
	m, _ = press(t, m, "p", "tab")
	if m.pvFocus {
		t.Fatal("tab no longer enters the preview")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if m = tm.(Model); !m.pvFocus {
		t.Fatal("ctrl+o focuses the preview")
	}
	if !strings.Contains(ansi.Strip(m.View()), "ctrl+g back to the cards") {
		t.Error("the pass-through footer names the configured leave key")
	}
	r.calls = nil
	m, _ = press(t, m, "tab")
	if !m.pvFocus || len(r.calls) == 0 || r.calls[0][len(r.calls[0])-1] != "Tab" {
		t.Errorf("tab is just a key for the pane: %q", r.calls)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if m = tm.(Model); !m.pvFocus {
		t.Error("ctrl+] is no longer the leave key: it goes to the pane")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if tm.(Model).pvFocus {
		t.Error("ctrl+g gives the keys back")
	}

	r = &fakeRunner{}
	m = withConfig(t, "[keys]\nconfirm = [\"Y\"]\nquit = [\"Q\"]\n")
	m.run = r
	m, _ = press(t, m, "x")
	if !strings.Contains(ansi.Strip(m.View()), "Y to confirm") {
		t.Error("the kill prompt names the configured confirm key")
	}
	m, quit := press(t, m, "y", "q")
	if len(r.calls) != 0 || quit {
		t.Errorf("y no longer confirms, q no longer quits: %q quit=%v", r.calls, quit)
	}
	if _, quit = press(t, m, "Q"); !quit {
		t.Error("Q quits")
	}
}

// Colors come from the config: borders, the context bar, the eyes, the footer.
func TestConfiguredColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	c, err := config.Parse("cfg", []byte("[keys]\ndown = [\"n\"]\n[colors]\nworking = \"#123456\"\nbar_empty = \"#010203\"\nattention = \"#0a0b0c\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	useConfig(c)
	defer useConfig(config.Default())
	s := store.Session{Status: "working", AvatarID: "bot-01", AgentType: "claude", ContextPct: -1}
	if cd := card(s, 48, 5, false, 0, now, hit{}); !strings.HasPrefix(cd, "\x1b[38;2;18;52;86m╭") {
		t.Errorf("working border from the config: %q", cd[:30])
	}
	if !strings.Contains(contextBar(-1), "38;2;1;2;3m■") {
		t.Error("empty context cells from the config")
	}
	sp, _ := avatar.ByID("bot-01")
	if !strings.Contains(avatar.Render(sp, "claude", avatar.Alert, 0), ";2;10;11;12") {
		t.Error("the eyes blink in the configured attention color")
	}
	if f := ansi.Strip(footer(false, 300)); !strings.HasPrefix(f, " n/k h/l move · enter jump") {
		t.Errorf("footer from the key bindings: %q", f)
	}
}

// a: the avatar grid under the focused card; h/j/k/l move, enter binds by name (ID without one) into
// avatars.json and the card shows it at once, esc / q cancel without writing; nothing else acts.
func TestAvatarPicker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "avatars.json")
	b, _ := avatar.LoadBindings(path)
	p := &store.Poller{StateDir: dir, Avatars: b, AvatarDefault: "bot-04", Show: func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a1", Summary: "one", CWD: "/a", TmuxPane: "%1", Status: "idle"}, {ID: "a2", CWD: "/a", Status: "idle"}}, nil
	}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	av := func(id string) string {
		for _, s := range p.Sessions() {
			if s.ID == id {
				return s.AvatarID
			}
		}
		return ""
	}
	r := &fakeRunner{}
	m := New(p, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m, _ = press(t, tm.(Model), "j", "a") // a1 "one" (a2 has no name and sorts first)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if top, _, _ := gridAt(lines); top < 0 {
		t.Fatalf("grid not open:\n%s", ansi.Strip(m.View()))
	}
	for _, sp := range avatar.Sprites { // the five sprites in one row, by name
		found := false
		for _, l := range lines {
			found = found || strings.Contains(l, sp.Name)
		}
		if !found {
			t.Errorf("%s missing from the grid", sp.Name)
		}
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	if v := m.View(); !strings.Contains(v, "48;2;255;142;190m   Boo   ") || strings.Count(v, "48;2;255;142;190m") != 1 { // the cursor starts on the sprite the card wears
		t.Error("the cursor is on Boo, only there")
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "h/j/k/l move · enter bind · q cancel") {
		t.Errorf("footer %q", last)
	}
	m, quit := press(t, m, "x", "f", "p", "i", "H", "r", "d", "h", "h", "enter") // bot-04 → bot-02
	if quit || m.mode != browsing || m.preview || m.folded["/a"] || len(r.calls) != 0 {
		t.Fatalf("keys acted while the list was open: mode %v preview %v folded %v calls %q", m.mode, m.preview, m.folded, r.calls)
	}
	if !strings.Contains(ansi.Strip(m.View()), "one wears Boxy") {
		t.Error("the bind is confirmed on the message line")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"one": "bot-02"`) {
		t.Errorf("avatars.json: %s", data)
	}
	if top, _, _ := gridAt(strings.Split(ansi.Strip(m.View()), "\n")); top >= 0 || av("a1") != "bot-02" {
		t.Errorf("list closed and card shows bot-02: %v", p.Sessions())
	}
	if m, _ = press(t, m, "a", "l", "l", "enter"); av("a1") != "bot-04" {
		t.Error("l l enter: two along from the current one")
	}
	for _, cancel := range []string{"esc", "q"} {
		m, quit = press(t, m, "a", "l", cancel)
		if quit || m.mode != browsing || av("a1") != "bot-04" {
			t.Errorf("%s cancels, keeps the board: quit %v mode %v", cancel, quit, m.mode)
		}
	}
	if m, _ = press(t, m, "a", "h", "h", "h", "h", "h", "enter"); av("a1") != "bot-01" {
		t.Error("h stops at the first sprite")
	}
	if m, _ = press(t, m, "a", "l", "l", "l", "l", "l", "l", "l", "enter"); av("a1") != "bot-05" {
		t.Error("l stops at the last sprite")
	}
	m, _ = press(t, m, "k", "a", "l", "enter") // a2: no name
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"a2": "bot-05"`) || !strings.Contains(ansi.Strip(m.View()), "bound to session id (name missing)") {
		t.Errorf("no name: by ID, said so: %s", data)
	}
	// pass-through: a goes to the pane
	r.calls = nil
	m, _ = press(t, m, "j", "i", "a")
	if m.mode != browsing || !slices.ContainsFunc(r.calls, func(c []string) bool {
		return reflect.DeepEqual(c, []string{"tmux", "send-keys", "-t", "%1", "-l", "--", "a"})
	}) {
		t.Errorf("a in pass-through: mode %v calls %q", m.mode, r.calls)
	}
}

// The box sits centered, the screen still showing left and right of it.
func TestOverlay(t *testing.T) {
	got := ansi.Strip(overlay("abcdefghij\n0123456789\nklmnopqrst", []string{"XX", "YY"}, 10))
	if got != "abcdXXghij\n0123YY6789\nklmnopqrst" {
		t.Errorf("%q", got)
	}
	if got := ansi.Strip(overlay("a\nb\nc\nd\ne", []string{"X"}, 1)); got != "a\nb\nX\nd\ne" {
		t.Errorf("centered vertically: %q", got)
	}
	// a wide character cut in half by the box: a space for the half that shows, the line keeps its width
	if got := ansi.Strip(put("a你cd", 0, "XY")); got != "XY cd" {
		t.Errorf("right cut: %q", got)
	}
	if got := ansi.Strip(put("ab", 1, "XYZ")); got != "aXYZ" {
		t.Errorf("a line shorter than the box: nothing after it: %q", got)
	}
	if got := ansi.Strip(put("你cd", 1, "X")); got != " Xcd" {
		t.Errorf("left cut: %q", got)
	}
	if got := ansi.Strip(overlay("abc", []string{"XXXXX"}, 3)); got != "XXX" {
		t.Errorf("a box wider than the screen is cut: %q", got)
	}
	if !strings.Contains(ansi.Strip(footer(false, 300)), "a avatar · q quit") {
		t.Errorf("footer: %s", ansi.Strip(footer(false, 300)))
	}
}

type failAvatarSrc struct{ staticSrc }

func (failAvatarSrc) SetAvatar(store.Session, string) error {
	return errors.New("avatars.json: not valid JSON\nsecond line")
}

// A binding that can't be written says why, on one line; the list closes.
func TestAvatarBindError(t *testing.T) {
	m := New(failAvatarSrc{sessions}, &fakeRunner{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m, _ = press(t, tm.(Model), "a", "enter")
	if v := ansi.Strip(m.View()); m.mode != browsing || !strings.Contains(v, "avatars.json: not valid JSON") || strings.Contains(v, "second line") || strings.Contains(v, "wears") {
		t.Errorf("mode %v\n%s", m.mode, v)
	}
}

// Every [colors] entry reaches the element design.md names for it.
func TestEveryColorWired(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	toml := "[colors]\n"
	sgr := map[string]string{}
	for i, n := range config.Colors {
		if n == "border" { // set, it replaces the status colors on borders: TestBorderColor
			continue
		}
		toml += fmt.Sprintf("%s = \"#0000%02x\"\n", n, i+1)
		sgr[n] = fmt.Sprintf("38;2;0;0;%dm", i+1)
	}
	c, err := config.Parse("cfg", []byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	useConfig(c)
	defer useConfig(config.Default())
	has := func(what, s, color string, after ...string) {
		t.Helper()
		if !strings.Contains(s, sgr[color]+strings.Join(after, "")) {
			t.Errorf("%s: no %s color in %q", what, color, s)
		}
	}
	idle := store.Session{Name: "nm", Branch: "br", Model: "claude-opus-5-5", Status: "idle", AvatarID: "bot-01", AgentType: "claude", ContextPct: -1, TmuxPane: "%1"}
	has("idle border", card(idle, 48, 5, false, 0, now, hit{}), "idle")
	has("focused border", card(idle, 48, 5, true, 0, now, hit{}), "focus")
	has("idle word", card(idle, 48, 5, false, 0, now, hit{}), "muted")
	has("name", card(idle, 48, 5, false, 0, now, hit{}), "name", "nm")
	has("branch", card(idle, 48, 5, false, 0, now, hit{}), "branch", "\ue725 br")
	has("model", card(idle, 48, 5, false, 0, now, hit{}), "model")
	withEffort := idle
	withEffort.Effort = "high"
	has("effort", card(withEffort, 48, 5, false, 0, now, hit{}), "effort", "high")
	done := idle
	done.Done = true
	has("done border", card(done, 48, 5, false, 0, now, hit{}), "done")
	working := idle
	working.Status = "working"
	has("working word", card(working, 48, 5, false, 0, now, hit{}), "working", "⏵ working")
	waiting := idle
	waiting.Status = "waiting"
	has("waiting word", card(waiting, 48, 5, false, 0, now, hit{}), "attention", "⏳ waiting")
	if !strings.Contains(matched("x"), strings.Replace(sgr["match"], "38;", "48;", 1)) {
		t.Errorf("match: %q", matched("x"))
	}
	has("bar low", contextBar(10), "bar_low")
	has("bar mid", contextBar(40), "bar_mid")
	has("bar high", contextBar(70), "bar_high")
	has("bar empty", contextBar(10), "bar_empty")
	has("disabled footer keys", footer(true, 300), "disabled")
	has("group title", header(group{key: "/x", sessions: []store.Session{idle}}, 40, false, false, nil), "dir")
	has("preview box", strings.Join(previewLines(idle, true, previewMsg{}, 60, 10, 0, false), "\n"), "idle")
}

// confirm and leave_preview may share keys with the board: each acts only in its own mode.
func TestModalKeysShareBoardKeys(t *testing.T) {
	m := withConfig(t, "[keys]\nconfirm = [\"enter\"]\nleave_preview = [\"tab\"]\n")
	r := m.run.(*fakeRunner)
	m, _ = press(t, m, "x", "enter")
	if len(r.calls) == 0 || r.calls[len(r.calls)-1][0] != "kill" {
		t.Errorf("enter confirms the kill: %q", r.calls)
	}
	m, _ = press(t, m, "p", "tab")
	if !m.pvFocus {
		t.Fatal("tab still enters the preview")
	}
	if m, _ = press(t, m, "tab"); m.pvFocus {
		t.Error("and in pass-through tab leaves it")
	}
}

func typeKeys(t *testing.T, m Model, text string) Model {
	t.Helper()
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}) // one burst, as typed fast
	return tm.(Model)
}

// /: the cards filter as the query is typed; keys go into the box (j, q are text); the top bar
// shows the query and the count; enter keeps the filter, focus on the first card shown; esc drops it.
func TestSearchFilter(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "j") // focus: two
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "thq")
	m, _ = press(t, m, "backspace", "r")
	v := ansi.Strip(m.View())
	lines := strings.Split(v, "\n")
	if m.query != "thr" || !strings.HasPrefix(lines[0], " / thr▏") || !strings.HasSuffix(lines[0], "1/3 ") {
		t.Fatalf("query %q, top %q", m.query, lines[0])
	}
	if strings.Contains(v, "one") || strings.Contains(v, "two") || !strings.Contains(v, "three") || strings.Contains(v, "/a (") {
		t.Errorf("only three and its group show:\n%s", v)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "enter keep the filter · esc clear it") {
		t.Errorf("footer %q", last)
	}
	m, quit := press(t, m, "enter")
	if quit || m.searching || m.query != "thr" || m.focus != "b1" || len(r.calls) != 0 {
		t.Fatalf("enter: searching %v query %q focus %s calls %q", m.searching, m.query, m.focus, r.calls)
	}
	if top := strings.Split(ansi.Strip(m.View()), "\n")[0]; strings.Contains(top, "▏") || !strings.Contains(top, "/ thr") {
		t.Errorf("kept filter, box closed: %q", top)
	}
	m, _ = press(t, m, "/", "esc")
	if m.query != "" || m.searching || m.focus != "b1" || !strings.Contains(ansi.Strip(m.View()), "one") {
		t.Errorf("esc: query %q focus %s", m.query, m.focus)
	}
	m, _ = press(t, m, "h", "/") // focus one, from there
	m = typeKeys(t, m, "zzz")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no match") || !strings.Contains(v, "0/3") {
		t.Errorf("no match:\n%s", v)
	}
	m, quit = press(t, m, "ctrl+c")
	if quit || m.query != "" || m.searching || m.focus != "a1" {
		t.Errorf("ctrl+c in the box clears, focus back on one: quit %v focus %s", quit, m.focus)
	}
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "ab")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if m = tm.(Model); m.query != "a" {
		t.Errorf("ctrl+h is backspace: %q", m.query)
	}
	if !strings.Contains(ansi.Strip(footer(false, 300)), "enter jump · / search · s flash · f fold") {
		t.Error("footer: … enter jump · / search · s flash · f fold …")
	}
}

// Where a card matched: rune offsets split into name, branch and the ~-path of its cwd.
func TestSearchHits(t *testing.T) {
	home, _ := os.UserHomeDir()
	src := staticSrc{{ID: "x", Name: "ab", Branch: "cd", CWD: home + "/ef", GroupKey: home + "/ef"}}
	for q, want := range map[string]hit{"'ab": {name: []int{0, 1}}, "'cd": {branch: []int{0, 1}}, "'ef": {dir: []int{2, 3}}} {
		m := New(src, &fakeRunner{})
		m.query = q
		if _, hits := m.shown(); !reflect.DeepEqual(hits["x"], want) {
			t.Errorf("%q: %+v, want %+v", q, hits["x"], want)
		}
	}
}

// Highlights color whole grapheme clusters, so wide and combined characters keep their width.
func TestHilite(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	got := hilite("éx你", []int{1, 3}, cName) // the combining accent, and 你
	if want := matched("é") + fg(cName, "x") + matched("你"); got != want {
		t.Errorf("%q\nwant %q", got, want)
	}
	if hilite("ab", nil, cName) != fg(cName, "ab") {
		t.Error("no hits: plain")
	}
	g := group{key: "/proj/agmux", sessions: []store.Session{{ID: "x"}}}
	if h := header(g, 40, false, false, dirHits(g, map[string]hit{"x": {dir: []int{6}}})); !strings.Contains(h, matched("a")) {
		t.Errorf("the group title shows its cards' path hits: %q", h)
	}
}

// A click lands on the card shown there under the filter, not the one there without it.
func TestSearchMouse(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "/")
	m = typeKeys(t, m, "tw")
	m, _ = press(t, m, "enter") // two, alone in /a: where one sits unfiltered
	tm, cmd := m.Update(tea.MouseMsg{X: 3, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = tm.(Model)
	run(&m, cmd)
	if m.focus != "a2" {
		t.Errorf("clicked %s", m.focus)
	}
}

// esc puts the focus back on the card it was on, even after the filter hid it and the focus fell back.
func TestSearchEscRestoresFocus(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "j", "p", "/") // two, preview open
	m = typeKeys(t, m, "thr")
	m.capturing = false
	tm, _ := m.Update(frameMsg{}) // the frame resolves the focus: two is hidden, three it is
	if m = tm.(Model); m.focus != "b1" {
		t.Fatalf("focus %s", m.focus)
	}
	if m, _ = press(t, m, "esc"); m.focus != "a2" {
		t.Errorf("esc: focus %s, want two", m.focus)
	}
}

func TestBranchHilite(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	if l := line2(store.Session{Branch: "cd"}, 36, []int{1}); !strings.Contains(l, matched("d")) || strings.Contains(l, matched("c")) {
		t.Errorf("%q", l)
	}
}

// With a filter kept, esc drops it first; the next esc quits.
func TestEscClearsKeptFilter(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "/")
	m = typeKeys(t, m, "thr")
	m, quit := press(t, m, "enter", "esc")
	if quit || m.query != "" {
		t.Fatalf("quit %v query %q", quit, m.query)
	}
	if _, quit = press(t, m, "esc"); !quit {
		t.Error("then esc quits")
	}
}

// a on a folded title: nothing to bind, says so.
func TestAvatarOnFoldedTitle(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "f", "a")
	if m.mode != browsing || !strings.Contains(ansi.Strip(m.View()), "fold a group? pick a card first") {
		t.Errorf("mode %v", m.mode)
	}
}

// An action the config left with no key is not in the footer.
func TestFooterSkipsKeylessAction(t *testing.T) {
	c, _ := config.Parse("cfg", []byte("[keys]\nfold = [\"a\"]\n"))
	f := ""
	for _, k := range footerFor(c) {
		f += k.text + " · "
	}
	if strings.Contains(f, "avatar") || !strings.Contains(f, "a fold") || strings.Contains(f, " avatar") {
		t.Errorf("%s", f)
	}
}

// The picker's footer offers only the keys there are.
func TestPickerFooterSkipsKeyless(t *testing.T) {
	m := withConfig(t, "[keys]\nfold = [\"enter\"]\n") // jump is left with no key
	m, _ = press(t, m, "a")
	v := strings.Split(ansi.Strip(m.View()), "\n")
	if last := v[len(v)-1]; last != " h/j/k/l move · q cancel" {
		t.Errorf("footer %q", last)
	}
	m, _ = press(t, withConfig(t, "[keys]\nfold = [\"k\", \"up\"]\n"), "a") // up: no key, so no "move"
	if v := strings.Split(ansi.Strip(m.View()), "\n"); v[len(v)-1] != " enter bind · q cancel" {
		t.Errorf("footer %q", v[len(v)-1])
	}
}

func psized(src Source, w, h int) Model {
	m := New(src, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return tm.(Model)
}

func column(n, h int) Model {
	var ss []store.Session
	for i := range n {
		ss = append(ss, store.Session{ID: fmt.Sprintf("x%02d", i), Name: fmt.Sprintf("n%02d", i), GroupKey: "/g", TmuxPane: "%1", Status: "idle", ContextPct: -1})
	}
	return psized(staticSrc(ss), 60, h)
}

// The label: focus color behind bold text, dark on a light focus color, light on a dark one.
func TestFlashLabelStyle(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	if l := flashLabel("s"); !strings.Contains(l, "48;2;255;142;190") || !strings.Contains(l, "38;2;40;42;54") || !strings.Contains(l, "\x1b[1;") {
		t.Errorf("label style %q", l)
	}
	c, _ := config.Parse("cfg", []byte("[colors]\nfocus = \"#202040\"\n"))
	useConfig(c)
	defer useConfig(config.Default())
	if l := flashLabel("s"); !strings.Contains(l, "48;2;32;32;64") || !strings.Contains(l, "38;2;248;248;242") {
		t.Errorf("dark focus color: light text %q", l)
	}
}

// esc clears a kept filter whatever quit is bound to; alt+esc (two quick escs) doesn't.
func TestEscClearsFilterAnyBinding(t *testing.T) {
	m := withConfig(t, "[keys]\nquit = [\"q\"]\n")
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "thr")
	m, _ = press(t, m, "enter")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	if m = tm.(Model); m.query != "thr" {
		t.Error("alt+esc leaves the filter")
	}
	if m, quit := press(t, m, "esc"); quit || m.query != "" {
		t.Errorf("esc clears: quit %v query %q", quit, m.query)
	}
}

func trueColor(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

// With nothing matching, / still opens the box (it is how the query is fixed); enter changes nothing.
func TestSearchAfterNoMatch(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "l", "/") // focus three
	m = typeKeys(t, m, "zzz")
	m, _ = press(t, m, "enter")
	if m.focus != "b1" || m.searching {
		t.Errorf("enter with no match: focus %s searching %v", m.focus, m.searching)
	}
	if m, _ = press(t, m, "/"); !m.searching {
		t.Fatal("/ is dead after no match")
	}
	m, _ = press(t, m, "backspace", "backspace", "backspace")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "3/3") {
		t.Errorf("query emptied:\n%s", v)
	}
}

// The box takes text only: a paste's control characters turn into spaces, alt+key is not text.
func TestSearchBoxTextOnly(t *testing.T) {
	for paste, want := range map[string]string{"a\rb": "a b", "a\tb": "a b", "a\x1b[31mb": "a [31mb", "a\nb": "a b", "a\x00b": "a b"} {
		m, _ := press(t, wide(&fakeRunner{}, 200, 40), "/")
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste), Paste: true})
		m = tm.(Model)
		if m.query != want || strings.ContainsAny(ansi.Strip(strings.SplitN(m.View(), "\n", 2)[0]), "\r\t\x1b\x00") {
			t.Errorf("paste %q: query %q, want %q", paste, m.query, want)
		}
	}
	m, _ := press(t, wide(&fakeRunner{}, 200, 40), "/")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e"), Alt: true})
	if m = tm.(Model); m.query != "" {
		t.Errorf("alt+e typed %q", m.query)
	}
}

// Clicking the preview while the box is open ends the box: the keys go to the pane, and the top
// bar stops showing a query being typed.
func TestSearchBoxClosesOnPassThrough(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "p", "/")
	m = typeKeys(t, m, "o")
	tm, _ := m.Update(tea.MouseMsg{X: 150, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = tm.(Model)
	if !m.pvFocus || m.searching || strings.Contains(strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0], "▏") {
		t.Fatalf("pvFocus %v searching %v", m.pvFocus, m.searching)
	}
	r.reset()
	m, _ = press(t, m, "enter")
	if sent := sentKeys(r); len(sent) != 1 || !strings.HasSuffix(sent[0], "Enter") {
		t.Errorf("enter goes to the pane, not the box: %q", sent)
	}
}

// enter lands on the best match, wherever the layout puts it and wherever the focus is.
func TestSearchEnterGoesToBestScore(t *testing.T) {
	ss := staticSrc{
		{ID: "a1", Name: "x-o-r-c-h", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%1"},    // weak
		{ID: "b1", Name: "orchestrator", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%2"}, // best, later in the layout
		{ID: "a2", Name: "zz-orch", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%3"},
	}
	for _, focusOn := range []string{"a1", "a2", "b1"} {
		m := psized(ss, 232, 50)
		m.focus = focusOn
		m, _ = press(t, m, "/")
		m = typeKeys(t, m, "orch")
		if m, _ = press(t, m, "enter"); m.focus != "b1" {
			t.Errorf("focus began on %s: enter went to %s, want the best match b1", focusOn, m.focus)
		}
	}
	// the cards are not reordered by score
	m := psized(ss, 232, 50)
	m.query = "orch"
	gs := groups(func() []store.Session { s, _ := m.shown(); return s }())
	if gs[0].key != "/a" || gs[0].sessions[0].ID != "a1" {
		t.Errorf("layout keeps its order: %v", gs)
	}
}

// Under a filter every group with a hit shows its cards; clearing it brings the folding back; the
// count is what is visible.
func TestSearchUnfoldsAndRestores(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "f") // /a folded: its title only
	if v := ansi.Strip(m.View()); strings.Contains(v, "one") || !strings.Contains(v, "▸ /a (2)") {
		t.Fatalf("setup:\n%s", v)
	}
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "o") // one, two (hit in a), no three?
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "one") || !strings.Contains(v, "▾ /a") || !strings.Contains(strings.SplitN(v, "\n", 2)[0], "2/3") {
		t.Errorf("the group with hits opens, count n/total:\n%s", v)
	}
	m, _ = press(t, m, "esc")
	if v := ansi.Strip(m.View()); strings.Contains(v, "one") || !strings.Contains(v, "▸ /a (2)") || !m.folded["/a"] {
		t.Errorf("folded again after esc:\n%s", v)
	}
}

// The card pass-through is locked on keeps showing while it no longer matches.
func TestSearchKeepsLockedCard(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "i") // locked on a1 "one"
	m.query = "thr"
	if ss, _ := m.shown(); len(ss) != 2 || ss[1].ID != "a1" {
		t.Errorf("shown %v", ss)
	}
	m.pvFocus = false
	if ss, _ := m.shown(); len(ss) != 1 {
		t.Errorf("not locked: only the match: %v", ss)
	}
}

// Esc on a kept filter leaves the focus on the card shown, not on one the filter had hidden.
func TestSearchEscKeepsCurrentCard(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "j", "/") // focus two
	m = typeKeys(t, m, "thr")
	m, _ = press(t, m, "enter") // three
	m.focus = "a2"              // as if a poll had moved the focus to a hidden card
	if m, _ = press(t, m, "esc"); m.query != "" || m.focus != "b1" {
		t.Errorf("query %q focus %s, want b1: the card that was showing", m.query, m.focus)
	}
}

// The matches reach the screen: the name's letters, the path in the group's title (the union over
// its cards, folded or not).
func TestSearchHighlightsOnScreen(t *testing.T) {
	trueColor(t)
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "/")
	m = typeKeys(t, m, "'thr")
	if !strings.Contains(m.View(), matched("t")+matched("h")+matched("r")+fg(cName, "e")) {
		t.Errorf("name letters not highlighted:\n%s", ansi.Strip(m.View()))
	}
	home, _ := os.UserHomeDir()
	ss := staticSrc{
		{ID: "a", Name: "agm-card", GroupKey: home + "/proj/agmux", CWD: home + "/proj/agmux", ContextPct: -1},
		{ID: "b", Name: "zzz", GroupKey: home + "/proj/agmux", CWD: home + "/proj/agmux", ContextPct: -1},
		{ID: "c", Name: "two", GroupKey: "/zzz/other", CWD: "/zzz/other", ContextPct: -1},
	}
	mm := psized(ss, 200, 40)
	mm.query = "'agmux"
	title := strings.Split(mm.View(), "\n")[1]
	if !strings.Contains(title, matched("a")+matched("g")+matched("m")+matched("u")+matched("x")) {
		t.Errorf("open title: %q", ansi.Strip(title))
	}
	if g := (group{key: home + "/proj/agmux", sessions: []store.Session{{ID: "b"}}}); true { // b has no hit but a does: the union
		h := header(g, 60, false, false, dirHits(group{sessions: ss[:2]}, map[string]hit{"a": {dir: []int{7, 8}}}))
		if !strings.Contains(h, matched("g")) {
			t.Errorf("union of the cards' hits: %q", ansi.Strip(h))
		}
	}
	mm.folded[home+"/proj/agmux"] = true
	mm.query = ""
	mm.focus = "c"
	if h := header(group{key: home + "/proj/agmux", sessions: ss[:2]}, 60, true, false, []int{7}); !strings.Contains(h, matched("a")) {
		t.Errorf("folded title: %q", ansi.Strip(h))
	}
}

// Offsets are runes, not bytes, with multi-byte names and branches.
func TestSearchHitsMultibyte(t *testing.T) {
	m := New(staticSrc{{ID: "x", Name: "名字名字", Branch: "分支", CWD: "/w/目录", GroupKey: "/w/目录"}}, &fakeRunner{})
	for q, want := range map[string]hit{"'分支": {branch: []int{0, 1}}, "'名字名字": {name: []int{0, 1, 2, 3}}, "'目录": {dir: []int{3, 4}}} {
		m.query = q
		if _, hits := m.shown(); !reflect.DeepEqual(hits["x"], want) {
			t.Errorf("%q: %+v, want %+v", q, hits["x"], want)
		}
	}
}

// A title too long for its column keeps both ends, and a highlight in the tail survives the cut.
func TestMidTruncKeepsHighlight(t *testing.T) {
	trueColor(t)
	path := "/proj/aaaaaaaaaaaa/bbbbbbbbbbbb/cccccccccccc/target-dir"
	r := []rune(path)
	g := group{key: path, sessions: []store.Session{{ID: "x"}}}
	h := header(g, 30, false, false, []int{len(r) - 3, len(r) - 2})
	if w := ansi.StringWidth(h); w != 30 || !strings.Contains(h, matched("d")+matched("i")) {
		t.Errorf("width %d: %q", w, ansi.Strip(h))
	}
	if got := midTrunc("abcdef", 2); got != "ab" || ansi.StringWidth(midTrunc("abcdefgh", 5)) != 5 {
		t.Errorf("fallback: %q %q", got, midTrunc("abcdefgh", 5))
	}
}

// spec: in flash, mouse clicks and the wheel are swallowed (cards and preview alike)
func TestFlashSwallowsMouse(t *testing.T) {
	m := wide(&fakeRunner{}, 232, 50)
	m, _ = press(t, m, "tab", "tab") // preview open, keys back on the board
	m.pvLines, m.capturing = 100, false
	m, _ = press(t, m, "s")
	before := m
	for _, ev := range []tea.MouseMsg{
		{X: 60, Y: 8, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},      // another card
		{X: 3, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},       // the focused card (second click would jump)
		{X: 200, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},    // preview box: would lock the pane
		{X: 200, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}, // preview wheel: would scroll
	} {
		tm, cmd := m.Update(ev)
		m = tm.(Model)
		if m.mode != flashing || m.focus != before.focus || m.pvFocus || m.scroll != 0 || cmd != nil {
			t.Errorf("%v: mode %v focus %q pvFocus %v scroll %d cmd %v", ev, m.mode, m.focus, m.pvFocus, m.scroll, cmd != nil)
		}
	}
}

// Equal scores: enter goes to the first of them in the layout, not in the poll's order.
func TestSearchEnterTieLayoutOrder(t *testing.T) {
	ss := staticSrc{
		{ID: "b1", Name: "x1", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%1"},
		{ID: "a1", Name: "x2", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%2"},
	}
	m, _ := press(t, psized(ss, 232, 50), "/")
	m = typeKeys(t, m, "x")
	if m, _ = press(t, m, "enter"); m.focus != "a1" {
		t.Errorf("tie: enter went to %s, the layout's first is a1", m.focus)
	}
}

// Clearing the filter while the focus is on a card of a group f folded: the focus goes to that
// group's title, not to the first item of the board. By esc on a kept filter and by esc in the box.
func TestSearchClearIntoFoldedGroup(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "l", "f") // /b folded, focus on its title
	if m.focus != headerID("/b") {
		t.Fatalf("setup focus %q", m.focus)
	}
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "thr")
	if m, _ = press(t, m, "enter"); m.focus != "b1" { // /b is open under the filter
		t.Fatalf("enter focus %q", m.focus)
	}
	if m, _ = press(t, m, "esc"); m.focus != headerID("/b") || m.query != "" {
		t.Errorf("kept-filter esc: focus %q", m.focus)
	}
	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "l", "f", "/")
	m = typeKeys(t, m, "thr")
	m, _ = press(t, m, "enter", "/", "esc") // the box again, on the kept filter: back to where / was pressed
	if m.focus != headerID("/b") {
		t.Errorf("box esc: focus %q", m.focus)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▸ /b") {
		t.Errorf("and it shows:\n%s", v)
	}
}

// f under a filter does nothing but say why: every group is open there, and the hidden fold state
// must not flip.
func TestSearchFoldRefused(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "/")
	m = typeKeys(t, m, "o")
	m, _ = press(t, m, "enter", "f")
	if m.folded["/a"] || !strings.Contains(ansi.Strip(m.View()), "clear the filter to fold") {
		t.Errorf("folded %v\n%s", m.folded, ansi.Strip(m.View()))
	}
	if m, _ = press(t, m, "esc"); m.folded["/a"] || !strings.Contains(ansi.Strip(m.View()), "one") {
		t.Error("after esc /a is as it was: open")
	}
	m, _ = press(t, m, "f") // no filter: folds
	if !m.folded["/a"] {
		t.Error("f folds without a filter")
	}
}

// Invisible format characters (U+202E, zero-width joiners…) typed or pasted never reach the query.
func TestSearchBoxDropsFormatChars(t *testing.T) {
	m, _ := press(t, wide(&fakeRunner{}, 200, 40), "/")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a‮b​c"), Paste: true})
	if m = tm.(Model); m.query != "a b c" {
		t.Errorf("query %q", m.query)
	}
}

// /a folded by f, then a filter hitting both its cards: they are open, so the focus, j/k, a click
// and s all reach them (items, current, the direction keys, flash and mouse all use the filter's folding).
func TestSearchOpensFoldedForEverything(t *testing.T) {
	setup := func() Model {
		m, _ := press(t, wide(&fakeRunner{}, 232, 50), "f", "/") // /a folded
		m = typeKeys(t, m, "o")                                  // one, two
		m, _ = press(t, m, "enter")
		return m
	}
	m := setup()
	if _, _, s, ok := m.current(); !ok || s.ID != "a1" {
		t.Errorf("enter: focus %q ok %v", m.focus, ok)
	}
	if m, _ = press(t, setup(), "j"); m.focus != "a2" {
		t.Errorf("j: focus %q", m.focus)
	}
	m = setup()
	ss, _ := m.shown()
	_, rects, off := view(ss, m.w, m.h, m.focus, m.frame, m.now(), "", false, nil, m.cardsW(), nil, nil, -1)
	clicked := false
	for _, r := range rects {
		if r.id == "a2" {
			tm, _ := m.Update(tea.MouseMsg{X: r.x + 3, Y: 1 + r.y - off + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			clicked = tm.(Model).focus == "a2"
		}
	}
	if !clicked {
		t.Error("click on a2 did not focus it")
	}
	if m, _ = press(t, setup(), "s", "t", "w"); len(m.flash.ms) != 1 || m.flash.ms[0].id != "a2" { // two, in the group f had folded
		t.Errorf("s: hits %v", m.flash.ms)
	}
}

var tieSrc = staticSrc{
	{ID: "b1", Name: "x1", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%1"},
	{ID: "a1", Name: "x2", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%2"},
	{ID: "a2", Name: "x3", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%3"},
}

// enter with no query (never typed, or backspaced to nothing) only closes the box.
func TestSearchEmptyEnterKeepsFocus(t *testing.T) {
	m, _ := press(t, psized(tieSrc, 232, 50), "j") // a2
	if m, _ = press(t, m, "/", "enter"); m.focus != "a2" || m.searching {
		t.Errorf("/ enter: focus %q searching %v", m.focus, m.searching)
	}
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "x")
	if m, _ = press(t, m, "backspace", "enter"); m.focus != "a2" {
		t.Errorf("backspaced to empty, enter: focus %q", m.focus)
	}
}

// A kept filter that matches nothing, then esc: the focus is where it was before.
func TestSearchNoMatchEscKeepsFocus(t *testing.T) {
	m, _ := press(t, psized(tieSrc, 232, 50), "j", "/") // a2
	m = typeKeys(t, m, "qqq")
	if m, _ = press(t, m, "enter", "esc"); m.focus != "a2" || m.query != "" {
		t.Errorf("focus %q query %q", m.focus, m.query)
	}
}

// leave_preview can be put back on ctrl+] (the old default): then tab is just a key for the pane.
func TestLeavePreviewRebound(t *testing.T) {
	r := &fakeRunner{}
	m := withConfig(t, "[keys]\nleave_preview = [\"ctrl+]\"]\n")
	m.run = r
	m, _ = press(t, m, "tab")
	if !m.pvFocus || !strings.Contains(ansi.Strip(m.View()), "ctrl+] back to the cards") {
		t.Fatalf("tab goes in; footer names ctrl+]")
	}
	r.reset()
	if m, _ = press(t, m, "tab"); !m.pvFocus || !slices.ContainsFunc(r.calls, func(c []string) bool { return c[len(c)-1] == "Tab" }) {
		t.Errorf("tab goes to the pane: %q", r.calls)
	}
	if m, _ = press(t, m, "ctrl+]"); m.pvFocus || !m.previewOn() {
		t.Error("ctrl+] leaves, the preview stays")
	}
}

// Search hits: the match color behind dark text (like nvim's Search), the color is configurable.
func TestMatchStyle(t *testing.T) {
	trueColor(t)
	if s := matched("x"); !strings.Contains(s, "48;2;242;208;107") || !strings.Contains(s, "38;2;44;42;46") { // lipgloss rounds #2d2a2e
		t.Errorf("default: %q", s)
	}
	c, _ := config.Parse("cfg", []byte("[colors]\nmatch = \"#00ff00\"\n"))
	useConfig(c)
	defer useConfig(config.Default())
	if s := matched("x"); !strings.Contains(s, "48;2;0;255;0") || !strings.Contains(s, "38;2;44;42;46") {
		t.Errorf("configured: %q", s)
	}
	if h := hilite("abc", []int{1}, cName); !strings.Contains(h, matched("b")) || strings.Contains(h, matched("a")) {
		t.Errorf("only the hit: %q", h)
	}
}

var matchSrc = staticSrc{
	{ID: "a1", Name: "x1", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%1"},
	{ID: "a2", Name: "x2", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%2"},
	{ID: "b1", Name: "x3", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%3"},
	{ID: "b2", Name: "other", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%4"},
}

// A kept filter: n / N walk its cards in layout order and wrap; with no filter they do nothing;
// the footer says so.
func TestNextPrevMatch(t *testing.T) {
	m, _ := press(t, psized(matchSrc, 232, 50), "n", "N") // no filter: nothing
	if m.focus != "" && m.focus != "a1" {
		t.Errorf("n without a filter moved the focus: %q", m.focus)
	}
	m, _ = press(t, m, "/")
	m = typeKeys(t, m, "x")
	m, _ = press(t, m, "enter") // a1
	foot := func() string { v := strings.Split(ansi.Strip(m.View()), "\n"); return v[len(v)-1] }
	if f := foot(); !strings.HasPrefix(f, " n/N next/prev · esc clear · ") || !strings.Contains(f, "enter jump") {
		t.Errorf("footer %q", f)
	}
	var got []string
	for _, k := range []string{"n", "n", "n", "N", "N", "N", "N"} {
		m, _ = press(t, m, k)
		got = append(got, m.focus)
	}
	if want := []string{"a2", "b1", "a1", "b1", "a2", "a1", "b1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("walk %v, want %v", got, want)
	}
	m, _ = press(t, m, "esc", "n")
	if m.query != "" || m.focus != "b1" {
		t.Errorf("after esc n does nothing: focus %q", m.focus)
	}
	if f := foot(); strings.Contains(f, "next/prev") {
		t.Errorf("footer after esc %q", f)
	}
}

// n / N are text in the box, keys for the pane in pass-through; from a card the filter hides they
// go on from the card the screen shows the focus on (the first shown), as j / k do.
func TestNextPrevMatchEdges(t *testing.T) {
	m, _ := press(t, psized(matchSrc, 232, 50), "/")
	if m = typeKeys(t, m, "nN"); m.query != "nN" {
		t.Errorf("in the box: %q", m.query)
	}
	m, _ = press(t, psized(matchSrc, 232, 50), "j", "j", "j", "/") // focus b2 "other"
	m = typeKeys(t, m, "x")
	m, _ = press(t, m, "enter")                   // a1 (enter goes to the first)
	m.focus = "b2"                                // hidden by the filter
	if m, _ = press(t, m, "n"); m.focus != "a2" { // the screen shows a1: the next one
		t.Errorf("n from a hidden card: %q", m.focus)
	}
	m.focus = "b2"
	if m, _ = press(t, m, "N"); m.focus != "b1" { // the screen shows a1: back, around to the last
		t.Errorf("N from a hidden card: %q", m.focus)
	}
	r := &fakeRunner{}
	m, _ = press(t, wide(r, 232, 50), "tab") // pass-through
	r.reset()
	m, _ = press(t, m, "n")
	if !slices.ContainsFunc(r.calls, func(c []string) bool { return c[len(c)-1] == "n" && c[1] == "send-keys" }) {
		t.Errorf("n goes to the pane: %q", r.calls)
	}
}

// ---- s: flash.nvim style: type what to find, press the label after the hit ----

// flashSrc: the names for flash tests. Groups /g (alpha, alps), /h (Bravo), /i (a CJK name).
var flashSrc = staticSrc{
	{ID: "a1", Name: "alpha", Branch: "main", GroupKey: "/g", CWD: "/g", ContextPct: -1, TmuxPane: "%1"},
	{ID: "a2", Name: "alps", GroupKey: "/g", CWD: "/g", ContextPct: -1, TmuxPane: "%2"},
	{ID: "b1", Name: "Bravo", Branch: "feature/x", GroupKey: "/h", CWD: "/h", ContextPct: -1, TmuxPane: "%3"},
	{ID: "c1", Name: "告警会话", Branch: "main", GroupKey: "/i", CWD: "/i", ContextPct: -1, TmuxPane: "%4"},
}

func hitOf(m Model, id string) (flashMatch, bool) {
	for _, h := range m.flash.ms {
		if h.id == id {
			return h, true
		}
	}
	return flashMatch{}, false
}

func labelsOf(m Model) string {
	var out []string
	for _, h := range m.flash.ms {
		out = append(out, h.id+"="+h.label)
	}
	return strings.Join(out, " ")
}

// typed presses the keys one by one in flash mode.
func typed(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m, _ = press(t, m, string(r))
	}
	return m
}

// onScreen checks every hit against the board as it is drawn outside flash: the cells it names
// hold its text (case aside), so the columns agree with how the cards are laid out.
func onScreen(t *testing.T, m Model) {
	t.Helper()
	plain := m
	plain.mode = browsing
	lines := strings.Split(ansi.Strip(plain.View()), "\n")
	for _, h := range m.flash.ms {
		if got := ansi.Cut(lines[1+h.y], h.x, h.x+h.w); !strings.EqualFold(got, h.text) {
			t.Errorf("hit %+v: the screen has %q at line %d column %d", h, got, 1+h.y, h.x)
		}
	}
}

// s alone labels nothing; each typed character narrows to the cards whose name, branch or
// group path hold it (case does not matter), and the pattern and footer say what is going on.
func TestFlashTypeToNarrow(t *testing.T) {
	m, _ := press(t, psized(flashSrc, 232, 50), "s")
	if m.mode != flashing || len(m.flash.ms) != 0 {
		t.Fatalf("s: mode %v hits %v", m.mode, m.flash.ms)
	}
	v := strings.Split(ansi.Strip(m.View()), "\n")
	if f := v[len(v)-1]; f != " flash: type to narrow · label to jump · esc" || v[len(v)-2] != " s ▏" {
		t.Errorf("footer %q msg %q", f, v[len(v)-2])
	}
	m = typed(t, m, "bR")
	if h, ok := hitOf(m, "b1"); !ok || h.text != "Br" {
		t.Errorf("bR (any case) finds Bravo, shown as it is: %v", labelsOf(m))
	}
	if v := strings.Split(ansi.Strip(m.View()), "\n"); v[len(v)-2] != " s bR▏" {
		t.Errorf("the pattern as typed: %q", v[len(v)-2])
	}
	if m = typed(t, m, "zz"); len(m.flash.ms) != 0 || m.mode != flashing {
		t.Errorf("no hit: no labels, still flashing: %v", labelsOf(m))
	}
	m, _ = press(t, m, "backspace", "backspace", "backspace")
	if h, ok := hitOf(m, "b1"); !ok || h.text != "B" || m.flash.pat != "b" {
		t.Errorf("back to b: %q %v", m.flash.pat, labelsOf(m))
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if m = tm.(Model); m.flash.pat != "" || len(m.flash.ms) != 0 {
		t.Errorf("ctrl+h is a backspace: %q", m.flash.pat)
	}
	if m, _ = press(t, m, "backspace"); m.flash.pat != "" || m.mode != flashing {
		t.Error("backspace on nothing")
	}
	// a burst, as tmux send-keys or a fast typist makes it
	tm, _ = psized(flashSrc, 232, 50).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("slp")})
	if m = tm.(Model); m.mode != flashing || m.flash.pat != "lp" || len(m.flash.ms) != 2 {
		t.Errorf("burst slp: mode %v pattern %q hits %v", m.mode, m.flash.pat, labelsOf(m))
	}
}

// Where it finds: a card's name, its branch (past the icon), a group's path; the first hit per
// card; wide characters in cells. Every hit is checked against the cells it names.
func TestFlashWhereItFinds(t *testing.T) {
	for _, c := range []struct {
		typed, id, text string
		w               int
	}{
		{"lph", "a1", "lph", 3},   // a name
		{"feat", "b1", "feat", 4}, // a branch
		{"警会", "c1", "警会", 4},     // two wide characters: four cells
		{"/h", "b1", "/h", 2},     // a group's path; an open group goes to its first card
		{"MAIN", "a1", "main", 4}, // a branch, case aside
	} {
		m := typed(t, psized(flashSrc, 232, 50), "s"+c.typed)
		h, ok := hitOf(m, c.id)
		if !ok || h.text != c.text || h.w != c.w {
			t.Errorf("%q: %+v ok %v", c.typed, h, ok)
		}
		onScreen(t, m)
	}
	m := typed(t, psized(flashSrc, 232, 50), "sa") // alpha holds a in its name and its branch: one hit
	n := 0
	for _, h := range m.flash.ms {
		if h.id == "a1" {
			n++
		}
	}
	if n != 1 || len(m.flash.ms) != 4 { // a1, a2, b1 (Bravo), c1 (its branch "main")
		t.Errorf("a: %d hits on a1, all %v", n, labelsOf(m))
	}
}

func namesSrc(names ...string) staticSrc {
	var ss staticSrc
	for i, n := range names {
		ss = append(ss, store.Session{ID: fmt.Sprintf("n%02d", i), Name: n, GroupKey: "/g", CWD: "/g", ContextPct: -1, TmuxPane: "%1"})
	}
	return ss
}

// A label is never the character after a hit (any hit, any case): typing on would collide with it.
// Names "xa" "xs" "xd" "xF": a, s, d, f are all taken as followers, so the labels start at g.
func TestFlashLabelsAvoidFollowers(t *testing.T) {
	m := typed(t, psized(namesSrc("xa", "xs", "xd", "xF"), 232, 50), "sx")
	got := ""
	for _, h := range m.flash.ms {
		got += h.label
	}
	if got != "ghjk" {
		t.Errorf("labels %q, want ghjk (a s d f are the next characters)", got)
	}
	// the spec's case: "alpha" and "alps", typing "al": the next character is p in both
	m = typed(t, psized(namesSrc("alpha", "alps", "alpine"), 232, 50), "sal")
	for _, h := range m.flash.ms {
		if h.label == "p" || h.label == "P" {
			t.Errorf("label %q is the character after the hit", h.label)
		}
	}
	// every letter follows some hit: no letters left, no labels, nothing to press
	var names []string
	for _, r := range "abcdefghijklmnopqrstuvwxyz" {
		names = append(names, "q"+string(r))
	}
	if m = typed(t, psized(namesSrc(names...), 232, 140), "sq"); len(m.flash.ms) != 26 || labelsOf(m) == "" {
		t.Fatalf("hits %d", len(m.flash.ms))
	}
	for _, h := range m.flash.ms {
		if h.label != "" {
			t.Errorf("a label %q although every letter follows a hit", h.label)
		}
	}
	// only the hits that exist count: followers of hits not drawn (scrolled away) don't use letters up — and the
	// tail of a hit that ends its text has no follower at all
	m = typed(t, psized(namesSrc("ab", "b"), 232, 50), "sb")
	if h, _ := hitOf(m, "n01"); h.label == "" {
		t.Errorf("a hit at the end of its text: %v", labelsOf(m))
	}
}

// Nearest the focused card first: its own hit gets "a" when it has no follower claim; the rest by
// distance, rows weighing a whole width, ties up before down.
func TestFlashLabelOrder(t *testing.T) {
	m, _ := press(t, psized(namesSrc("n1", "n2", "n3", "n4"), 232, 50), "j", "j") // the third card
	if m.focus != "n02" {
		t.Fatalf("focus %s", m.focus)
	}
	m = typed(t, m, "sn")
	var order []string
	for _, h := range m.flash.ms {
		order = append(order, h.id)
	}
	if want := []string{"n02", "n01", "n03", "n00"}; !reflect.DeepEqual(order, want) { // itself, then one above (tie → up), one below, two above
		t.Errorf("by distance from n02: %v, want %v", order, want)
	}
}

// A label moves the focus to its card and ends flash (no jump, no tmux); another character is the
// pattern; non-text keys are ignored.
func TestFlashLabelJumps(t *testing.T) {
	r := &fakeRunner{}
	m := New(flashSrc, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m = typed(t, tm.(Model), "sa") // alpha, alps, Bravo, 告警会话 (its branch main)
	m, _ = press(t, m, "enter", "tab", "down", "ctrl+]", "backspace", "backspace")
	if m.mode != flashing || m.flash.pat != "" {
		t.Fatalf("non-text keys ignored (backspaces emptied the pattern): mode %v pattern %q", m.mode, m.flash.pat)
	}
	m = typed(t, m, "br")
	h, _ := hitOf(m, "b1")
	if h.label == "" {
		t.Fatalf("no label: %v", labelsOf(m))
	}
	m, quit := press(t, m, h.label)
	if quit || m.mode != browsing || m.focus != "b1" || len(r.calls) != 0 {
		t.Errorf("mode %v focus %s calls %q", m.mode, m.focus, r.calls)
	}
	if m, _ = press(t, m, "h"); m.focus == "b1" { // browsing again: keys are the board's
		t.Error("h moves again")
	}
}

// esc ends flash with the focus where it was.
func TestFlashEsc(t *testing.T) {
	m, _ := press(t, psized(flashSrc, 232, 50), "l")
	was := m.focus
	m = typed(t, m, "sal")
	if m, _ = press(t, m, "esc"); m.mode != browsing || m.focus != was {
		t.Errorf("mode %v focus %s, was %s", m.mode, m.focus, was)
	}
}

// The labels are the ones drawn at the last key; cards gone since are no target (ignored, still
// flashing), and a poll adding cards does not move a label to another card.
func TestFlashFrozenAndGone(t *testing.T) {
	src := &mutSrc{ss: slices.Clone([]store.Session(flashSrc))}
	m := typed(t, psized(src, 232, 50), "sb")
	h, _ := hitOf(m, "b1")
	label := h.label
	src.ss = append([]store.Session{{ID: "a0", Name: "bbb", GroupKey: "/f", CWD: "/f", ContextPct: -1}}, src.ss...)
	_ = m.View()
	if m, _ = press(t, m, label); m.focus != "b1" || m.mode != browsing {
		t.Errorf("label %q was Bravo's when drawn: focus %s", label, m.focus)
	}
	m = typed(t, psized(src, 232, 50), "sb")
	h, _ = hitOf(m, "b1")
	src.ss = slices.DeleteFunc(slices.Clone(src.ss), func(s store.Session) bool { return s.ID == "b1" })
	before := m.focus
	if m, _ = press(t, m, h.label); m.mode != flashing || m.focus != before || m.flash.pat != "b" {
		t.Errorf("Bravo left: ignored, still flashing, pattern unchanged; mode %v focus %s pattern %q", m.mode, m.focus, m.flash.pat)
	}
}

// Under a kept filter only the cards shown take part; the group the filter opened is searched too.
func TestFlashUnderFilter(t *testing.T) {
	m, _ := press(t, psized(flashSrc, 232, 50), "f", "/") // /g folded
	m = typeKeys(t, m, "a")                               // alpha, alps, Bravo (a), 告警会话 (main)
	m, _ = press(t, m, "enter")
	m = typed(t, m, "sbr")
	if len(m.flash.ms) != 1 || m.flash.ms[0].id != "b1" {
		t.Errorf("hits %v", labelsOf(m))
	}
	m, _ = press(t, m, "backspace", "backspace")
	m = typed(t, m, "lp")
	if _, ok := hitOf(m, "a1"); !ok {
		t.Errorf("alpha, in the group the filter opened: %v", labelsOf(m))
	}
	m, _ = press(t, psized(flashSrc, 232, 50), "f") // no filter: /g folded, its title is a target
	m = typed(t, m, "s/g")
	h, ok := hitOf(m, headerID("/g"))
	if !ok {
		t.Fatalf("a folded title is a target: %v", labelsOf(m))
	}
	if m, _ = press(t, m, h.label); m.focus != headerID("/g") {
		t.Errorf("focus %q", m.focus)
	}
}

// Only what is in the window: a card whose top border is on screen.
func TestFlashOnlyOnScreen(t *testing.T) {
	m := typed(t, column(10, 20), "s")
	m = typed(t, m, "n")
	if len(m.flash.ms) == 0 || len(m.flash.ms) >= 10 {
		t.Fatalf("hits %v", labelsOf(m))
	}
	onScreen(t, m)
	end := typed(t, mustPress(t, column(10, 20), "j", "j", "j", "j", "j", "j", "j", "j", "j"), "sn")
	if _, ok := hitOf(end, "x00"); ok {
		t.Errorf("scrolled away: %v", labelsOf(end))
	}
	if _, ok := hitOf(end, "x09"); !ok {
		t.Errorf("the focused one: %v", labelsOf(end))
	}
	onScreen(t, end)
}

func mustPress(t *testing.T, m Model, keys ...string) Model {
	m, _ = press(t, m, keys...)
	return m
}

// Hits are drawn on the cards' rows only: after a poll moves the cards, the drawn hits (frozen) never
// land on the top bar, the message line or the footer, at any height.
func TestFlashDrawSafe(t *testing.T) {
	mk := func(id, name string) store.Session {
		return store.Session{ID: id, Name: name, GroupKey: "/g", TmuxPane: "%1", Status: "idle", ContextPct: -1}
	}
	for h := 8; h <= 34; h += 3 {
		for fi := 0; fi < 10; fi += 3 {
			for _, apply := range []func([]store.Session) []store.Session{
				func(ss []store.Session) []store.Session { return append([]store.Session{mk("y0", "000")}, ss...) },
				func(ss []store.Session) []store.Session { return ss[3:] },
				func(ss []store.Session) []store.Session {
					return append(slices.Clone(ss), mk("y1", "n10"), mk("y2", "n11"))
				},
			} {
				var ss []store.Session
				for i := range 10 {
					ss = append(ss, mk(fmt.Sprintf("x%02d", i), fmt.Sprintf("n%02d", i)))
				}
				src := &mutSrc{ss: slices.Clone(ss)}
				m := psized(src, 60, h)
				for range fi {
					m, _ = press(t, m, "j")
				}
				m = typed(t, m, "sn")
				src.ss = apply(slices.Clone(ss))
				lines := strings.Split(ansi.Strip(m.View()), "\n")
				if len(lines) != h || !strings.HasPrefix(lines[0], " agmux") || lines[h-2] != " s n▏" || !strings.HasPrefix(lines[h-1], " flash:") {
					t.Errorf("h=%d focus %d: %d lines, top %q msg %q foot %q", h, fi, len(lines), lines[0], lines[h-2], lines[h-1])
				}
			}
		}
	}
}

// Rows weigh a whole width: a hit one row down is farther than one in the next column.
func TestFlashDistanceWeighsRows(t *testing.T) {
	ss := staticSrc{
		{ID: "n1", Name: "n1", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%1"},
		{ID: "n2", Name: "n2", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%2"},
		{ID: "n3", Name: "n3", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%3"},
	}
	m := typed(t, psized(ss, 232, 50), "sn")
	var order []string
	for _, h := range m.flash.ms {
		order = append(order, h.id)
	}
	if want := []string{"n1", "n3", "n2"}; !reflect.DeepEqual(order, want) { // n3: next column, same row; n2: one card down
		t.Errorf("order %v, want %v", order, want)
	}
}

// A group's path too long for its column is searched as it is drawn, with its ellipsis.
func TestFlashFindsInTruncatedTitle(t *testing.T) {
	long := "/very/long/path/" + strings.Repeat("a", 60) + "/zzzend"
	ss := staticSrc{{ID: "x", Name: "x", GroupKey: long, CWD: long, ContextPct: -1, TmuxPane: "%1"}}
	m := typed(t, psized(ss, 60, 20), "szzz")
	if h, ok := hitOf(m, "x"); !ok || h.text != "zzz" {
		t.Fatalf("hits %v", labelsOf(m))
	}
	onScreen(t, m)
	// 30 a's are in the path, no more than 19 in a row on screen: the cut-out middle isn't found
	m, _ = press(t, psized(ss, 60, 20), "f") // folded: the title as a card-less line
	if m = typed(t, m, "szzz"); len(m.flash.ms) != 1 || m.flash.ms[0].id != headerID(long) {
		t.Fatalf("folded: %v", labelsOf(m))
	}
	onScreen(t, m)
	m, _ = press(t, psized(ss, 60, 20), "s")
	m.flash.pat = strings.Repeat("a", 25)
	if m.flashScan(); len(m.flash.ms) != 0 {
		t.Errorf("what the middle truncation hid isn't there to find: %v", labelsOf(m))
	}
}

// Only plain characters go onto the pattern: not alt+key, not a paste, not a control character.
func TestFlashOnlyPlainText(t *testing.T) {
	m, _ := press(t, psized(flashSrc, 232, 50), "s")
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("a"), Alt: true},
		{Type: tea.KeyRunes, Runes: []rune("alp"), Paste: true},
		{Type: tea.KeyRunes, Runes: []rune("a"), Paste: true},
		{Type: tea.KeyRunes, Runes: []rune{'\x01'}},
		{Type: tea.KeyRunes, Runes: []rune{'‮'}},
	} {
		tm, _ := m.Update(k)
		if m = tm.(Model); m.flash.pat != "" || m.mode != flashing {
			t.Errorf("%v: pattern %q mode %v", k, m.flash.pat, m.mode)
		}
	}
	if m, _ = press(t, m, " "); m.flash.pat != " " { // a space is text: names have them
		t.Errorf("space: %q", m.flash.pat)
	}
}

// Each s starts clean; esc leaves nothing behind.
func TestFlashStartsClean(t *testing.T) {
	m := typed(t, psized(flashSrc, 232, 50), "sal")
	m.flash.freed = map[string]bool{"a": true}
	m, _ = press(t, m, "esc", "s")
	if m.flash.pat != "" || len(m.flash.ms) != 0 || len(m.flash.freed) != 0 {
		t.Errorf("pattern %q hits %v freed %v", m.flash.pat, labelsOf(m), m.flash.freed)
	}
}

// A letter freed by a hit that went is not given to another in the reads that follow.
func TestFlashFreedLetterNotReused(t *testing.T) {
	m := typed(t, psized(namesSrc("xq"), 232, 50), "sx")
	if got := labelsOf(m); got != "n00=a" {
		t.Fatalf("labels %q", got)
	}
	m.flash.freed = map[string]bool{"a": true, "s": true}
	if got := (&m).flashRead(nil); len(got) != 1 || got[0].label != "d" {
		t.Errorf("a and s are freed: %+v", got)
	}
}

// What is drawn: the hit in the match color on dimmed text, its label (focus color, dark) on the
// next cell; the rest of the board muted.
func TestFlashDraws(t *testing.T) {
	trueColor(t)
	m := typed(t, psized(flashSrc, 232, 50), "sbr")
	h, _ := hitOf(m, "b1")
	v := m.View()
	if !strings.Contains(v, matched("Br")) || !strings.Contains(v, flashLabel(h.label)) {
		t.Errorf("hit and label are drawn:\n%q", strings.Split(v, "\n")[3])
	}
	line := strings.Split(ansi.Strip(v), "\n")[1+h.y]
	if got := ansi.Cut(line, h.x+h.w, h.x+h.w+1); got != h.label {
		t.Errorf("the label sits on the cell after the hit: %q, want %q", got, h.label)
	}
	if strings.Contains(v, fg(cName, "alpha")) {
		t.Error("the rest is dimmed")
	}
}

// Resizing while flashing reads the hits again, so they are on the new window, not outside it.
func TestFlashResize(t *testing.T) {
	m := typed(t, column(10, 34), "sn")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	m = tm.(Model)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if len(lines) != 10 || lines[8] != " s n▏" {
		t.Fatalf("%d lines, msg %q", len(lines), lines[8])
	}
	onScreen(t, m)
	for _, h := range m.flash.ms {
		if h.y < 0 || h.y >= 7 {
			t.Errorf("hit %+v outside the window", h)
		}
	}
}

// A poll makes the focused card stop matching: the screen shows the focus on the first card
// (current()'s fallback); n goes on from there, as j / k do.
func TestNextFromDisplayedFocus(t *testing.T) {
	mk := func(id, name string) store.Session {
		return store.Session{ID: id, Name: name, GroupKey: "/g", CWD: "/g", TmuxPane: "%1", Status: "idle", ContextPct: -1}
	}
	src := &mutSrc{ss: []store.Session{mk("x1", "ab1"), mk("x2", "ab2"), mk("x3", "ab3"), mk("y", "zz")}}
	m, _ := press(t, psized(src, 232, 50), "/")
	m = typeKeys(t, m, "ab")
	m, _ = press(t, m, "enter", "n") // x2
	if m.focus != "x2" {
		t.Fatalf("setup focus %q", m.focus)
	}
	src.ss[1].Name = "zz2" // a poll: x2 no longer matches; the screen puts the focus on x1
	if nm, _ := press(t, m, "n"); nm.focus != "x3" {
		t.Errorf("n: %q, want x3 (on from x1)", nm.focus)
	}
	if nm, _ := press(t, m, "N"); nm.focus != "x3" {
		t.Errorf("N: %q, want x3 (back from x1, around)", nm.focus)
	}
}

// n / N follow the layout, not the match scores.
func TestNextPrevLayoutOrder(t *testing.T) {
	ss := staticSrc{
		{ID: "a1", Name: "x-o-r-c-h", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%1"},
		{ID: "b1", Name: "orchestrator", GroupKey: "/b", CWD: "/b", ContextPct: -1, TmuxPane: "%2"},
		{ID: "a2", Name: "zz-orch", GroupKey: "/a", CWD: "/a", ContextPct: -1, TmuxPane: "%3"},
	}
	m, _ := press(t, psized(ss, 232, 50), "/")
	m = typeKeys(t, m, "orch")
	m, _ = press(t, m, "enter") // b1, the best
	var got []string
	for range 3 {
		m, _ = press(t, m, "n")
		got = append(got, m.focus)
	}
	if want := []string{"a1", "a2", "b1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("n from b1: %v, want layout order %v", got, want)
	}
}

// The footer names the n / N keys there are.
func TestMatchHintNamesWhatThereIs(t *testing.T) {
	for toml, want := range map[string]string{
		"": " n/N next/prev · esc clear · ", "[keys]\nprev_match = [\"ctrl+p\"]\n": " n/ctrl+p next/prev · esc clear · ",
		"[keys]\nnext_match = [\"n\"]\nfold = [\"N\"]\n": " n next · esc clear · ", "[keys]\nfold = [\"n\"]\n": " N prev · esc clear · ",
		"[keys]\nfold = [\"n\", \"N\"]\n": " esc clear · ",
	} {
		m := withConfig(t, toml)
		m.src = psized(sessions, 232, 50).src
		m, _ = press(t, m, "/")
		m = typeKeys(t, m, "o")
		m, _ = press(t, m, "enter")
		v := strings.Split(ansi.Strip(m.View()), "\n")
		if f := v[len(v)-1]; !strings.HasPrefix(f, want) {
			t.Errorf("%q: footer %q, want prefix %q", toml, f, want)
		}
	}
}

// ---- T24: the pane's cursor in the preview ----

// grab reads the cursor and the screen in one call: the info line is not part of the screen, the
// row is counted in the capture (history included), and a hidden cursor is none.
func TestGrabCursor(t *testing.T) {
	screen := strings.Repeat("l\n", 9) + "abcsh-3.2$ \n" // 10 lines: 4 of history, 6 visible
	for _, c := range []struct {
		name, out string
		want      *cursor
		text      string
	}{
		{"block shell prompt", "11 5 1 default 6\n" + screen, &cursor{row: 9, col: 11}, screen},
		{"bar", "3 0 1 bar 6\n" + screen, &cursor{row: 4, col: 3, bar: true}, screen},
		{"underline", "3 0 1 underline 6\n" + screen, &cursor{row: 4, col: 3, bar: true}, screen},
		{"block shape", "3 0 1 block 6\n" + screen, &cursor{row: 4, col: 3}, screen},
		{"hidden (civis, copy-mode)", "3 0 0 block 6\n" + screen, nil, screen},
		{"no history", "2 1 1 default 4\nab\ncd\nef\ngh\n", &cursor{row: 1, col: 2}, "ab\ncd\nef\ngh\n"},
		{"an info line it can't read: no cursor, not screen either", "plain\ncapture\n", nil, "capture\n"},
		{"older tmux without cursor_shape", "3 1 1  2\nabc\ndef\n", nil, "abc\ndef\n"},
		{"empty info line", "\nabc\n", nil, "abc\n"},
		{"numbers that aren't", "x y 1 bar 6\n" + screen, nil, screen},
		{"no height", "1 1 1 bar 0\n" + screen, nil, screen},
	} {
		r := &fakeRunner{tmuxOut: map[string]string{"display-message": c.out}}
		got := grab(r, "%1", 44)
		if !reflect.DeepEqual(got.cur, c.want) || got.text != c.text || got.pane != "%1" || got.err != nil {
			t.Errorf("%s: cursor %+v text %q, want %+v %q", c.name, got.cur, got.text, c.want, c.text)
		}
		if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], captureCall("%1", 44)) {
			t.Errorf("%s: calls %q", c.name, r.calls)
		}
	}
	r := &fakeRunner{tmuxFail: map[string]error{"display-message": errors.New("can't find pane: %9")}}
	if got := grab(r, "%9", 44); got.err == nil || got.cur != nil || got.text != "" {
		t.Errorf("a failed call: %+v", got)
	}
}

// noResets drops the resets put() leaves around what it draws, so the styled text reads in a row.
func noResets(s string) string { return strings.ReplaceAll(s, "\x1b[0m", "") }

func cursorBox(pv previewMsg, scroll int) []string {
	s := store.Session{ID: "a", Name: "n", TmuxPane: "%1", Status: "idle", ContextPct: -1}
	return previewLines(s, true, pv, 40, 12, scroll, false) // 9 rows inside the box, 35 columns wide
}

// The cursor is drawn on its cell: a block reversed (an empty cell too), a bar or underline
// underlined in the focus color; where it is counts rows of the capture, from the bottom of the content.
func TestPreviewDrawsCursor(t *testing.T) {
	trueColor(t)
	text := "one\ntwo\nthree\n\n\n" // content "one two three"; the blank rows are cut
	rev := lipgloss.NewStyle().Reverse(true)
	bar := lipgloss.NewStyle().Foreground(cFocus).Underline(true)
	for _, c := range []struct {
		name string
		cur  cursor
		row  int // box row
		want string
	}{
		{"block on a letter", cursor{row: 2, col: 1}, 2, "t" + rev.Render("h") + "ree"},
		{"block past the text", cursor{row: 2, col: 7}, 2, "three  " + rev.Render(" ")},
		{"block on a blank row below the content", cursor{row: 3, col: 2}, 3, "  " + rev.Render(" ")},
		{"bar", cursor{row: 0, col: 0, bar: true}, 0, bar.Render("o") + "ne"},
	} {
		box := cursorBox(previewMsg{pane: "%1", text: text, cur: &c.cur}, 0)
		line := box[2+c.row] // title, top border, then the rows
		if !strings.Contains(noResets(line), noResets(c.want)) {
			t.Errorf("%s: %q, want it to hold %q", c.name, line, c.want)
		}
		if w := ansi.StringWidth(line); w != 40 {
			t.Errorf("%s: width %d, the box keeps its edges", c.name, w)
		}
	}
	// no cursor drawn: hidden, scrolled back, above the window, off to the right
	plain := cursorBox(previewMsg{pane: "%1", text: text}, 0)
	for name, args := range map[string]struct {
		cur    *cursor
		scroll int
	}{
		"hidden": {nil, 0}, "scrolled": {&cursor{row: 2, col: 1}, 3},
		"above the window": {&cursor{row: -1, col: 1}, 0}, "below it": {&cursor{row: 40, col: 1}, 0},
		"right of the box": {&cursor{row: 2, col: 35}, 0}, "left of it": {&cursor{row: 2, col: -1}, 0},
	} {
		got := cursorBox(previewMsg{pane: "%1", text: text, cur: args.cur}, args.scroll)
		if args.scroll == 0 && !reflect.DeepEqual(got, plain) {
			t.Errorf("%s: drawn anyway", name)
		}
		for _, l := range got {
			if strings.Contains(l, "\x1b[7m") {
				t.Errorf("%s: a reversed cell", name)
			}
		}
	}
}

// A wide character under the cursor is reversed whole, the line keeps its width, and a long
// history shows its last lines: the row is counted from the capture's end.
func TestPreviewCursorWideAndHistory(t *testing.T) {
	trueColor(t)
	rev := lipgloss.NewStyle().Reverse(true)
	box := cursorBox(previewMsg{pane: "%1", text: "你好x\n", cur: &cursor{row: 0, col: 2}}, 0)
	if !strings.Contains(noResets(box[2]), "你"+noResets(rev.Render("好"))+"x") || ansi.StringWidth(box[2]) != 40 {
		t.Errorf("%q", box[2])
	}
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	box = cursorBox(previewMsg{pane: "%1", text: b.String(), cur: &cursor{row: 29, col: 0}}, 0) // 9 rows show lines 21–29
	if !strings.Contains(noResets(box[2+8]), noResets(rev.Render("l"))+"ine29") || strings.Contains(box[2], "\x1b[7m") {
		t.Errorf("last row %q", box[2+8])
	}
}

// Through the board: the cursor of the focused pane shows in the preview, in pass-through too, and
// not while it is scrolled back.
func TestBoardShowsPaneCursor(t *testing.T) {
	trueColor(t)
	r := &fakeRunner{tmuxOut: map[string]string{"display-message": "3 0 1 default 6\nabcdef\n\n\n\n\n\n"}}
	m := wide(r, 232, 50)
	m, _ = press(t, m, "p")
	m.capturing = false
	tm, cmd := m.Update(frameMsg{})
	tm, _ = tm.(Model).Update(cmd().(tea.BatchMsg)[1]())
	m = tm.(Model)
	rev := lipgloss.NewStyle().Reverse(true).Render("d")
	want := noResets("abc" + rev + "ef")
	if !strings.Contains(noResets(m.View()), want) {
		t.Fatalf("browsing: no cursor\n%s", ansi.Strip(m.View()))
	}
	m.pvFocus, m.pvID, m.pvPane = true, "a1", "%1"
	if !strings.Contains(noResets(m.View()), want) {
		t.Error("pass-through: no cursor")
	}
	m.scroll = 3
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Error("scrolled back: the cursor isn't drawn")
	}
}

// T25: [layout] max_columns caps the columns; the minimum column width still decides how many fit.
func TestMaxColumnsLayout(t *testing.T) {
	defer useConfig(config.Default())
	for _, c := range []struct{ max, w, want int }{
		{4, 232, 4}, {5, 232, 5}, {5, 180, 4}, {1, 232, 1}, {8, 232, 5}, {8, 400, 8}, {2, 232, 2},
	} {
		cfg := config.Default()
		cfg.MaxColumns = c.max
		useConfig(cfg)
		if n, _ := columns(c.w); n != c.want {
			t.Errorf("max %d at %d columns: %d, want %d", c.max, c.w, n, c.want)
		}
	}
	// and what is drawn follows it: nine one-card groups in a row of five
	cfg := config.Default()
	cfg.MaxColumns = 5
	useConfig(cfg)
	var ss staticSrc
	for i := range 9 {
		ss = append(ss, store.Session{ID: fmt.Sprintf("g%d", i), Name: fmt.Sprintf("n%d", i), GroupKey: fmt.Sprintf("/g%d", i), ContextPct: -1})
	}
	m := psized(ss, 232, 30)
	if v := strings.Split(ansi.Strip(m.View()), "\n")[1]; strings.Count(v, "▾") != 5 {
		t.Errorf("five groups in the first row: %q", v)
	}
}

// T26: with colors.border set every unfocused card has that border; the focused one keeps the focus
// color; the status line and the avatar's mood still follow the status. Unset, borders follow the status.
func TestBorderColor(t *testing.T) {
	trueColor(t)
	defer useConfig(config.Default())
	mk := func(status string, mod func(*store.Session)) store.Session {
		s := store.Session{ID: "x", Name: "n", Status: status, AvatarID: "bot-01", AgentType: "claude", ContextPct: -1}
		if mod != nil {
			mod(&s)
		}
		return s
	}
	cards := map[string]store.Session{
		"working": mk("working", nil), "idle": mk("idle", nil), "done": mk("idle", func(s *store.Session) { s.Done = true }),
		"alert": mk("idle", func(s *store.Session) { s.Attention = "permission" }), "waiting": mk("waiting", nil),
	}
	statusColor := map[string]lipgloss.Color{"working": cWorking, "idle": cDim, "done": cDone, "alert": cRed, "waiting": cRed}
	sgr := func(c lipgloss.Color) string { start, _, _ := strings.Cut(fg(c, "x"), "x"); return start }
	top := func(name string, focused bool) string {
		first, _, _ := strings.Cut(card(cards[name], 48, 5, focused, 0, now, hit{}), "\n")
		return first
	}
	for name := range cards { // unset: as before
		if got := top(name, false); !strings.HasPrefix(got, sgr(statusColor[name])+"╭") {
			t.Errorf("%s unset: %q", name, got)
		}
	}
	cfg, err := config.Parse("cfg", []byte("[colors]\nborder = \"#123456\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	useConfig(cfg)
	for name := range cards {
		if got := top(name, false); !strings.HasPrefix(got, "\x1b[38;2;18;52;86m╭") {
			t.Errorf("%s: %q", name, got)
		}
		for _, l := range strings.Split(card(cards[name], 48, 5, false, 0, now, hit{}), "\n") {
			if strings.HasPrefix(l, sgr(statusColor[name])+"│") && statusColor[name] != cBorder {
				t.Errorf("%s: a side in the status color: %q", name, l)
			}
		}
		if got := top(name, true); !strings.HasPrefix(got, sgr(cFocus)+"┏") {
			t.Errorf("%s focused: %q", name, got)
		}
	}
	// the status line and the avatar are untouched
	if !strings.Contains(card(cards["working"], 48, 5, false, 0, now, hit{}), fg(cWorking, "⏵ working")) {
		t.Error("the status word keeps its color")
	}
	sp, _ := avatar.ByID("bot-01")
	if face := strings.Split(avatar.Render(sp, "claude", avatar.Alert, 0), "\n"); !strings.Contains(card(cards["alert"], 48, 5, false, 0, now, hit{}), face[0]) {
		t.Error("the alert avatar keeps its mood")
	}
}

// A poll moves the cards under a flash in progress: the hits are drawn where the cards are now, with the
// labels they had, and pressing a label goes to the card it is drawn on.
func TestFlashFollowsPoll(t *testing.T) {
	trueColor(t)
	src := &mutSrc{ss: slices.Clone([]store.Session(flashSrc))}
	m := typed(t, psized(src, 232, 50), "sb")
	h, _ := hitOf(m, "b1")
	label := h.label
	// a card that sorts before Bravo in its group pushes it down five rows
	src.ss = append(src.ss, store.Session{ID: "b0", Name: "Aaa", GroupKey: "/h", CWD: "/h", ContextPct: -1, TmuxPane: "%9"})
	plain := m
	plain.mode = browsing
	pl := strings.Split(ansi.Strip(plain.View()), "\n")
	row := slices.IndexFunc(pl, func(l string) bool { return strings.Contains(l, "Bravo") })
	if row == h.y+1 {
		t.Fatalf("the poll moved nothing")
	}
	// drawn without waiting for a frame: the highlight and label are on Bravo's new row, none on the old
	v := strings.Split(m.View(), "\n")
	at := ansi.StringWidth(pl[row][:strings.Index(pl[row], "Bravo")]) // its column, not its byte
	if !strings.Contains(v[row], matched("B")) || ansi.Cut(ansi.Strip(v[row]), at+1, at+2) != label {
		t.Errorf("Bravo's row %d: %q", row, ansi.Strip(v[row]))
	}
	if strings.Contains(v[1+h.y], matched("B")) || strings.Contains(v[1+h.y], flashLabel(label)) {
		t.Errorf("the old row %d still has them: %q", 1+h.y, ansi.Strip(v[1+h.y]))
	}
	// the next frame keeps what was read, and the label still names Bravo
	tm, _ := m.Update(frameMsg{})
	m = tm.(Model)
	onScreen(t, m)
	if h2, ok := hitOf(m, "b1"); !ok || h2.label != label || h2.y == h.y {
		t.Errorf("after the frame: %+v (was %+v)", h2, h)
	}
	if m, _ = press(t, m, label); m.mode != browsing || m.focus != "b1" {
		t.Errorf("the label goes to the card it is drawn on: focus %s mode %v", m.focus, m.mode)
	}
}

// A label stays with its card unless a poll brings a card whose next character is that very letter:
// then it is given another, and no two hits share one.
func TestFlashLabelsFollowCards(t *testing.T) {
	src := &mutSrc{ss: []store.Session(namesSrc("xq", "xw"))}
	m := typed(t, psized(src, 232, 50), "sx")
	if got := labelsOf(m); got != "n00=a n01=s" {
		t.Fatalf("labels %q", got)
	}
	src.ss = append(slices.Clone(src.ss), store.Session{ID: "n02", Name: "xa", GroupKey: "/g", CWD: "/g", ContextPct: -1, TmuxPane: "%1"})
	tm, _ := m.Update(frameMsg{})
	m = tm.(Model)
	seen := map[string]bool{}
	for _, h := range m.flash.ms {
		if h.label == "a" || h.label == "q" || h.label == "w" || seen[h.label] || h.label == "" {
			t.Errorf("label %q: %s", h.label, labelsOf(m))
		}
		seen[h.label] = true
	}
	if h, _ := hitOf(m, "n01"); h.label != "s" { // untouched: it keeps its label
		t.Errorf("n01 lost its label: %s", labelsOf(m))
	}
}

// Real cards carry a time at the right of the name line, which narrows the name: a character cut off by
// that must not be found.
func TestFlashNameCutByTime(t *testing.T) {
	m0 := psized(staticSrc{{ID: "x", Name: "a", GroupKey: "/g", CWD: "/g", ContextPct: -1}}, 60, 30)
	now := m0.now()
	act := now.Add(-3 * time.Hour)
	_, colW := columns(m0.cardsW())
	tw := colW - 4 - avatar.Width - 2
	W := tw - (ansi.StringWidth(relTime(act, now)) + 1)
	name := strings.Repeat("a", W-1) + "zzzzzzzz" // z starts right where the cut puts its ellipsis
	m := psized(staticSrc{{ID: "x", Name: name, GroupKey: "/g", CWD: "/g", ContextPct: -1, LastActivity: act}}, 60, 30)
	m = typed(t, m, "sz")
	onScreen(t, m)
	if len(m.flash.ms) != 0 {
		t.Errorf("z is past the cut, yet hits %+v", m.flash.ms)
	}
}

// An open group's title after a card that had a hit is searched too.
func TestFlashOpenTitleAfterHit(t *testing.T) {
	src := staticSrc{
		{ID: "q1", Name: "qq", GroupKey: "/g1", CWD: "/g1", ContextPct: -1},
		{ID: "z1", Name: "zz", GroupKey: "/q2", CWD: "/q2", ContextPct: -1},
	}
	m := typed(t, psized(src, 60, 40), "sq") // 60 columns: one column, /g1 above /q2
	if _, ok := hitOf(m, "z1"); !ok {
		t.Errorf("/q2's title holds q: hits %+v", m.flash.ms)
	}
}

// The window starts one line below an open group's title: that title is off screen, no hit.
func TestFlashTitleJustAbove(t *testing.T) {
	var ss []store.Session
	for i := range 10 {
		ss = append(ss, store.Session{ID: fmt.Sprintf("x%02d", i), Name: fmt.Sprintf("n%02d", i), GroupKey: "/g", CWD: "/g", TmuxPane: "%1", Status: "idle", ContextPct: -1})
	}
	m := psized(staticSrc(ss), 60, 53) // bodyH 50; focus x09 → off 1: /g's title is line -1
	for range 9 {
		m, _ = press(t, m, "j")
	}
	m = typed(t, m, "sg")
	for _, h := range m.flash.ms {
		if h.y < 0 {
			t.Errorf("hit off screen: %+v", h)
		}
	}
	if top := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.HasPrefix(top, " agmux") {
		t.Errorf("top bar %q", top)
	}
}

// The first card of an open group can have two hits (its group's path and its name): each keeps its own label.
func TestFlashTwoHitsOneCardKeepLabels(t *testing.T) {
	src := &mutSrc{ss: []store.Session{{ID: "q1", Name: "qq", GroupKey: "/q", CWD: "/q", ContextPct: -1, TmuxPane: "%1"},
		{ID: "z1", Name: "zq", GroupKey: "/q", CWD: "/q", ContextPct: -1, TmuxPane: "%2"}}}
	m := typed(t, psized(src, 232, 50), "sq")
	before := labelsOf(m)
	if strings.Count(before, "q1=") != 2 {
		t.Fatalf("two hits on q1: %s", before)
	}
	tm, _ := m.Update(frameMsg{})
	if after := labelsOf(tm.(Model)); after != before {
		t.Errorf("labels moved: %s → %s", before, after)
	}
}

// A program waiting on an empty line below a full screen of output (seq 1 20; sleep): the cursor is
// on a blank row the trailing-blank trim would cut, and the content fills the box. The blank rows
// down to the cursor's are kept, so the box loses its top line and the cursor is on its last row.
func TestPreviewCursorOnBlankRowOfFullBox(t *testing.T) {
	trueColor(t)
	var b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	b.WriteString("\n") // row 30: empty, the cursor is here
	box := cursorBox(previewMsg{pane: "%1", text: b.String(), cur: &cursor{row: 30, col: 0}}, 0)
	last := box[len(box)-2] // the box's last row
	if !strings.Contains(last, "\x1b[7m") || !strings.Contains(ansi.Strip(box[len(box)-3]), "line29") {
		t.Errorf("last rows %q / %q", ansi.Strip(box[len(box)-3]), ansi.Strip(last))
	}
	if !strings.Contains(ansi.Strip(box[2]), "line22") { // 9 rows: line22…line29 and the cursor's blank row
		t.Errorf("top row %q", ansi.Strip(box[2]))
	}
	// without a cursor the blank rows are cut as ever
	plain := cursorBox(previewMsg{pane: "%1", text: b.String()}, 0)
	if !strings.Contains(ansi.Strip(plain[len(plain)-2]), "line29") {
		t.Errorf("no cursor: %q", ansi.Strip(plain[len(plain)-2]))
	}
	// and scrolled back the cursor isn't drawn, blank row or not
	for _, l := range cursorBox(previewMsg{pane: "%1", text: b.String(), cur: &cursor{row: 30, col: 0}}, 3) {
		if strings.Contains(l, "\x1b[7m") {
			t.Error("drawn while scrolled")
		}
	}
}

// A tab goes to the next multiple of 8 columns, as in the pane: the cursor on the b is on the b.
func TestPreviewCursorAfterTab(t *testing.T) {
	trueColor(t)
	box := cursorBox(previewMsg{pane: "%1", text: "a\tb\n", cur: &cursor{row: 0, col: 8}}, 0)
	if !strings.Contains(noResets(box[2]), "\x1b[7mb") {
		t.Errorf("%q", ansi.Strip(box[2]))
	}
	for in, want := range map[string]string{"a\tb": "a       b", "abcdefgh\tx": "abcdefgh        x", "\tx": "        x", "a\tb\tc": "a       b       c"} {
		if got := expandTabs(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	if got := expandTabs("\x1b[31mab\x1b[0m\tx"); ansi.Strip(got) != "ab      x" {
		t.Errorf("escapes take no columns: %q", got)
	}
}

// The color of text after the cursor cell carries on.
func TestPreviewCursorKeepsColorAfter(t *testing.T) {
	trueColor(t)
	box := cursorBox(previewMsg{pane: "%1", text: "\x1b[31mredtext\x1b[0m\n", cur: &cursor{row: 0, col: 2}}, 0)
	after := box[2][strings.Index(box[2], "\x1b[7m"):]
	if j := strings.Index(after, "text"); j < 0 || !strings.Contains(after[:j], "31") {
		t.Errorf("text after the cursor lost its red: %q", after)
	}
}

// A card goes in a poll and a frame tick reads the board again before the user presses its label:
// the label is still the one of a hit that was drawn, so it is ignored (not typed), and no new hit
// is given the letter it freed. Typing or backspace ends that.
func TestFlashGoneLabelAcrossTick(t *testing.T) {
	src := &mutSrc{ss: slices.Clone([]store.Session(flashSrc))}
	m := typed(t, psized(src, 232, 50), "sb")
	h, _ := hitOf(m, "b1")
	label := h.label
	// Bravo goes; a card with another b comes in the same poll; then the tick
	src.ss = slices.DeleteFunc(slices.Clone(src.ss), func(s store.Session) bool { return s.ID == "b1" })
	src.ss = append(src.ss, store.Session{ID: "n1", Name: "abba", GroupKey: "/z", CWD: "/z", ContextPct: -1, TmuxPane: "%9"})
	tm, _ := m.Update(frameMsg{})
	m = tm.(Model)
	if _, ok := hitOf(m, "b1"); ok {
		t.Fatal("Bravo's hit is gone after the tick")
	}
	for _, x := range m.flash.ms {
		if x.label == label {
			t.Errorf("the freed letter %q went to a new hit: %s", label, labelsOf(m))
		}
	}
	before := m.focus
	if m, _ = press(t, m, label); m.mode != flashing || m.flash.pat != "b" || m.focus != before {
		t.Errorf("the vanished card's label: mode %v pattern %q focus %s", m.mode, m.flash.pat, m.focus)
	}
	// typing ends the grace: the pattern changes, labels are given afresh
	m = typed(t, m, "r")
	if m.flash.pat != "br" || len(m.flash.freed) != 0 {
		t.Errorf("after typing: pattern %q freed %v", m.flash.pat, m.flash.freed)
	}
	// a backspace too
	m = typed(t, psized(src, 232, 50), "sb")
	h2, _ := hitOf(m, "n1")
	src.ss = slices.DeleteFunc(slices.Clone(src.ss), func(s store.Session) bool { return s.ID == "n1" })
	tm, _ = m.Update(frameMsg{})
	m = tm.(Model)
	if m, _ = press(t, m, h2.label); m.flash.pat != "b" {
		t.Fatalf("ignored: %q", m.flash.pat)
	}
	if m, _ = press(t, m, "backspace"); len(m.flash.freed) != 0 {
		t.Errorf("after backspace: %v", m.flash.freed)
	}
}

// The message line names the flash key as configured (and not a fixed "s").
func TestFlashMessageLineNamesTheKey(t *testing.T) {
	m := withConfig(t, "[keys]\nflash = [\"F\"]\nfold = [\"s\"]\n")
	m, _ = press(t, m, "F")
	m = typed(t, m, "br")
	v := strings.Split(ansi.Strip(m.View()), "\n")
	if v[len(v)-2] != " F br▏" {
		t.Errorf("msg line %q", v[len(v)-2])
	}
}

// In one poll Bravo goes and a new card with a hit comes: the new card must not take Bravo's label
// (the user saw it on Bravo), and pressing it is ignored. By the tick or by the key itself.
func TestFlashSwapInOnePoll(t *testing.T) {
	for _, via := range []string{"key", "tick"} {
		src := &mutSrc{ss: slices.Clone([]store.Session(flashSrc))}
		m := typed(t, psized(src, 232, 50), "sb")
		h, _ := hitOf(m, "b1")
		src.ss = slices.DeleteFunc(slices.Clone(src.ss), func(s store.Session) bool { return s.ID == "b1" })
		src.ss = append(src.ss, store.Session{ID: "y", Name: "bob", GroupKey: "/h", CWD: "/h", ContextPct: -1, TmuxPane: "%9"})
		if via == "tick" {
			tm, _ := m.Update(frameMsg{})
			m = tm.(Model)
			if y, ok := hitOf(m, "y"); !ok || y.label == h.label || y.label == "" {
				t.Errorf("tick: the new card has %+v; Bravo's label was %q", y, h.label)
			}
		}
		before := m.focus
		m, _ = press(t, m, h.label)
		if m.mode != flashing || m.focus != before || m.flash.pat != "b" {
			t.Errorf("%s: Bravo's label %q: mode %v focus %s pattern %q", via, h.label, m.mode, m.focus, m.flash.pat)
		}
	}
}

// View reads the board again for what it draws, but never writes that into the model.
func TestFlashViewDoesNotWriteFreed(t *testing.T) {
	src := &mutSrc{ss: slices.Clone([]store.Session(flashSrc))}
	m := typed(t, psized(src, 232, 50), "sb")
	m.flash.freed = map[string]bool{"z": true}
	src.ss = slices.DeleteFunc(slices.Clone(src.ss), func(s store.Session) bool { return s.ID == "b1" })
	_ = m.View()
	if len(m.flash.freed) != 1 || !m.flash.freed["z"] {
		t.Errorf("View wrote into the model's freed map: %v", m.flash.freed)
	}
	if _, ok := hitOf(m, "b1"); !ok {
		t.Error("nor into its hits")
	}
}

// 22 cards: the wheel and ctrl+d / ctrl+u scroll the cards without moving the focus; the view
// goes back to following the focus when a key moves it out of view; the top bar says what is cut.
func TestScrollCards(t *testing.T) {
	var ss staticSrc
	for i := range 22 {
		ss = append(ss, store.Session{ID: fmt.Sprintf("s%02d", i), Name: fmt.Sprintf("n%02d", i), GroupKey: "/a", Status: "idle", ContextPct: -1})
	}
	m := New(ss, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	wheel := func(m Model, b tea.MouseButton) Model {
		tm, _ := m.Update(tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: b})
		return tm.(Model)
	}
	off := func(m Model) int { _, o, _, _ := m.window(); return o }
	_, _, most, bodyH := m.window()
	if most == 0 {
		t.Fatal("fixture fits on screen")
	}
	top := func(m Model) string { return ansi.Strip(strings.SplitN(m.View(), "\n", 2)[0]) }
	if v := top(m); !strings.Contains(v, "↓ more") || strings.Contains(v, "↑ more") {
		t.Errorf("at the top: %q", v)
	}
	focus := m.focus
	m = wheel(m, tea.MouseButtonWheelDown)
	if off(m) != 3 || m.focus != focus {
		t.Errorf("wheel down: off %d, focus %q -> %q", off(m), focus, m.focus)
	}
	if v := top(m); !strings.Contains(v, "↓ more") || !strings.Contains(v, "↑ more") {
		t.Errorf("in the middle: %q", v)
	}
	for range 100 {
		m = wheel(m, tea.MouseButtonWheelDown)
	}
	if off(m) != most {
		t.Errorf("wheel to the end: off %d, most %d", off(m), most)
	}
	if v := top(m); strings.Contains(v, "↓ more") || !strings.Contains(v, "↑ more") {
		t.Errorf("at the bottom: %q", v)
	}
	if !strings.Contains(ansi.Strip(m.View()), "n21") {
		t.Error("the last card is not on screen at the bottom")
	}
	for range 100 {
		m = wheel(m, tea.MouseButtonWheelUp)
	}
	if off(m) != 0 {
		t.Errorf("wheel to the top: off %d", off(m))
	}
	m, _ = press(t, m, "ctrl+d")
	if off(m) != bodyH/2 || m.focus != focus {
		t.Errorf("ctrl+d: off %d, want %d, focus %q", off(m), bodyH/2, m.focus)
	}
	m, _ = press(t, m, "ctrl+u")
	if off(m) != 0 {
		t.Errorf("ctrl+u: off %d", off(m))
	}
	// scrolled to the bottom, the focus (the first card) is out of view: j brings it back into view
	for range 100 {
		m = wheel(m, tea.MouseButtonWheelDown)
	}
	m, _ = press(t, m, "j")
	if m.scrolled || !strings.Contains(ansi.Strip(m.View()), "n01") {
		t.Errorf("j with the focus off screen: scrolled %v, view\n%s", m.scrolled, ansi.Strip(m.View()))
	}
	m.top, m.scrolled = 1, true
	if v := top(m); !strings.Contains(v, "↑ more") {
		t.Errorf("one line scrolled off: %q", v)
	}
	// polls took cards away while scrolled to the end: the view stays inside what is left
	src := &mutSrc{ss: slices.Clone(ss)}
	m.src = src
	for range 100 {
		m = wheel(m, tea.MouseButtonWheelDown)
	}
	src.ss = src.ss[:4]
	if v := ansi.Strip(m.View()); !strings.Contains(v, "n00") || !strings.Contains(v, "n03") {
		t.Errorf("after the list shrank:\n%s", v)
	}
	m.src, m.scrolled = ss, false
	// the focus below the view: k brings it back
	m, _ = press(t, m, strings.Split(strings.Repeat("j", 21), "")...)
	for range 100 {
		m = wheel(m, tea.MouseButtonWheelUp)
	}
	m, _ = press(t, m, "k")
	if m.scrolled || !strings.Contains(ansi.Strip(m.View()), "n20") {
		t.Errorf("k with the focus below the view: scrolled %v", m.scrolled)
	}
	// the focus still in view: k leaves the scroll alone
	m = wheel(m, tea.MouseButtonWheelUp)
	o := off(m)
	m, _ = press(t, m, "k")
	if !m.scrolled || off(m) != o {
		t.Errorf("k with the focus on screen: scrolled %v, off %d -> %d", m.scrolled, o, off(m))
	}
}

// The board has no focus until the first event sets it: a wheel or ctrl+d as the very first event
// must scroll (the focus appearing is not the focus moving out of view).
func TestScrollAsFirstEvent(t *testing.T) {
	var ss staticSrc
	for i := range 22 {
		ss = append(ss, store.Session{ID: fmt.Sprintf("s%02d", i), Name: fmt.Sprintf("n%02d", i), GroupKey: "/a", Status: "idle", ContextPct: -1})
	}
	first := map[string]func(Model) Model{
		"wheel": func(m Model) Model {
			tm, _ := m.Update(tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
			return tm.(Model)
		},
		"ctrl+d": func(m Model) Model { m, _ = press(t, m, "ctrl+d"); return m },
	}
	for name, ev := range first {
		m := New(ss, &fakeRunner{})
		m.getenv = func(string) string { return "" }
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m = ev(tm.(Model))
		if _, off, _, _ := m.window(); !m.scrolled || off == 0 {
			t.Errorf("%s as the first event: scrolled %v, off %d", name, m.scrolled, off)
		}
	}
}

func scrollFixture(n int) (*mutSrc, Model) {
	src := &mutSrc{}
	for i := range n {
		src.ss = append(src.ss, store.Session{ID: fmt.Sprintf("s%02d", i), Name: fmt.Sprintf("n%02d", i), GroupKey: "/a", Status: "idle", ContextPct: -1})
	}
	m := New(src, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return src, tm.(Model)
}

func wheelOn(m Model, b tea.MouseButton) Model {
	tm, _ := m.Update(tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: b})
	return tm.(Model)
}

// layoutOf reads the window of a copy: window() sets a missing focus on the model it is called
// on, which would hide the very thing the tests below are about.
func layoutOf(m Model) (rects []rect, off, last, bodyH int) { return m.window() }

// Scrolled to the end with the focus on the last card, a poll takes that card away. The focus
// falls back to the first card lazily, in the next event: a wheel or ctrl+d there must still
// scroll from where the view is, not be taken for the focus moving out of view.
func TestScrollAfterFocusGone(t *testing.T) {
	for _, ev := range []string{"wheel", "ctrl+u"} {
		src, m := scrollFixture(22)
		m, _ = press(t, m, strings.Split(strings.Repeat("j", 21), "")...)
		_, _, most, bodyH := layoutOf(m)
		m.top, m.scrolled = most, true
		src.ss = src.ss[:21]
		_, _, last, _ := layoutOf(m)
		want := last - 3
		if ev == "wheel" {
			m = wheelOn(m, tea.MouseButtonWheelUp)
		} else {
			m, _ = press(t, m, ev)
			want = last - bodyH/2
		}
		if _, got, _, _ := layoutOf(m); !m.scrolled || got != want {
			t.Errorf("%s after the focused card left: scrolled %v off %d, want %d", ev, m.scrolled, got, want)
		}
	}
}

// A poll that takes the focused card away puts the focus on the first card; if that is out of view
// the manual scroll ends (the focus is always in view then); if it is in view the scroll stays.
func TestScrollWhenPollTakesFocus(t *testing.T) {
	for _, c := range []struct {
		top        int
		wantScroll bool
	}{{40, false}, {0, true}} {
		src, m := scrollFixture(22)
		m, _ = press(t, m, strings.Split(strings.Repeat("j", 21), "")...)
		m.top, m.scrolled = c.top, true
		src.ss = src.ss[:21]
		tm, _ := m.Update(frameMsg{})
		m = tm.(Model)
		if m.scrolled != c.wantScroll || m.focus != "s00" {
			t.Errorf("top %d: scrolled %v, focus %q; want scrolled %v on s00", c.top, m.scrolled, m.focus, c.wantScroll)
		}
		if !c.wantScroll && !strings.Contains(ansi.Strip(m.View()), "n00") {
			t.Errorf("top %d: the focused card is not on screen", c.top)
		}
	}
}

// Around every edge of a card: a key that moves the focus onto it keeps the scroll only if the
// card is wholly in view (one line cut at the top or the bottom counts as out).
func TestScrollPartialBoundary(t *testing.T) {
	_, base := scrollFixture(22)
	rects, _, most, bodyH := layoutOf(base)
	pos := map[string]rect{}
	for _, r := range rects {
		pos[r.id] = r
	}
	checked := 0
	for i := 1; i < 21; i++ {
		target := pos[fmt.Sprintf("s%02d", i)]
		var tops []int
		for _, d := range []int{-1, 0, 1} {
			tops = append(tops, target.y+d, target.y+target.h-bodyH+d)
		}
		for _, top := range tops {
			if top < 0 || top > most {
				continue
			}
			for _, k := range []string{"j", "k"} {
				from := i - 1 // j moves onto the target from the card above it, k from below
				if k == "k" {
					from = i + 1
				}
				m := base
				m.focus = fmt.Sprintf("s%02d", from)
				m.top, m.scrolled = top, true
				m, _ = press(t, m, k)
				if m.focus != fmt.Sprintf("s%02d", i) {
					t.Fatalf("%s from s%02d landed on %s", k, from, m.focus)
				}
				if in := target.y >= top && target.y+target.h <= top+bodyH; m.scrolled != in {
					t.Errorf("top %d, %s onto s%02d (lines %d..%d, view %d..%d): scrolled %v", top, k, i, target.y, target.y+target.h, top, top+bodyH, m.scrolled)
				}
				checked++
			}
		}
	}
	if checked < 100 {
		t.Errorf("only %d cases", checked)
	}
}

// While scrolled, a click lands on the card drawn under the pointer.
func TestScrollClick(t *testing.T) {
	_, m := scrollFixture(22)
	for range 3 {
		m = wheelOn(m, tea.MouseButtonWheelDown)
	}
	c := m
	rects, off, _, _ := layoutOf(c)
	for _, r := range rects {
		if r.id == "s06" { // off 9 puts it where s04 sits unscrolled
			tm, _ := m.Update(tea.MouseMsg{X: r.x + 1, Y: 1 + r.y - off, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if got := tm.(Model).focus; got != "s06" {
				t.Errorf("clicked s06 (screen line %d), focus %q", 1+r.y-off, got)
			}
			return
		}
	}
	t.Fatal("no s06")
}

// Only browsing scrolls the cards: under flash, a kill prompt or the avatar grid the wheel does nothing.
func TestScrollWheelOtherModes(t *testing.T) {
	for _, k := range []string{"s", "x", "a"} {
		_, m := scrollFixture(22)
		m, _ = press(t, m, "j") // a focus to act on
		if m, _ = press(t, m, k); m.mode == browsing {
			t.Fatalf("%s left the browsing mode", k)
		}
		if m = wheelOn(m, tea.MouseButtonWheelDown); m.scrolled {
			t.Errorf("the wheel scrolled the cards in mode %v (after %q)", m.mode, k)
		}
	}
}

// The wheel scrolls the focused card out of view (the point of the wheel); the 500ms frames that
// follow, with no poll change, leave the scroll alone.
func TestScrollSurvivesFrames(t *testing.T) {
	_, m := scrollFixture(22)
	m, _ = press(t, m, "j") // a focus that is set (a frame skips an empty one)
	for range 10 {
		m = wheelOn(m, tea.MouseButtonWheelDown)
	}
	_, want, _, _ := layoutOf(m)
	for range 3 {
		tm, _ := m.Update(frameMsg{})
		m = tm.(Model)
	}
	if _, got, _, _ := layoutOf(m); !m.scrolled || got != want || want == 0 {
		t.Errorf("after frames: scrolled %v off %d, want %d", m.scrolled, got, want)
	}
}
