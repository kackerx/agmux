package avatar

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/x/ansi"
)

func useSets(t *testing.T, fsys fstest.MapFS) []string {
	t.Helper()
	t.Cleanup(func() { Sets, problems = nil, nil })
	return UseSets(fsys)
}

func writeFiles(root string, files map[string]string) error {
	for name, data := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// A set is <id>/<state>/*.png: frames in file name order, only .png, a state with no files shows
// idle, a set with no idle is skipped and reported.
func TestLoadSets(t *testing.T) {
	probs := useSets(t, fstest.MapFS{
		"kit/idle/b.png":       file("idle-b"),
		"kit/idle/a.png":       file("idle-a"),
		"kit/idle/notes.txt":   file("not a frame"),
		"kit/idle/c.PNG":       file("upper case is not .png"),
		"kit/working/2.png":    file("work-2"),
		"kit/working/1.png":    file("work-1"),
		"kit/attention/x.gif":  file("not a frame"),
		"loose.png":            file("a file at the root"),
		"noidle/working/1.png": file("w"),
		"solo/idle/i.png":      file("solo"),
	})
	if len(probs) != 1 || !strings.Contains(probs[0], `"noidle"`) || !strings.Contains(probs[0], "idle") {
		t.Errorf("problems: %q", probs)
	}
	if len(Sets) != 2 || Sets[0].ID != "kit" || Sets[1].ID != "solo" {
		t.Fatalf("sets: %v", Sets)
	}
	kit := Sets[0]
	frames := func(m Mood) string {
		var s []string
		for _, f := range kit.Frames[m] {
			s = append(s, string(f))
		}
		return strings.Join(s, ",")
	}
	if got := frames(Dim); got != "idle-a,idle-b" {
		t.Errorf("idle frames: %s", got)
	}
	if got := frames(Scan); got != "work-1,work-2" {
		t.Errorf("working frames: %s", got)
	}
	if len(kit.Frames[Alert]) != 0 || len(kit.Frames[Normal]) != 0 {
		t.Errorf("attention / done should be empty: %v", kit.Frames)
	}
	// ids count up from baseID in load order: kit idle a b, working 1 2, then solo
	idle0 := kit.FrameID(Dim, 0)
	if idle0 != baseID || kit.FrameID(Dim, 1) != baseID+1 || kit.FrameID(Scan, 0) != baseID+2 || Sets[1].FrameID(Dim, 0) != baseID+4 {
		t.Errorf("ids: %x %x %x %x", idle0, kit.FrameID(Dim, 1), kit.FrameID(Scan, 0), Sets[1].FrameID(Dim, 0))
	}
	if kit.FrameID(Dim, 2) != idle0 || kit.FrameID(Scan, 3) != baseID+3 { // frames cycle
		t.Errorf("cycle: %x %x", kit.FrameID(Dim, 2), kit.FrameID(Scan, 3))
	}
	if kit.FrameID(Alert, 5) != kit.FrameID(Dim, 5) || kit.FrameID(Normal, 0) != idle0 { // no frames: idle's
		t.Errorf("missing state does not fall back to idle")
	}
}

// The embedded set loads whole: every state has frames and they are PNGs.
func TestBuiltinSet(t *testing.T) {
	t.Cleanup(func() { Sets, problems = nil, nil })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no user sets
	LoadSets()
	s, ok := SetByID("aza_choi")
	if !ok || s.Source != "builtin" || len(problems) != 0 {
		t.Fatalf("aza_choi: %v %v problems %q", s, ok, problems)
	}
	for _, st := range states {
		if len(s.Frames[st.Mood]) == 0 {
			t.Errorf("no %s frames", st.Dir)
		}
		for _, f := range s.Frames[st.Mood] {
			if !bytes.HasPrefix(f, []byte("\x89PNG")) {
				t.Errorf("%s frame is not a PNG", st.Dir)
			}
		}
	}
}

// A user set replaces a built-in one of the same ID; the others stay, in load order.
func TestUserSetReplacesBuiltin(t *testing.T) {
	t.Cleanup(func() { Sets, problems = nil, nil })
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := writeFiles(dir+"/agmux/avatars", map[string]string{"aza_choi/idle/1.png": "mine", "zed/idle/1.png": "z"}); err != nil {
		t.Fatal(err)
	}
	LoadSets()
	if len(Sets) != 2 || Sets[0].ID != "aza_choi" || Sets[0].Source != "user" || string(Sets[0].Frames[Dim][0]) != "mine" || Sets[1].ID != "zed" {
		t.Errorf("sets: %+v", Sets)
	}
}

func TestPlaceholder(t *testing.T) {
	const id = 0x00c0de12
	lines := Placeholder(id)
	if len(lines) != Height/2 {
		t.Fatalf("%d lines", len(lines))
	}
	for r, l := range lines {
		if w := ansi.StringWidth(l); w != Width {
			t.Errorf("row %d: StringWidth %d, want %d", r, w, Width)
		}
		if !strings.HasPrefix(l, "\x1b[38;2;192;222;18m") { // 0xc0 0xde 0x12
			t.Errorf("row %d: foreground is not the image id: %q", r, l)
		}
		cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(l, "\x1b[38;2;192;222;18m"), "\x1b[39m"), "\U0010EEEE")[1:]
		if len(cells) != Width {
			t.Fatalf("row %d: %d cells", r, len(cells))
		}
		for c, cell := range cells { // every cell says its own row and column
			if cell != string([]rune{diacritics[r], diacritics[c]}) {
				t.Errorf("row %d col %d: %+q", r, c, cell)
			}
		}
	}
}

