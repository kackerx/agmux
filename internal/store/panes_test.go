package store

import (
	"errors"
	"reflect"
	"testing"
)

// The pane of a background session is the one `claude attach <id>` was started in; quotes and a
// path before claude do not matter, the first one wins, and anything else is not it.
func TestAttachPanes(t *testing.T) {
	listing := "%0\tmain\t0\t\n" +
		"%3\tagmux-bg\t0\tclaude attach a7a826e7\n" +
		"%4\tagmux-bg\t0\t\"claude\" \"attach\" \"b2\"\n" +
		"%5\tother session\t0\t/usr/local/bin/claude attach c3 --extra\n" +
		"%6\tagmux-bg\t0\tclaude attach a7a826e7\n" +
		"%7\tmain\t0\tclaude --resume a7a826e7\n" +
		"%8\tmain\t0\tvim attach d4\n" +
		"%9\tmain\t0\tclaude attach\n" +
		"%12\tagmux # main\t0\tclaude attach d9\t✳ Title\n" + // a session named "<repo> # <branch>" is the repo; a title column is not the command
		"%11\tagmux-bg\t1\tclaude attach dead1\n" + // remain-on-exit: the process is gone, the window is not
		"no tab here\n" +
		"%10\tagmux-bg\tclaude attach f6\n" +
		"\tmain\t0\tclaude attach e5\n"
	want := map[string]attachedPane{"a7a826e7": {"%3", "agmux-bg"}, "b2": {"%4", "agmux-bg"}, "c3": {"%5", "other session"}, "d9": {"%12", "agmux"}}
	if got := attachPanes(listing); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := attachPanes(""); len(got) != 0 {
		t.Errorf("nothing listed: %v", got)
	}
}

// The listing reads with or without the title column, a command with a tab in it, and a session
// named "<repo> # <branch>" is the repo.
func TestPaneLine(t *testing.T) {
	for _, c := range []struct {
		line                string
		ok, dead            bool
		pane, sess, cmd, ti string
		pid                 int
	}{
		{"%1\tmain\t0\tzsh", true, false, "%1", "main", "zsh", "", 0},
		{"%1\tmain\t0\tzsh\t✳ Title", true, false, "%1", "main", "zsh", "✳ Title", 0},
		{"%1\tmain\t0\tzsh\t✳ Title\t4242", true, false, "%1", "main", "zsh", "✳ Title", 4242},
		{"%1\tmain\t1\ta\tb\tTitle\t77", true, true, "%1", "main", "a\tb", "Title", 77}, // a command with a tab
		{"%1\tmain\t0\tzsh\t\t5", true, false, "%1", "main", "zsh", "", 5},
		{"%1\tmain\t0\tzsh\t", true, false, "%1", "main", "zsh", "", 0},
		{"%1\tmain\t0", false, false, "", "", "", "", 0},
		{"\tmain\t0\tzsh", false, false, "", "", "", "", 0},
		{"", false, false, "", "", "", "", 0},
	} {
		pane, sess, dead, cmd, title, pid, ok := paneLine(c.line)
		if ok != c.ok || ok && (pane != c.pane || sess != c.sess || dead != c.dead || cmd != c.cmd || title != c.ti || pid != c.pid) {
			t.Errorf("%q: %v %q %q %v %q %q %d", c.line, ok, pane, sess, dead, cmd, title, pid)
		}
	}
	for in, want := range map[string]string{"agmux # main": "agmux", "my session": "my session", "a # b # c": "a", "": "", " # x": ""} {
		if got := shortSession(in); got != want {
			t.Errorf("shortSession(%q) = %q, want %q", in, got, want)
		}
	}
}

// A background session with no pane gets the attached one and is still background; a session
// with a pane is in the tmux session tmux says (its record names it as it was at start); one that
// has no pane and is not background is left alone; tmux is asked only when it matters, and failing
// is the same as there being nothing.
func TestFillPanes(t *testing.T) {
	asked, fail := 0, error(nil)
	raw := []Raw{
		{ID: "a7", Background: true},
		{ID: "b2", Background: true, Pane: "%1", TmuxSession: "stale"},
		{ID: "c3"},
		{ID: "d4", Background: true},
		{ID: "e5", Pane: "%9", TmuxSession: "old name"},
		{ID: "f6", Pane: "%77", TmuxSession: "as recorded"}, // not in the listing
		{ID: "g7", Pane: "%8", TmuxSession: "x"},
	}
	p := &Poller{Sources: []Source{srcFunc(func() ([]Raw, error) { return raw, nil })}, StateDir: t.TempDir(),
		Panes: func() (string, error) {
			asked++
			return "%1\tmain\t0\tzsh\n%3\tagmux-bg\t0\tclaude attach a7\n%4\tagmux-bg\t0\tclaude attach b2\n%5\tagmux-bg\t0\tclaude attach c3\n%9\tnew name\t0\tclaude\n%8\tagmux # main\t0\tclaude\n", fail
		}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	ss := p.Sessions()
	if got := []string{ss[0].TmuxPane, ss[1].TmuxPane, ss[2].TmuxPane, ss[3].TmuxPane}; !reflect.DeepEqual(got, []string{"%3", "%1", "", ""}) {
		t.Errorf("panes %q", got)
	}
	if got := []string{ss[0].TmuxSession, ss[1].TmuxSession, ss[2].TmuxSession, ss[4].TmuxSession, ss[5].TmuxSession, ss[6].TmuxSession}; !reflect.DeepEqual(got, []string{"agmux-bg", "main", "", "new name", "as recorded", "agmux"}) {
		t.Errorf("sessions %q: the attached card is in agmux-bg, the others where tmux has their pane", got)
	}
	if !ss[0].Background || asked != 1 {
		t.Errorf("background %v, asked %d times", ss[0].Background, asked)
	}
	fail = errors.New("tmux: no server")
	if err := p.Refresh(); err != nil || p.Sessions()[0].TmuxPane != "" || p.Sessions()[4].TmuxSession != "old name" || p.Err() != nil {
		t.Errorf("tmux failing: pane %q, err %v", p.Sessions()[0].TmuxPane, p.Err())
	}
	raw = raw[2:3] // no pane, not background: nothing to ask
	asked = 0
	p.Refresh()
	if asked != 0 {
		t.Errorf("asked tmux %d times with nothing that has or needs a pane", asked)
	}
}
