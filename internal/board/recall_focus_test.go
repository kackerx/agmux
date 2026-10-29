package board

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kackerx/agmux/internal/config"
	"github.com/muesli/termenv"
)

// scrollWorld is a session with a long preview and a second one, found by "scrolling".
func scrollWorld(t *testing.T) *rworld {
	w := newRWorld(t)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("u: sentence about scrolling %d", i))
	}
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, lines...)
	w.claude("s-two", w.dir("shopcart"), "two", 2*time.Hour, "u: more scrolling here")
	return w
}

// T92: tab goes input -> list -> preview -> input; the query stays; without a preview (a terminal
// under 100 columns) it is input -> list -> input.
func TestRecallTabCycle(t *testing.T) {
	w := scrollWorld(t)
	m := rtype(t, w.model(), "scrolling")
	type st struct{ locked, pv bool }
	at := func(m recallModel) st { return st{m.locked, m.previewFocused()} }
	for i, want := range []st{{true, false}, {true, true}, {false, false}, {true, false}, {true, true}, {false, false}} {
		m, _ = rpress(t, m, "tab")
		if at(m) != want || m.query != "scrolling" {
			t.Errorf("tab %d: %+v query %q, want %+v", i+1, at(m), m.query, want)
		}
	}
	if m.qpos != len("scrolling") {
		t.Errorf("back in the input the caret is at %d", m.qpos)
	}
	if m, _ = rpress(t, m, "x"); m.query != "scrollingx" { // the input again: letters are text
		t.Errorf("typing after the round: %q", m.query)
	}
	// no preview: the list and the input only
	tm, _ := w.model().Update(tea.WindowSizeMsg{Width: 99, Height: 30})
	n := rtype(t, tm.(recallModel), "scrolling")
	for i, want := range []st{{true, false}, {false, false}, {true, false}, {false, false}} {
		n, _ = rpress(t, n, "tab")
		if at(n) != want {
			t.Errorf("narrow, tab %d: %+v, want %+v", i+1, at(n), want)
		}
	}
	// tab is a key of [keys]: rebound, the old one does nothing
	cfg, err := config.Parse("cfg", []byte("[keys]\nfocus_preview = [\"ctrl+g\"]\nleave_preview = [\"ctrl+]\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := rtype(t, w.model(), "scrolling")
	r.cfg = cfg
	if r, _ = rpress(t, r, "tab"); r.locked {
		t.Error("tab is not bound any more")
	}
	if r, _ = rpress(t, r, "ctrl+g", "ctrl+g"); !r.previewFocused() {
		t.Error("the bound key moves the focus")
	}
}

// T92: the keys of the preview state.
func TestRecallPreviewState(t *testing.T) {
	w := scrollWorld(t)
	pv := func() recallModel {
		m, _ := rpress(t, rtype(t, w.model(), "scrolling"), "tab", "tab")
		if !m.previewFocused() {
			t.Fatal("not in the preview state")
		}
		return m
	}
	for _, c := range []struct {
		keys   []string
		scroll int
	}{
		{[]string{"j"}, 1}, {[]string{"j", "j", "k"}, 1}, {[]string{"k"}, 0}, {[]string{"down", "down"}, 2}, {[]string{"down", "up"}, 0},
		{[]string{"ctrl+n"}, 3}, {[]string{"pgdown"}, 3}, {[]string{"j", "ctrl+n", "ctrl+p"}, 1}, {[]string{"ctrl+p"}, 0},
	} {
		m, _ := rpress(t, pv(), c.keys...)
		if m.pvScroll != c.scroll || m.sel != 0 || !m.previewFocused() {
			t.Errorf("%v: scroll %d (want %d), cursor %d, still in the preview %v", c.keys, m.pvScroll, c.scroll, m.sel, m.previewFocused())
		}
	}
	m, _ := rpress(t, pv(), "esc")
	if m.previewFocused() || !m.locked || m.query != "scrolling" {
		t.Errorf("esc: back to the list with the query kept: locked %v query %q pv %v", m.locked, m.query, m.previewFocused())
	}
	if m, _ = rpress(t, m, "esc"); m.locked || m.query != "" { // and the next one is the list's
		t.Errorf("esc from the list: locked %v query %q", m.locked, m.query)
	}
	if m, _ = rpress(t, pv(), "/"); m.locked || m.previewFocused() || m.query != "scrolling" || m.qpos != len("scrolling") {
		t.Errorf("/: the input again with the query: locked %v query %q caret %d", m.locked, m.query, m.qpos)
	}
	if m, _ = rpress(t, pv(), "/", "enter"); m.previewFocused() || !m.locked { // kept again: the list, not the preview it came from
		t.Errorf("/ then enter: locked %v preview %v", m.locked, m.previewFocused())
	}
	m, quit := rpress(t, pv(), "enter")
	if RecallOutcome(m) == nil || !quit {
		t.Errorf("enter resumes the session of the list's cursor: %v quit %v", RecallOutcome(m), quit)
	}
	if m, quit = rpress(t, pv(), "q"); !quit {
		t.Error("q ends the screen, as in the list")
	}
	// the cursor of the list is not moved by the keys of the preview, and a click on a row still selects
	m, _ = rpress(t, pv(), "j", "j")
	if m.sel != 0 {
		t.Errorf("cursor %d", m.sel)
	}
	// the panel over it: its own keys, esc closes it first and the preview keeps the focus
	p, _ := rpress(t, pv(), "ctrl+f")
	if p, _ = rpress(t, p, "esc"); !p.previewFocused() || p.panel {
		t.Errorf("esc closes the panel: pv %v panel %v", p.previewFocused(), p.panel)
	}
	// a terminal made too narrow takes the preview away, and the focus with it: esc is the list's
	tm, _ := pv().Update(tea.WindowSizeMsg{Width: 99, Height: 30})
	n := tm.(recallModel)
	if n.previewFocused() || strings.Contains(n.footer(), "scroll") {
		t.Error("no preview, no preview state")
	}
	if n, _ = rpress(t, n, "esc"); n.query != "" {
		t.Errorf("esc cleared the query as in the list: %q", n.query)
	}
	// and the screen wider again, after esc the old pv flag must not bring it back
	tm, _ = pv().Update(tea.WindowSizeMsg{Width: 99, Height: 30})
	n, _ = rpress(t, tm.(recallModel), "esc")
	tm, _ = n.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	if tm.(recallModel).previewFocused() {
		t.Error("the preview has the focus again")
	}
	if n, _ = rpress(t, tm.(recallModel), "tab"); !n.locked || n.previewFocused() { // the list is next, not a preview from before
		t.Errorf("tab after that: locked %v preview %v", n.locked, n.previewFocused())
	}
	// the caret is at the end of the query when the input is given back, wherever it was
	c, _ := rpress(t, rtype(t, w.model(), "scrolling"), "left", "left", "tab", "tab", "tab")
	if c.locked || c.qpos != len("scrolling") {
		t.Errorf("after the round: locked %v caret %d", c.locked, c.qpos)
	}
}

// T92: the colors follow the focus: the box that has the keys is recall_focus, the others recall_border,
// the preview recall_preview when it has none.
func TestRecallFocusColorsFollowTab(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer useConfig(config.Default())
	w := scrollWorld(t)
	const focus, plain, prev = "38;2;255;142;190", "38;2;97;113;163", "38;2;189;147;249"
	paint := func(m recallModel) (input, list, preview string) {
		rows := strings.Split(m.View(), "\n")
		l := rows[3]
		cut := strings.Index(l, "┓")
		return rows[0], l[:cut], l[cut:]
	}
	m := rtype(t, w.model(), "scrolling")
	for _, c := range []struct {
		what          string
		keys          []string
		in, li, pview string
	}{
		{"typing", nil, focus, plain, prev},
		{"list", []string{"tab"}, plain, focus, prev},
		{"preview", []string{"tab", "tab"}, plain, plain, focus},
		{"preview, panel open", []string{"tab", "tab", "ctrl+f"}, plain, plain, focus},
		{"input again", []string{"tab", "tab", "tab"}, focus, plain, prev},
	} {
		s, _ := rpress(t, m, c.keys...)
		i, l, p := paint(s)
		for name, w := range map[string][2]string{"input": {i, c.in}, "list": {l, c.li}, "preview": {p, c.pview}} {
			if !strings.Contains(w[0], w[1]) {
				t.Errorf("%s: the %s border should be %s: %q", c.what, name, w[1], w[0])
			}
		}
		if strings.Contains(i, map[string]string{focus: plain, plain: focus}[c.in]) || strings.Contains(l, map[string]string{focus: plain, plain: focus}[c.li]) || strings.Contains(p, map[string]string{focus: prev, prev: focus}[c.pview]) {
			t.Errorf("%s: a box has two colors: %q %q %q", c.what, i, l, p)
		}
	}
	// configured
	cfg, err := config.Parse("cfg", []byte("[colors]\nrecall_focus = \"#445566\"\nrecall_preview = \"#778899\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	useConfig(cfg)
	s, _ := rpress(t, m, "tab", "tab")
	if _, _, p := paint(s); !strings.Contains(p, "38;2;68;85;102") || strings.Contains(p, "38;2;119;136;153") {
		t.Errorf("the focused preview takes recall_focus, not recall_preview: %q", p)
	}
	s, _ = rpress(t, m, "tab")
	if _, _, p := paint(s); !strings.Contains(p, "38;2;119;136;153") {
		t.Errorf("an unfocused preview takes recall_preview: %q", p)
	}
}

// T92: the footer of the preview state is one row of its own keys.
func TestRecallPreviewFooter(t *testing.T) {
	w := scrollWorld(t)
	m, _ := rpress(t, rtype(t, w.model(), "scrolling"), "tab", "tab")
	f := ansi.Strip(m.footer())
	if strings.Contains(f, "\n") {
		t.Errorf("not one row: %q", f)
	}
	for _, want := range []string{"enter resume", "j/k scroll", "/ edit", "esc back to the list"} {
		if !strings.Contains(f, want) {
			t.Errorf("footer lacks %q: %q", want, f)
		}
	}
	if strings.Contains(f, "filter") || strings.Contains(f, "copy") {
		t.Errorf("only what the preview takes: %q", f)
	}
	if l, _ := rpress(t, m, "esc"); !strings.Contains(ansi.Strip(l.footer()), "filter") {
		t.Errorf("the list's footer is back: %q", ansi.Strip(l.footer()))
	}
}

// T92 inside the board: tab is the recall screen's there too, and the board's own tab (pass-through)
// is not taken meanwhile.
func TestBoardRecallTab(t *testing.T) {
	e, _ := newEmb(t, userOnly, paneEnv, false)
	e = openRecallOn(t, e)
	press1 := func() {
		tm, cmd := e.Update(key("tab"))
		e = tm.(Model)
		run(&e, cmd)
	}
	press1()
	if e.rc == nil || !e.rc.locked || e.pvFocus {
		t.Fatalf("one tab: list; locked %v board pass-through %v", e.rc != nil && e.rc.locked, e.pvFocus)
	}
	press1()
	if e.rc == nil || !e.rc.previewFocused() || e.pvFocus {
		t.Errorf("two tabs: the preview of recall has the focus: %+v", e.rc)
	}
	press1()
	if e.rc == nil || e.rc.locked {
		t.Errorf("three tabs: the input again")
	}
}
