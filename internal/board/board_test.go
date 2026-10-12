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
		{30, 1, 30}, {81, 1, 81}, {82, 2, 40}, {113, 2, 55}, {200, 4, 48}, {232, 4, 56}, {300, 4, 73},
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
		out, rects, _ := view(ss, w, 200, ss[0].ID, 0, now, "", false, nil)
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
			corner := map[bool]string{false: "╭", true: "┏"}[r.id == ss[0].ID] // the focused card is thick
			if top := ansi.Strip(ansi.Cut(lines[1+r.y], r.x, r.x+1)); top != corner {
				t.Errorf("w=%d card %s: corner %q at its rect", w, r.id, top)
			}
		}
	}
	t.Setenv("HOME", "/Users/dev") // the fixture's home, so headers shorten to ~ on any machine
	out, _, _ := view(ss, 200, 50, "", 0, now, "", false, nil)
	plain := ansi.Strip(out)
	for _, want := range []string{"agmux · 15 sessions", "▾ ~/proj/sqlmux (4)", "sqlmux-orchestrator", "⏵ working", "● done", "q 退出"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestScrollKeepsFocusVisible(t *testing.T) {
	ss := fixture(t)
	last := groups(ss)
	id := last[len(last)-1].sessions[0].ID
	out, rects, off := view(ss, 60, 20, id, 0, now, "", false, nil)
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
		name   string
		mod    func(*store.Session)
		head   string         // status text, exactly as colored
		color  lipgloss.Color // its color
		tail   string         // what follows it (plain)
		mood   avatar.Mood
		border lipgloss.Color
	}{
		{"waiting", func(s *store.Session) { s.Status = "waiting" }, "⏳ waiting", cRed, "", avatar.Alert, cRed},
		{"attention", func(s *store.Session) { s.Status = "idle"; s.Attention = "permission" }, "⏸ idle", cMuted, "", avatar.Alert, cRed},
		{"attention beats working", func(s *store.Session) { s.Status = "working"; s.Attention = "permission" }, "⏵ working", cWorking, "", avatar.Alert, cRed},
		{"working + tool", func(s *store.Session) {
			s.Status = "working"
			s.PendingTool = "Bash"
			s.Subagents = make([]ccmux.Subagent, 1)
		}, "⏵ working", cWorking, " · Bash", avatar.Scan, cWorking},
		{"raw working + agents: no 'agents'", func(s *store.Session) { s.Status = "working"; s.Subagents = make([]ccmux.Subagent, 2) }, "⏵ working", cWorking, "", avatar.Scan, cWorking},
		{"working because of agents", func(s *store.Session) {
			s.Status = "working"
			s.AgentsWorking = true
			s.Subagents = make([]ccmux.Subagent, 1)
		}, "⏵ working", cWorking, " · agents", avatar.Scan, cWorking},
		{"done", func(s *store.Session) { s.Status = "idle"; s.Done = true }, "● done", cDone, "", avatar.Normal, cDone},
		{"idle", func(s *store.Session) { s.Status = "idle" }, "⏸ idle", cMuted, "", avatar.Dim, cDim},
	}
	sp, _ := avatar.ByID("bot-01")
	for _, c := range cases {
		s := base
		c.mod(&s)
		l1 := line1(s, 40, now)
		if !strings.Contains(l1, fg(c.color, c.head)) || !strings.HasSuffix(ansi.Strip(l1), " "+c.head+c.tail) {
			t.Errorf("%s: line 1 %q, want it to end with %q (%q in %s)", c.name, ansi.Strip(l1), c.head+c.tail, c.head, c.color)
		}
		for f := range 2 { // moods differ on different frames: Alert on even, Scan on odd
			cd := card(s, 44, 5, false, f, now)
			for i, l := range strings.Split(avatar.Render(sp, "claude", c.mood, f), "\n") {
				if !strings.Contains(cd, l) {
					t.Errorf("%s frame %d: eyes line %d are not mood %d", c.name, f, i, c.mood)
				}
			}
		}
		// border = status color; focus = a thick line in the focus color, whatever the status
		for corner, focused := range map[string]bool{"╭": false, "┏": true} {
			color := map[bool]lipgloss.Color{false: c.border, true: cFocus}[focused]
			want, _, _ := strings.Cut(fg(color, corner), corner)
			if cd := card(s, 44, 5, focused, 0, now); !strings.HasPrefix(cd, want+corner) {
				t.Errorf("%s focused=%v: border should start %q in %s", c.name, focused, corner, color)
			}
		}
	}
}

