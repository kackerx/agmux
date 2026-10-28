package board

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// editQuery: the caret moves, text goes in at the caret, backspace and the word delete work on what is
// before it; a Chinese character is one position. Each row is a start (text, caret), keys, the end.
func TestEditQuery(t *testing.T) {
	for _, c := range []struct {
		name string
		q    string
		pos  int
		keys []string
		want string
		wpos int
	}{
		{"left then insert", "abc", 3, []string{"left", "X"}, "abXc", 3},
		{"left stops at the start", "ab", 1, []string{"left", "left", "left", "X"}, "Xab", 1},
		{"right stops at the end", "ab", 1, []string{"right", "right", "right", "X"}, "abX", 3},
		{"home", "abc", 2, []string{"home", "X"}, "Xabc", 1},
		{"ctrl+a", "abc", 2, []string{"ctrl+a", "X"}, "Xabc", 1},
		{"end", "abc", 1, []string{"end", "X"}, "abcX", 4},
		{"ctrl+e", "abc", 0, []string{"ctrl+e", "X"}, "abcX", 4},
		{"backspace in the middle", "abc", 2, []string{"backspace"}, "ac", 1},
		{"backspace at the start", "abc", 0, []string{"backspace"}, "abc", 0},
		{"ctrl+h is backspace", "abc", 3, []string{"ctrl+h"}, "ab", 2},
		{"chinese is one position", "优惠券", 2, []string{"left", "X"}, "优X惠券", 2},
		{"chinese backspace", "优惠券", 2, []string{"backspace"}, "优券", 1},
		{"word before the caret", "fix the coupon bug", 18, []string{"alt+backspace"}, "fix the coupon ", 15},
		{"word with a trailing space", "fix the coupon ", 15, []string{"ctrl+w"}, "fix the ", 8},
		{"punctuation is between words", "cart.total-bug", 14, []string{"alt+backspace"}, "cart.total-", 11},
		{"punctuation only", "a ,.", 4, []string{"ctrl+w"}, "", 0},
		{"digits are in a word", "abc 123", 7, []string{"ctrl+w"}, "abc ", 4},
		{"underscore is in a word", "my_var", 6, []string{"ctrl+w"}, "", 0},
		{"chinese run is a word", "fix 优惠券计算", 10, []string{"alt+backspace"}, "fix ", 4},
		{"word delete mid text", "one two three", 7, []string{"ctrl+w"}, "one  three", 4},
		{"word delete at the start", "abc", 0, []string{"ctrl+w"}, "abc", 0},
		{"insert in the middle", "ac", 1, []string{"b", "d"}, "abdc", 3},
		{"space", "ab", 1, []string{"space"}, "a b", 2},
		{"paste with control characters", "", 0, []string{"a\tb\x1b[Ac"}, "a b [Ac", 7},
	} {
		q, pos := c.q, c.pos
		for _, k := range c.keys {
			var ok bool
			if q, pos, ok = editQuery(q, pos, key(k)); !ok {
				t.Errorf("%s: %s was not an editing key", c.name, k)
			}
		}
		if q != c.want || pos != c.wpos {
			t.Errorf("%s: %q caret %d, want %q caret %d", c.name, q, pos, c.want, c.wpos)
		}
	}
	// the caret itself stays in the text: it is what the screen draws
	if _, pos, _ := editQuery("ab", 2, key("right")); pos != 2 {
		t.Errorf("right at the end: caret %d", pos)
	}
	if _, pos, _ := editQuery("ab", 0, key("left")); pos != 0 {
		t.Errorf("left at the start: caret %d", pos)
	}
	// what is not the input's is left alone: enter, esc, an alt key, up
	for _, k := range []tea.KeyMsg{key("enter"), key("esc"), key("up"), key("down"), key("ctrl+f"), {Type: tea.KeyRunes, Runes: []rune("e"), Alt: true}} {
		if q, pos, ok := editQuery("abc", 1, k); ok || q != "abc" || pos != 1 {
			t.Errorf("%v: ok %v %q %d", k, ok, q, pos)
		}
	}
	// a caret that is out of the text (the text was cleared elsewhere) comes back in
	if q, pos, _ := editQuery("ab", 9, key("X")); q != "abX" || pos != 3 {
		t.Errorf("%q %d", q, pos)
	}
	if q, pos, _ := editQuery("", 5, key("backspace")); q != "" || pos != 0 {
		t.Errorf("%q %d", q, pos)
	}
}

