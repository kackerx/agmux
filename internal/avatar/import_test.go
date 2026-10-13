package avatar

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSheet is a navy sheet of cols × rows slots (cell px each), slot i painted with cellColor(i),
// then a light gray margin right and below as an image model leaves around the grid; the slots from
// blank on are white (an older sheet's empty cells).
func fakeSheet(t *testing.T, path string, cols, rows, cell, blank int) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, cols*cell+37, rows*cell+23))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.NRGBA{230, 230, 230, 255}), image.Point{}, draw.Src) // light, but only just
	draw.Draw(im, image.Rect(0, 0, cols*cell, rows*cell), image.NewUniform(color.NRGBA{20, 20, 60, 255}), image.Point{}, draw.Src)
	for i := 0; i < cols*rows; i++ {
		c := color.Color(color.White)
		if i < blank {
			c = cellColor(i)
		}
		x, y := i%cols*cell, i/cols*cell
		draw.Draw(im, image.Rect(x+1, y+1, x+cell-1, y+cell-1), image.NewUniform(c), image.Point{}, draw.Src)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
}

// dark enough (mean <= 200) to be told from the white margin, and different for every slot
func cellColor(i int) color.NRGBA {
	return color.NRGBA{uint8(10 + i*5), uint8(190 - i*5), uint8(30 + i*3), 255}
}

func readFrame(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func TestImport(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, c := range []struct {
		layout     string
		cols, rows int
		frames     int
	}{{"30", 6, 5, 30}, {"19", 5, 4, 19}} {
		sheet := filepath.Join(t.TempDir(), "sheet.png")
		fakeSheet(t, sheet, c.cols, c.rows, 40, c.frames)
		dir, err := Import(sheet, "set"+c.layout, c.layout, false)
		if err != nil {
			t.Fatal(c.layout, err)
		}
		for i, name := range piFrames[:c.frames] {
			im := readFrame(t, filepath.Join(dir, name))
			if im.Bounds().Dx() != 128 || im.Bounds().Dy() != 128 {
				t.Fatalf("%s %s: %v", c.layout, name, im.Bounds())
			}
			want := color.NRGBAModel.Convert(cellColor(i))
			for _, p := range []image.Point{{0, 0}, {127, 0}, {0, 127}, {127, 127}, {64, 64}} {
				if got := color.NRGBAModel.Convert(im.At(p.X, p.Y)); got != want {
					t.Errorf("%s %s at %v: %v, want %v (frame %d)", c.layout, name, p, got, want, i)
				}
			}
		}
		if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o755 {
			t.Errorf("set dir %v %v, want 0755 like the files' directories", fi, err)
		}
		var n int
		filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
			if !d.IsDir() {
				n++
			}
			return nil
		})
		if n != c.frames {
			t.Errorf("layout %s: %d files, want %d", c.layout, n, c.frames)
		}
	}
}

// the names are pi-agent-portrait's, in its order (draw-portrait's FRAMES, then NEW_FRAMES)
func TestPiFrameNames(t *testing.T) {
	for i, want := range map[int]string{0: "idle/idle.png", 1: "idle/idle_blink.png", 2: "hi/hi1.png", 8: "think/think.png", 10: "read/read1.png",
		12: "write/write1.png", 14: "tool/tool1.png", 18: "compact/compact1.png", 19: "success/success1.png", 20: "success/success2.png",
		23: "wait/wait1.png", 24: "wait/wait2.png", 29: "heard/heard1.png"} {
		if piFrames[i] != want {
			t.Errorf("frame %d is %s, want %s", i, piFrames[i], want)
		}
	}
	if len(piFrames) != 30 {
		t.Errorf("%d frames", len(piFrames))
	}
}

func TestImportErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	sheet := filepath.Join(t.TempDir(), "sheet.png")
	fakeSheet(t, sheet, 6, 5, 40, 30)
	text := filepath.Join(t.TempDir(), "sheet.txt")
	os.WriteFile(text, []byte("not a png"), 0o644)
	white := filepath.Join(t.TempDir(), "white.png")
	wi := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(wi, wi.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	wf, _ := os.Create(white)
	png.Encode(wf, wi)
	wf.Close()
	for _, c := range []struct{ sheet, name, layout, want string }{
		{sheet, "x", "20", "--layout"},
		{sheet, "", "30", "--name"},
		{sheet, "hash", "30", "reserved"},
		{sheet, "a/b", "30", "--name"},
		{sheet, ".x", "30", "--name"},
		{text, "x", "30", "cannot decode"},
		{white, "x", "30", "dark"},
		{filepath.Join(root, "missing.png"), "x", "30", "missing.png"},
	} {
		if _, err := Import(c.sheet, c.name, c.layout, false); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "agmux", "avatars", "x")); err == nil {
		t.Error("a failed import left a set behind")
	}
	// a set that exists: an error that says how to go on; --force replaces it, old files and all
	dir, err := Import(sheet, "kit", "30", false)
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "idle", "old.png")
	os.WriteFile(stale, []byte("x"), 0o644)
	if _, err := Import(sheet, "kit", "30", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("existing set: %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Error("a refused import touched the set")
	}
	if _, err := Import(sheet, "kit", "30", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("--force kept a stale file")
	}
	if ents, _ := os.ReadDir(filepath.Dir(dir)); len(ents) != 1 {
		t.Errorf("leftovers beside the set: %v", ents)
	}
}
