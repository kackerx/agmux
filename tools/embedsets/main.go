// Command embedsets copies pi-agent-portrait character directories into internal/avatar/sets, the way
// the built-in sets are stored: only the seven state directories agmux shows, each frame already
// cut to the centered 3:2 part (what avatar.cropPNG would upload) as a PNG.
//
//	go run ./tools/embedsets internal/avatar/sets ~/Pictures/pi-agent-portraits/emotes/developer ...
//
// Each source directory's name is the set's ID. Re-running replaces a set's directory.
package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var states = []string{"idle", "think", "read", "write", "tool", "hi", "success"}

// alternateTake: pi-agent-portrait's `_v2` takes are not frames (avatar.alternateTake).
var alternateTake = regexp.MustCompile(`_v\d+\.png$`)

// maxHeight is avatar.maxHeight: a taller picture would be shrunk again when loaded.
const maxHeight = 128

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: embedsets <internal/avatar/sets> <character dir>...")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "embedsets:", err)
		os.Exit(1)
	}
}

func run(dest string, srcs []string, out io.Writer) error {
	total := 0
	for _, src := range srcs {
		id := filepath.Base(src)
		if id == "." || id == ".." || id == string(filepath.Separator) { // dest/<id> is replaced: it must not be dest itself, or its parent
			return fmt.Errorf("%q does not name a character: give the directory by its own name (…/emotes/developer)", src)
		}
		n, err := convert(src, filepath.Join(dest, id))
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%-10s %3d frames\n", id, n)
		total += n
	}
	fmt.Fprintln(out, "total", total, "frames")
	return nil
}

// convert makes dst out of src's state directories. It is built beside dst and moved in only when
// it is complete, so that a source that is wrong leaves what is there alone.
func convert(src, dst string) (frames int, err error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), ".embedsets-") // a dot: neither the loader nor go:embed takes it
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp) // gone after the rename; what a failure left
	for _, st := range states {
		files, _ := os.ReadDir(filepath.Join(src, st))
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".png") || alternateTake.MatchString(f.Name()) {
				continue
			}
			if err := convertFrame(filepath.Join(src, st, f.Name()), filepath.Join(tmp, st, f.Name())); err != nil {
				return frames, err
			}
			frames++
		}
	}
	if frames == 0 {
		return 0, fmt.Errorf("%s: no frames in %v", src, states)
	}
	if err := os.RemoveAll(dst); err != nil {
		return 0, err
	}
	return frames, os.Rename(tmp, dst)
}

// convertFrame writes the centered 3:2 part of src to dst.
func convertFrame(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	b := im.Bounds()
	// 3k × 2k, the biggest centered one: exactly 3:2, so that avatar.cropFor leaves it alone (its
	// rounding would trim a column off a 128 × 85)
	k := min(b.Dx()/3, b.Dy()/2)
	x, y, w, h := (b.Dx()-3*k)/2, (b.Dy()-2*k)/2, 3*k, 2*k
	if h > maxHeight { // ponytail: the sources are 128 × 128; shrink bigger ones first (the board would shrink them again at load)
		return fmt.Errorf("%s: %d high after cropping, over %d: scale the set down first", src, h, maxHeight)
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), im, image.Pt(b.Min.X+x, b.Min.Y+y), draw.Src)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	o, err := os.Create(dst)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(o, out); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}
