package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/agmux/internal/avatar"
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
		{"[keys]\nquit = [\"j\"]\ndown = [\"j\"]\n", `cfg: keys.quit: "j" is already bound to down`}, // both written: error
		{"[keys]\njump = [\"alt+ctrl+m\"]\n", `"alt+ctrl+m" arrives as alt+enter (the terminal sends the same byte); write "alt+enter"`},
		{"[keys]\nkill = [\"alt+ctrl+c\"]\n", "cfg: keys.kill: ctrl+c can't be bound"},
		{"[keys]\njump = [\"ctrl+m\"]\n", `cfg: keys.jump: "ctrl+m" arrives as enter (the terminal sends the same byte); write "enter"`},
		{"[keys]\nfocus_preview = [\"ctrl+i\"]\n", `"ctrl+i" arrives as tab`},
		{"[keys]\nkill = [\"ctrl+c\"]\n", "cfg: keys.kill: ctrl+c can't be bound (it always quits)"},
		{"[keys]\nleave_preview = [\"ctrl+c\"]\n", "cfg: keys.leave_preview: ctrl+c can't be bound"},
		{"colors = \"dark\"\n", "cfg: colors: must be a table, [colors]"},
		{"avatars = 3\n", "cfg: avatars: must be a table, [avatars]"},
		{"[[avatars]]\ndefault = \"hash\"\n", "cfg: avatars: must be a table, [avatars]"},
		{"[[keys]]\nup = [\"k\"]\n", "cfg: keys: must be a table, [keys]"},
		{"[colors]\nworking = \"-1\"\n", `"-1" is neither`},
		{"[colors]\nworking = \"#12345\"\n", `cfg: colors.working: "#12345" is neither #rrggbb nor an ANSI color 0-255`},
		{"[colors]\nworking = \"256\"\n", `"256" is neither`},
		{"[colors]\nworking = \"red\"\n", `"red" is neither`},
		{"[colors]\npink = \"#ffffff\"\n", "cfg: colors.pink: unknown color"},
		{"[avatars]\ndefault = \"bot-99\"\n", `cfg: avatars.default: "bot-99" is neither hash nor an avatar id (bot-01, `},
		{"[avatars.bind]\n\"sql mux\" = \"Clawd\"\n", `cfg: avatars.bind."sql mux": "Clawd" is not an avatar id`},
		{"[avatars]\nbind = \"x\"\n", "cfg: avatars.bind: must be a table, [avatars.bind]"},
		{"keys = 3\n", "cfg: keys: must be a table, [keys]"},
		{"colors = \"x\"\n", "cfg: colors: must be a table"},
		{"[avatars]\nsize = 2\n", "cfg: avatars.size: unknown setting"},
		{"[avatars]\ndefault = \"bot-06\"\n", `bot-05; see agmux avatars); bot-06 was removed; pick another`},
		{"[avatars.bind]\nx = \"bot-06\"\n", `cfg: avatars.bind."x": "bot-06" is not an avatar id (`},
		{"[avatars.bind]\nx = \"bot-06\"\n", `; bot-06 was removed; pick another`},
		{"[avatars]\nrenderer = \"sixel\"\n", `cfg: avatars.renderer: "sixel" is not auto, image or pixel`},
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
	d, err := Parse("cfg", []byte(Default().String())) // and the defaults print as a file that changes nothing
	if err != nil || !reflect.DeepEqual(d.Keys, Default().Keys) || !reflect.DeepEqual(d.Colors, Default().Colors) || d.MaxColumns != 4 || d.Flow != "bands" {
		t.Errorf("defaults: %v\n%s", err, Default().String())
	}
}

