// Package config reads ~/.config/agmux/config.toml: key bindings, colors and avatars on top of defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/xdg"
)

// Actions, in the order `agmux config` prints them.
var Actions = []string{
	"up", "down", "left", "right", "jump", "fold", "preview", "focus_preview", "leave_preview",
	"reply", "handoff", "search", "flash", "restart", "kill", "review", "avatar", "quit", "confirm",
}

var defaultKeys = map[string][]string{
	"up": {"k", "up"}, "down": {"j", "down"}, "left": {"h", "left"}, "right": {"l", "right"},
	"jump": {"enter"}, "fold": {"f"}, "preview": {"p"}, "focus_preview": {"tab"}, "leave_preview": {"ctrl+]"},
	// search / flash are T19 / T20
	"reply": {"i"}, "handoff": {"H"}, "search": {"/"}, "flash": {"s"},
	"restart": {"r"}, "kill": {"x"}, "review": {"d"}, "avatar": {"a"},
	"quit": {"q", "esc"}, "confirm": {"y"},
}

// Colors, in print order.
var Colors = []string{
	"working", "idle", "done", "attention", "focus", "name", "branch", "model", "effort", "dir",
	"bar_low", "bar_mid", "bar_high", "bar_empty", "muted", "disabled",
}

var defaultColors = map[string]string{
	"working": "#ffb86c", "idle": "#585b70", "done": "#50fa7b", "attention": "#ff6188", "focus": "#ff8ebe",
	"name": "5", "branch": "12", "model": "14", "effort": "13", "dir": "11",
	"bar_low": "#a6e3a1", "bar_mid": "#f9e2af", "bar_high": "#ff6188", "bar_empty": "#585b70",
	"muted": "#6272a4", "disabled": "#45475a",
}

type Config struct {
	Path   string
	Keys   map[string][]string // action → key names (bubbletea's, "space" for the space bar)
	Colors map[string]string   // name → "#rrggbb" or an ANSI index "0".."255"

	AvatarDefault string            // [avatars] default: "hash" or a sprite ID
	AvatarBind    map[string]string // [avatars.bind]: session name → sprite ID
}

// Default is the configuration with no file.
func Default() *Config {
	c := &Config{Keys: map[string][]string{}, Colors: map[string]string{}, AvatarDefault: "hash", AvatarBind: map[string]string{}}
	for k, v := range defaultKeys {
		c.Keys[k] = v
	}
	for k, v := range defaultColors {
		c.Colors[k] = v
	}
	return c
}

