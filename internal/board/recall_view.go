package board

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/fuzzy"
	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/recall"
)

// posIn is where the query matches text, as rune offsets; none when it does not (or the query is empty).
func posIn(query, text string) []int {
	if strings.TrimSpace(query) == "" || text == "" {
		return nil
	}
	if ms := fuzzy.Filter(query, []string{text}); len(ms) > 0 {
		return ms[0].Pos
	}
	return nil
}

// window is the part of text that fits width cells, starting a little before its first match so the
// match is in it; "…" marks what is cut. pos is moved to the cut text.
func window(text string, pos []int, width int) (string, []int) {
	if width <= 0 {
		return "", nil
	}
	if ansi.StringWidth(text) <= width {
		return text, pos
	}
	r := []rune(text)
	start := 0
	if len(pos) > 0 { // by cells, not runes: a Chinese sentence is twice as wide as it is long
		cells := ansi.StringWidth(string(r[:pos[0]]))
		if cells > width/2 {
			for cells > width/3 {
				cells -= ansi.StringWidth(string(r[start]))
				start++
			}
		}
	}
	if start > 0 {
		r = append([]rune{'…'}, r[start:]...)
		moved := make([]int, 0, len(pos))
		for _, p := range pos {
			if p >= start {
				moved = append(moved, p-start+1)
			}
		}
		pos = moved
	}
	return ansi.Truncate(string(r), width, "…"), pos
}

// projectLabel is the project column: a linked worktree is "repo.wt/directory".
func projectLabel(e *recall.Entry) string {
	if e.Worktree != "" {
		return e.Project + ".wt/" + e.Worktree
	}
	return e.Project
}

// right pads s on the left to w cells.
func padLeft(s string, w int) string { return strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + s }

// stateColor is the color of a running session's dot and word.
func stateColor(st string) lipgloss.Color {
	switch st {
	case "working":
		return cWorking
	case "waiting":
		return cRed
	case "done":
		return cDone
	}
	return cMuted
}

// recallBorder is the border of recall's three boxes: [layout] recall_border, set by useConfig.
var recallBorder = lipgloss.ThickBorder()

// recallBox draws lines in a recallBorder box of color c, w wide and h tall (borders included); the
// title is set into the top border. Lines are cut to the box and padded to it.
func recallBox(c lipgloss.Color, title string, lines []string, w, h int) []string {
	if w < 2 || h < 2 {
		return nil
	}
	inner := w - 2
	b := recallBorder
	top := fg(c, b.TopLeft+strings.Repeat(b.Top, inner)+b.TopRight)
	if title != "" && inner >= 6 {
		t := ansi.Truncate(title, inner-4, "…")
		top = fg(c, b.TopLeft+b.Top+" ") + t + "\x1b[0m" + fg(c, " "+strings.Repeat(b.Top, w-5-ansi.StringWidth(t))+b.TopRight)
	}
	out := []string{top}
	for i := range h - 2 {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, fg(c, b.Left)+pad(ansi.Truncate(l, inner, ""), inner)+"\x1b[0m"+fg(c, b.Right))
	}
	return append(out, fg(c, b.BottomLeft+strings.Repeat(b.Bottom, inner)+b.BottomRight))
}

func (m recallModel) View() string {
	view, x, y, ok := m.frame()
	if m.curOut != nil && m.w > 0 {
		if ok {
			m.curOut.place(x, y)
		} else {
			m.curOut.place(-1, -1)
		}
	}
	return view
}

// frame is the screen and where the terminal's cursor goes on it (ok: it is shown), both made from one
// reading of the index's progress, which goes on meanwhile.
func (m recallModel) frame() (view string, x, y int, ok bool) {
	if m.w == 0 {
		return "", 0, 0, false
	}
	prog := m.progressText()
	x, y, ok = m.cursorAtFor(prog)
	lines := m.inputBox(prog)
	if bh := m.bodyH(); bh > 0 {
		lw := m.listW()
		body := recallBox(m.boxColor(m.locked), m.listTitle(), m.listLines(bh-2, lw-2), lw, bh)
		if m.previewOn() {
			title, content := m.previewParts(bh - 2)
			pv := recallBox(cRecallPreview, title, content, m.w-lw, bh)
			for i := range body {
				body[i] += pv[i]
			}
		}
		lines = append(lines, body...)
		if m.panel {
			lines = strings.Split(overlayAt(strings.Join(lines, "\n"), m.panelBox(), 2, 4, m.w), "\n")
		}
	}
	if m.msgRow() {
		lines = append(lines, ansi.Truncate(firstLine(m.msg), m.w, "…"))
	}
	if len(lines) >= m.h {
		lines = lines[:max(m.h-1, 0)]
	}
	return strings.Join(append(lines, m.footer()), "\n"), x, y, ok
}

