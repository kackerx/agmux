package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kackerx/agmux/internal/i18n"
)

// T90: `agmux shell zsh|bash` prints the function; anything else is a usage error that says what there is.
func TestShellCmd(t *testing.T) {
	var zsh, bash, errs bytes.Buffer
	if run([]string{"shell", "zsh"}, &zsh, &errs) != 0 || run([]string{"shell", "bash"}, &bash, &errs) != 0 || zsh.String() != shellFunc || bash.String() != shellFunc {
		t.Fatalf("zsh %q bash %q err %q", zsh.String(), bash.String(), errs.String())
	}
	for _, args := range [][]string{{"shell"}, {"shell", "fish"}, {"shell", "zsh", "bash"}, {"shell", ""}} {
		var out, e bytes.Buffer
		if code := run(args, &out, &e); code != 2 || out.Len() != 0 || !strings.Contains(e.String(), "agmux shell zsh|bash") {
			t.Errorf("%v: code %d out %q err %q", args, code, out.String(), e.String())
		}
	}
}

// The function itself, in the real shells: it cds to the directory agmux wrote into $AGMUX_CWD_FILE,
// passes the exit code and the arguments through, leaves no file behind, and does nothing when agmux
// wrote nothing.
func TestShellFunctionInRealShells(t *testing.T) {
	for _, sh := range []string{"zsh", "bash"} {
		path, err := exec.LookPath(sh)
		if err != nil {
			t.Logf("no %s here", sh)
			continue
		}
		t.Run(sh, func(t *testing.T) {
			tmp, target, start := t.TempDir(), t.TempDir(), t.TempDir()
			fake := filepath.Join(tmp, "bin")
			os.Mkdir(fake, 0o755)
			// the fake agmux writes $WRITE (if set) into the file it was given, echoes its arguments and exits 3
			os.WriteFile(filepath.Join(fake, "agmux"), []byte("#!/bin/sh\n[ -n \"$WRITE\" ] && printf %s \"$WRITE\" > \"$AGMUX_CWD_FILE\"\necho \"args: $*\"\nprintf %s \"$AGMUX_CWD_FILE\" > \"$REC\"\nexit 3\n"), 0o755)
			rec := filepath.Join(tmp, "cwdfile") // where the fake agmux says which file it was given
			invoke := func(write string) string {
				script := shellFunc + `agmux -r "two words"; echo "rc=$?"; pwd -P`
				cmd := exec.Command(path, "-f", "-c", script)
				if sh == "bash" {
					cmd = exec.Command(path, "--norc", "-c", script)
				}
				cmd.Dir = start
				cmd.Env = []string{"PATH=" + fake + ":/usr/bin:/bin", "REC=" + rec, "WRITE=" + write, "HOME=" + tmp}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %s", err, out)
				}
				return string(out)
			}
			real := func(d string) string { r, _ := filepath.EvalSymlinks(d); return r }
			if got := invoke(target); got != "args: -r two words\nrc=3\n"+real(target)+"\n" {
				t.Errorf("written: %q", got)
			}
			if got := invoke(""); got != "args: -r two words\nrc=3\n"+real(start)+"\n" {
				t.Errorf("nothing written: %q", got)
			}
			if f, _ := os.ReadFile(rec); len(f) == 0 {
				t.Error("agmux was not given a file")
			} else if _, err := os.Stat(string(f)); err == nil {
				t.Errorf("the temporary file %s is still there", f)
			}
		})
	}
}

// T90: a session resumed in place leaves its directory in $AGMUX_CWD_FILE for the shell function; without
// the variable nothing is written, and the agent is not given it.
func TestExecResumeWritesTheDirectory(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	defer func(f func(string, []string, []string) error) { syscallExec = f }(syscallExec)
	var gotEnv []string
	syscallExec = func(b string, argv, env []string) error { gotEnv = env; return nil }
	file := filepath.Join(t.TempDir(), "cwd")

	t.Setenv("AGMUX_CWD_FILE", file)
	if err := execResume(dir, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != dir {
		t.Errorf("file %q %v, want %q", got, err, dir)
	}
	for _, kv := range gotEnv {
		if strings.HasPrefix(kv, "AGMUX_CWD_FILE=") {
			t.Errorf("the agent got %s", kv)
		}
	}

	// nothing is written when the exec cannot happen: the shell would cd for a session that did not start
	os.Remove(file)
	execResume(dir, []string{"no-such-agent-xyz"})
	execResume(filepath.Join(dir, "gone"), []string{"claude"})
	if _, err := os.Stat(file); err == nil {
		t.Error("written for a resume that did not happen")
	}

	// no variable, no file; a file that can not be written does not stop the resume
	said := captureStderr(t)
	t.Setenv("AGMUX_CWD_FILE", "")
	if err := execResume(dir, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("written without the variable")
	}
	if s := said(); s != "" {
		t.Errorf("said something without the variable: %q", s)
	}
	said = captureStderr(t)
	bad := filepath.Join(dir, "no", "such", "dir", "f")
	t.Setenv("AGMUX_CWD_FILE", bad)
	called := false
	syscallExec = func(string, []string, []string) error { called = true; return nil }
	if err := execResume(dir, []string{"claude"}); err != nil || !called {
		t.Errorf("an unwritable file stops the resume: %v called %v", err, called)
	}
	if s := said(); !strings.Contains(s, bad) || !strings.Contains(s, "stay where it is") {
		t.Errorf("the warning names the file and what it means: %q", s)
	}
}

// captureStderr sends os.Stderr into a pipe until the returned function is called, which gives what was written.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })
	return func() string {
		os.Stderr = old
		w.Close()
		b, _ := io.ReadAll(r)
		return string(b)
	}
}

// `agmux setup` says how to get the shell function, in the language it speaks, and does not touch rc files.
func TestSetupMentionsTheShellFunction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Cleanup(func() { i18n.Set("en") })
	for lang, want := range map[string]string{"en": `eval "$(agmux shell zsh)"`, "zh": "加进你的 shell rc"} {
		t.Setenv("AGMUX_LANG", lang)
		var out, errs bytes.Buffer
		if code := run([]string{"setup"}, &out, &errs); code != 0 || !strings.Contains(out.String(), want) || !strings.Contains(out.String(), `agmux shell zsh`) {
			t.Errorf("%s: %d %q", lang, code, out.String())
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{"setup", "--bogus"}, &out, &errs); code != 2 || strings.Contains(out.String(), "agmux shell") {
		t.Errorf("a usage error says nothing more: %d %q", code, out.String())
	}
	if entries, _ := os.ReadDir(home); len(entries) > 0 {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".zsh") || strings.HasPrefix(e.Name(), ".bash") {
				t.Errorf("setup wrote %s", e.Name())
			}
		}
	}
}
