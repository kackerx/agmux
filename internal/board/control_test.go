package board

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/kackerx/agmux/internal/store"
)

// machine is a control over a pretend process table and a log of what was asked, in order.
type machine struct {
	mu        sync.Mutex
	log       []string
	alive     map[int]bool
	onTerm    func(pid int) // what a process does when told to end; nil = it ends at once
	ignore    bool          // SIGTERM does nothing
	immortal  bool          // SIGKILL does nothing either
	exitAfter time.Duration // SIGTERM ends it after this long
	killErr   error         // SIGKILL fails
	sigErr    error
	outputs   map[string]string // "claude stop x" → its output
	outputErr map[string]error
	hunk      bool
	root      string
	rootErr   error
}

func (m *machine) control() control {
	return control{
		signal: func(pid int, sig syscall.Signal) error {
			m.log = append(m.log, "signal "+sig.String()+" "+itoa(pid))
			if m.sigErr != nil {
				return m.sigErr
			}
			switch {
			case sig == syscall.SIGKILL && m.killErr != nil:
				return m.killErr
			case sig == syscall.SIGKILL && m.immortal:
			case sig == syscall.SIGKILL:
				m.alive[pid] = false
			case m.exitAfter > 0:
				time.AfterFunc(m.exitAfter, func() { m.mu.Lock(); m.alive[pid] = false; m.mu.Unlock() })
			case m.onTerm != nil:
				m.onTerm(pid)
			case !m.ignore:
				m.alive[pid] = false
			}
			return nil
		},
		alive: func(pid int) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.alive[pid] },
		output: func(name string, args ...string) (string, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			m.log = append(m.log, line)
			return m.outputs[line], m.outputErr[line]
		},
		tmux: func(args ...string) (string, error) {
			m.log = append(m.log, "tmux "+strings.Join(args, " "))
			return "", nil
		},
		lookPath: func(f string) (string, error) {
			if f == "hunk" && m.hunk {
				return "/usr/local/bin/hunk", nil
			}
			return "", exec.ErrNotFound
		},
		toplevel: func(dir string) (string, error) { return m.root, m.rootErr },
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func shortWaits(t *testing.T) {
	t.Helper()
	r, k, p := restartWait, killWait, waitPoll
	restartWait, killWait, waitPoll = 60*time.Millisecond, 30*time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { restartWait, killWait, waitPoll = r, k, p })
}

var (
	claudeFg = store.Session{ID: "sid-1", Name: "dev", AgentType: "claude", PID: 4242, SessionID: "sid-1", TmuxPane: "%5", CWD: "/w/p"}
	claudeBg = store.Session{ID: "job1", Name: "bgjob", AgentType: "claude", Background: true, PID: 4343, SessionID: "sid-bg"}
	piFg     = store.Session{ID: "pi-1", Name: "pee", AgentType: "pi", PID: 4444, SessionID: "pi-1", TmuxPane: "%6", Transcript: "/home/u/.pi/agent/sessions/--w p--/a b.jsonl"}
)

func newMachine(pids ...int) *machine {
	m := &machine{alive: map[int]bool{}, outputs: map[string]string{}, outputErr: map[string]error{}}
	for _, p := range pids {
		m.alive[p] = true
	}
	return m
}

// x: a foreground session gets SIGTERM and, if it is still there after 2 s, SIGKILL; a background one is stopped by the daemon.
func TestControlKill(t *testing.T) {
	shortWaits(t)
	m := newMachine(4242, 4444)
	if err := m.control().kill(claudeFg); err != nil || !reflect.DeepEqual(m.log, []string{"signal terminated 4242"}) || m.alive[4242] {
		t.Errorf("a process that ends: %v %v", err, m.log)
	}
	m = newMachine(4444)
	m.ignore = true
	if err := m.control().kill(piFg); err != nil || !reflect.DeepEqual(m.log, []string{"signal terminated 4444", "signal killed 4444"}) || m.alive[4444] {
		t.Errorf("one that does not: %v %v", err, m.log)
	}
	// kill is given 2 s, restart 5: one that takes between the two is killed by x and left alone by r
	m = newMachine(4242)
	m.exitAfter = 45 * time.Millisecond // killWait 30 ms, restartWait 60 ms in these tests
	if err := m.control().kill(claudeFg); err != nil || len(m.log) != 2 || m.log[1] != "signal killed 4242" {
		t.Errorf("x on a slow one: %v %v", err, m.log)
	}
	m = newMachine(4242)
	m.exitAfter = 45 * time.Millisecond
	if err := m.control().restart(claudeFg); err != nil || m.log[0] != "signal terminated 4242" || m.log[1] != "tmux send-keys -t %5 -l -- claude --resume sid-1" {
		t.Errorf("r on a slow one: %v %v", err, m.log)
	}
	// a process that can't be killed is an error
	m = newMachine(4242)
	m.ignore, m.killErr = true, syscall.EPERM
	if err := m.control().kill(claudeFg); err == nil || !strings.Contains(err.Error(), "kill") {
		t.Errorf("not allowed to kill: %v", err)
	}
	m = newMachine()
	if err := m.control().kill(claudeBg); err != nil || !reflect.DeepEqual(m.log, []string{"claude stop job1"}) {
		t.Errorf("background: %v %v", err, m.log)
	}
	m.outputs["claude stop job1"] = "stop: No job matching job1"
	m.outputErr["claude stop job1"] = errors.New("exit status 1")
	if err := m.control().kill(claudeBg); err != nil {
		t.Errorf("a job that is gone is stopped: %v", err)
	}
	m.outputs["claude stop job1"] = "daemon not running"
	if err := m.control().kill(claudeBg); err == nil {
		t.Error("another failure is one")
	}
}