// listTitle is how many sessions the query found out of how many there are to search: "3/14", "0/14" when nothing is found.
func (m recallModel) listTitle() string {
	return fmt.Sprintf("%d/%d", len(m.results), m.total)
}

// boxColor is the border of the query box or the list: focus color for the one that has the keys
// (the box while typing, the list once the query is kept), else the plain one. The filter panel
// opened over them changes nothing.
func (m recallModel) boxColor(focused bool) lipgloss.Color {
	if focused {
		return cRecallFocus
	}
	return cRecallBorder
}

// inputBox is the query in a box of its own; on the right how far the index is, while it is built.
// The caret is there while the input has the keys.
// prog is what progressText said for this frame: the index goes on while the frame is drawn, and
// the caret and the cursor must be put by the same words.
func (m recallModel) inputBox(prog string) []string {
	left := fg(cFocus, " / ") + queryView(m.query, m.qpos, !m.locked, m.roomFor(prog))
	return recallBox(m.boxColor(!m.locked), "", []string{spread(left, fg(cMuted, prog), m.w-2, "…")}, m.w, 3)
}

// progressText is how far the index is while it is built (read once for a frame), else nothing.
func (m recallModel) progressText() string {
	if !m.indexing {
		return ""
	}
	return i18n.T("recall.indexing", m.progress.done.Load(), m.progress.total.Load()) + " "
}

// roomFor is the cells the query has in its box: what is between the border and the progress text.
func (m recallModel) roomFor(prog string) int { return m.w - 2 - 3 - ansi.StringWidth(prog) - 1 }

// footer is the keys, each with its first configured key; esc closes the panel, clears the query
// or ends the screen, whichever it would do. Outside the panel letters are text, so only a key that
// is not a character is shown for an action (none: it is left out; resume is always enter).
func (m recallModel) footer() string {
	k := m.cfg.First
	kc := func(action string) string { return m.cfg.FirstCtl(action, "") }
	var parts []string
	add := func(key, text string, args ...any) {
		if key != "" {
			parts = append(parts, i18n.T(text, append([]any{key}, args...)...))
		}
	}
	scope := "all"
	if m.scopeCWD {
		scope = "project"
	}
	switch {
	case m.panel:
		parts = append(parts, i18n.T("recall.panel.hint", k("recall_toggle"), k("recall_all"), k("recall_none"), "esc"))
	case m.locked: // a kept query: every key of [keys] acts
		add(k("jump"), "recall.footer.resume")
		if d, u := k("down"), k("up"); d != "" && u != "" {
			parts = append(parts, i18n.T("recall.footer.move", d, u))
		}
		add(k("search"), "recall.footer.edit")
		add(k("recall_filter"), "recall.footer.filter")
		add(k("recall_scope"), "recall.footer.scope", i18n.T("recall.scope_short."+scope))
		add(k("recall_copy"), "recall.footer.copy")
		add(k("recall_open"), "recall.footer.open")
		parts = append(parts, i18n.T("search.clear", "esc"))
	default: // the input: enter keeps the query, resuming is for the list
		add(m.cfg.FirstCtl("jump", "enter"), "recall.footer.keep")
		if d, u := m.cfg.FirstCtl("down", ""), m.cfg.FirstCtl("up", ""); d != "" && u != "" {
			parts = append(parts, i18n.T("recall.footer.move", d, u))
		}
		add(kc("recall_filter"), "recall.footer.filter")
		add(kc("recall_scope"), "recall.footer.scope", i18n.T("recall.scope_short."+scope))
		add(kc("recall_copy"), "recall.footer.copy")
		add(kc("recall_open"), "recall.footer.open")
		if m.query != "" {
			parts = append(parts, i18n.T("search.clear", "esc"))
		} else {
			parts = append(parts, i18n.T("footer.quit", "esc"))
		}
	}
	return ansi.Truncate(fg(cMuted, " "+strings.Join(parts, " · ")), m.w, "…")
}

