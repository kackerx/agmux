package board

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// previewOn: asked for with P and the terminal is wide enough right now.
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

// previewMsg is one capture of pane.
type previewMsg struct {
	pane, text string
	err        error
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
	r, pane, n := m.run, s.TmuxPane, m.previewLinesN()
	return func() tea.Msg {
		out, err := r.Tmux("capture-pane", "-p", "-e", "-t", pane, "-S", "-"+strconv.Itoa(n))
		return previewMsg{pane, out, err}
	}
}

// previewLines renders the preview region: h lines, each exactly w wide.
//
//	 sqlmux-orchestrator · %7 · ⏵ working · Bash
//	╭──────────────────────────────╮
//	│ …the pane's last lines…      │
//	╰──────────────────────────────╯
func previewLines(s store.Session, ok bool, pv previewMsg, w, h int) []string {
	inner := w - 1 - 4 // one column of gap from the cards, then "│ " … " │"
	title := ""
	var content []string
	switch {
	case !ok || s.TmuxPane == "":
		content = []string{fg(cMuted, "没有 pane")}
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
			content = tail(pv.text, h-3)
		}
	}
	out := []string{pad(ansi.Truncate(" "+title, w, "…"), w)}
	out = append(out, " "+fg(cDim, "╭"+strings.Repeat("─", inner+2)+"╮"))
	for i := range max(h-3, 0) {
		l := ""
		if i < len(content) {
			l = ansi.Truncate(content[i], inner, "")
		}
		// the reset keeps the pane's own colors from bleeding into the border
		out = append(out, " "+fg(cDim, "│")+" "+pad(l, inner)+"\x1b[0m "+fg(cDim, "│"))
	}
	out = append(out, " "+fg(cDim, "╰"+strings.Repeat("─", inner+2)+"╯"))
	return out[:h]
}

// tail is the last n lines of a capture, without the blank rows below the cursor.
func tail(text string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines[max(len(lines)-n, 0):]
}
