package store

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/kackerx/agmux/internal/ccmux"
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
		"%11\tagmux-bg\t1\tclaude attach dead1\n" + // remain-on-exit: the process is gone, the window is not
		"no tab here\n" +
		"%10\tagmux-bg\tclaude attach f6\n" +
		"\tmain\t0\tclaude attach e5\n"
	want := map[string]attachedPane{"a7a826e7": {"%3", "agmux-bg"}, "b2": {"%4", "agmux-bg"}, "c3": {"%5", "other session"}}
	if got := attachPanes(listing); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := attachPanes(""); len(got) != 0 {
		t.Errorf("nothing listed: %v", got)
	}
}

// A background session with no pane gets the attached one and is still background; an ordinary
// one, or one ccmux gave a pane, is left alone; tmux is asked only when it matters, and failing
// is the same as there being nothing.
func TestFillAttached(t *testing.T) {
	asked, fail := 0, error(nil)
	raw := []ccmux.Session{
		{ID: "a7", TrackingMode: "background", LastPrompt: "hi"},
		{ID: "b2", TrackingMode: "background", LastPrompt: "hi", TmuxPane: "%1"},
		{ID: "c3", TrackingMode: "native"},
		{ID: "d4", TrackingMode: "background", LastPrompt: "hi"},
	}
	p := &Poller{Show: func() ([]ccmux.Session, error) { return raw, nil }, StateDir: t.TempDir(),
		Panes: func() (string, error) {
			asked++
			return "%3\tagmux-bg\t0\tclaude attach a7\n%4\tagmux-bg\t0\tclaude attach b2\n%5\tagmux-bg\t0\tclaude attach c3\n", fail
		}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	ss := p.Sessions()
	if got := []string{ss[0].TmuxPane, ss[1].TmuxPane, ss[2].TmuxPane, ss[3].TmuxPane}; !reflect.DeepEqual(got, []string{"%3", "%1", "", ""}) {
		t.Errorf("panes %q", got)
	}
	if ss[0].TmuxSession != "agmux-bg" || ss[1].TmuxSession != "" || ss[2].TmuxSession != "" {
		t.Errorf("sessions %q %q %q: the attached card is in agmux-bg, the others as ccmux said", ss[0].TmuxSession, ss[1].TmuxSession, ss[2].TmuxSession)
	}
	if !ss[0].Background || asked != 1 {
		t.Errorf("background %v, asked %d times", ss[0].Background, asked)
	}
	fail = errors.New("tmux: no server")
	if err := p.Refresh(); err != nil || p.Sessions()[0].TmuxPane != "" || p.Err() != nil {
		t.Errorf("tmux failing: pane %q, err %v", p.Sessions()[0].TmuxPane, p.Err())
	}
	raw = raw[1:3] // nothing in it needs a pane any more
	asked = 0
	p.Refresh()
	if asked != 0 {
		t.Errorf("asked tmux %d times with no background session without a pane", asked)
	}
}

// ccmux's mainRepoRoot reaches the session: the board groups a background session by it.
func TestMainRepoRootConverted(t *testing.T) {
	p := poller(t, []ccmux.Session{{ID: "a", MainRepoRoot: "/w/main"}})
	if got := p.Sessions()[0].MainRepoRoot; got != "/w/main" {
		t.Errorf("MainRepoRoot %q", got)
	}
}

// A background record with no transcript and no prompt is a spare process of the claude daemon,
// not a session; a record with a prompt or a transcript is one, and so is a native one without.
func TestSpareProcessesAreNoSessions(t *testing.T) {
	p := poller(t, []ccmux.Session{
		{ID: "spare", TrackingMode: "background", Prompts: []string{}},
		{ID: "new", TrackingMode: "background", LastPrompt: "hi"},
		{ID: "listed", TrackingMode: "background", Prompts: []string{"hi"}},
		{ID: "logged", TrackingMode: "background", LogPath: "/nope/x.jsonl"},
		{ID: "native", TrackingMode: "native"},
		{ID: "pane", TrackingMode: "pane"},
	})
	var got []string
	for _, s := range p.Sessions() {
		got = append(got, s.ID)
	}
	if want := []string{"new", "listed", "logged", "native", "pane"}; !slices.Equal(got, want) {
		t.Errorf("sessions %v, want %v", got, want)
	}
}
