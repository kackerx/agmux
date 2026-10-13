package board

import (
	"bytes"
	"errors"
	"fmt"
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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

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

func pngFile(s string) *fstest.MapFile { return &fstest.MapFile{Data: pngOf(s)} }

// useKit loads one set: two idle frames, two think, one read, one hi, nothing else.
func useKit(t *testing.T) {
	t.Helper()
	avatar.UseSets(fstest.MapFS{
		"kit/idle/1.png": pngFile("i1"), "kit/idle/2.png": pngFile("i2"),
		"kit/think/1.png": pngFile("t1"), "kit/think/2.png": pngFile("t2"),
		"kit/read/1.png": pngFile("r1"), "kit/hi/1.png": pngFile("h1"),
	})
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}); useImages = false })
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// Pictures are drawn only when [avatars] renderer and the terminal say so: pixel never; auto for
// kitty / ghostty (in tmux the attached client's TERM, with allow-passthrough on); image skips the
// terminal check but not the passthrough.
func TestImageMode(t *testing.T) {
	useKit(t)
	inTmux := map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}
	for _, c := range []struct {
		name     string
		renderer string
		env      map[string]string
		term     string // tmux's #{client_termname}
		pass     string // allow-passthrough, "" = the option doesn't exist
		on       bool
		note     string
	}{
		{"kitty client", "auto", inTmux, "xterm-kitty", "on", true, ""},
		{"ghostty client, passthrough all", "auto", inTmux, "xterm-ghostty", "all", true, ""},
		{"other client", "auto", inTmux, "xterm-256color", "on", false, ""},
		{"kitty, passthrough off", "auto", inTmux, "xterm-kitty", "off", false, ""},
		{"kitty, no such option (tmux < 3.3)", "auto", inTmux, "xterm-kitty", "", false, ""},
		{"TERM_PROGRAM says kitty but the client does not", "auto", map[string]string{"TMUX": "x", "TERM_PROGRAM": "kitty"}, "xterm-256color", "on", false, ""},
		{"pixel is never pictures", "pixel", inTmux, "xterm-kitty", "on", false, ""},
		{"image on a plain client", "image", inTmux, "xterm-256color", "on", true, ""},
		{"image, passthrough off: says so", "image", inTmux, "xterm-256color", "off", false, "allow-passthrough"},
		{"outside tmux: kitty TERM", "auto", map[string]string{"TERM": "xterm-kitty"}, "", "", true, ""},
		{"outside tmux: ghostty", "auto", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty"}, "", "", true, ""},
		{"outside tmux: other", "auto", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app"}, "", "", false, ""},
		{"outside tmux: image forced", "image", map[string]string{"TERM": "xterm-256color"}, "", "", true, ""},
	} {
		r := &fakeRunner{tmuxOut: map[string]string{"display-message": c.term + "\n", "show-options": c.pass + "\n"}}
		if c.pass == "" {
			r.tmuxFail = map[string]error{"show-options": errors.New("unknown option: allow-passthrough")}
		}
		on, note := imageMode(r, env(c.env), c.renderer)
		if on != c.on || (c.note == "") != (note == "") || !strings.Contains(note, c.note) {
			t.Errorf("%s: on=%v note=%q, want on=%v note containing %q", c.name, on, note, c.on, c.note)
		}
		if c.env["TMUX"] != "" && c.renderer == "auto" && c.term != "" { // the client is the one a jump would use, asked by name
			found := false
			for _, call := range r.calls {
				found = found || strings.Join(call, " ") == "tmux display-message -p -c /dev/c #{client_termname}"
			}
			if !found {
				t.Errorf("%s: client_termname not asked of the chosen client: %q", c.name, r.calls)
			}
		}
	}
	// no client to ask (none attached, or tmux says no): not kitty as far as anyone knows
	for name, r := range map[string]*fakeRunner{
		"no clients":         {tmuxOut: map[string]string{"list-clients": "", "display-message": "xterm-kitty\n", "show-options": "on\n"}},
		"list-clients fails": {tmuxFail: map[string]error{"list-clients": errors.New("no server")}, tmuxOut: map[string]string{"display-message": "xterm-kitty\n", "show-options": "on\n"}},
	} {
		if on, _ := imageMode(r, env(inTmux), "auto"); on {
			t.Errorf("%s: pictures without a client to ask", name)
		}
	}
	avatar.UseSets(fstest.MapFS{}) // no sets: nothing to draw
	if on, _ := imageMode(&fakeRunner{}, env(map[string]string{"TERM": "xterm-kitty"}), "auto"); on {
		t.Error("pictures with no image sets")
	}
}

// kitName is a session name whose blink offset is 0: its tick is the frame count.
var kitName = func() string {
	for _, n := range []string{"n", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		if blinkOffset(n) == 0 {
			return n
		}
	}
	panic("no name with offset 0")
}()

func kitSession(state string) store.Session {
	return store.Session{ID: "a", Name: kitName, AvatarID: "kit", Status: "idle", AvatarState: state, ContextPct: -1, TmuxPane: "%1"}
}

// A set's card holds placeholders (U+10EEEE, foreground = the frame's image id) and is exactly as
// wide as one with a sprite; the state picks the frames, which cycle with the frame tick.
// cardImageID is the image ID (hex) the card's placeholders are drawn in; the card must be 48 wide.
func cardImageID(t *testing.T, s store.Session, frame int) string {
	t.Helper()
	out := card(s, 48, 5, false, frame, now, hit{})
	if !strings.Contains(out, "\U0010EEEE") {
		t.Fatalf("no placeholders in %q", out)
	}
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w != 48 {
			t.Errorf("line %d is %d wide, want 48: %q", i, w, l)
		}
	}
	at := strings.Index(out, "\U0010EEEE")
	rgb, _, _ := strings.Cut(out[strings.LastIndex(out[:at], "38;2;")+len("38;2;"):], "m") // the color the placeholder is in
	var r, g, b uint32
	fmt.Sscanf(rgb, "%d;%d;%d", &r, &g, &b)
	return fmt.Sprintf("%x", r<<16|g<<8|b)
}

