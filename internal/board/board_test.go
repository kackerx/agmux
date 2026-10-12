package board

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/avatar"
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
	base := store.Session{ID: "a", Name: "n", AgentType: "claude", AvatarID: "bot-01", ContextPct: -1}
	cases := []struct {
		name  string
		mod   func(*store.Session)
		head  string         // status text, exactly as colored
		color lipgloss.Color // its color
		tail  string         // what follows it (plain)
		mood  avatar.Mood
		alert bool
	}{
		{"waiting", func(s *store.Session) { s.Status = "waiting" }, "⏳ waiting", cRed, "", avatar.Alert, true},
		{"attention", func(s *store.Session) { s.Status = "idle"; s.Attention = "permission" }, "⏸ idle", cMuted, "", avatar.Alert, true},
		{"attention beats working", func(s *store.Session) { s.Status = "working"; s.Attention = "permission" }, "⏵ working", cWorking, "", avatar.Alert, true},
		{"working + tool", func(s *store.Session) {
			s.Status = "working"
			s.PendingTool = "Bash"
			s.Subagents = make([]ccmux.Subagent, 1)
		}, "⏵ working", cWorking, " · Bash  ⚇ 1 agent", avatar.Scan, false},
		{"raw working + agents: no 'agents'", func(s *store.Session) { s.Status = "working"; s.Subagents = make([]ccmux.Subagent, 2) }, "⏵ working", cWorking, "  ⚇ 2 agents", avatar.Scan, false},
		{"working because of agents", func(s *store.Session) {
			s.Status = "working"
			s.AgentsWorking = true
			s.Subagents = make([]ccmux.Subagent, 1)
		}, "⏵ working", cWorking, " · agents  ⚇ 1 agent", avatar.Scan, false},
		{"done", func(s *store.Session) { s.Status = "idle"; s.Done = true }, "● done", cDone, "", avatar.Normal, false},
		{"idle", func(s *store.Session) { s.Status = "idle" }, "⏸ idle", cMuted, "", avatar.Dim, false},
	}
	sp, _ := avatar.ByID("bot-01")
	for _, c := range cases {
		s := base
		c.mod(&s)
		l2 := line2(s, 60)
		if !strings.Contains(l2, fg(c.color, c.head)) || !strings.HasPrefix(ansi.Strip(l2), c.head+c.tail+" ") {
			t.Errorf("%s: line 2 %q, want %q in %s then %q", c.name, ansi.Strip(l2), c.head, c.color, c.tail)
		}
		for f := range 2 { // moods differ on different frames: Alert on even, Scan on odd
			cd := card(s, 60, 5, false, f, now)
			for i, l := range strings.Split(avatar.Render(sp, "claude", c.mood, f), "\n") {
				if !strings.Contains(cd, l) {
					t.Errorf("%s frame %d: eyes line %d are not mood %d", c.name, f, i, c.mood)
				}
			}
		}
		border := map[[2]bool]lipgloss.Color{{false, false}: cDim, {false, true}: cFocus, {true, false}: cRed, {true, true}: cAlertFocus} // [alert, focused]
		for _, focused := range []bool{false, true} {
			want, _, _ := strings.Cut(fg(border[[2]bool{c.alert, focused}], "╭"), "╭")
			if !strings.Contains(card(s, 60, 5, focused, 0, now), want+"╭") {
				t.Errorf("%s focused=%v: border color wrong", c.name, focused)
			}
		}
	}
}

func TestCardLines(t *testing.T) {
	s := store.Session{Name: "qa-acceptance-pap-be-dev", Branch: "feature/qa-acceptance-pap", LastActivity: now.Add(-3 * time.Minute), ContextPct: -1}
	for _, c := range []struct {
		w            int
		branch       string // "" = must not show
		branchCutOff bool
	}{{60, "⑂ feature/qa-acceptance-pap", false}, {40, "⑂ feat", true}, {30, "", false}} {
		l := ansi.Strip(line1(s, c.w, now))
		if ansi.StringWidth(l) != c.w || !strings.HasPrefix(l, s.Name+" ") || !strings.HasSuffix(l, " 3m") {
			t.Errorf("w=%d line 1 %q: want full name first, 3m last, width %d", c.w, l, c.w)
		}
		switch {
		case c.branch == "" && strings.Contains(l, "⑂"):
			t.Errorf("w=%d: branch should give way: %q", c.w, l)
		case c.branch != "" && (!strings.Contains(l, c.branch) || strings.Contains(l, "…") != c.branchCutOff):
			t.Errorf("w=%d: want %q (cut=%v) in %q", c.w, c.branch, c.branchCutOff, l)
		}
	}

	for _, c := range []struct {
		model, effort, want string
	}{{"claude-opus-5-5", "high", "Opus 5.5 · high"}, {"claude-opus-5-5", "", "Opus 5.5"}, {"", "high", ""}} {
		l := ansi.Strip(line3(store.Session{Detail: "hello", Model: c.model, Effort: c.effort}, 50))
		if ansi.StringWidth(l) != 50 || !strings.HasPrefix(l, "󰭻 hello") || !strings.HasSuffix(l, " "+c.want) || (c.want == "" && strings.TrimSpace(l) != "󰭻 hello") {
			t.Errorf("line 3 %q: want %q flush right", l, c.want)
		}
	}

	for n, want := range map[int]string{0: "", 1: "⚙ 1 shell ", 2: "⚙ 2 shells"} {
		l := ansi.Strip(line2(store.Session{Status: "idle", Shells: n, ContextPct: -1}, 60))
		if (want == "") == strings.Contains(l, "⚙") || !strings.Contains(l, want) {
			t.Errorf("Shells=%d: %q, want %q", n, l, want)
		}
	}
}

func TestMultilineMessage(t *testing.T) {
	ss := fixture(t)
	out, _, _ := view(ss, 120, 30, "", 0, now, "ccmux kill x: boom\n  at stack\n  at more", false)
	lines := strings.Split(out, "\n")
	if len(lines) != 30 || !strings.HasPrefix(ansi.Strip(lines[0]), " agmux") || !strings.Contains(out, "boom") || strings.Contains(out, "at stack") {
		t.Errorf("multi-line message: %d lines (want 30), top bar %q", len(lines), ansi.Strip(lines[0]))
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
