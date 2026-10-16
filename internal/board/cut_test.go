package board

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

// ---- T61: the preview cuts a long line (T56's wrapping broke a TUI's boxes), and its width is configurable ----

// cutBox is a preview 40 wide (35 columns inside) of the capture text.
func cutBox(text string, cur *cursor, h, scroll int) []string {
	return cutBoxW(text, cur, 40, h, scroll)
}

func cutBoxW(text string, cur *cursor, w, h, scroll int) []string {
	s := store.Session{ID: "a", Name: "n", TmuxPane: "%1", Status: "idle", ContextPct: -1}
	return previewLines(s, true, previewMsg{pane: "%1", text: text, cur: cur}, w, h, scroll, false)
}

// inside is what the box shows between its borders, per row.
func inside(box []string) []string {
	var out []string
	for _, l := range box[2 : len(box)-1] {
		out = append(out, strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(ansi.Strip(l), " │ "), " │"), " "))
	}
	return out
}

// A line wider than the box is cut at its edge and goes no further: the next row is the next line
// of the pane, so a table or an input box keeps its shape. Every row keeps the box's width.
func TestPreviewCutsLongLine(t *testing.T) {
	box := cutBoxW(strings.Repeat("x", 150)+"\nnext\n", nil, 100, 12, 0) // a 150-column pane in a 100-wide preview
	got := inside(box)
	if got[0] != strings.Repeat("x", 95) || got[1] != "next" {
		t.Errorf("rows %q", got[:2])
	}
	for i, l := range box {
		if w := ansi.StringWidth(l); w != 100 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	// a wide character the edge would cut goes whole
	box = cutBox(strings.Repeat("a", 34)+"你好\nz\n", nil, 12, 0)
	if got := inside(box); got[0] != strings.Repeat("a", 34) || got[1] != "z" || ansi.StringWidth(box[2]) != 40 {
		t.Errorf("rows %q, %d wide", got[:2], ansi.StringWidth(box[2]))
	}
}

// Scrolling and the most it can go back count the pane's lines, however long they are.
func TestPreviewCutScrolls(t *testing.T) {
	var b strings.Builder
	for i := range 6 {
		fmt.Fprintf(&b, "L%d %s\n", i, strings.Repeat("-", 70)) // each is twice the box
	}
	rows := func(scroll int) string {
		var out []string
		for _, r := range inside(cutBox(b.String(), nil, 6, scroll)) { // 3 rows fit at h=6
			out = append(out, r[:2])
		}
		return strings.Join(out, " ")
	}
	for scroll, want := range map[int]string{0: "L3 L4 L5", 1: "L2 L3 L4", 2: "L1 L2 L3"} {
		if got := rows(scroll); got != want {
			t.Errorf("scrolled back %d: %q, want %q", scroll, got, want)
		}
	}
	m := wide(&fakeRunner{}, 232, 50)
	m, _ = press(t, m, "p")
	tm, _ := m.Update(previewMsg{pane: "%1", text: strings.Repeat("x", 300) + "\nshort\n"})
	if n := tm.(Model).pvLines; n != 2 {
		t.Errorf("pvLines %d, want the 2 lines of the capture", n)
	}
}

// The pane's color goes on across its lines and stops at the end of the text and at the border.
func TestPreviewCutKeepsColor(t *testing.T) {
	red := "\x1b[31m"
	box := cutBox(red+strings.Repeat("r", 80)+"\x1b[0m\nplain\n"+"\x1b[1;32m"+strings.Repeat("g", 40)+"\x1b[0;34m"+strings.Repeat("h", 40), nil, 12, 0)
	if strings.Contains(box[3], "\x1b[31m") {
		t.Errorf("the next line starts with the colour of the last: %q", box[3])
	}
	for _, l := range box[2 : len(box)-1] {
		if !strings.HasSuffix(l, "\x1b[0m "+fg(cDim, "│")) {
			t.Errorf("no reset before the border: %q", l)
		}
	}
}

// tmux ends a line without a reset when its last cell is colored and does not repeat the color on
// the next line: the color goes on, also from lines above the rows that show.
func TestPreviewColorCarriesAcrossLines(t *testing.T) {
	box := cutBox("first \x1b[41mred\nsecond\n\x1b[0mthird\n", nil, 12, 0)
	if !strings.Contains(box[3], "\x1b[41m") || !strings.Contains(box[4], "\x1b[0mthird") { // a reset right after the carried red
		t.Errorf("the next line is still red, and the one after a reset is not: %q %q", box[3], box[4])
	}
	var b strings.Builder
	b.WriteString("\x1b[44mblue starts here\n")
	for i := range 19 {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	box = cutBox(b.String(), nil, 12, 0) // the last 9 rows show; the blue was set far above them
	if got := inside(box); got[0] != "line10" || !strings.Contains(box[2], "\x1b[44m") {
		t.Errorf("first row %q: %q", got[0], box[2])
	}
}

// A line that ends with a background still on does not fill the padding after it, nor the border;
// the next line starts with it again.
func TestPreviewBackgroundStopsAtTheText(t *testing.T) {
	box := cutBox("\x1b[41mred\nnext\n", nil, 12, 0)
	if !strings.Contains(box[2], "red\x1b[0m    ") {
		t.Errorf("the padding is under the background: %q", box[2])
	}
	if !strings.Contains(box[3], "\x1b[41mnext") {
		t.Errorf("the next line lost it: %q", box[3])
	}
}

// The text of a box is previewInner wide: a line that long shows whole, one column more is cut.
func TestPreviewInnerWidth(t *testing.T) {
	w := previewW(240) // 96
	if n := previewInner(w); n != 91 {
		t.Fatalf("inner %d", n)
	}
	if rows := inside(cutBoxW(strings.Repeat("x", 91)+"\n", nil, w, 12, 0)); rows[0] != strings.Repeat("x", 91) {
		t.Errorf("exactly the width: %q", rows[0])
	}
	if rows := inside(cutBoxW(strings.Repeat("x", 92)+"y\n", nil, w, 12, 0)); rows[0] != strings.Repeat("x", 91) || rows[1] != "" {
		t.Errorf("one column more: %q", rows[:2])
	}
}

// [layout] preview_width is the preview's share, in percent, never under 60 columns; the cards get
// what is left.
func TestPreviewWidthConfig(t *testing.T) {
	defer useConfig(config.Default())
	for _, c := range []struct{ pct, w, want int }{
		{40, 253, 101}, {50, 253, 126}, {30, 253, 75}, {70, 253, 177}, {30, 150, 60}, {40, 150, 60}, {70, 150, 105},
	} {
		cfg := config.Default()
		cfg.PreviewWidth = c.pct
		useConfig(cfg)
		if got := previewW(c.w); got != c.want {
			t.Errorf("%d%% of %d: %d, want %d", c.pct, c.w, got, c.want)
		}
	}
	cfg := config.Default()
	cfg.PreviewWidth = 55
	useConfig(cfg)
	m := wide(&fakeRunner{}, 253, 50)
	m, _ = press(t, m, "p")
	if m.cardsW() != 253-previewW(253) || previewW(253) != 253*55/100 {
		t.Errorf("cards %d wide, preview %d", m.cardsW(), previewW(253))
	}
}