func TestImageCard(t *testing.T) {
	useKit(t)
	useImages = true
	id := func(s store.Session, frame int) string { return cardImageID(t, s, frame) }
	kit, _ := avatar.SetByID("kit")
	hex := func(state string, frame int) string { return fmt.Sprintf("%x", kit.FrameID(state, frame)) }
	// the card shows the session's AvatarState, a frame per tick (idle: the resting face, a blink every 8th)
	for _, c := range []struct {
		state string
		frame int
		want  string // the state whose picture it is
		at    int    // and the tick that picture is shown at
	}{
		{"idle", 0, "idle", 0}, {"idle", 1, "idle", 0}, {"idle", 8, "idle", 8}, {"idle", 9, "idle", 0},
		{"think", 0, "think", 0}, {"think", 1, "think", 1}, {"think", 2, "think", 0},
		{"read", 0, "read", 0}, {"read", 5, "read", 0},
		{"write", 1, "think", 1}, {"tool", 0, "think", 0}, // no frames of its own: think's
		{"hi", 3, "hi", 0}, {"success", 0, "idle", 0}, {"success", 8, "idle", 8}, // no success frames: idle's
	} {
		if got, want := id(kitSession(c.state), c.frame), hex(c.want, c.at); got != want {
			t.Errorf("%s at tick %d: %s, want %s at tick %d (%s)", c.state, c.frame, got, c.want, c.at, want)
		}
	}
	if id(kitSession("think"), 0) == id(kitSession("idle"), 0) {
		t.Error("think and idle show the same picture")
	}
}

// With no pictures (the terminal can't, or renderer = pixel) a set's card shows the sprite hashed
// from its ID: same width, no placeholders, no escape sequences for a terminal that can't take them.
func TestImageCardFallsBackToSprite(t *testing.T) {
	useKit(t)
	useImages = false
	out := card(kitSession("idle"), 48, 5, false, 0, now, hit{})
	if strings.Contains(out, "\U0010EEEE") {
		t.Errorf("placeholders without pictures: %q", out)
	}
	want := avatar.SpriteFor("kit")
	face := strings.Split(avatar.Render(want, "", avatar.Dim, 0), "\n")
	if !strings.Contains(out, face[0]) || !strings.Contains(out, face[1]) {
		t.Errorf("not the hashed sprite %s: %q", want.ID, out)
	}
}

// DetectImages tells the avatar package whether a hash may pick image sets: only when pictures are
// drawn. Outside tmux it asks nobody but the environment.
func TestDetectImagesSetsHashPool(t *testing.T) {
	useKit(t)
	t.Cleanup(func() { avatar.HashImages = false })
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TERM_PROGRAM", "")
	cfg := config.Default()
	if img := DetectImages(cfg); !img.On || !avatar.HashImages {
		t.Errorf("auto in kitty: %+v, HashImages %v", img, avatar.HashImages)
	}
	cfg.AvatarRenderer = "pixel"
	if img := DetectImages(cfg); img.On || avatar.HashImages {
		t.Errorf("pixel: %+v, HashImages %v", img, avatar.HashImages)
	}
	cfg.AvatarRenderer = "auto"
	t.Setenv("TERM", "xterm-256color")
	if img := DetectImages(cfg); img.On || avatar.HashImages {
		t.Errorf("auto elsewhere: %+v, HashImages %v", img, avatar.HashImages)
	}
}

