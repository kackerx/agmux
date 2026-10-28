package board

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/store"
)

func press1m(m recallModel, b tea.MouseButton, x, y int) (recallModel, tea.Cmd) {
	tm, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b})
	return tm.(recallModel), cmd
}

// A 140 × 30 screen: the query box is rows 0-2, the boxes start at row 3 (their first line of text is
// row 4), the list is columns 0-83 and the preview 84-139. The sessions, newest first: s-cart,
// p-api, s-ledger, s-gone.
const (
	row0 = 4 // the screen row of the first session
	lx   = 10
	px   = 100
)

type mclock struct{ at time.Time }

func mouseModel(t *testing.T, live ...store.Session) (recallModel, *mclock, *rworld) {
	w := newRWorld(t)
	w.fill()
	m := w.model(live...)
	c := &mclock{w.clock}
	m.now = func() time.Time { return c.at }
	return m, c, w
}

// A click selects the session under it and changes nothing else; in the input and in the kept state.
func TestRecallClickSelects(t *testing.T) {
	for _, locked := range []bool{false, true} {
		m, _, _ := mouseModel(t)
		m.locked = locked
		m, cmd := press1m(m, tea.MouseButtonLeft, lx, row0+2)
		if cmd != nil || m.sel != 2 || m.locked != locked || m.query != "" {
			t.Errorf("locked %v: sel %d locked %v cmd %v", locked, m.sel, m.locked, cmd != nil)
		}
		if r, _ := m.selected(); r.ID != "s-ledger" {
			t.Errorf("the third row is %s", r.ID)
		}
		// the edges of the row: the first and the last column inside the box
		if m, _ = press1m(m, tea.MouseButtonLeft, 1, row0); m.sel != 0 {
			t.Errorf("column 1: %d", m.sel)
		}
		if m, _ = press1m(m, tea.MouseButtonLeft, m.listW()-2, row0+3); m.sel != 3 {
			t.Errorf("last column: %d", m.sel)
		}
	}
	// a new row has its preview from the top, and the old message goes
	m, _, _ := mouseModel(t)
	m.pvScroll, m.msg = 5, "something"
	if m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1); m.pvScroll != 0 || m.msg != "" {
		t.Errorf("scroll %d msg %q", m.pvScroll, m.msg)
	}
}

// Where a click does nothing: the query box, the borders, the message line, the footer, the preview,
// under the last session; the other buttons and the other kinds of event.
func TestRecallClickElsewhereDoesNothing(t *testing.T) {
	m, _, _ := mouseModel(t)
	m.sel = 1
	bh := m.bodyH()
	for _, c := range []struct {
		name string
		x, y int
	}{
		{"query box", lx, 1}, {"its border", lx, 0}, {"top border of the list", lx, 3}, {"title", 4, 3},
		{"bottom border", lx, 3 + bh - 1}, {"left border", 0, row0 + 2}, {"right border", m.listW() - 1, row0 + 2},
		{"under the last session", lx, row0 + 4}, {"far below", lx, 3 + bh - 2}, {"message line", lx, 3 + bh}, {"footer", lx, 29},
		{"preview", px, row0 + 1}, {"preview border", m.listW(), row0 + 1},
	} {
		got, cmd := press1m(m, tea.MouseButtonLeft, c.x, c.y)
		if cmd != nil || got.sel != 1 || !got.clickAt.IsZero() {
			t.Errorf("%s (%d,%d): sel %d", c.name, c.x, c.y, got.sel)
		}
	}
	for _, b := range []tea.MouseButton{tea.MouseButtonRight, tea.MouseButtonMiddle} {
		if got, _ := press1m(m, b, lx, row0+2); got.sel != 1 {
			t.Errorf("button %v selected", b)
		}
	}
	for _, a := range []tea.MouseAction{tea.MouseActionRelease, tea.MouseActionMotion} {
		tm, _ := m.Update(tea.MouseMsg{X: lx, Y: row0 + 2, Action: a, Button: tea.MouseButtonLeft})
		if tm.(recallModel).sel != 1 {
			t.Errorf("action %v selected", a)
		}
	}
	// the panel open: nothing, wheel included
	m, _ = rpress(t, m, "ctrl+f")
	for _, b := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonWheelDown} {
		if got, _ := press1m(m, b, lx, row0+2); got.sel != 1 || !got.panel {
			t.Errorf("panel open, %v: sel %d", b, got.sel)
		}
	}
}

