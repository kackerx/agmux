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

// A done card the focus rests on for two frames is seen; one frame is not enough.
func TestSeenAfterTwoFrames(t *testing.T) {
	m, src, _ := seenModel()
	m = frame(t, m)
	if len(src.marked) != 0 {
		t.Fatalf("one frame: marked %v", src.marked)
	}
	m = frame(t, m)
	if !reflect.DeepEqual(src.marked, []string{"d1"}) {
		t.Fatalf("two frames: marked %v", src.marked)
	}
	frame(t, m) // seen is idle now: nothing more to mark
	if len(src.marked) != 1 {
		t.Errorf("marked again: %v", src.marked)
	}
}

// Passing over a card does not count, not even away and back between two frames.
func TestSeenPassingDoesNotCount(t *testing.T) {
	m, src, _ := seenModel()
	m = frame(t, m)
	m, _ = press(t, m, "j", "k") // d1 → d2 → d1 within one frame
	m = frame(t, m)
	if m.focus != "d1" || len(src.marked) != 0 {
		t.Errorf("focus %q marked %v", m.focus, src.marked)
	}
	m, _ = press(t, m, "j")
	m = frame(t, m)
	m, _ = press(t, m, "k")
	frame(t, m)
	if len(src.marked) != 0 {
		t.Errorf("marked %v", src.marked)
	}
}

// A card that is not done is never marked, however long the focus rests on it.
func TestSeenOnlyDone(t *testing.T) {
	m, src, _ := seenModel()
	m, _ = press(t, m, "l") // w1
	for range 4 {
		m = frame(t, m)
	}
	if m.focus != "w1" || len(src.marked) != 0 {
		t.Errorf("focus %q marked %v", m.focus, src.marked)
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
	m = frame(t, frame(t, m))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "seen.json: not valid JSON") {
		t.Errorf("no error on the board:\n%s", v)
	}
}

// The two frames count from when the card is done, not from when the focus got there.
func TestSeenCountsFromDone(t *testing.T) {
	m, src, _ := seenModel()
	m, _ = press(t, m, "l") // w1, working
	for range 3 {
		m = frame(t, m)
	}
	src.staticSrc[2].Done = true
	m = frame(t, m)
	if len(src.marked) != 0 {
		t.Fatalf("one frame after it was done: marked %v", src.marked)
	}
	frame(t, m)
	if !reflect.DeepEqual(src.marked, []string{"w1"}) {
		t.Errorf("marked %v", src.marked)
	}
}

// A board --stay has hidden keeps running unseen: nothing counts as seen until a key, a resize or
// tmux saying the popup is attached again shows someone is looking. (A popup attached at the same
// size sends no resize, which is why tmux is asked.)
func TestSeenNotWhileHidden(t *testing.T) {
	attached := "display-message #{session_attached}"
	for _, wake := range []string{"resize", "key", "attach"} {
		m, r := stayModel(stayClients)
		r.tmuxOut[attached] = "0\n"
		src := &seenSrc{staticSrc: seenSessions()}
		m.src = src
		m = frame(t, m) // d1: one frame of its own
		m, quit := press(t, m, "q")
		if quit || !m.hidden {
			t.Fatalf("quit %v hidden %v", quit, m.hidden)
		}
		for range 10 {
			m = frame(t, m)
		}
		if len(src.marked) != 0 || !m.hidden {
			t.Fatalf("%s: marked while hidden: %v (hidden %v)", wake, src.marked, m.hidden)
		}
		attachedCalls := func() (n int) { // "who is attached": the footer's own question about the popup is another
			for _, c := range tmuxCalls(r, "display-message") {
				if c[len(c)-1] == "#{session_attached}" {
					n++
				}
			}
			return
		}
		asked := attachedCalls() // the question is asked, one at a time
		switch wake {
		case "resize":
			tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			m = tm.(Model)
		case "key":
			m, _ = press(t, m, "k")
		case "attach":
			r.tmuxOut[attached] = "1\n"
			m = frame(t, m)
		}
		if m.hidden {
			t.Fatalf("%s: still hidden", wake)
		}
		m = frame(t, m) // the frame before the hiding must not count: one frame is not enough
		if len(src.marked) != 0 {
			t.Fatalf("%s: marked after one frame: %v", wake, src.marked)
		}
		m = frame(t, m)
		if !reflect.DeepEqual(src.marked, []string{"d1"}) {
			t.Errorf("%s: marked %v after two frames", wake, src.marked)
		}
		if asked < 10 {
			t.Errorf("%s: asked tmux %d times in 10 frames", wake, asked)
		}
		if n := attachedCalls(); wake != "attach" && n != asked {
			t.Errorf("%s: asked tmux again after waking (%d → %d)", wake, asked, n)
		}
	}
}

// enter under --stay hides the popup too: a done that comes after it is not seen by the frames that follow.
func TestSeenNotWhileHiddenByJump(t *testing.T) {
	m, _ := stayModel(stayClients)
	src := &seenSrc{staticSrc: seenSessions()}
	m.src = src
	m, _ = press(t, m, "enter")  // marks d1, hides
	src.staticSrc[0].Done = true // it works and is done again, while nobody looks
	for range 10 {
		m = frame(t, m)
	}
	if !reflect.DeepEqual(src.marked, []string{"d1"}) {
		t.Errorf("marked %v, want only the jump's", src.marked)
	}
}

// The focused done card is polled away after one frame and the focus falls back to another done
// card: that one has had no frames of its own, one is not enough.
func TestSeenDwellFallbackStartsOver(t *testing.T) {
	m, src, _ := seenModel()
	m = frame(t, m)                   // d1: 1 frame
	src.staticSrc = src.staticSrc[1:] // d1 gone: the focus falls back to d2
	m = frame(t, m)
	if len(src.marked) != 0 {
		t.Errorf("d2 marked after one frame of its own: %v", src.marked)
	}
	frame(t, m)
	if !reflect.DeepEqual(src.marked, []string{"d2"}) {
		t.Errorf("d2 after two frames: %v", src.marked)
	}
}

// A done card that goes back to work after one frame and is done again starts over.
func TestSeenDwellNotDoneStartsOver(t *testing.T) {
	m, src, _ := seenModel()
	m = frame(t, m) // d1: 1 frame
	src.staticSrc[0].Done, src.staticSrc[0].Status = false, "working"
	m = frame(t, m)
	src.staticSrc[0].Done, src.staticSrc[0].Status = true, "idle"
	frame(t, m)
	if len(src.marked) != 0 {
		t.Errorf("marked after one frame of the new done: %v", src.marked)
	}
}

// While hidden, tmux is asked one question at a time: frames do not stack them behind a slow tmux.
func TestSeenAsksOneAtATime(t *testing.T) {
	m, r := stayModel(stayClients)
	m.src = &seenSrc{staticSrc: seenSessions()}
	m, _ = press(t, m, "q")
	tm, _ := m.Update(frameMsg{}) // asks; the answer has not come
	tm, cmd := tm.(Model).Update(frameMsg{})
	r.reset()
	run(&m, cmd)
	for _, c := range r.calls {
		if c[len(c)-1] == "#{session_attached}" {
			t.Errorf("asked again with a question out: %q", c)
		}
	}
}
