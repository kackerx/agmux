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
		{`{"context_window":{"used_percentage":41.6,"remaining_percentage":58.4},"rate_limits":{"used_percentage":99}}`, 42},
		{`{"context_window":{"used_percentage":0}}`, 0},
		{`{"context_window":{"used_percentage":null}}`, -1},
		{`{"context_window":`, -1}, // half-written: treat as missing
	} {
		os.WriteFile(hud, []byte(c.body), 0o644)
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
		os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
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