// The printed config is the commented template: one line per action and per color, each with its
// default in the comment (the default, not the current value), the layout and avatar options spelled
// out, and everything in the order the code lists it.
func TestStringIsTheCommentedTemplate(t *testing.T) {
	c, err := Parse("cfg", []byte("[keys]\nup = [\"w\"]\n[colors]\nfocus = \"#010203\"\nborder = \"7\"\n[layout]\nmax_columns = 6\nflow = \"stack\"\ngroup_by = \"state\"\n[avatars]\nrenderer = \"pixel\"\ndefault = \"bot-02\"\n[avatars.bind]\nz = \"bot-03\"\na = \"bot-04\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	out := c.String()
	lines := strings.Split(out, "\n")
	find := func(prefix string) string {
		for _, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		t.Errorf("no line starting %q", prefix)
		return ""
	}
	for _, a := range Actions {
		if l := find(a + " = "); !strings.Contains(l, "# default: "+strings.Join(defaultKeys[a], ", ")) {
			t.Errorf("%s: %q", a, l)
		}
	}
	for _, n := range Colors {
		if l := find(n + " = "); !strings.Contains(l, "# default: ") {
			t.Errorf("%s: %q", n, l)
		}
	}
	for prefix, want := range map[string]string{
		`up = ["w"]`:         "# default: k, up",
		`focus = "#010203"`:  "# default: #ff8ebe",
		`border = "7"`:       `# default: "" (follow the status)`,
		"max_columns = 6":    "# default: 4",
		`flow = "stack"`:     `# default: "bands"`,
		`group_by = "state"`: `# default: "cwd"`,
		`renderer = "pixel"`: `# default: "auto"`,
		`default = "bot-02"`: `# default: "hash"`,
	} {
		if l := find(prefix); !strings.Contains(l, want) {
			t.Errorf("%q: %q, want %q", prefix, l, want)
		}
	}
	// the sections, in order, and the bindings after the comment that explains them
	var at []int
	for _, h := range []string{"[keys]", "[colors]", "[layout]", "[avatars]", "[avatars.bind]"} {
		at = append(at, strings.Index(out, "\n"+h+"\n"))
	}
	if !slices.IsSorted(at) || at[0] < 0 || strings.Index(out, `"a" = "bot-04"`) > strings.Index(out, `"z" = "bot-03"`) || strings.Index(out, `"a" = "bot-04"`) < at[4] {
		t.Errorf("sections / bindings out of order:\n%s", out)
	}
	// the printed file is the whole config
	if back, err := Parse("cfg", []byte(out)); err != nil || !reflect.DeepEqual(back, c) {
		t.Errorf("round trip: %v\n%s", err, out)
	}
	for _, l := range lines { // a comment is a comment: nothing after # on a setting line changes what it says
		if i := strings.Index(l, " # "); i > 0 && !strings.Contains(l[:i], "=") && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "#") {
			t.Errorf("text before a comment that is not a setting: %q", l)
		}
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
	if c, err := Parse("cfg", []byte("[avatars]\ndefault = \"bot-05\"\nbind = { x = \"bot-01\" }\n")); err != nil || c.AvatarDefault != "bot-05" || c.AvatarBind["x"] != "bot-01" {
		t.Errorf("a sprite as default: %v", err)
	}
}

// Only an avatar that used to exist gets the "was removed" sentence.
func TestRemovedAvatarMessage(t *testing.T) {
	_, err := Parse("cfg", []byte("[avatars]\ndefault = \"bot-99\"\n"))
	if err == nil || strings.Contains(err.Error(), "removed") {
		t.Errorf("an unknown id: %v", err)
	}
	if _, err := Parse("cfg", []byte("[avatars.bind]\nx = \"bot-06\"\n")); err == nil || !strings.HasSuffix(err.Error(), "; bot-06 was removed; pick another") {
		t.Errorf("bind: %v", err)
	}
}

// renderer is auto, image or pixel; default and bind may name an image set as well as a sprite.
func TestAvatarRendererAndSets(t *testing.T) {
	t.Cleanup(func() { avatar.Sets = nil })
	avatar.Sets = []*avatar.Set{{ID: "aza_choi"}}
	if got := Default().AvatarRenderer; got != "auto" || !strings.Contains(Default().String(), "\nrenderer = \"auto\"  ") || !strings.Contains(Default().String(), "\ndefault = \"hash\"  ") {
		t.Errorf("default renderer %q:\n%s", got, Default())
	}
	for _, r := range []string{"auto", "image", "pixel"} {
		if c, err := Parse("cfg", []byte("[avatars]\nrenderer = \""+r+"\"\n")); err != nil || c.AvatarRenderer != r {
			t.Errorf("renderer %s: %v", r, err)
		}
	}
	c, err := Parse("cfg", []byte("[avatars]\ndefault = \"aza_choi\"\n[avatars.bind]\nx = \"aza_choi\"\ny = \"bot-02\"\n"))
	if err != nil || c.AvatarDefault != "aza_choi" || c.AvatarBind["x"] != "aza_choi" || c.AvatarBind["y"] != "bot-02" {
		t.Fatalf("set ids: %v", err)
	}
	if back, err := Parse("cfg", []byte(c.String())); err != nil || !reflect.DeepEqual(back, c) {
		t.Errorf("round trip: %v", err)
	}
	avatar.Sets = nil // the set is gone: its name is an error again, and the message lists what is there
	if _, err := Parse("cfg", []byte("[avatars]\ndefault = \"aza_choi\"\n")); err == nil || !strings.Contains(err.Error(), "neither hash nor an avatar id") {
		t.Errorf("unknown set: %v", err)
	}
}

// A key the file binds is taken from the action that had it by default; confirm and
// leave_preview may share keys with the board's actions.
func TestExplicitBindingWins(t *testing.T) {
	c, err := Parse("cfg", []byte("[keys]\nquit = [\"j\"]\nconfirm = [\"enter\"]\nleave_preview = [\"tab\"]\nfold = [\"alt+space\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := c.KeyMap()
	if !reflect.DeepEqual(c.Keys["down"], []string{"down"}) || m["j"] != "quit" || m["down"] != "down" {
		t.Errorf("down %v, j → %q", c.Keys["down"], m["j"])
	}
	if m["enter"] != "jump" || m["tab"] != "focus_preview" || !c.Is("confirm", "enter") || !c.Is("leave_preview", "tab") || c.Is("confirm", "y") {
		t.Errorf("modal keys: %v", m)
	}
	if m["ctrl+]"] != "" || m["y"] != "" {
		t.Errorf("KeyMap is the board's keys only: %v", m)
	}
	if m["alt+ "] != "fold" {
		t.Errorf(`alt+space is bubbletea's "alt+ ": %v`, m)
	}
	if _, err := Parse("cfg", []byte(c.String())); err != nil {
		t.Errorf("round trip: %v", err)
	}
}

// A board action taking confirm's / leave_preview's default key leaves them theirs.
func TestModalKeepsDefaults(t *testing.T) {
	c, err := Parse("cfg", []byte("[keys]\nhandoff = [\"y\"]\nreply = [\"tab\"]\n"))
	if err != nil || !c.Is("confirm", "y") || !c.Is("leave_preview", "tab") || c.KeyMap()["y"] != "handoff" || c.KeyMap()["tab"] != "reply" {
		t.Errorf("%v %v %v", err, c.Keys["confirm"], c.Keys["leave_preview"])
	}
}

// The defaults never share a key among the board's actions, and handing them out doesn't share the slices.
func TestDefaultKeys(t *testing.T) {
	seen := map[string]string{}
	for _, a := range Actions {
		for _, k := range Default().Keys[a] {
			if o, ok := seen[k]; ok && !modal[a] && !modal[o] {
				t.Errorf("%q: %s and %s", k, o, a)
			}
			seen[k] = a
		}
	}
	Default().Keys["up"][0] = "z"
	if Default().Keys["up"][0] != "k" {
		t.Error("changing one config changed the defaults")
	}
}

// A default action left with no key by the file has none (no error): new default actions never
// stop an old config from loading. Only an empty list the file writes is an error.
func TestActionLeftWithNoKey(t *testing.T) {
	c, err := Parse("cfg", []byte("[keys]\nfold = [\"a\"]\n")) // a is avatar's
	if err != nil || len(c.Keys["avatar"]) != 0 || c.First("avatar") != "" || c.KeyMap()["a"] != "fold" {
		t.Errorf("%v %v", err, c.Keys["avatar"])
	}
	if _, err := Parse("cfg", []byte("[keys]\navatar = []\n")); err == nil {
		t.Error("an empty list the file writes is an error")
	}
	back, err := Parse("cfg", []byte(c.String())) // printed as a comment: loads back the same
	if err != nil || !reflect.DeepEqual(back.Keys, c.Keys) || !strings.Contains(c.String(), "# avatar: no key") {
		t.Errorf("round trip: %v\n%s", err, c.String())
	}
}

// T22: match color and the n / N actions.
func TestMatchColorAndKeys(t *testing.T) {
	d := Default()
	if d.Colors["match"] != "#f2d06b" || !reflect.DeepEqual(d.Keys["next_match"], []string{"n"}) || !reflect.DeepEqual(d.Keys["prev_match"], []string{"N"}) {
		t.Errorf("defaults: %v %v %v", d.Colors["match"], d.Keys["next_match"], d.Keys["prev_match"])
	}
	if s := d.String(); !strings.Contains(s, `match = "#f2d06b"`) || !strings.Contains(s, `next_match = ["n"]`) || !strings.Contains(s, `prev_match = ["N"]`) {
		t.Errorf("printed:\n%s", s)
	}
	c, err := Parse("cfg", []byte("[colors]\nmatch = \"#112233\"\n[keys]\nnext_match = [\"ctrl+n\"]\n"))
	if err != nil || c.Colors["match"] != "#112233" || c.KeyMap()["ctrl+n"] != "next_match" || c.KeyMap()["n"] != "" {
		t.Errorf("%v %v", err, c.Colors["match"])
	}
	if _, err := Parse("cfg", []byte("[colors]\nmatch = \"yellow\"\n")); err == nil || !strings.Contains(err.Error(), "colors.match") {
		t.Errorf("a bad match color is an error: %v", err)
	}
	if _, err := Parse("cfg", []byte("[keys]\nnext_match = [\"j\"]\n")); err != nil {
		t.Errorf("taking j from down is allowed: %v", err) // the file's own bindings win
	}
}

// T25: [layout] max_columns, 1 to 8, default 4.
func TestMaxColumns(t *testing.T) {
	if Default().MaxColumns != 4 {
		t.Errorf("default %d", Default().MaxColumns)
	}
	for _, n := range []int{1, 5, 8} {
		c, err := Parse("cfg", []byte(fmt.Sprintf("[layout]\nmax_columns = %d\n", n)))
		if err != nil || c.MaxColumns != n {
			t.Errorf("%d: %v %d", n, err, c.MaxColumns)
		}
		if back, err := Parse("cfg", []byte(c.String())); err != nil || back.MaxColumns != n || !strings.Contains(c.String(), fmt.Sprintf("[layout]\nmax_columns = %d ", n)) {
			t.Errorf("printed back: %v\n%s", err, c.String())
		}
	}
	if c, _ := Parse("cfg", []byte("[layout]\n")); c.MaxColumns != 4 {
		t.Error("a section without the key keeps the default")
	}
	for toml, want := range map[string]string{
		"[layout]\nmax_columns = 0\n":     "cfg: layout.max_columns: 0 is out of range (1 to 8 columns)",
		"[layout]\nmax_columns = 9\n":     "cfg: layout.max_columns: 9 is out of range (1 to 8 columns)",
		"[layout]\nmax_columns = -1\n":    "layout.max_columns: -1 is out of range",
		"[layout]\nmax_columns = 4.5\n":   "cfg: toml:",
		"[layout]\nmax_columns = \"5\"\n": "cfg: toml:",
		"[layout]\nwidth = 5\n":           "cfg: layout.width: unknown setting",
		"layout = 5\n":                    "cfg: layout: must be a table, [layout]",
		"[[layout]]\nmax_columns = 5\n":   "cfg: layout: must be a table, [layout]",
	} {
		_, err := Parse("cfg", []byte(toml))
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%q: %v, want one line with %q", toml, err, want)
		}
	}
}

// T26: colors.border, empty by default (borders follow the status); "" is allowed only there.
func TestBorderColor(t *testing.T) {
	if Default().Colors["border"] != "" || !strings.Contains(Default().String(), "border = \"\" ") {
		t.Errorf("default %q", Default().Colors["border"])
	}
	c, err := Parse("cfg", []byte("[colors]\nborder = \"#585b70\"\n"))
	if err != nil || c.Colors["border"] != "#585b70" {
		t.Errorf("%v %q", err, c.Colors["border"])
	}
	if back, err := Parse("cfg", []byte(c.String())); err != nil || back.Colors["border"] != "#585b70" {
		t.Errorf("round trip: %v", err)
	}
	if c, err = Parse("cfg", []byte("[colors]\nborder = \"\"\n")); err != nil || c.Colors["border"] != "" {
		t.Errorf("an empty border is the default spelled out: %v", err)
	}
	if back, err := Parse("cfg", []byte(Default().String())); err != nil || back.Colors["border"] != "" {
		t.Errorf("the printed default loads back: %v", err)
	}
	for toml, hint := range map[string]bool{"[colors]\nborder = \"gray\"\n": true, "[colors]\nborder = \"300\"\n": true, "[colors]\nworking = \"\"\n": false} {
		_, err := Parse("cfg", []byte(toml))
		if err == nil || !strings.Contains(err.Error(), "is neither #rrggbb nor an ANSI color 0-255") || strings.Contains(err.Error(), `or "" to follow the status`) != hint {
			t.Errorf("%q: %v (the hint only for border)", toml, err)
		}
	}
}

func TestFlow(t *testing.T) {
	if Default().Flow != "bands" {
		t.Errorf("default %q", Default().Flow)
	}
	for _, f := range []string{"bands", "stack"} {
		c, err := Parse("cfg", []byte(fmt.Sprintf("[layout]\nflow = %q\n", f)))
		if err != nil || c.Flow != f {
			t.Errorf("%s: %v %q", f, err, c.Flow)
		}
		if back, err := Parse("cfg", []byte(c.String())); err != nil || back.Flow != f || !strings.Contains(c.String(), fmt.Sprintf("flow = %q ", f)) {
			t.Errorf("printed back: %v\n%s", err, c.String())
		}
	}
	if c, _ := Parse("cfg", []byte("[layout]\nmax_columns = 3\n")); c.Flow != "bands" {
		t.Error("a section without the key keeps the default")
	}
	for toml, want := range map[string]string{
		"[layout]\nflow = \"waterfall\"\n": `cfg: layout.flow: "waterfall" is not a flow (use "bands" or "stack")`,
		"[layout]\nflow = \"\"\n":          `layout.flow: "" is not a flow`,
		"[layout]\nflow = 3\n":             "cfg: toml:",
	} {
		_, err := Parse("cfg", []byte(toml))
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%q: %v, want one line with %q", toml, err, want)
		}
	}
}

func TestGroupBy(t *testing.T) {
	if Default().GroupBy != "cwd" {
		t.Errorf("default %q", Default().GroupBy)
	}
	for _, g := range GroupModes {
		c, err := Parse("cfg", []byte(fmt.Sprintf("[layout]\ngroup_by = %q\n", g)))
		if err != nil || c.GroupBy != g {
			t.Errorf("%s: %v %q", g, err, c.GroupBy)
		}
		if back, err := Parse("cfg", []byte(c.String())); err != nil || back.GroupBy != g || !strings.Contains(c.String(), fmt.Sprintf("group_by = %q ", g)) {
			t.Errorf("printed back: %v\n%s", err, c.String())
		}
	}
	if !slices.Equal(GroupModes, []string{"cwd", "project", "session", "state"}) {
		t.Errorf("the order g goes through: %v", GroupModes)
	}
	if c, _ := Parse("cfg", []byte("[layout]\nflow = \"stack\"\n")); c.GroupBy != "cwd" {
		t.Error("a section without the key keeps the default")
	}
	for toml, want := range map[string]string{
		"[layout]\ngroup_by = \"branch\"\n": `cfg: layout.group_by: "branch" is not a grouping (use cwd, project, session, state)`,
		"[layout]\ngroup_by = \"\"\n":       `layout.group_by: "" is not a grouping`,
		"[layout]\ngroup_by = 3\n":          "cfg: toml:",
	} {
		_, err := Parse("cfg", []byte(toml))
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%q: %v, want one line with %q", toml, err, want)
		}
	}
	if k := Default().Keys["group"]; !slices.Equal(k, []string{"g"}) {
		t.Errorf("group key %v", k)
	}
}

// exit (default Q) really ends the board, quit (q / esc) may only hide it under --stay.
func TestExitKey(t *testing.T) {
	if k := Default().Keys["exit"]; !slices.Equal(k, []string{"Q"}) {
		t.Errorf("exit key %v", k)
	}
	if !strings.Contains(Default().String(), `exit = ["Q"]`) {
		t.Errorf("agmux config does not print exit:\n%s", Default().String())
	}
}

// Control characters in a name go out as \uXXXX: Go's %q spells them \a \v \xNN, which TOML can't read,
// and a config.toml saved from `agmux config` would stop agmux from starting.
func TestStringQuotesForTOML(t *testing.T) {
	for in, want := range map[string]string{
		"plain": `"plain"`, "a b": `"a b"`, "tab\t": `"tab\t"`, "q\"b\\s": `"q\"b\\s"`, `\x41`: `"\\x41"`,
		"esc\x1b": `"esc\u001B"`, "del\x7f": `"del\u007F"`, "bell\a": `"bell\u0007"`, "vt\v": `"vt\u000B"`, "ff\f": `"ff\f"`, "bs\b": `"bs\b"`,
		"é日\U0001F600": `"é日😀"`,
	} {
		if got := tomlQuote(in); got != want {
			t.Errorf("tomlQuote(%q) = %s, want %s", in, got, want)
		}
	}
	c := Default()
	c.AvatarBind = map[string]string{"esc\x1b[0m": "bot-02", "bell\a\v\f\b\x00\x7f": "bot-03", "q\"\\": "bot-04", "日本": "bot-05"}
	back, err := Parse("cfg", []byte(c.String()))
	if err != nil || !reflect.DeepEqual(back.AvatarBind, c.AvatarBind) {
		t.Errorf("round trip: %v\n%v\n%v", err, back.AvatarBind, c.AvatarBind)
	}
}

// The defaults print as exactly testdata/config.example.toml: the template people are told to save as
// their config.toml. A change to what `agmux config` says shows up here as a diff to read;
// UPDATE_GOLDEN=1 go test ./internal/config rewrites the file.
func TestStringMatchesTheExample(t *testing.T) {
	const golden = "testdata/config.example.toml"
	got := Default().String()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < max(len(gl), len(wl)); i++ {
			g, w := "<none>", "<none>"
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("line %d differs from %s (UPDATE_GOLDEN=1 rewrites it):\n got  %s\n want %s", i+1, golden, g, w)
			}
		}
	}
}