// The second click on the same row within 400 ms is enter on it: the clicked one, not the one that
// was selected; in either state.
func TestRecallDoubleClickResumes(t *testing.T) {
	for _, locked := range []bool{false, true} {
		m, _, w := mouseModel(t)
		m.locked = locked
		m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1) // p-api
		if RecallOutcome(m) != nil {
			t.Fatal("one click resumed")
		}
		m, cmd := press1m(m, tea.MouseButtonLeft, lx, row0+1)
		res := RecallOutcome(m)
		if cmd == nil || res == nil || res.Dir != w.dir("api") || res.Argv[0] != "pi" {
			t.Errorf("locked %v: cmd %v result %+v", locked, cmd != nil, res)
		}
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Error("the screen ends")
		}
		if m.locked != locked {
			t.Errorf("the state changed: %v", m.locked)
		}
	}
}

// What counts as a double click: the same row, 400 ms at most; the count starts again after one.
func TestRecallDoubleClickTiming(t *testing.T) {
	for _, c := range []struct {
		name   string
		second int // the row of the second click
		after  time.Duration
		double bool
	}{
		{"same row at once", 1, 0, true},
		{"same row at 400 ms", 1, 400 * time.Millisecond, true},
		{"same row at 401 ms", 1, 401 * time.Millisecond, false},
		{"another row at once", 0, 0, false},
		{"the row next to it", 2, 100 * time.Millisecond, false},
	} {
		m, clock, _ := mouseModel(t)
		m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1)
		clock.at = clock.at.Add(c.after)
		m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+c.second)
		if got := RecallOutcome(m) != nil; got != c.double {
			t.Errorf("%s: resumed %v", c.name, got)
		}
	}
	// a click that was not the second one is the first of the next pair
	m, clock, _ := mouseModel(t)
	m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1)
	clock.at = clock.at.Add(time.Second)
	m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1) // late: the first again
	if RecallOutcome(m) != nil {
		t.Fatal("late")
	}
	clock.at = clock.at.Add(100 * time.Millisecond)
	if m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1); RecallOutcome(m) == nil {
		t.Error("it paired with the late one")
	}
	// after a confirmation that did nothing (the directory is gone) the third click is a new first
	m, _, _ = mouseModel(t)
	m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+3)
	m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+3)
	if !strings.Contains(rview(m), "directory is gone") || RecallOutcome(m) != nil {
		t.Fatalf("gone:\n%s", rview(m))
	}
	if m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+3); strings.Contains(rview(m), "directory is gone") && !m.clickAt.IsZero() && m.msg != "" {
		t.Error("the third click confirmed again")
	}
	if m.msg != "" {
		t.Errorf("the click cleared the message: %q", m.msg)
	}
}

// The same guards as enter: a running session is said, not resumed; a window being made or a list that
// is not the query's yet: nothing.
func TestRecallDoubleClickGuards(t *testing.T) {
	live := store.Session{ID: "x1", SessionID: "s-cart", TmuxPane: "%5", Status: "working"}
	m, _, _ := mouseModel(t, live)
	m, _ = press1m(m, tea.MouseButtonLeft, lx, row0)
	m, _ = press1m(m, tea.MouseButtonLeft, lx, row0)
	if RecallOutcome(m) != nil || !strings.Contains(rview(m), "cart-fix is running") {
		t.Errorf("running:\n%s", rview(m))
	}
	for _, flag := range []string{"building", "searching"} {
		m, _, _ := mouseModel(t)
		m.building, m.searching = flag == "building", flag == "searching"
		m, _ = press1m(m, tea.MouseButtonLeft, lx, row0+1)
		m, cmd := press1m(m, tea.MouseButtonLeft, lx, row0+1)
		if cmd != nil || RecallOutcome(m) != nil || m.sel != 1 {
			t.Errorf("%s: cmd %v sel %d", flag, cmd != nil, m.sel)
		}
	}
}

