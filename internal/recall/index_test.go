package recall

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// spy is a Source that remembers where each Read started, and can fail on one transcript.
type spy struct {
	Source
	mu     sync.Mutex
	reads  map[string][]int64
	fail   string
	onRead func(path string)
}

func (s *spy) Read(path string, from int64) (Head, []Line, int64, error) {
	s.mu.Lock()
	if s.onRead != nil {
		s.onRead(path)
	}
	if s.reads == nil {
		s.reads = map[string][]int64{}
	}
	s.reads[filepath.Base(path)] = append(s.reads[filepath.Base(path)], from)
	s.mu.Unlock()
	if s.fail != "" && strings.HasSuffix(path, s.fail) {
		return Head{}, nil, from, errors.New("unreadable")
	}
	return s.Source.Read(path, from)
}

func (s *spy) readsOf(name string) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.reads[name]...)
}

// finished is the fixture without its unfinished last line.
func finished(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "claude-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return b[:strings.LastIndexByte(string(b), '\n')+1]
}

type world struct {
	t     *testing.T
	root  string // a Claude config directory
	cache string
	src   *spy
}

func newWorld(t *testing.T) *world {
	root := t.TempDir()
	return &world{t: t, root: root, cache: filepath.Join(t.TempDir(), "recall"), src: &spy{Source: Claude{Dir: root}}}
}

func (w *world) transcript(id string, data []byte) string {
	p := filepath.Join(w.root, "projects", "-work-shopcart", id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) index() *Index { return &Index{Dir: w.cache, Sources: []Source{w.src}} }

func (w *world) update() *Index {
	w.t.Helper()
	ix := w.index()
	if err := ix.Update(); err != nil {
		w.t.Fatal(err)
	}
	return ix
}

func sentences(ls []Line) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Text)
	}
	return out
}

