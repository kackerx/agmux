package board

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

const (
	minColW  = 40
	colGap   = 2
	cardH    = 5
	barCells = avatar.Width // the bar sits right under the avatar, same width
)

// maxCols is the most columns of cards; wider screens get wider cards, not more columns.
// useConfig sets it from [layout] max_columns.
var maxCols = 4

// Colors; defaults match claude-hud's segments (ANSI slots) and ccmux's palette, and
// useConfig replaces them with config.toml's [colors].
var (
	cName     = lipgloss.Color("5")
	cModel    = lipgloss.Color("14")
	cEffort   = lipgloss.Color("13")
	cDir      = lipgloss.Color("11")
	cBranch   = lipgloss.Color("12")
	cMuted    = lipgloss.Color("#6272a4") // secondary text
	cWorking  = lipgloss.Color("#ffb86c")
	cDone     = lipgloss.Color("#50fa7b")
	cRed      = lipgloss.Color("#ff6188") // attention / waiting, errors
	cDim      = lipgloss.Color("#585b70") // idle border, preview box
	cGreen    = lipgloss.Color("#a6e3a1") // context bar < 30%
	cYellow   = lipgloss.Color("#f9e2af") // 30–60%
	cBarHigh  = lipgloss.Color("#ff6188") // ≥ 60%
	cBarEmpty = lipgloss.Color("#585b70")
	cFocus    = lipgloss.Color("#ff8ebe")
	cBorder   = lipgloss.Color("")        // every card's border but the focused one's; empty: follows the status
	cMatch    = lipgloss.Color("#f2d06b") // search hits: this background, dark text, like nvim's Search
	cOff      = lipgloss.Color("#45475a") // disabled footer keys
)

// useConfig applies config.toml: colors, and the footer built from the key bindings.
func useConfig(c *config.Config) {
	for name, v := range map[string]*lipgloss.Color{
		"working": &cWorking, "idle": &cDim, "done": &cDone, "attention": &cRed, "focus": &cFocus, "match": &cMatch, "border": &cBorder,
		"name": &cName, "branch": &cBranch, "model": &cModel, "effort": &cEffort, "dir": &cDir,
		"bar_low": &cGreen, "bar_mid": &cYellow, "bar_high": &cBarHigh, "bar_empty": &cBarEmpty,
		"muted": &cMuted, "disabled": &cOff,
	} {
		*v = lipgloss.Color(c.Colors[name])
	}
	avatar.AlertColor = cRed
	maxCols = c.MaxColumns
	footerKeys = footerFor(c)
}

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

// rect is where a card sits in the body, in cells; band is which row of groups it is in.
type rect struct {
	id         string
	x, y, w, h int
	band       int
}

// headerID is the focus ID of a folded group's title; session IDs never start with NUL.
func headerID(key string) string { return "\x00" + key }

// layout renders the body and reports the position of every focusable thing: cards, and the
// titles of folded groups (for focus scrolling and mouse hits).
func layout(gs []group, width int, focus string, frame int, now time.Time, folded map[string]bool, hits map[string]hit) ([]string, []rect) {
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
				rects = append(rects, rect{headerID(g.key), x, top, colW, 1, start / n})
				cols[i] = append(cols[i], header(g, colW, true, focus == headerID(g.key), dirHits(g, hits)))
				height = max(height, 1)
				continue
			}
			cols[i] = append(cols[i], header(g, colW, false, false, dirHits(g, hits)))
			for _, s := range g.sessions {
				rects = append(rects, rect{s.ID, x, top + len(cols[i]), colW, cardH, start / n})
				cols[i] = append(cols[i], strings.Split(card(s, colW, cardH, s.ID == focus, frame, now, hits[s.ID]), "\n")...)
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

// hit is where the / filter matched a card: rune offsets into its name, branch and tildePath(cwd).
type hit struct{ name, branch, dir []int }

// dirHits: the group title shows every match in its path, from any of its cards.
func dirHits(g group, hits map[string]hit) []int {
	var pos []int
	for _, s := range g.sessions {
		pos = append(pos, hits[s.ID].dir...)
	}
	return pos
}

// matchText is the text on a search hit's background: dark, whatever the match color is.
const matchText = lipgloss.Color("#2d2a2e")

// hit draws s as a search hit: the match color behind dark text.
func matched(s string) string {
	return lipgloss.NewStyle().Background(cMatch).Foreground(matchText).Render(s)
}

// hilite draws text in c, the grapheme clusters holding a rune at pos (rune offsets) as search hits.
func hilite(text string, pos []int, c lipgloss.Color) string {
	if len(pos) == 0 {
		return fg(c, text)
	}
	var b strings.Builder
	i := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		n := len(g.Runes())
		if slices.ContainsFunc(pos, func(p int) bool { return p >= i && p < i+n }) {
			b.WriteString(matched(g.Str()))
		} else {
			b.WriteString(fg(c, g.Str()))
		}
		i += n
	}
	return b.String()
}

