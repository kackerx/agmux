package board

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/config"
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
		{30, 1, 30}, {80, 1, 80}, {81, 2, 40}, {113, 2, 56}, {200, 4, 49}, {232, 4, 57}, {300, 4, 74},
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
		out, rects, _ := view(ss, w, 200, ss[0].ID, 0, now, "", false, nil, w, nil, nil, -1)
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
	out, _, _ := view(ss, 200, 50, "", 0, now, "", false, nil, 200, nil, nil, -1)
	plain := ansi.Strip(out)
	for _, want := range []string{"agmux · 15 sessions", "▾ ~/proj/sqlmux (4)", "sqlmux-orchestrator", "⏵ working", "● done", "q quit"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestScrollKeepsFocusVisible(t *testing.T) {
	ss := fixture(t)
	last := groups(ss)
	id := last[len(last)-1].sessions[0].ID
	out, rects, off := view(ss, 60, 20, id, 0, now, "", false, nil, 60, nil, nil, -1)
	if off == 0 || len(strings.Split(out, "\n")) != 20 {
		t.Fatalf("expected a scrolled 20-line screen, off=%d", off)
	}
	for _, r := range rects {
		if r.id == id && (r.y-off < 0 || r.y-off+r.h > 17) {
			t.Errorf("focused card at body rows %d..%d, outside 0..17", r.y-off, r.y-off+r.h)
		}
	}
}

// What runs in the background is not muted: "agents" and the ⚇ / ⚙ counts are in the working color,
// like the status word; a tool name after it stays muted.
func TestBackgroundWorkIsWorkingColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	base := store.Session{ID: "a", Name: "n", Status: "working", ContextPct: -1, Subagents: make([]ccmux.Subagent, 2), Shells: 3}
	agents := base
	agents.AgentsWorking = true
	tool := base
	tool.PendingTool = "Bash"
	for _, c := range []struct {
		name string
		s    store.Session
		want []string
	}{
		{"agents", agents, []string{fg(cWorking, " · agents"), fg(cWorking, "  ⚇ 2 ⚙ 3")}},
		{"a tool", tool, []string{fg(cMuted, " · Bash"), fg(cWorking, "  ⚇ 2 ⚙ 3")}},
	} {
		out := line3(c.s, 60)
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: %q is missing %q", c.name, out, w)
			}
		}
	}
}

// Line 2: the branch, the agent definition flush right in its own color; the branch is cut, the agent kept.
func TestLine2Agent(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	for _, c := range []struct {
		name          string
		branch, agent string
		w             int
		want          string // plain text
	}{
		{"branch only: as before", "main", "", 30, "\ue725 main"},
		{"branch and agent", "main", "orchestrator", 30, "\ue725 main" + strings.Repeat(" ", 30-6-12) + "orchestrator"},
		{"the branch gives way: its last segment, tail cut", "feature/long-branch-name", "orchestrator", 24, "\ue725 long-bra…" + " " + "orchestrator"},
		{"no branch", "", "orchestrator", 20, strings.Repeat(" ", 8) + "orchestrator"},
		{"no branch, no agent", "", "", 20, ""},
		{"an agent wider than the card is cut", "main", "a-very-long-agent-definition-name", 12, "a-very-long…"},
	} {
		out := line2(store.Session{Branch: c.branch, Agent: c.agent}, c.w, nil)
		if got := ansi.Strip(out); got != c.want || (c.agent != "" && !strings.Contains(out, fg(cAgent, ansi.Strip(out[strings.LastIndex(out, " ")+1:])))) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
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
		l3 := line3(s, 60)
		if !strings.Contains(l3, fg(c.color, c.head)) || !strings.Contains(ansi.Strip(l3), "  "+c.head+c.tail) {
			t.Errorf("%s: line 3 %q, want %q (%q in %s)", c.name, ansi.Strip(l3), c.head+c.tail, c.head, c.color)
		}
		for f := range 2 { // moods differ on different frames: Alert on even, Scan on odd
			cd := card(s, 44, 5, false, f, now, hit{})
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
			if cd := card(s, 44, 5, focused, 0, now, hit{}); !strings.HasPrefix(cd, want+corner) {
				t.Errorf("%s focused=%v: border should start %q in %s", c.name, focused, corner, color)
			}
		}
	}
}

