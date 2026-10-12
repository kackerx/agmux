package avatar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestRenderShape(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	ids := map[string]bool{}
	for _, s := range Sprites {
		ids[s.ID] = true
		for _, m := range []Mood{Normal, Scan, Alert, Dim} {
			for f := range 4 {
				lines := strings.Split(Render(s, "claude", m, f), "\n")
				if len(lines) != 2 {
					t.Fatalf("%s mood %d: %d lines", s.ID, m, len(lines))
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != 6 {
						t.Errorf("%s mood %d frame %d line %d: width %d", s.ID, m, f, i, w)
					}
				}
			}
		}
	}
	if len(Sprites) != 6 || len(ids) != 6 {
		t.Fatalf("want 6 sprites with unique IDs, got %d / %d", len(Sprites), len(ids))
	}
}

func TestMoods(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	s := Sprites[0]
	normal := Render(s, "claude", Normal, 0)
	if Render(s, "claude", Scan, 1) == normal || Render(s, "claude", Scan, 3) == normal || Render(s, "claude", Scan, 1) == Render(s, "claude", Scan, 3) {
		t.Error("Scan: frames 1 and 3 should look right / left, unlike normal")
	}
	red := ";2;255;97;136" // fg or bg: an eye can sit in either half of a cell
	if !strings.Contains(Render(s, "claude", Alert, 0), red) || strings.Contains(Render(s, "claude", Alert, 1), red) {
		t.Error("Alert: red eyes on even frames only")
	}
	if Render(s, "claude", Dim, 0) == normal {
		t.Error("Dim should differ from normal")
	}
	if Render(s, "claude", Normal, 0) == Render(s, "codex", Normal, 0) {
		t.Error("band should differ by agentType")
	}
}

func TestBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "avatars.json")
	b, err := LoadBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Get("s1"); ok {
		t.Fatal("missing file should be empty")
	}
	seen := map[string]bool{}
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		seen[For(b, id).ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("hash default gave every session the same avatar: %v", seen)
	}
	bound := "bot-01" // anything but s1's hash default, so reading the binding is observable
	if For(nil, "s1").ID == bound {
		bound = "bot-02"
	}
	if err := b.Set("s1", bound); err != nil {
		t.Fatal(err)
	}
	b2, err := LoadBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := b2.Get("s1"); !ok || id != bound || For(b2, "s1").ID != bound || For(b2, "s1").ID == For(nil, "s1").ID {
		t.Fatalf("read back: %q %v, For=%s", id, ok, For(b2, "s1").ID)
	}
	if data, _ := os.ReadFile(path); strings.TrimSpace(string(data)) != "{\n  \"s1\": \""+bound+"\"\n}" {
		t.Errorf("file format: %s", data)
	}
	if err := b2.Set("s2", "no-such-bot"); err != nil {
		t.Fatal(err)
	}
	if got, want := For(b2, "s2").ID, For(nil, "s2").ID; got != want {
		t.Errorf("unknown bound id: got %s, want the hash default %s", got, want)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBindings(path); err == nil {
		t.Error("corrupt file should error")
	}
}

func TestBandColors(t *testing.T) {
	for at, want := range map[string]lipgloss.Color{"claude": "#ffb86c", "codex": "#50fa7b", "gemini": "#6272a4", "": "#6272a4"} {
		if got := BandColor(at); got != want {
			t.Errorf("BandColor(%q) = %s, want %s", at, got, want)
		}
	}
}
