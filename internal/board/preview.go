package board

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/store"
)

// The preview: the focused session's pane, captured with tmux and shown to the right of the cards.
const (
	previewMinTerm = 100 // narrower terminals can't open it
	previewMinW    = 60
)

// previewInner is the columns of text in a w-wide preview: one of gap from the cards, then "│ " … " │".
func previewInner(w int) int { return w - 1 - 4 }

// previewW is the preview's share of a w-wide terminal: 40%, at least previewMinW.
func previewW(w int) int { return max(previewMinW, w*40/100) }

// previewOn: asked for with p and the terminal is wide enough right now.
func (m Model) previewOn() bool { return m.preview && m.w >= previewMinTerm }

// cardsW is the width the cards get: everything, or what the preview leaves.
func (m Model) cardsW() int {
	if m.previewOn() {
		return m.w - previewW(m.w)
	}
	return m.w
}

// previewLinesN is how many captured lines fit: the body minus the title and the box's two borders.
func (m Model) previewLinesN() int { return max(m.h-3-3, 1) }

// previewMsg is one capture of pane (sendErr: the key forwarded before it failed).
type previewMsg struct {
	pane, text string
	cur        *cursor // where the pane's cursor is; nil = hidden or unknown
	err        error
	sendErr    bool
}

// cursor is the pane's cursor in the capture: row counts lines of the whole capture (history
// included), col is the cell. bar is the bar / underline shapes, which are drawn thinner than a block.
type cursor struct {
	row, col int
	bar      bool
}

// grab captures the last n lines of pane and, in the same tmux call so both are from the same
// moment, where its cursor is: the first line of the output is "x y flag shape height".
func grab(r Runner, pane string, n int) previewMsg {
	out, err := r.Tmux("display-message", "-p", "-t", pane, "#{cursor_x} #{cursor_y} #{cursor_flag} #{cursor_shape} #{pane_height}",
		";", "capture-pane", "-p", "-e", "-t", pane, "-S", "-"+strconv.Itoa(n))
	if err != nil {
		return previewMsg{pane: pane, err: err}
	}
	head, text, _ := strings.Cut(out, "\n")
	f := strings.Fields(head)
	if len(f) != 5 { // an info line we can't read (an older tmux without cursor_shape): no cursor, but it's no screen either
		return previewMsg{pane: pane, text: text}
	}
	x, ex := strconv.Atoi(f[0])
	y, ey := strconv.Atoi(f[1])
	h, eh := strconv.Atoi(f[4])
	msg := previewMsg{pane: pane, text: text}
	if ex == nil && ey == nil && eh == nil && h > 0 && f[2] == "1" { // flag 0: hidden (civis, copy-mode)
		lines := strings.Count(text, "\n") // one per captured line
		msg.cur = &cursor{row: lines - h + y, col: x, bar: f[3] == "bar" || f[3] == "underline"}
	}
	return msg
}

// capture grabs the focused session's pane (nil when there is nothing to capture or one is in flight).
func (m *Model) capture() tea.Cmd {
	if !m.previewOn() || m.capturing {
		return nil
	}
	_, _, s, ok := m.current()
	if !ok || s.TmuxPane == "" {
		return nil
	}
	m.capturing = true
	r, pane, n := m.run, s.TmuxPane, m.previewLinesN()+m.scroll
	// through the key queue: a capture started before a key must not land after that key's capture
	return m.keys.do(func() tea.Msg { return grab(r, pane, n) })
}

// previewLines renders the preview region: h lines, each exactly w wide.
//
//	 sqlmux-orchestrator · %7 · ⏵ working · Bash
//	╭──────────────────────────────╮
//	│ …the pane's last lines…      │
//	╰──────────────────────────────╯
func previewLines(s store.Session, ok bool, pv previewMsg, w, h, scroll int, focused bool) []string {
	out, _, _ := previewBox(s, ok, pv, w, h, scroll, focused)
	return out
}

