package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		body string
		want int
	}{
		// rate_limits first: an implementation grabbing the first used_percentage gets 99
		{`{"rate_limits":{"five_hour":{"used_percentage":99}},"context_window":{"used_percentage":41.6,"remaining_percentage":58.4}}`, 41},
		{`{"context_window":{"used_percentage":29.6}}`, 29}, // truncated, like claude-hud
		{`{"context_window":{"used_percentage":0}}`, 0},
		{`{"context_window":{"used_percentage":null}}`, -1},
		{`{"context_window":`, -1}, // half-written: treat as missing
	} {
		if err := os.WriteFile(hud, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		p.Refresh()
		if got := p.Sessions()[0].ContextPct; got != c.want {
			t.Errorf("%s: ContextPct=%d want %d", c.body, got, c.want)
		}
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
