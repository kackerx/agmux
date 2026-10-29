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
	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/xdg"
)

// Actions, in the order `agmux config` prints them.
var Actions = []string{
	"up", "down", "left", "right", "page_down", "page_up", "jump", "fold", "preview", "focus_preview", "leave_preview", "preview_up", "preview_down",
	"reply", "handoff", "search", "next_match", "prev_match", "clear_filter", "flash", "group", "group_cwd", "group_project", "group_session", "group_state", "restart", "kill", "review", "avatar", "pin", "mark_read",
	"recall", "recall_filter", "recall_scope", "recall_copy", "recall_open", "recall_toggle", "recall_all", "recall_none",
	"quit", "exit", "confirm",
}

var defaultKeys = map[string][]string{
	"up": {"k", "up"}, "down": {"j", "down"}, "left": {"h", "left"}, "right": {"l", "right"},
	"page_down": {"ctrl+d"}, "page_up": {"ctrl+u"},
	"jump": {"enter"}, "fold": {"f"}, "preview": {"p"}, "focus_preview": {"tab"}, "leave_preview": {"tab"}, "preview_up": {"pgup", "ctrl+p"}, "preview_down": {"pgdown", "ctrl+n"},
	// search / flash are T19 / T20
	"reply": {"i"}, "handoff": {"H"}, "search": {"/"}, "next_match": {"n"}, "prev_match": {"N"}, "clear_filter": {"esc"}, "flash": {"s"}, "group": {"g"},
	"group_cwd": {}, "group_project": {}, "group_session": {}, "group_state": {},
	"restart": {"r"}, "kill": {"x"}, "review": {"d"}, "avatar": {"a"}, "pin": {"t"}, "mark_read": {"m"},
	"recall": {"?"}, "recall_filter": {"ctrl+f"}, "recall_scope": {"ctrl+t"}, "recall_copy": {"ctrl+y"}, "recall_open": {"ctrl+o"},
	"recall_toggle": {"space"}, "recall_all": {"a"}, "recall_none": {"A"},
	"quit": {"q", "esc"}, "exit": {"Q"}, "confirm": {"y"},
}

// Colors, in print order.
var Colors = []string{
	"working", "idle", "done", "attention", "focus", "match", "border", "name", "branch", "model", "effort", "agent", "dir",
	"bar_low", "bar_mid", "bar_high", "bar_empty", "muted", "disabled", "recall_border", "recall_focus", "recall_preview",
}

var defaultColors = map[string]string{
	"working": "#ffb86c", "idle": "#585b70", "done": "#50fa7b", "attention": "#ff6188", "focus": "#ff8ebe", "match": "#f2d06b", "border": "",
	"name": "5", "branch": "12", "model": "14", "effort": "13", "agent": "#bd93f9", "dir": "11",
	"bar_low": "#a6e3a1", "bar_mid": "#f9e2af", "bar_high": "#ff6188", "bar_empty": "#585b70",
	"muted": "#6272a4", "disabled": "#45475a", "recall_border": "#6272a4", "recall_focus": "#ff8ebe", "recall_preview": "#bd93f9",
}

type Config struct {
	Path   string
	Keys   map[string][]string // action → key names (bubbletea's, "space" for the space bar)
	Colors map[string]string   // name → "#rrggbb" or an ANSI index "0".."255"

	MaxColumns   int    // [layout] max_columns: the most columns of cards, if the terminal is wide enough
	PreviewWidth int    // [layout] preview_width: the preview's share of the terminal width, in percent
	GroupBy      string // [layout] group_by: how cards are grouped at start: one of GroupModes
	Flow         string // [layout] flow: "bands" (a row of groups at a time) or "stack" (group k in column k mod n)
	RecallBorder string // [layout] recall_border: the corners of recall's three boxes, "thick" or "rounded"

	Language string // [ui] language: "en" or "zh"; $AGMUX_LANG wins (i18n.Resolve)

	AvatarRenderer string            // [avatars] renderer: "auto", "image" or "pixel"
	AvatarDefault  string            // [avatars] default: "hash" or an avatar ID (sprite or image set)
	AvatarBind     map[string]string // [avatars.bind]: session name → avatar ID
}