func TestUpdateBuildsAndReusesTheCache(t *testing.T) {
	w := newWorld(t)
	w.transcript("s1", finished(t))
	ix := w.update()
	es := ix.Entries()
	if len(es) != 1 {
		t.Fatalf("%d entries", len(es))
	}
	e := es[0]
	if e.Source != "claude" || e.ID != "s1" || e.CWD != "/work/shopcart" || e.Name != "cart-fix-2" || e.Branch != "fix/coupon-order" || e.Agent != "fullstack-dev" || e.Project != "shopcart" {
		t.Errorf("entry %+v", e)
	}
	if got := sentences(e.Users); len(got) != 3 || got[0] != "cart total is wrong when a coupon is applied" || got[2] != "now add a regression test for the coupon order" {
		t.Errorf("your sentences (the repeated one once): %q", got)
	}
	if got := sentences(e.Replies); len(got) != 1 {
		t.Errorf("the agent's: %q", got)
	}
	if _, err := os.Stat(filepath.Join(w.cache, "claude-s1.json")); err != nil {
		t.Error("no cache file:", err)
	}
	if got := w.src.readsOf("s1.jsonl"); len(got) != 1 || got[0] != 0 {
		t.Errorf("reads %v", got)
	}

	// the second open takes the cache as it is: nothing is read
	e2 := w.update().Entries()[0]
	if got := w.src.readsOf("s1.jsonl"); len(got) != 1 {
		t.Errorf("unchanged transcript read again: %v", got)
	}
	if e2.Name != e.Name || len(e2.Users) != 3 || e2.Offset != e.Offset || e2.Project != "shopcart" {
		t.Errorf("from the cache: %+v", e2)
	}
	if !e2.Users[0].At.Equal(e.Users[0].At) || e2.Users[0].At.IsZero() {
		t.Errorf("times survive the cache: %v vs %v", e2.Users[0].At, e.Users[0].At)
	}
	left, _ := filepath.Glob(filepath.Join(w.cache, ".tmp-*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestUpdateReadsOnFromTheOffset(t *testing.T) {
	w := newWorld(t)
	data := finished(t)
	p := w.transcript("s1", data)
	w.update()
	more := `{"type":"user","timestamp":"2026-10-21T01:00:00.000Z","cwd":"/work/shopcart","gitBranch":"main","message":{"role":"user","content":"deploy the coupon fix to staging"}}` + "\n" +
		`{"type":"user","timestamp":"2026-10-21T01:00:01.000Z","cwd":"/work/shopcart","message":{"role":"user","content":"now add a regression test for the coupon order"}}` + "\n"
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(more)
	f.Close()
	e := w.update().Entries()[0]
	if got := w.src.readsOf("s1.jsonl"); len(got) != 2 || got[1] != int64(len(data)) {
		t.Errorf("reads %v: the second starts where the first ended (%d)", got, len(data))
	}
	if got := sentences(e.Users); len(got) != 4 || got[3] != "deploy the coupon fix to staging" {
		t.Errorf("the new sentence once, the repeated one not again: %q", got)
	}
	if e.Branch != "main" || e.CWD != "/work/shopcart" || e.Offset != int64(len(data)+len(more)) {
		t.Errorf("branch %q cwd %q offset %d", e.Branch, e.CWD, e.Offset)
	}
	if e.Name != "cart-fix-2" || e.Agent != "fullstack-dev" || e.ID != "s1" || e.Path != p {
		t.Errorf("what the new part does not say stays: %+v", e.Head)
	}
}

func TestUpdateReadsAgainWhatShrankOrChangedVersion(t *testing.T) {
	w := newWorld(t)
	data := finished(t)
	p := w.transcript("s1", data)
	w.update()
	short := data[:strings.Index(string(data), `{"type":"custom-title"`)]
	os.WriteFile(p, short, 0o644)
	e := w.update().Entries()[0]
	if got := w.src.readsOf("s1.jsonl"); len(got) != 2 || got[1] != 0 {
		t.Errorf("shorter: reads %v, the second from 0", got)
	}
	if e.Name != "" || len(e.Users) != 2 || e.Offset != int64(len(short)) {
		t.Errorf("only what the short file says: name %q users %q", e.Name, sentences(e.Users))
	}

	// a cache of another format is read again
	cache := filepath.Join(w.cache, "claude-s1.json")
	b, _ := os.ReadFile(cache)
	os.WriteFile(cache, []byte(strings.Replace(string(b), fmt.Sprintf(`"v":%d`, Version), `"v":0`, 1)), 0o644)
	w.update()
	if got := w.src.readsOf("s1.jsonl"); len(got) != 3 || got[2] != 0 {
		t.Errorf("old cache version: reads %v", got)
	}
	// and so is one that is not even JSON
	os.WriteFile(cache, []byte("{half"), 0o644)
	if es := w.update().Entries(); len(es) != 1 || len(es[0].Users) != 2 {
		t.Errorf("garbage cache: %+v", es)
	}
	if got := w.src.readsOf("s1.jsonl"); len(got) != 4 || got[3] != 0 {
		t.Errorf("garbage cache: reads %v", got)
	}
}

func TestUpdateTouchedTranscriptReadsOnFromTheOffset(t *testing.T) {
	w := newWorld(t)
	data := finished(t)
	p := w.transcript("s1", data)
	w.update()
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	e := w.update().Entries()[0]
	if got := w.src.readsOf("s1.jsonl"); len(got) != 2 || got[1] != int64(len(data)) {
		t.Errorf("reads %v", got)
	}
	if e.MTime != later.UnixNano() || len(e.Users) != 3 {
		t.Errorf("mtime %d users %d", e.MTime, len(e.Users))
	}
}

func TestUpdateDropsWhatIsGone(t *testing.T) {
	w := newWorld(t)
	p1 := w.transcript("s1", finished(t))
	w.transcript("s2", finished(t))
	w.update()
	os.Remove(p1)
	es := w.update().Entries()
	if len(es) != 1 || es[0].ID != "s2" {
		t.Errorf("entries %+v", es)
	}
	if _, err := os.Stat(filepath.Join(w.cache, "claude-s1.json")); err == nil {
		t.Error("the cache file of a transcript that is gone stays")
	}
	if _, err := os.Stat(filepath.Join(w.cache, "claude-s2.json")); err != nil {
		t.Error("the other one went too")
	}
}

// Newest transcript first; progress counts every transcript; a transcript that can't be read is
// an error and the others are there.
func TestUpdateOrderProgressAndErrors(t *testing.T) {
	w := newWorld(t)
	old := w.transcript("old", finished(t))
	w.transcript("new", finished(t))
	w.transcript("bad", finished(t))
	os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	w.src.fail = "bad.jsonl"
	ix := w.index()
	var mu sync.Mutex
	maxDone, total := 0, 0
	ix.Progress = func(done, n int) {
		mu.Lock()
		maxDone, total = max(maxDone, done), n
		mu.Unlock()
	}
	err := ix.Update()
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("error %v", err)
	}
	es := ix.Entries()
	if len(es) != 2 || es[0].ID != "new" || es[1].ID != "old" {
		t.Errorf("entries %v", es)
	}
	if maxDone != 3 || total != 3 {
		t.Errorf("progress %d / %d", maxDone, total)
	}
	if _, err := os.Stat(filepath.Join(w.cache, "claude-bad.json")); err == nil {
		t.Error("a cache file for a transcript that was not read")
	}
}

func TestResetRemovesTheCache(t *testing.T) {
	w := newWorld(t)
	w.transcript("s1", finished(t))
	ix := w.update()
	if err := ix.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.cache); err == nil {
		t.Error("cache dir stays")
	}
	w.update()
	if got := w.src.readsOf("s1.jsonl"); len(got) != 2 || got[1] != 0 {
		t.Errorf("after a reset everything is read again: %v", got)
	}
}