func TestCardLines(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	s := store.Session{
		Name: "qa-acceptance-pap-be-dev", AgentType: "claude", Status: "idle", LastActivity: now.Add(-3 * time.Hour),
		Branch: "feature/qa-acceptance-pap-be-dev", Model: "claude-opus-5-5", Effort: "high", ContextPct: 41,
	}
	// line 1: the name, the time always at the right edge; the name is cut only when it doesn't fit
	for _, c := range []struct {
		w       int
		nameCut bool
	}{{36, false}, {27, false}, {20, true}} {
		l := ansi.Strip(line1(s, c.w, now, nil))
		if ansi.StringWidth(l) != c.w || !strings.HasPrefix(l, "qa-") || !strings.HasSuffix(l, " 3h") || // no icon before the name
			strings.Contains(l, s.Name) == c.nameCut || strings.Contains(l, "…") != c.nameCut {
			t.Errorf("w=%d line 1 %q: name cut=%v, time last", c.w, l, c.nameCut)
		}
	}
	// line 2: the branch with its icon, in the branch color; cut when it doesn't fit
	if l := line2(s, 36, nil); l != fg(cBranch, "\ue725 "+s.Branch) {
		t.Errorf("line 2: %q", ansi.Strip(l))
	}
	if l := ansi.Strip(line2(s, 20, nil)); ansi.StringWidth(l) != 20 || !strings.HasSuffix(l, "…") {
		t.Errorf("line 2 cut: %q", l)
	}
	if line2(store.Session{}, 36, nil) != "" {
		t.Error("no branch, empty line")
	}
	// the acceptance card: 48 columns hold the 24-character name and the 32-character branch whole
	lines := strings.Split(ansi.Strip(card(s, 48, 5, false, 0, now, hit{})), "\n")
	if !strings.Contains(lines[1], s.Name+" ") || strings.HasPrefix(lines[1], "│ ") || !strings.HasSuffix(lines[1], " 3h│") || !strings.Contains(lines[2], "\ue725 "+s.Branch) ||
		strings.HasPrefix(lines[3], "│ ") || !strings.HasSuffix(lines[3], "Opus 5.5·high│") { // the avatar and the right-edge text touch the border
		t.Errorf("48-column card:\n%s", strings.Join(lines, "\n"))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 48 {
			t.Errorf("card line %q is %d wide", l, ansi.StringWidth(l))
		}
	}
}

// The geometry: neighbouring columns are one cell apart, cards in a column touch, and nothing sits
// between a card's border and its avatar or the time at its right edge.
func TestCardGeometry(t *testing.T) {
	mk := func(id, cwd string) store.Session {
		return store.Session{ID: id, Name: id, CWD: cwd, GroupKey: cwd, Status: "idle", ContextPct: -1}
	}
	gs := groups([]store.Session{mk("a1", "/a"), mk("a2", "/a"), mk("b1", "/b")})
	_, rects := layout(gs, 120, "", 0, now, nil, nil)
	at := map[string]rect{}
	for _, r := range rects {
		at[r.id] = r
	}
	if d := at["a2"].y - at["a1"].y; d != cardH {
		t.Errorf("two cards of one column are %d lines apart, want %d (no blank line between)", d, cardH)
	}
	if d := at["b1"].x - at["a1"].x; d != at["a1"].w+1 {
		t.Errorf("columns start %d apart, want %d + 1", d, at["a1"].w)
	}
}

