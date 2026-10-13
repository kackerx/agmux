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
	"reply", "handoff", "search", "next_match", "prev_match", "flash", "restart", "kill", "review", "avatar", "quit", "confirm",
}

var defaultKeys = map[string][]string{
	"up": {"k", "up"}, "down": {"j", "down"}, "left": {"h", "left"}, "right": {"l", "right"},
	"jump": {"enter"}, "fold": {"f"}, "preview": {"p"}, "focus_preview": {"tab"}, "leave_preview": {"tab"},
	// search / flash are T19 / T20
	"reply": {"i"}, "handoff": {"H"}, "search": {"/"}, "next_match": {"n"}, "prev_match": {"N"}, "flash": {"s"},
	"restart": {"r"}, "kill": {"x"}, "review": {"d"}, "avatar": {"a"},
	"quit": {"q", "esc"}, "confirm": {"y"},
}

// Colors, in print order.
var Colors = []string{
	"working", "idle", "done", "attention", "focus", "match", "border", "name", "branch", "model", "effort", "dir",
	"bar_low", "bar_mid", "bar_high", "bar_empty", "muted", "disabled",
}

var defaultColors = map[string]string{
	"working": "#ffb86c", "idle": "#585b70", "done": "#50fa7b", "attention": "#ff6188", "focus": "#ff8ebe", "match": "#f2d06b", "border": "",
	"name": "5", "branch": "12", "model": "14", "effort": "13", "dir": "11",
	"bar_low": "#a6e3a1", "bar_mid": "#f9e2af", "bar_high": "#ff6188", "bar_empty": "#585b70",
	"muted": "#6272a4", "disabled": "#45475a",
}

type Config struct {
	Path   string
	Keys   map[string][]string // action → key names (bubbletea's, "space" for the space bar)
	Colors map[string]string   // name → "#rrggbb" or an ANSI index "0".."255"

	MaxColumns int // [layout] max_columns: the most columns of cards, if the terminal is wide enough

	AvatarRenderer string            // [avatars] renderer: "auto", "image" or "pixel"
	AvatarDefault  string            // [avatars] default: "hash" or an avatar ID (sprite or image set)
	AvatarBind     map[string]string // [avatars.bind]: session name → avatar ID
}