// header is a group's title: ▾ when open, ▸ when folded (then it can hold the focus).
func header(g group, w int, folded, focused bool, hl []int) string {
	count := fmt.Sprintf(" (%d)", len(g.sessions))
	mark, c := "▾ ", cDir
	if folded {
		mark = "▸ "
	}
	if focused {
		c = cFocus
	}
	return fg(c, mark) + midTrunc(hilite(tildePath(g.key), hl, c), w-2-len(count)) + fg(cMuted, count)
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
	tail := (w - 1) / 2
	head := w - 1 - tail
	return ansi.Truncate(s, head, "") + "…" + ansi.TruncateLeft(s, ansi.StringWidth(s)-tail, "")
}

// card renders a session as w×h cells. h is the full height including the border;
// S1 only draws 5 (3 body lines), smaller heights keep the first h-2 lines.
//
//	╭──────────────────────────────────────────────╮
//	│ ▄▀▀▀▀▄  qa-acceptance-pap-be-dev          3h │   avatar · name · time
//	│ ▀▄▄▄▄▀   feature/qa-acceptance-pap-be-dev   │   branch
//	│ ■■■□□□  ⏸ idle · Bash  ⚇ 2   Opus 5.5·high │   context bar · status + detail · counts · model·effort
//	╰──────────────────────────────────────────────╯
func card(s store.Session, w, h int, focused bool, frame int, now time.Time, hl hit) string {
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
	if cBorder != "" { // one color for them all: the status shows in the status line and the avatar
		border = cBorder
	}
	bs := lipgloss.RoundedBorder()
	if focused { // a thick line in its own color: a status-colored thick line was too easy to miss
		bs, border = lipgloss.ThickBorder(), cFocus
	}
	face := avatarFace(s, mood, frame)

	x := w - 4                 // inside the border and its one-cell margins
	tw := x - avatar.Width - 2 // right of the avatar
	body := []string{
		face[0] + "  " + line1(s, tw, now, hl.name),
		face[1] + "  " + line2(s, tw, hl.branch),
		line3(s, x),
	}
	b := fg(border, bs.TopLeft+strings.Repeat(bs.Top, w-2)+bs.TopRight)
	for i := range min(h-2, len(body)) {
		b += "\n" + fg(border, bs.Left) + " " + pad(ansi.Truncate(body[i], x, "…"), x) + " " + fg(border, bs.Right)
	}
	return b + "\n" + fg(border, bs.BottomLeft+strings.Repeat(bs.Bottom, w-2)+bs.BottomRight)
}

// spread puts right flush against the right edge and gives left whatever is left (keeping one
// cell between them when there is a right side at all).
func spread(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	if rw > 0 {
		rw++
	}
	left = ansi.Truncate(left, max(w-rw, 0), "…")
	return pad(left, w-ansi.StringWidth(right)) + right
}