// ---- the avatar grid (a) ----

// gridFixture: n image sets s01.. plus the five sprites, a poller with the given session names, all in
// one group so the cards stack, avatars wearing def.
func gridFixture(t *testing.T, nSets int, def string, names ...string) (*store.Poller, string) {
	t.Helper()
	files := fstest.MapFS{}
	for i := range nSets {
		files[fmt.Sprintf("s%02d/idle/1.png", i+1)] = pngFile(fmt.Sprintf("s%d", i))
		files[fmt.Sprintf("s%02d/idle/2.png", i+1)] = pngFile(fmt.Sprintf("s%d-blink", i)) // the grid must still draw the resting face
		files[fmt.Sprintf("s%02d/think/1.png", i+1)] = pngFile(fmt.Sprintf("s%d-think", i))
	}
	avatar.UseSets(files)
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}); useImages = false })
	dir := t.TempDir()
	b, _ := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	p := &store.Poller{StateDir: dir, Avatars: b, AvatarDefault: def, Show: func() ([]ccmux.Session, error) {
		var ss []ccmux.Session
		for i, n := range names {
			name, cwd, ok := strings.Cut(n, "@") // "name@/dir": its own group
			if !ok {
				cwd = "/a"
			}
			ss = append(ss, ccmux.Session{ID: fmt.Sprintf("a%d", i), Summary: name, CWD: cwd, TmuxPane: fmt.Sprintf("%%%d", i), Status: "idle"})
		}
		return ss, nil
	}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	return p, filepath.Join(dir, "avatars.json")
}

