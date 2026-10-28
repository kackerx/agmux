package recall

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The two real formats, anonymized (testdata/): what a transcript holds that is not a sentence is skipped.
func TestClaudeRead(t *testing.T) {
	path := filepath.Join("testdata", "claude-session.jsonl")
	h, lines, end, err := Claude{}.Read(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := Head{ID: "claude-session", CWD: "/work/shopcart", Name: "cart-fix-2", Branch: "fix/coupon-order", Agent: "fullstack-dev", Path: path, Custom: true}
	if h != want {
		t.Errorf("head %+v, want %+v", h, want)
	}
	var got []string
	for _, l := range lines {
		got = append(got, string(l.Role)+" "+l.Text)
	}
	wantLines := []string{
		"> cart total is wrong when a coupon is applied",
		"< The coupon is subtracted before tax. Let me look at pricing.go.",
		"> [Image #1] see the screenshot, the total is 12.50 not 10.00",
		"> cart total is wrong when a coupon is applied", // Read does not dedup: the index does
		"> now add a regression test for the coupon order",
	}
	if !reflect.DeepEqual(got, wantLines) {
		t.Errorf("lines:\n%q\nwant\n%q", got, wantLines)
	}
	if want := time.Date(2026, 10, 20, 3, 0, 1, 0, time.UTC); !lines[0].At.Equal(want) {
		t.Errorf("time %v, want %v", lines[0].At, want)
	}
	b, _ := os.ReadFile(path)
	if want := int64(strings.LastIndexByte(string(b), '\n') + 1); end != want {
		t.Errorf("offset %d: the unfinished last line is not consumed, want %d", end, want)
	}
	if c := (Claude{}).Resume(Head{ID: "abc"}); !reflect.DeepEqual(c, []string{"claude", "--resume", "abc"}) {
		t.Errorf("resume %q", c)
	}
}

func TestPiRead(t *testing.T) {
	path := filepath.Join("testdata", "pi-session.jsonl")
	h, lines, end, err := Pi{}.Read(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := Head{ID: "019e0000-aaaa-7bbb-8ccc-000000000001", CWD: "/work/ledger.worktrees/feature/audit/api", Name: "audit-export-v2", Path: path}
	if h != want {
		t.Errorf("head %+v, want %+v", h, want)
	}
	var got []string
	for _, l := range lines {
		got = append(got, string(l.Role)+" "+l.Text)
	}
	wantLines := []string{
		"> why does the audit export skip rows with a null owner",
		"< The export joins owners with an inner join, so null owners drop out.",
		"> switch to a left join and add a test",
	}
	if !reflect.DeepEqual(got, wantLines) {
		t.Errorf("lines:\n%q\nwant\n%q", got, wantLines)
	}
	if fi, _ := os.Stat(path); end != fi.Size() {
		t.Errorf("offset %d, want the whole file %d", end, fi.Size())
	}
	if c := (Pi{}).Resume(h); !reflect.DeepEqual(c, []string{"pi", "--session", path}) {
		t.Errorf("resume %q", c)
	}
}

// Reading on from the offset gives the rest, and the parts merge into what the whole file says:
// the session id and directory stay as first seen, name and branch are the latest.
func TestReadFromAnOffsetAndMerge(t *testing.T) {
	b, _ := os.ReadFile(filepath.Join("testdata", "claude-session.jsonl"))
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	cut := strings.Index(string(b), `{"type":"custom-title"`) // after the 4th real line, before the titles
	os.WriteFile(path, b[:cut], 0o644)
	h1, l1, off1, err := Claude{}.Read(path, 0)
	if err != nil || off1 != int64(cut) {
		t.Fatalf("first part: offset %d (want %d) %v", off1, cut, err)
	}
	os.WriteFile(path, b, 0o644)
	h2, l2, _, err := Claude{}.Read(path, off1)
	if err != nil {
		t.Fatal(err)
	}
	if len(l1)+len(l2) != 5 || len(l2) != 2 {
		t.Errorf("%d + %d lines, want 3 + 2", len(l1), len(l2))
	}
	if h2.CWD != "/work/shopcart/cmd/api" || h1.CWD != "/work/shopcart" {
		t.Errorf("each part has the directory it saw: %q, %q", h1.CWD, h2.CWD)
	}
	if m := h1.Merge(h2); m.CWD != "/work/shopcart" || m.Name != "cart-fix-2" || m.Branch != "fix/coupon-order" || m.Agent != "fullstack-dev" || m.ID != "s1" {
		t.Errorf("merged %+v", m)
	}
	if m := (Head{Name: "a", Branch: "b"}).Merge(Head{}); m.Name != "a" || m.Branch != "b" {
		t.Errorf("an empty later part changes nothing: %+v", m)
	}
}

// What is noise, and what is cut (design §4).
func TestClean(t *testing.T) {
	for _, c := range []struct {
		in, out string
		ok      bool
	}{
		{"fix the  cart\n total", "fix the cart total", true},
		{"  [Image #3] look at this  ", "[Image #3] look at this", true},
		{"\n  <system-reminder>x</system-reminder>", "", false}, // the start is after the white space
		{"This session is being continued from a previous conversation", "", false},
		{"<local-command-stdout>x</local-command-stdout>", "", false},
		{"<command-name>/clear</command-name>", "", false},
		{"<command-message>review</command-message>", "", false},
		{"<bash-input>ls</bash-input>", "", false},
		{"<task-notification>done</task-notification>", "", false},
		{"<system-reminder>be good</system-reminder>", "", false},
		{"[Request interrupted by user]", "", false},
		{"ok", "", false},
		{"好的好", "好的好", false},  // 3 characters
		{"好的好吗", "好的好吗", true}, // 4 characters, though 12 bytes
		{"<pasted_content id=7> % npm login\nUsername: x\n</pasted_content id=7> 为什么登录了还是这样", "为什么登录了还是这样", true},
		{"before <pasted_content id=1>aaa</pasted_content id=1> and <pasted_content id=2>bbb\nccc</pasted_content id=2> after", "before and after", true},
		{"stuck<pasted_content id=6>zzz</pasted_content id=6>together", "stucktogether", true},
		{"<pasted_content id=3>only pasted text, pages of it</pasted_content id=3>", "", false},
		{"<pasted_content id=4>a</pasted_content id=4> ok", "", false}, // what is left is too short
		{"an unfinished <pasted_content id=5> block stays", "an unfinished <pasted_content id=5> block stays", true},
		{"say <system-reminder> in the middle", "say <system-reminder> in the middle", true},
	} {
		if got, ok := Clean(c.in); ok != c.ok || ok && got != c.out {
			t.Errorf("Clean(%q) = %q, %v; want %q, %v", c.in, got, ok, c.out, c.ok)
		}
	}
	long := strings.Repeat("字", 250)
	if got, _ := Clean(long); got != strings.Repeat("字", maxText) {
		t.Errorf("a long sentence is cut to %d characters, got %d", maxText, len([]rune(got)))
	}
}

// A line longer than the read buffer is one line; one without a newline yet is left alone.
func TestEachLineLongAndUnfinished(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	long := strings.Repeat("x", 600<<10)
	os.WriteFile(path, []byte("a\n"+long+"\nb\nunfinished"), 0o644)
	var got []int
	end, err := eachLine(path, 0, func(l []byte) { got = append(got, len(l)) })
	if err != nil || !reflect.DeepEqual(got, []int{1, len(long), 1}) || end != int64(len(long)+5) {
		t.Errorf("lines %v end %d err %v", got, end, err)
	}
	if _, err := eachLine(filepath.Join(t.TempDir(), "none"), 0, nil); err == nil {
		t.Error("a missing file is an error")
	}
}

func TestClaudeDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if d, _ := ClaudeDir(); d != "/cfg" {
		t.Errorf("%q", d)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/h")
	if d, _ := ClaudeDir(); d != "/h/.claude" {
		t.Errorf("%q", d)
	}
}

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	return p
}

// What is in a Claude transcript and nobody said, or said elsewhere, is not a sentence; the other
// ways a message holds text are.
func TestClaudeSkipsWhatNobodySaid(t *testing.T) {
	p := writeLines(t,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"injected by the harness, not said"}}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"a summary of the earlier conversation"}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"said by a subagent"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"not a text block, though it has the field"},{"type":"text","text":"first text block"},{"type":"tool_use","text":"nor this","id":"t"},{"type":"text","text":"second text block"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"user block one"},{"type":"image","text":"no"},{"type":"text","text":"user block two"}]}}`,
		`{"type":"user","gitBranch":"main","cwd":"/a","message":{"role":"user","content":"on main for a while"}}`,
		`{"type":"user","gitBranch":"HEAD","cwd":"/b","message":{"role":"user","content":"then a detached head"}}`,
	)
	h, lines, _, err := Claude{}.Read(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lines {
		got = append(got, string(l.Role)+" "+l.Text)
	}
	want := []string{"< first text block", "< second text block", "> user block one", "> user block two", "> on main for a while", "> then a detached head"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	if h.Branch != "main" || h.CWD != "/a" {
		t.Errorf("a detached HEAD is not a branch, and the directory is the first: %q %q", h.Branch, h.CWD)
	}
}

// A name the user gave stays over one that was made up, in a part of the file or across parts; a
// made-up one is a name when there is no other; an empty one is none.
func TestClaudeTitles(t *testing.T) {
	custom := `{"type":"custom-title","customTitle":"mine","sessionId":"s"}`
	ai := `{"type":"ai-title","aiTitle":"made up","sessionId":"s"}`
	empty := `{"type":"custom-title","customTitle":"","sessionId":"s"}`
	for name, c := range map[string]struct {
		lines []string
		want  string
	}{
		"custom then ai": {[]string{custom, ai}, "mine"}, "ai then custom": {[]string{ai, custom}, "mine"},
		"only ai": {[]string{ai}, "made up"}, "empty custom": {[]string{custom, empty}, "mine"}, "none": {[]string{}, ""},
	} {
		h, _, _, _ := Claude{}.Read(writeLines(t, c.lines...), 0)
		if h.Name != c.want {
			t.Errorf("%s: %q, want %q", name, h.Name, c.want)
		}
	}
	h1, _, _, _ := Claude{}.Read(writeLines(t, custom), 0)
	h2, _, _, _ := Claude{}.Read(writeLines(t, ai), 0)
	if m := h1.Merge(h2); m.Name != "mine" || !m.Custom {
		t.Errorf("a made-up name in a later part does not replace the user's: %+v", m)
	}
	if m := h2.Merge(h1); m.Name != "mine" || !m.Custom {
		t.Errorf("and the user's replaces a made-up one: %+v", m)
	}
	if m := (Head{Name: "old", Custom: true}).Merge(Head{Name: "renamed", Custom: true}); m.Name != "renamed" {
		t.Errorf("a rename replaces the user's name: %+v", m)
	}
	if m := (Head{ID: "a", Path: "p1"}).Merge(Head{ID: "b", Path: "p2"}); m.ID != "a" || m.Path != "p1" {
		t.Errorf("the id and the path stay as first seen: %+v", m)
	}
}

// A directory with glob characters in its name is still a directory.
func TestFilesInADirectoryWithGlobCharacters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj[1]*?", ".claude")
	os.MkdirAll(filepath.Join(root, "projects", "-p"), 0o755)
	want := filepath.Join(root, "projects", "-p", "s.jsonl")
	os.WriteFile(want, []byte("{}\n"), 0o644)
	if got, err := (Claude{Dir: root}).Files(); err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("%v %v", got, err)
	}
	os.MkdirAll(filepath.Join(root, "sessions", "--p--"), 0o755)
	want = filepath.Join(root, "sessions", "--p--", "t.jsonl")
	os.WriteFile(want, []byte("{}\n"), 0o644)
	if got, err := (Pi{Dir: root}).Files(); err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("%v %v", got, err)
	}
}

// The characters of a pattern in a directory's name match only themselves: a sibling directory the
// pattern would also match is not listed.
func TestGlobCharactersMatchOnlyThemselves(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"p*", "pq", "p?", "p[a]", "pa"} {
		os.MkdirAll(filepath.Join(base, d, "projects", "-x"), 0o755)
		os.WriteFile(filepath.Join(base, d, "projects", "-x", "s.jsonl"), []byte("{}\n"), 0o644)
	}
	for _, d := range []string{"p*", "p?", "p[a]"} {
		got, err := (Claude{Dir: filepath.Join(base, d)}).Files()
		if want := filepath.Join(base, d, "projects", "-x", "s.jsonl"); err != nil || len(got) != 1 || got[0] != want {
			t.Errorf("%s: %v %v", d, got, err)
		}
	}
}
