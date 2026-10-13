package avatar_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/kackerx/agmux/internal/avatar"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func init() { lipgloss.SetColorProfile(termenv.TrueColor) }

func TestExtRenderShape(t *testing.T) {
	if len(avatar.Sprites) != 5 {
		t.Fatalf("%d sprites, want 5", len(avatar.Sprites))
	}
	ids := map[string]bool{}
	for _, s := range avatar.Sprites {
		if s.ID == "" || s.Name == "" || ids[s.ID] {
			t.Errorf("bad/duplicate id or empty name: %+v", s.ID)
		}
		ids[s.ID] = true
		for _, at := range []string{"claude", "codex", "gemini", ""} {
			for _, m := range []avatar.Mood{avatar.Normal, avatar.Scan, avatar.Alert, avatar.Dim} {
				for f := 0; f < 8; f++ {
					out := avatar.Render(s, at, m, f)
					lines := strings.Split(ansi.ReplaceAllString(out, ""), "\n")
					if len(lines) != 2 {
						t.Fatalf("%s/%s/mood%d/f%d: %d lines", s.ID, at, m, f, len(lines))
					}
					for _, l := range lines {
						if n := len([]rune(l)); n != 6 || strings.Trim(l, " ▀▄") != "" {
							t.Fatalf("%s/%s/mood%d/f%d: line %q is %d cells", s.ID, at, m, f, l, n)
						}
					}
					if !strings.Contains(out, "\x1b[38;2;") {
						t.Fatalf("%s: no truecolor fg escape in %q", s.ID, out)
					}
				}
			}
		}
	}
}

func TestExtMoods(t *testing.T) {
	const alert = "2;255;97;136" // fg (38;) or bg (48;) depending on which half-pixel the eye is
	for _, s := range avatar.Sprites {
		scan := map[string]bool{}
		for f := 0; f < 4; f++ {
			scan[avatar.Render(s, "claude", avatar.Scan, f)] = true
		}
		if len(scan) < 2 {
			t.Errorf("%s: Scan renders %d distinct frames over 4, want >=2 (eyes must move)", s.ID, len(scan))
		}
		for f := 0; f < 8; f++ {
			for _, m := range []avatar.Mood{avatar.Normal, avatar.Scan, avatar.Dim} {
				if strings.Contains(avatar.Render(s, "claude", m, f), alert) {
					t.Errorf("%s: red eyes outside Alert (mood %d frame %d)", s.ID, m, f)
				}
			}
		}
		on, off := avatar.Render(s, "claude", avatar.Alert, 0), avatar.Render(s, "claude", avatar.Alert, 1)
		if !strings.Contains(on, alert) || strings.Contains(off, alert) || on == off {
			t.Errorf("%s: Alert must flash red (frame0 red=%v frame1 red=%v)", s.ID, strings.Contains(on, alert), strings.Contains(off, alert))
		}
		if avatar.Render(s, "claude", avatar.Dim, 0) == avatar.Render(s, "claude", avatar.Normal, 0) {
			t.Errorf("%s: Dim == Normal", s.ID)
		}
		if avatar.Render(s, "claude", avatar.Dim, 0) != avatar.Render(s, "claude", avatar.Dim, 3) {
			t.Errorf("%s: Dim must be static", s.ID)
		}
		a, b, c := avatar.Render(s, "claude", avatar.Normal, 0), avatar.Render(s, "codex", avatar.Normal, 0), avatar.Render(s, "gemini", avatar.Normal, 0)
		if a == b || b == c || a == c || c != avatar.Render(s, "", avatar.Normal, 0) {
			t.Errorf("%s: band colour must differ for claude/codex/other and 'other' must be a single grey", s.ID)
		}
	}
}

func TestExtBindings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "agmux") // parent dirs do not exist yet
	path := filepath.Join(dir, "avatars.json")
	b, err := avatar.LoadBindings(path)
	if err != nil {
		t.Fatal("missing file must be an empty set:", err)
	}
	if _, ok := b.Get("s1"); ok {
		t.Fatal("empty set returned a binding")
	}
	if err := b.Set("s1", "bot-03"); err != nil {
		t.Fatal(err)
	}
	if err := b.Set("s2", "bot-05"); err != nil {
		t.Fatal(err)
	}
	b2, err := avatar.LoadBindings(path) // fresh reader
	if err != nil {
		t.Fatal(err)
	}
	for s, want := range map[string]string{"s1": "bot-03", "s2": "bot-05"} {
		if got, ok := b2.Get(s); !ok || got != want {
			t.Errorf("read back %s: %q %v want %q", s, got, ok, want)
		}
	}
	raw, _ := os.ReadFile(path)
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) != 2 {
		t.Errorf("file must be exactly {\"<sessionID>\":\"<avatarID>\"}: %s (%v)", raw, err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("leftover files after Set: %v", ents)
	}
	if err := b2.Set("s1", "bot-01"); err != nil { // overwrite keeps the others
		t.Fatal(err)
	}
	b3, _ := avatar.LoadBindings(path)
	if g1, _ := b3.Get("s1"); g1 != "bot-01" {
		t.Errorf("overwrite: %q", g1)
	}
	if g2, _ := b3.Get("s2"); g2 != "bot-05" {
		t.Errorf("overwrite dropped s2: %q", g2)
	}
	fmt.Fprintf(os.Stderr, "file after writes: %s\n", func() []byte { r, _ := os.ReadFile(path); return r }())
}

func TestExtBadFile(t *testing.T) {
	for _, body := range []string{"{not json", "", "[]", `{"s":1}`, `"str"`} {
		path := filepath.Join(t.TempDir(), "avatars.json")
		os.WriteFile(path, []byte(body), 0o644)
		_, err := avatar.LoadBindings(path)
		t.Logf("body %-12q -> err=%v", body, err)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("body %q: want an error naming %s, got %v", body, path, err)
		}
	}
	path := filepath.Join(t.TempDir(), "d")
	os.Mkdir(path, 0o755) // unreadable as a file
	if _, err := avatar.LoadBindings(path); err == nil {
		t.Error("directory in place of the file: want error")
	}
}

func TestExtFallbackAndHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "avatars.json")
	os.WriteFile(path, []byte(`{"s1":"no-such-bot","s2":"bot-04","s3":""}`), 0o644)
	b, err := avatar.LoadBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := avatar.LoadBindings(filepath.Join(t.TempDir(), "none.json"))
	for _, s := range []string{"s1", "s3"} {
		if got, want := avatar.For(b, nil, "hash", s), avatar.For(empty, nil, "hash", s); got != want {
			t.Errorf("%s bound to a nonexistent id: got %s, want hash default %s", s, got, want)
		}
	}
	if got := avatar.For(b, nil, "hash", "s2"); got != "bot-04" {
		t.Errorf("bound id ignored: %s", got)
	}
	count := map[string]int{}
	for i := 0; i < 600; i++ {
		id := fmt.Sprintf("%08x-%04x-4000-8000-%012x", i*2654435761, i, i*40503)
		a, c := avatar.For(empty, nil, "hash", id), avatar.For(empty, nil, "hash", id)
		if a != c {
			t.Fatalf("hash default not stable for %s", id)
		}
		count[a]++
	}
	t.Logf("600 uuid-like ids over sprites: %v", count)
	if len(count) != 5 {
		t.Errorf("only %d of 5 sprites ever assigned by default", len(count))
	}
	for id, n := range count {
		if n < 60 || n > 140 {
			t.Errorf("%s got %d/600 (expected ~100)", id, n)
		}
	}
}
