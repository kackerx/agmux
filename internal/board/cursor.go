package board

import (
	"fmt"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// cursorOut is the board's stdout with one addition: bubbletea ends every frame by putting the
// terminal cursor at the start of the last line (v1 has no API for anything else), and an input
// method follows the terminal cursor. So after each write the position View last asked for is
// written too: where the pane's cursor is, in the preview.
//
// It embeds *os.File so that it is still a term.File: bubbletea reads the window size from it.
//
// Only Write appends the position. The renderer's own sequences (alt screen, show / hide cursor)
// go out through io.WriteString, which finds the promoted (*os.File).WriteString and so never
// comes here: nothing is appended after the one that leaves the alt screen, where the position
// would land on the main screen. Giving cursorOut a WriteString would break that.
//
// Nor is it an *os.File: a child run with tea.ExecProcess whose Stdout is nil gets the program's
// output, and as a pipe it is no terminal (see Model.review).
type cursorOut struct {
	*os.File
	mu  sync.Mutex
	pos []byte // the cursor-position sequence to put after a write; nil: leave the cursor where it is
}

// place asks for the cursor on the 0-based screen cell (x, y); x < 0 takes it back.
func (c *cursorOut) place(x, y int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pos = nil
	if x >= 0 && y >= 0 {
		c.pos = []byte(fmt.Sprintf("\x1b[%d;%dH", y+1, x+1))
	}
}

// Write is one write to the file, so the frame and the cursor position reach the terminal together.
func (c *cursorOut) Write(p []byte) (int, error) {
	c.mu.Lock()
	pos := c.pos
	c.mu.Unlock()
	if pos == nil {
		return c.File.Write(p)
	}
	n, err := c.File.Write(append(p[:len(p):len(p)], pos...))
	return min(n, len(p)), err
}

// pvBox is the preview region and, when keys go to the pane and its cursor is drawn, the screen
// cell (0-based) of that cursor: the real terminal cursor goes there. -1: nowhere.
func (m Model) pvBox() (right []string, x, y int) {
	_, _, focused, ok := m.current()
	pass := m.passThrough()
	right, row, col := previewBox(focused, ok, m.pv, m.w-m.cardsW(), max(m.h-3, 1), m.scroll, pass)
	if !pass || row < 0 {
		return right, -1, -1
	}
	return right, m.cardsW() + col, 1 + row // the region starts to the right of the cards, below the header row
}

// searchCount is what the top bar says on the right while a filter is on: how many cards it shows of
// how many there are.
func (m Model) searchCount(shown int) string {
	return fmt.Sprintf("%d/%d ", shown, len(m.src.Sessions()))
}

// searchRoom is the cells the filter's text has in the top bar: what is between " / " and the count.
func (m Model) searchRoom(shown int) int { return m.w - 3 - ansi.StringWidth(m.searchCount(shown)) - 1 }

// searchCursor is the screen cell (0-based) of the / filter's caret, for the terminal's cursor (an
// input method puts its candidates there): the top bar, after " / " and the text before the caret,
// cut the way the bar cuts it. Only while the box has the keys; shown is how many cards it shows.
func (m Model) searchCursor(shown int) (x, y int, ok bool) {
	if !m.searching || m.rc != nil || m.w == 0 {
		return 0, 0, false
	}
	return 3 + queryCaretCells(m.query, m.qpos, m.searchRoom(shown)), 0, true
}

// wantCursor: the terminal's cursor should be shown (on the pane's cursor) rather than hidden.
func (m Model) wantCursor() bool {
	if m.curOut != nil && m.rc != nil { // the recall screen: its input's caret
		_, _, ok := m.rc.cursorAt()
		return ok
	}
	if m.curOut != nil && m.searching { // the / filter's caret
		ss, _ := m.shown()
		_, _, ok := m.searchCursor(len(ss))
		return ok
	}
	if m.curOut == nil || !m.previewOn() {
		return false
	}
	_, x, _ := m.pvBox()
	return x >= 0
}

// Update is update, and then the cursor shown or hidden to match what the frame will say.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	tm, cmd := m.update(msg)
	nm := tm.(Model)
	if want := nm.wantCursor(); want != nm.curOn {
		nm.curOn = want
		show := tea.HideCursor
		if want {
			show = tea.ShowCursor
		}
		cmd = tea.Batch(cmd, show)
	}
	return nm, cmd
}
