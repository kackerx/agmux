package board

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/recall"
	"github.com/kackerx/agmux/internal/store"
)

// rworld is a Claude config directory with transcripts, and the index over it.
type rworld struct {
	t     *testing.T
	root  string
	ix    *recall.Index
	dirs  map[string]string // name → a directory that exists
	clock time.Time
}

func newRWorld(t *testing.T) *rworld {
	root := t.TempDir()
	w := &rworld{t: t, root: root, dirs: map[string]string{}, clock: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	w.ix = &recall.Index{Dir: filepath.Join(t.TempDir(), "cache"), Sources: []recall.Source{recall.Claude{Dir: root}, recall.Pi{Dir: filepath.Join(root, "pi")}}}
	return w
}

// dir is a directory called name that exists.
func (w *rworld) dir(name string) string {
	if d, ok := w.dirs[name]; ok {
		return d
	}
	d := filepath.Join(w.t.TempDir(), name)
	os.MkdirAll(d, 0o755)
	w.dirs[name] = d
	return d
}

// claude writes a transcript: lines are "u: text" (yours) or "a: text" (the agent's), a second apart.
func (w *rworld) claude(id, cwd, title string, age time.Duration, lines ...string) string {
	var out []string
	enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if title != "" {
		out = append(out, enc(map[string]any{"type": "custom-title", "customTitle": title, "sessionId": id}))
	}
	for i, l := range lines {
		role, text := "user", strings.TrimPrefix(l, "u: ")
		if strings.HasPrefix(l, "a: ") {
			role, text = "assistant", strings.TrimPrefix(l, "a: ")
		}
		var content any = text
		if role == "assistant" {
			content = []any{map[string]any{"type": "text", "text": text}}
		}
		out = append(out, enc(map[string]any{"type": role, "timestamp": w.clock.Add(-age).Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano), "cwd": cwd, "gitBranch": "main",
			"message": map[string]any{"role": role, "content": content}}))
	}
	p := filepath.Join(w.root, "projects", "-x", id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(strings.Join(out, "\n")+"\n"), 0o644)
	at := w.clock.Add(-age)
	os.Chtimes(p, at, at)
	return p
}

func (w *rworld) pi(id, cwd string, age time.Duration, lines ...string) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	out := []string{enc(map[string]any{"type": "session", "id": id, "cwd": cwd})}
	for i, l := range lines {
		role := "user"
		if strings.HasPrefix(l, "a: ") {
			role = "assistant"
		}
		out = append(out, enc(map[string]any{"type": "message", "timestamp": w.clock.Add(-age).Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			"message": map[string]any{"role": role, "content": []any{map[string]any{"type": "text", "text": strings.TrimPrefix(strings.TrimPrefix(l, "u: "), "a: ")}}}}))
	}
	p := filepath.Join(w.root, "pi", "sessions", "--x--", "2026-01-01T00-00-00-000Z_"+id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(strings.Join(out, "\n")+"\n"), 0o644)
	at := w.clock.Add(-age)
	os.Chtimes(p, at, at)
	return p
}

// fill is the usual world: three Claude sessions (one in a directory that is gone) and a pi one.
func (w *rworld) fill() {
	w.claude("s-cart", w.dir("shopcart"), "cart-fix", time.Hour, "u: cart total is wrong with a coupon", "a: the coupon is subtracted before tax", "u: add a regression test for the coupon")
	w.claude("s-ledger", w.dir("ledger"), "", 3*time.Hour, "u: audit export skips rows with a null owner", "a: it is an inner join")
	w.claude("s-gone", "/no/such/dir/gone-project", "legacy", 24*time.Hour, "u: migrate the legacy table")
	w.pi("p-api", w.dir("api"), 2*time.Hour, "u: why does the export skip rows", "a: the join drops nulls")
}

func (w *rworld) model(live ...store.Session) recallModel {
	w.t.Helper()
	if err := w.ix.Update(); err != nil {
		w.t.Fatal(err)
	}
	cfg := config.Default()
	m := NewRecall(w.ix, cfg, live, "shopcart", "").(recallModel)
	m.now = func() time.Time { return w.clock }
	m.lookPath = func(s string) (string, error) { return "/bin/" + s, nil }
	m.copyToClip = func(string) error { return nil }
	m.indexing = false
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m, cmd := tm.(recallModel).search(false)
	m, _ = rfeed(m, cmd)
	return m
}

// rfeed runs the search the screen asked for and gives it the answer, as the runtime does; quit says
// that the command was tea.Quit.
func rfeed(m recallModel, cmd tea.Cmd) (recallModel, bool) {
	if cmd == nil {
		return m, false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return m, true
	case recallFoundMsg:
		tm, _ := m.Update(msg)
		return tm.(recallModel), false
	}
	return m, false
}

func rpress(t *testing.T, m recallModel, keys ...string) (recallModel, bool) {
	t.Helper()
	quit := false
	for _, k := range keys {
		tm, cmd := m.Update(key(k))
		var q bool
		m, q = rfeed(tm.(recallModel), cmd)
		quit = quit || q
	}
	return m, quit
}

func rtype(t *testing.T, m recallModel, text string) recallModel {
	t.Helper()
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	m, _ = rfeed(tm.(recallModel), cmd)
	return m
}

func rids(m recallModel) string {
	var s []string
	for _, r := range m.results {
		s = append(s, r.ID)
	}
	return strings.Join(s, " ")
}

func rview(m recallModel) string { return ansi.Strip(m.View()) }