// panelBox is the filter panel: the sources that are there to tick, the two scopes to pick from.
func (m recallModel) panelBox() []string {
	row := func(i int, text string) string {
		if i == m.pcur {
			return fg(cFocus, "▸ "+text)
		}
		return "  " + text
	}
	box := func(on bool) string {
		if on {
			return "[x] "
		}
		return "[ ] "
	}
	radio := func(on bool) string {
		if on {
			return "(•) "
		}
		return "( ) "
	}
	lines := []string{fg(cMuted, " "+i18n.T("recall.panel.sources"))}
	for i, s := range m.sources {
		lines = append(lines, row(i, box(!m.unticked[s])+s))
	}
	n := len(m.sources)
	lines = append(lines, fg(cMuted, " "+i18n.T("recall.panel.scope")),
		row(n, radio(!m.scopeCWD)+i18n.T("recall.scope.all")), row(n+1, radio(m.scopeCWD)+i18n.T("recall.scope.project")))
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	w = min(max(w+2, 28), max(m.w-2, 10))
	box0 := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFocus).Width(w).Render(strings.Join(lines, "\n"))
	return strings.Split(box0, "\n")
}

// listColumns are the widths of the columns of the rows now shown, for a list lw wide: the name up
// to 28, the project and the branch up to what they need; the snippet takes the rest. A column goes
// when the snippet would be left too narrow: the branch first, then the project.
func (m recallModel) listColumns(rows []recall.Result, lw int) (nw, pw, bw, sw int) {
	for _, r := range rows {
		nw = max(nw, min(ansi.StringWidth(r.Title()), 28))
		pw = max(pw, min(ansi.StringWidth(projectLabel(r.Entry)), 22))
		bw = max(bw, min(ansi.StringWidth(r.Branch), 20))
	}
	const lead, tail, snip = 3, 9, 14 // marker and dot; time and hits; the least snippet worth showing
	room := func() int {
		w := lead + nw + tail
		if pw > 0 {
			w += pw + 1
		}
		if bw > 0 {
			w += bw + 1
		}
		return lw - w - 1
	}
	if room() < snip {
		bw = 0
	}
	if room() < snip {
		pw = 0
	}
	if room() < snip {
		nw = max(nw-(snip-room()), 8)
	}
	return nw, pw, bw, max(room(), 0)
}

func (m recallModel) listLines(rows, lw int) []string {
	if len(m.results) == 0 {
		switch {
		case m.indexing:
			return nil
		case len(m.sources) > 0 && len(m.unticked) == len(m.sources):
			return []string{fg(cMuted, " "+i18n.T("recall.no_source"))}
		}
		return []string{fg(cMuted, " "+i18n.T("recall.empty"))}
	}
	shown := m.results[m.top:min(m.top+rows, len(m.results))]
	nw, pw, bw, sw := m.listColumns(shown, lw)
	now := m.now()
	var out []string
	for i, r := range shown {
		sel := m.top+i == m.sel
		out = append(out, m.row(r, sel, nw, pw, bw, sw, now))
	}
	return out
}

func (m recallModel) row(r recall.Result, sel bool, nw, pw, bw, sw int, now time.Time) string {
	mark, nameC := " ", cName
	if sel {
		mark, nameC = fg(cFocus, "▌"), cFocus
	}
	dot := fg(cDim, "○")
	if s, ok := m.liveOf(r.Entry); ok {
		dot = fg(stateColor(stateOf(s)), "●")
	}
	cell := func(text string, w int, c lipgloss.Color) string {
		if w == 0 {
			return ""
		}
		pos := posIn(m.query, text)
		t, pos := window(text, pos, w)
		return pad(hilite(t, pos, c), w) + " "
	}
	title := r.Title()
	text, spos := window(r.Snippet.Text, posIn(m.query, r.Snippet.Text), sw)
	snipC := lipgloss.Color("")
	if r.Snippet.Role == '<' {
		snipC = cMuted
	}
	hits := "-"
	if r.Hits > 0 {
		hits = fmt.Sprint(r.Hits)
	}
	line := mark + dot + " " + cell(title, nw, nameC)
	if pw > 0 {
		line += cell(projectLabel(r.Entry), pw, cDir)
	}
	if bw > 0 {
		line += cell(r.Branch, bw, cBranch)
	}
	line += fg(cMuted, padLeft(relTime(time.Unix(0, r.MTime), now), 4)) + " " + fg(cMuted, padLeft(hits, 3)) + " "
	return line + hilite(text, spos, snipC)
}