func TestControlEnd(t *testing.T) {
	shortWaits(t)
	for _, c := range []store.Session{{Name: "x"}, {Name: "x", PID: 1}, {Name: "x", PID: os.Getpid()}} {
		m := newMachine(1, os.Getpid())
		if err := m.control().end(c, killWait); err == nil || len(m.log) != 0 {
			t.Errorf("pid %d: %v %v", c.PID, err, m.log)
		}
	}
	m := newMachine() // not there: the signal fails, nothing to do
	m.sigErr = syscall.ESRCH
	if err := m.control().end(claudeFg, killWait); err != nil {
		t.Errorf("gone already: %v", err)
	}
	m = newMachine(4242)
	m.sigErr = syscall.EPERM
	if err := m.control().end(claudeFg, killWait); err == nil || !strings.Contains(err.Error(), "4242") {
		t.Errorf("not allowed: %v", err)
	}
	// it ends a little after SIGTERM, inside the wait: no SIGKILL
	m = newMachine(4242)
	polls := 0
	c := m.control()
	alive := c.alive
	c.alive = func(pid int) bool {
		if polls++; polls > 3 {
			m.alive[pid] = false
		}
		return alive(pid)
	}
	m.onTerm = func(int) {}
	if err := c.end(claudeFg, time.Second); err != nil || !reflect.DeepEqual(m.log, []string{"signal terminated 4242"}) {
		t.Errorf("a slow exit: %v %v", err, m.log)
	}
}

// r: the process ends first, then the pane's shell is told to run the agent again; every kind of session its own way.
func TestControlRestart(t *testing.T) {
	shortWaits(t)
	m := newMachine(4242)
	if err := m.control().restart(claudeFg); err != nil {
		t.Fatal(err)
	}
	want := []string{"signal terminated 4242", "tmux send-keys -t %5 -l -- claude --resume sid-1", "tmux send-keys -t %5 Enter"}
	if !reflect.DeepEqual(m.log, want) {
		t.Errorf("claude: %q", m.log)
	}
	m = newMachine(4444)
	if err := m.control().restart(piFg); err != nil {
		t.Fatal(err)
	}
	want = []string{"signal terminated 4444", "tmux send-keys -t %6 -l -- pi --session '/home/u/.pi/agent/sessions/--w p--/a b.jsonl'", "tmux send-keys -t %6 Enter"}
	if !reflect.DeepEqual(m.log, want) {
		t.Errorf("pi: %q", m.log)
	}
	m = newMachine()
	if err := m.control().restart(claudeBg); err != nil || !reflect.DeepEqual(m.log, []string{"claude respawn job1"}) {
		t.Errorf("background: %v %q", err, m.log)
	}
	// one that will not go is killed, and only then is the shell told
	m = newMachine(4242)
	m.ignore = true
	if err := m.control().restart(claudeFg); err != nil || len(m.log) != 4 || m.log[1] != "signal killed 4242" || !strings.HasPrefix(m.log[2], "tmux send-keys") {
		t.Errorf("a stubborn one: %v %q", err, m.log)
	}
	// r twice: the process is not there, and the shell is not told to start it (it would type the command into the session the first r started)
	m = newMachine()
	if err := m.control().restart(claudeFg); err == nil || !strings.Contains(err.Error(), "not running") || len(m.log) != 0 {
		t.Errorf("r on a process that is gone: %v %q", err, m.log)
	}
	m = newMachine(4242)
	m.sigErr = syscall.ESRCH // it went between the check and the signal
	m.alive[4242] = true
	m.onTerm = nil
	if err := m.control().restart(claudeFg); err == nil || slices.ContainsFunc(m.log, func(l string) bool { return strings.HasPrefix(l, "tmux") }) {
		t.Errorf("a signal that fails: %v %q", err, m.log)
	}
	// one that survives SIGKILL: nothing is typed, and it is an error
	m = newMachine(4242)
	m.ignore, m.immortal = true, true
	if err := m.control().restart(claudeFg); err == nil || !strings.Contains(err.Error(), "still there after SIGKILL") || slices.ContainsFunc(m.log, func(l string) bool { return strings.HasPrefix(l, "tmux") }) {
		t.Errorf("r on an immortal one: %v %q", err, m.log)
	}
	m = newMachine(4242)
	m.ignore, m.immortal = true, true
	if err := m.control().kill(claudeFg); err == nil || !strings.Contains(err.Error(), "still there after SIGKILL") {
		t.Errorf("x on an immortal one: %v", err)
	}
	// what is missing is said, and nothing is signalled
	for name, s := range map[string]store.Session{
		"no pane":       {Name: "n", AgentType: "claude", PID: 4242, SessionID: "x"},
		"no session id": {Name: "n", AgentType: "claude", PID: 4242, TmuxPane: "%1"},
		"pi, no file":   {Name: "n", AgentType: "pi", PID: 4242, TmuxPane: "%1"},
		"codex":         {Name: "n", AgentType: "codex", PID: 4242, TmuxPane: "%1", SessionID: "x"},
		"no type":       {Name: "n", PID: 4242, TmuxPane: "%1", SessionID: "x"},
	} {
		m = newMachine(4242)
		if err := m.control().restart(s); err == nil || len(m.log) != 0 {
			t.Errorf("%s: %v %q", name, err, m.log)
		}
	}
	// the process can't be ended: the shell is not told
	m = newMachine(4242)
	m.sigErr = syscall.EPERM
	if err := m.control().restart(claudeFg); err == nil || len(m.log) != 1 {
		t.Errorf("not allowed: %v %q", err, m.log)
	}
	m = newMachine()
	m.outputErr["claude respawn job1"] = errors.New("boom")
	if err := m.control().restart(claudeBg); err == nil {
		t.Error("respawn failing")
	}
}

