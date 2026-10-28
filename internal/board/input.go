package board

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The one-line input of the board's / and of the recall screen: a query (a string) and the caret in
// it (a rune offset). The editing keys are fixed, not in [keys] (like backspace and ctrl+c):
//
//	left / right        the caret one character
//	home / ctrl+a       to the start        end / ctrl+e      to the end
//	backspace / ctrl+h  the character before the caret
//	alt+backspace / ctrl+w   the word before it (a word is letters and digits; spaces and
//	                         punctuation are what is between)
//	anything printable  is put in at the caret (a paste may carry control characters: spaces)
//
// Kitty sends option+backspace as ESC DEL, which bubbletea reads as alt+backspace.

// editQuery applies k to the input; ok says k was an editing key (the caller then has nothing to
// do with it). The caret is kept inside the text.
func editQuery(q string, pos int, k tea.KeyMsg) (string, int, bool) {
	r := []rune(q)
	pos = min(max(pos, 0), len(r))
	cut := func(from int) (string, int, bool) { return string(append(r[:from:from], r[pos:]...)), from, true }
	switch k.String() {
	case "left":
		return q, max(pos-1, 0), true
	case "right":
		return q, min(pos+1, len(r)), true
	case "home", "ctrl+a":
		return q, 0, true
	case "end", "ctrl+e":
		return q, len(r), true
	case "backspace", "ctrl+h":
		return cut(max(pos-1, 0))
	case "alt+backspace", "ctrl+w":
		from := pos
		for from > 0 && !isWordRune(r[from-1]) {
			from--
		}
		for from > 0 && isWordRune(r[from-1]) {
			from--
		}
		return cut(from)
	}
	if (k.Type == tea.KeyRunes || k.Type == tea.KeySpace) && !k.Alt { // alt+e is a key, not the text "e"
		in := []rune(strings.Map(func(c rune) rune {
			if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) { // Cf: U+202E and friends
				return ' '
			}
			return c
		}, string(k.Runes)))
		return string(append(append(r[:pos:pos], in...), r[pos:]...)), pos + len(in), true
	}
	return q, pos, false
}

func isWordRune(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' }

// queryCells is how many cells the text before the caret takes (a Chinese character is two).
func queryCells(q string, pos int) int {
	r := []rune(q)
	return ansi.StringWidth(string(r[:min(max(pos, 0), len(r))]))
}

// queryShown is the part of the text that is drawn in width cells, and the caret in it: what is
// before the caret goes from the left when the caret would not fit.
func queryShown(q string, pos, width int) ([]rune, int) {
	r := []rune(q)
	pos = min(max(pos, 0), len(r))
	for len(r) > 0 && width > 0 && ansi.StringWidth(string(r[:pos]))+1 > width && pos > 0 {
		r, pos = r[1:], pos-1
	}
	return r, pos
}

// queryCaretCells is how many cells from the start of the drawn text the caret is at.
func queryCaretCells(q string, pos, width int) int {
	r, pos := queryShown(q, pos, width)
	return ansi.StringWidth(string(r[:pos]))
}

// queryView is the input drawn in at most width cells: with the caret (a cell in reverse video,
// a space at the end) when typing, else the plain text. When it does not fit, what is before the
// caret goes from the left so the caret stays on screen.
func queryView(q string, pos int, caret bool, width int) string {
	r, pos := queryShown(q, pos, width)
	if !caret {
		return string(r)
	}
	at := " "
	if pos < len(r) {
		at = string(r[pos])
	}
	rest := ""
	if pos+1 < len(r) {
		rest = string(r[pos+1:])
	}
	return string(r[:pos]) + lipgloss.NewStyle().Reverse(true).Render(at) + rest
}