// A cache file is written whole or not at all: a failed write leaves the old one and no temp file.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-x.json")
	if err := save(path, &Entry{V: Version, Head: Head{ID: "one"}}); err != nil {
		t.Fatal(err)
	}
	if e := load(path); e == nil || e.ID != "one" {
		t.Fatalf("saved: %+v", e)
	}
	os.Chmod(dir, 0o500) // no new files: the temp file can't be made
	defer os.Chmod(dir, 0o755)
	if os.Geteuid() != 0 {
		if err := save(path, &Entry{V: Version, Head: Head{ID: "two"}}); err == nil {
			t.Error("save into a read-only directory should fail")
		}
		if e := load(path); e == nil || e.ID != "one" {
			t.Errorf("the old file is intact: %+v", e)
		}
	}
	os.Chmod(dir, 0o755)
	if left, _ := filepath.Glob(filepath.Join(dir, ".tmp-*")); len(left) != 0 {
		t.Errorf("temp files %v", left)
	}
}

// Sentences are kept once per role, the first one and the newest of more than maxLines.
func TestAddDedupsAndCaps(t *testing.T) {
	var e Entry
	mk := func(role byte, texts ...string) (ls []Line) {
		for _, x := range texts {
			ls = append(ls, Line{Role: role, Text: x})
		}
		return
	}
	e.add(append(mk('>', "same", "other", "same"), mk('<', "same", "reply")...))
	if got := sentences(e.Users); strings.Join(got, ",") != "same,other" {
		t.Errorf("yours %q", got)
	}
	if got := sentences(e.Replies); strings.Join(got, ",") != "same,reply" {
		t.Errorf("the agent's: the same words by the other role stay: %q", got)
	}
	e.add(mk('>', "other", "third")) // across calls too
	if got := sentences(e.Users); strings.Join(got, ",") != "same,other,third" {
		t.Errorf("yours after more: %q", got)
	}
	var many []string
	for i := range maxLines + 50 {
		many = append(many, fmt.Sprintf("sentence number %d", i))
	}
	var f Entry
	f.add(mk('>', many...))
	f.add(mk('<', many[:10]...))
	if len(f.Users) != maxLines || f.Users[0].Text != many[0] || f.Users[1].Text != many[51] || f.Users[maxLines-1].Text != many[len(many)-1] {
		t.Errorf("cap: %d kept, first %q second %q last %q", len(f.Users), f.Users[0].Text, f.Users[1].Text, f.Users[len(f.Users)-1].Text)
	}
	if len(f.Replies) != 10 {
		t.Errorf("the cap is per role: %d replies", len(f.Replies))
	}
	var g Entry
	g.add(mk('>', many[:maxLines+1]...))
	if len(g.Users) != maxLines || g.Users[1].Text != many[2] {
		t.Errorf("one over the cap: %d kept, second %q", len(g.Users), g.Users[1].Text)
	}
}

func TestTitleFallsBackToTheFirstThingYouSaid(t *testing.T) {
	e := &Entry{Users: []Line{{Text: "first words"}, {Text: "later"}}}
	if e.Title() != "first words" {
		t.Errorf("%q", e.Title())
	}
	e.Name = "named"
	if e.Title() != "named" {
		t.Errorf("%q", e.Title())
	}
	if (&Entry{}).Title() != "" {
		t.Error("nothing to say")
	}
}