// No query: every session, newest first, each with the last thing you said; letters are text.
func TestRecallListAndTyping(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	if got := rids(m); got != "s-cart p-api s-ledger s-gone" {
		t.Fatalf("no query: %q", got)
	}
	if r := m.results[0]; r.Snippet.Text != "add a regression test for the coupon" || r.Snippet.Role != '>' {
		t.Errorf("snippet %+v", r.Snippet)
	}
	v := rview(m)
	for _, want := range []string{" / ", "4 ", "cart-fix", "shopcart", "main", "1h", "3 ", "add a regression test for the coupon", "legacy", "1d"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m = rtype(t, m, "'coupon")
	if got := rids(m); got != "s-cart" || m.query != "'coupon" {
		t.Errorf("coupon: %q", got)
	}
	if r := m.results[0]; r.Hits != 3 {
		t.Errorf("hits %d (three sentences say it)", r.Hits)
	}
	m, _ = rpress(t, m, "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	if m.query != "" || len(m.results) != 4 {
		t.Errorf("backspace to nothing: %q, %d results", m.query, len(m.results))
	}
	// letters the board uses as keys are text here
	m = rtype(t, m, "jkaAq?")
	if m.query != "jkaAq?" {
		t.Errorf("query %q", m.query)
	}
	if m, quit := rpress(t, rtype(t, w.model(), "x"), "ctrl+c"); quit != true || m.query != "x" {
		t.Error("ctrl+c quits")
	}
}

func TestRecallMoveAndPick(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	if m.sel != 0 {
		t.Fatal(m.sel)
	}
	m, _ = rpress(t, m, "down", "down")
	if r, _ := m.selected(); r.ID != "s-ledger" {
		t.Errorf("down down: %s", r.ID)
	}
	m, _ = rpress(t, m, "up", "up", "up")
	if m.sel != 0 {
		t.Errorf("up stops at the top: %d", m.sel)
	}
	m, _ = rpress(t, m, "down", "down", "down", "down", "down")
	if m.sel != 3 {
		t.Errorf("down stops at the end: %d", m.sel)
	}
	// typing again goes back to the best match
	if m = rtype(t, m, "legacy"); m.sel != 0 || rids(m) != "s-gone" {
		t.Errorf("sel %d results %q", m.sel, rids(m))
	}
}

// enter: a closed session is resumed in its directory (the screen ends, the caller runs it); one that
// runs is not; a directory that is gone, or an agent that is not installed, is said and nothing runs.
func TestRecallEnter(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m, quit := rpress(t, m, "enter", "enter") // the first keeps the query (none here), the second resumes
	res := RecallOutcome(m)
	if !quit || res == nil || res.Dir != w.dir("shopcart") || strings.Join(res.Argv, " ") != "claude --resume s-cart" {
		t.Errorf("claude: quit %v %+v", quit, res)
	}
	m = w.model()
	m, quit = rpress(t, m, "down", "enter", "enter") // pi
	res = RecallOutcome(m)
	if !quit || res == nil || res.Dir != w.dir("api") || len(res.Argv) != 3 || res.Argv[0] != "pi" || res.Argv[1] != "--session" || !strings.HasSuffix(res.Argv[2], "_p-api.jsonl") {
		t.Errorf("pi: quit %v %+v", quit, res)
	}

	m = w.model()
	m, quit = rpress(t, rtype(t, m, "legacy"), "enter", "enter")
	if quit || RecallOutcome(m) != nil || !strings.Contains(rview(m), "directory is gone: /no/such/dir/gone-project") {
		t.Errorf("gone: quit %v\n%s", quit, rview(m))
	}

	m = w.model()
	m.lookPath = func(string) (string, error) { return "", fmt.Errorf("no") }
	m, quit = rpress(t, m, "enter", "enter")
	if quit || RecallOutcome(m) != nil || !strings.Contains(rview(m), "claude not found in PATH") {
		t.Errorf("no claude: quit %v\n%s", quit, rview(m))
	}

	live := store.Session{ID: "x1", SessionID: "s-cart", TmuxPane: "%5", Status: "working"}
	m = w.model(live)
	m, quit = rpress(t, m, "enter", "enter")
	if quit || RecallOutcome(m) != nil || !strings.Contains(rview(m), "cart-fix is running (%5): jump to it from the board") {
		t.Errorf("running: quit %v\n%s", quit, rview(m))
	}
	m = w.model(store.Session{SessionID: "s-cart"})
	if m, _ = rpress(t, m, "enter", "enter"); !strings.Contains(rview(m), "cart-fix is running: ") && !strings.Contains(rview(m), "cart-fix is running") || strings.Contains(rview(m), "running (") {
		t.Errorf("running with no pane:\n%s", rview(m))
	}
	// a running pi session is found by its transcript
	m = w.model(store.Session{Transcript: w.ix.Entries()[1].Path})
	if _, ok := m.liveOf(m.ix.Entries()[1]); !ok {
		t.Error("by transcript")
	}
	if _, quit := rpress(t, rtype(t, w.model(), "zzzzqqq"), "enter", "enter"); quit {
		t.Error("enter with nothing to resume does nothing")
	}
}

// esc: the panel, then the query, then the screen.
func TestRecallEscIsThreeLevels(t *testing.T) {
	m := func() recallModel { w := newRWorld(t); w.fill(); return w.model() }()
	m = rtype(t, m, "coupon")
	m, _ = rpress(t, m, "ctrl+f")
	if !m.panel {
		t.Fatal("ctrl+f opens the panel")
	}
	var quit bool
	if m, quit = rpress(t, m, "esc"); m.panel || m.query != "coupon" || quit {
		t.Errorf("first: panel %v query %q quit %v", m.panel, m.query, quit)
	}
	if m, quit = rpress(t, m, "esc"); m.query != "" || quit || len(m.results) != 4 {
		t.Errorf("second: query %q quit %v results %d", m.query, quit, len(m.results))
	}
	if _, quit = rpress(t, m, "esc"); !quit {
		t.Error("third: quit")
	}
}

func TestRecallPanel(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m, _ = rpress(t, m, "ctrl+f")
	v := rview(m)
	for _, want := range []string{"sources", "[x] claude", "[x] pi", "scope", "(•) all history", "( ) this project", "space tick · a all · A none · esc close"} {
		if !strings.Contains(v, want) {
			t.Errorf("panel lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "▸ [x] claude") {
		t.Errorf("the cursor is on the first row:\n%s", v)
	}
	m, _ = rpress(t, m, "space") // untick claude
	if rids(m) != "p-api" || !strings.Contains(rview(m), "[ ] claude") {
		t.Errorf("claude unticked: %q", rids(m))
	}
	m, _ = rpress(t, m, "down", "space") // untick pi too
	closed := m
	closed.panel = false
	if len(m.results) != 0 || !strings.Contains(rview(closed), "no source ticked") {
		t.Errorf("nothing ticked: %d results\n%s", len(m.results), rview(closed))
	}
	m, _ = rpress(t, m, "a")
	if len(m.results) != 4 || !strings.Contains(rview(m), "[x] claude") || !strings.Contains(rview(m), "[x] pi") {
		t.Errorf("a ticks all: %d", len(m.results))
	}
	m, _ = rpress(t, m, "A")
	if len(m.results) != 0 {
		t.Errorf("A clears: %d", len(m.results))
	}
	m, _ = rpress(t, m, "a", "down", "down", "space") // the second scope: this project
	if !m.scopeCWD || rids(m) != "s-cart" || !strings.Contains(rview(m), "(•) this project") {
		t.Errorf("project scope: %v %q", m.scopeCWD, rids(m))
	}
	m, _ = rpress(t, m, "up", "space")
	if m.scopeCWD || len(m.results) != 4 {
		t.Errorf("all history: %v %d", m.scopeCWD, len(m.results))
	}
	m, _ = rpress(t, m, "j", "j", "k", "enter") // down / up in the panel are [keys] down / up; enter ticks as space does
	if m.pcur != 2 {
		t.Errorf("pcur %d", m.pcur)
	}
	// typed text does not reach the query while the panel is open
	if m = rtype(t, m, "zz"); m.query != "" {
		t.Errorf("query %q", m.query)
	}
	if m, _ = rpress(t, m, "ctrl+f"); m.panel {
		t.Error("ctrl+f closes it too")
	}
}

func TestRecallScopeKey(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	if m, _ = rpress(t, m, "ctrl+t"); !m.scopeCWD || rids(m) != "s-cart" || !strings.Contains(rview(m), "ctrl+t scope:project") {
		t.Errorf("project: %q\n%s", rids(m), footerOf(m.View()))
	}
	if m, _ = rpress(t, m, "ctrl+t"); m.scopeCWD || len(m.results) != 4 || !strings.Contains(rview(m), "ctrl+t scope:all") {
		t.Errorf("all: %d", len(m.results))
	}
	// scope and query go together; the cursor stays on its session when it is still there
	m = rtype(t, m, "'export")
	m, _ = rpress(t, m, "down")
	if m, _ = rpress(t, m, "ctrl+t"); len(m.results) != 0 {
		t.Errorf("project + export: %q", rids(m))
	}
	if m, _ = rpress(t, m, "ctrl+t"); len(m.results) != 2 || m.sel != 0 {
		t.Errorf("back to all: %q, cursor %d (the session it was on was not in the list)", rids(m), m.sel)
	}
}

// What is in $XDG... the keys: ctrl+y copies `cd '<dir>' && <command>`; no tool, or a failing one, is said.
func TestRecallCopy(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	var got string
	m.copyToClip = func(s string) error { got = s; return nil }
	m, _ = rpress(t, m, "ctrl+y")
	want := "cd '" + w.dir("shopcart") + "' && claude --resume s-cart"
	if got != want || !strings.Contains(rview(m), "copied: "+want) {
		t.Errorf("copied %q\n%s", got, rview(m))
	}
	m, _ = rpress(t, m, "down", "down") // pi's: the file in the command, quoted when it needs it
	m.copyToClip = func(s string) error { got = s; return nil }
	rpress(t, m, "ctrl+y")
	if !strings.HasPrefix(got, "cd '"+w.dir("ledger")+"' && claude --resume s-ledger") {
		t.Errorf("%q", got)
	}
	m.copyToClip = func(string) error { return errNoClipboard }
	if m, _ = rpress(t, m, "ctrl+y"); !strings.Contains(rview(m), "no clipboard tool (pbcopy, xclip, wl-copy)") {
		t.Errorf("no tool:\n%s", rview(m))
	}
	m.copyToClip = func(string) error { return fmt.Errorf("pbcopy: exit status 1") }
	if m, _ = rpress(t, m, "ctrl+y"); !strings.Contains(rview(m), "copy failed: pbcopy: exit status 1") {
		t.Errorf("failing tool:\n%s", rview(m))
	}
	// a directory with a quote in it
	if r := (recallModel{ix: w.ix}); true {
		odd := &recall.Entry{Head: recall.Head{ID: "a b;c", CWD: "/x"}, Source: "claude"}
		if got := r.resumeLine(recall.Result{Entry: odd}); got != `cd '/x' && claude --resume 'a b;c'` {
			t.Errorf("%q", got)
		}
		e := &recall.Entry{Head: recall.Head{ID: "s1", CWD: "/it's here"}, Source: "claude"}
		if got := r.resumeLine(recall.Result{Entry: e}); got != `cd '/it'\''s here' && claude --resume s1` {
			t.Errorf("%q", got)
		}
	}
}

// The real copy: the first of pbcopy, wl-copy, xclip that is on the PATH gets the text on its input.
func TestCopyToClipboard(t *testing.T) {
	bin, out := t.TempDir(), filepath.Join(t.TempDir(), "clip")
	t.Setenv("PATH", bin)
	if err := copyToClipboard("x"); err != errNoClipboard {
		t.Errorf("no tool: %v", err)
	}
	script := func(name, body string) {
		os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	}
	script("xclip", `echo "xclip $*" > `+out+`; /bin/cat >> `+out)
	if err := copyToClipboard("hello there"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "xclip -selection clipboard\nhello there" {
		t.Errorf("xclip: %q", b)
	}
	script("pbcopy", `/bin/cat > `+out)
	if err := copyToClipboard("via pbcopy"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "via pbcopy" {
		t.Errorf("pbcopy is first: %q", b)
	}
	script("pbcopy", `echo broken >&2; exit 3`)
	if err := copyToClipboard("x"); err == nil || !strings.Contains(err.Error(), "pbcopy") || !strings.Contains(err.Error(), "broken") {
		t.Errorf("a failing tool: %v", err)
	}
}

// ctrl+o hands the terminal to $EDITOR (with its own arguments) on the transcript, whatever its name has in it.
func TestRecallOpen(t *testing.T) {
	w := newRWorld(t)
	p := w.claude("s-sp", w.dir("shopcart"), "x", time.Hour, "u: a sentence about opening")
	m := w.model()
	m.editor = func() string { return "cat -n" }
	tm, cmd := m.Update(key("ctrl+o"))
	if cmd == nil || tm.(recallModel).msg != "" {
		t.Fatalf("ctrl+o: cmd %v", cmd)
	}
	spaced := filepath.Join(t.TempDir(), "a b's $file.jsonl")
	os.WriteFile(spaced, []byte("content\n"), 0o644)
	out, err := editorCmd("cat -n", spaced).Output()
	if err != nil || !strings.Contains(string(out), "content") {
		t.Errorf("the editor with its arguments, on a path with a space, a quote and a $: %q %v", out, err)
	}
	_ = p
	// the error of the editor shows
	tm, _ = m.Update(recallEditedMsg{fmt.Errorf("exit status 2")})
	if !strings.Contains(rview(tm.(recallModel)), "exit status 2") {
		t.Error("editor error")
	}
	if _, cmd := w.model().Update(key("ctrl+o")); cmd == nil {
		t.Error("default editor")
	}
	m.results = nil
	if _, cmd := m.Update(key("ctrl+o")); cmd != nil {
		t.Error("nothing selected: nothing to open")
	}
}

// The preview: the card, then each hit with a sentence either side and a note for what is between.
func TestRecallPreview(t *testing.T) {
	w := newRWorld(t)
	var lines []string
	for i := range 9 {
		role := "u: "
		if i%2 == 1 {
			role = "a: "
		}
		lines = append(lines, fmt.Sprintf("%ssentence number %d", role, i))
	}
	lines[4] = "u: here is the needle of this conversation"
	w.claude("s-pv", w.dir("shopcart"), "pv", time.Hour, lines...)
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := rtype(t, w.model(), "'needle")
	v := rview(m)
	for _, want := range []string{"pv", "claude · main · 1h", "s-pv", "… 3 lines …", "‹ sentence number 3", "› here is the needle of this conversation", "‹ sentence number 5", "… 3 lines …"} {
		if !strings.Contains(v, want) {
			t.Errorf("preview lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "sentence number 0") || strings.Contains(v, "sentence number 8") {
		t.Errorf("what is between shows as a note:\n%s", v)
	}
	if !strings.Contains(v, "┏") || !strings.Contains(v, "┗") || strings.Count(v, "┏") != 3 {
		t.Error("the three boxes")
	}
	// the matching characters are lit in the list and the preview
	if raw := m.View(); strings.Count(raw, "48;2;242;208;107") < 2 {
		t.Errorf("highlights: %d", strings.Count(raw, "48;2;242;208;107"))
	}
	raw := strings.Split(m.View(), "\n")
	for i, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "› here is the needle") && !strings.Contains(raw[i], "48;2;242;208;107") {
			t.Errorf("the hit in the preview is not lit: %q", raw[i])
		}
	}
	// no query: the last of what was said
	m = w.model()
	v = rview(m)
	if !strings.Contains(v, "sentence number 8") || strings.Contains(v, "… ") && strings.Contains(v, " lines …") {
		t.Errorf("no query:\n%s", v)
	}
	// a match by the name only
	m = rtype(t, w.model(), "'pv")
	if v = rview(m); !strings.Contains(v, "matched by name, project or branch: no sentence matches") {
		t.Errorf("by name:\n%s", v)
	}
	if m.results[0].Hits != 0 {
		t.Errorf("hits %d", m.results[0].Hits)
	}
	// too narrow for a preview: only the list
	tm, _ := w.model().Update(tea.WindowSizeMsg{Width: 99, Height: 30})
	if v := rview(tm.(recallModel)); strings.Count(v, "┏") != 2 || !strings.Contains(v, "pv") {
		t.Errorf("99 columns:\n%s", v)
	}
	if tm, _ := w.model().Update(tea.WindowSizeMsg{Width: 100, Height: 30}); strings.Count(rview(tm.(recallModel)), "┏") != 3 {
		t.Error("100 columns has it")
	}
}

func TestRecallPreviewScroll(t *testing.T) {
	w := newRWorld(t)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("u: sentence about scrolling %d", i))
	}
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, lines...)
	w.claude("s-two", w.dir("shopcart"), "two", 2*time.Hour, "u: more scrolling here")
	m := rtype(t, w.model(), "scrolling")
	before := rview(m)
	if m, _ = rpress(t, m, "pgdown"); m.pvScroll != 3 || rview(m) == before {
		t.Errorf("pgdown: %d", m.pvScroll)
	}
	if m, _ = rpress(t, m, "pgup", "pgup"); m.pvScroll != 0 {
		t.Errorf("pgup stops at the top: %d", m.pvScroll)
	}
	if m, _ = rpress(t, m, "ctrl+p", "ctrl+p"); m.pvScroll != 0 { // T81: ctrl+n / ctrl+p are the default pgdown / pgup too
		t.Errorf("ctrl+p: %d", m.pvScroll)
	}
	if m, _ = rpress(t, m, "ctrl+n"); m.pvScroll != 3 {
		t.Errorf("ctrl+n: %d", m.pvScroll)
	}
	m, _ = rpress(t, m, "pgdown", "pgdown")
	if m, _ = rpress(t, m, "down"); m.pvScroll != 0 || m.sel != 1 { // another session: its preview from the top
		t.Errorf("down: scroll %d, cursor %d", m.pvScroll, m.sel)
	}
}

// The progress, while the index is built; what is there already is listed.
func TestRecallIndexing(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m.indexing = true
	m.progress.done.Store(3)
	m.progress.total.Store(10)
	if v := rview(m); !strings.Contains(strings.Split(v, "\n")[1], "indexing 3 / 10") || !strings.Contains(v, " 1/4 ") {
		t.Errorf("the input box: %s", strings.Split(v, "\n")[1])
	}
	m.results, m.indexing = nil, true
	if v := rview(m); strings.Contains(v, "no session matches") {
		t.Error("nothing is said to be missing while it is built")
	}
	tm, cmd := m.Update(recallIndexedMsg{fmt.Errorf("one bad transcript")})
	m, _ = rfeed(tm.(recallModel), cmd)
	if m.indexing || len(m.results) != 4 || !strings.Contains(rview(m), "some transcripts could not be read: one bad transcript") {
		t.Errorf("done: %v %d\n%s", m.indexing, len(m.results), rview(m))
	}
	if v := strings.Split(rview(m), "\n")[1]; strings.Contains(v, "indexing") || !strings.Contains(rview(m), " 1/4 ") {
		t.Errorf("input box after: %q", v)
	}
	// a tick picks up what the index has got since, and the cursor stays on its session
	m, _ = rpress(t, m, "down")
	was, _ := m.selected()
	w.claude("s-new", w.dir("shopcart"), "fresh", 10*time.Minute, "u: a brand new session")
	w.ix.Update()
	tm, cmd = m.Update(recallTickMsg{})
	if _, ok := cmd().(recallFoundMsg); !ok { // a search, and no next tick
		t.Error("it stops ticking when the index is done")
	}
	m, _ = rfeed(tm.(recallModel), cmd)
	if now, _ := m.selected(); len(m.results) != 5 || m.results[0].ID != "s-new" || now.ID != was.ID {
		t.Errorf("tick: %d results, first %s, selected %s (was %s)", len(m.results), m.results[0].ID, now.ID, was.ID)
	}
	m.indexing = true
	if _, cmd := m.Update(recallTickMsg{}); cmd == nil {
		t.Error("while it is built, it ticks")
	}
}

// A linked worktree is "repo.wt/directory" in the project column; a running session has a colored dot and its state.
func TestRecallWorktreeAndLive(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := newRWorld(t)
	repo := w.dir("ledger")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=a@b", "-c", "user.name=n", "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "x")
	wt := filepath.Join(t.TempDir(), "audit-wt")
	run("worktree", "add", "-q", "-b", "audit", wt)
	w.claude("s-wt", wt, "wt-session", time.Hour, "u: work in a worktree")
	w.claude("s-main", repo, "main-session", 2*time.Hour, "u: work in the main checkout")
	m := w.model(store.Session{SessionID: "s-wt", Status: "waiting"})
	v := rview(m)
	if !strings.Contains(v, "ledger.wt/audit-wt") || !strings.Contains(v, "main-session ledger ") && !strings.Contains(v, "main-session  ledger") {
		t.Errorf("project column:\n%s", v)
	}
	if raw := m.View(); !strings.Contains(raw, "●") || !strings.Contains(raw, "○") || !strings.Contains(v, "wt-session · waiting") {
		t.Errorf("dots / state:\n%s", v)
	}
}

// The keys are in [keys]: rebinding moves them, the old ones go; the words are in the language.
func TestRecallKeysAreConfigurable(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	c, err := config.Parse("cfg", []byte("[keys]\nrecall_filter = [\"ctrl+x\"]\nrecall_scope = [\"ctrl+q\"]\nrecall_toggle = [\"x\"]\nrecall_all = [\"y\"]\nrecall_none = [\"z\"]\ndown = [\"ctrl+f\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.cfg = c
	if m, _ = rpress(t, m, "ctrl+f"); m.sel != 1 || m.panel {
		t.Errorf("ctrl+f is down now: sel %d panel %v", m.sel, m.panel)
	}
	if m, _ = rpress(t, m, "ctrl+x"); !m.panel {
		t.Fatal("ctrl+x opens the panel")
	}
	if m, _ = rpress(t, m, "space"); len(m.unticked) != 0 {
		t.Error("space no longer ticks")
	}
	if m, _ = rpress(t, m, "x"); len(m.results) != 1 || !strings.Contains(rview(m), "[ ] claude") {
		t.Errorf("x ticks: %d", len(m.results))
	}
	if m, _ = rpress(t, m, "a"); len(m.results) != 1 {
		t.Error("a no longer ticks all")
	}
	if m, _ = rpress(t, m, "y"); len(m.results) != 4 {
		t.Error("y ticks all")
	}
	if m, _ = rpress(t, m, "z"); len(m.results) != 0 {
		t.Error("z clears")
	}
	if v := rview(m); !strings.Contains(v, "x tick · y all · z none · esc close") {
		t.Errorf("hint names the keys:\n%s", v)
	}
	m, _ = rpress(t, m, "esc")
	if m, _ = rpress(t, m, "ctrl+q"); !m.scopeCWD {
		t.Error("ctrl+q is the scope key")
	}
	// the footer names them too
	m.panel = false
	if v := footerOf(m.View()); !strings.Contains(v, "ctrl+x filter") || !strings.Contains(v, "ctrl+q scope:project") {
		t.Errorf("footer: %s", v)
	}
}

// Letters are text in the list, so the footer names only a key that acts there: the first one that
// is not a character; with none the action is left out, and resume is enter.
func TestRecallFooterNamesOnlyKeysThatAct(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	c, err := config.Parse("cfg", []byte("[keys]\njump = [\"j\"]\nrecall_filter = [\"f\"]\nrecall_scope = [\"s\", \"ctrl+q\"]\nrecall_copy = [\"y\", \"alt+y\"]\nrecall_open = [\"o\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.cfg = c
	f := footerOf(m.View())
	for _, want := range []string{"enter keep the query", "down/up move", "ctrl+q scope:all", "alt+y copy", "esc quit"} {
		if !strings.Contains(f, want) {
			t.Errorf("footer lacks %q: %s", want, f)
		}
	}
	for _, not := range []string{"j resume", "j keep", "f filter", "s scope", " y copy", "o open", "open", "resume"} {
		if strings.Contains(f, not) {
			t.Errorf("footer has %q: %s", not, f)
		}
	}
	// a kept query is a list: every key of [keys] acts, the letters too (n / N are not among them)
	if strings.Contains(footerOf(rtype(t, m, "x").View()), "next") {
		t.Error("no next / prev in the input's footer")
	}
	m.locked = true
	f = footerOf(m.View())
	for _, want := range []string{"j resume", "/ edit", "f filter", "s scope:all", "y copy", "o open", "esc clear"} {
		if !strings.Contains(f, want) {
			t.Errorf("locked footer lacks %q: %s", want, f)
		}
	}
	if strings.Contains(f, "next") {
		t.Errorf("n / N are not keys of the list: %s", f)
	}
	m.locked = false
	// the panel is not text: its letters are the keys, and they are named
	m.panel = true
	if f := footerOf(m.View()); !strings.Contains(f, "space tick") {
		t.Errorf("panel footer: %s", f)
	}
}

func TestRecallChinese(t *testing.T) {
	zh(t)
	w := newRWorld(t)
	w.fill()
	m := w.model(store.Session{SessionID: "s-cart", TmuxPane: "%5"})
	v := rview(m)
	for _, want := range []string{"enter 保留查询", "ctrl+f 筛选", "ctrl+t 范围:全部", "ctrl+y 复制", "ctrl+o 打开", "esc 退出"} {
		if !strings.Contains(v, want) {
			t.Errorf("footer lacks %q:\n%s", want, footerOf(m.View()))
		}
	}
	if m, _ = rpress(t, m, "enter", "enter"); !strings.Contains(rview(m), "cart-fix 在跑（%5）：到看板里跳过去") {
		t.Errorf("running:\n%s", rview(m))
	}
	m, _ = rpress(t, m, "ctrl+f")
	if v := rview(m); !strings.Contains(v, "来源") || !strings.Contains(v, "范围") || !strings.Contains(v, "(•) 全部历史") || !strings.Contains(v, "( ) 当前项目") || !strings.Contains(v, "space 勾选 · a 全选 · A 清空 · esc 收起") {
		t.Errorf("panel:\n%s", v)
	}
	m, _ = rpress(t, m, "esc", "/") // the panel closes; / is the input again
	m = rtype(t, m, "zzzzqqq")
	if v := rview(m); !strings.Contains(v, "没有匹配的会话") || !strings.Contains(footerOf(m.View()), "esc 清除") {
		t.Errorf("empty:\n%s", v)
	}
	if i18n.T("status.working") == "working" {
		t.Error("zh is on")
	}
}

// The columns: the name up to 28 characters, the branch and project dropped before the snippet is squeezed away.
func TestRecallColumns(t *testing.T) {
	w := newRWorld(t)
	w.claude("s-long", w.dir("shopcart"), strings.Repeat("n", 40), time.Hour, "u: the snippet of the session with a long name")
	m := w.model()
	for _, width := range []int{140, 100, 80, 60, 40} {
		tm, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		for i, l := range strings.Split(rview(tm.(recallModel)), "\n") {
			if ansi.StringWidth(l) > width {
				t.Errorf("width %d, line %d is %d wide: %q", width, i, ansi.StringWidth(l), l)
			}
		}
	}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 99, Height: 12})
	v := rview(tm.(recallModel))
	if !strings.Contains(v, strings.Repeat("n", 27)+"…") || strings.Contains(v, strings.Repeat("n", 29)) {
		t.Errorf("the name is cut at 28:\n%s", v)
	}
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	if v := rview(tm.(recallModel)); !strings.Contains(v, "the snippet") {
		t.Errorf("narrow: the snippet stays:\n%s", v)
	}
	// a column goes only when the snippet would be left under 14 cells, and the branch before the
	// project: name 10, project 10, branch 10 take 44 + 1 with 14 left for the snippet at 59
	row := []recall.Result{{Entry: &recall.Entry{Project: strings.Repeat("p", 10), Head: recall.Head{Name: strings.Repeat("n", 10), Branch: strings.Repeat("b", 10)}}}}
	for _, c := range []struct{ lw, pw, bw, sw int }{
		{59, 10, 10, 14}, // just enough for all
		{58, 10, 0, 24},  // one cell short: the branch goes, the project stays
		{48, 10, 0, 14},  // just enough without the branch
		{47, 0, 0, 24},   // one short again: the project goes too
	} {
		if _, pw, bw, sw := m.listColumns(row, c.lw); pw != c.pw || bw != c.bw || sw != c.sw {
			t.Errorf("width %d: project %d branch %d snippet %d, want %d %d %d", c.lw, pw, bw, sw, c.pw, c.bw, c.sw)
		}
	}
}

// A window on a line shows its match: the part before it is cut by cells, not by runes, so a long
// Chinese sentence (two cells a character) has the match in what is shown.
func TestRecallWindowCutsByCells(t *testing.T) {
	text := strings.Repeat("字", 56) + "优惠券"
	pos := posIn("优惠券", text)
	if len(pos) == 0 || pos[0] != 56 {
		t.Fatalf("pos %v", pos)
	}
	for _, width := range []int{40, 30, 61} {
		got, moved := window(text, pos, width)
		if ansi.StringWidth(got) > width || !strings.Contains(got, "优惠券") || len(moved) == 0 || []rune(got)[moved[0]] != '优' {
			t.Errorf("width %d: %q (%v)", width, got, moved)
		}
	}
	// ascii is as it was: the match starts a third of the width in
	ascii := strings.Repeat("a", 60) + "coupon"
	if got, _ := window(ascii, posIn("coupon", ascii), 30); !strings.HasPrefix(got, "…"+strings.Repeat("a", 10)+"coupon") {
		t.Errorf("ascii: %q", got)
	}
	// a match past the middle of the width is brought forward even when it would still fit
	mid := strings.Repeat("a", 50) + "coupon" + strings.Repeat("b", 30)
	if got, _ := window(mid, posIn("coupon", mid), 60); !strings.HasPrefix(got, "…") || !strings.Contains(got, "coupon") {
		t.Errorf("middle: %q", got)
	}
}

// The preview scrolls to its end and no further, so one pgup moves it; a tick that brings nothing
// new for the session under the cursor leaves the scroll where it is, a new query takes it back to the top.
func TestRecallPreviewScrollIsBounded(t *testing.T) {
	w := newRWorld(t)
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf("u: sentence about bounds %d", i))
	}
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, lines...)
	m := rtype(t, w.model(), "bounds")
	for range 60 {
		m, _ = rpress(t, m, "pgdown")
	}
	r, _ := m.selected()
	max := m.previewScrollMax(r)
	box := strings.Split(rview(m), "\n")
	for i, l := range box {
		if i > 3 && strings.Contains(l, "┗") && !strings.Contains(box[i-1], "bounds 29") {
			t.Errorf("at the end of the scroll the last sentence is the last row of the box, not %q", box[i-1])
		}
	}
	if max <= 0 || m.pvScroll != max {
		t.Fatalf("scroll %d, the most is %d", m.pvScroll, max)
	}
	if m, _ = rpress(t, m, "pgup"); m.pvScroll != max-3 {
		t.Errorf("one pgup from the end: %d, want %d", m.pvScroll, max-3)
	}
	tm, _ := m.Update(recallTickMsg{})
	if m = tm.(recallModel); m.pvScroll != max-3 {
		t.Errorf("a tick: %d", m.pvScroll)
	}
	w.claude("s-new", w.dir("shopcart"), "fresh", 10*time.Minute, "u: another session about bounds")
	w.ix.Update()
	tm, _ = m.Update(recallTickMsg{})
	if m = tm.(recallModel); m.pvScroll != max-3 {
		t.Errorf("a tick with a new session in the list: the cursor stays on its session, and so does the scroll: %d", m.pvScroll)
	}
	if m = rtype(t, m, " x"); m.pvScroll != 0 {
		t.Errorf("a new query: %d", m.pvScroll)
	}
	// no preview, nothing to scroll
	tm, _ = w.model().Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if n, _ := rpress(t, tm.(recallModel), "pgdown"); n.pvScroll != 0 {
		t.Errorf("narrow: %d", n.pvScroll)
	}
}

