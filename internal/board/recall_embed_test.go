package board

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/recall"
	"github.com/kackerx/agmux/internal/store"
)

// A board with the recall screen's index ready (opened already), the sessions it shows as running,
// and the tmux around it: env says whether it is in an ordinary pane, a popup or --stay.
type emb struct {
	w *rworld
	r *fakeRunner
}

func newEmb(t *testing.T, clients string, env map[string]string, stay bool, live ...store.Session) (Model, emb) {
	t.Helper()
	w := newRWorld(t)
	w.fill()
	if err := w.ix.Update(); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{tmuxOut: map[string]string{"list-clients": clients, "display-message": "fl-1\n", "new-window": "%9\n"}}
	m := New(&mutSrc{ss: live}, r)
	m.getenv = func(k string) string { return env[k] }
	m.stay = stay
	m.rx = &recallIndex{ix: w.ix}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	return tm.(Model), emb{w, r}
}

var (
	paneEnv  = map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "TMUX_PANE": "%1"} // an ordinary tmux pane
	popupEnv = map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}                    // display-popup: no pane of its own
	stayEnv  = map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "TMUX_PANE": "%2"}
	userOnly = "5\t/dev/outer\tmain\txterm-kitty\t/dev/outer\n"
)

// bpress feeds keys to the board (not the recall model), running what they ask for like the runtime.
func bpress(t *testing.T, m Model, keys ...string) (Model, bool) {
	t.Helper()
	quit := false
	for _, k := range keys {
		tm, cmd := m.Update(key(k))
		m = tm.(Model)
		quit = run(&m, cmd) || quit
	}
	return m, quit
}

// openRecallOn presses ? and lets the index finish.
func openRecallOn(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(key("?"))
	m = tm.(Model)
	if m.rc == nil {
		t.Fatal("? did not open the recall screen")
	}
	tm, _ = m.Update(recallIndexedMsg{})
	return tm.(Model)
}

func btype(t *testing.T, m Model, text string) Model {
	t.Helper()
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return tm.(Model)
}