func gridModel(p *store.Poller, w, h int) Model {
	m := New(p, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return tm.(Model)
}

// gridAt finds the picker box on the screen: its top and bottom row and its left column (-1: none).
func gridAt(lines []string) (top, bottom, col int) {
	top, bottom, col = -1, -1, -1
	for y, l := range lines {
		if i := strings.Index(l, "╭"+strings.Repeat("─", gridCols*cellW)+"╮"); i >= 0 && top < 0 {
			top, col = y, len([]rune(l[:i]))
		}
		if strings.Contains(l, "╰"+strings.Repeat("─", gridCols*cellW)+"╯") {
			bottom = y
		}
	}
	return
}

// The grid is 5 columns × 4 rows of a 6 × 2 avatar and its name, the sets first and the sprites in
// the last row, straight under the card it is for, at that card's left edge; the cursor starts on
// the avatar the session wears, whose name is in the focus color's inverse.
func TestAvatarGrid(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	p, path := gridFixture(t, 15, "s07", "one")
	for _, images := range []bool{false, true} {
		useImages = images
		m, _ := press(t, gridModel(p, 120, 40), "a")
		v := m.View()
		lines := strings.Split(ansi.Strip(v), "\n")
		top, bottom, col := gridAt(lines)
		cardTop, cardLeft := -1, -1
		for y, l := range lines { // the focused card has the thick border
			if i := strings.Index(l, "┏"); i >= 0 && cardTop < 0 {
				cardTop, cardLeft = y, len([]rune(l[:i]))
			}
		}
		if len(lines) != 40 || top != cardTop+5 || bottom != top+13 || col != cardLeft {
			t.Fatalf("images=%v: card at row %d col %d, box rows %d..%d col %d, %d lines:\n%s", images, cardTop, cardLeft, top, bottom, col, len(lines), strings.Join(lines, "\n"))
		}
		names := func(row int) []string { // the name row of grid row `row` (0..3)
			return strings.Fields(strings.Trim(lines[top+1+3*row+2], "│"))
		}
		if got := names(0); !slicesEqual(got, []string{"s01", "s02", "s03", "s04", "s05"}) {
			t.Errorf("images=%v: first row %q", images, got)
		}
		if got := names(2); !slicesEqual(got, []string{"s11", "s12", "s13", "s14", "s15"}) {
			t.Errorf("images=%v: third row %q", images, got)
		}
		if got := names(3); !slicesEqual(got, []string{"Clawd", "Boxy", "Sprout", "Boo", "Mochi"}) { // sprites last, by name
			t.Errorf("images=%v: last row %q", images, got)
		}
		if n := strings.Count(v, "\U0010EEEE"); (n > 0) != images || (images && n != 15*12+12) { // the 15 sets in the box; the card wears one too
			t.Errorf("images=%v: %d placeholder cells", images, n)
		}
		if images { // the grid draws each set's resting face: idle's first frame, not its blink or think
			s01, _ := avatar.SetByID("s01")
			sgr := func(id uint32) string { return fmt.Sprintf("38;2;%d;%d;%dm", id>>16&255, id>>8&255, id&255) }
			if !strings.Contains(v, sgr(s01.FrameID("idle", 0))) || strings.Contains(v, sgr(s01.FrameID("idle", 8))) || strings.Contains(v, sgr(s01.FrameID("think", 0))) {
				t.Errorf("the grid's s01 is not its first idle frame")
			}
		}
		if !strings.Contains(v, "48;2;255;142;190m   s07   ") { // the cursor starts on the avatar it wears
			t.Errorf("images=%v: cursor not on s07:\n%q", images, lines[top+1+3*1+2])
		}
		if msg := lines[len(lines)-2]; strings.TrimSpace(msg) != "s07" {
			t.Errorf("images=%v: message line %q", images, msg)
		}
		if f := lines[len(lines)-1]; !strings.HasPrefix(f, " h/j/k/l move · enter bind · q cancel") {
			t.Errorf("footer %q", f)
		}
		// moves: l → s08, j → s13, j → the sprite row (index 17: Sprout), k k back, h h to the start of the row
		for _, c := range []struct{ keys, want string }{
			{"l", "s08"}, {"j", "s13"}, {"j", "bot-03  Sprout"}, {"j", "bot-03  Sprout"}, {"k", "s13"}, {"k", "s08"}, {"k", "s03"}, {"k", "s03"},
			{"h", "s02"}, {"h", "s01"}, {"h", "s01"}, {"right", "s02"}, {"down", "s07"}, {"up", "s02"}, {"left", "s01"},
		} {
			m, _ = press(t, m, c.keys)
			if got := strings.TrimSpace(strings.Split(ansi.Strip(m.View()), "\n")[38]); got != c.want {
				t.Fatalf("images=%v: after %q the message line is %q, want %q", images, c.keys, got, c.want)
			}
		}
		m, _ = press(t, m, "l", "l", "enter") // s03
		if v := ansi.Strip(m.View()); !strings.Contains(v, "one wears s03") || strings.Contains(v, "╭"+strings.Repeat("─", 40)) {
			t.Errorf("images=%v: bound message, grid closed: %q", images, v)
		}
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"one": "s03"`) || p.Sessions()[0].AvatarID != "s03" {
			t.Errorf("a set's ID is bound: %s %v", data, p.Sessions())
		}
		os.Remove(path)
		p.Refresh() // back to the default
	}
}

func slicesEqual(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

// Under the card, or above it when there is no room below; at the card's left edge, pulled in
// when the grid would run off the right of the screen.

func TestAvatarGridPlacement(t *testing.T) {
	p, _ := gridFixture(t, 15, "s07", "n1", "n2", "n3", "n4")
	cardRows := func(lines []string) (top int) {
		for y, l := range lines {
			if strings.Contains(l, "┏") {
				return y
			}
		}
		return -1
	}
	m, _ := press(t, gridModel(p, 60, 30), "a")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if top, bottom, _ := gridAt(lines); top != cardRows(lines)+5 || bottom != top+13 {
		t.Errorf("first card: grid at %d..%d, card at %d", top, bottom, cardRows(lines))
	}
	m, _ = press(t, gridModel(p, 60, 30), "j", "j", "j", "a") // the last card: no room below on 30 rows
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if top, bottom, _ := gridAt(lines); bottom != cardRows(lines)-1 || top != bottom-13 {
		t.Errorf("last card: grid at %d..%d, card at %d:\n%s", top, bottom, cardRows(lines), strings.Join(lines, "\n"))
	}
	// room both above and below: below
	p6, _ := gridFixture(t, 15, "s07", "n1", "n2", "n3", "n4", "n5", "n6")
	m, _ = press(t, gridModel(p6, 60, 60), "j", "j", "j", "a")
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if top, _, _ := gridAt(lines); top != cardRows(lines)+5 {
		t.Errorf("room on both sides: grid at %d, card at %d", top, cardRows(lines))
	}
	// a card in the second column, 82 columns wide: the grid (gridW) is pulled in to end at the screen's edge
	p2, _ := gridFixture(t, 15, "s07", "n1", "n2@/b")
	m, _ = press(t, gridModel(p2, 82, 40), "l", "a")
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if _, _, col := gridAt(lines); col != 82-gridW {
		t.Errorf("second column: grid at column %d, want %d:\n%s", col, 82-gridW, strings.Join(lines, "\n"))
	}
}

// More than 20 avatars scroll by row; on a short screen the grid shows fewer rows; the cursor is
// on the screen wherever it is, and enter binds what it is on.
func TestAvatarGridScrolls(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	p, path := gridFixture(t, 18, "s01", "one") // 18 sets and 5 sprites: 23, five rows
	all := avatar.Listing()
	if len(all) != 23 {
		t.Fatalf("%d avatars", len(all))
	}
	for _, c := range []struct{ h, boxH int }{{40, 14}, {20, 11}, {18, 8}, {16, 5}, {12, 5}} { // 4, 3, 2, 1, 1 rows: what fits under the 7 rows above (on 12 it covers the card)
		m := gridModel(p, 120, c.h)
		m, _ = press(t, m, "a")
		for i, id := range all {
			v := m.View()
			lines := strings.Split(ansi.Strip(v), "\n")
			top, bottom, _ := gridAt(lines)
			if len(lines) != c.h || bottom-top+1 != c.boxH || bottom > c.h-3 || strings.Count(v, "48;2;255;142;190m") != 1 {
				t.Fatalf("h=%d cursor %d (%s): %d lines, grid rows %d..%d, %d cursors:\n%s", c.h, i, id, len(lines), top, bottom, strings.Count(v, "48;2;255;142;190m"), strings.Join(lines, "\n"))
			}
			if i < len(all)-1 {
				m, _ = press(t, m, "l")
			}
		}
		m, _ = press(t, m, "enter") // on the last: Mochi
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"one": "bot-05"`) {
			t.Errorf("h=%d: avatars.json %s", c.h, data)
		}
		os.Remove(path)
		p.Refresh()
	}
	// down from a row whose successor is shorter goes to the very last one
	m, _ := press(t, gridModel(p, 120, 40), "a", "j", "j", "j", "l", "l", "l", "l", "j") // index 19 → 24 doesn't exist → 22
	if got := strings.TrimSpace(strings.Split(ansi.Strip(m.View()), "\n")[38]); got != "bot-05  Mochi" {
		t.Errorf("the short last row: %q", got)
	}
}

