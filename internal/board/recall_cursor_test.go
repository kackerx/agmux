package board

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// cursorKind is what a cursor command (tea.ShowCursor / HideCursor) is, "" for anything else; a
// Batch is looked into.
func cursorKinds(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if v, ok := msg.(tea.BatchMsg); ok {
		var out []string
		for _, c := range v {
			out = append(out, cursorKinds(c)...)
		}
		return out
	}
	switch name := fmt.Sprintf("%T", msg); {
	case strings.HasSuffix(name, "showCursorMsg"):
		return []string{"show"}
	case strings.HasSuffix(name, "hideCursorMsg"):
		return []string{"hide"}
	}
	return nil
}

// T85: the terminal's cursor goes to the caret of the query box (row 1; the border, " / " and the
// text before the caret in front of it) so that an input method puts its candidates there. A
// Chinese character is two cells; a query longer than the box is shown from where the caret fits.
func TestRecallCursorAt(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	at := func(m recallModel) (int, int, bool) { return m.cursorAt() }
	m := w.model()
	if x, y, ok := at(m); !ok || x != 4 || y != 1 {
		t.Errorf("empty: %d,%d %v", x, y, ok)
	}
	m = rtype(t, m, "abc")
	if x, _, _ := at(m); x != 7 {
		t.Errorf("abc: %d", x)
	}
	m, _ = rpress(t, m, "left")
	if x, _, _ := at(m); x != 6 {
		t.Errorf("abc, left: %d", x)
	}
	m, _ = rpress(t, m, "home")
	if x, _, _ := at(m); x != 4 {
		t.Errorf("home: %d", x)
	}
	// Chinese: two cells each, and the caret between them
	m = rtype(t, w.model(), "优惠券")
	if x, _, _ := at(m); x != 4+6 {
		t.Errorf("优惠券: %d", x)
	}
	if m, _ = rpress(t, m, "left"); true {
		if x, _, _ := at(m); x != 4+4 {
			t.Errorf("优惠券, left: %d", x)
		}
	}
	m = rtype(t, w.model(), "a优b")
	if m, _ = rpress(t, m, "left"); true {
		if x, _, _ := at(m); x != 4+3 {
			t.Errorf("a优|b: %d", x)
		}
	}
	// a long query: the box shows it from where the caret fits, the cursor is at the end of what is shown
	narrow := resized(t, w.model(), 40, 20)
	room := narrow.inputRoom()
	long := rtype(t, narrow, strings.Repeat("x", 100))
	if x, _, ok := long.cursorAt(); !ok || x != 4+room-1 {
		t.Errorf("long, caret at the end: %d (room %d)", x, room)
	}
	if x := 4 + room - 1; x > 40-2 {
		t.Errorf("the cursor is outside the box: %d", x)
	}
	if long, _ = rpress(t, long, "home"); true {
		if x, _, _ := long.cursorAt(); x != 4 {
			t.Errorf("long, home: %d", x)
		}
	}
	// the progress takes room from the query while the index is built
	idx := resized(t, w.model(), 40, 20)
	idx.indexing = true
	if idx.inputRoom() >= narrow.inputRoom() {
		t.Errorf("room while indexing %d, after %d", idx.inputRoom(), narrow.inputRoom())
	}
	// not where the keys are the list's or the panel's, and not before there is a size
	locked := rtype(t, w.model(), "ab")
	locked, _ = rpress(t, locked, "enter")
	if _, _, ok := at(locked); ok {
		t.Error("kept query: the cursor is hidden")
	}
	if locked, _ = rpress(t, locked, "/"); true {
		if _, _, ok := at(locked); !ok {
			t.Error("/ gives the input back: the cursor shows")
		}
	}
	if panel, _ := rpress(t, w.model(), "ctrl+f"); true {
		if _, _, ok := at(panel); ok {
			t.Error("panel open: hidden")
		}
	}
	if _, _, ok := (recallModel{}).cursorAt(); ok {
		t.Error("no size yet")
	}
}