func TestMultilineMessage(t *testing.T) {
	ss := fixture(t)
	out, _, _ := view(ss, 120, 30, "", 0, now, "ccmux kill x: boom\n  at stack\n  at more", false, nil, 120, nil, nil, -1)
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
	want, _, _ := view(ss, 200, 60, "", 0, now, "", false, nil, 200, nil, nil, -1)
	for name, order := range map[string][]store.Session{"reversed": reversed, "rotated": rotated} {
		if got, _, _ := view(order, 200, 60, "", 0, now, "", false, nil, 200, nil, nil, -1); got != want {
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

// Line 3: the bar and the status word always show; as space runs out the detail goes first,
// then the subagent / shell counts, then the model is cut.
// Line 3's fields: the model only with a model, effort only after it, counts only when non-zero,
// two cells between the counts and the model.
func TestLine3Fields(t *testing.T) {
	base := store.Session{Status: "idle", ContextPct: -1}
	for _, c := range []struct {
		name   string
		w      int
		mod    func(*store.Session)
		want   string // the line ends with this
		absent []string
	}{
		{"model and effort", 40, func(s *store.Session) { s.Model, s.Effort = "claude-opus-5-5", "high" }, "  Opus 5.5·high", nil},
		{"model only", 40, func(s *store.Session) { s.Model = "claude-opus-5-5" }, "  Opus 5.5", []string{"·"}},
		{"effort only: nothing", 40, func(s *store.Session) { s.Effort = "high" }, "⏸ idle", []string{"high", "·"}},
		{"no agents, no shells", 40, func(s *store.Session) {}, "⏸ idle", []string{"⚇", "⚙", "0"}},
		{"agents only", 40, func(s *store.Session) { s.Subagents = make([]ccmux.Subagent, 2) }, "⏸ idle  ⚇ 2", []string{"⚙"}},
		{"shells only", 40, func(s *store.Session) { s.Shells = 1 }, "⏸ idle  ⚙ 1", []string{"⚇"}},
		{"counts then two cells then the model", 29, func(s *store.Session) { s.Shells, s.Model = 1, "claude-opus-5-5" }, "⚙ 1  Opus 5.5", nil},
	} {
		s := base
		c.mod(&s)
		l := strings.TrimRight(ansi.Strip(line3(s, c.w)), " ") // 29 is exact for the last one: the gap is the two cells
		if !strings.HasSuffix(l, c.want) {
			t.Errorf("%s: %q, want it to end with %q", c.name, l, c.want)
		}
		for _, a := range c.absent {
			if strings.Contains(strings.TrimPrefix(l, "■■■■■■"), a) {
				t.Errorf("%s: %q must not contain %q", c.name, l, a)
			}
		}
	}
}

func TestLine3Yields(t *testing.T) {
	s := store.Session{Status: "working", PendingTool: "mcp__claude_ai_Claude_Docs__batch", Subagents: make([]ccmux.Subagent, 2),
		Shells: 1, Model: "claude-opus-5-5", Effort: "high", ContextPct: 41}
	for _, c := range []struct {
		w            int
		detail, cnts string // what shows after the status word
		model        string // at the right edge
	}{
		{90, " · mcp__claude_ai_Claude_Docs__batch", "  ⚇ 2 ⚙ 1", "Opus 5.5·high"},
		{60, " · mcp__claude_ai_…", "  ⚇ 2 ⚙ 1", "Opus 5.5·high"}, // the detail is cut first
		{46, " · m…", "  ⚇ 2 ⚙ 1", "Opus 5.5·high"},               // 5 cells: one character of it
		{44, "", "  ⚇ 2 ⚙ 1", "Opus 5.5·high"},                    // 3 cells left: no bare " · …"
		{41, "", "  ⚇ 2 ⚙ 1", "Opus 5.5·high"},                    // then gone, " · " included
		{40, "", "", "Opus 5.5·high"},                             // then the counts
		{26, "", "", "Opus 5…"},                                   // the model last
		{17, "", "", ""},                                          // only bar + status word left
	} {
		l := ansi.Strip(line3(s, c.w))
		want := "■■■■■■  ⏵ working" + c.detail + c.cnts
		if ansi.StringWidth(l) != c.w || !strings.HasPrefix(l, want) || !strings.HasSuffix(l, c.model) ||
			(c.detail == "" && strings.Contains(l, "·  ")) || (c.cnts == "" && strings.ContainsAny(l, "⚇⚙")) {
			t.Errorf("w=%d: %q\n want %q … %q", c.w, l, want, c.model)
		}
	}
}

func TestFoldedTitleFocusColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	g := group{key: "/a", sessions: make([]store.Session, 2)}
	focused, plain := header(g, 40, true, true, nil), header(g, 40, true, false, nil)
	if !strings.HasPrefix(focused, "\x1b[38;2;255;142;190m▸") || strings.HasPrefix(plain, "\x1b[38;2;255;142;190m") {
		t.Errorf("a focused folded title is #ff8ebe, an unfocused one is not: %q / %q", focused, plain)
	}
}

// groupsOf: groups /g0.. with the given number of cards each.
func groupsOf(counts ...int) []group {
	var ss []store.Session
	for g, n := range counts {
		for i := range n {
			id := fmt.Sprintf("g%d-%d", g, i)
			ss = append(ss, store.Session{ID: id, Name: id, CWD: fmt.Sprintf("/g%d", g), GroupKey: fmt.Sprintf("/g%d", g), Status: "idle", ContextPct: -1})
		}
	}
	return groups(ss)
}

func rectsOf(rects []rect) map[string]rect {
	at := map[string]rect{}
	for _, r := range rects {
		at[r.id] = r
	}
	return at
}

// flow = "bands": every n groups are a row, as tall as its tallest group, with a blank line between
// rows. (200 columns: 4 columns.)
func TestLayoutBands(t *testing.T) {
	_, rects := layout(groupsOf(1, 3, 1, 2, 1), 200, "", 0, now, nil, nil)
	at := rectsOf(rects)
	// row 0: groups 0-3 side by side from line 0; the tallest (3 cards) is 1 + 15 lines; row 1 after a blank line
	if at["g0-0"].y != 1 || at["g1-0"].y != 1 || at["g3-1"].y != 6 || at["g0-0"].x != 0 || at["g1-0"].x != at["g0-0"].w+colGap {
		t.Errorf("row 0: %+v %+v %+v", at["g0-0"], at["g1-0"], at["g3-1"])
	}
	if want := 1 + 15 + 1 + 1; at["g4-0"].y != want || at["g4-0"].x != 0 || at["g4-0"].band != 1 || at["g0-0"].band != 0 {
		t.Errorf("row 1: g4-0 %+v, want y %d in band 1", at["g4-0"], want)
	}
}

// flow = "stack": the k-th group goes to column k mod n and each column stacks its groups on its own:
// no blank lines, no alignment between rows.
func TestLayoutStack(t *testing.T) {
	stackFlow = true
	t.Cleanup(func() { stackFlow = false })
	lines, rects := layout(groupsOf(1, 3, 1, 2, 1, 1, 2), 200, "", 0, now, nil, nil)
	at := rectsOf(rects)
	w := at["g0-0"].w
	col := func(id string) int { return at[id].x / (w + colGap) }
	if col("g0-0") != 0 || col("g1-0") != 1 || col("g2-0") != 2 || col("g3-0") != 3 || col("g4-0") != 0 || col("g5-0") != 1 || col("g6-0") != 2 {
		t.Errorf("columns: %+v", rects)
	}
	// group 4 follows group 0 at once (a title line + one card), however tall the others are
	if at["g4-0"].y != 1+cardH+1 || at["g5-0"].y != 1+3*cardH+1 || at["g6-0"].y != 1+cardH+1 || at["g0-0"].band != at["g6-0"].band {
		t.Errorf("stacking: g4 %+v g5 %+v g6 %+v", at["g4-0"], at["g5-0"], at["g6-0"])
	}
	// the tallest column decides the height: g1, g5 = 2 titles + 4 cards
	if want := 2 + 4*cardH; len(lines) != want {
		t.Errorf("%d lines, want %d", len(lines), want)
	}
	// fewer groups than columns: only as many columns as groups
	if lines, _ := layout(groupsOf(1, 1), 200, "", 0, now, nil, nil); ansi.StringWidth(lines[0]) > 2*at["g0-0"].w+colGap {
		t.Errorf("two groups use %d columns", ansi.StringWidth(lines[0]))
	}
}

// A group's title: the path, the count, then a heavy line to the column's right edge in the muted
// color (folded or not); a title that fills the column has no room for one.
func TestGroupHeaderRule(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	g := group{key: "/some/path", sessions: make([]store.Session, 3)}
	for _, folded := range []bool{false, true} {
		h := header(g, 40, folded, false, nil)
		plain := ansi.Strip(h)
		if ansi.StringWidth(h) != 40 || !strings.HasPrefix(plain, map[bool]string{false: "▾ ", true: "▸ "}[folded]+"/some/path (3) ━") || !strings.HasSuffix(plain, "━") ||
			!strings.Contains(h, fg(cMuted, strings.Repeat("━", 40-ansi.StringWidth("▾ /some/path (3) ")))) {
			t.Errorf("folded=%v: %q (%d wide)", folded, plain, ansi.StringWidth(h))
		}
	}
	if h := header(g, 18, false, false, nil); ansi.StringWidth(h) != 18 || !strings.HasSuffix(ansi.Strip(h), " ━") { // room for the space and one stroke
		t.Errorf("one stroke: %q", ansi.Strip(h))
	}
	long := group{key: "/a/very/long/path/that/fills/the/whole/column/and/more", sessions: make([]store.Session, 3)}
	if h := header(long, 30, false, false, nil); ansi.StringWidth(h) != 30 || strings.Contains(h, "━") {
		t.Errorf("full title: %q (%d wide)", ansi.Strip(h), ansi.StringWidth(h))
	}
	// in the layout every title line is a column wide
	lines, _ := layout(groupsOf(1, 1), 120, "", 0, now, map[string]bool{"/g1": true}, nil)
	_, colW := columns(120)
	if ansi.StringWidth(lines[0]) != colW+colGap+colW {
		t.Errorf("title line is %d wide: %q", ansi.StringWidth(lines[0]), ansi.Strip(lines[0]))
	}
}

// [layout] flow reaches the layout through useConfig.
func TestUseConfigFlow(t *testing.T) {
	t.Cleanup(func() { useConfig(config.Default()) })
	c := config.Default()
	if useConfig(c); stackFlow {
		t.Error("the default flow is bands")
	}
	c.Flow = "stack"
	if useConfig(c); !stackFlow {
		t.Error("flow = stack not used")
	}
}

// A branch that doesn't fit loses its leading "/" segments, one at a time, and gets a "…" in front
// of what is left; only when even the last segment (with the …) doesn't fit is that cut at its tail,
// without a second ellipsis; a branch without "/" is cut at its tail as ever.
func TestBranchOmitsPrefix(t *testing.T) {
	for _, c := range []struct {
		branch string
		w      int // the line's width (the icon takes 2)
		want   string
	}{
		{"hotfix/alert-group", 2 + 18, "hotfix/alert-group"}, // just fits: nothing omitted
		{"hotfix/alert-group", 2 + 17, "…alert-group"},       // one cell short
		{"hotfix/alert-group", 2 + 12, "…alert-group"},       // …alert-group is 12
		{"hotfix/alert-group", 2 + 11, "alert-group"},        // the last segment alone: no leading …
		{"hotfix/alert-group", 2 + 10, "alert-gro…"},         // and cut at its tail
		{"a/bb/ccc", 2 + 8, "a/bb/ccc"},
		{"a/bb/ccc", 2 + 7, "…bb/ccc"},
		{"a/bb/ccc", 2 + 6, "…ccc"},
		{"a/bb/ccc", 2 + 4, "…ccc"},
		{"a/bb/ccc", 2 + 3, "ccc"},
		{"a/bb/ccc", 2 + 2, "c…"},
		{"feature-permission-dashboard", 2 + 12, "feature-per…"}, // no "/": the tail goes
		{"main", 2 + 4, "main"},
	} {
		got := ansi.Strip(line2(store.Session{Branch: c.branch}, c.w, nil))
		if want := branchIcon + c.want; got != want {
			t.Errorf("%q in %d: %q, want %q", c.branch, c.w, got, want)
		}
	}
	// the same with an agent at the right edge: the branch has what is left of it
	got := ansi.Strip(line2(store.Session{Branch: "hotfix/alert-group", Agent: "orch"}, 2+12+1+4, nil))
	if got != branchIcon+"…alert-group orch" {
		t.Errorf("with an agent: %q", got)
	}
	// highlights follow the characters that are shown: those in the omitted part are not drawn, the
	// others move with their text (and the … in front is not one)
	prevP := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prevP) })
	s := store.Session{Branch: "hotfix/alert-group"}
	if got, want := line2(s, 2+12, []int{7, 8, 9, 10, 11}), hilite(branchIcon+"…alert-group", []int{3, 4, 5, 6, 7}, cBranch); got != want {
		t.Errorf("highlight on the shown part: %q, want %q", got, want)
	}
	if got, want := line2(s, 2+12, []int{0, 1, 2}), fg(cBranch, branchIcon+"…alert-group"); got != want {
		t.Errorf("highlight on the omitted part: %q, want %q", got, want)
	}
}
