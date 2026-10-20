package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func piDir(t *testing.T, files map[string]string) *Pi {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Pi{Dir: dir, Alive: func(pid int) bool { return pid != 999 }}
}

func piFile(pid int, rest string) string {
	return fmt.Sprintf(`{"pid":%d,"sessionId":"pi-%d","cwd":"/w/p","name":"n%d","status":"working","pendingTool":"bash","statusAt":1792483390000,"updatedAt":1792483395000%s}`, pid, pid, pid, rest)
}

// What the extension's files make: a session per live process, in pi's own words mapped to Raw;
// a dead pid, a pid that is another process now, and files that do not parse are not there.
func TestPiSource(t *testing.T) {
	c := piDir(t, map[string]string{
		"1.json":          piFile(1, ""),
		"2.json":          piFile(2, `,"status":"waiting"`),
		"3.json":          piFile(3, `,"status":"gone wrong","statusAt":0`),
		"4.json":          piFile(4, ""), // started after the file was written: the pid is used again
		"999.json":        piFile(999, ""),
		"5.json":          `{"pid":5,"sessionId":`,
		"6.json":          `{"pid":6}`,   // no session id
		"7.json":          piFile(7, ""), // gone from ps
		"1.json.tmp.1792": "half",
	})
	c.StartsOf = func([]int) (map[int]string, error) {
		old, late := time.UnixMilli(1792483395000).UTC().Add(-time.Hour), time.UnixMilli(1792483395000).UTC().Add(time.Hour)
		f := func(t time.Time) string { return t.Format("Mon Jan _2 15:04:05 2006") }
		return map[int]string{1: f(old), 2: f(old), 3: f(old), 4: f(late)}, nil
	}
	rs, err := c.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Raw{}
	for _, r := range rs {
		got[r.ID] = r
	}
	if len(got) != 3 || got["pi-1"].PID != 1 || got["pi-2"].PID != 2 || got["pi-3"].PID != 3 {
		t.Fatalf("listed %d: %v", len(got), got)
	}
	r := got["pi-1"]
	if r.AgentType != "pi" || r.SessionID != "pi-1" || r.CWD != "/w/p" || r.Name != "n1" || r.Status != "working" || r.PendingTool != "bash" || r.Attention != "" || r.Background || r.Pane != "" ||
		!r.StatusChangedAt.Equal(time.UnixMilli(1792483390000)) || !r.LastActivity.Equal(time.UnixMilli(1792483395000)) || r.Transcript != "" {
		t.Errorf("fields: %+v", r)
	}
	if r := got["pi-2"]; r.Status != "waiting" || r.Attention != "question" {
		t.Errorf("waiting: %+v", r)
	}
	if r := got["pi-3"]; r.Status != "idle" || !r.StatusChangedAt.Equal(r.LastActivity) {
		t.Errorf("unknown status and no statusAt: %+v", r)
	}
	// ps failing lists everything alive; no directory or no file lists nothing
	c.StartsOf = func([]int) (map[int]string, error) { return nil, fmt.Errorf("no ps") }
	if rs, _ := c.Sessions(); len(rs) != 5 {
		t.Errorf("without ps: %d", len(rs))
	}
	if rs, err := (&Pi{Dir: filepath.Join(t.TempDir(), "none")}).Sessions(); len(rs) != 0 || err != nil {
		t.Errorf("no directory: %v %v", rs, err)
	}
	if rs, err := (&Pi{}).Sessions(); len(rs) != 0 || err != nil {
		t.Errorf("no dir at all: %v %v", rs, err)
	}
}

