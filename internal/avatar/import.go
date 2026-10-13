package avatar

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/kackerx/agmux/internal/xdg"
)

// The frames of a pi-agent-portrait sheet, left to right, top to bottom (its scripts/draw-portrait:
// FRAMES, then NEW_FRAMES). Layout "19" is the first 19 on 5 × 4; "30" all of them on 6 × 5.
var piFrames = []string{"idle/idle.png", "idle/idle_blink.png", "hi/hi1.png", "hi/hi2.png", "talk/talk_close.png",
	"talk/talk_small.png", "talk/talk_mid.png", "talk/talk_wide.png", "think/think.png",
	"think/think_hard.png", "read/read1.png", "read/read2.png", "write/write1.png",
	"write/write2.png", "tool/tool1.png", "tool/tool2.png", "failure/failure1.png",
	"failure/failure2.png", "compact/compact1.png",
	"success/success1.png", "success/success2.png", "sleep/sleep1.png", "sleep/sleep2.png",
	"wait/wait1.png", "wait/wait2.png", "interrupted/interrupted1.png", "search/search1.png",
	"bash/bash1.png", "error/error1.png", "heard/heard1.png"}

const importSize = 128 // pixels a frame is cut to

// Import cuts the pi-agent-portrait sheet (a PNG) into the set <name> under the user's avatars
// directory and returns that directory. An existing set is an error unless force replaces it.
func Import(sheet, name, layout string, force bool) (string, error) {
	n, cols, rows := 30, 6, 5
	if layout == "19" {
		n, cols, rows = 19, 5, 4
	} else if layout != "30" {
		return "", fmt.Errorf("--layout %q: use 30 (6 × 5 cells) or 19 (5 × 4)", layout)
	}
	if name == "" || name == "hash" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("--name %q: a set ID is a plain directory name, and not \"hash\" (reserved)", name)
	}
	cfg, err := xdg.ConfigDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(cfg, "avatars")
	dst := filepath.Join(root, name)
	if _, err := os.Stat(dst); err == nil && !force {
		return "", fmt.Errorf("%s exists: pass --force to replace it, or pick another --name", dst)
	}
	f, err := os.Open(sheet)
	if err != nil {
		return "", err
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		return "", fmt.Errorf("%s: cannot decode as PNG (%v); the sheet must be a .png file", sheet, err)
	}
	frames, err := cutSheet(im, n, cols, rows)
	if err != nil {
		return "", fmt.Errorf("%s: %v", sheet, err)
	}
	// written beside the target and renamed into place, so a failure never leaves half a set (a "." name is not a set)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil { // MkdirTemp makes 0700; this becomes the set's directory
		return "", err
	}
	for i, fr := range frames {
		p := filepath.Join(tmp, piFrames[i])
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := writePNG(p, fr); err != nil {
			return "", err
		}
	}
	// a set that is there (only reached when forced) goes aside first and comes back if the new one can't be moved in
	bak := tmp + ".old"
	if err := os.Rename(dst, bak); err == nil {
		defer os.RemoveAll(bak)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Rename(bak, dst)
		return "", err
	}
	return dst, nil
}

func writePNG(path string, im image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, im); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// cutSheet is draw-portrait's cut(): image models keep the grid, whose extent is where the dark
// background ends (a column at 1/20 of the width gives the bottom, a row at 1/10 of the height the
// right edge; 19-cell sheets have white empty cells at the end); each frame is the square, 97% of a
// slot, centered in it, shrunk to importSize by nearest neighbour.
// ponytail: nearest neighbour is blocky on a photo-like sheet; pixel-art sheets are fine.
func cutSheet(im image.Image, n, cols, rows int) ([]*image.NRGBA, error) {
	b := im.Bounds()
	dark := func(x, y int) bool {
		r, g, bl, _ := im.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return (r>>8)+(g>>8)+(bl>>8) <= 600 // mean of the three channels <= 200
	}
	bottom, right := 0, 0
	for y := b.Dy() - 1; y >= 0 && bottom == 0; y-- {
		if dark(b.Dx()/20, y) {
			bottom = y + 1
		}
	}
	for x := b.Dx() - 1; x >= 0 && right == 0; x-- {
		if dark(x, b.Dy()/10) {
			right = x + 1
		}
	}
	cw, ch := float64(right)/float64(cols), float64(bottom)/float64(rows)
	side := int(math.Min(cw, ch) * 0.97)
	if side < 1 {
		return nil, fmt.Errorf("no dark background found at the left or top of the sheet: is it a %d × %d pi-agent-portrait sheet on a dark navy background?", cols, rows)
	}
	out := make([]*image.NRGBA, n)
	for i := range out {
		left := int(math.RoundToEven((float64(i%cols)+0.5)*cw - float64(side)/2))
		top := int(math.RoundToEven((float64(i/cols)+0.5)*ch - float64(side)/2))
		fr := image.NewNRGBA(image.Rect(0, 0, importSize, importSize))
		for y := range importSize {
			for x := range importSize {
				fr.Set(x, y, im.At(b.Min.X+left+(2*x+1)*side/(2*importSize), b.Min.Y+top+(2*y+1)*side/(2*importSize)))
			}
		}
		out[i] = fr
	}
	return out, nil
}
