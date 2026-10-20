package store

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kackerx/agmux/internal/avatar"
)

func fixture(t *testing.T) []Raw {
	t.Helper()
	data, err := os.ReadFile("../../testdata/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw []Raw
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func poller(t *testing.T, raw []Raw) *Poller {
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return raw, nil })}, StateDir: t.TempDir()}
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
	// real captures: idle since a known time = done; a session working is not
	if s := byID(t, ss, "bd8201ec-e255-4d0d-ba88-7413f3c10329"); !s.Done || s.Status != "idle" {
		t.Errorf("sqlmux-test-engineer: done=%v status=%s, want done", s.Done, s.Status)
	}
	if s := byID(t, ss, "c8cbecee-e813-4fe4-b7ed-b971ef4878bc"); s.Done || s.Status != "working" {
		t.Errorf("agmux-orchestrator: done=%v status=%s, want working", s.Done, s.Status)
	}
	for _, s := range ss {
		if s.ContextPct != -1 {
			t.Errorf("%s: ContextPct=%d with no HUD file, want -1", s.ID, s.ContextPct)
		}
	}
}

func TestHUD(t *testing.T) {
	raw := fixture(t)[:1]
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return raw, nil })}, StateDir: t.TempDir()}
	hud := filepath.Join(p.StateDir, raw[0].SessionID+".json")
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
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) {
		return []Raw{{ID: "a", SessionID: "n1", Transcript: log}}, nil
	})}, StateDir: dir}
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
	p.Sources = []Source{SourceFunc(func() ([]Raw, error) { return nil, os.ErrDeadlineExceeded })}
	if p.Refresh() == nil || p.Err() == nil || len(p.Sessions()) != n {
		t.Fatalf("failed poll should keep %d sessions and expose the error", n)
	}
}

func TestModelCache(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s.jsonl")
	raw := []Raw{{ID: "a", Transcript: log}}
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return raw, nil })}, StateDir: dir}
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