// capture is what Upload writes, split into the escape sequences it is made of.
func capture(t *testing.T, tmux bool) (seqs []string, png []byte) {
	t.Helper()
	png = bytes.Repeat([]byte("0123456789"), 1000) // 10000 bytes: 13336 base64 characters, 4 chunks
	useSets(t, fstest.MapFS{"s/idle/1.png": &fstest.MapFile{Data: png}})
	var b bytes.Buffer
	if err := Upload(&b, Sets, tmux); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if tmux { // inside the wrapper an ESC \ is doubled, so only the wrappers' own starts split cleanly
		for _, w := range strings.Split(out, "\x1bPtmux;")[1:] {
			seqs = append(seqs, "\x1bPtmux;"+w)
		}
		return seqs, png
	}
	for _, w := range strings.SplitAfter(out, "\x1b\\") {
		if w != "" {
			seqs = append(seqs, w)
		}
	}
	return seqs, png
}

func TestUpload(t *testing.T) {
	seqs, png := capture(t, false)
	if len(seqs) != 4 {
		t.Fatalf("%d sequences, want 4 chunks", len(seqs))
	}
	var b64 strings.Builder
	for i, s := range seqs {
		head, payload, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(s, "\x1b_G"), "\x1b\\"), ";")
		if !ok || len(payload) > 4096 {
			t.Fatalf("chunk %d: %d payload bytes", i, len(payload))
		}
		more := "m=1"
		if i == len(seqs)-1 {
			more = "m=0"
		}
		want := more // later chunks carry only m
		if i == 0 {
			want = fmt.Sprintf("a=T,U=1,f=100,q=2,i=%d,c=6,r=2,%s", baseID, more)
		}
		if head != want {
			t.Errorf("chunk %d control %q, want %q", i, head, want)
		}
		b64.WriteString(payload)
	}
	if got, err := base64.StdEncoding.DecodeString(b64.String()); err != nil || !bytes.Equal(got, png) {
		t.Errorf("payload does not decode to the file: %v", err)
	}
}

// In tmux every sequence is its own DCS passthrough with its ESCs doubled.
func TestUploadTmux(t *testing.T) {
	plain, _ := capture(t, false)
	wrapped, _ := capture(t, true)
	if len(wrapped) != len(plain) {
		t.Fatalf("%d sequences, want %d", len(wrapped), len(plain))
	}
	for i, w := range wrapped {
		inner, ok := strings.CutPrefix(w, "\x1bPtmux;")
		inner, ok2 := strings.CutSuffix(inner, "\x1b\\")
		if !ok || !ok2 || strings.ReplaceAll(inner, "\x1b\x1b", "\x1b") != plain[i] || strings.Count(inner, "\x1b") != 2*strings.Count(plain[i], "\x1b") {
			t.Errorf("chunk %d is not the doubled passthrough of the plain one: %.60q", i, w)
		}
	}
}

// Every frame's image is deleted, with its data (d=I), silently.
func TestDelete(t *testing.T) {
	useSets(t, fstest.MapFS{"s/idle/1.png": file("a"), "s/idle/2.png": file("b"), "s/done/1.png": file("c")})
	var b bytes.Buffer
	Delete(&b, Sets, false)
	want := ""
	for i := range 3 {
		want += fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", baseID+i)
	}
	if b.String() != want {
		t.Errorf("%q, want %q", b.String(), want)
	}
	b.Reset()
	Delete(&b, Sets, true)
	if !strings.HasPrefix(b.String(), "\x1bPtmux;\x1b\x1b_Ga=d,d=I,i=") || strings.Count(b.String(), "\x1bPtmux;") != 3 {
		t.Errorf("tmux: %q", b.String())
	}
}

// A binding, [avatars.bind] or default may name an image set; the hash picks among the sprites
// and the sets, and a set takes the sprite of its name away from the pick.
func TestForSets(t *testing.T) {
	useSets(t, fstest.MapFS{"kit/idle/1.png": file("a"), "bot-01/idle/1.png": file("b")})
	if !Known("kit") || !Known("bot-02") || Known("nope") {
		t.Errorf("Known: kit %v bot-02 %v nope %v", Known("kit"), Known("bot-02"), Known("nope"))
	}
	if got := For(nil, map[string]string{"s": "kit"}, "hash", "s"); got != "kit" {
		t.Errorf("config bind: %s", got)
	}
	if got := For(nil, nil, "kit", "s"); got != "kit" {
		t.Errorf("default: %s", got)
	}
	if got := For(nil, map[string]string{"s": "nope"}, "kit", "s"); got != "kit" {
		t.Errorf("unknown id falls through: %s", got)
	}
	picked := map[string]bool{}
	for i := range 400 {
		picked[For(nil, nil, "hash", fmt.Sprintf("session-%d", i))] = true
	}
	if !picked["kit"] || !picked["bot-01"] || len(picked) != len(Sprites)+1 { // bot-01 is the set now: 5 sprites + 2 sets
		t.Errorf("hash picked %v", picked)
	}
	// without sets nothing changes: the hash picks only sprites
	Sets = nil
	for i := range 100 {
		if _, ok := ByID(For(nil, nil, "hash", fmt.Sprintf("session-%d", i))); !ok {
			t.Fatal("no sets, but the hash picked a non-sprite")
		}
	}
}

// With no picture to draw, a set shows a sprite hashed from its ID; a sprite shows itself.
func TestSpriteFor(t *testing.T) {
	if SpriteFor("bot-03").ID != "bot-03" {
		t.Error("a sprite ID is its sprite")
	}
	a, b := SpriteFor("kit"), SpriteFor("kit")
	if a.ID == "" || a.ID != b.ID {
		t.Errorf("set stand-in: %q %q", a.ID, b.ID)
	}
}