// previewBox is previewLines and the row and column of the pane's cursor in those lines: -1 when it
// isn't drawn. Keys going to the pane (focused) with the cursor drawn, the terminal's own cursor
// goes there: the cell is left as it is, but for a sequence that changes nothing, so that the
// frame changes when the cursor moves and the cursor is put where it now is.
func previewBox(s store.Session, ok bool, pv previewMsg, w, h, scroll int, focused bool) ([]string, int, int) {
	inner := previewInner(w)
	title := ""
	var content []string
	curRow, curCol := -1, 0 // the row and column of the box the pane's cursor is on, if it is drawn
	switch {
	case !ok || s.TmuxPane == "":
		content = []string{fg(cMuted, "no pane")}
		if ok {
			title = fg(cName, s.Name)
			if s.Background { // bg where the pane would be
				word, detail := status(s)
				title += fg(cMuted, " · "+bgTag+" · ") + word + fg(cMuted, detail)
			}
		}
	default:
		word, detail := status(s)
		where := s.TmuxPane
		if s.Background { // attached to a pane: bg · %id
			where = bgTag + " · " + s.TmuxPane
		}
		title = fg(cName, s.Name) + fg(cMuted, " · "+where+" · ") + word + fg(cMuted, detail)
		switch {
		case pv.pane != s.TmuxPane: // the capture for this pane hasn't arrived yet
		case pv.err != nil:
			content = []string{fg(cRed, firstLine(pv.err.Error()))}
		default:
			n := max(h-3, 0) // rows inside the box; none on a tiny terminal
			keep := 0
			if pv.cur != nil && scroll == 0 { // a scrolled-back view isn't where the cursor is
				keep = pv.cur.row + 1 // the blank rows down to the cursor's are part of the screen
			}
			var cur *cursor
			if keep > 0 {
				cur = pv.cur
			}
			rows, row, col := wrapRows(trimTail(pv.text, keep), inner, cur)
			content = rows[min(max(len(rows)-(n+scroll), 0), len(rows)):] // scroll rows back: drop that many from the bottom
			content = content[:max(len(content)-scroll, min(len(content), n))]
			if cur != nil {
				curRow, curCol = row-(len(rows)-len(content)), col
			}
		}
	}
	out := []string{pad(ansi.Truncate(" "+title, w, "…"), w)}
	border := cDim
	if focused { // keys go to the pane
		border = cFocus
	}
	out = append(out, " "+fg(border, "╭"+strings.Repeat("─", inner+2)+"╮"))
	drawn := false // the cursor is on a row of the box
	for i := range max(h-3, 0) {
		l := ""
		if i < len(content) {
			l = ansi.Truncate(content[i], inner, "")
		}
		if i == curRow && curCol >= 0 && curCol < inner {
			l = ansi.Truncate(paintCursor(l, pv.cur, curCol, focused), inner, "")
			drawn = true
		}
		// the reset right after the text keeps a color still on at its end from filling the padding
		// and the border
		out = append(out, " "+fg(border, "│")+" "+pad(l+"\x1b[0m", inner)+"\x1b[0m "+fg(border, "│"))
	}
	out = append(out, " "+fg(border, "╰"+strings.Repeat("─", inner+2)+"╯"))
	if !drawn {
		return out[:h], -1, -1
	}
	return out[:h], 2 + curRow, 3 + curCol // below the title and the top border; in from the gap, the border and its space
}

// paintCursor draws the pane's cursor on its line l: a block is the cell in reverse video, a bar or
// underline (a terminal can't draw a thin line here) the cell underlined in the focus color. With
// the terminal's own cursor on it (real), the cell is only marked by a sequence that changes nothing.
func paintCursor(l string, c *cursor, col int, real bool) string {
	cell := " " // past the end of the text, an empty cell
	for _, g := range clustersOf(ansi.Strip(l)) {
		if g.x == col { // a wide character is one cell of the cursor, two columns wide
			cell = g.s
		}
	}
	if real {
		return put(l, col, "\x1b[27m"+cell)
	}
	style := lipgloss.NewStyle().Reverse(true)
	if c.bar {
		style = lipgloss.NewStyle().Foreground(cFocus).Underline(true)
	}
	return put(l, col, style.Render(cell))
}