// A session with no id is not a running session with no id.
func TestRecallLiveNeedsAnID(t *testing.T) {
	w := newRWorld(t)
	m := w.model()
	e := &recall.Entry{Head: recall.Head{Path: "/x/y.jsonl"}}
	m.live = []store.Session{{SessionID: "", Transcript: ""}}
	if _, ok := m.liveOf(e); ok {
		t.Error("two empty ids are not a match")
	}
	m.live = []store.Session{{SessionID: "", Transcript: "/x/y.jsonl"}}
	if _, ok := m.liveOf(e); !ok {
		t.Error("a pi session is found by its transcript")
	}
}

// Single letters are text, whatever they are in [keys]: j and k type, they do not move the cursor.
func TestRecallLettersTypeNotMove(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m, _ = rpress(t, m, "down")
	m, _ = rpress(t, m, "j", "k", "q", "a", "A", "?")
	if m.query != "jkqaA?" || m.sel != 0 {
		t.Errorf("query %q, cursor %d (typing goes back to the best match)", m.query, m.sel)
	}
	// an alt+letter is a key, not the letter
	tm, _ := w.model().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	if tm.(recallModel).query != "" {
		t.Errorf("alt+x typed %q", tm.(recallModel).query)
	}
	// a paste's control characters are not kept
	if m = rtype(t, w.model(), "a\tb\x1b[31m\nc"); strings.ContainsAny(m.query, "\t\n\x1b") {
		t.Errorf("query %q", m.query)
	}
}