// line1: the name, the time at the right edge; the name is cut only when it doesn't fit.
func line1(s store.Session, w int, now time.Time, hl []int) string {
	return spread(hilite(s.Name, hl, cName), fg(cMuted, relTime(s.LastActivity, now)), w)
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

// line2: the branch, cut when it doesn't fit.
func line2(s store.Session, w int, hl []int) string {
	if s.Branch == "" {
		return ""
	}
	shifted := make([]int, len(hl))
	for i, p := range hl {
		shifted[i] = p + 2 // past the icon and its space
	}
	return ansi.Truncate(hilite("\ue725 "+s.Branch, shifted, cBranch), w, fg(cBranch, "…")) // nf-dev-git_branch
}

// line3: context bar, the status word and its detail, subagent / shell counts, model·effort at the
// right edge. The bar and status word always show; when space runs out the detail goes first,
// then the counts, then the model is cut.
func line3(s store.Session, w int) string {
	word, detail := status(s)
	left := contextBar(s.ContextPct) + "  " + word
	room := w - ansi.StringWidth(left)

	model := ""
	if m := modelName(s.Model); m != "" {
		model = fg(cModel, m)
		if s.Effort != "" {
			model += fg(cMuted, "·") + fg(cEffort, s.Effort)
		}
	}
	// two cells before the model, or `⚇ 2 Opus 5.5` reads as two Opuses
	if mw := ansi.StringWidth(model); mw+2 > room { // last to go: cut the model to what is left
		model = ansi.Truncate(model, max(room-2, 0), "…")
	}
	if model != "" {
		room -= ansi.StringWidth(model) + 2 // what's left of the model keeps ≥ 2 cells away from it
	}

	var counts []string
	if n := len(s.Subagents); n > 0 {
		counts = append(counts, fmt.Sprintf("⚇ %d", n))
	}
	if s.Shells > 0 {
		counts = append(counts, fmt.Sprintf("⚙ %d", s.Shells))
	}
	c := ""
	if len(counts) > 0 {
		c = "  " + strings.Join(counts, " ")
	}
	if ansi.StringWidth(c) > room {
		c = ""
	}
	room -= ansi.StringWidth(c)

	if ansi.StringWidth(detail) > room {
		full := detail
		detail = ""
		if room >= 5 { // " · " plus at least one character and "…"
			detail = ansi.Truncate(full, room, "…")
		}
	}
	// the detail is the muted tool name, or "agents" (no pending tool) in the working color with the counts
	dc := cMuted
	if s.PendingTool == "" {
		dc = cWorking
	}
	return spread(left+fg(dc, detail)+fg(cWorking, c), model, w)
}

// contextBar draws claude-hud's ■ bar, no number; pct<0 = unknown (all dark).
func contextBar(pct int) string {
	if pct < 0 {
		return fg(cBarEmpty, strings.Repeat("■", barCells))
	}
	c := cGreen
	switch {
	case pct >= 60:
		c = cBarHigh
	case pct >= 30:
		c = cYellow
	}
	filled := min((pct*barCells+50)/100, barCells)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	return fg(c, strings.Repeat("■", filled)) + fg(cBarEmpty, strings.Repeat("■", barCells-filled))
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

type footerKey struct {
	text     string
	needPane bool
}

// footerKeys is the key line, each action shown with its first configured key.
var footerKeys = footerFor(config.Default())

func footerFor(c *config.Config) []footerKey {
	var out []footerKey
	add := func(needPane bool, text string, actions ...string) {
		keys := make([]any, len(actions))
		for i, a := range actions {
			if keys[i] = c.First(a); keys[i] == "" { // an action left with no key isn't shown
				return
			}
		}
		out = append(out, footerKey{fmt.Sprintf(text, keys...), needPane})
	}
	add(false, "%s/%s %s/%s move", "down", "up", "left", "right")
	add(true, "%s jump", "jump")
	add(false, "%s search", "search")
	add(false, "%s flash", "flash")
	add(false, "%s fold", "fold")
	add(false, "%s preview", "preview")
	add(true, "%s focus preview", "focus_preview")
	add(true, "%s reply", "reply")
	add(true, "%s handoff", "handoff")
	add(false, "%s restart", "restart")
	add(false, "%s kill", "kill")
	add(false, "%s review", "review")
	add(false, "%s avatar", "avatar")
	add(false, "%s quit", "quit")
	return out
}

// The picker is a grid of avatars below the card it is for: gridCols across, at most gridRows
// down (more rows scroll), each cell cellW × cellH: the avatar (6 × 2) with its name under it.
const (
	gridCols, gridRows = 5, 4
	cellW, cellH       = 10, 3              // 6 for the avatar, two spaces either side: an 8 character name (aza_choi, sprocket) keeps some air around it
	gridW              = gridCols*cellW + 2 // with the border
)

// avatarGrid draws the picker for s showing rows rows of the grid, the ones around the cursor pick.
// Under the cursor the name is in the focus color's inverse; the avatar s wears now has its name in
// the focus color.
func avatarGrid(s store.Session, pick, rows int) []string {
	ids := avatar.Listing()
	total := (len(ids) + gridCols - 1) / gridCols
	rows = min(rows, total)
	first := min(max(pick/gridCols-rows/2, 0), total-rows)
	var lines []string
	for r := first; r < first+rows; r++ {
		cells := [cellH][]string{}
		for c := range gridCols {
			i := r*gridCols + c
			if i >= len(ids) {
				for l := range cellH {
					cells[l] = append(cells[l], strings.Repeat(" ", cellW))
				}
				continue
			}
			id := ids[i]
			face := gridFace(id, s.AgentType)
			label := ansi.Truncate(avatarName(id), cellW, "…")
			label = pad(strings.Repeat(" ", (cellW-ansi.StringWidth(label))/2)+label, cellW)
			switch {
			case i == pick:
				label = lipgloss.NewStyle().Foreground(lipgloss.Color("#2d2a2e")).Background(cFocus).Render(label)
			case id == s.AvatarID:
				label = fg(cFocus, label)
			}
			cells[0] = append(cells[0], "  "+face[0]+"  ")
			cells[1] = append(cells[1], "  "+face[1]+"  ")
			cells[2] = append(cells[2], label)
		}
		for l := range cellH {
			lines = append(lines, strings.Join(cells[l], ""))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFocus).Render(strings.Join(lines, "\n"))
	return strings.Split(box, "\n")
}

// gridPlace picks where the grid goes for a card whose top is at screen row cardTop and which is
// cardH tall, on a screen h rows high (the top bar above row 1, the message line and footer from
// h-2 on): straight below the card, else above it, with fewer rows if need be. rows is how many it
// shows.
func gridPlace(cardTop, cardH, h, want int) (y, rows int) {
	below, above := h-2-(cardTop+cardH), cardTop-1
	for rows = want; rows >= 1; rows-- {
		if need := 2 + cellH*rows; below >= need {
			return cardTop + cardH, rows
		} else if above >= need {
			return cardTop - need, rows
		}
	}
	return 1, 1 // no room at all: the top of the board
}

// overlayAt puts box on screen with its top left corner at column x, row y.
func overlayAt(screen string, box []string, x, y, w int) string {
	lines := strings.Split(screen, "\n")
	bw := min(ansi.StringWidth(box[0]), w-x)
	for i, b := range box {
		if y+i >= len(lines) {
			break
		}
		lines[y+i] = put(lines[y+i], x, ansi.Truncate(b, bw, ""))
	}
	return strings.Join(lines, "\n")
}

// overlay draws box centered over screen (w columns), the screen showing on both sides.
func overlay(screen string, box []string, w int) string {
	lines := strings.Split(screen, "\n")
	bw := min(ansi.StringWidth(box[0]), w)
	x, y := (w-bw)/2, max((len(lines)-len(box))/2, 0)
	for i, b := range box {
		if y+i >= len(lines) {
			break
		}
		lines[y+i] = put(lines[y+i], x, ansi.Truncate(b, bw, ""))
	}
	return strings.Join(lines, "\n")
}

// put draws s over line from column x.
func put(line string, x int, s string) string {
	end := x + ansi.StringWidth(s)
	right := ansi.TruncateLeft(line, end, "")
	if ansi.StringWidth(right) > max(ansi.StringWidth(line)-end, 0) { // a wide character straddles the cut: a space for its half
		right = " " + ansi.TruncateLeft(line, end+1, "")
	}
	// resets: an SGR left open where the line is cut must not color s, nor s the rest
	return pad(ansi.Truncate(line, x, ""), x) + "\x1b[0m" + s + "\x1b[0m" + right
}

// scrollOff is how many body lines scroll off the top: top when the wheel or the page keys set it
// (top >= 0; held inside the bodyLen lines), else just enough for the focused card to show in bodyH lines.
func scrollOff(rects []rect, focus string, bodyH, bodyLen, top int) int {
	if top >= 0 {
		return min(top, max(bodyLen-bodyH, 0))
	}
	for _, r := range rects {
		if r.id == focus && r.y+r.h > bodyH {
			return r.y + r.h - bodyH
		}
	}
	return 0
}

// flashAlphabet: home row first, as flash.tmux and sqlmux; then the same in upper case.
const flashAlphabet = "asdfghjklqwertyuiopzxcvbnmASDFGHJKLQWERTYUIOPZXCVBNM"

// flashMatch is a place the flash pattern is on screen: the text of a card's name or branch, or
// of a group's path, from column x of body line y, w cells wide. id is where the focus goes when
// its label is pressed; label is "" when there are no letters left for it.
type flashMatch struct {
	id      string
	title   bool // in a group's path (its card's own hit, if any, is another match)
	y, x, w int
	text    string // the hit as drawn
	label   string
}

// key tells a match from the others of the same card.
func (f flashMatch) key() string { return fmt.Sprintf("%s/%v", f.id, f.title) }

// cluster is a grapheme as drawn: its text, the column it starts at in its string, its width.
type cluster struct {
	s    string
	x, w int
}

func clustersOf(text string) []cluster {
	var cs []cluster
	x := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		cs = append(cs, cluster{g.Str(), x, g.Width()})
		x += g.Width()
	}
	return cs
}

// findAll is where pat (lower case) starts in cs, ignoring case: every start, overlapping ones too.
func findAll(cs []cluster, pat []rune) []int {
	var at []int
	for i := 0; i+len(pat) <= len(cs); i++ {
		ok := true
		for j, p := range pat {
			r := []rune(cs[i+j].s)
			if len(r) != 1 || unicode.ToLower(r[0]) != p { // a multi-rune grapheme is never a typed character
				ok = false
				break
			}
		}
		if ok {
			at = append(at, i)
		}
	}
	return at
}

// flashScan reads the pattern off the board as drawn, as flash.nvim does: in each visible card's
// name or branch, and in each visible group's path, the first hit; labels go to the hits nearest
// the focused card first. A label is never the character after any hit (not only the ones taken),
// or typing on would be taken for pressing it. off and bodyH are the window onto the body;
// rects are the cards and folded titles as laid out for width cardsW.
func flashScan(gs []group, rects []rect, focus, pat string, off, bodyH, cardsW int, now time.Time, prev []flashMatch, avoid map[string]bool) []flashMatch {
	p := []rune(pat)
	for i, r := range p {
		p[i] = unicode.ToLower(r)
	}
	if len(p) == 0 {
		return nil
	}
	at := map[string]rect{}
	for _, r := range rects {
		at[r.id] = r
	}
	var ms []flashMatch
	skip := map[rune]bool{}
	found := false
	scan := func(text string, x0, y int, id string, title bool) {
		if y < 0 || y >= bodyH {
			return
		}
		cs := clustersOf(text)
		for _, i := range findAll(cs, p) {
			if j := i + len(p); j < len(cs) {
				if r := []rune(cs[j].s); len(r) == 1 {
					skip[unicode.ToLower(r[0])] = true
				}
			}
			if found {
				continue
			}
			found = true
			last := cs[i+len(p)-1]
			ms = append(ms, flashMatch{id: id, title: title, y: y, x: x0 + cs[i].x, w: last.x + last.w - cs[i].x, text: joinClusters(cs[i : i+len(p)])})
		}
	}
	icon := ansi.StringWidth("\ue725 ")
	for _, g := range gs {
		count := fmt.Sprintf(" (%d)", len(g.sessions))
		if hr, ok := at[headerID(g.key)]; ok { // folded: the title is the target
			found = false
			scan(midTrunc(tildePath(g.key), hr.w-2-len(count)), hr.x+2, hr.y-off, hr.id, true)
			continue
		}
		for i, s := range g.sessions {
			r, ok := at[s.ID]
			if !ok {
				continue
			}
			if i == 0 { // an open group's title: the first card is where it goes
				found = false
				scan(midTrunc(tildePath(g.key), r.w-2-len(count)), r.x+2, r.y-1-off, s.ID, true)
			}
			tw := r.w - 4 - avatar.Width - 2
			x0, y := r.x+2+avatar.Width+2, r.y-off
			found = false
			rw := 0
			if t := relTime(s.LastActivity, now); t != "" {
				rw = ansi.StringWidth(t) + 1
			}
			scan(ansi.Truncate(s.Name, max(tw-rw, 0), "…"), x0, y+1, s.ID, false)
			if s.Branch != "" {
				scan(strings.TrimPrefix(ansi.Truncate("\ue725 "+s.Branch, tw, "…"), "\ue725 "), x0+icon, y+2, s.ID, false)
			}
		}
	}
	cur := at[focus]
	dist := func(m flashMatch) int { return abs(m.y-(cur.y-off))*cardsW + abs(m.x-cur.x) }
	slices.SortStableFunc(ms, func(a, b flashMatch) int {
		if d := dist(a) - dist(b); d != 0 {
			return d
		}
		if a.y != b.y {
			return a.y - b.y
		}
		return a.x - b.x
	})
	// a hit that was labelled before keeps its label if it may still have it (the cards moved or
	// went, the pattern didn't): what was drawn is what stays pressable
	used := map[string]bool{}
	was := map[string]string{}
	drawn := map[string]bool{} // every letter that was on screen: a new hit never takes one a user saw on another
	for _, p := range prev {
		was[p.key()] = p.label
		drawn[p.label] = true
	}
	for i := range ms {
		if l := was[ms[i].key()]; l != "" && !skip[unicode.ToLower([]rune(l)[0])] && !used[l] {
			ms[i].label, used[l] = l, true
		}
	}
	var free []string
	for _, r := range flashAlphabet {
		if l := string(r); !skip[unicode.ToLower(r)] && !used[l] && !avoid[l] && !drawn[l] {
			free = append(free, l)
		}
	}
	for i := range ms {
		if ms[i].label == "" && len(free) > 0 {
			ms[i].label, free = free[0], free[1:]
		}
	}
	return ms
}

func joinClusters(cs []cluster) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.s)
	}
	return b.String()
}