func TestQueryCellsAndView(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	if got := queryCells("优a惠", 2); got != 3 {
		t.Errorf("cells before the caret: %d (a Chinese character is two)", got)
	}
	if got := queryCells("ab", 9); got != 2 {
		t.Errorf("a caret past the end is at the end: %d", got)
	}
	rev := "\x1b[7m"
	// the caret is a reversed cell: on the character it is before, a space at the end; none when not typing
	if v := queryView("abc", 1, true, 20); v != "a"+rev+"b\x1b[0m"+"c" {
		t.Errorf("middle: %q", v)
	}
	if v := queryView("abc", 3, true, 20); v != "abc"+rev+" \x1b[0m" {
		t.Errorf("end: %q", v)
	}
	if v := queryView("abc", 1, false, 20); v != "abc" {
		t.Errorf("not typing: %q", v)
	}
	// a wide character under the caret is all of it reversed
	if v := queryView("优惠", 0, true, 20); v != rev+"优\x1b[0m惠" {
		t.Errorf("wide: %q", v)
	}
	// too long for the room: the start goes so that the caret stays in
	long := strings.Repeat("a", 30) + "XY"
	if v := ansi.Strip(queryView(long, 31, true, 10)); !strings.HasSuffix(v, "XY") || ansi.StringWidth(v) > 10 {
		t.Errorf("long, caret at the end: %q", v)
	}
	if v := ansi.Strip(queryView(long, 0, true, 10)); !strings.HasPrefix(v, "aaa") {
		t.Errorf("long, caret at the start: %q", v)
	}
}

// The caret is a reversed cell on the top bar while typing; a kept filter (the board) has none.
func TestInputCaretIsDrawnWhileTyping(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	const rev = "\x1b[7m"
	top := func(v string) string { return strings.SplitN(v, "\n", 2)[0] }
	m, _ := press(t, wide(&fakeRunner{}, 232, 50), "/")
	m = typeKeys(t, m, "ab")
	m, _ = press(t, m, "left")
	if !strings.Contains(top(m.View()), "a"+rev+"b") {
		t.Errorf("typing: %q", top(m.View()))
	}
	if m, _ = press(t, m, "enter"); strings.Contains(top(m.View()), rev) {
		t.Errorf("kept filter has no caret: %q", top(m.View()))
	}
	w := newRWorld(t)
	w.fill()
	r := rtype(t, w.model(), "ab")
	in := func(v string) string { return strings.Split(v, "\n")[1] } // the query box's row
	if r, _ = rpress(t, r, "left"); !strings.Contains(in(r.View()), "a"+rev+"b") {
		t.Errorf("recall: %q", in(r.View()))
	}
}

// The board's / and the recall screen edit at the caret.
func TestSearchInputEditsAtTheCaret(t *testing.T) {
	r := &fakeRunner{}
	m, _ := press(t, wide(r, 232, 50), "/")
	m = typeKeys(t, m, "thre")
	m, _ = press(t, m, "left", "left", "X", "e")
	if m.query != "thXere" || m.qpos != 4 {
		t.Errorf("query %q caret %d", m.query, m.qpos)
	}
	m, _ = press(t, m, "home", "Z", "end", "!")
	if m.query != "ZthXere!" || m.qpos != 8 {
		t.Errorf("home / end: %q caret %d", m.query, m.qpos)
	}
	m, _ = press(t, m, "alt+backspace")
	if m.query != "" || !m.searching {
		t.Errorf("the word is deleted, the box stays: %q searching %v", m.query, m.searching)
	}
	// the filter that was kept is edited from its end
	m = typeKeys(t, m, "ab")
	m, _ = press(t, m, "enter", "/")
	m, _ = press(t, m, "X")
	if m.query != "abX" {
		t.Errorf("reopened: %q", m.query)
	}
}

func TestRecallInputEditsAtTheCaret(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := rtype(t, w.model(), "cupon")
	m, _ = rpress(t, m, "left", "left", "left", "left", "o")
	if m.query != "coupon" || m.qpos != 2 {
		t.Fatalf("query %q caret %d", m.query, m.qpos)
	}
	if len(m.results) == 0 || m.results[0].ID != "s-cart" {
		t.Errorf("the list follows the text: %q", rids(m))
	}
	m, _ = rpress(t, m, "home", "ctrl+e", "ctrl+w")
	if m.query != "" || len(m.results) != 4 {
		t.Errorf("word delete clears it: %q, %d results", m.query, len(m.results))
	}
	// moving the caret is not a new search
	m = rtype(t, m, "ab")
	gen := m.qgen
	if m, _ = rpress(t, m, "left", "right", "home"); m.qgen != gen {
		t.Error("the caret moved: the list was asked for again")
	}
	// a query given on the command line starts with the caret at its end
	if got := newRecallModel(w.ix, m.cfg, nil, "", "优惠").qpos; got != 2 {
		t.Errorf("start caret %d", got)
	}
}