// The wheel over the list moves the cursor a row, and stops at the ends; over the preview it
// scrolls three lines, to the ends; elsewhere nothing.
func TestRecallWheel(t *testing.T) {
	m, _, _ := mouseModel(t)
	for i, want := range []int{1, 2, 3, 3} {
		if m, _ = press1m(m, tea.MouseButtonWheelDown, lx, row0+i); m.sel != want {
			t.Errorf("wheel down %d: %d, want %d", i, m.sel, want)
		}
	}
	for i, want := range []int{2, 1, 0, 0} {
		if m, _ = press1m(m, tea.MouseButtonWheelUp, lx, row0); m.sel != want {
			t.Errorf("wheel up %d: %d, want %d", i, m.sel, want)
		}
	}
	// the preview's left border is the preview's, the list's right border its own
	if got, _ := press1m(m, tea.MouseButtonWheelDown, m.listW(), row0); got.sel != m.sel {
		t.Errorf("on the preview's border: the list moved to %d", got.sel)
	}
	if got, _ := press1m(m, tea.MouseButtonWheelDown, m.listW()-1, row0); got.sel != m.sel+1 {
		t.Errorf("on the list's border: %d", got.sel)
	}
	// anywhere in the list's box, borders and empty rows too
	if m, _ = press1m(m, tea.MouseButtonWheelDown, 0, 3); m.sel != 1 {
		t.Errorf("on the border: %d", m.sel)
	}
	// nothing under the other places: the query box (its bottom border too), the message line, the footer
	for _, c := range [][2]int{{lx, 1}, {lx, 2}, {px, 1}, {lx, 3 + m.bodyH()}, {lx, 29}} {
		if got, _ := press1m(m, tea.MouseButtonWheelDown, c[0], c[1]); got.sel != m.sel || got.pvScroll != 0 {
			t.Errorf("wheel at %v moved", c)
		}
	}
	// the preview: a long session to scroll
	w := newRWorld(t)
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf("u: sentence about bounds %d", i))
	}
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, lines...)
	p := rtype(t, w.model(), "bounds")
	r, _ := p.selected()
	max := p.previewScrollMax(r)
	if p, _ = press1m(p, tea.MouseButtonWheelDown, px, row0); p.pvScroll != 3 || p.sel != 0 {
		t.Errorf("over the preview: scroll %d sel %d", p.pvScroll, p.sel)
	}
	for range 40 {
		p, _ = press1m(p, tea.MouseButtonWheelDown, px, row0)
	}
	if p.pvScroll != max {
		t.Errorf("to the end: %d, the most is %d", p.pvScroll, max)
	}
	for range 40 {
		p, _ = press1m(p, tea.MouseButtonWheelUp, px, row0)
	}
	if p.pvScroll != 0 {
		t.Errorf("back to the top: %d", p.pvScroll)
	}
	// a narrow terminal has no preview: the wheel at the right is the list's
	n := resized(t, m, 99, 30)
	if got, _ := press1m(n, tea.MouseButtonWheelDown, 95, row0); got.sel != n.sel+1 {
		t.Errorf("no preview: %d", got.sel)
	}
}

