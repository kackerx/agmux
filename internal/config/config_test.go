package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadDefaultsAndPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c, err := Load()
	if err != nil || !reflect.DeepEqual(c.Keys, Default().Keys) || !reflect.DeepEqual(c.Colors, Default().Colors) {
		t.Fatalf("no file = defaults: %v", err)
	}
	if p, _ := Path(); p != filepath.Join(dir, "agmux", "config.toml") || c.Path != p {
		t.Errorf("path %q", p)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	t.Setenv("HOME", "/h")
	if p, _ := Path(); p != "/h/.config/agmux/config.toml" {
		t.Errorf("relative XDG_CONFIG_HOME is ignored: %q", p)
	}
	os.MkdirAll(filepath.Join(dir, "agmux"), 0o755)
	os.WriteFile(filepath.Join(dir, "agmux", "config.toml"), []byte("[keys]\ndown = [\"n\"]\n"), 0o644)
	t.Setenv("XDG_CONFIG_HOME", dir)
	if c, err := Load(); err != nil || c.Keys["down"][0] != "n" {
		t.Errorf("file read: %v %v", c, err)
	}
}

func TestParse(t *testing.T) {
	c, err := Parse("cfg", []byte(`
[keys]
down = ["n", "down"]
focus_preview = ["ctrl+o"]
quit = ["Q"]

[colors]
working = "#123456"
name = "200"
`))
	if err != nil {
		t.Fatal(err)
	}
	m := c.KeyMap()
	if m["n"] != "down" || m["j"] != "" || m["ctrl+o"] != "focus_preview" || m["tab"] != "" || m["Q"] != "quit" || m["q"] != "" || m["k"] != "up" {
		t.Errorf("keymap %v", m)
	}
	if c.Colors["working"] != "#123456" || c.Colors["name"] != "200" || c.Colors["done"] != "#50fa7b" {
		t.Errorf("colors %v", c.Colors)
	}
	if c.First("down") != "n" {
		t.Error("First is the first configured key")
	}
	if c, _ := Parse("cfg", []byte("[keys]\nfold = [\"space\"]\n")); c.KeyMap()[" "] != "fold" {
		t.Error(`"space" is the space bar, which bubbletea spells " "`)
	}
}

func TestParseErrors(t *testing.T) {
	for _, c := range []struct{ toml, want string }{
		{"[keys\n", "cfg: toml:"},
		{"[keys]\ndown = \"j\"\n", "cfg: toml:"},
		{"[foo]\nx = 1\n", "cfg: foo: unknown setting"},
		{"theme = \"dark\"\n", "cfg: theme: unknown setting"},
		{"[keys]\njumpp = [\"x\"]\n", "cfg: keys.jumpp: unknown action"},
		{"[keys]\ndown = [\"ctrl+\"]\n", `cfg: keys.down: "ctrl+" is not a key name`},
		{"[keys]\ndown = [\"enterr\"]\n", `cfg: keys.down: "enterr" is not a key name`},
		{"[keys]\ndown = []\n", "cfg: keys.down: needs at least one key"},
		{"[keys]\nquit = [\"j\"]\n", `"j" is already bound to down`},
		{"[keys]\nfold = [\"x\"]\n", `"x" is already bound to`}, // x is kill's default
		{"[colors]\nworking = \"#12345\"\n", `cfg: colors.working: "#12345" is neither #rrggbb nor an ANSI color 0-255`},
		{"[colors]\nworking = \"256\"\n", `"256" is neither`},
		{"[colors]\nworking = \"red\"\n", `"red" is neither`},
		{"[colors]\npink = \"#ffffff\"\n", "cfg: colors.pink: unknown color"},
		{"[avatars]\ndefault = \"bot-99\"\n", `cfg: avatars.default: "bot-99" is neither hash nor a sprite id (bot-01, `},
		{"[avatars.bind]\n\"sql mux\" = \"Clawd\"\n", `cfg: avatars.bind."sql mux": "Clawd" is not a sprite id`},
		{"[avatars]\nbind = \"x\"\n", "cfg: avatars.bind: must be a table, [avatars.bind]"},
		{"keys = 3\n", "cfg: keys: must be a table, [keys]"},
		{"colors = \"x\"\n", "cfg: colors: must be a table"},
		{"[avatars]\nsize = 2\n", "cfg: avatars.size: unknown setting"},
	} {
		_, err := Parse("cfg", []byte(c.toml))
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%q: got %v, want one line containing %q", c.toml, err, c.want)
		}
	}
}

func TestValidKey(t *testing.T) {
	for k, want := range map[string]bool{
		"j": true, "H": true, "?": true, "enter": true, "esc": true, "tab": true, "shift+tab": true, "space": true,
		"ctrl+k": true, "ctrl+@": true, "alt+x": true, "alt+enter": true, "f5": true, "f20": true, "pgdown": true,
		"": false, " ": false, "ctrl+": false, "ctrl+1": false, "f21": false, "f0": false, "enterr": false, "jk": false,
	} {
		if validKey(k) != want {
			t.Errorf("validKey(%q) = %v", k, !want)
		}
	}
}

// What `agmux config` prints parses back to the same config.
func TestStringRoundTrip(t *testing.T) {
	c, _ := Parse("cfg", []byte("[keys]\ndown = [\"n\", \"down\"]\n[colors]\nfocus = \"#010203\"\n[avatars]\ndefault = \"bot-02\"\n[avatars.bind]\n\"a b\" = \"bot-03\"\nc = \"bot-04\"\n"))
	back, err := Parse("cfg", []byte(c.String()))
	if err != nil || !reflect.DeepEqual(back, c) {
		t.Errorf("round trip: %v\n%s", err, c.String())
	}
	if !strings.HasPrefix(Default().String(), "[keys]\nup = [\"k\", \"up\"]\ndown = [\"j\", \"down\"]\n") {
		t.Errorf("printed in action order:\n%s", Default().String())
	}
}

func TestAvatars(t *testing.T) {
	if d := Default(); d.AvatarDefault != "hash" || len(d.AvatarBind) != 0 {
		t.Errorf("default: %q %v", d.AvatarDefault, d.AvatarBind)
	}
	c, err := Parse("cfg", []byte("[avatars]\ndefault = \"hash\"\n[avatars.bind]\nsqlmux-orchestrator = \"bot-03\"\n"))
	if err != nil || c.AvatarDefault != "hash" || c.AvatarBind["sqlmux-orchestrator"] != "bot-03" {
		t.Errorf("%v %q %v", err, c.AvatarDefault, c.AvatarBind)
	}
	if c, err := Parse("cfg", []byte("[avatars]\ndefault = \"bot-06\"\nbind = { x = \"bot-01\" }\n")); err != nil || c.AvatarDefault != "bot-06" || c.AvatarBind["x"] != "bot-01" {
		t.Errorf("a sprite as default: %v", err)
	}
}
