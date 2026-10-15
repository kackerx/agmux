package board

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/store"
)

// ---- T56: a long pane line wraps in the preview box (35 columns inside a 40-wide one) ----

func wrapBox(text string, cur *cursor, h, scroll int) []string {
	s := store.Session{ID: "a", Name: "n", TmuxPane: "%1", Status: "idle", ContextPct: -1}
	return previewLines(s, true, previewMsg{pane: "%1", text: text, cur: cur}, 40, h, scroll, false)
}

// inside is what the box shows between its borders, per row.
func inside(box []string) []string {
	var out []string
	for _, l := range box[2 : len(box)-1] {
		out = append(out, strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(ansi.Strip(l), " │ "), " │"), " "))
	}
	return out
}

// 2×inner+3 columns take three rows and the end of the line is on screen; every row keeps the box's width.
func TestPreviewWrapsLongLine(t *testing.T) {
	long := strings.Repeat("a", 35) + strings.Repeat("b", 35) + "xyz"
	box := wrapBox(long+"\nnext\n", nil, 12, 0)
	got := inside(box)
	if got[0] != strings.Repeat("a", 35) || got[1] != strings.Repeat("b", 35) || got[2] != "xyz" || got[3] != "next" {
		t.Errorf("rows %q", got[:4])
	}
	for i, l := range box {
		if w := ansi.StringWidth(l); w != 40 {
			t.Errorf("line %d is %d wide: %q", i, w, l)
		}
	}
}

// The pane's color goes on across the rows of one line, and stops at its end and at the border.
func TestPreviewWrapKeepsColor(t *testing.T) {
	red := "\x1b[31m"
	box := wrapBox(red+strings.Repeat("r", 80)+"\x1b[0m\nplain\n"+"\x1b[1;32m"+strings.Repeat("g", 40)+"\x1b[0;34m"+strings.Repeat("h", 40), nil, 12, 0)
	if !strings.Contains(box[3], red) || !strings.Contains(box[4], red) { // the second row of the red line, and the third
		t.Errorf("colour lost on the continuation: %q %q", box[3], box[4])
	}
	if strings.Contains(box[5], "\x1b[31m") {
		t.Errorf("the next line starts with the colour of the last: %q", box[5])
	}
	if !strings.Contains(box[6], "\x1b[1;32m") || !strings.Contains(box[7], "\x1b[1;32m") || !strings.Contains(box[8], "\x1b[0;34m") || strings.Contains(box[8], "\x1b[1;32m") {
		t.Errorf("a colour that changes mid-line: %q %q %q", box[6], box[7], box[8])
	}
	for _, l := range box[2 : len(box)-1] { // the reset before the right border, whatever was on
		if !strings.HasSuffix(l, "\x1b[0m "+fg(cDim, "│")) {
			t.Errorf("no reset before the border: %q", l)
		}
	}
}