// The list scrolled: the row under the click is counted from what is shown.
func TestRecallClickWhenTheListIsScrolled(t *testing.T) {
	w := newRWorld(t)
	for i := range 12 {
		w.claude(fmt.Sprintf("s%02d", i), w.dir("shopcart"), fmt.Sprintf("session-%02d", i), time.Duration(i+1)*time.Minute, fmt.Sprintf("u: sentence %d", i))
	}
	m := resized(t, w.model(), 140, 12) // 5 rows in the list
	m, _ = rpress(t, m, "down", "down", "down", "down", "down", "down", "down")
	if m.top == 0 {
		t.Fatal("not scrolled")
	}
	got, _ := press1m(m, tea.MouseButtonLeft, lx, row0+1)
	if r, _ := got.selected(); r.ID != fmt.Sprintf("s%02d", m.top+1) {
		t.Errorf("clicked %s with the top at %d", r.ID, m.top)
	}
	// the last row of the box
	if got, _ = press1m(m, tea.MouseButtonLeft, lx, row0+4); got.sel != m.top+4 {
		t.Errorf("last row: %d", got.sel)
	}
	if got, _ = press1m(m, tea.MouseButtonLeft, lx, row0+5); got.sel != m.sel { // the bottom border
		t.Errorf("border: %d", got.sel)
	}
}

// After the editor ($EDITOR on ctrl+o) the mouse is asked for again (bubbletea v1 turns it off).
func TestRecallMouseIsTurnedOnAgainAfterTheEditor(t *testing.T) {
	m, _, _ := mouseModel(t)
	if _, cmd := m.Update(recallEditedMsg{}); cmd == nil {
		t.Error("no command: the mouse stays off")
	}
}

// The board's ? screen takes the mouse too: a double click resumes like enter, the board under it
// does not move.
func TestBoardRecallMouse(t *testing.T) {
	m, e := newEmb(t, userOnly, paneEnv, false)
	m = openRecallOn(t, m)
	click := func(m Model, y int) (Model, bool) {
		tm, cmd := m.Update(tea.MouseMsg{X: lx, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		m = tm.(Model)
		return m, run(&m, cmd)
	}
	m, quit := click(m, row0+1)
	if quit || m.rc == nil || m.rc.sel != 1 {
		t.Fatalf("one click: quit %v", quit)
	}
	m, quit = click(m, row0+1)
	res := RecallOutcome(m)
	if !quit || res == nil || res.Dir != e.w.dir("api") {
		t.Errorf("two: quit %v result %+v", quit, res)
	}
	// in a popup it opens a window, as enter does; and one click only selects
	m, e = newEmb(t, userOnly, popupEnv, false)
	m = openRecallOn(t, m)
	m, _ = click(m, row0)
	if len(tmuxCalls(e.r, "new-window")) != 0 {
		t.Error("one click made a window")
	}
	m, _ = click(m, row0)
	if got := tmuxCalls(e.r, "new-window"); len(got) != 1 || !strings.Contains(strings.Join(got[0], " "), "s-cart") {
		t.Errorf("popup: %q", got)
	}
	// a window being made: a third and a fourth click make no second one
	m, e = newEmb(t, userOnly, popupEnv, false)
	m = openRecallOn(t, m)
	tm, _ := m.Update(tea.MouseMsg{X: lx, Y: row0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	tm, first := tm.Update(tea.MouseMsg{X: lx, Y: row0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if first == nil {
		t.Fatal("no window asked for")
	}
	for range 2 {
		tm, _ = tm.Update(tea.MouseMsg{X: lx, Y: row0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if _, again := tm.Update(tea.MouseMsg{X: lx, Y: row0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); again != nil {
			t.Error("a second window while the first is being made")
		}
	}
	// the wheel in the board's screen is the screen's, not the board's
	m, _ = newEmb(t, userOnly, paneEnv, false)
	m = openRecallOn(t, m)
	tm, _ = m.Update(tea.MouseMsg{X: lx, Y: row0, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if mm := tm.(Model); mm.rc == nil || mm.rc.sel != 1 {
		t.Error("the wheel did not reach the list")
	}
}