func abs(n int) int { return max(n, -n) }

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

// view assembles top bar, body (scrolled so the focused card is visible, or to pin >= 0 if the
// wheel set it), message line and footer. The cards get the first cw columns; right (the preview,
// one line per body row) fills the rest.
func view(ss []store.Session, w, h int, focus string, frame int, now time.Time, msg string, noPane bool, folded map[string]bool, cw int, right []string, hits map[string]hit, pin int) (string, []rect, int) {
	top := " " + lipgloss.NewStyle().Bold(true).Render("agmux") + fg(cMuted, fmt.Sprintf(" · %d sessions", len(ss)))
	body, rects := layout(groups(ss), cw, focus, frame, now, folded, hits)
	bodyH := max(h-3, 1) // top bar, message line, footer
	off := scrollOff(rects, focus, bodyH, len(body), pin)
	if off > 0 {
		top += fg(cMuted, " · ↑ more")
	}
	if off+bodyH < len(body) {
		top += fg(cMuted, " · ↓ more")
	}
	vis := body[min(off, len(body)):]
	vis = vis[:min(bodyH, len(vis))]
	for len(vis) < bodyH {
		vis = append(vis, "")
	}
	for i := range vis {
		if i < len(right) {
			vis[i] = pad(vis[i], cw) + right[i]
		}
	}
	out := append([]string{top}, vis...)
	// ccmux stderr is often multi-line; one line keeps the screen exactly h tall
	out = append(out, ansi.Truncate(firstLine(msg), w, "…"), footer(noPane, w))
	return strings.Join(out, "\n"), rects, off
}

// flashLabel: like a yank highlight, the focus color behind dark text (light text on a dark focus color).
func flashLabel(l string) string {
	text := lipgloss.Color("#282a36")
	if r, g, b, _ := cFocus.RGBA(); (299*r+587*g+114*b)/1000 < 0x8000 {
		text = "#f8f8f2"
	}
	return lipgloss.NewStyle().Background(cFocus).Foreground(text).Bold(true).Render(l)
}