// A wide character that would be cut by the edge goes whole to the next row.
func TestPreviewWrapWide(t *testing.T) {
	box := wrapBox(strings.Repeat("a", 34)+"你好x\n", nil, 12, 0)
	got := inside(box)
	if got[0] != strings.Repeat("a", 34) || got[1] != "你好x" {
		t.Errorf("rows %q", got[:2])
	}
	for i, l := range box {
		if w := ansi.StringWidth(l); w != 40 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}

// Wrapping loses no cell: blanks at the start of a continued row stay, and a cursor after a wide
// character that was moved down finds its cell by the width of the rows, not by the box's width.
func TestPreviewWrapCellsKeep(t *testing.T) {
	trueColor(t)
	if got := inside(wrapBox(strings.Repeat("a", 35)+"  b\n", nil, 12, 0)); got[1] != "  b" {
		t.Errorf("blanks at the start of a row: %q", got[:2])
	}
	box := wrapBox(strings.Repeat("a", 34)+"你好x\n", &cursor{row: 0, col: 36}, 12, 0) // 你 is cells 34–35, 好 36–37
	if !strings.Contains(noResets(box[3]), "你"+noResets(lipgloss.NewStyle().Reverse(true).Render("好"))+"x") {
		t.Errorf("%q", box[3])
	}
}

// Scrolling counts rows as drawn: one scroll step moves one wrapped row.
func TestPreviewWrapScrolls(t *testing.T) {
	a, b := strings.Repeat("A", 70), strings.Repeat("B", 70) // two rows each; 3 rows fit at h=6
	text := a + "\n" + b + "\n"
	rows := func(scroll int) string { return strings.Join(inside(wrapBox(text, nil, 6, scroll)), "|") }
	A, B := strings.Repeat("A", 35), strings.Repeat("B", 35)
	if got := rows(0); got != A+"|"+B+"|"+B {
		t.Errorf("at the bottom: %q", got)
	}
	if got := rows(1); got != A+"|"+A+"|"+B {
		t.Errorf("one row back: %q", got)
	}
	// the most the board lets it go back is what the capture shows, counted in rows
	m := wide(&fakeRunner{}, 232, 50)
	m, _ = press(t, m, "p")
	tm, _ := m.Update(previewMsg{pane: "%1", text: strings.Repeat("x", 200) + "\nshort\n"}) // 87 columns inside: 3 rows + 1
	if n := tm.(Model).pvLines; n != 4 {
		t.Errorf("pvLines %d, want 4 rows", n)
	}
}

// The cursor goes with its cell: the second segment of a line, the cell after a full row, past the
// end of a wrapped line, and on a line below one that wrapped.
func TestPreviewWrapCursor(t *testing.T) {
	trueColor(t)
	rev := lipgloss.NewStyle().Reverse(true)
	line := strings.Repeat("a", 35) + "bcdefghij" + "\nz\n" // 44 columns: 35 + 9
	for _, c := range []struct {
		name string
		cur  cursor
		row  int // box row
		want string
	}{
		{"second row, fifth cell", cursor{row: 0, col: 39}, 1, "bcde" + rev.Render("f") + "ghij"},
		{"first cell of the second row", cursor{row: 0, col: 35}, 1, rev.Render("b") + "cdefghij"},
		{"last cell of the first", cursor{row: 0, col: 34}, 0, strings.Repeat("a", 34) + rev.Render("a")},
		{"past the end of a wrapped line", cursor{row: 0, col: 44}, 1, "bcdefghij" + rev.Render(" ")},
		{"on the line below", cursor{row: 1, col: 0}, 2, rev.Render("z")},
	} {
		box := wrapBox(line, &c.cur, 12, 0)
		if got := box[2+c.row]; !strings.Contains(noResets(got), noResets(c.want)) || strings.Count(got, "\x1b[7m") != 1 {
			t.Errorf("%s: %q, want it to hold %q", c.name, got, c.want)
		}
		if n := strings.Count(strings.Join(box, ""), "\x1b[7m"); n != 1 {
			t.Errorf("%s: %d reversed cells", c.name, n)
		}
	}
	// a cursor on the cell after a row that is full starts a row of its own below it
	box := wrapBox(strings.Repeat("a", 35)+"\nz\n", &cursor{row: 0, col: 35}, 12, 0)
	if got := inside(box); got[0] != strings.Repeat("a", 35) || got[1] != "" || got[2] != "z" || !strings.Contains(box[3], "\x1b[7m") {
		t.Errorf("rows %q, cursor row %q", got[:3], box[3])
	}
}

// No cursor mapped by col/inner alone: the rows above it that wrapped push it down.
func TestPreviewWrapCursorAfterWrappedLines(t *testing.T) {
	trueColor(t)
	text := strings.Repeat("a", 80) + "\n" + "hello\n" // 3 rows, then hello
	box := wrapBox(text, &cursor{row: 1, col: 1}, 12, 0)
	if !strings.Contains(noResets(box[2+3]), "h"+noResets(lipgloss.NewStyle().Reverse(true).Render("e"))+"llo") {
		t.Errorf("%q", box[2+3])
	}
}

// tmux ends a line without a reset when its last cell is colored and does not repeat the color on
// the next line: the color goes on, also from lines above the rows that show.
func TestPreviewWrapColorCarriesAcrossLines(t *testing.T) {
	box := wrapBox("first \x1b[41mred\nsecond\n\x1b[0mthird\n", nil, 12, 0)
	if !strings.Contains(box[3], "\x1b[41m") || !strings.Contains(box[4], "\x1b[0mthird") { // a reset right after the carried red
		t.Errorf("the next line is still red, and the one after a reset is not: %q %q", box[3], box[4])
	}
	var b strings.Builder
	b.WriteString("\x1b[44mblue starts here\n")
	for i := range 19 {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	box = wrapBox(b.String(), nil, 12, 0) // the last 9 rows show; the blue was set far above them
	if got := inside(box); got[0] != "line10" || !strings.Contains(box[2], "\x1b[44m") {
		t.Errorf("first row %q: %q", got[0], box[2])
	}
}

// The text of a box is previewInner wide, in every place that counts it: a capture of exactly
// that many columns is one row, one more is two.
func TestPreviewInnerWidth(t *testing.T) {
	w := previewW(240) // 96
	if n := previewInner(w); n != 91 {
		t.Fatalf("inner %d", n)
	}
	for text, want := range map[string]int{strings.Repeat("x", 91): 1, strings.Repeat("x", 92): 2} {
		m := wide(&fakeRunner{}, 240, 50)
		m, _ = press(t, m, "p")
		tm, _ := m.Update(previewMsg{pane: "%1", text: text + "\n"})
		if n := tm.(Model).pvLines; n != want {
			t.Errorf("%d columns: pvLines %d, want %d", len(text), n, want)
		}
		if rows := inside(previewBoxLines(text, w)); len(rows) < want || (want == 2 && rows[1] != "x") || (want == 1 && rows[1] != "") {
			t.Errorf("%d columns: rows %q", len(text), rows[:2])
		}
	}
}

func previewBoxLines(text string, w int) []string {
	s := store.Session{ID: "a", Name: "n", TmuxPane: "%1", Status: "idle", ContextPct: -1}
	return previewLines(s, true, previewMsg{pane: "%1", text: text + "\n"}, w, 12, 0, false)
}
