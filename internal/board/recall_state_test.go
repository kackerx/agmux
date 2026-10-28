package board

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// T80: the input and the kept query are two states. Rows: a state (typing / locked), a key, what it
// does — the state after it, whether the screen ended, and whether a session was chosen.
func TestRecallTwoStates(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	empty := func() recallModel { return w.model() }
	typing := func() recallModel { return rtype(t, w.model(), "o") } // a query that matches several
	locked := func() recallModel {
		m, _ := rpress(t, typing(), "enter")
		return m
	}
	for _, c := range []struct {
		name   string
		start  func() recallModel
		keys   []string
		locked bool   // the state after
		query  string // the query after
		quit   bool
		chose  bool
	}{
		{"typing: enter keeps the query, resumes nothing", typing, []string{"enter"}, true, "o", false, false},
		{"typing: enter with no query keeps it too", empty, []string{"enter"}, true, "", false, false},
		{"typing: esc clears the query, stays", typing, []string{"esc"}, false, "", false, false},
		{"typing: esc on an empty query ends", empty, []string{"esc"}, false, "", true, false},
		{"typing: n and j are text", typing, []string{"n", "N", "j", "k", "/"}, false, "onNjk/", false, false},
		{"typing: the arrows choose", typing, []string{"down", "up", "down"}, false, "o", false, false},
		{"locked: enter resumes", locked, []string{"enter"}, true, "o", true, true},
		{"locked: / is the input again, the query kept", locked, []string{"/"}, false, "o", false, false},
		{"locked: esc clears the query and gives the input back", locked, []string{"esc"}, false, "", false, false},
		{"locked: esc, esc ends", locked, []string{"esc", "esc"}, false, "", true, false},
		{"locked: letters are not text", locked, []string{"x", "y", "z"}, true, "o", false, false},
		{"locked: q ends the screen", locked, []string{"q"}, true, "o", true, false},
		{"locked then / then enter locks again", locked, []string{"/", "enter"}, true, "o", false, false},
	} {
		m, quit := rpress(t, c.start(), c.keys...)
		if m.locked != c.locked || m.query != c.query || quit != c.quit || (RecallOutcome(m) != nil) != c.chose {
			t.Errorf("%s: locked %v query %q quit %v chose %v; want %v %q %v %v", c.name, m.locked, m.query, quit, RecallOutcome(m) != nil, c.locked, c.query, c.quit, c.chose)
		}
	}
	// n / N come round at the ends, j / k stop there
	m, _ := rpress(t, rtype(t, w.model(), ""), "enter")
	if len(m.results) != 4 {
		t.Fatalf("%d results", len(m.results))
	}
	for _, c := range []struct {
		keys []string
		sel  int
	}{
		{[]string{"N"}, 3}, {[]string{"N", "n"}, 0}, {[]string{"n"}, 1}, {[]string{"n", "n", "n", "n"}, 0},
		{[]string{"j", "j", "j", "j", "j"}, 3}, {[]string{"k"}, 0}, {[]string{"j", "k", "k", "k"}, 0}, {[]string{"down", "down", "up"}, 1},
	} {
		if got, _ := rpress(t, m, c.keys...); got.sel != c.sel {
			t.Errorf("%v: cursor %d, want %d", c.keys, got.sel, c.sel)
		}
	}
	if got, _ := rpress(t, m, "N", "N"); got.sel != 2 || got.pvScroll != 0 {
		t.Errorf("N N: %d", got.sel)
	}
	// the caret is at the end of the query when / gives the input back; a new row has its preview from the top
	m2, _ := rpress(t, locked(), "/")
	if m2.qpos != 1 {
		t.Errorf("caret after /: %d", m2.qpos)
	}
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf("u: sentence about bounds %d", i))
	}
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, lines...)
	w.claude("s-two", w.dir("shopcart"), "two", 2*time.Hour, "u: more bounds here")
	m3, _ := rpress(t, rtype(t, w.model(), "bounds"), "enter", "ctrl+n")
	if m3.pvScroll != 3 {
		t.Fatalf("the preview scrolls in the kept state too: %d", m3.pvScroll)
	}
	if got, _ := rpress(t, m3, "n"); got.sel == m3.sel || got.pvScroll != 0 {
		t.Errorf("n: row %d→%d, preview %d", m3.sel, got.sel, got.pvScroll)
	}
	// nothing found: n / N do nothing
	m, _ = rpress(t, rtype(t, w.model(), "zzzzqqq"), "enter")
	if got, _ := rpress(t, m, "n", "N", "j", "k"); got.sel != 0 || !got.locked {
		t.Errorf("empty list: %d locked %v", got.sel, got.locked)
	}
	// the keys that are not letters work in both states: copy, the filter panel, the preview
	copied := 0
	for _, start := range []func() recallModel{typing, locked} {
		m := start()
		m.copyToClip = func(string) error { copied++; return nil }
		m, _ = rpress(t, m, "ctrl+y")
		if m, _ = rpress(t, m, "ctrl+f"); !m.panel {
			t.Error("ctrl+f opens the panel")
		}
		if m, _ = rpress(t, m, "esc"); m.panel || m.locked != (m.query != "" && m.locked) {
			t.Errorf("esc closes the panel and nothing else: %v", m.locked)
		}
	}
	if copied != 2 {
		t.Errorf("copied %d times", copied)
	}
	// the panel closed by esc leaves the state as it was
	m = locked()
	if m, _ = rpress(t, m, "ctrl+f", "esc"); !m.locked || m.query != "o" {
		t.Errorf("locked, panel, esc: %v %q", m.locked, m.query)
	}
}

