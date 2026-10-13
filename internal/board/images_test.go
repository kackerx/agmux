package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
	"github.com/kackerx/agmux/internal/store"
)

func png(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// useKit loads one set: two idle frames, two working, one attention, nothing for done.
func useKit(t *testing.T) {
	t.Helper()
	avatar.UseSets(fstest.MapFS{
		"kit/idle/1.png": png("i1"), "kit/idle/2.png": png("i2"),
		"kit/working/1.png": png("w1"), "kit/working/2.png": png("w2"),
		"kit/attention/1.png": png("a1"),
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

func kitSession(status string) store.Session {
	return store.Session{ID: "a", Name: "n", AvatarID: "kit", Status: status, ContextPct: -1, TmuxPane: "%1"}
}

// A set's card holds placeholders (U+10EEEE, foreground = the frame's image id) and is exactly as
// wide as one with a sprite; the state picks the frames, which cycle with the frame tick.
func TestImageCard(t *testing.T) {
	useKit(t)
	useImages = true
	id := func(s store.Session, frame int) string {
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
	idle, working, attention := kitSession("idle"), kitSession("working"), kitSession("waiting")
	kit, _ := avatar.SetByID("kit")
	hex := func(m avatar.Mood, frame int) string { return fmt.Sprintf("%x", kit.FrameID(m, frame)) }
	if a, b, c := id(idle, 0), id(idle, 1), id(idle, 2); a != hex(avatar.Dim, 0) || b != hex(avatar.Dim, 1) || c != a || a == b {
		t.Errorf("idle frames: %s %s %s", a, b, c)
	}
	if a, b := id(working, 0), id(working, 1); a != hex(avatar.Scan, 0) || b != hex(avatar.Scan, 1) || a == b || a == hex(avatar.Dim, 0) {
		t.Errorf("working frames: %s %s", a, b)
	}
	if a, b := id(attention, 0), id(attention, 3); a != hex(avatar.Alert, 0) || b != a {
		t.Errorf("attention frames: %s %s", a, b)
	}
	done := kitSession("idle")
	done.Done = true
	if a := id(done, 0); a != hex(avatar.Dim, 0) { // the set has no done frames: idle's
		t.Errorf("done falls back to idle: %s", a)
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

// The avatar list shows the image sets first, then the sprites; the cursor starts on what the
// session wears, and enter binds a set's ID like a sprite's.
func TestAvatarPickerSets(t *testing.T) {
	avatar.UseSets(fstest.MapFS{ // kit has done frames, so the list must pick idle's; bot-03 shadows the sprite
		"kit/idle/1.png": png("i1"), "kit/done/1.png": png("d1"), "bot-03/idle/1.png": png("b"),
	})
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}); useImages = false })
	dir := t.TempDir()
	path := filepath.Join(dir, "avatars.json")
	b, _ := avatar.LoadBindings(path)
	p := &store.Poller{StateDir: dir, Avatars: b, AvatarDefault: "kit", Show: func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a1", Summary: "one", CWD: "/a", TmuxPane: "%1", Status: "idle"}}, nil
	}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	for _, images := range []bool{true, false} {
		useImages = images
		m := New(p, &fakeRunner{})
		m.getenv = func(string) string { return "" }
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
		m, _ = press(t, tm.(Model), "a")
		view := m.View()
		lines := strings.Split(ansi.Strip(view), "\n")
		kit, bot01 := -1, -1
		for i, l := range lines {
			if kit < 0 && strings.Contains(l, "▸") { // the cursor
				kit = i
			}
			if strings.Contains(l, "bot-01  Clawd") {
				bot01 = i
			}
		}
		if kit < 0 || bot01 < 0 || kit > bot01 || !strings.Contains(lines[kit], "▸") {
			t.Fatalf("images=%v: the set comes first and the cursor starts on it (kit row %d, bot-01 row %d):\n%s", images, kit, bot01, ansi.Strip(view))
		}
		if f := strings.Fields(strings.TrimSuffix(strings.TrimSpace(lines[kit]), "│")); f[len(f)-1] != "kit" { // a set's name is its ID: no second name
			t.Errorf("images=%v: set row %q", images, lines[kit])
		}
		if got := strings.Contains(view, "\U0010EEEE"); got != images {
			t.Errorf("images=%v: placeholders in the list: %v", images, got)
		}
		sgr := func(m avatar.Mood) string {
			kit, _ := avatar.SetByID("kit")
			id := kit.FrameID(m, 0)
			return fmt.Sprintf("38;2;%d;%d;%dm", id>>16&255, id>>8&255, id&255)
		}
		if images && !strings.Contains(view, sgr(avatar.Dim)) { // kit's idle frame
			t.Errorf("the set's row is not its first idle frame:\n%q", view)
		}
		if images && strings.Contains(view, sgr(avatar.Normal)) { // not its done frame
			t.Errorf("the list shows the done frame:\n%q", view)
		}
		if strings.Contains(ansi.Strip(view), "bot-03  Sprout") || !strings.Contains(ansi.Strip(view), "bot-03  ") {
			t.Errorf("images=%v: a set named like a sprite replaces it, under its ID alone:\n%s", images, ansi.Strip(view))
		}
		m, _ = press(t, m, "j", "enter") // down one: the first sprite
		if !strings.Contains(ansi.Strip(m.View()), "one wears Clawd") {
			t.Errorf("images=%v: %q", images, ansi.Strip(m.View()))
		}
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"one": "bot-01"`) {
			t.Errorf("avatars.json: %s", data)
		}
		m, _ = press(t, m, "a", "k", "enter") // back up to the set
		if !strings.Contains(ansi.Strip(m.View()), "one wears kit") {
			t.Errorf("images=%v: %q", images, ansi.Strip(m.View()))
		}
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"one": "kit"`) || p.Sessions()[0].AvatarID != "kit" {
			t.Errorf("a set's ID is bound: %s %v", data, p.Sessions())
		}
		// a set that shadows a sprite is bound under its ID, not the sprite's name
		if n := len(avatar.Listing()); n != len(avatar.Sets)+len(avatar.Sprites)-1 {
			t.Errorf("images=%v: %d entries, want the sets and the sprites minus the shadowed one", images, n)
		}
		if n := strings.Count(ansi.Strip(view), "bot-03"); n != 1 {
			t.Errorf("images=%v: bot-03 is listed %d times", images, n)
		}
		m, _ = press(t, m, "a", "k", "enter")
		if v := ansi.Strip(m.View()); !strings.Contains(v, "one wears bot-03") || strings.Contains(v, "wears Sprout") {
			t.Errorf("images=%v: %q", images, v)
		}
		m, _ = press(t, m, "a", "j", "enter") // back to kit for the next round
	}
}

// A list taller than the screen shows the entries around the cursor: the cursor never leaves the
// screen, the board's bars stay, and enter binds the entry the cursor is on.
func TestAvatarPickerScrolls(t *testing.T) {
	files := fstest.MapFS{}
	for i := range 20 {
		files[fmt.Sprintf("u%02d/idle/1.png", i)] = png("x")
	}
	avatar.UseSets(files)
	t.Cleanup(func() { avatar.UseSets(fstest.MapFS{}); useImages = false })
	useImages = false
	dir := t.TempDir()
	b, _ := avatar.LoadBindings(filepath.Join(dir, "avatars.json"))
	p := &store.Poller{StateDir: dir, Avatars: b, AvatarDefault: "u00", Show: func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a1", Summary: "one", CWD: "/a", TmuxPane: "%1", Status: "idle"}}, nil
	}}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	all := avatar.Listing() // 20 sets, then the 6 sprites
	for _, h := range []int{30, 12, 6} {
		m := New(p, &fakeRunner{})
		m.getenv = func(string) string { return "" }
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: h})
		m, _ = press(t, tm.(Model), "a")
		for i := range all { // every cursor position, top to bottom
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			var marked []string
			for _, l := range lines {
				if _, rest, ok := strings.Cut(l, "▸"); ok {
					marked = append(marked, rest)
				}
			}
			if len(lines) != h || len(marked) != 1 || !strings.Contains(marked[0], all[i]) {
				t.Fatalf("h=%d cursor on %s: %d lines, marked rows %q\n%s", h, all[i], len(lines), marked, strings.Join(lines, "\n"))
			}
			top, bottom := -1, -1 // the box's own rows: the top bar above it and the footer below it stay
			for y, l := range lines {
				if strings.Contains(l, "╭") {
					top = y
				}
				if strings.Contains(l, "╰") {
					bottom = y
				}
			}
			if h >= 12 && (top < 1 || bottom > h-2) {
				t.Errorf("h=%d cursor on %s: the box covers the bars (rows %d..%d):\n%s", h, all[i], top, bottom, strings.Join(lines, "\n"))
			}
			m, _ = press(t, m, "j")
		}
		m, _ = press(t, m, "enter") // the cursor is on the last entry
		if data, _ := os.ReadFile(filepath.Join(dir, "avatars.json")); !strings.Contains(string(data), `"one": "`+all[len(all)-1]+`"`) {
			t.Errorf("h=%d: bound %s, want %s", h, data, all[len(all)-1])
		}
		os.Remove(filepath.Join(dir, "avatars.json"))
		p.Refresh() // the session wears the default again
	}
}
