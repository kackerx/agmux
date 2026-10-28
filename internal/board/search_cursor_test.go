package board

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// T85 (board): the terminal's cursor is the caret of the / filter while the box has the keys: on the
// top bar, after " / " and the text before the caret (a Chinese character is two cells), cut the way
// the bar cuts a long text; hidden for a kept filter and everything else.
func TestSearchCursor(t *testing.T) {
	m := wide(&fakeRunner{}, 232, 50)
	m.curOut = &cursorOut{}
	step := func(k tea.Msg) []string {
		t.Helper()
		tm, cmd := m.Update(k)
		m = tm.(Model)
		return cursorKinds(cmd)
	}
	typed := func(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	at := func() string { m.View(); return string(m.curOut.pos) }

	if k := step(key("/")); len(k) != 1 || k[0] != "show" {
		t.Errorf("/ shows the cursor: %v", k)
	}
	if got := at(); got != "\x1b[1;4H" {
		t.Errorf("empty box: %q (row 1, after ' / ')", got)
	}
	if k := step(typed("ab")); len(k) != 0 {
		t.Errorf("typing: nothing to change: %v", k)
	}
	if got := at(); got != "\x1b[1;6H" {
		t.Errorf("ab: %q", got)
	}
	step(key("left"))
	if got := at(); got != "\x1b[1;5H" {
		t.Errorf("ab, left: %q", got)
	}
	step(key("home"))
	if got := at(); got != "\x1b[1;4H" {
		t.Errorf("home: %q", got)
	}
	step(key("ctrl+e"))
	step(typed("优惠"))
	if got := at(); got != "\x1b[1;10H" { // 3 + ab (2) + two Chinese (4) = column 9, 1-based 10
		t.Errorf("ab优惠: %q", got)
	}
	step(key("left"))
	if got := at(); got != "\x1b[1;8H" {
		t.Errorf("one Chinese character back: %q", got)
	}
	// kept: the keys are the board's again
	if k := step(key("enter")); len(k) != 1 || k[0] != "hide" {
		t.Errorf("enter keeps the filter: %v", k)
	}
	if got := at(); got != "" {
		t.Errorf("kept: placed %q", got)
	}
	// / edits it again, the caret at its end
	if k := step(key("/")); len(k) != 1 || k[0] != "show" {
		t.Errorf("/ again: %v", k)
	}
	if got := at(); got != "\x1b[1;10H" {
		t.Errorf("back in the box: %q", got)
	}
	// esc drops it
	if k := step(key("esc")); len(k) != 1 || k[0] != "hide" {
		t.Errorf("esc: %v", k)
	}
	if got := at(); got != "" {
		t.Errorf("dropped: placed %q", got)
	}
}

// A text longer than the bar has room for is cut from the left so that the caret stays in it, and
// the count on the right takes its share of the room.
func TestSearchCursorOfALongText(t *testing.T) {
	m := wide(&fakeRunner{}, 40, 20)
	m.curOut = &cursorOut{}
	press1 := func(k tea.Msg) {
		tm, _ := m.Update(k)
		m = tm.(Model)
	}
	press1(key("/"))
	press1(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", 60))})
	ss, _ := m.shown()
	room := m.searchRoom(len(ss))
	x, y, ok := m.searchCursor(len(ss))
	if !ok || y != 0 || x != 3+room-1 || x > m.w-2 {
		t.Errorf("x %d y %d ok %v (room %d)", x, y, ok, room)
	}
	m.View()
	if want := fmt.Sprintf("\x1b[1;%dH", x+1); string(m.curOut.pos) != want {
		t.Errorf("placed %q, want %q", m.curOut.pos, want)
	}
	// one blank cell between the text and the count
	if cw := ansi.StringWidth(m.searchCount(len(ss))); x != m.w-cw-2 {
		t.Errorf("x %d, the count starts at %d", x, m.w-cw)
	}
	// the cursor is on the cell the screen draws the caret in
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	row := strings.Split(m.View(), "\n")[0]
	if i := strings.Index(row, "\x1b[7m"); i < 0 || ansi.StringWidth(row[:i]) != x {
		t.Errorf("the caret is drawn at %d, the cursor at %d: %q", ansi.StringWidth(row[:max(i, 0)]), x, row)
	}
	// the count's width is part of it: "10/14 " takes a cell more than "3/14 "
	if m.searchRoom(10) != m.searchRoom(3)-1 {
		t.Errorf("room %d vs %d", m.searchRoom(10), m.searchRoom(3))
	}
}

// Not typing a filter: no cursor of its own (the pass-through cursor is T57's and is left alone).
func TestSearchCursorNotWhenNotTyping(t *testing.T) {
	m := wide(&fakeRunner{}, 232, 50)
	m.curOut = &cursorOut{}
	for _, k := range []string{"s", "H", "x"} { // flash, handoff prompt, kill prompt: other inputs
		tm, cmd := m.Update(key(k))
		if kinds := cursorKinds(cmd); len(kinds) != 0 {
			t.Errorf("%s: %v", k, kinds)
		}
		_ = tm
	}
	if _, _, ok := m.searchCursor(3); ok {
		t.Error("not searching: no cursor")
	}
	// before the first size nothing is drawn: no cursor
	fresh := New(&mutSrc{ss: sessions}, &fakeRunner{})
	fresh.curOut = &cursorOut{}
	fresh.searching = true
	if _, _, ok := fresh.searchCursor(3); ok || fresh.wantCursor() {
		t.Error("no size yet")
	}
	// in the recall screen the filter box is not on screen
	e, _ := newEmb(t, userOnly, paneEnv, false)
	e = openRecallOn(t, e)
	e.searching = true
	if _, _, ok := e.searchCursor(3); ok {
		t.Error("recall open: the board's box is not there")
	}
}
