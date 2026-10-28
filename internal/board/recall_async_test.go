package board

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/config"
)

// Until the answer of a search is there the list is the old query's: nothing is resumed, copied or
// opened from it.
func TestRecallDoesNotActOnAListThatIsNotTheQuerys(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	copied, edited := 0, 0
	m.copyToClip = func(string) error { copied++; return nil }
	m.editor = func() string { edited++; return "true" }
	tm, search := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	for _, k := range []string{"enter", "ctrl+y", "ctrl+o"} {
		if got, cmd := tm.Update(key(k)); cmd != nil || got.(recallModel).msg != "" || got.(recallModel).res != nil {
			t.Errorf("%s while searching: cmd %v msg %q", k, cmd != nil, got.(recallModel).msg)
		}
	}
	if copied != 0 || edited != 0 {
		t.Errorf("copied %d, editor %d", copied, edited)
	}
	// enter and the jump key are two things: with jump moved off enter, both are held back
	c, err := config.Parse("cfg", []byte("[keys]\njump = [\"ctrl+g\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.cfg = c
	jumped, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	for _, k := range []string{"enter", "ctrl+g"} {
		if got, cmd := jumped.Update(key(k)); cmd != nil || got.(recallModel).res != nil {
			t.Errorf("%s with jump on ctrl+g, while searching: cmd %v", k, cmd != nil)
		}
	}
	// and when the answer is there they work again
	tm, _ = tm.Update(search())
	tm.Update(key("ctrl+y"))
	if copied != 1 {
		t.Errorf("copied %d after the answer", copied)
	}
}

// The answer of a search of another screen (one that was left) is not this screen's.
func TestRecallSearchNumbersAreOnePerProcess(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	_, ca := w.model().search(false)
	older := ca().(recallFoundMsg)
	b, _ := w.model().search(false)
	tm, _ := b.Update(older)
	if !tm.(recallModel).searching {
		t.Error("the answer of another screen ended the wait of this one")
	}
}

// A tick brings the results up to date while the index is built, but not over a search that is still
// out (a slow one would never be shown), and it does nothing when the index has not changed.
func TestRecallTickSearches(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	if _, cmd := m.Update(recallTickMsg{}); cmd != nil {
		t.Error("nothing new in the index: a tick asks for nothing")
	}
	w.claude("s-new", w.dir("shopcart"), "fresh", 10*time.Minute, "u: a brand new session")
	w.ix.Update()
	m.indexing = true
	tm, cmd := m.Update(recallTickMsg{})
	found := 0
	if b, ok := cmd().(tea.BatchMsg); ok { // the search and the next tick
		for _, c := range b {
			if f, ok := c().(recallFoundMsg); ok && len(f.results) == 5 {
				found++
			}
		}
	}
	if found != 1 || !tm.(recallModel).searching {
		t.Fatalf("while it is built a tick searches and ticks again: %d answers", found)
	}
	// a search is out: the tick leaves it alone, and only ticks
	before := tm.(recallModel).qgen
	w.claude("s-newer", w.dir("shopcart"), "fresher", 5*time.Minute, "u: one more")
	w.ix.Update()
	tm, _ = tm.Update(recallTickMsg{})
	if tm.(recallModel).qgen != before {
		t.Error("a tick over a search that is out asked again")
	}
}