// d: hunk in the repo's root; without hunk git's diff in the pager the user has, else less -R; outside git, said.
func TestControlReview(t *testing.T) {
	m := newMachine()
	m.root, m.hunk = "/w/repo", true
	cmd, err := m.control().review(claudeFg)
	if err != nil || cmd.Path != "/usr/local/bin/hunk" || !reflect.DeepEqual(cmd.Args[1:], []string{"diff", "--watch"}) || cmd.Dir != "/w/repo" {
		t.Errorf("hunk: %v %+v", err, cmd)
	}
	m.hunk = false
	t.Setenv("LESS", "--quit-if-one-screen")
	cmd, err = m.control().review(claudeFg)
	if err != nil || !reflect.DeepEqual(cmd.Args, []string{"sh", "-c", `git --no-pager -C "$1" diff --color=always | less -R`, "sh", "/w/repo"}) || !slices.Contains(cmd.Env, "LESS=") || lastEnv(cmd.Env, "LESS") != "" {
		t.Errorf("git diff piped to less: %v %v %v", err, cmd.Args, lastEnv(cmd.Env, "LESS"))
	}
	m.rootErr = errors.New("not a git repository")
	if _, err := m.control().review(claudeFg); err == nil || !strings.Contains(err.Error(), "/w/p is not in a git repository (not a git repository)") || !strings.HasPrefix(err.Error(), "dev: ") {
		t.Errorf("not a repo: %v", err)
	}
	if _, err := m.control().review(store.Session{Name: "n"}); err == nil {
		t.Error("no directory")
	}
}

// lastEnv is the value of the last entry of key: the one a child sees.
func lastEnv(env []string, key string) string {
	v := ""
	for _, e := range env {
		if rest, ok := strings.CutPrefix(e, key+"="); ok {
			v = rest
		}
	}
	return v
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"sid-1": "sid-1", "/a/b/c.jsonl": "/a/b/c.jsonl", "a b": "'a b'", "it's": `'it'\''s'`, "": "''", "$(x)": "'$(x)'", "a;b": "'a;b'",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The fallback really runs: git's diff of the repo's root goes into less -R, whatever LESS says.
func TestReviewFallbackRuns(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	repo, _ = filepath.EvalSymlinks(repo)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "x")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo-added\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "sub"), 0o755)
	bin, got := t.TempDir(), filepath.Join(t.TempDir(), "less.out")
	os.WriteFile(filepath.Join(bin, "less"), []byte("#!/bin/sh\necho \"args: $* LESS=[$LESS]\" > "+got+"\ncat >> "+got+"\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LESS", "--quit-if-one-screen")
	c := realControl(nil)
	c.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	cmd, err := c.review(store.Session{Name: "n", CWD: filepath.Join(repo, "sub")}) // from a directory below the root
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	data, _ := os.ReadFile(got)
	if !strings.Contains(string(data), "args: -R LESS=[]") || !strings.Contains(string(data), "two-added") {
		t.Errorf("what less was given:\n%s", data)
	}
}