// wrapRows hard-wraps each line of the capture to inner columns, the pane's colors carried onto the
// rows that continue a line. cur (nil: none) is the pane's cursor in lines; row and col are where it
// lands among the rows (the cell after the end of its line may need a row of its own).
func wrapRows(lines []string, inner int, cur *cursor) (rows []string, row, col int) {
	row = -1 // the cursor is not among them
	// tmux ends a line without a reset when its last cell is colored, and does not repeat the color
	// on the next: so what is in force goes on across lines, from the first line of the capture
	// (the lines above the rows shown may be where it was set).
	// ponytail: the state is every sequence since the last reset: a long capture that never resets
	// makes every row's prefix as long.
	sgr := ""
	for i, l := range lines {
		parts := strings.Split(ansi.Hardwrap(l, inner, true), "\n")
		for j, p := range parts {
			parts[j] = sgr + p
			sgr = activeSGR(parts[j])
		}
		if cur != nil && i == cur.row {
			j, acc := 0, 0 // the row the cursor is on, and the column that row starts on
			for ; j < len(parts)-1 && cur.col >= acc+ansi.StringWidth(parts[j]); j++ {
				acc += ansi.StringWidth(parts[j])
			}
			for ; cur.col-acc >= inner; acc += inner { // past the end of the line: rows of blanks
				parts, j = append(parts, ""), j+1
			}
			row, col = len(rows)+j, cur.col-acc
		}
		rows = append(rows, parts...)
	}
	return rows, row, col
}

// activeSGR is the color / style sequence in force at the end of l, to start the next row with.
func activeSGR(l string) string {
	state := ""
	for len(l) > 0 {
		seq, _, n, _ := ansi.DecodeSequence(l, ansi.NormalState, nil)
		if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
			if p := seq[2 : len(seq)-1]; p == "" || p == "0" {
				state = ""
			} else if strings.HasPrefix(p, "0;") {
				state = seq // it resets first, so it is the whole state
			} else {
				state += seq
			}
		}
		l = l[n:]
	}
	return state
}

// trimTail is the lines of a capture without the blank rows at its end, but at least keep lines
// (a cursor waiting on a blank row below the text is on the screen).
func trimTail(text string, keep int) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = expandTabs(l)
	}
	for len(lines) > keep && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// tail is the last n lines of a capture, without the blank rows below the cursor.
func tail(text string, n int) []string {
	lines := trimTail(text, 0)
	return lines[min(max(len(lines)-n, 0), len(lines)):] // n < 0 on a tiny terminal: nothing
}

// expandTabs turns a tab into spaces up to the next multiple of 8 columns, as a terminal does.
func expandTabs(l string) string {
	if !strings.Contains(l, "\t") {
		return l
	}
	var b strings.Builder
	for i, seg := range strings.Split(l, "\t") {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", 8-ansi.StringWidth(b.String())%8))
		}
		b.WriteString(seg)
	}
	return b.String()
}

// tmuxKeyNames maps bubbletea's key names to tmux's send-keys names.
var tmuxKeyNames = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "enter": "Enter", "esc": "Escape",
	"backspace": "BSpace", "delete": "DC", "insert": "IC", "home": "Home", "end": "End",
	"pgup": "PPage", "pgdown": "NPage", "tab": "Tab",
	"f1": "F1", "f2": "F2", "f3": "F3", "f4": "F4", "f5": "F5", "f6": "F6", "f7": "F7", "f8": "F8", "f9": "F9",
	"f10": "F10", "f11": "F11", "f12": "F12", // tmux has no F13 and up: they'd arrive as the text "F13"
}

