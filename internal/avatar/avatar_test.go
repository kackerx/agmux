package avatar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if len(Sprites) != 5 || len(ids) != 5 {
		t.Fatalf("want 5 sprites with unique IDs, got %d / %d", len(Sprites), len(ids))
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
		seen[For(b, nil, "hash", id)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("hash default gave every session the same avatar: %v", seen)
	}
	bound := "bot-01" // anything but s1's hash default, so reading the binding is observable
	if For(nil, nil, "hash", "s1") == bound {
		bound = "bot-02"
	}
	if err := b.Set("s1", bound); err != nil {
		t.Fatal(err)
	}
	b2, err := LoadBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := b2.Get("s1"); !ok || id != bound || For(b2, nil, "hash", "s1") != bound || For(b2, nil, "hash", "s1") == For(nil, nil, "hash", "s1") {
		t.Fatalf("read back: %q %v, For=%s", id, ok, For(b2, nil, "hash", "s1"))
	}
	if data, _ := os.ReadFile(path); strings.TrimSpace(string(data)) != "{\n  \"s1\": \""+bound+"\"\n}" {
		t.Errorf("file format: %s", data)
	}
	if err := b2.Set("s2", "no-such-bot"); err != nil {
		t.Fatal(err)
	}
	if got, want := For(b2, nil, "hash", "s2"), For(nil, nil, "hash", "s2"); got != want {
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

// state file > config.toml's bind > default ("hash" or a sprite ID); an unknown ID falls through.
func TestForPrecedence(t *testing.T) {
	b, _ := LoadBindings(filepath.Join(t.TempDir(), "avatars.json"))
	b.Set("a", "bot-01")
	b.Set("bad", "nope")
	conf := map[string]string{"a": "bot-02", "c": "bot-03", "bad": "bot-04", "badconf": "nope"}
	for _, c := range []struct{ def, key, want string }{
		{"bot-05", "a", "bot-01"},                       // state wins
		{"bot-05", "c", "bot-03"},                       // then config
		{"bot-05", "bad", "bot-04"},                     // unknown state ID: config
		{"bot-05", "badconf", "bot-05"},                 // unknown config ID: default
		{"bot-05", "none", "bot-05"},                    // default sprite
		{"hash", "none", For(nil, nil, "hash", "none")}, // hash of the key
	} {
		if got := For(b, conf, c.def, c.key); got != c.want {
			t.Errorf("For(%q, def %q) = %s, want %s", c.key, c.def, got, c.want)
		}
	}
	if For(nil, nil, "hash", "x") != For(nil, nil, "nope", "x") {
		t.Error("an unknown default is the hash pick")
	}
}

// Set merges into what the file holds now: another board's binding, or a hand edit, survives.
func TestSetMergesWithDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "avatars.json")
	b1, _ := LoadBindings(path)
	b2, _ := LoadBindings(path)
	if err := b1.Set("one", "bot-01"); err != nil {
		t.Fatal(err)
	}
	if err := b2.Set("two", "bot-02"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"one":"bot-01","two":"bot-02","hand":"bot-03"}`), 0o644)
	if err := b1.Set("three", "bot-04"); err != nil {
		t.Fatal(err)
	}
	b3, _ := LoadBindings(path)
	for k, want := range map[string]string{"one": "bot-01", "two": "bot-02", "hand": "bot-03", "three": "bot-04"} {
		if got, _ := b3.Get(k); got != want {
			t.Errorf("%s: %q, want %q", k, got, want)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode())
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}

// A file that can't be read is never overwritten; a write that fails leaves the bindings as they were.
func TestSetFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "avatars.json")
	b, _ := LoadBindings(path)
	b.Set("one", "bot-01")
	os.WriteFile(path, []byte("{oops"), 0o644)
	if err := b.Set("two", "bot-02"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("err %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Errorf("overwritten: %s", data)
	}
	if _, ok := b.Get("two"); ok {
		t.Error("a refused binding is not in memory either")
	}
	os.Remove(path)
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	if err := b.Set("three", "bot-03"); err == nil {
		t.Fatal("a read-only directory can't be written")
	}
	if _, ok := b.Get("three"); ok {
		t.Error("memory changed though the file wasn't written")
	}
}

// "null" is an empty set, not a nil map that panics on the first binding.
func TestNullFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "avatars.json")
	os.WriteFile(path, []byte("null"), 0o644)
	b, err := LoadBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Set("one", "bot-01"); err != nil {
		t.Fatal(err)
	}
	b.m = nil // as Reload would leave it if read() let null through
	os.WriteFile(path, []byte("null"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(time.Hour))
	if err := b.Reload(); err != nil || b.m == nil {
		t.Errorf("reload of null: %v %v", err, b.m)
	}
}

// Reload follows the file when its mtime moves: another board's binding shows on the next poll;
// a broken file reports, keeps what was read, and stops reporting once fixed.
func TestReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "avatars.json")
	b, _ := LoadBindings(path)
	b.Set("one", "bot-01")
	other, _ := LoadBindings(path)
	other.Set("two", "bot-02")
	os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)) // a later mtime, whatever the clock's grain
	if err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if id, _ := b.Get("two"); id != "bot-02" {
		t.Error("another board's binding not picked up")
	}
	os.WriteFile(path, []byte("{oops"), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(2*time.Hour))
	if err := b.Reload(); err == nil || b.Reload() == nil {
		t.Error("a broken file reports, every poll")
	}
	if id, _ := b.Get("one"); id != "bot-01" {
		t.Error("a broken file keeps the bindings read before")
	}
	os.WriteFile(path, []byte(`{"one":"bot-05"}`), 0o644)
	os.Chtimes(path, time.Now(), time.Now().Add(3*time.Hour))
	if err := b.Reload(); err != nil {
		t.Errorf("fixed: %v", err)
	}
	if id, _ := b.Get("one"); id != "bot-05" {
		t.Error("the fixed file is read")
	}
}

// Prune drops from what the file holds now (another board's keys survive), and leaves a file it can't read alone.
func TestPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	b, _ := LoadBindings(path)
	b.Set("a", "1")
	b.Set("b", "2")
	other, _ := LoadBindings(path)
	other.Set("c", "3") // b's copy does not have it
	if err := b.Prune(func(k string) bool { return k != "a" }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := strings.Join(strings.Fields(string(data)), ""); got != `{"b":"2","c":"3"}` {
		t.Errorf("file %s", got)
	}
	if err := b.Prune(func(string) bool { return true }); err != nil {
		t.Error(err)
	}
	os.WriteFile(path, []byte("{oops"), 0o644)
	if err := b.Prune(func(string) bool { return false }); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("corrupt file: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Errorf("corrupt file overwritten: %q", data)
	}
}
