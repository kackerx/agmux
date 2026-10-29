package board

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// T87: bubbletea v1 can hand an SGR mouse report over cut after "ESC [": "<64;174;28M" arrives as
// text. It is not text, in whichever key reader it lands.
func TestWithoutMouseTail(t *testing.T) {
	for _, c := range []struct {
		name string
		in   tea.KeyMsg
		want string
		ok   bool
	}{
		{"one wheel report", runes("<64;174;28M"), "", false},
		{"two of them, as seen on the screen", runes("<64;174;28M<64;174;28M"), "", false},
		{"a release (m)", runes("<0;5;5m"), "", false},
		{"text around it stays", runes("ab<65;1;2Mcd"), "abcd", true},
		{"plain text", runes("fix the <b> tag"), "fix the <b> tag", true},
		{"numbers and a semicolon are text", runes("1;2;3M"), "1;2;3M", true},
		{"a paste is the user's own", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("<64;174;28M"), Paste: true}, "<64;174;28M", true},
		{"not a rune key", tea.KeyMsg{Type: tea.KeyEnter}, "", true},
	} {
		k, ok := withoutMouseTail(c.in)
		if ok != c.ok || (ok && c.in.Type == tea.KeyRunes && string(k.Runes) != c.want) {
			t.Errorf("%s: %q ok %v, want %q ok %v", c.name, string(k.Runes), ok, c.want, c.ok)
		}
	}
}

// The recall input, typing: the tail does not go in, the text around it does; the locked state does
// not read it as keys either.
func TestRecallInputIgnoresMouseTail(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := rtype(t, w.model(), "ab")
	m = rtype(t, m, "<64;174;28M<64;174;28M")
	if m.query != "ab" || m.qpos != 2 {
		t.Errorf("query %q caret %d after the tail", m.query, m.qpos)
	}
	if m = rtype(t, m, "c<65;9;9Md"); m.query != "abcd" {
		t.Errorf("query %q, want abcd", m.query)
	}
	locked, _ := rpress(t, m, "enter")
	if after := rtype(t, locked, "<0;5;5m"); after.query != "abcd" || !after.locked {
		t.Errorf("locked: query %q locked %v", after.query, after.locked)
	}
}

// The board's / filter and the recall screen inside the board drop the tail; the locked pane is sent what came.
func TestBoardIgnoresMouseTail(t *testing.T) {
	m := wide(&fakeRunner{}, 232, 50)
	m, _ = press(t, m, "/")
	tm, _ := m.Update(runes("<64;174;28M"))
	if m = tm.(Model); m.query != "" {
		t.Errorf("the / filter took %q", m.query)
	}
	tm, _ = m.Update(runes("x<64;174;28My"))
	if m = tm.(Model); m.query != "xy" {
		t.Errorf("the / filter has %q, want xy", m.query)
	}

	r := &fakeRunner{}
	p, _ := press(t, wide(r, 232, 50), "i") // pass-through on a1 / %1
	r.calls = nil
	_, cmd := p.Update(runes("<64;174;28M"))
	run(&p, cmd)
	if got := sentKeys(r); len(got) != 1 || !strings.Contains(got[0], "<64;174;28M") { // the pane gets what came, whole: not filtered
		t.Errorf("sent to the pane: %q", got)
	}

	e, _ := newEmb(t, userOnly, paneEnv, false)
	e = openRecallOn(t, e)
	tm, _ = e.Update(runes("<64;174;28M"))
	if e = tm.(Model); e.rc == nil || e.rc.query != "" {
		t.Errorf("recall in the board took the tail: %+v", e.rc)
	}
}