// The group_<mode> actions are unbound by default; [] leaves them so (any other action needs a key),
// a key binds one, and a key still serves a single action.
func TestGroupDirectActions(t *testing.T) {
	for _, a := range groupActions {
		if len(Default().Keys[a]) != 0 || len(Default().KeyMap()) == 0 {
			t.Errorf("%s has keys by default: %v", a, Default().Keys[a])
		}
		if c, err := Parse("cfg", []byte("[keys]\n"+a+" = []\n")); err != nil || len(c.Keys[a]) != 0 {
			t.Errorf("%s = []: %v %v", a, err, c)
		}
	}
	if _, err := Parse("cfg", []byte("[keys]\ngroup = []\n")); err == nil || !strings.Contains(err.Error(), "needs at least one key") {
		t.Errorf("group = [] must still be refused: %v", err)
	}
	c, err := Parse("cfg", []byte("[keys]\ngroup_state = [\"G\"]\ngroup_cwd = [\"C\", \"alt+c\"]\n"))
	if err != nil || c.KeyMap()["G"] != "group_state" || c.KeyMap()["C"] != "group_cwd" || c.KeyMap()["alt+c"] != "group_cwd" {
		t.Fatalf("bound: %v %v", err, c.KeyMap())
	}
	if _, err := Parse("cfg", []byte("[keys]\ngroup_state = [\"G\"]\ngroup_cwd = [\"G\"]\n")); err == nil || !strings.Contains(err.Error(), "already bound to group_cwd") && !strings.Contains(err.Error(), "already bound to group_state") {
		t.Errorf("a key for two actions: %v", err)
	}
	if c, err := Parse("cfg", []byte("[keys]\ngroup_state = [\"g\"]\n")); err != nil || c.KeyMap()["g"] != "group_state" || len(c.Keys["group"]) != 0 {
		t.Errorf("a key the file binds is taken from the default's owner: %v %v", err, c.KeyMap())
	}
	if back, err := Parse("cfg", []byte(c.String())); err != nil || !reflect.DeepEqual(back, c) {
		t.Errorf("round trip: %v\n%s", err, c.String())
	}
	if out := Default().String(); !strings.Contains(out, "group_state = []") || !strings.Contains(out, "# default: none — switch straight to the state grouping") ||
		strings.Index(out, "\ngroup = ") > strings.Index(out, "\ngroup_cwd = ") || strings.Index(out, "\ngroup_state = ") > strings.Index(out, "\nrestart = ") {
		t.Errorf("printed:\n%s", out)
	}
}