// What the user did before is not what they type next: a new query starts at the best match even when
// the session the cursor was on is still in the list; a scope or panel change keeps the session.
func TestRecallCursorFollowsOnlyWhenAsked(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m, _ = rpress(t, m, "down", "down", "down") // s-gone
	if r, _ := m.selected(); r.ID != "s-gone" {
		t.Fatal(r.ID)
	}
	m = rtype(t, m, "e")
	if m.sel != 0 {
		t.Errorf("typing: the cursor goes to the best match, not %d", m.sel)
	}
	m = w.model()
	m, _ = rpress(t, m, "down", "ctrl+f", "down", "space") // s-ledger / p-api: untick pi
	if r, _ := m.selected(); r.ID == "" {
		t.Error("nothing selected")
	}
}

// Moving the cursor past the last row shown scrolls the list; the cursor row is always shown.
func TestRecallListScrolls(t *testing.T) {
	w := newRWorld(t)
	for i := range 12 {
		w.claude(fmt.Sprintf("s%02d", i), w.dir("shopcart"), fmt.Sprintf("session-%02d", i), time.Duration(i+1)*time.Minute, fmt.Sprintf("u: sentence %d", i))
	}
	m := w.model()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 12}) // 5 rows in the list's box
	m = tm.(recallModel)
	for range 9 {
		m, _ = rpress(t, m, "down")
	}
	v := rview(m)
	if !strings.Contains(v, "▌○ session-09") || strings.Contains(v, "session-00") || m.top != 5 {
		t.Errorf("top %d:\n%s", m.top, v)
	}
	for range 20 {
		m, _ = rpress(t, m, "down")
	}
	if m.sel != 11 || !strings.Contains(rview(m), "▌○ session-11") {
		t.Errorf("the end: %d", m.sel)
	}
	for range 30 {
		m, _ = rpress(t, m, "up")
	}
	if m.sel != 0 || m.top != 0 || !strings.Contains(rview(m), "▌○ session-00") {
		t.Errorf("the start: %d top %d", m.sel, m.top)
	}
	m, _ = rpress(t, m, "ctrl+d")
	if m.sel != 2 { // half of the 5 rows
		t.Errorf("ctrl+d: %d", m.sel)
	}
	if m, _ = rpress(t, m, "ctrl+u"); m.sel != 0 {
		t.Errorf("ctrl+u: %d", m.sel)
	}
}

