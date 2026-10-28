package board

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

func mustRows(t *testing.T, m recallModel, keys ...string) recallModel {
	t.Helper()
	m, _ = rpress(t, m, keys...)
	return m
}

func resized(t *testing.T, m recallModel, w, h int) recallModel {
	t.Helper()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return tm.(recallModel)
}

// T82: the query, the list and the preview are each in a thick box; the list's title is where the
// cursor is, the preview's the session; narrow terminals have no preview.
func TestRecallThreeBoxes(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	lines := strings.Split(rview(m), "\n")
	// the query box: no title, three rows
	if !strings.HasPrefix(lines[0], "┏━━") || !strings.HasPrefix(lines[1], "┃ / ") || !strings.HasPrefix(lines[2], "┗━━") || strings.Contains(lines[0], " ") {
		t.Errorf("the query box:\n%s", strings.Join(lines[:3], "\n"))
	}
	// the list and the preview side by side under it, titles in the top border
	if !strings.HasPrefix(lines[3], "┏━ 4/4 ━") || !strings.Contains(lines[3], "┓┏━ cart-fix ━") || !strings.HasSuffix(lines[3], "┓") {
		t.Errorf("titles: %q", lines[3])
	}
	if last := lines[len(lines)-3]; !strings.HasPrefix(last, "┗━") || strings.Count(last, "┛") != 2 {
		t.Errorf("the bottom of the boxes: %q", last)
	}
	// the cursor moves the preview's session, not the number: that is how many sessions the query found
	m, _ = rpress(t, m, "down", "down")
	if l := strings.Split(rview(m), "\n")[3]; !strings.HasPrefix(l, "┏━ 4/4 ━") || !strings.Contains(l, "┏━ audit export") {
		t.Errorf("titles after down down: %q", l)
	}
	if l := strings.Split(rview(rtype(t, w.model(), "'coupon")), "\n")[3]; !strings.HasPrefix(l, "┏━ 1/4 ━") {
		t.Errorf("one of four found: %q", l)
	}
	// a scope or a ticked-off source narrows what there is to search: found of that
	if l := strings.Split(rview(mustRows(t, rtype(t, w.model(), "'export"), "ctrl+f", "down", "space", "esc")), "\n")[3]; !strings.HasPrefix(l, "┏━ 1/3 ━") {
		t.Errorf("a source ticked off: %q", l)
	}
	// nothing found: 0 of the 4 there are, and no preview title
	m = rtype(t, m, "zzzzqqq")
	if l := strings.Split(rview(m), "\n")[3]; !strings.HasPrefix(l, "┏━ 0/4 ━") || strings.Count(l, "┏━ ") != 1 && strings.Contains(l, "cart") {
		t.Errorf("empty: %q", l)
	}
	// narrow: two boxes
	n := resized(t, w.model(), 99, 30)
	if v := rview(n); strings.Count(v, "┏") != 2 || strings.Count(v, "┗") != 2 {
		t.Errorf("99 columns:\n%s", v)
	}
	// the live state is in the preview's title
	if l := strings.Split(rview(w.model(store.Session{SessionID: "s-cart", TmuxPane: "%5", Status: "working"})), "\n")[3]; !strings.Contains(l, "cart-fix · ") {
		t.Errorf("live: %q", l)
	}
	// a title longer than its box is cut
	long := newRWorld(t)
	long.claude("s-l", long.dir("shopcart"), strings.Repeat("name", 30), time.Hour, "u: hello")
	if l := strings.Split(rview(resized(t, long.model(), 100, 20)), "\n")[3]; !strings.Contains(l, "…") || ansi.StringWidth(l) != 100 {
		t.Errorf("long title: %q (%d)", l, ansi.StringWidth(l))
	}
}