// The standalone screen shows and hides the cursor as the input has and loses the keys, and puts it
// where the caret is when it draws; in the board that is the board's to do.
func TestRecallPutsTheCursor(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m.curOut = &cursorOut{}
	update := func(m recallModel, msg tea.Msg) (recallModel, []string) {
		tm, cmd := m.Update(msg)
		return tm.(recallModel), cursorKinds(cmd)
	}
	var kinds []string
	if m, kinds = update(m, tea.WindowSizeMsg{Width: 140, Height: 30}); len(kinds) != 1 || kinds[0] != "show" {
		t.Errorf("the input has the keys: %v", kinds)
	}
	m.View()
	if got := string(m.curOut.pos); got != "\x1b[2;5H" {
		t.Errorf("placed %q", got)
	}
	if m, kinds = update(m, key("a")); len(kinds) != 0 {
		t.Errorf("typing: nothing to change: %v", kinds)
	}
	m.View()
	if got := string(m.curOut.pos); got != "\x1b[2;6H" {
		t.Errorf("after a: %q", got)
	}
	if m, kinds = update(m, key("enter")); len(kinds) != 1 || kinds[0] != "hide" {
		t.Errorf("kept: %v", kinds)
	}
	m.View()
	if m.curOut.pos != nil {
		t.Errorf("kept: placed %q", m.curOut.pos)
	}
	if m, kinds = update(m, key("/")); len(kinds) != 1 || kinds[0] != "show" {
		t.Errorf("/: %v", kinds)
	}
	if m, kinds = update(m, key("ctrl+f")); len(kinds) != 1 || kinds[0] != "hide" {
		t.Errorf("panel: %v", kinds)
	}
	if _, kinds = update(m, key("esc")); len(kinds) != 1 || kinds[0] != "show" {
		t.Errorf("panel closed: %v", kinds)
	}
	// where it does not own the terminal it says nothing
	free := w.model()
	if _, kinds = update(free, tea.WindowSizeMsg{Width: 140, Height: 30}); len(kinds) != 0 {
		t.Errorf("no curOut: %v", kinds)
	}
	// started with words (-r words): the keys are the list's from the start
	kept := newRecallModel(w.ix, w.model().cfg, nil, "", "cart")
	kept.curOut = &cursorOut{}
	if _, kinds = update(kept, tea.WindowSizeMsg{Width: 140, Height: 30}); len(kinds) != 0 {
		t.Errorf("-r words: %v", kinds)
	}
}

// Run for real on a pipe: the cursor is shown and put on the caret, and nothing is written after
// the alt screen is left.
func TestRecallProgramWritesTheCursor(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m.curOut = &cursorOut{}
	r, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	m.curOut = &cursorOut{File: wr}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(m.curOut), tea.WithInput(nil), tea.WithoutSignalHandler())
	done := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); done <- b }()
	go func() {
		time.Sleep(100 * time.Millisecond)
		p.Send(tea.WindowSizeMsg{Width: 140, Height: 30})
		time.Sleep(100 * time.Millisecond)
		p.Send(key("a"))
		time.Sleep(300 * time.Millisecond)
		p.Quit()
	}()
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	wr.Close()
	out := string(<-done)
	if !strings.Contains(out, "\x1b[?25h") || !strings.Contains(out, "\x1b[2;6H") {
		t.Errorf("no show / position in %q", out)
	}
	if i := strings.LastIndex(out, "\x1b[?1049l"); i < 0 || strings.Contains(out[i:], "H") {
		t.Errorf("after the alt screen: %q", out)
	}
}

// In the board's ? screen the board puts the cursor: shown when the recall screen is open and the
// input has the keys, hidden when it is kept or the board is back.
func TestBoardPutsTheCursorForTheRecallScreen(t *testing.T) {
	m, _ := newEmb(t, userOnly, paneEnv, false)
	m.curOut = &cursorOut{}
	tm, cmd := m.Update(key("?"))
	m = tm.(Model)
	if k := cursorKinds(cmd); len(k) != 1 || k[0] != "show" {
		t.Errorf("? opens the screen: %v", k)
	}
	m.View()
	if got := string(m.curOut.pos); got != "\x1b[2;5H" {
		t.Errorf("placed %q", got)
	}
	tm, cmd = m.Update(key("e"))
	m = tm.(Model)
	m.View()
	if got := string(m.curOut.pos); got != "\x1b[2;6H" || len(cursorKinds(cmd)) != 0 {
		t.Errorf("after e: %q %v", got, cursorKinds(cmd))
	}
	tm, cmd = m.Update(key("enter"))
	m = tm.(Model)
	if k := cursorKinds(cmd); len(k) != 1 || k[0] != "hide" {
		t.Errorf("kept: %v", k)
	}
	m.View()
	if m.curOut.pos != nil {
		t.Errorf("kept: placed %q", m.curOut.pos)
	}
	tm, _ = m.Update(key("/"))
	m = tm.(Model)
	m.View()
	if m.curOut.pos == nil {
		t.Error("/: back to the input")
	}
	// leaving: the board has no use for it
	tm, _ = m.Update(key("esc")) // clears
	tm, cmd = tm.Update(key("esc"))
	m = tm.(Model)
	if m.rc != nil {
		t.Fatal("not back")
	}
	if k := cursorKinds(cmd); len(k) != 1 || k[0] != "hide" {
		t.Errorf("back on the board: %v", k)
	}
	m.View()
	if m.curOut.pos != nil {
		t.Errorf("board: placed %q", m.curOut.pos)
	}
}