// In the panel: the cursor stays inside it, space and enter tick a source and tick it again, and the
// quit keys other than esc still close it.
func TestRecallPanelCursorAndToggle(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m, _ = rpress(t, m, "ctrl+f", "up", "up")
	if m.pcur != 0 {
		t.Errorf("up stops at the top: %d", m.pcur)
	}
	m, _ = rpress(t, m, "space") // claude off
	if !m.unticked["claude"] {
		t.Fatal("space ticks off")
	}
	m, _ = rpress(t, m, "space") // and on
	if m.unticked["claude"] || len(m.results) != 4 {
		t.Errorf("space again ticks it: %v, %d results", m.unticked, len(m.results))
	}
	m, _ = rpress(t, m, "enter")
	if !m.unticked["claude"] {
		t.Error("enter ticks as space does")
	}
	for range 9 {
		m, _ = rpress(t, m, "down")
	}
	if m.pcur != 3 {
		t.Errorf("down stops at the last row: %d", m.pcur)
	}
	c, _ := config.Parse("cfg", []byte("[keys]\nquit = [\"q\", \"ctrl+q\"]\n"))
	m.cfg = c
	if m, _ = rpress(t, m, "ctrl+q"); m.panel {
		t.Error("a quit key closes the panel")
	}
	if _, quit := rpress(t, m, "ctrl+q"); !quit {
		t.Error("and ends the screen when there is nothing to close")
	}
}