// previewLines is the right side: the session's card, and what matched in it with a line of what was
// said before and after, scrolled by pvScroll.
func (m recallModel) previewParts(rows int) (title string, content []string) {
	r, ok := m.selected()
	if !ok {
		return "", nil
	}
	title = fg(cName, r.Title())
	if s, live := m.liveOf(r.Entry); live {
		st := stateOf(s)
		title += fg(cMuted, " · ") + fg(stateColor(st), i18n.T("status."+st))
	}
	all := m.previewContent(r, m.pvInner())
	off := min(m.pvScroll, max(len(all)-rows, 0))
	for _, l := range all[off:] {
		content = append(content, " "+l)
	}
	return title, content
}

// pvInner is the columns of text in the preview: its box less the borders and a space either side.
func (m recallModel) pvInner() int { return max(m.w-m.listW()-4, 1) }

// previewContent: the card (where, when, which agent), then the sentences.
func (m recallModel) previewContent(r recall.Result, w int) []string {
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", r.Path, m.query, w, r.MTime, m.now().Unix()/60)
	if c := m.pvCache; c.key == key && c.lines != nil {
		return c.lines
	}
	lines := m.buildPreview(r, w)
	*m.pvCache = pvCache{key, lines}
	return lines
}

// previewScrollMax is how far the preview of r can be scrolled down.
func (m recallModel) previewScrollMax(r recall.Result) int {
	return max(len(m.previewContent(r, m.pvInner()))-m.listRows(), 0)
}

// buildPreview is previewContent without the cache.
func (m recallModel) buildPreview(r recall.Result, w int) []string {
	e := r.Entry
	var out []string
	meta := e.Source
	if e.Branch != "" {
		meta += " · " + e.Branch
	}
	out = append(out, fg(cDir, tildePath(e.CWD)), fg(cMuted, meta+" · "+relTime(time.Unix(0, e.MTime), m.now())), fg(cMuted, e.ID), "")
	lines := e.Chrono()
	hits := e.Matches(m.query)
	switch {
	case m.query == "" || len(hits) == 0:
		if m.query != "" {
			out = append(out, fg(cMuted, i18n.T("recall.by_name")), "")
		}
		for _, l := range lines[max(len(lines)-6, 0):] {
			out = append(out, sentence(l, nil, false, w)...)
		}
		return out
	}
	// each hit with a line either side; the sentences between are one note
	show := map[int]recall.LineMatch{}
	var order []int
	for _, h := range hits {
		show[h.I] = h
		for _, i := range []int{h.I - 1, h.I, h.I + 1} {
			if i >= 0 && i < len(lines) && !slices.Contains(order, i) {
				order = append(order, i)
			}
		}
	}
	slices.Sort(order)
	next := 0
	for _, i := range order {
		if i > next {
			out = append(out, fg(cMuted, i18n.T("recall.gap", i-next)))
		}
		h, isHit := show[i]
		out = append(out, sentence(lines[i], h.Pos, isHit, w)...)
		next = i + 1
	}
	if next < len(lines) {
		out = append(out, fg(cMuted, i18n.T("recall.gap", len(lines)-next)))
	}
	return out
}

// sentence is one line of the conversation, wrapped to w with a marker for who said it; a hit has
// its matching characters lit, a neighbour is muted.
func sentence(l recall.Line, pos []int, hit bool, w int) []string {
	mark, c := "› ", cFocus
	if l.Role == '<' {
		mark, c = "‹ ", cMuted
	}
	body := fg(cMuted, l.Text)
	if hit {
		body = hilite(l.Text, pos, lipgloss.Color(""))
	}
	var out []string
	for i, row := range strings.Split(ansi.Wrap(body, max(w-2, 1), ""), "\n") {
		if i == 0 {
			out = append(out, fg(c, mark)+row)
		} else {
			out = append(out, "  "+row)
		}
	}
	return out
}
