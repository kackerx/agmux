package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range im.Pix {
		im.Pix[i] = uint8(i)
	}
	im.Set(0, 0, color.NRGBA{1, 2, 3, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sets is a sets directory with one set "old" worth keeping.
func sets(t *testing.T) string {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "old", "idle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old", "idle", "idle.png"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func kept(t *testing.T, dir string) {
	t.Helper()
	if b, err := os.ReadFile(filepath.Join(dir, "old", "idle", "idle.png")); err != nil || string(b) != "keep" {
		t.Errorf("what was in the sets directory is gone: %q, %v", b, err)
	}
}

// A source that does not name a character (`.`, `..`) would make the directory to replace the sets
// directory itself or its parent: refused, and nothing is touched.
func TestRefusesSourceWithoutAName(t *testing.T) {
	dir := sets(t)
	for _, src := range []string{".", "..", "./", "a/.."} {
		var out bytes.Buffer
		if err := run(dir, []string{src}, &out); err == nil || !strings.Contains(err.Error(), "does not name a character") {
			t.Errorf("%q: %v", src, err)
		}
	}
	kept(t, dir)
}

// A source without frames fails before the set that is there is touched; no half-built directory is left.
func TestFailedConversionLeavesTheSetAlone(t *testing.T) {
	dir := sets(t)
	src := filepath.Join(t.TempDir(), "old") // same name as the set that is there
	writePNG(t, filepath.Join(src, "wrongstate", "x.png"), 9, 6)
	if err := run(dir, []string{src}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no frames") {
		t.Fatalf("no frames: %v", err)
	}
	kept(t, dir)
	// the second frame is not a PNG: the first one converted is not left behind either
	bad := filepath.Join(t.TempDir(), "old")
	writePNG(t, filepath.Join(bad, "idle", "a.png"), 9, 6)
	if err := os.WriteFile(filepath.Join(bad, "idle", "b.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, []string{bad}, &bytes.Buffer{}); err == nil {
		t.Fatal("a bad frame went through")
	}
	kept(t, dir)
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("left in the sets directory: %v", ents)
	}
}

// A good source replaces the set: only the seven states, alternate takes left out, 3:2 frames.
func TestConvertReplacesTheSet(t *testing.T) {
	dir := sets(t)
	src := filepath.Join(t.TempDir(), "old")
	writePNG(t, filepath.Join(src, "idle", "idle.png"), 128, 128)
	writePNG(t, filepath.Join(src, "idle", "idle_v2.png"), 128, 128)
	writePNG(t, filepath.Join(src, "talk", "talk.png"), 128, 128)
	var out bytes.Buffer
	if err := run(dir, []string{src}, &out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "old", "idle", "idle.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != 126 || cfg.Height != 84 {
		t.Errorf("frame %d × %d, %v: want 126 × 84", cfg.Width, cfg.Height, err)
	}
	for _, gone := range []string{"idle/idle_v2.png", "talk"} {
		if _, err := os.Stat(filepath.Join(dir, "old", gone)); err == nil {
			t.Errorf("%s was copied", gone)
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 || !strings.Contains(out.String(), "1 frames") {
		t.Errorf("sets directory %v, output %q", ents, out.String())
	}
}