func TestCardLines(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	s := store.Session{
		Name: "qa-acceptance-pap-be-dev", AgentType: "claude", Status: "idle", LastActivity: now.Add(-3 * time.Minute),
		Branch: "feature/qa-acceptance-pap", Model: "claude-opus-5-5", Effort: "high", ContextPct: -1,
	}

	// line 1: icon + name give way; status and time always stay
	for _, c := range []struct {
		w       int
		nameCut bool
	}{{40, false}, {22, true}} {
		l := ansi.Strip(line1(s, c.w, now))
		if ansi.StringWidth(l) != c.w || !strings.HasPrefix(l, "\uec82 qa-") || !strings.HasSuffix(l, " ⏸ idle  3m") ||
			strings.Contains(l, s.Name) == c.nameCut || strings.Contains(l, "…") != c.nameCut {
			t.Errorf("w=%d line 1 %q: name cut=%v, status and time last", c.w, l, c.nameCut)
		}
	}
	for at, icon := range map[string]string{"claude": "\uec82", "codex": "\uec81", "gemini": "\uec20", "": "\uec20"} {
		if l := line1(store.Session{Name: "n", AgentType: at}, 30, now); !strings.HasPrefix(l, fg(avatar.BandColor(at), icon)) {
			t.Errorf("%q: icon should be %U in the band color", at, []rune(icon)[0])
		}
	}

	// line 2: branch gives way, model·effort flush right
	for _, c := range []struct {
		w             int
		model, effort string
		right         string
		branchCut     bool
	}{
		{60, "claude-opus-5-5", "high", "Opus 5.5·high", false},
		{34, "claude-opus-5-5", "high", "Opus 5.5·high", true},
		{60, "claude-opus-5-5", "", "Opus 5.5", false},
		{60, "", "high", "", false},
	} {
		s := s
		s.Model, s.Effort = c.model, c.effort
		l := ansi.Strip(line2(s, c.w))
		if ansi.StringWidth(l) != c.w || !strings.HasPrefix(l, "⑂ feature/") || !strings.HasSuffix(l, c.right) ||
			strings.Contains(l, "feature/qa-acceptance-pap") == c.branchCut || strings.Contains(l, "…") != c.branchCut ||
			(c.right == "" && strings.Contains(l, "high")) {
			t.Errorf("w=%d line 2 %q: want %q flush right, branch cut=%v", c.w, l, c.right, c.branchCut)
		}
	}

	// line 3: context bar at the left edge, counts on the right
	for _, c := range []struct {
		agents, shells int
		right          string
	}{{0, 0, ""}, {2, 0, "⚇ 2 agents"}, {0, 1, "⚙ 1 shell"}, {1, 2, "⚇ 1 agent  ⚙ 2 shells"}} {
		s := store.Session{ContextPct: 41, Subagents: make([]ccmux.Subagent, c.agents), Shells: c.shells}
		l := ansi.Strip(line3(s, 40))
		if ansi.StringWidth(l) != 40 || !strings.HasPrefix(l, "■■■■■■ ") || strings.Contains(l, "41") || !strings.HasSuffix(l, c.right) ||
			(c.shells == 0 && strings.Contains(l, "⚙")) || (c.agents == 0 && strings.Contains(l, "⚇")) {
			t.Errorf("agents=%d shells=%d line 3 %q, want %q on the right", c.agents, c.shells, l, c.right)
		}
	}

	// in the card: line 3's bar starts right under the avatar
	lines := strings.Split(ansi.Strip(card(s, 44, 5, false, 0, now)), "\n")
	if !strings.HasPrefix(lines[3], "│ ■") || ansi.StringWidth(lines[1]) != 44 {
		t.Errorf("card rows: %q", lines)
	}
}

