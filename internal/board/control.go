package board

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/kackerx/agmux/internal/execx"
	"github.com/kackerx/agmux/internal/store"
)

// What r, x and d do to a session. A foreground agent is a process: it is sent
// SIGTERM, and when it has gone the pane's shell is told to start it again. A background Claude
// session is the daemon's: claude stop and claude respawn.

// How long a process is given to exit after SIGTERM before it is killed; tests make them short.
var (
	restartWait = 5 * time.Second
	killWait    = 2 * time.Second
	waitPoll    = 50 * time.Millisecond
)

// control is what the actions need of the machine, as functions so that tests can record them.
type control struct {
	signal   func(pid int, sig syscall.Signal) error
	alive    func(pid int) bool
	output   func(name string, args ...string) (string, error) // runs a command, its output with the error
	tmux     func(args ...string) (string, error)
	lookPath func(file string) (string, error)
	toplevel func(dir string) (string, error) // the root of the git repo dir is in
}

func realControl(tmux func(args ...string) (string, error)) control {
	return control{
		signal: func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
		alive: func(pid int) bool {
			err := syscall.Kill(pid, 0)
			return err == nil || errors.Is(err, syscall.EPERM)
		},
		output: func(name string, args ...string) (string, error) {
			out, err := execx.Run(name, args...)
			return string(out), err
		},
		tmux:     tmux,
		lookPath: exec.LookPath,
		toplevel: func(dir string) (string, error) {
			out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
			return strings.TrimSpace(string(out)), err
		},
	}
}

// end stops the process of a foreground session: SIGTERM, then SIGKILL if it is still there after wait.
func (c control) end(s store.Session, wait time.Duration) error {
	if s.PID <= 1 || s.PID == os.Getpid() {
		return fmt.Errorf("%s: no process to signal (the pid is %d)", s.Name, s.PID)
	}
	if err := c.signal(s.PID, syscall.SIGTERM); err != nil {
		if !c.alive(s.PID) {
			return nil // gone already
		}
		return fmt.Errorf("signal %s (pid %d): %w", s.Name, s.PID, err)
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(waitPoll) {
		if !c.alive(s.PID) {
			return nil
		}
	}
	if c.alive(s.PID) {
		if err := c.signal(s.PID, syscall.SIGKILL); err != nil && c.alive(s.PID) {
			return fmt.Errorf("kill %s (pid %d): %w", s.Name, s.PID, err)
		}
		for i := 0; i < 20 && c.alive(s.PID); i++ { // the kernel needs a moment
			time.Sleep(waitPoll)
		}
		if c.alive(s.PID) {
			return fmt.Errorf("%s (pid %d) is still there after SIGKILL", s.Name, s.PID)
		}
	}
	return nil
}

// kill is x: a background Claude session is stopped by the daemon, anything else is signalled.
func (c control) kill(s store.Session) error {
	if s.Background {
		out, err := c.output("claude", "stop", s.ID)
		if err != nil && strings.Contains(out+err.Error(), "No job matching") { // gone already
			return nil
		}
		return err
	}
	return c.end(s, killWait)
}

// restart is r.
func (c control) restart(s store.Session) error {
	if s.Background {
		_, err := c.output("claude", "respawn", s.ID)
		return err
	}
	var again string // what the pane's shell is told to run once the process has gone
	switch {
	case s.TmuxPane == "":
		return fmt.Errorf("%s has no tmux pane to start it again in (x ends it)", s.Name)
	case s.AgentType == "pi" && s.Transcript != "":
		again = "pi --session " + shellQuote(s.Transcript)
	case s.AgentType == "pi":
		return fmt.Errorf("%s: pi has not said which session file it has", s.Name)
	case s.AgentType == "claude" && s.SessionID != "":
		again = "claude --resume " + shellQuote(s.SessionID)
	case s.AgentType == "claude":
		return fmt.Errorf("%s: no session id to resume it by", s.Name)
	default:
		return fmt.Errorf("%s: r can't start a %s session again", s.Name, s.AgentType)
	}
	if s.PID > 1 && !c.alive(s.PID) { // r pressed twice: the second would type the command into the session the first started
		return fmt.Errorf("%s is not running (restarted already?)", s.Name)
	}
	if err := c.end(s, restartWait); err != nil {
		return err
	}
	if _, err := c.tmux("send-keys", "-t", s.TmuxPane, "-l", "--", again); err != nil {
		return err
	}
	_, err := c.tmux("send-keys", "-t", s.TmuxPane, "Enter")
	return err
}

// review is d: the diff of the repo the session works in, in hunk (live) or, without it, git's own
// pager. The command is the caller's to run with the terminal.
func (c control) review(s store.Session) (*exec.Cmd, error) {
	if s.CWD == "" {
		return nil, fmt.Errorf("%s: no directory to review", s.Name)
	}
	root, err := c.toplevel(s.CWD)
	if err != nil || root == "" {
		return nil, fmt.Errorf("%s: %s is not in a git repository (%v)", s.Name, s.CWD, err)
	}
	if hunk, err := c.lookPath("hunk"); err == nil {
		cmd := exec.Command(hunk, "diff", "--watch")
		cmd.Dir = root
		return cmd, nil
	}
	// git's own pager would quit at once on a short diff when the user's LESS says so (-F) and keep no colors: less -R, with no LESS
	cmd := exec.Command("sh", "-c", `git --no-pager -C "$1" diff --color=always | less -R`, "sh", root)
	cmd.Env = append(os.Environ(), "LESS=")
	return cmd, nil
}

// shellQuote is s as one word for a POSIX shell.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:@%+=,") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