// literal sends text as typed. tmux takes an argument ending in ";" as a command separator (even
// after -l --), so trailing semicolons go as separate `\;` arguments: `x;;` → x \; \;.
func literal(pane, text string) []string {
	args := []string{"send-keys", "-t", pane, "-l", "--"}
	trimmed := strings.TrimRight(text, ";")
	if trimmed != "" {
		args = append(args, trimmed)
	}
	for range len(text) - len(trimmed) {
		args = append(args, `\;`)
	}
	return args
}

// sendKeysArgs turns a key into tmux send-keys arguments for pane: text goes literally (-l --),
// everything else by tmux key name with C- / M- / S- for ctrl / alt / shift. nil = no equivalent.
func sendKeysArgs(pane string, k tea.KeyMsg) []string {
	if (k.Type == tea.KeyRunes || k.Type == tea.KeySpace) && (!k.Alt || len(k.Runes) != 1) {
		return literal(pane, string(k.Runes))
	}
	name := k.String()
	if name == "shift+tab" {
		return []string{"send-keys", "-t", pane, "BTab"}
	}
	mods := ""
	for _, m := range [][2]string{{"alt+", "M-"}, {"ctrl+", "C-"}, {"shift+", "S-"}} {
		if rest, ok := strings.CutPrefix(name, m[0]); ok {
			mods, name = mods+m[1], rest
		}
	}
	switch n, ok := tmuxKeyNames[name]; {
	case ok:
		name = n
	case name == "@" && strings.Contains(mods, "C-"): // ctrl+@ is NUL, tmux's C-Space
		name, mods = "Space", strings.Replace(mods, "C-", "", 1)+"C-"
	case name == ";":
		name = `\;` // a key name ending in ";" would be a command separator too
	case len([]rune(name)) != 1:
		return nil
	}
	return []string{"send-keys", "-t", pane, mods + name}
}

// serial runs jobs one at a time in the order they were queued. Forwarded keys go through it:
// bubbletea runs each tea.Cmd on its own goroutine, so a fast burst ("a b c", split into one
// KeyMsg per rune and space) would otherwise reach the pane in any order.
type serial struct{ jobs chan func() }

func newSerial() *serial {
	s := &serial{make(chan func(), 256)}
	go func() {
		for job := range s.jobs {
			job()
		}
	}()
	return s
}

// do queues f now (Update calls it, in key order) and returns a Cmd that waits for f's result.
func (s *serial) do(f func() tea.Msg) tea.Cmd {
	done := make(chan tea.Msg, 1)
	s.jobs <- func() { done <- f() }
	return func() tea.Msg { return <-done }
}

// forward sends k to the locked pane (pvPane) and recaptures right after, in the same job, so the
// capture can't come back before the key arrived. A paste goes through a tmux buffer so the pane
// gets it as a bracketed paste (several lines are not submitted one by one).
func (m *Model) forward(k tea.KeyMsg) tea.Cmd {
	pane := m.pvPane
	var send func(r Runner) error
	if k.Paste {
		if len(k.Runes) == 0 { // load-buffer with nothing keeps the old buffer, which would be pasted
			return nil
		}
		text := string(k.Runes)
		send = func(r Runner) error {
			if err := r.TmuxIn(text, "load-buffer", "-b", "agmux", "-"); err != nil {
				return err
			}
			_, err := r.Tmux("paste-buffer", "-p", "-d", "-b", "agmux", "-t", pane)
			if err != nil { // -d only deletes after a paste: don't leave the text for the next one
				r.Tmux("delete-buffer", "-b", "agmux")
			}
			return err
		}
	} else {
		args := sendKeysArgs(pane, k)
		if args == nil {
			return nil
		}
		send = func(r Runner) error { _, err := r.Tmux(args...); return err }
	}
	m.scroll, m.capturing = 0, true
	r, n := m.run, m.previewLinesN()
	return m.keys.do(func() tea.Msg {
		if err := send(r); err != nil {
			return previewMsg{pane: pane, err: err, sendErr: true}
		}
		return grab(r, pane, n)
	})
}