// Whatever the size of the terminal: every line is as wide as it, the screen is as tall as it (a low
// one loses the message line, then the boxes) and never taller.
func TestRecallFitsAnyTerminal(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	for _, width := range []int{20, 40, 60, 99, 100, 101, 140, 232} {
		for h := 1; h <= 40; h++ {
			m := resized(t, w.model(), width, h)
			lines := strings.Split(rview(m), "\n")
			if len(lines) > h || h >= 7 && len(lines) != h {
				t.Fatalf("%dx%d: %d lines", width, h, len(lines))
			}
			for i, l := range lines {
				if ansi.StringWidth(l) > width {
					t.Fatalf("%dx%d: line %d is %d wide: %q", width, h, i, ansi.StringWidth(l), l)
				}
			}
			if last := lines[len(lines)-1]; !strings.HasPrefix(last, " enter keep") {
				t.Errorf("%dx%d: the footer is not last: %q", width, h, last)
			}
			if h >= 8 && width >= 60 && strings.Count(rview(m), "┗") < 2 {
				t.Errorf("%dx%d: the boxes are not whole", width, h)
			}
			if rows := m.listRows(); rows < 1 || h >= 8 && rows != h-5-2 {
				t.Errorf("%dx%d: list rows %d", width, h, rows)
			}
		}
	}
	// the message line is the first to go
	if m := resized(t, w.model(), 100, 8); !m.msgRow() || m.bodyH() != 3 {
		t.Errorf("8 rows: msg %v body %d", m.msgRow(), m.bodyH())
	}
	if m := resized(t, w.model(), 100, 7); m.msgRow() || m.bodyH() != 3 {
		t.Errorf("7 rows: msg %v body %d", m.msgRow(), m.bodyH())
	}
	if m := resized(t, w.model(), 100, 6); m.bodyH() != 0 {
		t.Errorf("6 rows: body %d", m.bodyH())
	}
}