// Path is $XDG_CONFIG_HOME/agmux/config.toml, defaulting to ~/.config/agmux/config.toml.
func Path() (string, error) {
	dir, err := xdg.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads the config file; a missing file is all defaults.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		c := Default()
		c.Path = path
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Parse merges a config file over the defaults (an action set in the file replaces its default
// keys) and checks it. Every error is one line naming the file, the setting and the reason.
func Parse(path string, data []byte) (*Config, error) {
	var raw struct {
		Keys    map[string][]string `toml:"keys"`
		Colors  map[string]string   `toml:"colors"`
		Avatars struct {
			Default string            `toml:"default"`
			Bind    map[string]string `toml:"bind"`
		} `toml:"avatars"`
	}
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		msg, _, _ := strings.Cut(err.Error(), "\n")
		return nil, fmt.Errorf("%s: %s", path, msg)
	}
	// toml decodes a non-table into a map as nothing at all, which would silently drop the setting
	for _, sec := range [][]string{{"keys"}, {"colors"}, {"avatars", "bind"}} {
		if md.IsDefined(sec...) && md.Type(sec...) != "Hash" {
			return nil, fmt.Errorf("%s: %s: must be a table, [%[2]s]", path, strings.Join(sec, "."))
		}
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("%s: %s: unknown setting (sections are [keys], [colors], [avatars] and [avatars.bind])", path, u[0])
	}
	c := Default()
	c.Path = path
	for action, keys := range raw.Keys {
		if _, ok := defaultKeys[action]; !ok {
			return nil, fmt.Errorf("%s: keys.%s: unknown action (one of %s)", path, action, strings.Join(Actions, ", "))
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("%s: keys.%s: needs at least one key", path, action)
		}
		for _, k := range keys {
			if !validKey(k) {
				return nil, fmt.Errorf("%s: keys.%s: %q is not a key name (examples: j, ctrl+k, enter, tab, shift+tab, esc, space, f5)", path, action, k)
			}
		}
		c.Keys[action] = keys
	}
	for name, v := range raw.Colors {
		if _, ok := defaultColors[name]; !ok {
			return nil, fmt.Errorf("%s: colors.%s: unknown color (one of %s)", path, name, strings.Join(Colors, ", "))
		}
		if n, err := strconv.Atoi(v); !colorRe.MatchString(v) && (err != nil || n < 0 || n > 255) {
			return nil, fmt.Errorf("%s: colors.%s: %q is neither #rrggbb nor an ANSI color 0-255", path, name, v)
		}
		c.Colors[name] = v
	}
	if d := raw.Avatars.Default; d != "" {
		if d != "hash" && !isSprite(d) {
			return nil, fmt.Errorf("%s: avatars.default: %q is neither hash nor a sprite id (%s; see agmux avatars)", path, d, spriteIDs())
		}
		c.AvatarDefault = d
	}
	for name, id := range raw.Avatars.Bind {
		if !isSprite(id) {
			return nil, fmt.Errorf("%s: avatars.bind.%q: %q is not a sprite id (%s; see agmux avatars)", path, name, id, spriteIDs())
		}
		c.AvatarBind[name] = id
	}
	seen := map[string]string{}
	for _, action := range Actions {
		for _, k := range c.Keys[action] {
			if other, ok := seen[norm(k)]; ok && other != action {
				return nil, fmt.Errorf("%s: keys.%s: %q is already bound to %s", path, action, k, other)
			}
			seen[norm(k)] = action
		}
	}
	return c, nil
}

// KeyMap maps a key, as bubbletea's KeyMsg.String() spells it, to its action.
func (c *Config) KeyMap() map[string]string {
	m := map[string]string{}
	for action, keys := range c.Keys {
		for _, k := range keys {
			m[norm(k)] = action
		}
	}
	return m
}

// First is the key shown for action in the footer and prompts.
func (c *Config) First(action string) string { return c.Keys[action][0] }

// String is the effective config as TOML, ready to copy into config.toml.
func (c *Config) String() string {
	var b strings.Builder
	b.WriteString("[keys]\n")
	for _, a := range Actions {
		q := make([]string, len(c.Keys[a]))
		for i, k := range c.Keys[a] {
			q[i] = strconv.Quote(k)
		}
		fmt.Fprintf(&b, "%s = [%s]\n", a, strings.Join(q, ", "))
	}
	b.WriteString("\n[colors]\n")
	for _, n := range Colors {
		fmt.Fprintf(&b, "%s = %q\n", n, c.Colors[n])
	}
	fmt.Fprintf(&b, "\n[avatars]\ndefault = %q\n\n[avatars.bind]\n", c.AvatarDefault)
	names := slices.Sorted(maps.Keys(c.AvatarBind))
	for _, n := range names {
		fmt.Fprintf(&b, "%q = %q\n", n, c.AvatarBind[n])
	}
	return b.String()
}

func isSprite(id string) bool { _, ok := avatar.ByID(id); return ok }

func spriteIDs() string {
	ids := make([]string, len(avatar.Sprites))
	for i, s := range avatar.Sprites {
		ids[i] = s.ID
	}
	return strings.Join(ids, ", ")
}

// norm spells a configured key the way bubbletea's KeyMsg.String() does.
func norm(k string) string {
	if k == "space" {
		return " "
	}
	return k
}

var keyNames = map[string]bool{
	"up": true, "down": true, "left": true, "right": true, "enter": true, "esc": true, "tab": true,
	"shift+tab": true, "backspace": true, "delete": true, "insert": true, "home": true, "end": true,
	"pgup": true, "pgdown": true, "space": true,
	"ctrl+up": true, "ctrl+down": true, "ctrl+left": true, "ctrl+right": true,
	"shift+up": true, "shift+down": true, "shift+left": true, "shift+right": true,
	"ctrl+home": true, "ctrl+end": true, "shift+home": true, "shift+end": true, "ctrl+pgup": true, "ctrl+pgdown": true,
}

// validKey accepts the names bubbletea gives keys: one printable character, a named key, ctrl+<letter>,
// f1..f20, each optionally with alt+.
func validKey(k string) bool {
	k = strings.TrimPrefix(k, "alt+")
	switch {
	case keyNames[k]:
		return true
	case utf8.RuneCountInString(k) == 1:
		r, _ := utf8.DecodeRuneInString(k)
		return r > ' ' && r != 0x7f
	case strings.HasPrefix(k, "ctrl+") && len(k) == 6 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyz@\\]^_", rune(k[5])):
		return true
	case strings.HasPrefix(k, "f"):
		n, err := strconv.Atoi(k[1:])
		return err == nil && n >= 1 && n <= 20
	}
	return false
}
