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
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+]":
		return tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}
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
		{"background: enter / H / s refused", []string{"j", "enter", "H", "s", "x", "y"}, [][]string{{"kill", "a2"}}, false},
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
	m, _ = press(t, m, "p", "tab")
	if !m.pvFocus {
		t.Fatal("tab should focus the preview")
	}
	v := m.View()
	focusEsc, _, _ := strings.Cut(fg(cFocus, "╭"), "╭")
	if !strings.Contains(v, focusEsc+"╭") || !strings.HasSuffix(ansi.Strip(v), "keys go to the pane · ctrl+] back to the cards") {
		t.Error("focused preview: #ff8ebe border and the pass-through footer")
	}
	r.calls = nil
	m.scroll = 6
	m, quit := press(t, m, "你好 world", "enter", "ctrl+c", "q", "esc", "tab")
	if quit || !m.pvFocus {
		t.Fatal("ctrl+c / q / esc / tab go to the pane while it has the keys")
	}
	capture := []string{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-44"}
	var want [][]string
	for _, k := range [][]string{{"-l", "--", "你好 world"}, {"Enter"}, {"C-c"}, {"-l", "--", "q"}, {"Escape"}, {"Tab"}} {
		want = append(want, append([]string{"tmux", "send-keys", "-t", "%1"}, k...), capture)
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("forwarded:\n %q\nwant\n %q", r.calls, want)
	}
	if m.scroll != 0 {
		t.Error("a forwarded key scrolls the preview back to the bottom")
	}
	m, quit = press(t, m, "ctrl+]", "q")
	if m.pvFocus || !quit {
		t.Error("ctrl+] gives the keys back to the board, then q quits")
	}

	m, _ = press(t, wide(&fakeRunner{}, 232, 50), "p", "j", "tab") // a2: no pane
	if m.pvFocus {
		t.Error("no pane, nothing to type into")
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

// s = reply: open the preview if needed and hand it the keys; what is typed next goes to the pane.
func TestSReplies(t *testing.T) {
	r := &fakeRunner{}
	m := wide(r, 232, 50)
	tm, cmd := m.Update(key("i"))
	m = tm.(Model)
	if !m.previewOn() || !m.pvFocus || cmd == nil {
		t.Fatalf("s: preview %v, keys %v", m.previewOn(), m.pvFocus)
	}
	cmd() // the opening capture
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
	if last := r.calls[len(r.calls)-1]; m.focus != "b1" || last[1] != "capture-pane" || last[5] != "%3" {
		t.Errorf("a click captures the new card's pane: %q", last)
	}
	m.capturing = false
	m, _ = press(t, m, "h", "f") // fold /a: focus on its title
	m.capturing = false
	before := len(r.calls)
	tm, cmd = m.Update(key("enter")) // unfold: focus lands on a1
	m = tm.(Model)
	runAll(cmd)
	if len(r.calls) == before || r.calls[len(r.calls)-1][5] != "%1" {
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
			if call[0] != "tmux" || call[1] != "capture-pane" {
				t.Errorf("%s: only captures may run while locked out, got %q", c.name, call)
			}
		}
		r.mu.Unlock()
		if quit || !strings.Contains(ansi.Strip(m.View()), "session is gone · ctrl+] back to the cards") {
			t.Errorf("%s: locked out with a red footer (quit=%v)", c.name, quit)
		}
		if m, _ = press(t, m, "ctrl+]"); m.pvFocus {
			t.Errorf("%s: ctrl+] takes the keys back", c.name)
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
// entered on, keys go nowhere (locked out) until ctrl+] / esc.
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
	if m, _ = press(t, m, "ctrl+]"); m.pvFocus {
		t.Error("ctrl+] takes the keys back")
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
	if m.scroll != 0 || !slices.ContainsFunc(r.calls, func(c []string) bool { return c[1] == "capture-pane" && c[len(c)-1] == "-44" }) {
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
		{"tmux", "capture-pane", "-p", "-e", "-t", "%1", "-S", "-44"},
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
		if want := []string{"capture-pane", "send-keys", "capture-pane"}; !reflect.DeepEqual(order, want) {
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
	if cd := card(s, 48, 5, false, 0, now); !strings.HasPrefix(cd, "\x1b[38;2;18;52;86m╭") {
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

// a: the avatar list over the focused card; j/k move, enter binds by name (ID without one) into
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
	if !strings.Contains(ansi.Strip(m.View()), "avatar for one") {
		t.Fatalf("list not open:\n%s", ansi.Strip(m.View()))
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for _, sp := range avatar.Sprites {
		found := false
		for _, l := range lines {
			if strings.Contains(l, sp.ID+"  "+sp.Name) {
				found = true
				if strings.Contains(l, "▸") != (sp.ID == "bot-04") {
					t.Errorf("cursor starts on the sprite the card wears: %q", l)
				}
			}
		}
		if !found {
			t.Errorf("%s missing from the list", sp.ID)
		}
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	if v := m.View(); !strings.Contains(v, fg(cFocus, "bot-04  Boo")) || strings.Contains(v, fg(cFocus, "bot-01  Clawd")) {
		t.Error("the sprite the card wears now is in the focus color, only it")
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "j/k move · enter bind · q cancel") {
		t.Errorf("footer %q", last)
	}
	m, quit := press(t, m, "x", "f", "p", "i", "H", "r", "d", "k", "k", "enter") // bot-04 → bot-02
	if quit || m.mode != browsing || m.preview || m.folded["/a"] || len(r.calls) != 0 {
		t.Fatalf("keys acted while the list was open: mode %v preview %v folded %v calls %q", m.mode, m.preview, m.folded, r.calls)
	}
	if !strings.Contains(ansi.Strip(m.View()), "one wears Boxy") {
		t.Error("the bind is confirmed on the message line")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"one": "bot-02"`) {
		t.Errorf("avatars.json: %s", data)
	}
	if strings.Contains(ansi.Strip(m.View()), "avatar for") || av("a1") != "bot-02" {
		t.Errorf("list closed and card shows bot-02: %v", p.Sessions())
	}
	if m, _ = press(t, m, "a", "j", "j", "enter"); av("a1") != "bot-04" {
		t.Error("j j enter: two below the current one")
	}
	for _, cancel := range []string{"esc", "q"} {
		m, quit = press(t, m, "a", "j", cancel)
		if quit || m.mode != browsing || av("a1") != "bot-04" {
			t.Errorf("%s cancels, keeps the board: quit %v mode %v", cancel, quit, m.mode)
		}
	}
	if m, _ = press(t, m, "a", "k", "k", "k", "k", "k", "enter"); av("a1") != "bot-01" {
		t.Error("k stops at the first sprite")
	}
	if m, _ = press(t, m, "a", "j", "j", "j", "j", "j", "j", "j", "enter"); av("a1") != "bot-06" {
		t.Error("j stops at the last sprite")
	}
	m, _ = press(t, m, "k", "a", "j", "enter") // a2: no name
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