// A pi session's transcript is for resuming it: it reaches the Session, and is not read for names, models or branches.
func TestPiTranscriptIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"custom-title","customTitle":"read by mistake"}`+"\n"+`{"gitBranch":"mistake"}`+"\n"), 0o644)
	p := poller(t, []Raw{{ID: "p", AgentType: "pi", PID: 77, SessionID: "sid", Transcript: path, Project: "proj"}, {ID: "c", AgentType: "claude", PID: 78, SessionID: "sid2", Transcript: path, Project: "proj"}})
	pi, cl := byID(t, p.Sessions(), "p"), byID(t, p.Sessions(), "c")
	if pi.Transcript != path || pi.PID != 77 || pi.SessionID != "sid" || pi.Name != "proj" || pi.Branch == "mistake" {
		t.Errorf("pi: %+v", pi)
	}
	if cl.Transcript != path || cl.Name != "read by mistake" || cl.Branch != "mistake" {
		t.Errorf("claude's is read: %+v", cl)
	}
}

func TestParseStart(t *testing.T) {
	for in, want := range map[string]string{
		"Tue Oct 20 07:34:09 2026": "2026-10-20T07:34:09Z",
		"Tue Oct  6 07:34:09 2026": "2026-10-06T07:34:09Z",
		"Tue Oct 6 07:34:09 2026":  "2026-10-06T07:34:09Z",
	} {
		if got, ok := parseStart(in); !ok || got.Format(time.RFC3339) != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	if _, ok := parseStart(""); ok {
		t.Error("empty")
	}
}

// A session with a pid and no pane is put in the pane whose process is the first ancestor of it: three
// levels up (pi is started by a wrapper script, that by the shell the pane runs), or the nearest if two are; none
// above, none. A register's pane, a background session and a session with no pid stay as they are.
func TestPlaceByPID(t *testing.T) {
	listing := "%1\tmain\t0\tzsh\t\t100\n%2\tmain\t0\tzsh\t\t200\n%3\tother # b\t0\tzsh\tπ - x\t300\n"
	table := procs{parent: map[int]int{10: 20, 20: 30, 30: 100, 11: 200, 12: 300, 300: 200, 50: 51, 51: 1, 70: 71, 71: 70}}
	asked := 0
	raw := []Raw{
		{ID: "deep", PID: 10},     // 10 → 20 → 30 → 100: %1
		{ID: "nearest", PID: 12},  // 12 → 300: %3, not 200's
		{ID: "own", PID: 11},      // 11 → 200
		{ID: "none", PID: 50},     // 50 → 51 → 1: no pane
		{ID: "unknown", PID: 777}, // not in the table
		{ID: "cycle", PID: 70},    // a table that goes round
		{ID: "claude", PID: 10, Pane: "%9", TmuxSession: "x"},
		{ID: "bg", PID: 10, Background: true},
		{ID: "nopid"},
	}
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return raw, nil })}, StateDir: t.TempDir(),
		Panes: func() (string, error) { return listing, nil },
		Procs: func() (procs, error) { asked++; return table, nil }}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{"deep": {"%1", "main"}, "nearest": {"%3", "other"}, "own": {"%2", "main"}, "none": {"", ""}, "unknown": {"", ""}, "cycle": {"", ""}, "claude": {"%9", "x"}, "bg": {"", ""}, "nopid": {"", ""}}
	for _, s := range p.Sessions() {
		if w := want[s.ID]; s.TmuxPane != w[0] || s.TmuxSession != w[1] {
			t.Errorf("%s: pane %q session %q, want %v", s.ID, s.TmuxPane, s.TmuxSession, w)
		}
	}
	if asked != 1 {
		t.Errorf("the process table was read %d times in a poll", asked)
	}
	if raw[0].Pane != "" {
		t.Error("the source's own records were changed")
	}
	// nothing to place, no ps; a failing ps places nothing
	asked = 0
	raw = []Raw{{ID: "x", PID: 10, Pane: "%1"}}
	p.Refresh()
	if asked != 0 {
		t.Errorf("read the process table with nothing to place: %d", asked)
	}
	raw = []Raw{{ID: "deep", PID: 10}}
	p.Procs = func() (procs, error) { return procs{}, fmt.Errorf("no ps") }
	if err := p.Refresh(); err != nil || p.Sessions()[0].TmuxPane != "" {
		t.Errorf("failing ps: %v %q", err, p.Sessions()[0].TmuxPane)
	}
}

func TestPsProcs(t *testing.T) {
	tb, err := psProcs()
	if err != nil || tb.parent[os.Getpid()] != os.Getppid() || tb.comm[os.Getpid()] == "" {
		t.Errorf("%v %d %q", err, tb.parent[os.Getpid()], tb.comm[os.Getpid()])
	}
}

// The hint says to install the extension when a process called pi runs, the extension is not
// installed and no pi session is known; asked every 30 s, not every poll.
func TestPiHint(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	comm, installed, reads := map[int]string{1: "zsh", 2: "/usr/bin/vim"}, false, 0
	var raw []Raw
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return raw, nil })}, StateDir: t.TempDir(),
		Now:         func() time.Time { return now },
		Procs:       func() (procs, error) { reads++; return procs{comm: comm}, nil },
		PiInstalled: func() bool { return installed }}
	hint := func() string { p.Refresh(); return p.Hint() }
	if got := hint(); got != "" {
		t.Errorf("no pi: %q", got)
	}
	comm[3] = "pi"
	now = now.Add(10 * time.Second)
	if got := hint(); got != "" || reads != 1 {
		t.Errorf("asked again within 30 s: %q, %d reads", got, reads)
	}
	now = now.Add(30 * time.Second)
	if got := hint(); got != PiHint || reads != 2 {
		t.Errorf("a pi runs, no extension: %q", got)
	}
	comm[3] = "/opt/homebrew/bin/pi"
	now = now.Add(time.Minute)
	if got := hint(); got != PiHint {
		t.Errorf("the path of a pi: %q", got)
	}
	installed = true
	if got := hint(); got != "" {
		t.Errorf("installed: %q", got)
	}
	installed = false
	now = now.Add(time.Minute)
	raw = []Raw{{ID: "p", AgentType: "pi"}}
	if got := hint(); got != "" {
		t.Errorf("a pi session is known: %q", got)
	}
	raw = nil
	delete(comm, 3)
	now = now.Add(time.Minute)
	if got := hint(); got != "" {
		t.Errorf("the pi is gone: %q", got)
	}
	comm[4] = "pip" // not pi
	now = now.Add(time.Minute)
	if got := hint(); got != "" {
		t.Errorf("pip is not pi: %q", got)
	}
	// no Procs, no hint
	q := &Poller{Sources: p.Sources, StateDir: t.TempDir(), PiInstalled: func() bool { return false }}
	q.Refresh()
	if q.Hint() != "" {
		t.Error("hint without a process table")
	}
}

// The branch of a session whose transcript does not say is the one git says is checked out, asked
// again after 10 s; the transcript's, when there is one, is not argued with.
func TestGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "first")
	run("commit", "-q", "--allow-empty", "-m", "x")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	p := &Poller{Now: func() time.Time { return now }, StateDir: t.TempDir(),
		Sources: []Source{SourceFunc(func() ([]Raw, error) {
			return []Raw{{ID: "a", CWD: dir}, {ID: "b", CWD: dir, Branch: "given"}, {ID: "c", CWD: filepath.Join(dir, "gone")}, {ID: "d"}}, nil
		})}}
	branches := func() string {
		p.Refresh()
		return p.Sessions()[0].Branch + "|" + p.Sessions()[1].Branch + "|" + p.Sessions()[2].Branch + "|" + p.Sessions()[3].Branch
	}
	if got := branches(); got != "first|given||" {
		t.Errorf("%q", got)
	}
	run("checkout", "-q", "-b", "second")
	now = now.Add(5 * time.Second)
	if got := branches(); got != "first|given||" {
		t.Errorf("believed for 10 s: %q", got)
	}
	now = now.Add(6 * time.Second)
	if got := branches(); got != "second|given||" {
		t.Errorf("asked again: %q", got)
	}
	run("checkout", "-q", "--detach")
	now = now.Add(11 * time.Second)
	if got := branches(); got != "|given||" {
		t.Errorf("detached HEAD: %q", got)
	}
}