// A message goes at the next key.
func TestRecallMessageGoesAtTheNextKey(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	m, _ = rpress(t, rtype(t, m, "legacy"), "enter", "enter")
	if !strings.Contains(rview(m), "directory is gone") {
		t.Fatal("no message")
	}
	if m, _ = rpress(t, m, "down"); strings.Contains(rview(m), "directory is gone") {
		t.Errorf("still there:\n%s", rview(m))
	}
}

// A project the sessions have no word of is found by name, with no hit to show: the count is a dash.
func TestRecallByNameShowsNoHits(t *testing.T) {
	w := newRWorld(t)
	w.claude("s-pv", w.dir("shopcart"), "pv", time.Hour, "u: nothing here about it")
	m := rtype(t, w.model(), "'pv")
	if row := strings.Split(rview(m), "\n")[4]; !strings.Contains(row, "  - nothing here") {
		t.Errorf("row %q", row)
	}
}

// A snippet longer than its column is cut around the match, not at the start.
func TestRecallSnippetShowsTheMatch(t *testing.T) {
	w := newRWorld(t)
	w.claude("s-long", w.dir("shopcart"), "long", time.Hour, "u: "+strings.Repeat("padding ", 12)+"the needle is here and then "+strings.Repeat("more ", 20))
	m := rtype(t, w.model(), "'needle")
	if row := strings.Split(rview(m), "\n")[4]; !strings.Contains(row, "needle") || !strings.Contains(row, "…") {
		t.Errorf("row %q", row)
	}
}

