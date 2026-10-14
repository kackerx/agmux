package board

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