// mark_read is m by default; rebound it is the new key only; like the other actions it must keep a
// key, and two actions may not both ask for one.
func TestMarkReadKeys(t *testing.T) {
	if Default().KeyMap()["m"] != "mark_read" {
		t.Errorf("default: %v", Default().KeyMap())
	}
	if c, err := Parse("cfg", []byte("[keys]\nmark_read = [\"M\"]\n")); err != nil || c.KeyMap()["M"] != "mark_read" || c.KeyMap()["m"] != "" {
		t.Errorf("rebound: %v %v", err, c.KeyMap())
	}
	if _, err := Parse("cfg", []byte("[keys]\nmark_read = []\n")); err == nil || !strings.Contains(err.Error(), "keys.mark_read: needs at least one key") {
		t.Errorf("mark_read = []: %v", err)
	}
	if _, err := Parse("cfg", []byte("[keys]\nmark_read = [\"x\"]\nkill = [\"x\"]\n")); err == nil || !strings.Contains(err.Error(), "already bound to") {
		t.Errorf("a key for two actions: %v", err)
	}
	if c, err := Parse("cfg", []byte("[keys]\npin = [\"m\"]\n")); err != nil || c.KeyMap()["m"] != "pin" || len(c.Keys["mark_read"]) != 0 {
		t.Errorf("m taken from the default's owner: %v %v", err, c.Keys["mark_read"])
	}
}

