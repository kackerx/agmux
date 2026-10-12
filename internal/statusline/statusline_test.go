package statusline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(args, strings.NewReader(stdin), &o, &e)
	return code, o.String(), e.String()
}

func TestRun(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Join(state, "agmux")
	p := `{"session_id":"abc-1","context_window":{"used_percentage":12}}`

	if code, out, _ := run(t, p, "--", "cat; exit 3"); code != 3 || out != p {
		t.Errorf("passthrough: code %d out %q", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "abc-1.json")); string(got) != p {
		t.Errorf("saved %q", got)
	}
	if code, out, _ := run(t, p, "--", "echo", "a", "b"); code != 0 || out != "a b\n" {
		t.Errorf("args joined for sh -c: %d %q", code, out)
	}
	if code, out, _ := run(t, p); code != 0 || out != "" {
		t.Errorf("no command: %d %q", code, out)
	}
	if code, out, _ := run(t, p, "--"); code != 0 || out != "" {
		t.Errorf("bare --: %d %q", code, out)
	}

	for _, bad := range []string{"not json", `{"session_id":"../x"}`, `{"session_id":"a.b"}`, `{"session_id":5}`, `[1]`, `{"session_id":""}`} {
		if code, out, _ := run(t, bad, "--", "cat"); code != 0 || out != bad {
			t.Errorf("%s: still passed through: %d %q", bad, code, out)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("only abc-1.json may exist (no bad ids, no tmp files): %v", entries)
	}

	t.Setenv("PATH", t.TempDir()) // no sh
	if code, _, e := run(t, `{"session_id":"s2"}`, "--", "echo hi"); code != 127 || !strings.HasPrefix(e, "agmux statusline: ") || strings.Count(e, "\n") != 1 {
		t.Errorf("sh missing: code %d stderr %q", code, e)
	}
	if _, err := os.Stat(filepath.Join(dir, "s2.json")); err != nil {
		t.Error("the file is written before the command starts")
	}
}

func TestRunUnwritableStateDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	t.Setenv("XDG_STATE_HOME", blocker) // a file, so the dir can't be made
	if code, out, _ := run(t, `{"session_id":"s"}`, "--", "echo ok; exit 2"); code != 2 || out != "ok\n" {
		t.Errorf("status line must survive a failed save: %d %q", code, out)
	}
}

func setup(t *testing.T, args ...string) (int, []string, string) {
	t.Helper()
	var o, e bytes.Buffer
	code := Setup(args, &o, &e)
	return code, strings.Split(strings.TrimSuffix(o.String(), "\n"), "\n"), e.String()
}

func TestSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	// key order, number forms, non-ASCII and <&> must all survive; only statusLine.command changes
	orig := `{
  "theme": "dark",
  "n": 1e3,
  "env": {"中文": "值", "h": "<&>"},
  "statusLine": {"type": "command", "command": "echo it's $HOME | cat 2>&1", "padding": 0},
  "z": true
}`
	os.WriteFile(path, []byte(orig), 0o600)

	code, lines, _ := setup(t)
	after := `agmux statusline -- 'echo it'\''s $HOME | cat 2>&1'`
	want := []string{"statusLine.command:", "  before: echo it's $HOME | cat 2>&1", "  after:  " + after, "dry run: add --write to apply"}
	if code != 0 || strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("dry run: %d %q", code, lines)
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Fatal("dry run changed the file")
	}

	if code, lines, e := setup(t, "--write"); code != 0 || lines[3] != "written (backup: "+path+".bak)" {
		t.Fatalf("write: %d %q %s", code, lines, e)
	}
	got, _ := os.ReadFile(path)
	wantFile := `{
  "theme": "dark",
  "n": 1e3,
  "env": {
    "中文": "值",
    "h": "<&>"
  },
  "statusLine": {
    "type": "command",
    "command": "agmux statusline -- 'echo it'\\''s $HOME | cat 2>&1'",
    "padding": 0
  },
  "z": true
}
`
	if string(got) != wantFile {
		t.Errorf("written file:\n%s\nwant:\n%s", got, wantFile)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != orig {
		t.Error(".bak must be the original bytes")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want the original 0600", fi.Mode().Perm())
	}
	if code, lines, _ := setup(t, "--write"); code != 0 || lines[0] != "already set: "+after {
		t.Errorf("second run: %d %q", code, lines)
	}
}

func TestSetupEdges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "settings.json")

	if code, lines, _ := setup(t); code != 0 || lines[1] != "  before: (none)" || lines[2] != "  after:  agmux statusline" {
		t.Errorf("no file, dry run: %d %q", code, lines)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("dry run created the file")
	}
	if code, _, e := setup(t, "--write"); code != 0 {
		t.Fatalf("no file, write: %d %s", code, e)
	}
	if got, _ := os.ReadFile(path); string(got) != "{\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"agmux statusline\"\n  }\n}\n" {
		t.Errorf("new file: %s", got)
	}

	for _, bad := range []string{"{oops", "", "[1]", `{"statusLine": 5}`, `{"statusLine": {"command": 5}}`, `{} {}`} {
		os.WriteFile(path, []byte(bad), 0o644)
		if code, _, e := setup(t, "--write"); code != 1 || e == "" {
			t.Errorf("%q: code %d stderr %q", bad, code, e)
		}
		if got, _ := os.ReadFile(path); string(got) != bad {
			t.Errorf("%q: file changed", bad)
		}
	}
	for _, args := range [][]string{{"--bogus"}, {"--write", "x"}} {
		if code, _, _ := setup(t, args...); code != 2 {
			t.Errorf("%v: want usage error 2", args)
		}
	}
}

// ~/.claude/settings.json as a symlink into a dotfiles repo: the target changes, the link stays.
func TestSetupSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	real := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(real, []byte(`{"statusLine":{"type":"command","command":"hud.sh"}}`), 0o644)
	link := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.Symlink(real, link)

	if code, _, e := setup(t, "--write"); code != 0 {
		t.Fatalf("%d %s", code, e)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a file")
	}
	if got, _ := os.ReadFile(real); !strings.Contains(string(got), `"agmux statusline -- 'hud.sh'"`) {
		t.Errorf("target not updated: %s", got)
	}
	if _, err := os.Stat(real + ".bak"); err != nil {
		t.Error(".bak belongs next to the target")
	}
	if _, err := os.Lstat(link + ".bak"); err == nil {
		t.Error("no .bak next to the link")
	}
}

func TestSetupReadOnlyDirLeavesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o644)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if code, _, _ := setup(t, "--write"); code != 1 {
		t.Errorf("want exit 1, got %d", code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("stray files left: %v", entries)
	}
}

// A read-only settings.json in a writable directory: rename would succeed, but the user said no.
func TestSetupReadOnlyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	path := filepath.Join(dir, "settings.json")
	os.MkdirAll(dir, 0o755)
	orig := []byte(`{"theme":"dark"}`)
	os.WriteFile(path, orig, 0o444)
	if code, _, e := setup(t, "--write"); code != 1 || e == "" {
		t.Errorf("want exit 1 with an error, got %d %q", code, e)
	}
	if got, _ := os.ReadFile(path); string(got) != string(orig) {
		t.Error("file changed")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o444 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("no .bak or tmp file may be left: %v", entries)
	}
}
