package avatar

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	"image/draw"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/kackerx/agmux/internal/xdg"
)

// Image sets are pictures instead of pixel sprites: a directory <id>/<state>/*.png, drawn through
// kitty's graphics protocol. The picture is uploaded once under an image ID; the card holds Unicode
// placeholder characters whose foreground color is that ID, so tmux treats the avatar as plain text.

//go:embed sets
var builtin embed.FS

// States are pi-emote's, so a pi-emote character directory works as it is: a set keeps its frames
// in <id>/<state>/*.png. A state with no frames falls back along fallback, ending at idle. The
// directory names working, attention and done are older spellings of think, hi and success.
var States = [...]string{"idle", "think", "read", "write", "tool", "hi", "success"}

var (
	fallback = map[string]string{"read": "think", "write": "think", "tool": "think", "think": "idle", "hi": "idle", "success": "idle"}
	oldName  = map[string]string{"think": "working", "hi": "attention", "success": "done"}
)

// crop is a rectangle in pixels.
type crop struct{ x, y, w, h int }

// cropFor is the centered 3:2 part of a w × h picture, which a 6 × 2 cell box (cells are about 1:2)
// shows without bars at the sides or distortion: a picture wider than 3:2 loses its sides, any
// other loses top and bottom (a square one about a sixth of each). Sizes round down.
func cropFor(w, h int) crop {
	if w*2 > h*3 {
		cw := max(h*3/2, 1)
		return crop{(w - cw) / 2, 0, cw, h}
	}
	ch := max(w*2/3, 1)
	return crop{0, (h - ch) / 2, w, ch}
}