// Default is the configuration with no file.
func Default() *Config {
	c := &Config{Keys: map[string][]string{}, Colors: map[string]string{}, MaxColumns: 4, PreviewWidth: 40, Flow: "bands", RecallBorder: "thick", GroupBy: "cwd", Language: "en", AvatarRenderer: "auto", AvatarDefault: "hash", AvatarBind: map[string]string{}}
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

// groupActions switch straight to a grouping (group_<mode>); they have no default key, and may be left unbound with [].
var groupActions = []string{"group_cwd", "group_project", "group_session", "group_state"}

// GroupModes are the ways to group cards, in the order the group key goes through them.
var GroupModes = []string{"cwd", "project", "session", "state"}

// The range of [layout] max_columns.
const minMaxColumns, maxMaxColumns = 1, 8

// The range of [layout] preview_width, in percent.
const minPreviewWidth, maxPreviewWidth = 30, 70

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Parse merges a config file over the defaults (an action set in the file replaces its default
// keys) and checks it. Every error is one line naming the file, the setting and the reason.
func Parse(path string, data []byte) (*Config, error) {
	var raw struct {
		Keys   map[string][]string `toml:"keys"`
		Colors map[string]string   `toml:"colors"`
		Layout struct {
			MaxColumns   int    `toml:"max_columns"`
			PreviewWidth int    `toml:"preview_width"`
			Flow         string `toml:"flow"`
			GroupBy      string `toml:"group_by"`
			RecallBorder string `toml:"recall_border"`
		} `toml:"layout"`
		UI struct {
			Language string `toml:"language"`
		} `toml:"ui"`
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
	for _, sec := range []string{"keys", "colors", "layout", "ui", "avatars"} {
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
		return nil, fmt.Errorf("%s: %s: unknown setting (sections are [keys], [colors], [layout], [ui], [avatars] and [avatars.bind])", path, u[0])
	}
	c := Default()
	c.Path = path
	for action, keys := range raw.Keys {
		if _, ok := defaultKeys[action]; !ok {
			return nil, fmt.Errorf("%s: keys.%s: unknown action (one of %s)", path, action, strings.Join(Actions, ", "))
		}
		if len(keys) == 0 && !slices.Contains(groupActions, action) {
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
	if md.IsDefined("layout", "preview_width") {
		if n := raw.Layout.PreviewWidth; n < minPreviewWidth || n > maxPreviewWidth {
			return nil, fmt.Errorf("%s: layout.preview_width: %d is out of range (%d to %d percent)", path, n, minPreviewWidth, maxPreviewWidth)
		}
		c.PreviewWidth = raw.Layout.PreviewWidth
	}
	if md.IsDefined("layout", "flow") {
		if f := raw.Layout.Flow; f != "bands" && f != "stack" {
			return nil, fmt.Errorf("%s: layout.flow: %q is not a flow (use \"bands\" or \"stack\")", path, f)
		}
		c.Flow = raw.Layout.Flow
	}
	if md.IsDefined("layout", "group_by") {
		if g := raw.Layout.GroupBy; !slices.Contains(GroupModes, g) {
			return nil, fmt.Errorf("%s: layout.group_by: %q is not a grouping (use %s)", path, g, strings.Join(GroupModes, ", "))
		}
		c.GroupBy = raw.Layout.GroupBy
	}
	if md.IsDefined("layout", "recall_border") {
		if b := raw.Layout.RecallBorder; b != "thick" && b != "rounded" {
			return nil, fmt.Errorf("%s: layout.recall_border: %q is not a border (use \"thick\" or \"rounded\")", path, b)
		}
		c.RecallBorder = raw.Layout.RecallBorder
	}
	if md.IsDefined("ui", "language") {
		if l := raw.UI.Language; !i18n.Valid(l) {
			return nil, fmt.Errorf("%s: ui.language: %q is not a language (use %s)", path, l, strings.Join(i18n.Langs, " or "))
		}
		c.Language = raw.UI.Language
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

// modal actions only act in their own mode (the kill prompt, pass-through, a kept filter), so they
// may share keys with the board's; KeyMap leaves them out, Is asks about them.
var modal = map[string]bool{"confirm": true, "leave_preview": true, "clear_filter": true,
	"recall_filter": true, "recall_scope": true, "recall_copy": true, "recall_open": true, "recall_toggle": true, "recall_all": true, "recall_none": true}

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

// typed reports whether key (as KeyMsg.String() spells it) is a character that text can contain.
func typed(key string) bool { return utf8.RuneCountInString(key) == 1 }

// IsCtl is Is for a key that is not a character: while a prompt takes text (search, flash) the
// letters of a binding are text, and only ctrl / alt / named keys act.
func (c *Config) IsCtl(action, key string) bool { return !typed(key) && c.Is(action, key) }

// FirstCtl is the first key of action that IsCtl accepts and that is not one of skip, or def when
// there is none.
func (c *Config) FirstCtl(action, def string, skip ...string) string {
	for _, k := range c.Keys[action] {
		if !typed(norm(k)) && !slices.Contains(skip, k) {
			return k
		}
	}
	return def
}

// First is the key shown for action in the footer and prompts; "" when it has none.
func (c *Config) First(action string) string {
	if len(c.Keys[action]) == 0 {
		return ""
	}
	return c.Keys[action][0]
}

// actionUse and colorUse are what the printed config says about a setting after its default; none
// for a self-explanatory one.
var actionUse = map[string]string{
	"page_down": "scroll the board a page down", "page_up": "scroll the board a page up",
	"jump": "switch tmux to the focused pane", "fold": "fold / unfold the focused group",
	"preview": "open / close the pane preview", "focus_preview": "pass keys through to the previewed pane; in recall, move the focus input > list > preview",
	"leave_preview": "back to the board", "reply": "type a reply into the focused session",
	"handoff": "send /handoff to the focused session", "search": "filter the cards by fuzzy search",
	"next_match": "next search hit", "prev_match": "previous search hit", "flash": "move the focus to a card by its label",
	"group": "cycle the grouping (see [layout] group_by)", "group_cwd": "switch straight to the cwd grouping",
	"group_project": "switch straight to the project grouping", "group_session": "switch straight to the session grouping",
	"group_state": "switch straight to the state grouping", "restart": "restart the focused session", "kill": "kill the focused session (asks first)",
	"review": "show the diff of the focused session's repo (hunk, else git diff)", "avatar": "open or close the avatar picker under the focused card",
	"pin": "pin the focused card to the top of its group", "mark_read": "mark the focused card's done as seen (it turns idle)",
	"clear_filter": "drop a kept search filter before anything else", "preview_up": "scroll the preview back", "preview_down": "scroll the preview forward",
	"recall": "find a closed session by what was said in it (agmux recall)", "recall_filter": "recall: open / close the filter panel (sources, scope)",
	"recall_scope": "recall: switch between this project and all history", "recall_copy": "recall: copy the command that resumes the session",
	"recall_open": "recall: open the transcript in $EDITOR", "recall_toggle": "recall: in the filter panel, tick / pick the line", "recall_all": "recall: in the filter panel, tick every source",
	"recall_none": "recall: in the filter panel, clear every source",
	"quit":        "with `board --stay` inside a popup: hide instead", "exit": "always quit, even with --stay", "confirm": "answer yes to a prompt",
}

var colorUse = map[string]string{
	"working": "card border / status while an agent works", "attention": "waiting for you / alerts",
	"focus": "the focused card", "match": "search hits", "border": "borders of unfocused cards",
	"name": "session name", "agent": "the agent role on line 2", "dir": "group headers and their rule",
	"bar_low": "context bar, low usage", "muted": "secondary text, footer",
	"recall_border": "recall: the border of the query box and the list when they have no keys", "recall_focus": "recall: the border of the one that has the keys: the query box while typing, the list once the query is kept, the preview after tab", "recall_preview": "recall: the border of the preview when it has no keys",
}

// String is the effective config as a commented TOML file, ready to save as config.toml: each setting
// on a line of its own with its current value, and after it a comment with its default (the same in
// every file) and what it does. Parse reads it back as the same config.
func (c *Config) String() string {
	var b strings.Builder
	// line is a setting with a trailing comment starting at column 30; cont goes on under it
	line := func(text, comment string) { fmt.Fprintf(&b, "%-29s # %s\n", text, comment) }
	cont := func(comment string) { fmt.Fprintf(&b, "%30s#   %s\n", "", comment) }
	with := func(def, use string) string {
		if use == "" {
			return "default: " + def
		}
		return "default: " + def + " — " + use
	}
	b.WriteString(`# agmux configuration — ~/.config/agmux/config.toml ($XDG_CONFIG_HOME/agmux/config.toml)
# Every setting is optional; a missing one keeps its default. ` + "`agmux config`" + ` prints the
# effective values. Defaults are shown in the comments; lines left at their default are harmless.

[keys]
# Each action takes a list of key names (bubbletea names): any single character, "enter", "esc",
# "tab", "shift+tab", "space", "backspace", "up"/"down"/"left"/"right", "home"/"end", "pgup"/"pgdown",
# "f1".."f20", "ctrl+<letter>", "alt+<key>". An uppercase letter means shift+letter.
# ctrl+c cannot be bound (it always quits); ctrl+i and ctrl+m are tab and enter.
# A key may be bound to only one action, but confirm, leave_preview, clear_filter and the recall_* ones (all but
# recall) may share keys with others.
# While a prompt takes text (search, flash) letters are text: enter and jump's non-character keys
# keep the filter, esc and quit's non-character keys cancel it; backspace, ctrl+h, ctrl+c are fixed.
# The text box of search and recall has fixed editing keys too (not in [keys]): left / right move the caret,
# home / ctrl+a and end / ctrl+e go to the start and the end, alt+backspace and ctrl+w delete the word before
# it, anything printable goes in at the caret. [keys] left / right do not act while typing.
`)
	for _, a := range Actions {
		def := strings.Join(defaultKeys[a], ", ")
		if def == "" {
			def = "none"
		}
		if len(c.Keys[a]) == 0 && len(defaultKeys[a]) > 0 { // "a = []" would not load back
			fmt.Fprintf(&b, "# %s: no key (another action took its default)    # %s\n", a, with(def, actionUse[a]))
			continue
		}
		q := make([]string, len(c.Keys[a]))
		for i, k := range c.Keys[a] {
			q[i] = strconv.Quote(k) // key names are printable: nothing Quote spells TOML can't read
		}
		line(fmt.Sprintf("%s = [%s]", a, strings.Join(q, ", ")), with(def, actionUse[a]))
	}
	b.WriteString(`
[colors]
# A color is "#rrggbb" or an ANSI palette index "0".."255" (as a string).
`)
	for _, n := range Colors {
		d := defaultColors[n]
		if d == "" {
			d = `"" (follow the status)`
		}
		line(fmt.Sprintf("%s = %q", n, c.Colors[n]), with(d, colorUse[n]))
	}
	b.WriteString("\n[layout]\n")
	line(fmt.Sprintf("max_columns = %d", c.MaxColumns), with("4", fmt.Sprintf("%d to %d; fewer columns are used when the terminal is narrow", minMaxColumns, maxMaxColumns)))
	line(fmt.Sprintf("preview_width = %d", c.PreviewWidth), with("40", fmt.Sprintf("the preview's share of the terminal width, in percent (%d–%d; never under 60 columns)", minPreviewWidth, maxPreviewWidth)))
	line(fmt.Sprintf("flow = %q", c.Flow), with(`"bands"`, `"bands": a row of groups at a time, all rows the same`))
	cont(`height; "stack": groups stack down each column, no gaps`)
	line(fmt.Sprintf("group_by = %q", c.GroupBy), with(`"cwd"`, `grouping at start: "cwd", "project", "session" or`))
	cont(`"state"; the group key cycles through them in that order`)
	line(fmt.Sprintf("recall_border = %q", c.RecallBorder), with(`"thick"`, `recall's three boxes: "thick" ┏━┓ or "rounded" ╭─╮`))
	b.WriteString("\n[ui]\n")
	line(fmt.Sprintf("language = %q", c.Language), with(`"en"`, `"en" or "zh"; $AGMUX_LANG overrides it`))
	b.WriteString("\n[avatars]\n")
	line(fmt.Sprintf("renderer = %q", c.AvatarRenderer), with(`"auto"`, `"auto": pictures in kitty / ghostty, pixel art`))
	cont(`elsewhere; "image": always pictures; "pixel": always pixel art`)
	line(fmt.Sprintf("default = %q", c.AvatarDefault), with(`"hash"`, `"hash": pick one from the session name (a picture set`))
	cont(`in picture mode, a pixel avatar otherwise); or an avatar id (see ` + "`agmux avatars`" + `)`)
	cont(`for every unbound session`)
	b.WriteString(`
[avatars.bind]
# session name = avatar id, e.g.  "agmux" = "nova"
# The picker (a) saves its choices in $XDG_STATE_HOME/agmux/avatars.json instead; they win over these.
# Your own sets go in ~/.config/agmux/avatars/<id>/<state>/*.png (states: idle, think, read,
# write, tool, hi, success) or ` + "`agmux avatars import <sheet.png> --name <id>`" + `.
`)
	for _, n := range slices.Sorted(maps.Keys(c.AvatarBind)) {
		fmt.Fprintf(&b, "%s = %s\n", tomlQuote(n), tomlQuote(c.AvatarBind[n]))
	}
	return b.String()
}

// tomlQuote is s as a TOML basic string. Go's %q writes \a \v and \xNN for control characters, which
// TOML has no escapes for (it has \b \t \n \f \r \uXXXX): they go as \u00XX.
func tomlQuote(s string) string {
	q := strconv.Quote(s)
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		if q[i] != '\\' || i+1 >= len(q) {
			b.WriteByte(q[i])
			continue
		}
		i++
		switch q[i] {
		case 'a':
			b.WriteString(`\u0007`)
		case 'v':
			b.WriteString(`\u000B`)
		case 'x':
			if i+2 < len(q) {
				b.WriteString(`\u00` + strings.ToUpper(q[i+1:i+3]))
				i += 2
				continue
			}
			b.WriteString(`\x`)
		default:
			b.WriteByte('\\')
			b.WriteByte(q[i])
		}
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
