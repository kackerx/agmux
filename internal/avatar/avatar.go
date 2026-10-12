// Package avatar draws 6×4 pixel-art agent heads with half-block characters
// (6 columns × 2 rows) and remembers which head a session wears.
package avatar

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Width × Height pixels; renders as Width columns × Height/2 rows.
const (
	Width  = 6
	Height = 4
)

// Sprite pixels: '.' transparent, 'B' body, 'E' eye, 'b' agentType band (the bottom row).
// Eyes need body on both sides so Scan can sweep them.
type Sprite struct {
	ID, Name string
	Body     lipgloss.Color
	Pixels   [Height]string
}

var Sprites = []Sprite{
	{"bot-01", "Clawd", "#d97757", [Height]string{
		".BBBB.",
		"BEBBEB",
		"BBBBBB",
		"bbbbbb"}},
	{"bot-02", "Boxy", "#8be9fd", [Height]string{
		"BBBBBB",
		"BEBBEB",
		"B.BB.B",
		"bbbbbb"}},
	{"bot-03", "Sprout", "#bd93f9", [Height]string{
		"..BB..",
		"BEBBEB",
		"BBBBBB",
		"bbbbbb"}},
	{"bot-04", "Boo", "#f8f8f2", [Height]string{
		".BBBB.",
		"BEBBEB",
		"BB..BB",
		"bbbbbb"}},
	{"bot-05", "Mochi", "#f1fa8c", [Height]string{
		"B....B",
		"BEBBEB",
		".BBBB.",
		"bbbbbb"}},
	{"bot-06", "Cyclops", "#ff79c6", [Height]string{
		".BBBB.",
		"BBEEBB",
		"BBBBBB",
		"bbbbbb"}},
}

// Mood is what the eyes say.
type Mood int

const (
	Normal Mood = iota
	Scan        // working: eyes sweep left/right
	Alert       // needs attention: eyes blink red
	Dim         // idle: whole head darkened
)

const eyeColor = lipgloss.Color("#221e1e") // popup background: eyes read as holes

// AlertColor is what the eyes blink in when a session needs attention (config colors.attention).
var AlertColor = lipgloss.Color("#ff6188")

// BandColor is the agentType color (ccmux's agent label colors).
func BandColor(agentType string) lipgloss.Color {
	switch agentType {
	case "claude":
		return "#ffb86c"
	case "codex":
		return "#50fa7b"
	}
	return "#6272a4"
}

// Render returns Height/2 lines of Width cells. frame drives the Scan / Alert animation.
func Render(s Sprite, agentType string, mood Mood, frame int) string {
	var px [Height][Width]lipgloss.Color
	shift := 0
	if mood == Scan {
		shift = []int{0, 1, 0, -1}[frame%4]
		for _, row := range s.Pixels { // only sweep if every eye lands on the head
			for x := range row {
				if row[x] == 'E' && (x+shift < 0 || x+shift >= Width || row[x+shift] == '.' || row[x+shift] == 'b') {
					shift = 0
				}
			}
		}
	}
	eye := eyeColor
	if mood == Alert && frame%2 == 0 {
		eye = AlertColor
	}
	for y, row := range s.Pixels {
		for x, c := range row {
			switch c {
			case 'B', 'E':
				px[y][x] = s.Body
			case 'b':
				px[y][x] = BandColor(agentType)
			}
		}
		for x, c := range row {
			if c == 'E' {
				px[y][x+shift] = eye
			}
		}
	}
	if mood == Dim {
		for y := range px {
			for x := range px[y] {
				if px[y][x] != "" && px[y][x] != eyeColor {
					px[y][x] = darken(px[y][x])
				}
			}
		}
	}
	lines := make([]string, Height/2)
	for r := range lines {
		var b strings.Builder
		for x := range Width {
			top, bot := px[2*r][x], px[2*r+1][x]
			st := lipgloss.NewStyle()
			ch := "▀"
			switch {
			case top == "" && bot == "":
				ch = " "
			case top == "":
				ch, st = "▄", st.Foreground(bot)
			case bot == "":
				st = st.Foreground(top)
			default:
				st = st.Foreground(top).Background(bot)
			}
			b.WriteString(st.Render(ch))
		}
		lines[r] = b.String()
	}
	return strings.Join(lines, "\n")
}