// What the colors say: the agent's sentence in the list is muted, yours is not.
func TestRecallSnippetColorByRole(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	count := func(line string) int {
		w := newRWorld(t)
		w.claude("s", w.dir("shopcart"), "x", time.Hour, line)
		return strings.Count(strings.Split(w.model().View(), "\n")[4], "38;2;97;113;163")
	}
	if you, agent := count("u: a sentence that you said"), count("a: a sentence that the agent said"); agent != you+1 {
		t.Errorf("muted spans: yours %d, the agent's %d", you, agent)
	}
}

// Each gap is a note with the number of sentences it stands for, before the first hit and after the last.
func TestRecallPreviewGaps(t *testing.T) {
	w := newRWorld(t)
	var lines []string
	for i := range 12 {
		role := "u: "
		if i%2 == 1 {
			role = "a: "
		}
		lines = append(lines, fmt.Sprintf("%ssentence number %d", role, i))
	}
	lines[4] = "u: here is the needle"
	lines[8] = "u: and the needle again"
	w.claude("s-pv", w.dir("shopcart"), "pv", time.Hour, lines...)
	v := rview(rtype(t, w.model(), "'needle"))
	at := func(s string) int { return strings.Index(v, s) }
	before, between, after := at("… 3 lines …"), at("… 1 lines …"), at("… 2 lines …")
	if before < 0 || between < 0 || after < 0 || !(before < at("sentence number 3") && at("sentence number 5") < between && between < at("sentence number 7") && at("sentence number 9") < after) {
		t.Errorf("gaps (3 before the first hit, 1 between, 2 after the last) at %d %d %d:\n%s", before, between, after, v)
	}
	if strings.Count(v, " lines …") != 3 {
		t.Errorf("three notes:\n%s", v)
	}
	// no query: the last six
	var ten []string
	for i := range 10 {
		ten = append(ten, fmt.Sprintf("u: line number %d", i))
	}
	w2 := newRWorld(t)
	w2.claude("s-ten", w2.dir("shopcart"), "ten", time.Hour, ten...)
	v = rview(w2.model())
	if !strings.Contains(v, "line number 4") || strings.Contains(v, "line number 3") || !strings.Contains(v, "line number 9") {
		t.Errorf("the last six:\n%s", v)
	}
}