// The status is the source's; an idle one is done unless its time is not known or the user has seen it.
func TestStatusAndDone(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	raw := []Raw{
		{ID: "idle", Status: "idle", StatusChangedAt: at},
		{ID: "idle, no time", Status: "idle"},
		{ID: "working", Status: "working", StatusChangedAt: at},
		{ID: "waiting", Status: "waiting", StatusChangedAt: at},
	}
	want := map[string]struct {
		status string
		done   bool
	}{"idle": {"idle", true}, "idle, no time": {"idle", false}, "working": {"working", false}, "waiting": {"waiting", false}}
	for _, s := range poller(t, raw).Sessions() {
		w := want[s.ID]
		if s.Status != w.status || s.Done != w.done || s.AgentsWorking {
			t.Errorf("%s: status %s done %v agents %v, want %s %v", s.ID, s.Status, s.Done, s.AgentsWorking, w.status, w.done)
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
	p.Sources = []Source{SourceFunc(func() ([]Raw, error) { return []Raw{{ID: "a"}}, nil })}
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
	p := &Poller{StateDir: dir, Avatars: b, AvatarDefault: "bot-05",
		AvatarBind: map[string]string{"orch": "bot-02", "dev": "bot-03", "id-3": "bot-04"},
		Sources: []Source{SourceFunc(func() ([]Raw, error) {
			return []Raw{{ID: "id-1", Name: "orch"}, {ID: "id-2", Name: "dev"}, {ID: "id-3"}, {ID: "id-4", Project: "proj"}}, nil
		})}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	ss := p.Sessions()
	for id, want := range map[string]string{"id-1": "bot-01", "id-2": "bot-03", "id-3": "bot-04", "id-4": "bot-05"} {
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
	p.Sources = []Source{SourceFunc(func() ([]Raw, error) {
		return []Raw{{ID: "p1", Project: "proj"}, {ID: "p2", Project: "proj"}, {ID: "id-3"}}, nil
	})}
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
	p.Sources = []Source{SourceFunc(func() ([]Raw, error) { return []Raw{{ID: "a", Name: "n"}}, nil })}
	p.Refresh()
	if p.Err() == nil {
		t.Fatal("broken file: error")
	}
	os.WriteFile(path, []byte(`{"n":"bot-05"}`), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(time.Hour))
	p.Refresh()
	if p.Err() != nil || p.Sessions()[0].AvatarID != "bot-05" {
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
	avatar.UseSets(fstest.MapFS{"kit/idle/1.png": &fstest.MapFile{Data: pngOf("x")}})
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}) })
	dir := t.TempDir()
	b, _ := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	b.Set("orch", "kit")
	p := &Poller{StateDir: dir, Avatars: b, AvatarDefault: "kit", AvatarBind: map[string]string{"dev": "kit", "gone": "no-such-set"},
		Sources: []Source{SourceFunc(func() ([]Raw, error) {
			return []Raw{{ID: "1", Name: "orch"}, {ID: "2", Name: "dev"}, {ID: "3", Name: "gone"}}, nil
		})}}
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
		r    Raw
		want string
	}{
		{"idle", Raw{Status: "idle"}, "idle"},
		{"done", Raw{Status: "idle", StatusChangedAt: time.Unix(1, 0)}, "success"},
		{"waiting", Raw{Status: "waiting"}, "hi"},
		{"attention", Raw{Status: "working", Attention: "permission", PendingTool: "Bash"}, "hi"},
		{"idle with attention", Raw{Status: "idle", Attention: "question", StatusChangedAt: time.Unix(1, 0)}, "hi"},
		{"working, no tool", Raw{Status: "working"}, "think"},
		{"Read", Raw{Status: "working", PendingTool: "Read"}, "read"},
		{"Grep", Raw{Status: "working", PendingTool: "Grep"}, "read"},
		{"Glob", Raw{Status: "working", PendingTool: "Glob"}, "read"},
		{"WebFetch", Raw{Status: "working", PendingTool: "WebFetch"}, "read"},
		{"WebSearch", Raw{Status: "working", PendingTool: "WebSearch"}, "read"},
		{"Write", Raw{Status: "working", PendingTool: "Write"}, "write"},
		{"Edit", Raw{Status: "working", PendingTool: "Edit"}, "write"},
		{"MultiEdit", Raw{Status: "working", PendingTool: "MultiEdit"}, "write"},
		{"NotebookEdit", Raw{Status: "working", PendingTool: "NotebookEdit"}, "write"},
		{"Bash", Raw{Status: "working", PendingTool: "Bash"}, "tool"},
		{"an MCP tool", Raw{Status: "working", PendingTool: "mcp__x__y"}, "tool"},
		{"background", Raw{Status: "background"}, "idle"},
	} {
		c.r.ID = "id"
		p := &Poller{StateDir: t.TempDir(), Sources: []Source{SourceFunc(func() ([]Raw, error) { return []Raw{c.r}, nil })}}
		if err := p.Refresh(); err != nil {
			t.Fatal(err)
		}
		if got := p.Sessions()[0].AvatarState; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// pngOf is a small valid PNG, a different one for every label.
func pngOf(label string) []byte {
	h := fnv.New32a()
	h.Write([]byte(label))
	im := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(im, im.Bounds(), &image.Uniform{color.NRGBA{uint8(h.Sum32()), uint8(h.Sum32() >> 8), uint8(h.Sum32() >> 16), 255}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}

func TestAgentSetting(t *testing.T) {
	as := func(n string) string { return `{"type":"agent-setting","agentSetting":"` + n + `","sessionId":"x"}` }
	pad := `{"type":"user","pad":"` + strings.Repeat("x", headBytes) + `"}`
	for _, c := range []struct {
		name  string
		body  string
		want  string
		final bool
	}{
		{"first line", as("orchestrator") + "\n" + `{"type":"user"}` + "\n", "orchestrator", true},
		{"none, a short file: may still come", `{"type":"user"}` + "\n", "", false},
		{"empty file", "", "", false},
		{"none, a long file: the line is not coming", pad + "\n", "", true},
		{"only after the first 64KB: not looked for", pad + "\n" + as("late") + "\n", "", true},
		{"resume writes it again: the first one", as("a") + "\n" + as("b") + "\n", "a", true},
		{"text that only mentions it", `{"type":"user","note":"agent-setting","agentSetting":"fake"}` + "\n" + as("real") + "\n", "real", true},
		{"no value is no agent", `{"type":"agent-setting","agentSetting":""}` + "\n" + as("next") + "\n", "next", true},
		{"a line still being written", as("whole") + "\n" + `{"type":"agent-setting","agentSetting":"par`, "whole", true},
		{"not finished and alone", `{"type":"agent-setting","agentSetting":"par`, "", false},
	} {
		path := filepath.Join(t.TempDir(), "s.jsonl")
		os.WriteFile(path, []byte(c.body), 0o644)
		if got, final := agentSetting(path); got != c.want || final != c.final {
			t.Errorf("%s: %q final %v, want %q %v", c.name, got, final, c.want, c.final)
		}
	}
	if got, final := agentSetting(filepath.Join(t.TempDir(), "missing")); got != "" || final {
		t.Errorf("missing file: %q %v", got, final)
	}
}

func TestAgentIn(t *testing.T) {
	as := func(n string) []byte { return []byte(`{"type":"agent-setting","agentSetting":"` + n + `"}`) }
	for _, c := range []struct {
		name  string
		lines [][]byte
		want  string
	}{
		{"the last one", [][]byte{as("a"), []byte(`{"type":"user"}`), as("b"), []byte(`{"type":"user"}`)}, "b"},
		{"none", [][]byte{[]byte(`{"type":"user"}`), nil}, ""},
		{"nothing read", nil, ""},
		{"empty value: the one before", [][]byte{as("a"), []byte(`{"type":"agent-setting","agentSetting":""}`)}, "a"},
		{"another type that mentions it", [][]byte{as("real"), []byte(`{"type":"user","note":"agent-setting","agentSetting":"fake"}`)}, "real"},
		{"a half written last line", [][]byte{as("whole"), []byte(`{"type":"agent-setting","agentSetting":"par`)}, "whole"},
	} {
		if got := agentIn(c.lines); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Session.Agent: the last agent-setting of the tail, else the first of the head (kept per path);
// the tail is read on every poll, so an agent set after the head (a late --agent resume, a change) shows.
func TestAgentSources(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s.jsonl")
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return []Raw{{ID: "a", Transcript: log}, {ID: "b"}}, nil })}, StateDir: dir}
	as := func(n string) string { return `{"type":"agent-setting","agentSetting":"` + n + `"}` + "\n" }
	pad := `{"type":"user","pad":"` + strings.Repeat("x", tailBytes) + `"}` + "\n" // longer than the tail
	write := func(body string) { os.WriteFile(log, []byte(body), 0o644) }
	agent := func() string { p.Refresh(); return p.Sessions()[0].Agent }
	for _, c := range []struct{ name, body, want string }{
		{"neither", `{"type":"user"}` + "\n", ""},
		{"head only (the file is short: it is the tail too)", as("first") + `{"type":"user"}` + "\n", "first"},
		{"head only, a long file", as("first") + pad + pad, "first"},
		{"the tail's last wins over the head", as("first") + pad + as("second") + `{"type":"user"}` + "\n" + as("third"), "third"},
		{"only in the tail: a late resume with --agent", `{"type":"user"}` + "\n" + pad + pad + as("late"), "late"},
		{"neither, a long file", pad + pad, ""},
	} {
		os.Remove(log)
		p = &Poller{Sources: p.Sources, StateDir: dir}
		write(c.body)
		if got := agent(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// the head is kept per path; the tail is not: a later change shows
	p = &Poller{Sources: p.Sources, StateDir: dir}
	write(as("first") + pad + pad)
	if a := agent(); a != "first" {
		t.Fatalf("head: %q", a)
	}
	write(as("first") + pad + pad + as("changed"))
	if a := agent(); a != "changed" {
		t.Errorf("an agent changed later: %q", a)
	}
	// a short file with none is not final: an agent set at 10KB (a late resume) that the file then
	// grows past is in neither tail but still in the head, and must be found there
	p = &Poller{Sources: p.Sources, StateDir: dir}
	write(`{"type":"user"}` + "\n")
	if a := agent(); a != "" {
		t.Fatalf("none yet: %q", a)
	}
	write(`{"type":"user"}` + "\n" + as("late-resume"))
	if a := agent(); a != "late-resume" {
		t.Fatalf("from the tail: %q", a)
	}
	write(`{"type":"user"}` + "\n" + as("late-resume") + pad + pad + pad)
	if a := agent(); a != "late-resume" {
		t.Errorf("scrolled out of the tail, still in the head: %q", a)
	}
	p = &Poller{Sources: p.Sources, StateDir: dir}
	write(as("first") + pad + pad)
	agent()
	write(`{"type":"user"}` + "\n") // the head is remembered even if the file is replaced
	if a := agent(); a != "first" {
		t.Errorf("head not kept: %q", a)
	}
	if b := p.Sessions()[1].Agent; b != "" {
		t.Errorf("a session without a log: %q", b)
	}
}

func seenPoller(t *testing.T, raw *[]Raw) (*Poller, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seen.json")
	seen, _ := avatar.LoadBindings(path)
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return *raw, nil })}, StateDir: t.TempDir(), Seen: seen}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	return p, path
}

func seenFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(b)), "")
}

// A done the user has seen is idle until its status changes; the entry of a session that is gone is forgotten.
func TestSeenDone(t *testing.T) {
	t1 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	raw := []Raw{
		{ID: "a", Status: "idle", StatusChangedAt: t1},
		{ID: "b", Status: "idle", StatusChangedAt: t1},
		{ID: "c", Status: "idle", StatusChangedAt: t1},
	}
	p, path := seenPoller(t, &raw)
	a := byID(t, p.Sessions(), "a")
	if !a.Done || a.AvatarState != "success" || !a.StatusChangedAt.Equal(t1) {
		t.Fatalf("before: %+v", a)
	}
	for _, id := range []string{"a", "b"} {
		if err := p.MarkSeen(byID(t, p.Sessions(), id)); err != nil {
			t.Fatal(err)
		}
	}
	if a = byID(t, p.Sessions(), "a"); a.Done || a.AvatarState != "idle" || !byID(t, p.Sessions(), "c").Done {
		t.Errorf("right after MarkSeen: %+v", a)
	}
	p.Refresh()
	if byID(t, p.Sessions(), "a").Done || !byID(t, p.Sessions(), "c").Done {
		t.Error("a poll brings the done back")
	}
	if got := seenFile(t, path); got != `{"a":"2026-10-13T12:00:00Z","b":"2026-10-13T12:00:00Z"}` {
		t.Errorf("seen.json %s", got)
	}
	// another board, same file: seen there is seen here
	other, _ := avatar.LoadBindings(path)
	p2 := &Poller{Sources: p.Sources, StateDir: t.TempDir(), Seen: other}
	p2.Refresh()
	if byID(t, p2.Sessions(), "a").Done {
		t.Error("a restart forgets what was seen")
	}
	// it works again and is done again: a new done
	raw[0].Status = "working"
	p.Refresh()
	raw[0].Status, raw[0].StatusChangedAt = "idle", t1.Add(time.Minute)
	p.Refresh()
	if !byID(t, p.Sessions(), "a").Done {
		t.Error("a new done (statusChangedAt moved) is not seen")
	}
	// b's session is gone: its entry goes, a's stays
	raw = raw[:1]
	p.Refresh()
	if got := seenFile(t, path); got != `{"a":"2026-10-13T12:00:00Z"}` {
		t.Errorf("seen.json %s, want b's entry dropped", got)
	}
}

