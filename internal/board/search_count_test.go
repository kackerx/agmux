package board

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kackerx/agmux/internal/store"
	"github.com/muesli/termenv"
)

// flipSrc has 9 sessions, then 10, then 9 ...: every read of the list is another one than the last,
// as a poll landing in the middle of a frame is.
type flipSrc struct {
	staticSrc
	n *int
}

func (c flipSrc) Sessions() []store.Session {
	*c.n++
	out := make([]store.Session, 9+*c.n%2)
	for i := range out {
		out[i] = store.Session{ID: fmt.Sprint("s", i), Name: fmt.Sprint("name", i), GroupKey: "/a", Status: "idle", ContextPct: -1}
	}
	return out
}

// A frame reads the session count once: the count in the bar, the room it leaves the text and the
// cursor all take the same one (the caret was a cell off for the frame where the total got one more
// figure).
func TestSearchCountReadOnceAFrame(t *testing.T) {
	n := 0
	m := New(flipSrc{n: &n}, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = tm.(Model)
	m.curOut = &cursorOut{}
	for _, k := range []tea.Msg{key("/"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("z", 60))}} { // no card matches
		tm, _ = m.Update(k)
		m = tm.(Model)
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	for frame := 0; frame < 3; frame++ { // whichever of the two the frame's reads start on
		row := strings.Split(m.View(), "\n")[0]
		i := strings.Index(row, "\x1b[7m")
		// the text fills the room the count left and one blank cell is between them: the count drawn
		// is the count the room was made for
		plain := strings.TrimRight(ansi.Strip(row), " ")
		if cw := ansi.StringWidth(plain[strings.LastIndex(plain, " ")+1:]) + 1; i < 0 || ansi.StringWidth(row[:i]) != m.w-cw-2 {
			t.Errorf("frame %d: the count is %d cells wide and the caret is not just before it: %q", frame, cw, row)
		}
		if want := fmt.Sprintf("\x1b[1;%dH", ansi.StringWidth(row[:max(i, 0)])+1); i < 0 || string(m.curOut.pos) != want {
			t.Errorf("frame %d: the caret is drawn at %d, the cursor is %q: %q", frame, ansi.StringWidth(row[:max(i, 0)]), m.curOut.pos, row)
		}
	}
}
