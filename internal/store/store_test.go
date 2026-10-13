package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
)

func fixture(t *testing.T) []ccmux.Session {
	t.Helper()
	data, err := os.ReadFile("../../testdata/show.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ccmux.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i].LogPath = "" // fixture points at real transcripts; keep the test hermetic
	}
	return raw
}

func poller(t *testing.T, raw []ccmux.Session) *Poller {
	p := &Poller{Show: func() ([]ccmux.Session, error) { return raw, nil }, StateDir: t.TempDir()}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	return p
}

func byID(t *testing.T, ss []Session, id string) Session {
	t.Helper()
	for _, s := range ss {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s not found", id)
	return Session{}
}

func TestFixture(t *testing.T) {
	raw := fixture(t)
	ss := poller(t, raw).Sessions()
	if len(ss) != len(raw) || len(ss) == 0 {
		t.Fatalf("got %d sessions, want %d", len(ss), len(raw))
	}
	// summary null -> project
	if s := byID(t, ss, "6a31a6fb"); s.Name != "sqlmux" || s.TmuxPane != "" {
		t.Errorf("null summary: name=%q pane=%q", s.Name, s.TmuxPane)
	}
	s := byID(t, ss, "f566a75d-e244-4fcd-9c61-bf8d97a8bcf6")
	if s.Name != "rename-flag" || s.GroupKey != s.CWD || s.GroupKey == "" || s.PendingTool != "Bash" || s.LastActivity.IsZero() {
		t.Errorf("summary set: %+v", s)
	}
	// real captures: idle + unread = done; idle + unread + a working subagent = working · agents
	if s := byID(t, ss, "bd8201ec-e255-4d0d-ba88-7413f3c10329"); !s.Done || s.Status != "idle" {
		t.Errorf("sqlmux-test-engineer: done=%v status=%s, want done", s.Done, s.Status)
	}
	if s := byID(t, ss, "c8cbecee-e813-4fe4-b7ed-b971ef4878bc"); s.Done || s.Status != "working" || !s.AgentsWorking {
		t.Errorf("agmux-orchestrator: done=%v status=%s agents=%v, want working · agents", s.Done, s.Status, s.AgentsWorking)
	}
	for _, s := range ss {
		if s.ContextPct != -1 {
			t.Errorf("%s: ContextPct=%d with no HUD file, want -1", s.ID, s.ContextPct)
		}
	}
}

func TestHUD(t *testing.T) {
	raw := fixture(t)[:1]
	p := &Poller{Show: func() ([]ccmux.Session, error) { return raw, nil }, StateDir: t.TempDir()}
	hud := filepath.Join(p.StateDir, raw[0].NativeSessionID+".json")
	for _, c := range []struct {
		body   string
		want   int
		model  string
		effort string
	}{
		// rate_limits first: an implementation grabbing the first used_percentage gets 99
		{`{"rate_limits":{"five_hour":{"used_percentage":99}},"model":{"display_name":"Opus 5.5"},"context_window":{"used_percentage":41.6,"remaining_percentage":58.4},"effort":{"level":"high"}}`, 41, "Opus 5.5", "high"},
		{`{"context_window":{"used_percentage":29.6}}`, 29, "", ""}, // truncated, like claude-hud
		{`{"context_window":{"used_percentage":0}}`, 0, "", ""},
		{`{"context_window":{"used_percentage":null},"model":{"display_name":"Sonnet 5.5"}}`, -1, "Sonnet 5.5", ""},
		// a wrongly typed field must not take the good ones down with it
		{`{"context_window":{"used_percentage":41.6},"model":{"display_name":"Opus 5.5"},"effort":{"level":3}}`, 41, "Opus 5.5", ""},
		{`{"context_window":{"used_percentage":41.6},"model":{"display_name":"Opus 5.5"},"effort":"high"}`, 41, "Opus 5.5", ""},
		{`{"context_window":`, -1, "", ""}, // half-written: treat as missing
	} {
		if err := os.WriteFile(hud, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		p.Refresh()
		if s := p.Sessions()[0]; s.ContextPct != c.want || s.Model != c.model || s.Effort != c.effort {
			t.Errorf("%s: ContextPct=%d Model=%q Effort=%q, want %d %q %q", c.body, s.ContextPct, s.Model, s.Effort, c.want, c.model, c.effort)
		}
	}
}

// The status line's model name wins over the transcript's model ID; without it the ID stays.
func TestHUDModelBeatsJSONL(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(log, []byte(`{"type":"assistant","message":{"model":"claude-opus-5-5"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Poller{Show: func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a", NativeSessionID: "n1", LogPath: log}}, nil
	}, StateDir: dir}
	p.Refresh()
	if m := p.Sessions()[0].Model; m != "claude-opus-5-5" {
		t.Fatalf("no HUD file: %q", m)
	}
	if err := os.WriteFile(filepath.Join(dir, "n1.json"), []byte(`{"model":{"display_name":"Opus 5.5 (1M context)"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p.Refresh()
	if m := p.Sessions()[0].Model; m != "Opus 5.5 (1M context)" {
		t.Errorf("with HUD: %q", m)
	}
}

func TestLastModel(t *testing.T) {
	dir := t.TempDir()
	write := func(lines ...string) string {
		f := filepath.Join(dir, "s.jsonl")
		if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return f
	}
	asst := func(m string) string { return `{"type":"assistant","message":{"model":"` + m + `"}}` }
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"last wins", []string{asst("claude-sonnet-4-5"), `{"type":"user"}`, asst("claude-opus-5-5"), `{"type":"user"}`}, "claude-opus-5-5"},
		{"skip synthetic", []string{asst("claude-opus-5-5"), asst("<synthetic>")}, "claude-opus-5-5"},
		{"substring in tool output", []string{asst("claude-opus-5-5"), `{"type":"user","content":"{\"type\":\"assistant\"}"}`}, "claude-opus-5-5"},
		{"none", []string{`{"type":"user"}`}, ""},
		// the old assistant line falls outside the 64KB tail
		{"only tail read", []string{asst("old"), `{"type":"user","pad":"` + strings.Repeat("x", tailBytes) + `"}`}, ""},
		// the window starts exactly at the assistant line: it is whole, so it must count
		{"cut on line start", []string{`{"type":"user"}`, asst("edge"), `{"type":"user","pad":"` + strings.Repeat("x", tailBytes-len(asst("edge"))-1-len(`{"type":"user","pad":""}`)-1) + `"}`}, "edge"},
		{"cut first line ignored", []string{`{"type":"user","pad":"` + strings.Repeat("x", tailBytes) + `"}`, asst("new")}, "new"},
	}
	for _, c := range cases {
		if got := lastModel(write(c.lines...)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if got := lastModel(filepath.Join(dir, "missing.jsonl")); got != "" {
		t.Errorf("missing file: %q", got)
	}
}

func TestRefreshErrorKeepsSessions(t *testing.T) {
	p := poller(t, fixture(t))
	n := len(p.Sessions())
	p.Show = func() ([]ccmux.Session, error) { return nil, os.ErrDeadlineExceeded }
	if p.Refresh() == nil || p.Err() == nil || len(p.Sessions()) != n {
		t.Fatalf("failed poll should keep %d sessions and expose the error", n)
	}
}

func TestModelCache(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s.jsonl")
	raw := []ccmux.Session{{ID: "a", LogPath: log}}
	p := &Poller{Show: func() ([]ccmux.Session, error) { return raw, nil }, StateDir: dir}
	write := func(body string) {
		if err := os.WriteFile(log, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"type":"user"}` + "\n")
	p.Refresh()
	if m := p.Sessions()[0].Model; m != "" {
		t.Fatalf("first poll with no assistant: %q", m)
	}
	write(`{"type":"assistant","message":{"model":"claude-opus-5-5"}}` + "\n")
	p.Refresh()
	write(`{"type":"user","pad":"` + strings.Repeat("x", tailBytes) + `"}` + "\n")
	p.Refresh()
	if m := p.Sessions()[0].Model; m != "claude-opus-5-5" {
		t.Fatalf("assistant scrolled out of the tail: got %q, want the cached model", m)
	}
}

// Effective status follows ccmux 1.4.3 getEffectiveStatus.
func TestEffectiveStatus(t *testing.T) {
	sub := func(st string) []ccmux.Subagent { return []ccmux.Subagent{{AgentID: "x", Status: st}} }
	raw := []ccmux.Session{
		{ID: "done-unread", Status: "idle", AttentionState: "unread"},
		{ID: "done-read", Status: "idle", AttentionState: "read"},
		{ID: "idle", Status: "idle"},
		{ID: "idle+agent working", Status: "idle", AttentionState: "unread", Subagents: sub("working")},
		{ID: "idle+agent waiting", Status: "idle", Subagents: sub("waiting")},
		{ID: "idle+agent finished", Status: "idle", AttentionState: "unread", Subagents: sub("idle")},
		{ID: "working+agent", Status: "working", Subagents: sub("working")},
		{ID: "waiting", Status: "waiting", AttentionState: "unread"},
	}
	want := map[string]struct {
		status       string
		agents, done bool
	}{
		"done-unread":         {"idle", false, true},
		"done-read":           {"idle", false, true},
		"idle":                {"idle", false, false},
		"idle+agent working":  {"working", true, false},
		"idle+agent waiting":  {"working", true, false},
		"idle+agent finished": {"idle", false, true},
		"working+agent":       {"working", false, false},
		"waiting":             {"waiting", false, false},
	}
	for _, s := range poller(t, raw).Sessions() {
		w := want[s.ID]
		if s.Status != w.status || s.AgentsWorking != w.agents || s.Done != w.done {
			t.Errorf("%s: status %s agents %v done %v, want %s %v %v", s.ID, s.Status, s.AgentsWorking, s.Done, w.status, w.agents, w.done)
		}
	}
}

func TestCorruptAvatarsShownNotFatal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "agmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agmux", "avatars.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewPoller()
	if err != nil {
		t.Fatalf("corrupt avatars.json must not stop the board: %v", err)
	}
	p.Show = func() ([]ccmux.Session, error) { return []ccmux.Session{{ID: "a"}}, nil }
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if e := p.Err(); e == nil || !strings.Contains(e.Error(), "avatars.json") {
		t.Errorf("Err() = %v, want the avatars.json problem", e)
	}
	if p.Sessions()[0].AvatarID == "" {
		t.Error("sessions still get hash-default avatars")
	}
}

// Avatars follow the session's name (its ID only when it has none): state file > config > default.
func TestAvatarsByName(t *testing.T) {
	dir := t.TempDir()
	b, _ := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	b.Set("orch", "bot-01")
	p := &Poller{StateDir: dir, Avatars: b, AvatarDefault: "bot-06",
		AvatarBind: map[string]string{"orch": "bot-02", "dev": "bot-03", "id-3": "bot-04"},
		Show: func() ([]ccmux.Session, error) {
			return []ccmux.Session{{ID: "id-1", Summary: "orch"}, {ID: "id-2", Summary: "dev"}, {ID: "id-3"}, {ID: "id-4", Project: "proj"}}, nil
		}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	ss := p.Sessions()
	for id, want := range map[string]string{"id-1": "bot-01", "id-2": "bot-03", "id-3": "bot-04", "id-4": "bot-06"} {
		if got := byID(t, ss, id).AvatarID; got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
	// the board binds: file written by name, the sessions show it without waiting for a poll
	if err := p.SetAvatar(byID(t, ss, "id-2"), "bot-05"); err != nil {
		t.Fatal(err)
	}
	if got := byID(t, p.Sessions(), "id-2").AvatarID; got != "bot-05" || byID(t, ss, "id-2").AvatarID != "bot-03" {
		t.Errorf("after SetAvatar: %s; the slice handed out before must not change", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "avatars.json")); !strings.Contains(string(data), `"dev": "bot-05"`) {
		t.Errorf("avatars.json: %s", data)
	}
	// no summary, same project: one name, one binding; both cards change at once
	p.Show = func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "p1", Project: "proj"}, {ID: "p2", Project: "proj"}, {ID: "id-3"}}, nil
	}
	p.Refresh()
	if err := p.SetAvatar(byID(t, p.Sessions(), "p1"), "bot-04"); err != nil {
		t.Fatal(err)
	}
	if a, b := byID(t, p.Sessions(), "p1").AvatarID, byID(t, p.Sessions(), "p2").AvatarID; a != "bot-04" || b != "bot-04" {
		t.Errorf("same name: %s %s", a, b)
	}
	p.SetAvatar(byID(t, ss, "id-3"), "bot-02")
	if data, _ := os.ReadFile(filepath.Join(dir, "avatars.json")); !strings.Contains(string(data), `"id-3": "bot-02"`) {
		t.Errorf("no name: bound by ID: %s", data)
	}
}

// A corrupt avatars.json is never overwritten by a binding: the user may still fix it.
func TestSetAvatarKeepsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "agmux", "avatars.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{oops"), 0o644)
	p, _ := NewPoller()
	if err := p.SetAvatar(Session{ID: "a", Name: "n"}, "bot-01"); err == nil || !strings.Contains(err.Error(), "avatars.json") {
		t.Errorf("err %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Errorf("file changed: %s", data)
	}
}

// SetAvatar waits for a poll in flight, or that poll's sessions (built with the old binding) would land after it.
func TestSetAvatarWaitsForPoll(t *testing.T) {
	b, _ := avatar.LoadBindings(filepath.Join(t.TempDir(), "avatars.json"))
	p := &Poller{Avatars: b}
	p.refreshMu.Lock()
	done := make(chan error)
	go func() { done <- p.SetAvatar(Session{ID: "a", Name: "n"}, "bot-01") }()
	select {
	case <-done:
		t.Fatal("SetAvatar ran during a poll")
	case <-time.After(50 * time.Millisecond):
	}
	p.refreshMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// avatars.json is followed on every poll: another board's binding shows, a fixed file stops the error.
func TestPollFollowsAvatarsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "agmux", "avatars.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{oops"), 0o644)
	p, _ := NewPoller()
	p.Show = func() ([]ccmux.Session, error) { return []ccmux.Session{{ID: "a", Summary: "n"}}, nil }
	p.Refresh()
	if p.Err() == nil {
		t.Fatal("broken file: error")
	}
	os.WriteFile(path, []byte(`{"n":"bot-06"}`), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(time.Hour))
	p.Refresh()
	if p.Err() != nil || p.Sessions()[0].AvatarID != "bot-06" {
		t.Errorf("fixed by hand, another board's pick: err %v avatar %s", p.Err(), p.Sessions()[0].AvatarID)
	}
	os.WriteFile(path, []byte("{oops"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(2*time.Hour))
	p.Refresh()
	os.WriteFile(path, []byte(`{}`), 0o644)
	if err := p.SetAvatar(p.Sessions()[0], "bot-01"); err != nil || p.Err() != nil {
		t.Errorf("a bind reads the file now: fixed, no error: %v %v", err, p.Err())
	}
}

// A bind that can't be written says so once (its own error); the file reads fine, so no avatars error.
func TestSetAvatarWriteErrorNotAvatarErr(t *testing.T) {
	dir := t.TempDir()
	b, _ := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	p := &Poller{Avatars: b}
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	if err := p.SetAvatar(Session{ID: "a", Name: "n"}, "bot-01"); err == nil || p.Err() != nil {
		t.Errorf("err %v, Err() %v", err, p.Err())
	}
}

// An image set's ID is as good as a sprite's in a binding, [avatars.bind] or default.
func TestAvatarImageSetIDs(t *testing.T) {
	avatar.UseSets(fstest.MapFS{"kit/idle/1.png": &fstest.MapFile{Data: []byte("x")}})
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}) })
	dir := t.TempDir()
	b, _ := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	b.Set("orch", "kit")
	p := &Poller{StateDir: dir, Avatars: b, AvatarDefault: "kit", AvatarBind: map[string]string{"dev": "kit", "gone": "no-such-set"},
		Show: func() ([]ccmux.Session, error) {
			return []ccmux.Session{{ID: "1", Summary: "orch"}, {ID: "2", Summary: "dev"}, {ID: "3", Summary: "gone"}}, nil
		}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2", "3"} { // "gone": the unknown ID falls through to the default
		if got := byID(t, p.Sessions(), id).AvatarID; got != "kit" {
			t.Errorf("%s: %s, want kit", id, got)
		}
	}
}

// The avatar state an image avatar shows, worked out once per poll: hi beats everything, a working
// session is think / read / write / tool by its pending tool, then success, else idle.
func TestAvatarState(t *testing.T) {
	for _, c := range []struct {
		name string
		r    ccmux.Session
		want string
	}{
		{"idle", ccmux.Session{Status: "idle"}, "idle"},
		{"done", ccmux.Session{Status: "idle", AttentionState: "read"}, "success"},
		{"waiting", ccmux.Session{Status: "waiting"}, "hi"},
		{"attention", ccmux.Session{Status: "working", AttentionType: "permission", PendingTool: "Bash"}, "hi"},
		{"idle with attention", ccmux.Session{Status: "idle", AttentionType: "question", AttentionState: "read"}, "hi"},
		{"working, no tool", ccmux.Session{Status: "working"}, "think"},
		{"working on subagents", ccmux.Session{Status: "idle", Subagents: []ccmux.Subagent{{Status: "working"}}}, "think"},
		{"Read", ccmux.Session{Status: "working", PendingTool: "Read"}, "read"},
		{"Grep", ccmux.Session{Status: "working", PendingTool: "Grep"}, "read"},
		{"Glob", ccmux.Session{Status: "working", PendingTool: "Glob"}, "read"},
		{"WebFetch", ccmux.Session{Status: "working", PendingTool: "WebFetch"}, "read"},
		{"WebSearch", ccmux.Session{Status: "working", PendingTool: "WebSearch"}, "read"},
		{"Write", ccmux.Session{Status: "working", PendingTool: "Write"}, "write"},
		{"Edit", ccmux.Session{Status: "working", PendingTool: "Edit"}, "write"},
		{"MultiEdit", ccmux.Session{Status: "working", PendingTool: "MultiEdit"}, "write"},
		{"NotebookEdit", ccmux.Session{Status: "working", PendingTool: "NotebookEdit"}, "write"},
		{"Bash", ccmux.Session{Status: "working", PendingTool: "Bash"}, "tool"},
		{"an MCP tool", ccmux.Session{Status: "working", PendingTool: "mcp__x__y"}, "tool"},
		{"background", ccmux.Session{Status: "background"}, "idle"},
	} {
		c.r.ID = "id"
		p := &Poller{StateDir: t.TempDir(), Show: func() ([]ccmux.Session, error) { return []ccmux.Session{c.r}, nil }}
		if err := p.Refresh(); err != nil {
			t.Fatal(err)
		}
		if got := p.Sessions()[0].AvatarState; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
