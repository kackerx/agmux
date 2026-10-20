package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// claudeDir is a config dir with the given registry entries (file name → JSON) and jobs
// (job id → state.json), and a Claude source over it that says every pid but 999 is alive.
func claudeDir(t *testing.T, entries map[string]string, jobs map[string]string) *Claude {
	t.Helper()
	dir := t.TempDir()
	for name, body := range entries {
		if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sessions", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for id, body := range jobs {
		if err := os.MkdirAll(filepath.Join(dir, "jobs", id), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "jobs", id, "state.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Claude{Dir: dir, HUDDir: t.TempDir(), Alive: func(pid int) bool { return pid != 999 }}
}

func reg(pid int, rest string) string {
	return fmt.Sprintf(`{"pid":%d,"sessionId":"sid-%d","cwd":"/w/p","kind":"interactive","name":"n%d","status":"idle","statusUpdatedAt":1792483390952,"updatedAt":1792483390999%s}`, pid, pid, pid, rest)
}

func rawsByID(t *testing.T, c *Claude) map[string]Raw {
	t.Helper()
	rs, err := c.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Raw{}
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

// What is listed: a live process of a session that is not a spare; a dead pid, a pid some other
// process has taken over (procStart is not what ps says), a spare, a file that does not parse and
// the key files next to the entries are not; one bad file costs only itself.
func TestClaudeDiscovery(t *testing.T) {
	c := claudeDir(t, map[string]string{
		"1.json":       reg(1, ""),
		"2.json":       reg(2, `,"procStart":"Tue Oct  6 07:34:09 2026"`), // ps pads the day, the register may not
		"3.json":       reg(3, `,"procStart":"Mon Jan 1 00:00:00 2024"`),  // pid reused
		"4.json":       reg(4, `,"spare":true`),
		"999.json":     reg(999, ""), // dead
		"5.json":       `{"pid":5,"sessionId":`,
		"6.json":       `{"peerToken":"x","procStart":"y"}`, // no pid: not an entry
		"1.0a1b2c.key": `secret`,
		"7.json":       reg(7, `,"procStart":""`),
		"8.json":       reg(8, `,"procStart":"Tue Oct 20 07:34:09 2026"`), // alive a moment ago, not in ps now
		"9.json":       reg(9, ""),                                        // likewise, with no procStart to compare
	}, nil)
	c.StartsOf = func(pids []int) (map[int]string, error) {
		return map[int]string{1: "Tue Oct 20 07:34:09 2026", 2: "Tue Oct  6 07:34:09 2026", 3: "Tue Oct 20 07:34:09 2026", 4: "x", 7: "x"}, nil
	}
	got := rawsByID(t, c)
	var ids []string
	for id := range got {
		ids = append(ids, id)
	}
	if len(got) != 3 || got["sid-1"].PID != 1 || got["sid-2"].PID != 2 || got["sid-7"].PID != 7 {
		t.Errorf("listed %v, want sid-1 sid-2 sid-7", ids)
	}
	// ps failing is no reason to list nothing
	c.StartsOf = func([]int) (map[int]string, error) { return nil, fmt.Errorf("no ps") }
	if got := rawsByID(t, c); len(got) != 6 || got["sid-3"].PID != 3 { // the reused pid is let through: it can't be told; the spare, the dead and the broken are still out
		t.Errorf("without ps: %d listed", len(got))
	}
}

func TestClaudeStatusMapping(t *testing.T) {
	for _, c := range []struct {
		status, waitingFor, want, attention string
	}{
		{"busy", "", "working", ""},
		{"waiting", "permission prompt", "waiting", "permission"},
		{"waiting", "input needed", "waiting", "question"},
		{"waiting", "", "waiting", "question"},
		{"idle", "", "idle", ""},
		{"shell", "", "idle", ""},
		{"somethingnew", "", "idle", ""},
		{"busy", "permission prompt", "working", ""}, // waitingFor only counts while waiting
	} {
		src := claudeDir(t, map[string]string{"1.json": reg(1, "")[:len(reg(1, ""))-1] + fmt.Sprintf(`,"status":%q,"waitingFor":%q}`, c.status, c.waitingFor)}, nil)
		r := rawsByID(t, src)["sid-1"]
		if r.Status != c.want || r.Attention != c.attention {
			t.Errorf("%s / %q: %q %q, want %q %q", c.status, c.waitingFor, r.Status, r.Attention, c.want, c.attention)
		}
		if !r.StatusChangedAt.Equal(time.UnixMilli(1792483390952)) || r.Name != "n1" || r.CWD != "/w/p" || r.AgentType != "claude" || r.Background {
			t.Errorf("%s: %+v", c.status, r)
		}
	}
}

func TestTmuxField(t *testing.T) {
	for in, want := range map[string][2]string{
		"agmux # main:@14.%37":              {"%37", "agmux # main"}, // a session may be named with " # ", spaces, ":" and "/"
		"8:@14.%38":                         {"%38", "8"},
		"my:proj # build/api:@3.%10":        {"%10", "my:proj # build/api"},
		"my session:@3.%10":                 {"%10", "my session"},
		"doraemon-dsl # feature/x-y:@7.%10": {"%10", "doraemon-dsl # feature/x-y"},
		"":                                  {"", ""},
		"no pane here":                      {"", ""},
	} {
		if pane, sess := tmuxField(in); [2]string{pane, sess} != want {
			t.Errorf("%q: %q %q, want %q", in, pane, sess, want)
		}
	}
}

// A background entry is the job: its id, the daemon's state for the status, its name and transcript; no job file is the register's.
func TestClaudeBackground(t *testing.T) {
	bg := func(job string) string {
		return `{"pid":%d,"sessionId":"sid-` + job + `","cwd":"/w/p","kind":"bg","jobId":"` + job + `","name":"reg-name","status":"idle","statusUpdatedAt":1,"updatedAt":2}`
	}
	entries := map[string]string{}
	jobs := map[string]string{}
	for i, c := range []struct{ job, state string }{
		{"blocked", `{"state":"running","tempo":"blocked","name":"job-name","linkScanPath":"/x/t.jsonl"}`},
		{"asks", `{"tempo":"blocked","questions":[{"q":1}],"needs":["tool"]}`},
		{"needs", `{"tempo":"blocked","needs":["Bash"]}`},
		{"done", `{"state":"done","tempo":"idle"}`},
		{"stopped", `{"state":"stopped","tempo":"active"}`},
		{"failed", `{"state":"failed"}`},
		{"running", `{"state":"running","tempo":"active"}`},
		{"nojob", ``},
	} {
		entries[fmt.Sprintf("%d.json", i+1)] = fmt.Sprintf(bg(c.job), i+1)
		if c.state != "" {
			jobs[c.job] = c.state
		}
	}
	got := rawsByID(t, claudeDir(t, entries, jobs))
	for job, w := range map[string][2]string{
		"blocked": {"waiting", "question"}, "asks": {"waiting", "question"}, "needs": {"waiting", "permission"},
		"done": {"idle", ""}, "stopped": {"idle", ""}, "failed": {"idle", ""}, "running": {"working", ""},
		"nojob": {"idle", ""}, // the register's own status
	} {
		r, ok := got[job]
		if !ok || !r.Background || r.SessionID != "sid-"+job || r.Status != w[0] || r.Attention != w[1] {
			t.Errorf("%s: %+v, want %v", job, r, w)
		}
	}
	if r := got["blocked"]; r.Name != "job-name" || r.Transcript != "/x/t.jsonl" {
		t.Errorf("name and transcript from the job: %+v", r)
	}
	if r := got["done"]; r.Name != "reg-name" {
		t.Errorf("the register's name without the job's: %+v", r)
	}
}

// The transcript is the path the statusline saved if that file is there, else projects/<cwd as slug>/<id>.jsonl if that is, else none.
func TestClaudeTranscript(t *testing.T) {
	c := claudeDir(t, map[string]string{"1.json": reg(1, "")}, nil)
	c.Dir = t.TempDir()
	os.WriteFile(filepath.Join(c.Dir, "x"), nil, 0o644)
	cwd := "/Users/dev/my.proj_x"
	slugged := filepath.Join(c.Dir, "projects", "-Users-dev-my-proj-x", "sid.jsonl")
	if got := c.transcript("sid", cwd); got != "" {
		t.Errorf("nothing there: %q", got)
	}
	os.MkdirAll(filepath.Dir(slugged), 0o755)
	os.WriteFile(slugged, []byte("{}\n"), 0o644)
	if got := c.transcript("sid", cwd); got != slugged {
		t.Errorf("by the slug: %q", got)
	}
	saved := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	os.WriteFile(saved, []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(c.HUDDir, "sid.json"), []byte(`{"transcript_path":"`+saved+`"}`), 0o644)
	if got := c.transcript("sid", cwd); got != saved {
		t.Errorf("the statusline's: %q", got)
	}
	os.WriteFile(filepath.Join(c.HUDDir, "sid.json"), []byte(`{"transcript_path":"/gone/x.jsonl"}`), 0o644)
	if got := c.transcript("sid", cwd); got != slugged {
		t.Errorf("the statusline's is gone: %q", got)
	}
	if got := c.transcript("", cwd); got != "" {
		t.Errorf("no id: %q", got)
	}
}

func TestPsStarts(t *testing.T) {
	m, err := psStarts([]int{os.Getpid(), 99998})
	if err != nil || m[os.Getpid()] == "" || len(m) != 1 {
		t.Fatalf("%v %v", m, err)
	}
	if at, err := time.Parse("Mon Jan _2 15:04:05 2006", m[os.Getpid()]); err != nil || time.Since(at) < 0 || time.Since(at) > time.Hour {
		t.Errorf("lstart %q is not this process's start in English and UTC (%v)", m[os.Getpid()], err) // the register's procStart is
	}
	if !alive(os.Getpid()) || alive(99998) {
		t.Error("alive")
	}
}

// The name is the register's, else the title the session was given (the transcript's custom-title), else the project; the pane's session is tmux's.
func TestNameFallback(t *testing.T) {
	log := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(log, []byte(`{"type":"custom-title","customTitle":"titled"}`+"\n"), 0o644)
	p := poller(t, []Raw{
		{ID: "a", Name: "named", Transcript: log, Project: "proj"},
		{ID: "b", Transcript: log, Project: "proj"},
		{ID: "c", Project: "proj"},
		{ID: "d", Name: "made-up-7", NameDerived: true, Transcript: log, Project: "proj"}, // the title beats a name the agent made up
		{ID: "e", Name: "made-up-7", NameDerived: true, Project: "proj"},                  // and a made-up one is still a name
	})
	want := map[string]string{"a": "named", "b": "titled", "c": "proj", "d": "titled", "e": "made-up-7"}
	for _, s := range p.Sessions() {
		if s.Name != want[s.ID] {
			t.Errorf("%s: %q, want %q", s.ID, s.Name, want[s.ID])
		}
	}
}

func line(typ, content string) []byte {
	return []byte(fmt.Sprintf(`{"type":%q,"message":{"content":%s}}`, typ, content))
}

// The pending tool is the last tool_use without a tool_result; none when all are answered; a
// user message with text content is no trouble.
func TestPendingIn(t *testing.T) {
	use := func(id, name string) []byte {
		return line("assistant", fmt.Sprintf(`[{"type":"text","text":"x"},{"type":"tool_use","id":%q,"name":%q,"input":{}}]`, id, name))
	}
	res := func(id string) []byte {
		return line("user", fmt.Sprintf(`[{"type":"tool_result","tool_use_id":%q,"content":"ok"}]`, id))
	}
	text := line("user", `"just words with tool_use and tool_result in them"`)
	for _, c := range []struct {
		name  string
		lines [][]byte
		want  string
	}{
		{"none", [][]byte{text}, ""},
		{"unpaired", [][]byte{text, use("1", "Bash")}, "Bash"},
		{"paired", [][]byte{use("1", "Bash"), res("1")}, ""},
		{"two unpaired: the last", [][]byte{use("1", "Read"), use("2", "Bash")}, "Bash"},
		{"the last unpaired", [][]byte{use("1", "Read"), res("1"), use("2", "Edit")}, "Edit"},
		{"parallel, one answered", [][]byte{line("assistant", `[{"type":"tool_use","id":"1","name":"Read"},{"type":"tool_use","id":"2","name":"Bash"}]`), res("1")}, "Bash"},
		{"parallel, the earlier left", [][]byte{line("assistant", `[{"type":"tool_use","id":"1","name":"Read"},{"type":"tool_use","id":"2","name":"Bash"}]`), res("2")}, "Read"},
		{"a result for what the tail does not show", [][]byte{res("0"), use("1", "Write")}, "Write"},
		{"broken line", [][]byte{[]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use"`), use("1", "Glob")}, "Glob"},
	} {
		if got := pendingIn(c.lines); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBranchIn(t *testing.T) {
	lines := [][]byte{[]byte(`{"gitBranch":"old"}`), []byte(`{"type":"user"}`), []byte(`{"gitBranch":"feat/x","message":"gitBranch"}`), []byte(`{"type":"x"}`)}
	if got := branchIn(lines); got != "feat/x" {
		t.Errorf("%q", got)
	}
	if got := branchIn(lines[1:2]); got != "" {
		t.Errorf("none: %q", got)
	}
}

// What the transcript's tail says reaches the session: the branch, the tool a working session runs
// (not an idle one: that one is stale), and a waiting session whose tool is ExitPlanMode asks for approval of a plan.
func TestConvertFromTail(t *testing.T) {
	log := filepath.Join(t.TempDir(), "s.jsonl")
	body := `{"type":"user","gitBranch":"feat/x","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","message":{"model":"m","content":[{"type":"tool_use","id":"1","name":"ExitPlanMode"}]}}` + "\n"
	os.WriteFile(log, []byte(body), 0o644)
	p := poller(t, []Raw{
		{ID: "w", Status: "waiting", Attention: "question", Transcript: log},
		{ID: "p", Status: "working", Transcript: log},
		{ID: "i", Status: "idle", Transcript: log},
		{ID: "given", Status: "working", Transcript: log, PendingTool: "Bash", Branch: "main"},
	})
	for id, w := range map[string][3]string{
		"w": {"feat/x", "ExitPlanMode", "plan_approval"}, "p": {"feat/x", "ExitPlanMode", ""},
		"i": {"feat/x", "", ""}, "given": {"main", "Bash", ""},
	} {
		s := byID(t, p.Sessions(), id)
		if s.Branch != w[0] || s.PendingTool != w[1] || s.Attention != w[2] {
			t.Errorf("%s: branch %q tool %q attention %q, want %q", id, s.Branch, s.PendingTool, s.Attention, w)
		}
	}
}

// The project is the main repo's name, from a worktree too; outside git it is the directory's name and the root is empty.
func TestRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	main := filepath.Join(dir, "shopcart")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", main, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.MkdirAll(main, 0o755)
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "x")
	wt := filepath.Join(dir, "wt", "feature")
	run("worktree", "add", "-q", "-b", "f", wt)
	sub := filepath.Join(main, "internal")
	os.MkdirAll(sub, 0o755)
	plain := filepath.Join(dir, "notes")
	os.MkdirAll(plain, 0o755)
	p := &Poller{}
	for cwd, want := range map[string][2]string{main: {"shopcart", main}, sub: {"shopcart", main}, wt: {"shopcart", main}, plain: {"notes", ""}, "": {"", ""}} {
		if project, root := p.repo(cwd); project != want[0] || root != want[1] {
			t.Errorf("%q: %q %q, want %q", cwd, project, root, want)
		}
	}
	if !reflect.DeepEqual(len(p.repos), 4) {
		t.Errorf("asked once per directory: %d kept", len(p.repos))
	}
}

func TestClaudeNameSource(t *testing.T) {
	c := claudeDir(t, map[string]string{
		"1.json": reg(1, `,"nameSource":"derived"`), "2.json": reg(2, `,"nameSource":"user"`), "3.json": reg(3, `,"nameSource":"peer"`), "4.json": reg(4, ""),
	}, nil)
	got := rawsByID(t, c)
	if !got["sid-1"].NameDerived || got["sid-2"].NameDerived || got["sid-3"].NameDerived || got["sid-4"].NameDerived {
		t.Errorf("derived: %v %v %v %v", got["sid-1"].NameDerived, got["sid-2"].NameDerived, got["sid-3"].NameDerived, got["sid-4"].NameDerived)
	}
}
