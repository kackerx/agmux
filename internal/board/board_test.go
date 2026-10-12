package board

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/store"
)

var now = time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC)

func fixture(t *testing.T) []store.Session {
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
		raw[i].LogPath = ""
	}
	p := &store.Poller{Show: func() ([]ccmux.Session, error) { return raw, nil }, StateDir: t.TempDir()}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	return p.Sessions()
}

func TestColumns(t *testing.T) {
	for _, c := range []struct{ w, n, colW int }{
		{40, 1, 40}, {113, 1, 113}, {114, 2, 56}, {120, 2, 59}, {200, 3, 65},
	} {
		if n, colW := columns(c.w); n != c.n || colW != c.colW {
			t.Errorf("columns(%d) = %d×%d, want %d×%d", c.w, n, colW, c.n, c.colW)
		}
	}
}

func TestView(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	ss := fixture(t)
	for _, w := range []int{60, 114, 120, 200} {
		out, rects, _ := view(ss, w, 200, ss[0].ID, 0, now, "", false)
		lines := strings.Split(out, "\n")
		if len(lines) != 200 {
			t.Errorf("w=%d: %d lines, want the full height", w, len(lines))
		}
		for i, l := range lines {
			if lw := ansi.StringWidth(l); lw > w {
				t.Errorf("w=%d line %d: width %d overflows: %q", w, i, lw, ansi.Strip(l))
			}
		}
		if len(rects) != len(ss) {
			t.Errorf("w=%d: %d cards, want %d", w, len(rects), len(ss))
		}
		_, colW := columns(w)
		for _, r := range rects {
			for dy := range cardH {
				l := lines[1+r.y+dy]
				if cw := ansi.StringWidth(ansi.Cut(l, r.x, r.x+r.w)); cw != colW {
					t.Errorf("w=%d card %s line %d width %d, want %d", w, r.id, dy, cw, colW)
				}
			}
			if top := ansi.Strip(ansi.Cut(lines[1+r.y], r.x, r.x+1)); top != "╭" {
				t.Errorf("w=%d card %s: corner %q at its rect", w, r.id, top)
			}
		}
	}
	t.Setenv("HOME", "/Users/dev") // the fixture's home, so headers shorten to ~ on any machine
	out, _, _ := view(ss, 200, 50, "", 0, now, "", false)
	plain := ansi.Strip(out)
	for _, want := range []string{"agmux · 15 sessions", "▾ ~/proj/sqlmux (4)", "sqlmux-orchestrator", "← sqlmux-code-reviewer: review notes", "q 退出"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestScrollKeepsFocusVisible(t *testing.T) {
	ss := fixture(t)
	last := groups(ss)
	id := last[len(last)-1].sessions[0].ID
	out, rects, off := view(ss, 60, 20, id, 0, now, "", false)
	if off == 0 || len(strings.Split(out, "\n")) != 20 {
		t.Fatalf("expected a scrolled 20-line screen, off=%d", off)
	}
	for _, r := range rects {
		if r.id == id && (r.y-off < 0 || r.y-off+r.h > 17) {
			t.Errorf("focused card at body rows %d..%d, outside 0..17", r.y-off, r.y-off+r.h)
		}
	}
}

func TestCardStatus(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	base := store.Session{ID: "a", Name: "n", Status: "working", PendingTool: "Bash", ContextPct: -1}
	cases := []struct {
		name string
		mod  func(*store.Session)
		want string
	}{
		{"working tool", func(s *store.Session) {}, "⏵ working · Bash"},
		{"working agents", func(s *store.Session) { s.PendingTool = ""; s.Subagents = make([]ccmux.Subagent, 2) }, "⏵ working · agents  ⚇ 2 agents"},
		{"idle", func(s *store.Session) { s.Status = "idle" }, "⏸ idle"},
		{"done", func(s *store.Session) { s.Status = "idle"; s.Done = true }, "● done"},
		{"waiting", func(s *store.Session) { s.Status = "waiting" }, "⏳ waiting"},
	}
	for _, c := range cases {
		s := base
		c.mod(&s)
		if got := ansi.Strip(line2(s, 60)); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: %q", c.name, got)
		}
	}
	red := "38;2;255;97;136m╭"
	if !strings.Contains(card(store.Session{Status: "waiting"}, 60, 5, false, 0, now), red) ||
		!strings.Contains(card(store.Session{Status: "idle", Attention: "permission"}, 60, 5, false, 0, now), red) ||
		strings.Contains(card(store.Session{Status: "idle"}, 60, 5, false, 0, now), red) {
		t.Error("border must be red exactly when attentionType is set or status is waiting")
	}
	if card(base, 60, 5, true, 0, now) == card(base, 60, 5, false, 0, now) {
		t.Error("focused card must look different")
	}
}

func TestContextBar(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, c := range []struct {
		pct    int
		filled int
		color  string
		text   string
	}{
		{-1, 0, "", " --"}, {0, 0, "166;227;161", " 0%"}, {1, 1, "166;227;161", " 1%"}, {29, 3, "166;227;161", " 29%"},
		{30, 3, "249;226;175", " 30%"}, {41, 4, "249;226;175", " 41%"}, {60, 6, "255;97;136", " 60%"}, {100, 10, "255;97;136", " 100%"},
	} {
		bar := contextBar(c.pct)
		if plain := ansi.Strip(bar); plain != strings.Repeat("■", 10)+c.text {
			t.Errorf("%d: %q", c.pct, plain)
		}
		if c.filled > 0 && !strings.Contains(bar, c.color+"m"+strings.Repeat("■", c.filled)+"\x1b") {
			t.Errorf("%d: want %d cells in %s: %q", c.pct, c.filled, c.color, bar)
		}
	}
}

func TestModelName(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5": "Opus 5.5", "claude-sonnet-4-5-20250929": "Sonnet 4.5", "claude-fable-5-1": "Fable 5.1",
		"gpt-5-codex": "gpt-5-codex", "": "",
	} {
		if got := modelName(in); got != want {
			t.Errorf("modelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMidTrunc(t *testing.T) {
	s := "~/ProjectsFolder/shopcart.worktrees/hotfix/rename-flag"
	got := midTrunc(s, 30)
	if ansi.StringWidth(got) != 30 || !strings.HasPrefix(got, "~/Projec") || !strings.HasSuffix(got, "rename-flag") || !strings.Contains(got, "…") {
		t.Errorf("midTrunc = %q", got)
	}
	if midTrunc("~/a", 30) != "~/a" {
		t.Error("short paths unchanged")
	}
}

func TestRelTime(t *testing.T) {
	for d, want := range map[time.Duration]string{5 * time.Second: "5s", 3 * time.Minute: "3m", 5 * time.Hour: "5h", 50 * time.Hour: "2d", -time.Second: "0s"} {
		if got := relTime(now.Add(-d), now); got != want {
			t.Errorf("%v: %q want %q", d, got, want)
		}
	}
	if relTime(time.Time{}, now) != "" {
		t.Error("zero time = blank")
	}
}
