package board

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/store"
)

const (
	minColW  = 56
	colGap   = 2
	cardH    = 5
	barCells = 10
)

// Colors: ANSI slots match claude-hud's segments, so they follow the terminal theme like the HUD does;
// hex values are ccmux's picker palette and claude-hud's context bar.
var (
	cName       = lipgloss.Color("5")  // claude-hud session name (35)
	cModel      = lipgloss.Color("14") // hud model segment
	cEffort     = lipgloss.Color("13")
	cDir        = lipgloss.Color("11") // hud directory icon
	cBranch     = lipgloss.Color("12") // hud git segment
	cMuted      = lipgloss.Color("#6272a4")
	cWorking    = lipgloss.Color("#ffb86c")
	cDone       = lipgloss.Color("#50fa7b")
	cRed        = lipgloss.Color("#ff6188")
	cAlertFocus = lipgloss.Color("#ffb3c4") // focused card that needs attention: red, but brighter
	cDim        = lipgloss.Color("#585b70")
	cGreen      = lipgloss.Color("#a6e3a1")
	cYellow     = lipgloss.Color("#f9e2af")
	cFocus      = lipgloss.Color("#f8f8f2")
	cOff        = lipgloss.Color("#3b3d4d") // disabled footer keys (ccmux's separator color)
)

func fg(c lipgloss.Color, s string) string { return lipgloss.NewStyle().Foreground(c).Render(s) }

// columns returns how many columns fit in width w and how wide each is.
func columns(w int) (n, colW int) {
	n = max((w+colGap)/(minColW+colGap), 1)
	return n, (w - colGap*(n-1)) / n
}

type group struct {
	key      string
	sessions []store.Session
}

// groups splits sessions by GroupKey, sorted by key; card order inside a group is ccmux's.
func groups(ss []store.Session) []group {
	var gs []group
	idx := map[string]int{}
	for _, s := range ss {
		i, ok := idx[s.GroupKey]
		if !ok {
			i = len(gs)
			idx[s.GroupKey] = i
			gs = append(gs, group{key: s.GroupKey})
		}
		gs[i].sessions = append(gs[i].sessions, s)
	}
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].key < gs[j].key })
	return gs
}

// rect is where a card sits in the body, in cells.
type rect struct {
	id         string
	x, y, w, h int
}

// layout renders the body and reports every card's position (for focus scrolling and mouse hits).
func layout(gs []group, width int, focus string, frame int, now time.Time) ([]string, []rect) {
	n, colW := columns(width)
	var lines []string
	var rects []rect
	for start := 0; start < len(gs); start += n {
		row := gs[start:min(start+n, len(gs))]
		if start > 0 {
			lines = append(lines, "")
		}
		top := len(lines)
		cols := make([][]string, len(row))
		height := 0
		for i, g := range row {
			x := i * (colW + colGap)
			cols[i] = append(cols[i], header(g, colW))
			for _, s := range g.sessions {
				rects = append(rects, rect{s.ID, x, top + len(cols[i]), colW, cardH})
				cols[i] = append(cols[i], strings.Split(card(s, colW, cardH, s.ID == focus, frame, now), "\n")...)
			}
			height = max(height, len(cols[i]))
		}
		for y := range height {
			var b strings.Builder
			for i, col := range cols {
				l := ""
				if y < len(col) {
					l = col[y]
				}
				if i < len(cols)-1 {
					l = pad(l, colW+colGap)
				}
				b.WriteString(l)
			}
			lines = append(lines, b.String())
		}
	}
	return lines, rects
}

func pad(s string, w int) string { return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) }

func header(g group, w int) string {
	count := fmt.Sprintf(" (%d)", len(g.sessions))
	return fg(cDir, "▾ "+midTrunc(tildePath(g.key), w-2-len(count))) + fg(cMuted, count)
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return "~/" + rel
		}
	}
	return p
}

// midTrunc keeps both ends of a path: ~/ProjectsFolder/…/rename-flag.
func midTrunc(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w < 3 {
		return ansi.Truncate(s, w, "")
	}
	r := []rune(s)
	tail := (w - 1) / 2
	head := w - 1 - tail
	return ansi.Truncate(string(r[:min(head, len(r))]), head, "") + "…" + ansi.TruncateLeft(s, ansi.StringWidth(s)-tail, "")
}

// card renders a session as w×h cells. h is the full height including the border;
// S1 only draws 5 (3 body lines), smaller heights keep the first h-2 lines.
func card(s store.Session, w, h int, focused bool, frame int, now time.Time) string {
	border := cDim
	alert := s.Attention != "" || s.Status == "waiting"
	switch {
	case alert && focused:
		border = cAlertFocus
	case alert:
		border = cRed
	case focused:
		border = cFocus
	}
	inner := w - 2
	tw := inner - 10 // " " + avatar(6) + "  " … " "

	mood := avatar.Normal
	switch {
	case alert:
		mood = avatar.Alert
	case s.Status == "working":
		mood = avatar.Scan
	case s.Status == "idle" && !s.Done:
		mood = avatar.Dim
	}
	sp, _ := avatar.ByID(s.AvatarID)
	face := strings.Split(avatar.Render(sp, s.AgentType, mood, frame), "\n")

	body := []string{line1(s, tw, now), line2(s, tw), line3(s, tw)}
	b := fg(border, "╭"+strings.Repeat("─", inner)+"╮")
	for i := range min(h-2, len(body)) {
		b += "\n" + fg(border, "│") + " " + face[i] + "  " + pad(ansi.Truncate(body[i], tw, "…"), tw) + " " + fg(border, "│")
	}
	return b + "\n" + fg(border, "╰"+strings.Repeat("─", inner)+"╯")
}

