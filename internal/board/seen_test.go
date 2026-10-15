package board

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

// seenSrc is the store as far as "seen" goes: MarkSeen is recorded and takes the done off the session.
type seenSrc struct {
	staticSrc
	marked []string
	err    error // MarkSeen fails with it
}

func (s *seenSrc) Sessions() []store.Session { return s.staticSrc }
func (s *seenSrc) MarkSeen(x store.Session) error {
	s.marked = append(s.marked, x.ID)
	if s.err != nil {
		return s.err
	}
	for i := range s.staticSrc {
		if s.staticSrc[i].ID == x.ID {
			s.staticSrc[i].Done = false
		}
	}
	return nil
}

func seenSessions() staticSrc {
	return staticSrc{
		{ID: "d1", Name: "one", GroupKey: "/a", TmuxPane: "%1", Status: "idle", Done: true, ContextPct: -1},
		{ID: "d2", Name: "two", GroupKey: "/a", TmuxPane: "%2", Status: "idle", Done: true, ContextPct: -1},
		{ID: "w1", Name: "three", GroupKey: "/b", TmuxPane: "%3", Status: "working", ContextPct: -1},
	}
}

func seenModel() (Model, *seenSrc, *fakeRunner) {
	src := &seenSrc{staticSrc: seenSessions()}
	r := &fakeRunner{}
	m := New(src, r)
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return tm.(Model), src, r
}

func frame(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(frameMsg{})
	m = tm.(Model)
	run(&m, cmd)
	return m
}

// Resting on a done card, however long, and passing over it, do not make it seen: only a look at
// its result does (the preview, the keys, a jump). It stays done, and nothing is written.
func TestSeenNotByResting(t *testing.T) {
	m, src, _ := seenModel()
	for range 10 {
		m = frame(t, m)
	}
	m, _ = press(t, m, "j", "k", "l", "h")
	for range 10 {
		m = frame(t, m)
	}
	if len(src.marked) != 0 || !src.staticSrc[0].Done {
		t.Errorf("marked %v, d1 done %v", src.marked, src.staticSrc[0].Done)
	}
}

// Opening the preview, taking the keys and jumping on a done card each count at once.
func TestSeenOnPreviewLockJump(t *testing.T) {
	for name, act := range map[string]func(Model) (Model, bool){
		"preview": func(m Model) (Model, bool) { return press(t, m, "p") },
		"lock":    func(m Model) (Model, bool) { return press(t, m, "tab") },
		"click": func(m Model) (Model, bool) {
			m.preview = true
			tm, cmd := m.Update(tea.MouseMsg{X: m.cardsW() + 2, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			m = tm.(Model)
			return m, run(&m, cmd)
		},
		"jump": func(m Model) (Model, bool) { return press(t, m, "enter") },
	} {
		m, src, _ := seenModel()
		act(m)
		if !reflect.DeepEqual(src.marked, []string{"d1"}) {
			t.Errorf("%s: marked %v", name, src.marked)
		}
		// the same on a card that is not done marks nothing
		m, src, _ = seenModel()
		m, _ = press(t, m, "l")
		act(m)
		if len(src.marked) != 0 {
			t.Errorf("%s on a working card: marked %v", name, src.marked)
		}
	}
	m, _, _ := seenModel()
	m, _ = press(t, m, "p", "p") // closing it marks nothing more
	if m.preview {
		t.Error("p twice should close the preview")
	}
}

// A mark that fails does not stop the jump; on the board it says why.
func TestSeenFailure(t *testing.T) {
	m, src, r := seenModel()
	src.err = errors.New("seen.json: not valid JSON")
	press(t, m, "enter")
	if !slices.ContainsFunc(r.calls, func(c []string) bool { return slices.Contains(c, "switch-client") }) || len(src.marked) != 1 {
		t.Errorf("calls %q marked %v", r.calls, src.marked)
	}
	m, _, _ = seenModel()
	m.src = src
	m, _ = press(t, m, "p")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "seen.json: not valid JSON") {
		t.Errorf("no error on the board:\n%s", v)
	}
}

// m marks the focused done card as seen, as the preview does, without opening anything: it turns
// idle at once and the footer says so. Not on a done card, or on a folded title: nothing.
func TestMarkReadKey(t *testing.T) {
	m, src, r := seenModel()
	m, _ = press(t, m, "m")
	if !reflect.DeepEqual(src.marked, []string{"d1"}) || src.staticSrc[0].Done || m.preview || len(r.calls) != 0 {
		t.Errorf("marked %v, d1 done %v, preview %v, calls %q", src.marked, src.staticSrc[0].Done, m.preview, r.calls)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "read one") {
		t.Errorf("no read message:\n%s", v)
	}
	for name, keys := range map[string][]string{
		"idle card":    {"m", "m"}, // the second one: d1 is idle by then
		"working card": {"l", "m"},
		"folded title": {"f", "m"}, // f folds the card's group and leaves the focus on its title
	} {
		m, src, _ := seenModel()
		m, _ = press(t, m, keys...)
		want := 0
		if name == "idle card" {
			want = 1 // only the first m
		}
		if len(src.marked) != want || strings.Contains(ansi.Strip(m.View()), "read three") {
			t.Errorf("%s: marked %v", name, src.marked)
		}
	}
	// no message when there was nothing to mark
	m, _, _ = seenModel()
	if m, _ = press(t, m, "l", "m"); m.msg != "" {
		t.Errorf("m on a working card said %q", m.msg)
	}
}

// m is the key of mark_read, and only while it is: rebound, M works and m does not. In the search
// box, the pass-through and the picker m is not the action.
func TestMarkReadRebound(t *testing.T) {
	cfg, err := config.Parse("cfg", []byte("[keys]\nmark_read = [\"M\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, src, _ := seenModel()
	m.cfg, m.keymap = cfg, cfg.KeyMap()
	if m, _ = press(t, m, "m"); len(src.marked) != 0 {
		t.Errorf("m after rebinding: marked %v", src.marked)
	}
	if m, _ = press(t, m, "M"); !reflect.DeepEqual(src.marked, []string{"d1"}) {
		t.Errorf("M: marked %v", src.marked)
	}
	for name, keys := range map[string][]string{"search": {"/", "m"}, "pass-through": {"l", "i", "m"}, "picker": {"a", "m"}} {
		m, src, _ := seenModel() // the pass-through is on the working card: i on a done one marks it
		m, _ = press(t, m, keys...)
		if len(src.marked) != 0 {
			t.Errorf("%s: m marked %v", name, src.marked)
		}
	}
}