// A worktree is under its main repository's name; a directory that is not in a repository, or is gone, has its own.
func TestProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "ledger")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=a@b", "-c", "user.name=n", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(repo, 0o755)
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "x")
	wt := filepath.Join(dir, "ledger.worktrees", "audit")
	run("worktree", "add", "-q", "-b", "audit", wt)
	os.MkdirAll(filepath.Join(wt, "pkg", "api"), 0o755) // a session may be in a directory of the worktree
	// a repository whose git directory is elsewhere is not a worktree, and is named after its checkout
	sep := filepath.Join(dir, "sep")
	os.MkdirAll(filepath.Join(dir, "gitdata"), 0o755)
	if out, err := exec.Command("git", "init", "-q", "--separate-git-dir", filepath.Join(dir, "gitdata", "real.git"), sep).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	ix := &Index{}
	for cwd, want := range map[string][2]string{
		sep: {"sep", ""}, filepath.Join(wt, "pkg", "api"): {"ledger", "audit"},
		repo: {"ledger", ""}, wt: {"ledger", "audit"}, filepath.Join(wt, "."): {"ledger", "audit"},
		filepath.Join(repo, "sub"): {"sub", ""}, dir: {filepath.Base(dir), ""}, filepath.Join(dir, "gone", "api"): {"api", ""}, "": {"", ""},
	} {
		if p, w := ix.project(cwd); p != want[0] || w != want[1] {
			t.Errorf("project(%q) = %q, %q; want %q, %q", cwd, p, w, want[0], want[1])
		}
	}
}

// A transcript that moved (the cache is for another path) or changed size under the same mtime is
// not read on from an offset that means nothing now.
func TestUpdateDoesNotTrustAnOffsetOfAnotherFile(t *testing.T) {
	w := newWorld(t)
	data := finished(t)
	p := w.transcript("s1", data)
	w.update()
	moved := filepath.Join(w.root, "projects", "-elsewhere", "s1.jsonl")
	os.MkdirAll(filepath.Dir(moved), 0o755)
	os.Rename(p, moved)
	e := w.update().Entries()[0]
	if got := w.src.readsOf("s1.jsonl"); len(got) != 2 || got[1] != 0 || e.Path != moved {
		t.Errorf("moved: reads %v path %q", got, e.Path)
	}

	fi, _ := os.Stat(moved)
	f, _ := os.OpenFile(moved, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user","message":{"role":"user","content":"a sentence added later"}}` + "\n")
	f.Close()
	os.Chtimes(moved, fi.ModTime(), fi.ModTime()) // the same mtime, another size
	e = w.update().Entries()[0]
	if got := w.src.readsOf("s1.jsonl"); len(got) != 3 || got[2] != int64(len(data)) {
		t.Errorf("same mtime, grown: reads %v", got)
	}
	if got := sentences(e.Users); got[len(got)-1] != "a sentence added later" {
		t.Errorf("%q", got)
	}
}

// Each transcript is read in the order of its age when one is read at a time: what the user wants most is first.
func TestUpdateNewestFirstAndKeepsTheCacheOfAFailedRead(t *testing.T) {
	w := newWorld(t)
	var order []string
	w.src.onRead = func(p string) { order = append(order, filepath.Base(p)) }
	for i, id := range []string{"a", "b", "c"} {
		p := w.transcript(id, finished(t))
		os.Chtimes(p, time.Now().Add(time.Duration(i)*time.Hour), time.Now().Add(time.Duration(i)*time.Hour))
	}
	ix := w.index()
	ix.Workers = 1
	if err := ix.Update(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, " ") != "c.jsonl b.jsonl a.jsonl" {
		t.Errorf("read in the order %v", order)
	}
	// a transcript that changes and then can't be read is still found by what was cached
	p := filepath.Join(w.root, "projects", "-work-shopcart", "b.jsonl")
	later := time.Now().Add(5 * time.Hour)
	os.Chtimes(p, later, later)
	w.src.fail = "b.jsonl"
	ix = w.index()
	err := ix.Update()
	var found bool
	for _, e := range ix.Entries() {
		found = found || e.ID == "b" && len(e.Users) == 3
	}
	if err == nil || !found || len(ix.Entries()) != 3 {
		t.Errorf("err %v, entries %d, b found %v", err, len(ix.Entries()), found)
	}
}

// A rename that fails leaves no temp file behind.
func TestSaveRemovesItsTempFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "claude-x.json")
	os.MkdirAll(filepath.Join(target, "occupied"), 0o755) // a directory is in the way: rename fails
	if err := save(target, &Entry{V: Version}); err == nil {
		t.Fatal("save over a directory should fail")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".tmp-*")); len(left) != 0 {
		t.Errorf("temp files %v", left)
	}
}