// spread puts right flush against the right edge and gives left whatever is left.
func spread(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	left = ansi.Truncate(left, max(w-rw-1, 0), "…")
	return pad(left, w-rw) + right
}

func line1(s store.Session, w int, now time.Time) string {
	ago := fg(cMuted, relTime(s.LastActivity, now))
	avail := w - ansi.StringWidth(ago) - 1
	name := ansi.Truncate(s.Name, avail, "…")
	left := fg(cName, name)
	if room := avail - ansi.StringWidth(name) - 2; s.Branch != "" && room >= 5 { // branch gives way first
		left += "  " + fg(cBranch, ansi.Truncate("⑂ "+s.Branch, room, "…"))
	}
	return spread(left, ago, w)
}

func line2(s store.Session, w int) string {
	var st string
	switch {
	case s.Status == "working":
		st = fg(cWorking, "⏵ working")
		if s.PendingTool != "" {
			st += fg(cMuted, " · "+s.PendingTool)
		} else if s.AgentsWorking {
			st += fg(cMuted, " · agents")
		}
	case s.Status == "waiting":
		st = fg(cRed, "⏳ waiting")
	case s.Done:
		st = fg(cDone, "● done")
	case s.Status == "idle":
		st = fg(cMuted, "⏸ idle")
	default:
		st = fg(cMuted, "● "+s.Status)
	}
	if n := len(s.Subagents); n > 0 {
		st += "  " + fg(cMuted, fmt.Sprintf("⚇ %d %s", n, plural(n, "agent")))
	}
	if s.Shells > 0 {
		st += "  " + fg(cMuted, fmt.Sprintf("⚙ %d %s", s.Shells, plural(s.Shells, "shell")))
	}
	return spread(st, contextBar(s.ContextPct), w)
}

func line3(s store.Session, w int) string {
	right := ""
	if m := modelName(s.Model); m != "" {
		right = fg(cModel, m)
		if s.Effort != "" {
			right += fg(cMuted, " · ") + fg(cEffort, s.Effort)
		}
	}
	left := ""
	if s.Detail != "" {
		left = fg(cMuted, "󰭻 "+s.Detail)
	}
	return spread(left, right, w)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// contextBar draws claude-hud's 10-cell ■ bar; pct<0 = unknown.
func contextBar(pct int) string {
	if pct < 0 {
		return fg(cDim, strings.Repeat("■", barCells)+" --")
	}
	c := cGreen
	switch {
	case pct >= 60:
		c = cRed
	case pct >= 30:
		c = cYellow
	}
	filled := min((pct*barCells+50)/100, barCells)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	return fg(c, strings.Repeat("■", filled)) + fg(cDim, strings.Repeat("■", barCells-filled)) + fg(c, fmt.Sprintf(" %d%%", pct))
}

// modelName turns claude-opus-5-5 into "Opus 5.5" and drops date suffixes; other models pass through.
func modelName(m string) string {
	rest, ok := strings.CutPrefix(m, "claude-")
	if !ok {
		return m
	}
	parts := strings.Split(rest, "-")
	var ver []string
	for _, p := range parts[1:] {
		if len(p) >= 8 { // 20250929
			break
		}
		ver = append(ver, p)
	}
	name := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	if len(ver) == 0 {
		return name
	}
	return name + " " + strings.Join(ver, ".")
}

func relTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

var footerKeys = []struct {
	text     string
	needPane bool
}{
	{"j/k 卡片", false}, {"h/l 跨列", false}, {"enter 跳转", true}, {"H 交接", true}, {"s 发送", true},
	{"r 重启", false}, {"x 杀", false}, {"d review", false}, {"q 退出", false},
}

// footer is ccmux's key line; keys that need a tmux pane go dark for background sessions.
func footer(noPane bool, w int) string {
	parts := make([]string, len(footerKeys))
	for i, k := range footerKeys {
		c := cMuted
		if k.needPane && noPane {
			c = cOff
		}
		parts[i] = fg(c, k.text)
	}
	return ansi.Truncate(" "+strings.Join(parts, fg(cMuted, " · ")), w, "…")
}

// view assembles top bar, body (scrolled so the focused card is visible), message line and footer.
func view(ss []store.Session, w, h int, focus string, frame int, now time.Time, msg string, noPane bool) (string, []rect, int) {
	top := " " + lipgloss.NewStyle().Bold(true).Render("agmux") + fg(cMuted, fmt.Sprintf(" · %d sessions", len(ss)))
	body, rects := layout(groups(ss), w, focus, frame, now)
	bodyH := max(h-3, 1) // top bar, message line, footer
	off := 0
	for _, r := range rects {
		if r.id == focus && r.y+r.h > bodyH {
			off = r.y + r.h - bodyH
		}
	}
	vis := body[min(off, len(body)):]
	vis = vis[:min(bodyH, len(vis))]
	for len(vis) < bodyH {
		vis = append(vis, "")
	}
	out := append([]string{top}, vis...)
	// ccmux stderr is often multi-line; one line keeps the screen exactly h tall
	out = append(out, ansi.Truncate(firstLine(msg), w, "…"), footer(noPane, w))
	return strings.Join(out, "\n"), rects, off
}
