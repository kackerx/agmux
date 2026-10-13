package main

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kackerx/agmux/internal/avatar"
)

// `agmux avatars` lists the image sets first (id, source, frames per state), then the sprites as
// before; a set without idle frames is said on stderr, and a set named like a sprite replaces it.
func TestAvatarsOutput(t *testing.T) {
	t.Cleanup(func() { avatar.Sets = nil })
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for name, label := range map[string]string{
		"avatars/kit/idle/1.png": "a", "avatars/kit/working/1.png": "b", "avatars/kit/working/2.png": "c",
		"avatars/bot-01/idle/1.png": "d", "avatars/noidle/done/1.png": "e",
	} {
		data := string(pngOf(label))
		p := filepath.Join(dir, "agmux", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	avatars(&out, &errs)
	lines := strings.Split(out.String(), "\n")
	// the ten built-in sets, with the frames of each state that are not alternate takes (_v2…)
	want := []string{
		"aza_choi  builtin  idle 2 · think 2 · read 4 · write 4 · tool 4 · hi 4 · success 0",
		"cipher  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 0",
		"default  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 0",
		"forge  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"moss  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"nova  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"oana  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"quill  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"red  builtin  idle 2 · think 2 · read 3 · write 2 · tool 2 · hi 2 · success 0",
		"sprocket  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
	}
	want = append(want,
		"bot-01  user  idle 1 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 0",
		"kit  user  idle 1 · think 2 · read 0 · write 0 · tool 0 · hi 0 · success 0", // working/ is think's old name
		"",
		"bot-02  Boxy", // bot-01 is a set now: its sprite is gone from the list
	)
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: %q, want %q", i, lines[i], w)
		}
	}
	if strings.Count(out.String(), "bot-01") != 1 || !strings.Contains(out.String(), "\nbot-05  ") {
		t.Errorf("sprites:\n%s", out.String())
	}
	if !strings.Contains(errs.String(), `"noidle"`) || strings.Count(errs.String(), "\n") != 1 {
		t.Errorf("stderr: %q", errs.String())
	}
}

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
