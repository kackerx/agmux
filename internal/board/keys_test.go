package board

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/store"
)

type staticSrc []store.Session

func (s staticSrc) Sessions() []store.Session { return s }
func (s staticSrc) Err() error                { return nil }

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(args ...string) error {
	f.calls = append(f.calls, args)
	return f.err
}

func (f *fakeRunner) Interactive(args ...string) *exec.Cmd {
	f.calls = append(f.calls, append([]string{"(interactive)"}, args...))
	return exec.Command("true")
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
		{"enter switches and quits", []string{"enter"}, [][]string{{"switch", "a1"}}, true},
		{"l moves to next column", []string{"l", "enter"}, [][]string{{"switch", "b1"}}, true},
		{"l then h comes back", []string{"l", "h", "r"}, [][]string{{"restart", "a1"}}, false},
		{"j/k within column", []string{"j", "j", "r", "k", "r"}, [][]string{{"restart", "a2"}, {"restart", "a1"}}, false},
		{"H sends /handoff", []string{"H"}, [][]string{{"send", "a1", "/handoff"}}, false},
		{"d reviews interactively", []string{"d"}, [][]string{{"(interactive)", "review", "a1"}}, false},
		{"x y kills", []string{"x", "y"}, [][]string{{"kill", "a1"}}, false},
		{"x other cancels", []string{"x", "n", "x", "q"}, nil, false},
		{"s types and sends", []string{"s", "hi", " ", "你好", "enter"}, [][]string{{"send", "a1", "hi 你好"}}, false},
		{"s esc cancels", []string{"s", "hi", "esc"}, nil, false},
		{"s empty cancels", []string{"s", " ", "enter"}, nil, false},
		{"q in s mode is text", []string{"s", "q", "enter"}, [][]string{{"send", "a1", "q"}}, false},
		{"background: enter / H / s refused", []string{"j", "enter", "H", "s", "x", "y"}, [][]string{{"kill", "a2"}}, false},
		{"q quits", []string{"q"}, nil, true},
		{"burst of keys = one by one", []string{"lr"}, [][]string{{"restart", "b1"}}, false},
		{"burst keeps typed order", []string{"Hr"}, [][]string{{"send", "a1", "/handoff"}, {"restart", "a1"}}, false},
		{"ctrl+c quits from the prompt", []string{"s", "hi", "ctrl+c"}, nil, true},
		{"pasted newlines become spaces", []string{"s", "a\nb", "enter"}, [][]string{{"send", "a1", "a b"}}, false},
		{"burst into the prompt", []string{"shi", "enter"}, [][]string{{"send", "a1", "hi"}}, false},
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
	r.err = errors.New("ccmux switch a1: no pane")
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
	_, rects, _ := view(sessions, m.w, m.h, m.focus, 0, now, "", false)
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
	if !reflect.DeepEqual(r.calls, [][]string{{"switch", "b1"}}) {
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