// A source that fails drops nothing.
func TestSeenKeptWhenNotKnown(t *testing.T) {
	t1 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	raw := []Raw{{ID: "a", Status: "idle", StatusChangedAt: t1}}
	p, path := seenPoller(t, &raw)
	p.MarkSeen(p.Sessions()[0])
	p.Sources = []Source{SourceFunc(func() ([]Raw, error) { return nil, os.ErrDeadlineExceeded })}
	p.Refresh()
	if got := seenFile(t, path); !strings.Contains(got, `"a"`) {
		t.Errorf("a failed poll forgot: %s", got)
	}
}

// A seen.json that is not JSON means nothing is seen; the board says so, and the file is left for the user.
func TestSeenCorruptFile(t *testing.T) {
	raw := []Raw{{ID: "a", Status: "idle", StatusChangedAt: time.Now()}}
	p, path := seenPoller(t, &raw)
	os.WriteFile(path, []byte("{oops"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(time.Hour))
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if e := p.Err(); e == nil || !strings.Contains(e.Error(), "seen.json") {
		t.Errorf("Err() = %v, want the seen.json problem", e)
	}
	if !p.Sessions()[0].Done {
		t.Error("nothing is seen")
	}
	if err := p.MarkSeen(p.Sessions()[0]); err == nil || !strings.Contains(err.Error(), "seen.json") {
		t.Errorf("MarkSeen: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "{oops" {
		t.Errorf("the corrupt file was overwritten: %q", b)
	}
	os.WriteFile(path, []byte("{}"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(2*time.Hour))
	p.Refresh()
	if p.Err() != nil {
		t.Errorf("fixed by hand: %v", p.Err())
	}
}

// A session with no statusChangedAt can't be told apart from its next done: it is never done, and never marked.
func TestSeenNeedsStatusChangedAt(t *testing.T) {
	raw := []Raw{{ID: "a", Status: "idle"}}
	p, path := seenPoller(t, &raw)
	if err := p.MarkSeen(p.Sessions()[0]); err != nil || p.Sessions()[0].Done || seenFile(t, path) != "" {
		t.Errorf("err %v done %v file %q", err, p.Sessions()[0].Done, seenFile(t, path))
	}
}

// A MarkSeen of an older done that lands after a poll already shows a newer one leaves the newer one done.
func TestMarkSeenOldDone(t *testing.T) {
	seen, _ := avatar.LoadBindings(filepath.Join(t.TempDir(), "seen.json"))
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	p := &Poller{Seen: seen}
	p.sessions = []Session{{ID: "a", Status: "idle", Done: true, StatusChangedAt: t0.Add(time.Minute)}}
	if err := p.MarkSeen(Session{ID: "a", Status: "idle", Done: true, StatusChangedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if !p.Sessions()[0].Done {
		t.Error("the newer done was taken off by a MarkSeen of the older one")
	}
}

func pinPoller(t *testing.T, raw *[]Raw) (*Poller, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pins.json")
	pins, _ := avatar.LoadBindings(path)
	p := &Poller{Sources: []Source{SourceFunc(func() ([]Raw, error) { return *raw, nil })}, StateDir: t.TempDir(), Pins: pins}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	return p, path
}

// TogglePin pins and unpins by name (here the ID: these sessions have none): at once on the sessions the board holds, in pins.json
// (read back by another poller, a restart), and a session that is gone loses its pin.
func TestTogglePin(t *testing.T) {
	raw := []Raw{{ID: "a", Status: "idle"}, {ID: "b", Status: "idle"}, {ID: "c", Status: "idle"}}
	p, path := pinPoller(t, &raw)
	if byID(t, p.Sessions(), "a").Pinned {
		t.Fatal("pinned from the start")
	}
	for _, id := range []string{"b", "a"} {
		if _, err := p.TogglePin(byID(t, p.Sessions(), id)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // two pins never share a time
	}
	a, b := byID(t, p.Sessions(), "a"), byID(t, p.Sessions(), "b")
	if !a.Pinned || !b.Pinned || !b.PinnedAt.Before(a.PinnedAt) || byID(t, p.Sessions(), "c").Pinned {
		t.Errorf("right after: a %+v b %+v", a, b)
	}
	p.Refresh()
	if a2 := byID(t, p.Sessions(), "a"); !a2.Pinned || !a2.PinnedAt.Equal(a.PinnedAt) {
		t.Errorf("a poll loses the pin: %+v", a2)
	}
	// a restart: a new poller on the same file
	other, _ := avatar.LoadBindings(path)
	p2 := &Poller{Sources: p.Sources, StateDir: t.TempDir(), Pins: other}
	p2.Refresh()
	if !byID(t, p2.Sessions(), "a").Pinned || !byID(t, p2.Sessions(), "b").Pinned || byID(t, p2.Sessions(), "c").Pinned {
		t.Errorf("a restart forgets the pins: %+v", p2.Sessions())
	}
	// unpin
	if _, err := p.TogglePin(byID(t, p.Sessions(), "b")); err != nil {
		t.Fatal(err)
	}
	if byID(t, p.Sessions(), "b").Pinned || !byID(t, p.Sessions(), "a").Pinned {
		t.Errorf("unpin b: %+v", p.Sessions())
	}
	if got := seenFile(t, path); !strings.HasPrefix(got, `{"a":"`) || strings.Contains(got, `"b"`) {
		t.Errorf("pins.json %s", got)
	}
	// a's session is gone: its pin goes, after pinGrace
	now := time.Now()
	p.Now = func() time.Time { return now }
	raw = raw[1:]
	p.Refresh()
	if got := seenFile(t, path); got == "{}" {
		t.Error("the pin went at the first poll without the session")
	}
	now = now.Add(pinGrace + time.Second)
	p.Refresh()
	if got := seenFile(t, path); got != "{}" {
		t.Errorf("pins.json %s, want the gone session dropped", got)
	}
	// ... but not while ccmux fails
	raw = append(raw, Raw{ID: "d", Status: "idle"})
	p.Refresh()
	p.TogglePin(byID(t, p.Sessions(), "d"))
	p.Sources = []Source{SourceFunc(func() ([]Raw, error) { return nil, os.ErrDeadlineExceeded })}
	p.Refresh()
	if got := seenFile(t, path); !strings.Contains(got, `"d"`) {
		t.Errorf("a failed poll dropped the pin: %s", got)
	}
}

// A pins.json that is not JSON means nothing is pinned; the board says so and the file is left for the user.
func TestPinsCorruptFile(t *testing.T) {
	raw := []Raw{{ID: "a", Status: "idle"}}
	p, path := pinPoller(t, &raw)
	os.WriteFile(path, []byte("{oops"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(time.Hour))
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if e := p.Err(); e == nil || !strings.Contains(e.Error(), "pins.json") {
		t.Errorf("Err() = %v, want the pins.json problem", e)
	}
	if p.Sessions()[0].Pinned {
		t.Error("nothing is pinned")
	}
	if _, err := p.TogglePin(p.Sessions()[0]); err == nil || !strings.Contains(err.Error(), "pins.json") {
		t.Errorf("TogglePin: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "{oops" {
		t.Errorf("the corrupt file was overwritten: %q", b)
	}
	os.WriteFile(path, []byte("{}"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(2*time.Hour))
	p.Refresh()
	if p.Err() != nil {
		t.Errorf("fixed by hand: %v", p.Err())
	}
}

// A pin is kept by name: a session restarted or handed off (a new ID, the same name) is still
// pinned, and unpinning it works from the new ID; it goes when no session has the name.
func TestPinSurvivesNewID(t *testing.T) {
	raw := []Raw{{ID: "a1", Name: "agmux-dev", Status: "idle"}, {ID: "b1", Name: "other", Status: "idle"}}
	p, path := pinPoller(t, &raw)
	if _, err := p.TogglePin(byID(t, p.Sessions(), "a1")); err != nil {
		t.Fatal(err)
	}
	if got := seenFile(t, path); !strings.HasPrefix(got, `{"agmux-dev":"`) {
		t.Errorf("pins.json %s, want it keyed by name", got)
	}
	raw[0].ID = "a2" // ccmux restart: same name, new ID
	p.Refresh()
	if a := byID(t, p.Sessions(), "a2"); !a.Pinned || byID(t, p.Sessions(), "b1").Pinned {
		t.Errorf("after the restart: %+v", p.Sessions())
	}
	if !strings.Contains(seenFile(t, path), `"agmux-dev"`) {
		t.Error("the restart dropped the pin")
	}
	if _, err := p.TogglePin(byID(t, p.Sessions(), "a2")); err != nil || byID(t, p.Sessions(), "a2").Pinned {
		t.Errorf("unpin from the new ID: %v %+v", err, p.Sessions())
	}
	p.TogglePin(byID(t, p.Sessions(), "a2"))
	now := time.Now()
	p.Now = func() time.Time { return now }
	raw[0].Name = "renamed" // no session has the name now
	p.Refresh()
	now = now.Add(pinGrace + time.Second)
	p.Refresh()
	if got := seenFile(t, path); got != "{}" {
		t.Errorf("pins.json %s, want the name dropped", got)
	}
}

// A session restarted by ccmux is out of its list for a few seconds, and a daemon just started
// answers with none: a pin survives that, and goes only when no session has had the name for pinGrace.
func TestPinGrace(t *testing.T) {
	raw := []Raw{{ID: "a1", Name: "agmux-dev", Status: "idle"}, {ID: "b1", Name: "other", Status: "idle"}}
	p, path := pinPoller(t, &raw)
	now := time.Now()
	p.Now = func() time.Time { return now }
	p.TogglePin(byID(t, p.Sessions(), "a1"))
	p.TogglePin(byID(t, p.Sessions(), "b1"))
	// the restart gap: one poll without the session, then it is back with a new ID
	saved := raw
	raw = raw[1:]
	p.Refresh()
	now = now.Add(5 * time.Second)
	raw = append([]Raw{{ID: "a2", Name: "agmux-dev", Status: "idle"}}, saved[1:]...)
	p.Refresh()
	if !byID(t, p.Sessions(), "a2").Pinned || !byID(t, p.Sessions(), "b1").Pinned {
		t.Errorf("after the gap: %+v", p.Sessions())
	}
	// it came back, so the clock is reset: a gap of 50 s, back, and another of 50 s is not 100 s
	for range 2 {
		raw = raw[1:]
		p.Refresh()
		now = now.Add(50 * time.Second)
		p.Refresh()
		raw = append([]Raw{{ID: "a2", Name: "agmux-dev", Status: "idle"}}, raw...)
		p.Refresh()
	}
	if !byID(t, p.Sessions(), "a2").Pinned {
		t.Errorf("two short gaps lost the pin: %s", seenFile(t, path))
	}
	// an empty list (a daemon just started): every pin stays until pinGrace is up
	raw = nil
	p.Refresh()
	now = now.Add(pinGrace - time.Second)
	p.Refresh()
	if got := seenFile(t, path); !strings.Contains(got, "agmux-dev") || !strings.Contains(got, "other") {
		t.Errorf("an empty list for %v dropped pins: %s", pinGrace-time.Second, got)
	}
	now = now.Add(2 * time.Second)
	p.Refresh()
	if got := seenFile(t, path); got != "{}" {
		t.Errorf("pins.json %s after more than pinGrace without them", got)
	}
}

// What is pinned is what pins.json says now, not what the caller saw: two presses of t on one
// snapshot pin and unpin; a pin made by another board is seen, and undone, without a poll between.
func TestTogglePinReadsTheFile(t *testing.T) {
	raw := []Raw{{ID: "a1", Name: "x", Status: "idle"}}
	p, path := pinPoller(t, &raw)
	snap := byID(t, p.Sessions(), "a1") // what the board read for both key presses
	on1, err1 := p.TogglePin(snap)
	on2, err2 := p.TogglePin(snap)
	if err1 != nil || err2 != nil || !on1 || on2 || byID(t, p.Sessions(), "a1").Pinned || seenFile(t, path) != "{}" {
		t.Errorf("t t: %v %v, then %v %v, pinned %v, pins.json %s", on1, err1, on2, err2, byID(t, p.Sessions(), "a1").Pinned, seenFile(t, path))
	}
	// another board pins x; this one has not polled since
	other, _ := avatar.LoadBindings(path)
	other.Set("x", time.Now().UTC().Format(time.RFC3339Nano))
	if on, err := p.TogglePin(snap); err != nil || on || strings.Contains(seenFile(t, path), "x") { // snap says "not pinned": the file says it is
		t.Errorf("on a pin another board made: pinned %v err %v pins.json %s", on, err, seenFile(t, path))
	}
}

// Pinning one name leaves the others alone, and sessions of one name go together.
func TestTogglePinByName(t *testing.T) {
	raw := []Raw{{ID: "a1", Name: "x", Status: "idle"}, {ID: "a2", Name: "x", Status: "idle"}, {ID: "b1", Name: "y", Status: "idle"}}
	p, _ := pinPoller(t, &raw)
	p.TogglePin(byID(t, p.Sessions(), "a1"))
	if !byID(t, p.Sessions(), "a1").Pinned || !byID(t, p.Sessions(), "a2").Pinned || byID(t, p.Sessions(), "b1").Pinned {
		t.Errorf("same name pinned together, the other left alone: %+v", p.Sessions())
	}
}