// Idle cards don't all blink in the same tick: each has its own offset, from its avatar key.
func TestBlinksAreStaggered(t *testing.T) {
	useKit(t)
	useImages = true
	kit, _ := avatar.SetByID("kit")
	rest := fmt.Sprintf("%x", kit.FrameID("idle", 0))
	blinkTicks := func(name string) (ticks []int) {
		s := kitSession("idle")
		s.Name = name
		for frame := range 24 {
			if cardImageID(t, s, frame) != rest {
				ticks = append(ticks, frame)
			}
		}
		return
	}
	// two names with different offsets
	a, b := "", ""
	for _, n := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"} {
		switch {
		case a == "":
			a = n
		case blinkOffset(n) != blinkOffset(a):
			b = n
		}
		if b != "" {
			break
		}
	}
	ta, tb := blinkTicks(a), blinkTicks(b)
	wantA, wantB := []int{}, []int{}
	for _, t := range []int{8, 16, 24} { // the blink is at tick 8k on the card's own clock: frame + offset
		for _, c := range []struct {
			name string
			into *[]int
		}{{a, &wantA}, {b, &wantB}} {
			if f := t - blinkOffset(c.name); f >= 0 {
				*c.into = append(*c.into, f)
			}
		}
	}
	if fmt.Sprint(ta) != fmt.Sprint(wantA) || fmt.Sprint(tb) != fmt.Sprint(wantB) || fmt.Sprint(ta) == fmt.Sprint(tb) {
		t.Errorf("%s (offset %d) blinks at frames %v, want %v; %s (offset %d) at %v, want %v", a, blinkOffset(a), ta, wantA, b, blinkOffset(b), tb, wantB)
	}
	// the offset belongs to the key: the same name always gets the same one
	if blinkOffset("alpha") != blinkOffset("alpha") || blinkOffset("") != blinkOffset("") {
		t.Error("offset is not stable")
	}
}
