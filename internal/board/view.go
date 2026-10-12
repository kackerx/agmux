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
	minColW  = 40
	maxCols  = 4 // wider screens get wider cards, not more columns
	colGap   = 2
	cardH    = 5
	barCells = avatar.Width // the bar sits right under the avatar, same width
)

// Colors: ANSI slots match claude-hud's segments, so they follow the terminal theme like the HUD does;
// hex values are ccmux's picker palette and claude-hud's context bar.
var (
	cName    = lipgloss.Color("5")  // claude-hud session name (35)
	cModel   = lipgloss.Color("14") // hud model segment
	cEffort  = lipgloss.Color("13")
	cDir     = lipgloss.Color("11") // hud directory icon
	cBranch  = lipgloss.Color("12") // hud git segment
	cMuted   = lipgloss.Color("#6272a4")
	cWorking = lipgloss.Color("#ffb86c")
	cDone    = lipgloss.Color("#50fa7b")
	cRed     = lipgloss.Color("#ff6188")
	cDim     = lipgloss.Color("#585b70")
	cGreen   = lipgloss.Color("#a6e3a1")
	cYellow  = lipgloss.Color("#f9e2af")
	cFocus   = lipgloss.Color("#ff8ebe")
	cOff     = lipgloss.Color("#45475a") // disabled footer keys: visible on #221e1e, darker than cMuted
)

func fg(c lipgloss.Color, s string) string { return lipgloss.NewStyle().Foreground(c).Render(s) }

// columns returns how many columns fit in width w and how wide each is.
func columns(w int) (n, colW int) {
	n = min(max((w+colGap)/(minColW+colGap), 1), maxCols)
	return n, (w - colGap*(n-1)) / n
}

type group struct {
	key      string
	sessions []store.Session
}

// groups splits sessions by GroupKey. The order comes only from the data (groups by key,
// cards by name then id), never from ccmux's array order, so polls don't shuffle the board.
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
	sort.Slice(gs, func(i, j int) bool { return gs[i].key < gs[j].key })
	for _, g := range gs {
		sort.Slice(g.sessions, func(i, j int) bool {
			a, b := g.sessions[i], g.sessions[j]
			return a.Name < b.Name || a.Name == b.Name && a.ID < b.ID
		})
	}
	return gs
}

// rect is where a card sits in the body, in cells.
type rect struct {
	id         string
	x, y, w, h int
}

// headerID is the focus ID of a folded group's title; session IDs never start with NUL.
func headerID(key string) string { return "\x00" + key }

