package avatar

import (
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/kackerx/agmux/internal/xdg"
)

// Image sets are pictures instead of pixel sprites: a directory <id>/<state>/*.png, drawn through
// kitty's graphics protocol. The picture is uploaded once under an image ID; the card holds Unicode
// placeholder characters whose foreground color is that ID, so tmux treats the avatar as plain text.

//go:embed sets
var builtin embed.FS

// states maps what the eyes say (Mood) to the directory a set keeps its frames in.
var states = [...]struct {
	Mood Mood
	Dir  string
}{{Dim, "idle"}, {Scan, "working"}, {Alert, "attention"}, {Normal, "done"}}

// baseID is the image ID of the first frame loaded; the rest count up from it. Fixed, so a restart
// overwrites what the last run left in the terminal instead of piling up.
const baseID = 0x00c0de00

// Set is one image avatar. Its ID shares the sprite ID namespace.
type Set struct {
	ID     string
	Source string            // "builtin" or "user"
	Frames map[Mood][][]byte // PNG data per state, in file name order; Dim (idle) is always there
	ids    map[Mood][]uint32 // the image ID of each frame
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
	number(Sets)
}

// UseSets makes the sets under fsys the only ones; for tests. It returns what it skipped.
func UseSets(fsys fs.FS) []string {
	Sets, problems = loadSets(fsys, "user")
	number(Sets)
	return problems
}

// number gives every frame its image ID, in load order.
func number(sets []*Set) {
	next := uint32(baseID)
	for _, s := range sets {
		s.ids = map[Mood][]uint32{}
		for _, st := range states {
			for range s.Frames[st.Mood] {
				s.ids[st.Mood] = append(s.ids[st.Mood], next)
				next++
			}
		}
	}
}

// loadSets reads every <id>/<state>/*.png under fsys. Only .png files count; a missing root is no sets.
func loadSets(fsys fs.FS, source string) (sets []*Set, problems []string) {
	dirs, _ := fs.ReadDir(fsys, ".") // ReadDir sorts by name
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		s := &Set{ID: d.Name(), Source: source, Frames: map[Mood][][]byte{}}
		for _, st := range states {
			files, _ := fs.ReadDir(fsys, path.Join(d.Name(), st.Dir))
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".png") {
					continue
				}
				if data, err := fs.ReadFile(fsys, path.Join(d.Name(), st.Dir, f.Name())); err == nil {
					s.Frames[st.Mood] = append(s.Frames[st.Mood], data)
				}
			}
		}
		if len(s.Frames[Dim]) == 0 {
			problems = append(problems, fmt.Sprintf("avatar set %q has no idle/*.png: skipped", d.Name()))
			continue
		}
		sets = append(sets, s)
	}
	return
}

// FrameID is the image ID to show for mood at frame; a state with no frames shows idle.
func (s *Set) FrameID(m Mood, frame int) uint32 {
	ids := s.ids[m]
	if len(ids) == 0 {
		ids = s.ids[Dim]
	}
	return ids[frame%len(ids)]
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

// Upload sends every frame of sets to the terminal as a virtual placement of Width × Height/2 cells
// (U=1: nothing is drawn until placeholders show it). q=2 silences the terminal's replies, which
// inside tmux would arrive as keystrokes. In tmux each sequence goes through its DCS passthrough.
func Upload(w io.Writer, sets []*Set, tmux bool) error {
	for _, s := range sets {
		for _, st := range states {
			for i, png := range s.Frames[st.Mood] {
				b64 := base64.StdEncoding.EncodeToString(png)
				for off := 0; off < len(b64); off += chunkSize {
					end := min(off+chunkSize, len(b64))
					more := 1
					if end == len(b64) {
						more = 0
					}
					seq := fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", more, b64[off:end])
					if off == 0 {
						seq = fmt.Sprintf("\x1b_Ga=T,U=1,f=100,q=2,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", s.ids[st.Mood][i], Width, Height/2, more, b64[off:end])
					}
					if _, err := io.WriteString(w, passthrough(seq, tmux)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// Delete frees every uploaded image (and its placements) in the terminal.
func Delete(w io.Writer, sets []*Set, tmux bool) {
	for _, s := range sets {
		for _, st := range states {
			for _, id := range s.ids[st.Mood] {
				io.WriteString(w, passthrough(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id), tmux))
			}
		}
	}
}

// passthrough wraps seq for tmux's DCS passthrough: every ESC inside is doubled.
func passthrough(seq string, tmux bool) string {
	if !tmux {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}