func TestMultilineMessage(t *testing.T) {
	ss := fixture(t)
	out, _, _ := view(ss, 120, 30, "", 0, now, "ccmux kill x: boom\n  at stack\n  at more", false, nil)
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
		// 6 cells, rounded: 29% → 1.74 → 2, 41% → 2.46 → 2, 42% → 2.52 → 3; any non-zero shows at least one
		{-1, 0, "", ""}, {0, 0, "166;227;161", ""}, {1, 1, "166;227;161", ""}, {29, 2, "166;227;161", ""},
		{30, 2, "249;226;175", ""}, {41, 2, "249;226;175", ""}, {42, 3, "249;226;175", ""},
		{60, 4, "255;97;136", ""}, {100, 6, "255;97;136", ""},
	} {
		bar := contextBar(c.pct)
		if plain := ansi.Strip(bar); plain != strings.Repeat("■", 6)+c.text { // no number, no "--"
			t.Errorf("%d: %q", c.pct, plain)
		}
		if c.filled > 0 && !strings.Contains(bar, "38;2;"+c.color+"m"+strings.Repeat("■", c.filled)+"\x1b") {
			t.Errorf("%d: want %d cells in %s: %q", c.pct, c.filled, c.color, bar)
		}
		// with no number, the colors are all the bar says: the rest must be the dark #585b70
		if dark := 6 - c.filled; !strings.Contains(bar, "38;2;88;91;112m"+strings.Repeat("■", dark)+"\x1b") || strings.Count(bar, "■") != 6 {
			t.Errorf("%d: want %d dark cells: %q", c.pct, dark, bar)
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

// The board's order comes only from the data: shuffling ccmux's array changes nothing.
func TestOrderIgnoresCcmuxOrder(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	ss := fixture(t)
	reversed := make([]store.Session, len(ss))
	for i, s := range ss {
		reversed[len(ss)-1-i] = s
	}
	rotated := append(append([]store.Session{}, ss[5:]...), ss[:5]...)
	want, _, _ := view(ss, 200, 60, "", 0, now, "", false, nil)
	for name, order := range map[string][]store.Session{"reversed": reversed, "rotated": rotated} {
		if got, _, _ := view(order, 200, 60, "", 0, now, "", false, nil); got != want {
			t.Errorf("%s ccmux order renders differently", name)
		}
	}
	for _, g := range groups(ss) {
		for i := 1; i < len(g.sessions); i++ {
			if a, b := g.sessions[i-1], g.sessions[i]; a.Name > b.Name {
				t.Errorf("%s: %q before %q", g.key, a.Name, b.Name)
			}
		}
	}
	same := []store.Session{{ID: "b", Name: "x", GroupKey: "/g"}, {ID: "a", Name: "x", GroupKey: "/g"}}
	if g := groups(same)[0].sessions; g[0].ID != "a" {
		t.Error("same name: ordered by id")
	}
}

// Line 1: the name gets all it needs first; the pending tool takes what is left and goes
// (" · " included) when none of it would show; the status word and time always stay.
func TestLine1NameFirst(t *testing.T) {
	base := store.Session{Name: "qa-acceptance-pap-be-dev", AgentType: "claude", Status: "working", LastActivity: now.Add(-2 * time.Minute)}
	for _, c := range []struct {
		w          int
		tool, want string // want: what follows "⏵ working" before the time
		nameWhole  bool
	}{
		{44, "Bash", "", true}, // 56-column card: whole name, " · Bash" doesn't fit and goes
		{48, "Bash", " · Bash", true},
		{46, "Bash", " · Ba…", true}, // 30 cells left of the status: 24 name + 6
		{60, "mcp__claude_ai_Claude_Docs__batch", " · mcp__claude_ai_C…", true},
		{28, "mcp__claude_ai_Claude_Docs__batch", "", false}, // 40-column card: tool gone, then the name is cut
		{80, "mcp__claude_ai_Claude_Docs__batch", " · mcp__claude_ai_Claude_Docs__batch", true},
	} {
		s := base
		s.PendingTool = c.tool
		l := ansi.Strip(line1(s, c.w, now))
		if ansi.StringWidth(l) != c.w || !strings.HasSuffix(l, " ⏵ working"+c.want+"  2m") ||
			strings.Contains(l, s.Name) != c.nameWhole || strings.Contains(l, " · …") {
			t.Errorf("w=%d tool=%s: %q, want name whole=%v then \"⏵ working%s  2m\"", c.w, c.tool, l, c.nameWhole, c.want)
		}
		if !c.nameWhole && ansi.StringWidth(strings.Fields(l)[1]) < 10 {
			t.Errorf("w=%d: the name should still keep 10+ cells on the narrowest card: %q", c.w, l)
		}
	}
}

func TestFoldedTitleFocusColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	g := group{key: "/a", sessions: make([]store.Session, 2)}
	focused, plain := header(g, 40, true, true), header(g, 40, true, false)
	if !strings.HasPrefix(focused, "\x1b[38;2;255;142;190m▸") || strings.HasPrefix(plain, "\x1b[38;2;255;142;190m") {
		t.Errorf("a focused folded title is #ff8ebe, an unfocused one is not: %q / %q", focused, plain)
	}
}