// layout renders the body and reports the position of every focusable thing: cards, and the
// titles of folded groups (for focus scrolling and mouse hits).
func layout(gs []group, width int, focus string, frame int, now time.Time, folded map[string]bool) ([]string, []rect) {
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
			if folded[g.key] {
				rects = append(rects, rect{headerID(g.key), x, top, colW, 1})
				cols[i] = append(cols[i], header(g, colW, true, focus == headerID(g.key)))
				height = max(height, 1)
				continue
			}
			cols[i] = append(cols[i], header(g, colW, false, false))
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

// header is a group's title: ▾ when open, ▸ when folded (then it can hold the focus).
func header(g group, w int, folded, focused bool) string {
	count := fmt.Sprintf(" (%d)", len(g.sessions))
	mark, c := "▾ ", cDir
	if folded {
		mark = "▸ "
	}
	if focused {
		c = cFocus
	}
	return fg(c, mark+midTrunc(tildePath(g.key), w-2-len(count))) + fg(cMuted, count)
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
//
//	╭──────────────────────────────────────────╮
//	│ ▄▀▀▀▀▄   mods-tmux          ⏸ idle  2h │   avatar · agent icon + name · status · time
//	│ ▀▄▄▄▄▀  ⑂ master          Opus 5.5·high │   branch · model·effort
//	│ ■■■■■■             ⚇ 2 agents  ⚙ 1 shell │   context bar (avatar-wide) · subagents · shells
//	╰──────────────────────────────────────────╯
func card(s store.Session, w, h int, focused bool, frame int, now time.Time) string {
	alert := s.Attention != "" || s.Status == "waiting"
	border, mood := cDim, avatar.Dim
	switch {
	case alert:
		border, mood = cRed, avatar.Alert
	case s.Status == "working":
		border, mood = cWorking, avatar.Scan
	case s.Done:
		border, mood = cDone, avatar.Normal
	case s.Status != "idle":
		mood = avatar.Normal
	}
	bs := lipgloss.RoundedBorder()
	if focused { // a thick line in its own color: a status-colored thick line was too easy to miss
		bs, border = lipgloss.ThickBorder(), cFocus
	}
	sp, _ := avatar.ByID(s.AvatarID)
	face := strings.Split(avatar.Render(sp, s.AgentType, mood, frame), "\n")

	x := w - 4                 // inside the border and its one-cell margins
	tw := x - avatar.Width - 2 // right of the avatar
	body := []string{
		face[0] + "  " + line1(s, tw, now),
		face[1] + "  " + line2(s, tw),
		line3(s, x),
	}
	b := fg(border, bs.TopLeft+strings.Repeat(bs.Top, w-2)+bs.TopRight)
	for i := range min(h-2, len(body)) {
		b += "\n" + fg(border, bs.Left) + " " + pad(ansi.Truncate(body[i], x, "…"), x) + " " + fg(border, bs.Right)
	}
	return b + "\n" + fg(border, bs.BottomLeft+strings.Repeat(bs.Bottom, w-2)+bs.BottomRight)
}

// spread puts right flush against the right edge and gives left whatever is left.
func spread(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	left = ansi.Truncate(left, max(w-rw-1, 0), "…")
	return pad(left, w-rw) + right
}

// agentIcons are Nerd Font codicons (3.5+), drawn in the band color.
var agentIcons = map[string]string{"claude": "\uec82", "codex": "\uec81"}

func agentIcon(agentType string) string {
	if i, ok := agentIcons[agentType]; ok {
		return i
	}
	return "\uec20" // cod-robot
}

// line1: icon + name, then the status word, its detail (pending tool / agents) and the time.
// The status word and time always show. The name gets all it needs first; the detail (MCP tool
// names run to 40+ characters) takes what is left and is dropped, " · " included, when nothing
// of it would show; only then is the name cut.
func line1(s store.Session, w int, now time.Time) string {
	word, detail := status(s)
	ago := relTime(s.LastActivity, now)
	if ago != "" {
		ago = "  " + ago
	}
	icon := agentIcon(s.AgentType)
	avail := w - ansi.StringWidth(icon) - 1 - ansi.StringWidth(word) - ansi.StringWidth(ago) - 1
	if room := avail - ansi.StringWidth(s.Name); room < ansi.StringWidth(detail) {
		full := detail
		detail = ""
		if room >= 5 { // " · " plus at least one character and "…"
			detail = ansi.Truncate(full, room, "…")
		}
	}
	left := fg(avatar.BandColor(s.AgentType), icon) + " " + fg(cName, s.Name)
	return spread(left, word+fg(cMuted, detail+ago), w) // spread cuts the name if it still doesn't fit
}

// status is the colored status word and its plain detail (" · Bash", " · agents" or "").
func status(s store.Session) (word, detail string) {
	switch {
	case s.Status == "working":
		if s.PendingTool != "" {
			detail = " · " + s.PendingTool
		} else if s.AgentsWorking {
			detail = " · agents"
		}
		return fg(cWorking, "⏵ working"), detail
	case s.Status == "waiting":
		return fg(cRed, "⏳ waiting"), ""
	case s.Done:
		return fg(cDone, "● done"), ""
	case s.Status == "idle":
		return fg(cMuted, "⏸ idle"), ""
	}
	return fg(cMuted, "● "+s.Status), ""
}

// line2: branch on the left (cut first), model·effort on the right.
func line2(s store.Session, w int) string {
	right := ""
	if m := modelName(s.Model); m != "" {
		right = fg(cModel, m)
		if s.Effort != "" {
			right += fg(cMuted, "·") + fg(cEffort, s.Effort)
		}
	}
	left := ""
	if s.Branch != "" {
		left = fg(cBranch, "⑂ "+s.Branch)
	}
	return spread(left, right, w)
}

// line3: context bar under the avatar, subagent / shell counts on the right.
func line3(s store.Session, w int) string {
	var right []string
	if n := len(s.Subagents); n > 0 {
		right = append(right, fg(cMuted, fmt.Sprintf("⚇ %d %s", n, plural(n, "agent"))))
	}
	if s.Shells > 0 {
		right = append(right, fg(cMuted, fmt.Sprintf("⚙ %d %s", s.Shells, plural(s.Shells, "shell"))))
	}
	return spread(contextBar(s.ContextPct), strings.Join(right, "  "), w)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// contextBar draws claude-hud's ■ bar, no number; pct<0 = unknown (all dark).
func contextBar(pct int) string {
	if pct < 0 {
		return fg(cDim, strings.Repeat("■", barCells))
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
	return fg(c, strings.Repeat("■", filled)) + fg(cDim, strings.Repeat("■", barCells-filled))
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
	{"j/k 卡片", false}, {"h/l 跨列", false}, {"enter 跳转", true}, {"f 折叠", false}, {"H 交接", true}, {"s 发送", true},
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
func view(ss []store.Session, w, h int, focus string, frame int, now time.Time, msg string, noPane bool, folded map[string]bool) (string, []rect, int) {
	top := " " + lipgloss.NewStyle().Bold(true).Render("agmux") + fg(cMuted, fmt.Sprintf(" · %d sessions", len(ss)))
	body, rects := layout(groups(ss), w, focus, frame, now, folded)
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