// The command that opens a session again comes from its own source; an unknown one has none.
func TestResumeIsOfTheSource(t *testing.T) {
	ix := &Index{Sources: []Source{Claude{}, Pi{}}}
	if got := ix.Resume(&Entry{Source: "claude", Head: Head{ID: "c1", Path: "/p/c1.jsonl"}}); strings.Join(got, " ") != "claude --resume c1" {
		t.Errorf("claude: %q", got)
	}
	if got := ix.Resume(&Entry{Source: "pi", Head: Head{ID: "p1", Path: "/p/p1.jsonl"}}); strings.Join(got, " ") != "pi --session /p/p1.jsonl" {
		t.Errorf("pi: %q", got)
	}
	if got := ix.Resume(&Entry{Source: "codex"}); got != nil {
		t.Errorf("codex: %q", got)
	}
}

// Several transcripts are read at once and the newer ones take longer: Entries is newest first all the
// same, and at every moment of the build; Progress is told 1, 2, 3 … in that order.
func TestUpdateWithManyWorkersKeepsTheOrder(t *testing.T) {
	w := newWorld(t)
	const n = 6
	for i := range n {
		p := w.transcript(fmt.Sprintf("s%d", i), finished(t))
		at := time.Now().Add(-time.Duration(i) * time.Hour) // s0 is the newest
		os.Chtimes(p, at, at)
	}
	w.src.onRead = func(p string) { // the newest is the slowest
		var i int
		fmt.Sscanf(filepath.Base(p), "s%d.jsonl", &i)
		time.Sleep(time.Duration(n-i) * 15 * time.Millisecond)
	}
	ix := w.index()
	ix.Workers = n
	var seen []int
	var mu sync.Mutex
	ix.Progress = func(done, total int) {
		es := ix.Entries()
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, done)
		for i := 1; i < len(es); i++ {
			if es[i-1].MTime < es[i].MTime {
				t.Errorf("during the build, after %d: entries are not newest first", done)
			}
		}
	}
	if err := ix.Update(); err != nil {
		t.Fatal(err)
	}
	for i, e := range ix.Entries() {
		if want := fmt.Sprintf("s%d", i); e.ID != want {
			t.Errorf("entry %d is %s, want %s", i, e.ID, want)
		}
	}
	for i, d := range seen {
		if d != i+1 {
			t.Errorf("Progress was told %v", seen)
			break
		}
	}
}

// A temp file that a crashed write left is removed, a young one (another agmux may be writing it) is not.
func TestUpdateRemovesOldTempFiles(t *testing.T) {
	w := newWorld(t)
	w.transcript("s1", finished(t))
	os.MkdirAll(w.cache, 0o755)
	old, young := filepath.Join(w.cache, ".tmp-old"), filepath.Join(w.cache, ".tmp-young")
	os.WriteFile(old, []byte("x"), 0o644)
	os.WriteFile(young, []byte("x"), 0o644)
	at := time.Now().Add(-2 * time.Minute)
	os.Chtimes(old, at, at)
	w.update()
	if _, err := os.Stat(old); err == nil {
		t.Error("the old temp file stays")
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("the young one went:", err)
	}
}

// A session's name may hold anything the user typed; it shows as one line.
func TestTitleHasNoControlCharacters(t *testing.T) {
	e := &Entry{Head: Head{Name: "a\x1b[0mb\nc\td\u202ee"}}
	if got := e.Title(); got != "a [0mb c d e" {
		t.Errorf("%q", got)
	}
	e = &Entry{Users: []Line{{Text: "first\rwords"}}}
	if got := e.Title(); got != "first words" {
		t.Errorf("%q", got)
	}
}

// Transcripts of the same age are in the order of their paths, whichever was read first.
func TestUpdateEqualAgesGoByPath(t *testing.T) {
	w := newWorld(t)
	at := time.Now().Add(-time.Hour)
	for _, id := range []string{"b", "c", "a"} {
		os.Chtimes(w.transcript(id, finished(t)), at, at)
	}
	ix := w.index()
	ix.Workers = 1
	if err := ix.Update(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ix.Entries() {
		got = append(got, e.ID)
	}
	if strings.Join(got, " ") != "a b c" {
		t.Errorf("%v", got)
	}
}