// darken halves the color's brightness toward black.
func darken(c lipgloss.Color) lipgloss.Color {
	var r, g, b int
	if _, err := fmt.Sscanf(string(c), "#%02x%02x%02x", &r, &g, &b); err != nil {
		return c
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r/2, g/2, b/2))
}

// Bindings maps a session's key (its name, or its ID when it has none) → avatar ID.
type Bindings interface {
	Get(key string) (id string, ok bool)
	Set(key, id string) error
	Reload() error // pick up the file if someone else changed it; the error if it is unreadable
}

// FileBindings stores bindings in avatars.json as {"<key>":"<avatarID>"}. Another board, or a
// hand edit, may change the file at any time: Set merges into what is on disk, Reload follows it.
type FileBindings struct {
	Path string
	mu   sync.Mutex // the board sets while the poller reads
	m    map[string]string
	mod  time.Time // the file's mtime when m was read
}

// LoadBindings reads path; a missing file is an empty set. On error the set is empty, still usable.
func LoadBindings(path string) (*FileBindings, error) {
	b := &FileBindings{Path: path, m: map[string]string{}}
	return b, b.Reload()
}

// read is what the file holds now (missing = empty) and its mtime.
func (b *FileBindings) read() (map[string]string, time.Time, error) {
	m := map[string]string{}
	fi, err := os.Stat(b.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(b.Path)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, time.Time{}, errors.New(b.Path + ": not valid JSON, fix or delete it to reset avatar bindings (" + err.Error() + ")")
	}
	if m == nil { // the file says null
		m = map[string]string{}
	}
	return m, fi.ModTime(), nil
}

// Reload rereads the file when its mtime moved (a file that can't be read is retried every time,
// keeping what was read before).
func (b *FileBindings) Reload() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if fi, err := os.Stat(b.Path); err == nil && fi.ModTime().Equal(b.mod) {
		return nil
	}
	m, mod, err := b.read()
	if err == nil {
		b.m, b.mod = m, mod
	}
	return err
}

func (b *FileBindings) Get(key string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id, ok := b.m[key]
	return id, ok
}

// Set adds one binding to what the file holds now, never to a stale copy, and writes it whole and
// atomically (temp file + rename). A file it can't read is not overwritten.
// ponytail: the lock is per process; two boards binding in the same instant can still lose one.
func (b *FileBindings) Set(key, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, _, err := b.read()
	if err != nil {
		return err
	}
	m[key] = id
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.Path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.Path), "avatars-*.json.tmp") // another board may be writing too
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o644) // CreateTemp makes it 0600
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), b.Path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	b.m = m
	if fi, err := os.Stat(b.Path); err == nil {
		b.mod = fi.ModTime()
	}
	return nil
}

// ByID finds a sprite by its ID.
func ByID(id string) (Sprite, bool) {
	for _, s := range Sprites {
		if s.ID == id {
			return s, true
		}
	}
	return Sprite{}, false
}

// For picks the sprite for a session key: the state file's binding (b, nil = none), then
// config.toml's (conf), then def — a sprite ID, or "hash" for a stable pick from the key.
// An ID naming no sprite falls through to the next.
func For(b Bindings, conf map[string]string, def, key string) Sprite {
	if b != nil {
		if id, ok := b.Get(key); ok {
			if s, ok := ByID(id); ok {
				return s
			}
		}
	}
	for _, id := range []string{conf[key], def} {
		if s, ok := ByID(id); ok {
			return s
		}
	}
	h := fnv.New32a()
	h.Write([]byte(key))
	return Sprites[h.Sum32()%uint32(len(Sprites))]
}