// Default is the configuration with no file.
func Default() *Config {
	c := &Config{Keys: map[string][]string{}, Colors: map[string]string{}, MaxColumns: 4, AvatarRenderer: "auto", AvatarDefault: "hash", AvatarBind: map[string]string{}}
	for k, v := range defaultKeys {
		c.Keys[k] = slices.Clone(v) // callers may change c; the defaults stay
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

// The range of [layout] max_columns.
const minMaxColumns, maxMaxColumns = 1, 8

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Parse merges a config file over the defaults (an action set in the file replaces its default
// keys) and checks it. Every error is one line naming the file, the setting and the reason.
func Parse(path string, data []byte) (*Config, error) {
	var raw struct {
		Keys   map[string][]string `toml:"keys"`
		Colors map[string]string   `toml:"colors"`
		Layout struct {
			MaxColumns int `toml:"max_columns"`
		} `toml:"layout"`
		Avatars struct {
			Renderer string            `toml:"renderer"`
			Default  string            `toml:"default"`
			Bind     map[string]string `toml:"bind"`
		} `toml:"avatars"`
	}
	oneLine := func(err error) error {
		msg, _, _ := strings.Cut(err.Error(), "\n")
		return fmt.Errorf("%s: %s", path, msg)
	}
	// a section that isn't a table (layout = 3, [[layout]]) would be a Go type error, or for a map
	// silently nothing: say it the same way for every section before decoding into the types
	var generic map[string]any
	if _, err := toml.Decode(string(data), &generic); err != nil {
		return nil, oneLine(err)
	}
	for _, sec := range []string{"keys", "colors", "layout", "avatars"} {
		if v, ok := generic[sec]; ok {
			if _, isTable := v.(map[string]any); !isTable {
				return nil, fmt.Errorf("%s: %s: must be a table, [%[2]s]", path, sec)
			}
		}
	}
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, oneLine(err)
	}
	if md.IsDefined("avatars", "bind") && md.Type("avatars", "bind") != "Hash" {
		return nil, fmt.Errorf("%s: avatars.bind: must be a table, [avatars.bind]", path)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("%s: %s: unknown setting (sections are [keys], [colors], [layout], [avatars] and [avatars.bind])", path, u[0])
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
			base, alt := strings.CutPrefix(k, "alt+")
			if as, ok := sentAs[base]; ok {
				if alt {
					as = "alt+" + as
				}
				return nil, fmt.Errorf("%s: keys.%s: %q arrives as %s (the terminal sends the same byte); write %q", path, action, k, as, as)
			}
			if !validKey(k) {
				return nil, fmt.Errorf("%s: keys.%s: %q is not a key name (examples: j, ctrl+k, enter, tab, shift+tab, esc, space, f5)", path, action, k)
			}
			if base == "ctrl+c" {
				return nil, fmt.Errorf("%s: keys.%s: ctrl+c can't be bound (it always quits)", path, action)
			}
		}
		c.Keys[action] = keys
	}
	for name, v := range raw.Colors {
		if _, ok := defaultColors[name]; !ok {
			return nil, fmt.Errorf("%s: colors.%s: unknown color (one of %s)", path, name, strings.Join(Colors, ", "))
		}
		if name == "border" && v == "" { // empty: borders follow the status, as before
			c.Colors[name] = v
			continue
		}
		if n, err := strconv.Atoi(v); !colorRe.MatchString(v) && (err != nil || n < 0 || n > 255) {
			hint := ""
			if name == "border" {
				hint = ` or "" to follow the status`
			}
			return nil, fmt.Errorf("%s: colors.%s: %q is neither #rrggbb nor an ANSI color 0-255%s", path, name, v, hint)
		}
		c.Colors[name] = v
	}
	if md.IsDefined("layout", "max_columns") {
		if n := raw.Layout.MaxColumns; n < minMaxColumns || n > maxMaxColumns {
			return nil, fmt.Errorf("%s: layout.max_columns: %d is out of range (%d to %d columns)", path, n, minMaxColumns, maxMaxColumns)
		}
		c.MaxColumns = raw.Layout.MaxColumns
	}
	if r := raw.Avatars.Renderer; r != "" {
		if r != "auto" && r != "image" && r != "pixel" {
			return nil, fmt.Errorf("%s: avatars.renderer: %q is not auto, image or pixel", path, r)
		}
		c.AvatarRenderer = r
	}
	if d := raw.Avatars.Default; d != "" {
		if d != "hash" && !avatar.Known(d) {
			return nil, fmt.Errorf("%s: avatars.default: %q is neither hash nor an avatar id (%s; see agmux avatars)%s", path, d, avatarIDs(), removed(d))
		}
		c.AvatarDefault = d
	}
	for name, id := range raw.Avatars.Bind {
		if !avatar.Known(id) {
			return nil, fmt.Errorf("%s: avatars.bind.%q: %q is not an avatar id (%s; see agmux avatars)%s", path, name, id, avatarIDs(), removed(id))
		}
		c.AvatarBind[name] = id
	}
	// what the file binds wins: two actions it binds can't share a key, but a key it binds is
	// taken from the action that had it by default (so new default keys never break a config)
	owner := map[string]string{}
	for _, a := range Actions {
		if modal[a] {
			continue
		}
		for _, k := range raw.Keys[a] {
			if o, ok := owner[norm(k)]; ok && o != a {
				return nil, fmt.Errorf("%s: keys.%s: %q is already bound to %s", path, a, k, o)
			}
			owner[norm(k)] = a
		}
	}
	for _, a := range Actions {
		if _, set := raw.Keys[a]; set || modal[a] {
			continue
		}
		// left with none, the action has no key (and no footer entry): a new default action never
		// stops an old config from loading
		c.Keys[a] = slices.DeleteFunc(slices.Clone(c.Keys[a]), func(k string) bool { return owner[norm(k)] != "" })
	}
	return c, nil
}

// modal actions only act in their own mode (the kill prompt, pass-through), so they may share
// keys with the board's; KeyMap leaves them out, Is asks about them.
var modal = map[string]bool{"confirm": true, "leave_preview": true}

// sentAs: key names bubbletea never reports, and what it reports instead.
var sentAs = map[string]string{"ctrl+i": "tab", "ctrl+m": "enter"}

// KeyMap maps a key, as bubbletea's KeyMsg.String() spells it, to its board action.
func (c *Config) KeyMap() map[string]string {
	m := map[string]string{}
	for action, keys := range c.Keys {
		for _, k := range keys {
			if !modal[action] {
				m[norm(k)] = action
			}
		}
	}
	return m
}

// Is reports whether key (as KeyMsg.String() spells it) is bound to action.
func (c *Config) Is(action, key string) bool {
	return slices.ContainsFunc(c.Keys[action], func(k string) bool { return norm(k) == key })
}

// First is the key shown for action in the footer and prompts; "" when it has none.
func (c *Config) First(action string) string {
	if len(c.Keys[action]) == 0 {
		return ""
	}
	return c.Keys[action][0]
}

// String is the effective config as TOML, ready to copy into config.toml.
func (c *Config) String() string {
	var b strings.Builder
	b.WriteString("[keys]\n")
	for _, a := range Actions {
		if len(c.Keys[a]) == 0 { // "a = []" would not load back
			fmt.Fprintf(&b, "# %s: no key (another action took its default)\n", a)
			continue
		}
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
	fmt.Fprintf(&b, "\n[layout]\nmax_columns = %d\n", c.MaxColumns)
	fmt.Fprintf(&b, "\n[avatars]\nrenderer = %q\ndefault = %q\n\n[avatars.bind]\n", c.AvatarRenderer, c.AvatarDefault)
	names := slices.Sorted(maps.Keys(c.AvatarBind))
	for _, n := range names {
		fmt.Fprintf(&b, "%q = %q\n", n, c.AvatarBind[n])
	}
	return b.String()
}

// removed is the sentence that goes after the error for an avatar that used to exist: a config that
// names it stops the board, so it should say why.
func removed(id string) string {
	if id == "bot-06" {
		return "; bot-06 was removed; pick another"
	}
	return ""
}

func avatarIDs() string { return strings.Join(avatar.Listing(), ", ") }

// norm spells a configured key the way bubbletea's KeyMsg.String() does: "space" is " ",
// with alt+ too.
func norm(k string) string {
	rest, alt := strings.CutPrefix(k, "alt+")
	if rest == "space" {
		rest = " "
	}
	if alt {
		return "alt+" + rest
	}
	return rest
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
