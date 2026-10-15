package board

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- T57: in pass-through the terminal's cursor goes to the pane's cursor in the preview ----

// The wrapper writes what it is given, byte for byte, and after it the position View asked for.
// It still has Fd() and Read: bubbletea measures the window and takes resizes from a term.File.
func TestCursorOutWrite(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var _ interface {
		io.ReadWriter
		Fd() uintptr
	} = &cursorOut{}
	c := &cursorOut{File: f}
	read := func() string { b, _ := os.ReadFile(f.Name()); return string(b) }
	if n, err := c.Write([]byte("frame\x1b[1m")); n != 9 || err != nil || read() != "frame\x1b[1m" {
		t.Errorf("no position: %d %v %q", n, err, read())
	}
	c.place(5, 2) // 0-based cell → 1-based escape: row 3, column 6
	if n, err := c.Write([]byte("next")); n != 4 || err != nil || read() != "frame\x1b[1mnext\x1b[3;6H" {
		t.Errorf("with a position: %d (what was written, not what was added) %v %q", n, err, read())
	}
	c.place(-1, -1)
	c.Write([]byte("!"))
	if got := read(); !strings.HasSuffix(got, "H!") {
		t.Errorf("a cleared position is not written: %q", got)
	}
}

// pass-through model with the pane's cursor at (col, row) of the capture.
func cursorModel(text string, cur *cursor) (Model, *cursorOut) {
	m := wide(&fakeRunner{}, 232, 50) // the preview is 92 wide: the cards get 140, 87 columns inside the box
	m.curOut = &cursorOut{}
	m.preview = true
	m.pv = previewMsg{pane: "%1", text: text, cur: cur}
	m.focus, m.pvFocus, m.pvID, m.pvPane = "a1", true, "a1", "%1" // (a focus that is not on a card is moved to one, and the scroll reset)
	return m, m.curOut
}

func msgsOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1) // a tick or a key queue may wait: what answers at once is what counts
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(50 * time.Millisecond):
		return nil
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice {
		var out []tea.Msg
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				out = append(out, msgsOf(c)...)
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

func has(msgs []tea.Msg, want tea.Msg) bool {
	for _, m := range msgs {
		if m == want {
			return true
		}
	}
	return false
}

// View puts the cursor on the pane's cursor cell: the cards' width, the header row, the preview's
// title and the box's border and padding all counted; on a wrapped line, where that cell landed.
func TestPassThroughPlacesCursor(t *testing.T) {
	for _, c := range []struct {
		name, text string
		cur        cursor
		want       string // the escape: 1-based row; column
	}{
		// column: 140 cards + 1 gap + "│ " = 143 (0-based) + the cell; row: header 0, title 1, border 2, text from 3
		{"first cell", "abc\n", cursor{row: 0, col: 0}, "\x1b[4;144H"},
		{"third line, fifth cell", "a\nb\nabcdef\n", cursor{row: 2, col: 4}, "\x1b[6;148H"},
		{"a bar, the same cell", "abc\n", cursor{row: 0, col: 2, bar: true}, "\x1b[4;146H"},
		{"the second row of a wrapped line", strings.Repeat("x", 90) + "\n", cursor{row: 0, col: 89}, "\x1b[5;146H"}, // 87 inside: cell 89 is the third of row two
	} {
		m, out := cursorModel(c.text, &c.cur)
		m.View()
		if got := string(out.pos); got != c.want {
			t.Errorf("%s: placed %q, want %q", c.name, got, c.want)
		}
	}
}

// Shown on the way into pass-through, hidden on the way out, and nothing for a cursor that isn't
// drawn: hidden by the pane, scrolled back, a preview that is closed.
func TestPassThroughShowsAndHidesCursor(t *testing.T) {
	m, out := cursorModel("abc\n", &cursor{row: 0, col: 1})
	m.pvFocus, m.curOn = false, false // browsing: the board's own look
	m.View()
	if out.pos != nil {
		t.Errorf("browsing: placed %q", out.pos)
	}
	tm, cmd := m.Update(key("tab"))
	m = tm.(Model)
	if !m.curOn || !has(msgsOf(cmd), tea.ShowCursor()) {
		t.Errorf("into pass-through: cursor on %v, msgs %v", m.curOn, msgsOf(cmd))
	}
	if tm, cmd = m.Update(frameMsg{}); !has(msgsOf(cmd), tea.ShowCursor()) && has(msgsOf(cmd), tea.HideCursor()) {
		t.Error("a frame in pass-through hides it")
	} else {
		m = tm.(Model)
	}
	m.View()
	if string(out.pos) != "\x1b[4;145H" {
		t.Errorf("placed %q", out.pos)
	}
	tm, cmd = m.Update(key("tab")) // back to the cards
	m = tm.(Model)
	if m.curOn || !has(msgsOf(cmd), tea.HideCursor()) || has(msgsOf(cmd), tea.ShowCursor()) {
		t.Errorf("out of pass-through: cursor on %v, msgs %v", m.curOn, msgsOf(cmd))
	}
	m.View()
	if out.pos != nil {
		t.Errorf("out of pass-through: still placing %q", out.pos)
	}
	for name, mod := range map[string]func(*Model){
		"hidden by the pane": func(m *Model) { m.pv.cur = nil },
		"scrolled back":      func(m *Model) { m.scroll = 2 },
		"preview closed":     func(m *Model) { m.preview = false },
		"cursor off screen":  func(m *Model) { m.pv.cur = &cursor{row: 99, col: 0} },
	} {
		m, out := cursorModel("abc\n", &cursor{row: 0, col: 1})
		mod(&m)
		m.View()
		if out.pos != nil || m.wantCursor() {
			t.Errorf("%s: placed %q, wanted %v", name, out.pos, m.wantCursor())
		}
	}
}

// The cursor moving alone must change the frame, or the renderer would not write it and the
// position would stay where it was: no block is drawn, but the cell is marked.
func TestPassThroughCursorMoveChangesFrame(t *testing.T) {
	a, _ := cursorModel("abcdef\n", &cursor{row: 0, col: 1})
	b, _ := cursorModel("abcdef\n", &cursor{row: 0, col: 2})
	if a.View() == b.View() {
		t.Error("the frame is the same with the cursor on another cell")
	}
	if strings.Contains(a.View(), "\x1b[7m") {
		t.Error("a block is drawn under the terminal's cursor")
	}
}