// T82: the border colors are [colors] recall_input / recall_list / recall_preview.
func TestRecallBorderColors(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer useConfig(config.Default())
	w := newRWorld(t)
	w.fill()
	// per row of the screen: the input box is rows 0-2, the list and the preview (right of the list) from row 3
	paint := func(m recallModel) (input, list, preview string) {
		rows := strings.Split(m.View(), "\n")
		l := rows[3]
		cut := strings.Index(l, "┓")
		return rows[0], l[:cut], l[cut:]
	}
	i, l, p := paint(w.model())
	for name, c := range map[string][3]string{"input": {i, "38;2;255;142;190", "ff8ebe"}, "list": {l, "38;2;97;113;163", "6272a4"}, "preview": {p, "38;2;189;147;249", "bd93f9"}} {
		if !strings.Contains(c[0], c[1]) {
			t.Errorf("default %s border: want %s (#%s) in %q", name, c[1], c[2], c[0])
		}
	}
	cfg, err := config.Parse("cfg", []byte("[colors]\nrecall_input = \"#112233\"\nrecall_list = \"#445566\"\nrecall_preview = \"#778899\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	useConfig(cfg)
	i, l, p = paint(w.model())
	for name, c := range map[string][2]string{"input": {i, "38;2;17;34;51"}, "list": {l, "38;2;68;85;102"}, "preview": {p, "38;2;119;136;153"}} {
		if !strings.Contains(c[0], c[1]) {
			t.Errorf("configured %s border: want %s in %q", name, c[1], c[0])
		}
	}
	if strings.Contains(i, "38;2;255;142;190") {
		t.Errorf("the default is gone from the input box: %q", i)
	}
	if _, err := config.Parse("cfg", []byte("[colors]\nrecall_list = \"nope\"\n")); err == nil {
		t.Error("a bad color is refused")
	}
	if c := config.Default(); c.Colors["recall_input"] != "#ff8ebe" || c.Colors["recall_list"] != "#6272a4" || c.Colors["recall_preview"] != "#bd93f9" {
		t.Errorf("defaults %v %v %v", c.Colors["recall_input"], c.Colors["recall_list"], c.Colors["recall_preview"])
	}
}

// thickBox is as wide and as tall as asked whatever it holds, at any size.
func TestThickBoxHasTheSizeAsked(t *testing.T) {
	for w := 2; w <= 12; w++ {
		for h := 2; h <= 5; h++ {
			box := thickBox(cRecallList, "a title that is long", []string{"x", "a line much longer than the box"}, w, h)
			if len(box) != h {
				t.Fatalf("%dx%d: %d rows", w, h, len(box))
			}
			for i, l := range box {
				if ansi.StringWidth(l) != w {
					t.Errorf("%dx%d: row %d is %d wide: %q", w, h, i, ansi.StringWidth(l), ansi.Strip(l))
				}
			}
		}
	}
	if thickBox(cRecallList, "", nil, 1, 3) != nil || thickBox(cRecallList, "", nil, 5, 1) != nil {
		t.Error("no room for a border: no box")
	}
	// a title that fits is set into the border, with a space either side
	if l := ansi.Strip(thickBox(cRecallList, "ab", nil, 12, 3)[0]); l != "┏━ ab ━━━━━┓" {
		t.Errorf("title: %q", l)
	}
	if l := ansi.Strip(thickBox(cRecallList, "ab", nil, 5, 3)[0]); l != "┏━━━┓" { // no room for one
		t.Errorf("a title with no room: %q", l)
	}
}

// The preview holds its text with a space either side and the text is cut to fit; scrolled past
// its end it still shows the last page.
func TestRecallPreviewTextFitsItsBox(t *testing.T) {
	w := newRWorld(t)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("u: sentence %d %s", i, strings.Repeat("long ", 30)))
	}
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, lines...)
	m := rtype(t, w.model(), "sentence")
	_, content := m.previewParts(m.listRows())
	pvW := m.w - m.listW()
	for i, l := range content {
		if !strings.HasPrefix(l, " ") || strings.Contains(l, "long") && ansi.StringWidth(l) > pvW-2 { // the card's path is cut by the box, the sentences are wrapped to it
			t.Fatalf("row %d is %d wide in a box with %d inside: %q", i, ansi.StringWidth(l), pvW-2, l)
		}
	}
	m.pvScroll = 9999
	if _, past := m.previewParts(m.listRows()); len(past) != m.listRows() {
		t.Errorf("scrolled past the end: %d rows of %d", len(past), m.listRows())
	}
	r, _ := m.selected()
	if max := m.previewScrollMax(r); max != len(m.previewContent(r, m.pvInner()))-m.listRows() {
		t.Errorf("the most it scrolls: %d", max)
	}
}

// The locked state shows no caret on the query box; the message line stays when the boxes are gone.
func TestRecallLockedHasNoCaretAndLowScreensKeepTheMessage(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	w := newRWorld(t)
	w.fill()
	m := rtype(t, w.model(), "ab")
	if !strings.Contains(strings.Split(m.View(), "\n")[1], "\x1b[7m") {
		t.Error("typing: a caret")
	}
	m, _ = rpress(t, m, "enter")
	if strings.Contains(strings.Split(m.View(), "\n")[1], "\x1b[7m") {
		t.Error("kept: no caret")
	}
	for _, h := range []int{5, 6} {
		if low := resized(t, w.model(), 100, h); !low.msgRow() || low.bodyH() != 0 {
			t.Errorf("%d rows: msg %v body %d", h, low.msgRow(), low.bodyH())
		}
	}
	if low := resized(t, w.model(), 100, 4); low.msgRow() {
		t.Error("4 rows: no message line")
	}
}

// The panel is over the list, inside the screen.
func TestRecallPanelIsInsideTheList(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m, _ := rpress(t, resized(t, w.model(), 100, 20), "ctrl+f")
	lines := strings.Split(rview(m), "\n")
	if len(lines) != 20 || !strings.HasPrefix(lines[1], "┃ / ") {
		t.Fatalf("%d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(strings.Join(lines[3:12], "\n"), "╭") || !strings.Contains(strings.Join(lines, "\n"), "[x] claude") {
		t.Errorf("the panel:\n%s", strings.Join(lines, "\n"))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 100 {
			t.Errorf("too wide: %q", l)
		}
	}
}