// The preview is built once per session, query and width: the same screen again does not search the
// sentences again, and a query, a session or a size that changes does.
func TestRecallPreviewIsCached(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	m := w.model()
	r, _ := m.selected()
	m.previewContent(r, 40)
	key := m.pvCache.key
	if key == "" {
		t.Fatal("not cached")
	}
	fake := func() { m.pvCache = &pvCache{key: key, lines: []string{"from the cache"}} }
	fake()
	if got := m.previewContent(r, 40); len(got) != 1 || got[0] != "from the cache" {
		t.Errorf("the same key is the cache: %q", got)
	}
	changed := map[string]func(*recallModel) (recall.Result, int){
		"width":   func(*recallModel) (recall.Result, int) { return r, 41 },
		"query":   func(m *recallModel) (recall.Result, int) { m.query = "coupon"; return r, 40 },
		"session": func(m *recallModel) (recall.Result, int) { return m.results[1], 40 },
		"path": func(m *recallModel) (recall.Result, int) {
			o := r
			o.Entry = &recall.Entry{Head: recall.Head{ID: r.ID, CWD: r.CWD, Path: r.Path + "x"}, MTime: r.MTime}
			return o, 40
		},
		"mtime": func(m *recallModel) (recall.Result, int) {
			o := r
			o.Entry = &recall.Entry{Head: r.Head, MTime: r.MTime + 1}
			return o, 40
		},
		"minute": func(m *recallModel) (recall.Result, int) {
			m.now = func() time.Time { return w.clock.Add(time.Hour) }
			return r, 40
		},
	}
	for name, change := range changed {
		fake() // each change starts from the cache that was there
		mm := m
		rr, width := change(&mm)
		if got := mm.previewContent(rr, width); len(got) == 1 && got[0] == "from the cache" {
			t.Errorf("%s changed and the cache was used", name)
		}
	}
}

// The search runs off the update loop: what it found for a query that has been typed over by the time
// it comes back is not shown, in whatever order the answers come; and enter does not act on a list
// that is not the query's yet.
func TestRecallAnswersOfAnOlderQueryAreDropped(t *testing.T) {
	w := newRWorld(t)
	w.fill()
	letters := func(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	for _, newerFirst := range []bool{true, false} {
		tm, c1 := w.model().Update(letters("l"))
		tm, c2 := tm.Update(letters("egacy"))
		if got := rids(tm.(recallModel)); got != "s-cart p-api s-ledger s-gone" {
			t.Fatalf("until an answer comes the list is the old one: %q", got)
		}
		if m, quit := rpress(t, tm.(recallModel), "enter"); quit || m.res != nil || m.msg != "" {
			t.Errorf("enter on a list that is not the query's: quit %v res %v msg %q", quit, m.res, m.msg)
		}
		older, newer := c1().(recallFoundMsg), c2().(recallFoundMsg)
		if len(older.results) < 2 {
			t.Fatalf("the older query found %d: the test needs it to differ", len(older.results))
		}
		for _, msg := range map[bool][]recallFoundMsg{true: {newer, older}, false: {older, newer}}[newerFirst] {
			tm, _ = tm.Update(msg)
		}
		if m := tm.(recallModel); rids(m) != "s-gone" || m.searching {
			t.Errorf("newer first %v: results %q searching %v", newerFirst, rids(m), m.searching)
		}
	}
}