// agmux -r <words> starts with the query kept; with none, in the input.
func TestRecallStartsLockedWithAQuery(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	if got := newRecallModel(w.ix, m.cfg, nil, "", "cart"); !got.locked || got.qpos != 4 {
		t.Errorf("with a query: locked %v caret %d", got.locked, got.qpos)
	}
	if got := newRecallModel(w.ix, m.cfg, nil, "", ""); got.locked {
		t.Error("with none: the input")
	}
	// and the first enter then resumes
	k, cmd := newRecallModel(w.ix, m.cfg, nil, "", "'coupon").search(false)
	k.lookPath, k.indexing = m.lookPath, false
	k, _ = rfeed(k, cmd)
	if k, quit := rpress(t, k, "enter"); !quit || RecallOutcome(k) == nil {
		t.Errorf("enter after -r words: quit %v", quit)
	}
}

// The two states in the board's ? screen: ? leaves from the kept query too, and esc goes back a
// step at a time.
func TestBoardRecallTwoStates(t *testing.T) {
	m, _ := newEmb(t, userOnly, paneEnv, false)
	m = openRecallOn(t, m)
	m = btype(t, m, "cart")
	if m, _ = bpress(t, m, "?"); m.rc == nil || m.rc.query != "cart?" {
		t.Fatalf("? after text is text: %v", m.rc)
	}
	m, _ = bpress(t, m, "backspace", "enter")
	if m.rc == nil || !m.rc.locked || m.rc.query != "cart" {
		t.Fatalf("kept: %+v", m.rc)
	}
	if m, _ = bpress(t, m, "?"); m.rc != nil {
		t.Error("? from the kept query goes back to the board")
	}
	m = openRecallOn(t, m)
	m = btype(t, m, "cart")
	m, quit := bpress(t, m, "enter", "esc")
	if m.rc == nil || m.rc.locked || m.rc.query != "" || quit {
		t.Fatalf("esc from the kept query: input again, empty: %+v", m.rc)
	}
	if m, quit = bpress(t, m, "esc"); m.rc != nil || quit {
		t.Error("esc on an empty input is back to the board")
	}
	if !strings.Contains(ansiStrip(m.View()), "sessions") {
		t.Error("the board")
	}
	// enter resumes only from the kept query
	m = openRecallOn(t, m)
	m = btype(t, m, "coupon")
	if _, quit = bpress(t, m, "enter"); quit {
		t.Error("the first enter resumed")
	}
	if m, quit = bpress(t, m, "enter", "enter"); !quit || RecallOutcome(m) == nil {
		t.Errorf("the second one does: quit %v", quit)
	}
}
