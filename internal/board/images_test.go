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
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

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
// kitty / ghostty (in tmux the TERM of the client a jump would use, read from list-clients; a client
// that is a tmux itself is skipped for the most recent real one); image skips the terminal check.
// In tmux the pictures go to that client's tty, so allow-passthrough does not matter.
func TestImageMode(t *testing.T) {
	useKit(t)
	inTmux := map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}
	nested := map[string]string{"TMUX": "x", "TMUX_PANE": "%2"}
	line := func(act int, name, sess, term string) string {
		return fmt.Sprintf("%d\t%s\t%s\t%s\t%s\n", act, name, sess, term, name)
	}
	for _, c := range []struct {
		name     string
		renderer string
		env      map[string]string
		clients  string // list-clients output
		on       bool
		client   string // the client the pictures go to
		note     string // part of the board's note ("" = none)
	}{
		{"kitty client", "auto", inTmux, line(5, "/dev/c", "main", "xterm-kitty"), true, "/dev/c", ""},
		{"no tty in the answer: the client name is the tty", "auto", inTmux, "5\t/dev/c\tmain\txterm-kitty\t\n", true, "/dev/c", ""},
		{"ghostty client", "auto", inTmux, line(5, "/dev/c", "main", "xterm-ghostty"), true, "/dev/c", ""},
		{"other client: says which and what", "auto", inTmux, line(5, "/dev/c", "main", "xterm-256color"), false, "", "/dev/c"},
		{"other client: the TERM is in the note", "auto", inTmux, line(5, "/dev/c", "main", "xterm-256color"), false, "", "xterm-256color"},
		{"pixel is never pictures", "pixel", inTmux, line(5, "/dev/c", "main", "xterm-kitty"), false, "", ""},
		{"image on a plain client", "image", inTmux, line(5, "/dev/c", "main", "xterm-256color"), true, "/dev/c", ""},
		{"no client: says so", "auto", inTmux, "", false, "", "no tmux client"},
		{"image with no client: tries passthrough", "image", inTmux, "", true, "", "passthrough"},
		// the user's layout: agmux runs in fl-1, shown by a popup client (tmux-256color) of the kitty client of another session
		{"popup client in own session, kitty elsewhere", "auto", nested, line(5, "/dev/kitty", "main", "xterm-kitty") + line(9, "/dev/popup", "fl-1", "tmux-256color"), true, "/dev/kitty", ""},
		// a chosen client that is a tmux itself: the most recent real terminal instead
		{"chosen client is nested: the real one", "auto", map[string]string{"TMUX": "x", "AGMUX_CLIENT": "/dev/popup"},
			line(5, "/dev/old", "a", "xterm-kitty") + line(7, "/dev/new", "b", "xterm-ghostty") + line(9, "/dev/popup", "c", "screen-256color"), true, "/dev/new", ""},
		{"only nested clients, image forced: no terminal, passthrough", "image", map[string]string{"TMUX": "x", "AGMUX_CLIENT": "/dev/popup"}, line(9, "/dev/popup", "c", "tmux-256color"), true, "", "every tmux client"},
		{"only nested clients", "auto", map[string]string{"TMUX": "x", "AGMUX_CLIENT": "/dev/popup"}, line(9, "/dev/popup", "c", "tmux-256color"), false, "", "tmux-256color"},
		{"outside tmux: kitty TERM", "auto", map[string]string{"TERM": "xterm-kitty"}, "", true, "", ""},
		{"outside tmux: ghostty", "auto", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty"}, "", true, "", ""},
		{"outside tmux: other", "auto", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app"}, "", false, "", ""},
		{"outside tmux: image forced", "image", map[string]string{"TERM": "xterm-256color"}, "", true, "", ""},
	} {
		r := &fakeRunner{tmuxOut: map[string]string{"list-clients": c.clients, "display-message": "fl-1\n"}}
		img := imageMode(r, env(c.env), c.renderer)
		if img.On != c.on || img.Client != c.client || (c.note == "") != (img.Note == "") || !strings.Contains(img.Note, c.note) {
			t.Errorf("%s: %+v, want on=%v client %q note containing %q", c.name, img, c.on, c.client, c.note)
		}
		if img.On && img.Client != "" && img.TTY != img.Client {
			t.Errorf("%s: tty %q, want the client's tty", c.name, img.TTY)
		}
		for _, call := range r.calls {
			if call[1] == "display-message" && strings.Contains(strings.Join(call, " "), "client_termname") || call[1] == "show-options" {
				t.Errorf("%s: asked tmux for %q: the termname comes from list-clients, passthrough does not matter", c.name, call)
			}
		}
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
// Its borders may carry a "↑ n more" / "↓ n more", so it is found by its corners gridW cells apart.
func gridAt(lines []string) (top, bottom, col int) {
	top, bottom, col = -1, -1, -1
	for y, l := range lines {
		r := []rune(l)
		for i := 0; i+gridW <= len(r); i++ {
			if r[i] == '╭' && r[i+gridW-1] == '╮' && top < 0 {
				top, col = y, i
			}
			if r[i] == '╰' && r[i+gridW-1] == '╯' && i == col {
				bottom = y
			}
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
		// move the cursor off: the avatar the card wears keeps its name in the focus color, not inverse
		moved, _ := press(t, m, "l")
		if mv := moved.View(); !strings.Contains(mv, "38;2;255;142;190m   s07    ") || strings.Contains(mv, "48;2;255;142;190m   s07") || !strings.Contains(mv, "48;2;255;142;190m   s08") {
			t.Errorf("images=%v: s07 is worn: focus color, not inverse; the cursor is on s08:\n%q", images, strings.Split(mv, "\n")[8+3*1+2])
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

// The grid shows all its rows (23 avatars: five) and scrolls by row only where the screen is short; the cursor is
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
	for _, c := range []struct{ h, boxH int }{{40, 17}, {20, 11}, {18, 8}, {16, 5}, {12, 5}} { // 5 (all), 3, 2, 1, 1 rows: what fits under the 7 rows above (on 12 it covers the card)
		m := gridModel(p, 120, c.h)
		m, _ = press(t, m, "a")
		for i, id := range all {
			v := m.View()
			lines := strings.Split(ansi.Strip(v), "\n")
			top, bottom, _ := gridAt(lines)
			if len(lines) != c.h || bottom-top+1 != c.boxH || bottom > c.h-3 || strings.Count(v, "48;2;255;142;190m") != 1 {
				t.Fatalf("h=%d cursor %d (%s): %d lines, grid rows %d..%d, %d cursors:\n%s", c.h, i, id, len(lines), top, bottom, strings.Count(v, "48;2;255;142;190m"), strings.Join(lines, "\n"))
			}
			if i == len(all)-1 { // on the last avatar the grid still ends with it: no row of empty cells after it
				for r := top + 1; r < bottom; r += 3 {
					if strings.Trim(strings.Join(lines[r:r+3], ""), "│ ") == "" {
						t.Errorf("h=%d: an empty row of cells at rows %d..%d:\n%s", c.h, r, r+2, strings.Join(lines, "\n"))
					}
				}
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

// A grid has as many rows as its sets need: 10 avatars two, 25 five, 26 six (ceil(n/5)), and with
// room for all of them nothing says "more". Where the screen is short only some rows show, and the
// top border says "↑ n more", the bottom "↓ n more" (muted; n counts avatars, a short last row too),
// each only where there is something that way. The box keeps its height all the way.
func TestAvatarGridRowsAndMore(t *testing.T) {
	for _, c := range []struct{ sets, rows int }{{5, 2}, {20, 5}, {21, 6}} {
		p, _ := gridFixture(t, c.sets, "s01", "one")
		m, _ := press(t, gridModel(p, 120, 60), "a")
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		top, bottom, _ := gridAt(lines)
		if got := (bottom - top - 1) / cellH; got != c.rows || strings.Contains(strings.Join(lines, "\n"), "more") {
			t.Errorf("%d avatars: %d rows, want %d:\n%s", c.sets+5, got, c.rows, strings.Join(lines, "\n"))
		}
	}
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	for _, c := range []struct {
		sets, h int
		keys    []string
		up, dn  string // the notes in the borders; "" = a plain border
	}{
		{20, 20, nil, "", "↓ 10 more"},                          // 25 avatars, 3 rows shown, the cursor on the first
		{20, 20, []string{"j", "j"}, "↑ 5 more", "↓ 5 more"},    // on the third row: one row out above, one below
		{20, 20, []string{"j", "j", "j", "j"}, "↑ 10 more", ""}, // the last row
		{18, 20, nil, "", "↓ 8 more"},                           // 23 avatars: the last row has 3, and they count (not 10)
		{18, 20, []string{"j", "j", "j", "j"}, "↑ 10 more", ""}, // and from the bottom: the rows out above are full
		{18, 12, []string{"j", "j"}, "↑ 10 more", "↓ 8 more"},   // a short screen: one row shown; the notes still say the rest
	} {
		p, _ := gridFixture(t, c.sets, "s01", "one")
		m, _ := press(t, gridModel(p, 120, c.h), "a")
		m, _ = press(t, m, c.keys...)
		raw := strings.Split(m.View(), "\n")
		top, bottom, col := gridAt(strings.Split(ansi.Strip(m.View()), "\n"))
		muted := strings.SplitN(fg(cMuted, "x"), "x", 2)[0]
		for _, e := range []struct{ row, want string }{{ansi.Strip(raw[top]), c.up}, {ansi.Strip(raw[bottom]), c.dn}} {
			e.row = string([]rune(e.row)[col : col+gridW])
			note := strings.Trim(strings.Trim(e.row, "╭╮╰╯─"), " ")
			if note != e.want {
				t.Errorf("sets=%d h=%d keys=%v: border %q has note %q, want %q", c.sets, c.h, c.keys, e.row, note, e.want)
			}
		}
		if got := strings.Count(strings.Join(raw[top:bottom+1], ""), muted) > 0; got != (c.up != "" || c.dn != "") {
			t.Errorf("sets=%d h=%d keys=%v: muted note colored: %v", c.sets, c.h, c.keys, got)
		}
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

// An 8 character name doesn't touch the next cell's: there are at least two spaces between them.
func TestAvatarGridNamesKeepApart(t *testing.T) {
	avatar.UseSets(fstest.MapFS{"aaaaaaaa/idle/1.png": pngFile("a"), "bbbbbbbb/idle/1.png": pngFile("b"), "cccccccc/idle/1.png": pngFile("c")})
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}); useImages = false })
	p := &store.Poller{StateDir: t.TempDir(), Show: func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a1", Summary: "one", CWD: "/a", TmuxPane: "%1", Status: "idle"}}, nil
	}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	m, _ := press(t, gridModel(p, 120, 40), "a")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	top, _, _ := gridAt(lines)
	if !strings.Contains(lines[top+3], "aaaaaaaa  ") || !strings.Contains(lines[top+3], "  bbbbbbbb  ") || !strings.Contains(lines[top+3], "  cccccccc  ") {
		t.Errorf("names touch: %q", lines[top+3])
	}
	if gridW != 52 { // 5 cells of 10 and the border
		t.Errorf("box is %d wide", gridW)
	}
}

// For every card, screen height and width: the grid never covers the top bar, the message line or
// the footer, never covers its own card when there was room, keeps the cursor on screen, and the
// message line names what the cursor is on.
func TestAvatarGridEverywhere(t *testing.T) {
	names := []string{"s1@/a", "s2@/a", "s3@/a", "s4@/b", "s5@/b", "s6@/c", "s7@/c", "s8@/c", "s9@/c"}
	p, _ := gridFixture(t, 18, "s05", names...)
	checked, below, above, fallback := 0, 0, 0, 0
	defer func() {
		t.Logf("checked %d card×size cases: below %d, above %d, overlapping fallback %d", checked, below, above, fallback)
	}()
	for _, w := range []int{60, 120, 200} {
		for _, h := range []int{7, 8, 9, 10, 12, 14, 17, 20, 24, 30, 40} {
			for _, s := range p.Sessions() {
				m := gridModel(p, w, h)
				m.focus = s.ID
				before := strings.Split(ansi.Strip(m.View()), "\n")
				ct, cb := -1, -1
				for y, l := range before {
					if strings.Contains(l, "┏") && ct < 0 {
						ct = y
					}
					if strings.Contains(l, "┗") {
						cb = y
					}
				}
				if ct < 0 || cb < 0 {
					continue // the focused card is not fully on screen at this size
				}
				m, _ = press(t, m, "a")
				checked++
				if tp, bt, _ := gridAt(strings.Split(ansi.Strip(m.View()), "\n")); tp > cb {
					below++
				} else if bt < ct {
					above++
				} else {
					fallback++
				}
				for step := range 10 { // walk the cursor around too
					lines := strings.Split(ansi.Strip(m.View()), "\n")
					where := fmt.Sprintf("w=%d h=%d card %s rows %d..%d step %d", w, h, s.Name, ct, cb, step)
					top, bottom, _ := gridAt(lines)
					if len(lines) != h || top < 0 || bottom < 0 {
						t.Fatalf("%s: no grid (%d lines)\n%s", where, len(lines), strings.Join(lines, "\n"))
					}
					if lines[0] != before[0] {
						t.Errorf("%s: top bar covered", where)
					}
					if bottom > h-3 && h >= 8 {
						t.Errorf("%s: grid rows %d..%d cover the message line / footer", where, top, bottom)
					}
					roomBelow, roomAbove := h-3-cb >= 5, ct-1 >= 5
					if (roomBelow || roomAbove) && top <= cb && bottom >= ct {
						t.Errorf("%s: grid %d..%d covers its card although there was room", where, top, bottom)
					}
					id := avatar.Listing()[m.pick]
					if !strings.Contains(lines[h-2], id) {
						t.Errorf("%s: message line %q does not name %s", where, lines[h-2], id)
					}
					inGrid := false
					for _, l := range lines[top : bottom+1] {
						inGrid = inGrid || strings.Contains(l, ansi.Truncate(avatarName(id), cellW, "…"))
					}
					if !inGrid {
						t.Errorf("%s: cursor on %s is not in the drawn rows\n%s", where, id, strings.Join(lines, "\n"))
					}
					keys := []string{"l", "j", "j", "h", "l", "l", "j", "k"}
					m, _ = press(t, m, keys[step%len(keys)])
				}
			}
		}
	}
}

// In tmux the pictures are written to the real terminal's client tty, bare, and tmux is told to
// redraw that client afterwards; a tty that cannot be opened falls back to tmux's passthrough on
// stdout, and says so.
// asFile makes the "tty" an ordinary file for the test (the real opener takes only devices).
func asFile(t *testing.T) {
	t.Helper()
	old := openTTY
	openTTY = func(path string) (io.WriteCloser, error) { return os.OpenFile(path, os.O_WRONLY, 0) }
	t.Cleanup(func() { openTTY = old })
}

func TestUploadImages(t *testing.T) {
	useKit(t)
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	tty := filepath.Join(t.TempDir(), "ttys001")
	os.WriteFile(tty, nil, 0o644)
	asFile(t)
	r := &fakeRunner{}
	note, err := uploadImages(ImageMode{On: true, Client: "/dev/c", TTY: tty, Note: "kept"}, r)
	got, _ := os.ReadFile(tty)
	if err != nil || note != "kept" || !strings.HasPrefix(string(got), "\x1b_Ga=T,U=1") || strings.Contains(string(got), "\x1bPtmux;") || strings.Contains(string(got), "\x1b[?1049") {
		t.Errorf("to the tty: err %v, note %q, wrote %.40q", err, note, got)
	}
	if want := []string{"tmux", "refresh-client", "-t", "/dev/c"}; !slices.ContainsFunc(r.calls, func(c []string) bool { return slices.Equal(c, want) }) {
		t.Errorf("no refresh-client: %q", r.calls)
	}

	out, _ := os.Create(filepath.Join(t.TempDir(), "stdout"))
	old := os.Stdout
	os.Stdout = out
	note, err = uploadImages(ImageMode{On: true, Client: "/dev/c", TTY: filepath.Join(t.TempDir(), "no", "such", "tty")}, &fakeRunner{})
	os.Stdout = old
	out.Close()
	got, _ = os.ReadFile(out.Name())
	if err != nil || !strings.Contains(note, "cannot write") || !strings.HasPrefix(string(got), "\x1bPtmux;") {
		t.Errorf("tty not openable: err %v, note %q, stdout %.40q", err, note, got)
	}
}

// A write that fails halfway to the tty falls back to passthrough like one that cannot open it.
type failingTTY struct{ n int }

func (f *failingTTY) Write(p []byte) (int, error) {
	if f.n--; f.n < 0 {
		return 0, errors.New("input/output error")
	}
	return len(p), nil
}
func (f *failingTTY) Close() error { return nil }

func TestUploadImagesWriteError(t *testing.T) {
	useKit(t)
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	openTTY = func(string) (io.WriteCloser, error) { return &failingTTY{n: 1}, nil }
	t.Cleanup(func() {
		openTTY = func(path string) (io.WriteCloser, error) { return os.OpenFile(path, os.O_WRONLY, 0) }
	})
	out, _ := os.Create(filepath.Join(t.TempDir(), "stdout"))
	old := os.Stdout
	os.Stdout = out
	note, err := uploadImages(ImageMode{On: true, Client: "/dev/c", TTY: "/dev/ttys001", Note: "stale"}, &fakeRunner{})
	os.Stdout = old
	out.Close()
	got, _ := os.ReadFile(out.Name())
	if err != nil || !strings.Contains(note, "input/output error") || strings.Contains(note, "stale") || !strings.HasPrefix(string(got), "\x1bPtmux;") {
		t.Errorf("err %v, note %q, stdout %.40q", err, note, got)
	}
}

// Outside tmux the upload goes to stdout and switches to the alternate screen first (T28: kitty
// keeps pictures per screen, the board runs on the alternate one).
func TestUploadImagesOutsideTmux(t *testing.T) {
	useKit(t)
	t.Setenv("TMUX", "")
	out, _ := os.Create(filepath.Join(t.TempDir(), "stdout"))
	old := os.Stdout
	os.Stdout = out
	note, err := uploadImages(ImageMode{On: true}, &fakeRunner{})
	os.Stdout = old
	out.Close()
	got, _ := os.ReadFile(out.Name())
	if err != nil || note != "" || !strings.HasPrefix(string(got), "\x1b[?1049h\x1b_Ga=T") {
		t.Errorf("outside tmux: err %v note %q stdout %.30q", err, note, got)
	}
}

// refresh-client failing (the client detached meanwhile, an old tmux) is not fatal and says nothing.
func TestUploadImagesRefreshFailNotFatal(t *testing.T) {
	useKit(t)
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	tty := filepath.Join(t.TempDir(), "ttys001")
	os.WriteFile(tty, nil, 0o644)
	asFile(t)
	r := &fakeRunner{tmuxFail: map[string]error{"refresh-client": errors.New("can't find client: /dev/c")}}
	note, err := uploadImages(ImageMode{On: true, Client: "/dev/c", TTY: tty}, r)
	got, _ := os.ReadFile(tty)
	if err != nil || note != "" || !strings.HasPrefix(string(got), "\x1b_Ga=T") {
		t.Errorf("refresh-client failed: err %v note %q tty %.30q", err, note, got)
	}
}

// Only a device is opened as a client's tty: a name that is a file (client_tty empty: the name
// "client-123" is used) is not written, the upload goes through passthrough with a note.
func TestOpenClientTTYOnlyDevices(t *testing.T) {
	f := filepath.Join(t.TempDir(), "client-123")
	os.WriteFile(f, nil, 0o644)
	if w, err := openClientTTY(f); err == nil {
		w.Close()
		t.Fatal("opened a file as a tty")
	}
	w, err := openClientTTY("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("x")); n != 1 || err != nil {
		t.Errorf("write to /dev/null: %d %v", n, err)
	}
	w.Close()
}

// A tty that takes bytes slowly is waited for; one that stops taking them is an error after ttyStall,
// and the board's upload falls back (it must not hang on a terminal that stopped reading).
func TestTTYWriterWaitsAndStalls(t *testing.T) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	syscall.SetNonblock(fds[1], true)
	t.Cleanup(func() { syscall.Close(fds[0]); syscall.Close(fds[1]) })
	old := ttyStall
	ttyStall = 100 * time.Millisecond
	t.Cleanup(func() { ttyStall = old })
	big := make([]byte, 300<<10) // far more than a pipe holds
	got := make(chan int)
	go func() { // a slow reader: takes a little every few ms
		n := 0
		buf := make([]byte, 8<<10)
		for n < len(big) {
			m, _ := syscall.Read(fds[0], buf)
			n += m
			time.Sleep(5 * time.Millisecond) // all in all several times ttyStall: only progress keeps the writer waiting
		}
		got <- n
	}()
	if n, err := (ttyWriter{fds[1]}).Write(big); n != len(big) || err != nil {
		t.Fatalf("slow reader: wrote %d of %d, %v", n, len(big), err)
	}
	<-got
	// nobody reads: the pipe fills and the write gives up
	start := time.Now()
	n, err := (ttyWriter{fds[1]}).Write(big)
	if err == nil || n >= len(big) || time.Since(start) > 3*time.Second || !strings.Contains(err.Error(), "stopped reading") {
		t.Errorf("stalled: wrote %d, %v after %v", n, err, time.Since(start))
	}
}

// The client's tty is opened non-blocking: without it ttyWriter's EAGAIN / stall logic never runs
// and a terminal that stopped reading hangs the board's start.
func TestOpenClientTTYNonblocking(t *testing.T) {
	w, err := openClientTTY("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	fl, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(w.(ttyWriter).fd), syscall.F_GETFL, 0)
	if e != 0 || fl&syscall.O_NONBLOCK == 0 {
		t.Errorf("flags %#x (errno %v): not O_NONBLOCK", fl, e)
	}
}

// A write that fits the tty's queue waits until the whole of it fits: it is then taken in one piece,
// and tmux's own drawing cannot land inside it (see avatar.ttyChunk). One that can't be asked (a
// pipe) or is bigger than the queue is written at once.
func TestTTYWriterWaitsForRoom(t *testing.T) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fds[0]); syscall.Close(fds[1]) })
	syscall.SetNonblock(fds[1], true)
	old := outQueue
	t.Cleanup(func() { outQueue = old })
	asked := 0
	outQueue = func(int) (int, error) { // 700 queued for the first 5 questions, then the terminal has read it
		if asked++; asked <= 5 {
			return 700, nil
		}
		return 0, nil
	}
	msg := make([]byte, 500)
	if n, err := (ttyWriter{fds[1]}).Write(msg); n != 500 || err != nil || asked != 6 {
		t.Errorf("wrote %d, %v, asked %d times (want 6: 5 without room, 1 with)", n, err, asked)
	}
	buf := make([]byte, 1<<16)
	syscall.SetNonblock(fds[0], true)
	if n, _ := syscall.Read(fds[0], buf); n != 500 {
		t.Errorf("the pipe got %d bytes in one go", n)
	}
	// too big for any queue, or a tty that can't say: no waiting
	outQueue = func(int) (int, error) { return 1024, nil }
	if n, err := (ttyWriter{fds[1]}).Write(make([]byte, 2000)); n != 2000 || err != nil {
		t.Errorf("bigger than the queue: %d, %v", n, err)
	}
	syscall.Read(fds[0], buf)
	outQueue = func(int) (int, error) { return 0, syscall.ENOTTY }
	if n, err := (ttyWriter{fds[1]}).Write(msg); n != 500 || err != nil {
		t.Errorf("no queue to ask: %d, %v", n, err)
	}
	// a terminal that never reads is a stall, as before
	outQueue = func(int) (int, error) { return 1000, nil }
	ttyStall = 50 * time.Millisecond
	t.Cleanup(func() { ttyStall = 2 * time.Second })
	if n, err := (ttyWriter{fds[1]}).Write(msg); err == nil || n != 0 || !strings.Contains(err.Error(), "stopped reading") {
		t.Errorf("full queue: %d, %v", n, err)
	}
}