func TestBoardOpensAndLeavesRecall(t *testing.T) {
	m, _ := newEmb(t, userOnly, paneEnv, false)
	if m.rc != nil {
		t.Fatal("recall is not open before ?")
	}
	m = openRecallOn(t, m)
	v := rview(*m.rc)
	if got := m.View(); got != m.rc.View() || !strings.Contains(v, " / ▏") || len(m.rc.results) != 4 {
		t.Errorf("the board draws the recall screen:\n%s", v)
	}
	if !m.rc.embedded || m.rc.w != 140 || m.rc.h != 30 || m.rc.project == "" {
		t.Errorf("embedded %v size %dx%d project %q", m.rc.embedded, m.rc.w, m.rc.h, m.rc.project)
	}
	// the board's keys are not the board's now: x types, it does not ask to kill
	m = btype(t, m, "x")
	if m.rc.query != "x" || m.mode != browsing {
		t.Errorf("query %q mode %v", m.rc.query, m.mode)
	}
	// esc: the query first, then back to the board
	m, quit := bpress(t, m, "esc")
	if m.rc == nil || m.rc.query != "" || quit {
		t.Fatalf("esc clears the query: rc %v", m.rc != nil)
	}
	if m, quit = bpress(t, m, "ctrl+f"); !m.rc.panel {
		t.Fatal("panel")
	}
	if m, _ = bpress(t, m, "esc"); m.rc == nil || m.rc.panel {
		t.Error("esc closes the panel, not the screen")
	}
	if m, quit = bpress(t, m, "esc"); m.rc != nil || quit {
		t.Errorf("esc leaves for the board: rc %v quit %v", m.rc != nil, quit)
	}
	if strings.Contains(m.View(), " / ▏") || !strings.Contains(ansiStrip(m.View()), "0 sessions") {
		t.Errorf("the board again:\n%s", ansiStrip(m.View()))
	}
	// ? opens it again, ? with nothing typed leaves, ? after text is text
	m = openRecallOn(t, m)
	if m, _ = bpress(t, m, "?"); m.rc != nil {
		t.Error("? with an empty query goes back")
	}
	m = openRecallOn(t, m)
	m = btype(t, m, "a")
	if m, _ = bpress(t, m, "?"); m.rc == nil || m.rc.query != "a?" {
		t.Errorf("? after a letter is text: %v", m.rc)
	}
	// an unrelated message (the board keeps polling) is the board's; the mouse is not
	tm, _ := m.Update(tea.MouseMsg{X: 3, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if tm.(Model).rc == nil {
		t.Error("the mouse left the screen")
	}
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if mm := tm.(Model); mm.rc.w != 100 || mm.rc.h != 20 || mm.w != 100 {
		t.Errorf("a resize reaches both: %d %d %d", mm.rc.w, mm.rc.h, mm.w)
	}
}

func ansiStrip(s string) string { return ansi.Strip(s) }

// enter on a closed session, by where the board is: in an ordinary pane (or no tmux at all) the board
// ends and its Run hands the command over; in a popup or --stay a window of the user's session
// is made in the session's directory and the client goes there.
func TestBoardResumeClosedSession(t *testing.T) {
	want := func(m Model, e emb) []string {
		return []string{"claude", "--resume", "s-cart"}
	}
	for _, c := range []struct {
		name     string
		clients  string
		env      map[string]string
		stay     bool
		inPlace  bool
		client   string // the client switched
		wantSess string // the session the window is in
		detach   string
	}{
		{"ordinary pane", userOnly, paneEnv, false, true, "", "", ""},
		{"no tmux", "", map[string]string{}, false, true, "", "", ""},
		{"popup", userOnly, popupEnv, false, false, "/dev/outer", "main", ""},
		{"stay", stayClients, stayEnv, true, false, "/dev/outer", "main", "/dev/popup"},
	} {
		m, e := newEmb(t, c.clients, c.env, c.stay)
		m = openRecallOn(t, m)
		m, quit := bpress(t, m, "enter")
		dir := e.w.dir("shopcart")
		if c.inPlace {
			res := RecallOutcome(m)
			if !quit || res == nil || res.Dir != dir || !reflect.DeepEqual(res.Argv, want(m, e)) || len(e.r.calls) != 0 {
				t.Errorf("%s: quit %v result %+v tmux %q", c.name, quit, res, e.r.calls)
			}
			continue
		}
		nw := tmuxCalls(e.r, "new-window")
		wantNW := [][]string{{"tmux", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "=" + c.wantSess + ":", "-c", dir, "-n", "cart-fix", "--", "claude", "--resume", "s-cart"}}
		if !reflect.DeepEqual(nw, wantNW) {
			t.Errorf("%s: new-window %q\nwant %q", c.name, nw, wantNW)
		}
		if sw := tmuxCalls(e.r, "switch-client"); !reflect.DeepEqual(sw, [][]string{{"tmux", "switch-client", "-c", c.client, "-t", "%9"}}) {
			t.Errorf("%s: switch-client %q", c.name, sw)
		}
		if c.stay { // the board stays, in its own session, and the popup closes
			if quit || m.rc != nil || RecallOutcome(m) != nil {
				t.Errorf("%s: quit %v rc %v result %v", c.name, quit, m.rc != nil, RecallOutcome(m))
			}
			if d := tmuxCalls(e.r, "detach-client"); !reflect.DeepEqual(d, [][]string{{"tmux", "detach-client", "-t", c.detach}}) {
				t.Errorf("%s: detach %q", c.name, d)
			}
		} else if !quit || RecallOutcome(m) != nil { // a popup: the board ends with the switch
			t.Errorf("%s: quit %v result %v", c.name, quit, RecallOutcome(m))
		}
	}
}

// A session that runs is the board's own jump: switch to its pane. Nothing is resumed, in any place.
func TestBoardRecallEnterOnARunningSession(t *testing.T) {
	live := store.Session{ID: "x1", Name: "cart-fix", SessionID: "s-cart", TmuxPane: "%5", Status: "working"}
	for _, c := range []struct {
		name    string
		clients string
		env     map[string]string
		stay    bool
		client  string
	}{
		{"ordinary pane", userOnly, paneEnv, false, "/dev/outer"},
		{"popup", userOnly, popupEnv, false, "/dev/outer"},
		{"stay", stayClients, stayEnv, true, "/dev/outer"},
	} {
		m, e := newEmb(t, c.clients, c.env, c.stay, live)
		m = openRecallOn(t, m)
		m, quit := bpress(t, m, "enter")
		if sw := tmuxCalls(e.r, "switch-client"); !reflect.DeepEqual(sw, [][]string{{"tmux", "switch-client", "-c", c.client, "-t", "%5"}}) {
			t.Errorf("%s: switch-client %q", c.name, sw)
		}
		if len(tmuxCalls(e.r, "new-window")) != 0 || RecallOutcome(m) != nil || m.rc != nil {
			t.Errorf("%s: new-window %q result %v rc %v", c.name, tmuxCalls(e.r, "new-window"), RecallOutcome(m), m.rc != nil)
		}
		if quit == c.stay { // the board ends with a switch, except under --stay
			t.Errorf("%s: quit %v", c.name, quit)
		}
	}
	// the session is the board's: a background one with no pane is attached, as enter on its card does
	bg := store.Session{ID: "b1", Name: "cart-fix", SessionID: "s-cart", Background: true, Status: "idle"}
	m, e := newEmb(t, userOnly, paneEnv, false, bg)
	m = openRecallOn(t, m)
	bpress(t, m, "enter")
	if len(tmuxCalls(e.r, "new-window")) != 1 || !strings.Contains(strings.Join(tmuxCalls(e.r, "new-window")[0], " "), "claude attach b1") {
		t.Errorf("a background session is attached: %q", e.r.calls)
	}
}

// A directory that is gone, a command that is not there, a window tmux can not make: said, and nothing runs.
func TestBoardRecallEnterRefusals(t *testing.T) {
	m, e := newEmb(t, userOnly, popupEnv, false)
	m = openRecallOn(t, m)
	m = btype(t, m, "legacy")
	m, quit := bpress(t, m, "enter")
	if quit || m.rc == nil || !strings.Contains(rview(*m.rc), "directory is gone: /no/such/dir/gone-project") || len(e.r.calls) != 0 {
		t.Errorf("gone: quit %v calls %q", quit, e.r.calls)
	}

	m, e = newEmb(t, userOnly, popupEnv, false)
	m = openRecallOn(t, m)
	m.rc.lookPath = func(string) (string, error) { return "", fmt.Errorf("no") }
	if m, quit = bpress(t, m, "enter"); quit || !strings.Contains(rview(*m.rc), "claude not found in PATH") || len(e.r.calls) != 0 {
		t.Errorf("no command: quit %v calls %q", quit, e.r.calls)
	}

	m, e = newEmb(t, userOnly, popupEnv, false)
	e.r.tmuxFail = map[string]error{"new-window": fmt.Errorf("no space for another window")}
	m = openRecallOn(t, m)
	m = btype(t, m, "'coupon")
	m, quit = bpress(t, m, "enter")
	if quit || m.rc == nil || !strings.Contains(rview(*m.rc), "new-window: no space for another window") || len(tmuxCalls(e.r, "switch-client")) != 0 {
		t.Errorf("window failed: quit %v rc %v", quit, m.rc != nil)
	}
	if m.rc.query != "'coupon" || len(m.rc.results) != 1 {
		t.Errorf("the screen is as it was: query %q, %d results", m.rc.query, len(m.rc.results))
	}
	m, e = newEmb(t, userOnly, popupEnv, false)
	e.r.tmuxOut["new-window"] = "\n"
	m = openRecallOn(t, m)
	if m, quit = bpress(t, m, "enter"); quit || m.rc == nil || !strings.Contains(rview(*m.rc), "did not say which pane") {
		t.Errorf("no pane back: quit %v", quit)
	}
	// and no client to switch: said too, the board stays
	m, e = newEmb(t, "", popupEnv, false)
	m = openRecallOn(t, m)
	if m, quit = bpress(t, m, "enter"); quit || !strings.Contains(rview(*m.rc), "no tmux client") {
		t.Errorf("no client: quit %v\n%s", quit, rview(*m.rc))
	}
}

func TestBoardRecallCannotOpenTheIndex(t *testing.T) {
	m, _ := newEmb(t, userOnly, paneEnv, false)
	m.rx = &recallIndex{open: func() (*recall.Index, error) { return nil, fmt.Errorf("cannot locate the cache directory") }}
	tm, _ := m.Update(key("?"))
	m = tm.(Model)
	if m.rc != nil || !strings.Contains(ansiStrip(m.View()), "cannot locate the cache directory") {
		t.Errorf("rc %v\n%s", m.rc != nil, ansiStrip(m.View()))
	}
	// opened once, the index is kept for the next time
	opened := 0
	w := newRWorld(t)
	w.fill()
	m, _ = newEmb(t, userOnly, paneEnv, false)
	m.rx = &recallIndex{open: func() (*recall.Index, error) { opened++; return w.ix, nil }}
	for range 3 {
		m = openRecallOn(t, m)
		m, _ = bpress(t, m, "esc")
	}
	if opened != 1 {
		t.Errorf("the index was opened %d times", opened)
	}
	// ? is configurable like every key
	m, _ = newEmb(t, userOnly, paneEnv, false)
	tm, _ = m.Update(key("ctrl+f")) // not the board's key: nothing happens
	if tm.(Model).rc != nil {
		t.Error("ctrl+f opens nothing on the board")
	}
}

// What the screen's own tick and answers do after it was left is nothing (a stale message).
func TestBoardIgnoresStaleRecallMessages(t *testing.T) {
	m, _ := newEmb(t, userOnly, paneEnv, false)
	m = openRecallOn(t, m)
	m, _ = bpress(t, m, "esc")
	for _, msg := range []tea.Msg{recallTickMsg{}, recallIndexedMsg{}, recallEditedMsg{}} {
		tm, cmd := m.Update(msg)
		if tm.(Model).rc != nil || cmd != nil {
			t.Errorf("%T: rc %v cmd %v", msg, tm.(Model).rc != nil, cmd != nil)
		}
	}
	// the editor came back while the screen is on: the mouse is turned on again
	m = openRecallOn(t, m)
	if _, cmd := m.Update(recallEditedMsg{}); cmd == nil {
		t.Error("after the editor the board wants its mouse back")
	}
}

// A running session with no pane has nowhere to be jumped to: the screen stays and says so (the board
// would drop its focus and say nothing).
func TestBoardRecallEnterOnARunningSessionWithNoPane(t *testing.T) {
	m, e := newEmb(t, userOnly, paneEnv, false, store.Session{ID: "x1", SessionID: "s-cart", Status: "working"})
	m = openRecallOn(t, m)
	m, quit := bpress(t, m, "enter")
	if quit || m.rc == nil || !strings.Contains(rview(*m.rc), "cart-fix is running") || len(e.r.calls) != 0 {
		t.Errorf("quit %v calls %q", quit, e.r.calls)
	}
}

// The window is made in the session of the client that is switched, never in the board's own.
func TestNewResumeWindowNeedsTheClientsSession(t *testing.T) {
	r := &fakeRunner{tmuxOut: map[string]string{"list-clients": "5\t/dev/outer\t\n", "new-window": "%9\n"}}
	if pane, err := newResumeWindow(r, func(string) string { return "" }, RecallResult{Dir: "/d", Argv: []string{"claude"}}, "n"); err == nil || pane != "" || len(tmuxCalls(r, "new-window")) != 0 {
		t.Errorf("pane %q err %v calls %q", pane, err, r.calls)
	}
}
