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
	inner := w - 1 - 4 // one column of gap from the cards, then "│ " … " │"
	title := ""
	var content []string
	curRow := -1 // the row of the box the pane's cursor is on, if it is drawn
	switch {
	case !ok || s.TmuxPane == "":
		content = []string{fg(cMuted, "no pane")}
		if ok {
			title = fg(cName, s.Name)
		}
	default:
		word, detail := status(s)
		title = fg(cName, s.Name) + fg(cMuted, " · "+s.TmuxPane+" · ") + word + fg(cMuted, detail)
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
			lines := trimTail(pv.text, keep)
			content = lines[min(max(len(lines)-(n+scroll), 0), len(lines)):] // scroll lines back: drop that many from the bottom
			content = content[:max(len(content)-scroll, min(len(content), n))]
			if keep > 0 {
				curRow = pv.cur.row - (len(lines) - len(content))
			}
		}
	}
	out := []string{pad(ansi.Truncate(" "+title, w, "…"), w)}
	border := cDim
	if focused { // keys go to the pane
		border = cFocus
	}
	out = append(out, " "+fg(border, "╭"+strings.Repeat("─", inner+2)+"╮"))
	for i := range max(h-3, 0) {
		l := ""
		if i < len(content) {
			l = ansi.Truncate(content[i], inner, "")
		}
		if i == curRow && pv.cur.col >= 0 && pv.cur.col < inner {
			l = ansi.Truncate(paintCursor(l, pv.cur), inner, "")
		}
		// the reset keeps the pane's own colors from bleeding into the border
		out = append(out, " "+fg(border, "│")+" "+pad(l, inner)+"\x1b[0m "+fg(border, "│"))
	}
	out = append(out, " "+fg(border, "╰"+strings.Repeat("─", inner+2)+"╯"))
	return out[:h]
}

// paintCursor draws the pane's cursor on its line l: a block is the cell in reverse video, a bar or
// underline (a terminal can't draw a thin line here) the cell underlined in the focus color.
func paintCursor(l string, c *cursor) string {
	cell := " " // past the end of the text, an empty cell
	for _, g := range clustersOf(ansi.Strip(l)) {
		if g.x == c.col { // a wide character is one cell of the cursor, two columns wide
			cell = g.s
		}
	}
	style := lipgloss.NewStyle().Reverse(true)
	if c.bar {
		style = lipgloss.NewStyle().Foreground(cFocus).Underline(true)
	}
	return put(l, c.col, style.Render(cell))
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