// [layout] preview_width is a percent from 30 to 70, 40 when not written; outside it is an error that
// says the range; the template prints it with its default.
func TestPreviewWidth(t *testing.T) {
	if Default().PreviewWidth != 40 {
		t.Errorf("default %d", Default().PreviewWidth)
	}
	for _, n := range []int{30, 40, 50, 70} {
		c, err := Parse("cfg", []byte(fmt.Sprintf("[layout]\npreview_width = %d\n", n)))
		if err != nil || c.PreviewWidth != n {
			t.Errorf("%d: %v %v", n, err, c)
		}
	}
	if c, err := Parse("cfg", []byte("[layout]\nflow = \"stack\"\n")); err != nil || c.PreviewWidth != 40 {
		t.Errorf("not written: %v %v", err, c)
	}
	for _, n := range []int{29, 71, 0, -5, 100} {
		if _, err := Parse("cfg", []byte(fmt.Sprintf("[layout]\npreview_width = %d\n", n))); err == nil || err.Error() != "cfg: layout.preview_width: must be 30–70" {
			t.Errorf("%d: %v", n, err)
		}
	}
	if _, err := Parse("cfg", []byte("[layout]\npreview_width = \"wide\"\n")); err == nil || !strings.Contains(err.Error(), "preview_width") {
		t.Errorf("a string: %v", err)
	}
	c, _ := Parse("cfg", []byte("[layout]\npreview_width = 55\n"))
	if out := c.String(); !strings.Contains(out, "preview_width = 55 ") {
		t.Errorf("printed:\n%s", out)
	}
	if back, err := Parse("cfg", []byte(c.String())); err != nil || back.PreviewWidth != 55 {
		t.Errorf("round trip: %v", err)
	}
	if !strings.Contains(Default().String(), "preview_width = 40            # default: 40 — the preview's share of the terminal width, in percent (30–70; never under 60 columns)") {
		t.Errorf("template line:\n%s", Default().String())
	}
}