// cropPNG is the centered 3:2 part of a PNG, no more than maxHeight pixels high, as a PNG (a picture
// that is that already goes as it is). kitty can crop by itself (x, y, w, h in the upload), but
// ignores that for the virtual placements the placeholders show, so the pixels are cut here. A frame
// over maxSide pixels either way is refused before it is decoded.
// ponytail: shrinking takes the nearest pixel, which suits pixel art; a photograph would alias.
func cropPNG(data []byte) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data)) // the header only
	if err != nil {
		return nil, err
	}
	if cfg.Width > maxSide || cfg.Height > maxSide {
		return nil, fmt.Errorf("%d × %d is over %d × %d", cfg.Width, cfg.Height, maxSide, maxSide)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := im.Bounds()
	c := cropFor(b.Dx(), b.Dy())
	if c.w == b.Dx() && c.h == b.Dy() && c.h <= maxHeight {
		return data, nil
	}
	var out *image.NRGBA
	if c.h > maxHeight {
		out = image.NewNRGBA(image.Rect(0, 0, max(c.w*maxHeight/c.h, 1), maxHeight))
		for y := range out.Rect.Dy() {
			for x := range out.Rect.Dx() {
				out.Set(x, y, im.At(b.Min.X+c.x+(2*x+1)*c.w/(2*out.Rect.Dx()), b.Min.Y+c.y+(2*y+1)*c.h/(2*maxHeight)))
			}
		}
	} else {
		out = image.NewNRGBA(image.Rect(0, 0, c.w, c.h))
		draw.Draw(out, out.Bounds(), im, image.Pt(b.Min.X+c.x, b.Min.Y+c.y), draw.Src)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Set is one image avatar. Its ID shares the sprite ID namespace.
type Set struct {
	ID     string
	Source string              // "builtin" or "user"
	Frames map[string][][]byte // PNG data per state, in file name order; idle is always there
	sent   map[string][][]byte // what is uploaded for each frame: its centered 3:2 part (cropPNG)
	ids    map[string][]uint32 // the image ID of each frame: a hash of the set, state, file name and bytes, so the
	// same picture always has the same ID (a restart overwrites it; two boards, or a user edit between
	// them, can't mix pictures up) and a different one almost surely a different ID
}

// Sets are the image sets, filled by LoadSets; empty means pixel sprites only.
var Sets []*Set

var problems []string

// Problems are the sets LoadSets skipped, one line each.
func Problems() []string { return problems }

// SetByID finds an image set by its ID.
func SetByID(id string) (*Set, bool) {
	for _, s := range Sets {
		if s.ID == id {
			return s, true
		}
	}
	return nil, false
}

// Known reports whether id names an image set or a sprite.
func Known(id string) bool {
	if _, ok := SetByID(id); ok {
		return true
	}
	_, ok := ByID(id)
	return ok
}

// LoadSets reads the built-in sets and then the user's ($XDG_CONFIG_HOME/agmux/avatars/<id>/); a
// user set replaces a built-in one of the same ID. A set without idle frames is skipped and
// reported by Problems.
func LoadSets() {
	sub, _ := fs.Sub(builtin, "sets")
	all, problems1 := loadSets(sub, "builtin")
	var problems2 []string
	if dir, err := xdg.ConfigDir(); err == nil {
		var user []*Set
		user, problems2 = loadSets(os.DirFS(path.Join(dir, "avatars")), "user")
		for _, u := range user {
			if i := slices.IndexFunc(all, func(s *Set) bool { return s.ID == u.ID }); i >= 0 {
				all[i] = u
			} else {
				all = append(all, u)
			}
		}
	}
	Sets, problems = all, append(problems1, problems2...)
}

// UseSets makes the sets under fsys the only ones; for tests. It returns what it skipped.
func UseSets(fsys fs.FS) []string {
	Sets, problems = loadSets(fsys, "user")
	return problems
}

// pixelsVersion goes into every image ID and names what is uploaded of a file (the centered 3:2 part,
// at most maxHeight pixels high): change it when that changes, so that a board of another version
// running at the same time never finds a different picture under an ID it knows.
const pixelsVersion = "3:2-128"

// maxHeight is the most pixels high an uploaded frame is: a 6 × 2 cell box shows nothing finer.
// maxSide is the most a frame may measure in either direction (a small file can claim a huge image).
const (
	maxHeight = 128
	maxSide   = 4096
)

// frameID is a 24-bit image ID (it rides in the placeholders' foreground color); never 0, which
// means "no image" to kitty.
func frameID(set, state, file string, data []byte) uint32 {
	h := fnv.New32a()
	io.WriteString(h, pixelsVersion+"\x00"+set+"\x00"+state+"\x00"+file+"\x00")
	h.Write(data)
	return max(h.Sum32()&0xffffff, 1)
}

// alternateTake matches idle_v2.png and the like: pi-agent-portrait draws a state again as another
// "take" and picks one of them at random each time the state starts, so they are not animation
// frames. idle_2.png is a frame.
var alternateTake = regexp.MustCompile(`_v\d+\.png$`)

// loadSets reads every <id>/<state>/*.png under fsys. Only .png files count, not hidden ones or alternate takes (macOS
// leaves ._ copies next to files on some volumes); a directory may be a symbolic link; a missing
// root is no sets.
func loadSets(fsys fs.FS, source string) (sets []*Set, problems []string) {
	dirs, _ := fs.ReadDir(fsys, ".") // ReadDir sorts by name
	for _, d := range dirs {
		if fi, err := fs.Stat(fsys, d.Name()); err != nil || !fi.IsDir() || strings.HasPrefix(d.Name(), ".") { // Stat follows links, the entry's own IsDir does not
			continue
		}
		if d.Name() == "hash" { // [avatars] default = "hash" would mean this set
			problems = append(problems, `avatar set "hash" skipped: "hash" is reserved for picking one by hashing; rename the directory`)
			continue
		}
		s := &Set{ID: d.Name(), Source: source, Frames: map[string][][]byte{}, ids: map[string][]uint32{}, sent: map[string][][]byte{}}
		var bad []string
		for _, st := range States {
			dirs := []string{st}
			if old, ok := oldName[st]; ok {
				dirs = append(dirs, old) // used only when the new name has no frames
			}
			for _, dir := range dirs {
				files, _ := fs.ReadDir(fsys, path.Join(d.Name(), dir))
				for _, f := range files {
					if f.IsDir() || !strings.HasSuffix(f.Name(), ".png") || strings.HasPrefix(f.Name(), ".") || alternateTake.MatchString(f.Name()) {
						continue
					}
					data, err := fs.ReadFile(fsys, path.Join(d.Name(), dir, f.Name()))
					if err != nil {
						continue
					}
					crop, err := cropPNG(data)
					if err != nil {
						bad = append(bad, dir+"/"+f.Name())
						continue
					}
					s.Frames[st] = append(s.Frames[st], data)
					s.ids[st] = append(s.ids[st], frameID(d.Name(), dir, f.Name(), data))
					s.sent[st] = append(s.sent[st], crop)
				}
				if len(s.Frames[st]) > 0 {
					break
				}
			}
		}
		if len(bad) > 0 {
			problems = append(problems, fmt.Sprintf("avatar set %q: %d frame(s) are not usable PNGs (invalid, or over 4096 × 4096) and are skipped, first %s", d.Name(), len(bad), bad[0]))
		}
		if len(s.Frames["idle"]) == 0 {
			problems = append(problems, fmt.Sprintf("avatar set %q has no idle/*.png: skipped", d.Name()))
			continue
		}
		sets = append(sets, s)
	}
	return
}

// Listing is every avatar a person can pick, in the order the picker shows them: the image sets,
// then the sprites (a set shadows a sprite of the same ID).
func Listing() []string {
	var ids []string
	for _, s := range Sets {
		ids = append(ids, s.ID)
	}
	for _, s := range Sprites {
		if _, shadowed := SetByID(s.ID); !shadowed {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// FrameCounts is how many frames the set has per state, as "idle 2 · think 4 · read 0 · …".
func (s *Set) FrameCounts() string {
	var parts []string
	for _, st := range States {
		parts = append(parts, fmt.Sprintf("%s %d", st, len(s.Frames[st])))
	}
	return strings.Join(parts, " · ")
}

// FrameID is the image ID to show for state at tick: the frames of the state or of the first state
// on its fallback chain that has any, cycling once a tick. Idle is different: its first frame is
// the resting face and the others (a blink) show for one tick in every 8.
func (s *Set) FrameID(state string, tick int) uint32 {
	st := state
	for len(s.ids[st]) == 0 && st != "idle" {
		if st = fallback[st]; st == "" {
			st = "idle"
		}
	}
	ids := s.ids[st]
	if st == "idle" && len(ids) > 1 {
		if tick > 0 && tick%8 == 0 {
			return ids[1+(tick/8-1)%(len(ids)-1)]
		}
		return ids[0]
	}
	return ids[tick%len(ids)]
}

// diacritics are kitty's row / column numbers 0..5 as combining marks (rowcolumn-diacritics.txt).
var diacritics = [...]rune{0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D}

// Placeholder returns the Height/2 lines of Width cells that show image id. Every cell carries its
// own row and column, so a line cut anywhere still says which piece of the picture is left.
func Placeholder(id uint32) []string {
	lines := make([]string, Height/2)
	for r := range lines {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", id>>16&255, id>>8&255, id&255)
		for c := range Width {
			b.WriteRune('\U0010EEEE')
			b.WriteRune(diacritics[r])
			b.WriteRune(diacritics[c])
		}
		lines[r] = b.String() + "\x1b[39m"
	}
	return lines
}

// chunkSize is what kitty takes of base64 in one escape sequence.
const chunkSize = 4096

// ttyChunk is the base64 per sequence on a client's tty: with its framing a sequence must fit in the
// 1024 bytes a pty holds, or one write of it is taken in pieces and tmux's own drawing (a BEL or an
// ST in it ends the sequence early in kitty, which then prints what is left as text) lands between them.
const ttyChunk = 768

// Via is where Upload writes: how the bytes get to the terminal that stores the pictures.
type Via int

const (
	// ToTerminal: agmux's own stdout is the terminal (outside tmux): it first switches to the
	// alternate screen, where the board will run.
	ToTerminal Via = iota
	// ToPassthrough: stdout is a tmux pane: each sequence goes through tmux's DCS passthrough.
	// It needs allow-passthrough, an attached client, and reaches only one tmux layer.
	ToPassthrough
	// ToTTY: w is the tty of the tmux client in the real terminal: bare sequences, no tmux in
	// the way (so no passthrough, nesting or attached-client conditions).
	ToTTY
)

// Upload sends every frame of sets to the terminal as a virtual placement of Width × Height/2 cells
// (U=1: nothing is drawn until placeholders show it), each the centered 3:2 part of its picture
// (cropPNG) so that it fills all the cells. q=2 silences the terminal's replies, which
// inside tmux would arrive as keystrokes. Every write is one sequence of at most chunkSize base64
// bytes (plus its framing), on a client's tty ttyChunk: see there.
// The images are never deleted: they are small, stay in the terminal until it closes, and the next
// start overwrites them under the same IDs (a second board's exit must not blank the first's).
func Upload(w io.Writer, sets []*Set, via Via) (err error) {
	if via == ToTerminal { // kitty keeps images per screen; the board runs on the alternate one, so upload there (tmux's client is already on it)
		if _, err := io.WriteString(w, "\x1b[?1049h"); err != nil {
			return err
		}
		defer func() { // a failed upload must not leave the user on a blank alternate screen to read the error on
			if err != nil {
				io.WriteString(w, "\x1b[?1049l")
			}
		}()
	}
	size := chunkSize
	if via == ToTTY {
		size = ttyChunk
	}
	for _, s := range sets {
		for _, st := range States {
			for i, png := range s.sent[st] {
				b64 := base64.StdEncoding.EncodeToString(png)
				for off := 0; off < len(b64); off += size {
					end := min(off+size, len(b64))
					more := 1
					if end == len(b64) {
						more = 0
					}
					seq := fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", more, b64[off:end])
					if off == 0 {
						seq = fmt.Sprintf("\x1b_Ga=T,U=1,f=100,q=2,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", s.ids[st][i], Width, Height/2, more, b64[off:end])
					}
					if _, err = io.WriteString(w, passthrough(seq, via == ToPassthrough)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// passthrough wraps seq for tmux's DCS passthrough: every ESC inside is doubled.
func passthrough(seq string, tmux bool) string {
	if !tmux {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}
