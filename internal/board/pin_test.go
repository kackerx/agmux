package board

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/store"
)

// pinSrc is the store as far as pins go: TogglePin flips the session, the pin time ticking up.
type pinSrc struct {
	staticSrc
	n   int
	err error // TogglePin fails with it
}

func (s *pinSrc) Sessions() []store.Session { return s.staticSrc }
func (s *pinSrc) TogglePin(x store.Session) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	s.n++
	on := false
	for i := range s.staticSrc {
		if s.staticSrc[i].ID == x.ID {
			on = !s.staticSrc[i].Pinned
			s.staticSrc[i].Pinned = on
			s.staticSrc[i].PinnedAt = time.Unix(int64(s.n), 0)
		}
	}
	return on, nil
}

func pinModel() (Model, *pinSrc) {
	src := &pinSrc{staticSrc: staticSrc{
		{ID: "a", Name: "alpha", GroupKey: "/x", Project: "p2", TmuxPane: "%1", Status: "idle", ContextPct: -1},
		{ID: "b", Name: "bravo", GroupKey: "/x", Project: "p1", TmuxPane: "%2", Status: "idle", ContextPct: -1},
		{ID: "c", Name: "charlie", GroupKey: "/x", Project: "p1", TmuxPane: "%3", Status: "idle", ContextPct: -1},
		{ID: "z", Name: "zulu", GroupKey: "/y", Project: "p1", TmuxPane: "%4", Status: "idle", ContextPct: -1},
	}}
	m := New(src, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return tm.(Model), src
}

// order is the groups from left to right, each as its card names in order, a pinned one with a ▲.
func order(m Model) []string {
	ss, _ := m.shown()
	var out []string
	for _, g := range groups(ss) {
		var names []string
		for _, s := range g.sessions {
			if s.Pinned {
				s.Name = "▲" + s.Name
			}
			names = append(names, s.Name)
		}
		out = append(out, strings.Join(names, " "))
	}
	return out
}

// t pins the focused card to the top of its group: the ones pinned later go after the ones pinned
// before; t again puts it back among the rest, by name. The focus stays on the card.
func TestPinKey(t *testing.T) {
	m, _ := pinModel()
	want := func(w ...string) {
		t.Helper()
		if got := order(m); !slices.Equal(got, w) {
			t.Errorf("order %q, want %q", got, w)
		}
	}
	want("alpha bravo charlie", "zulu")
	m, _ = press(t, m, "j", "j", "t") // charlie
	want("▲charlie alpha bravo", "zulu")
	v := ansi.Strip(m.View())
	if m.focus != "c" || !strings.Contains(v, "pinned charlie") || !strings.Contains(v, "▲ charlie") || strings.Contains(v, "▲ alpha") {
		t.Errorf("focus %q, the mark or the message is missing:\n%s", m.focus, v)
	}
	m, _ = press(t, m, "j", "t") // the card under charlie is alpha
	want("▲charlie ▲alpha bravo", "zulu")
	m, _ = press(t, m, "j", "t") // bravo
	want("▲charlie ▲alpha ▲bravo", "zulu")
	m, _ = press(t, m, "k", "t") // alpha off: back among the rest
	want("▲charlie ▲bravo alpha", "zulu")
	if !strings.Contains(ansi.Strip(m.View()), "unpinned alpha") {
		t.Error("no unpinned message")
	}
	m, _ = press(t, m, "g") // project: p1 (bravo charlie zulu), p2 (alpha): pins stay first in their groups
	want("▲charlie ▲bravo zulu", "alpha")
	m, _ = press(t, m, "g", "g", "g") // back to cwd
	want("▲charlie ▲bravo alpha", "zulu")
}

// A pin that can't be written is said on the board; nothing moves.
func TestPinFailure(t *testing.T) {
	m, src := pinModel()
	src.err = errors.New("pins.json: not valid JSON")
	m, _ = press(t, m, "t")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "pins.json: not valid JSON") || strings.Contains(v, "▲") {
		t.Errorf("no error, or a mark:\n%s", v)
	}
}

// The name is drawn after the mark, and the flash labels and hits follow it.
func TestPinMarkMovesFlashHit(t *testing.T) {
	m, _ := pinModel()
	m, _ = press(t, m, "j", "j", "t") // charlie
	m, _ = press(t, m, "s", "c", "h")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	var hit *flashMatch
	for i := range m.flash.ms {
		if m.flash.ms[i].id == "c" && !m.flash.ms[i].title {
			hit = &m.flash.ms[i]
		}
	}
	if hit == nil {
		t.Fatalf("no hit on charlie: %+v", m.flash.ms)
	}
	row := string([]rune(lines[1+hit.y]))
	if !strings.Contains(row, "▲ ch") { // the hit is drawn over the name, not over the mark
		t.Errorf("hit at column %d: %q", hit.x, row)
	}
}

// Two presses of t read in one go: both Cmds are built from the same snapshot (not pinned), and
// run after both keys. The second must undo the first, as it would with a pause between them.
func TestPinBurst(t *testing.T) {
	p, _ := gridFixture(t, 0, "", "one", "two")
	p.Pins, _ = avatar.LoadBindings(filepath.Join(t.TempDir(), "pins.json"))
	m := gridModel(p, 120, 40)
	tm, c1 := m.Update(key("t"))
	tm, c2 := tm.Update(key("t"))
	m1, m2 := c1(), c2()
	if m1.(doneMsg).ok != "pinned one" || m2.(doneMsg).ok != "unpinned one" {
		t.Errorf("messages %q, %q", m1.(doneMsg).ok, m2.(doneMsg).ok)
	}
	tm, _ = tm.Update(m1)
	tm, _ = tm.Update(m2)
	if v := ansi.Strip(tm.(Model).View()); strings.Contains(v, "▲") || !strings.Contains(v, "unpinned one") {
		t.Errorf("two presses of t left the card pinned:\n%s", v)
	}
}

// The mark is in the focus color.
func TestPinMarkColor(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m, _ := pinModel()
	m, _ = press(t, m, "t")
	if v := m.View(); !strings.Contains(v, fg(cFocus, pinGlyph)) {
		t.Errorf("no ▲ in the focus color:\n%q", v)
	}
}

// A pinned name that has to be cut is cut two cells earlier, and flash gives no label to what
// the cut took off.
func TestPinFlashCutName(t *testing.T) {
	m, src := pinModel()
	_, rects := layout(groups(src.staticSrc), m.cardsW(), "", 0, m.now(), nil, nil)
	tw := rects[0].w - 2 - avatar.Width - 2 // the room right of the avatar
	name := strings.Repeat("n", tw-2) + "Q" // Q is the (tw-1)th cell: shown unpinned, cut with the mark
	src.staticSrc[0].Name = name
	m.focus = "a"
	m, _ = press(t, m, "t")
	if !src.staticSrc[0].Pinned {
		t.Fatal("not pinned")
	}
	m, _ = press(t, m, "s", "q")
	for _, f := range m.flash.ms {
		if f.id == "a" && !f.title {
			t.Errorf("a hit on the cut-off part of the name: %+v", f)
		}
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▲ nnn") || !strings.Contains(v, "n…┃") {
		t.Errorf("the name is not cut with an ellipsis after the mark:\n%s", v)
	}
}
