// Package avatar draws 6×6 pixel-art agent heads with half-block characters
// (6 columns × 3 rows) and remembers which head a session wears.
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

	"github.com/charmbracelet/lipgloss"
)

// Sprite pixels: '.' transparent, 'B' body, 'E' eye, 'b' agentType band.
type Sprite struct {
	ID, Name string
	Body     lipgloss.Color
	Pixels   [6]string
}

var Sprites = []Sprite{
	{"bot-01", "Clawd", "#d97757", [6]string{
		".BBBB.",
		"BEBBEB",
		"BBBBBB",
		".BBBB.",
		".B..B.",
		"bbbbbb"}},
	{"bot-02", "Boxy", "#8be9fd", [6]string{
		"BBBBBB",
		"BEBBEB",
		"BBBBBB",
		"B.BB.B",
		"BBBBBB",
		"bbbbbb"}},
	{"bot-03", "Sprout", "#bd93f9", [6]string{
		"..BB..",
		".BBBB.",
		"BEBBEB",
		"BBBBBB",
		"B.BB.B",
		"bbbbbb"}},
	{"bot-04", "Boo", "#f8f8f2", [6]string{
		".BBBB.",
		"BBBBBB",
		"BEBBEB",
		"BBBBBB",
		"BB..BB",
		"bbbbbb"}},
	{"bot-05", "Mochi", "#f1fa8c", [6]string{
		"B....B",
		"BBBBBB",
		"BEBBEB",
		"BBBBBB",
		".BBBB.",
		"bbbbbb"}},
	{"bot-06", "Cyclops", "#ff79c6", [6]string{
		".BBBB.",
		"BBBBBB",
		"BBEEBB",
		"BBBBBB",
		".B..B.",
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

const (
	eyeColor   = lipgloss.Color("#221e1e") // popup background: eyes read as holes
	alertColor = lipgloss.Color("#ff6188")
)

// Band colors follow ccmux's agent label colors.
func bandColor(agentType string) lipgloss.Color {
	switch agentType {
	case "claude":
		return "#ffb86c"
	case "codex":
		return "#50fa7b"
	}
	return "#6272a4"
}

// Render returns 3 lines of 6 cells. frame drives the Scan / Alert animation.
func Render(s Sprite, agentType string, mood Mood, frame int) string {
	var px [6][6]lipgloss.Color
	shift := 0
	if mood == Scan {
		shift = []int{0, 1, 0, -1}[frame%4]
		for _, row := range s.Pixels { // only sweep if every eye lands on the head
			for x := range row {
				if row[x] == 'E' && (x+shift < 0 || x+shift > 5 || row[x+shift] == '.' || row[x+shift] == 'b') {
					shift = 0
				}
			}
		}
	}
	eye := eyeColor
	if mood == Alert && frame%2 == 0 {
		eye = alertColor
	}
	for y, row := range s.Pixels {
		for x, c := range row {
			switch c {
			case 'B', 'E':
				px[y][x] = s.Body
			case 'b':
				px[y][x] = bandColor(agentType)
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
	lines := make([]string, 3)
	for r := range lines {
		var b strings.Builder
		for x := range 6 {
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

// Bindings maps session ID → avatar ID.
type Bindings interface {
	Get(sessionID string) (id string, ok bool)
	Set(sessionID, id string) error
}

// FileBindings stores bindings in avatars.json as {"<sessionID>":"<avatarID>"}.
type FileBindings struct {
	Path string
	m    map[string]string
}

// LoadBindings reads path; a missing file is an empty set.
func LoadBindings(path string) (*FileBindings, error) {
	b := &FileBindings{Path: path, m: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b.m); err != nil {
		return b, errors.New(path + ": not valid JSON, fix or delete it to reset avatar bindings (" + err.Error() + ")")
	}
	return b, nil
}

func (b *FileBindings) Get(sessionID string) (string, bool) {
	id, ok := b.m[sessionID]
	return id, ok
}

// Set writes the whole file atomically (tmp + rename) so a crash never leaves it half-written.
func (b *FileBindings) Set(sessionID, id string) error {
	b.m[sessionID] = id
	data, err := json.MarshalIndent(b.m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.Path), 0o755); err != nil {
		return err
	}
	tmp := b.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.Path)
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

// For returns the session's bound sprite, or a stable hash-picked default.
// A nil b means no bindings.
func For(b Bindings, sessionID string) Sprite {
	if b != nil {
		if id, ok := b.Get(sessionID); ok {
			if s, ok := ByID(id); ok {
				return s
			}
		}
	}
	h := fnv.New32a()
	h.Write([]byte(sessionID))
	return Sprites[h.Sum32()%uint32(len(Sprites))]
}
