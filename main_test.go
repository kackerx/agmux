package main

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

// `agmux avatars` lists the image sets first (id, source, frames per state), then the sprites as
// before; a set without idle frames is said on stderr, and a set named like a sprite replaces it.
func TestAvatarsOutput(t *testing.T) {
	t.Cleanup(func() { avatar.Sets = nil })
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for name, label := range map[string]string{
		"avatars/kit/idle/1.png": "a", "avatars/kit/working/1.png": "b", "avatars/kit/working/2.png": "c",
		"avatars/bot-01/idle/1.png": "d", "avatars/noidle/done/1.png": "e",
	} {
		data := string(pngOf(label))
		p := filepath.Join(dir, "agmux", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	avatars(&out, &errs)
	lines := strings.Split(out.String(), "\n")
	// the twenty built-in sets, with the frames of each state that are not alternate takes (_v2…)
	want := []string{
		"aza_choi  builtin  idle 2 · think 2 · read 4 · write 4 · tool 4 · hi 4 · success 0",
		"cipher  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 0",
		"default  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 0",
		"forge  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"moss  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"nova  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"oana  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"quill  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
		"red  builtin  idle 2 · think 2 · read 3 · write 2 · tool 2 · hi 2 · success 0",
		"sprocket  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2",
	}
	for _, id := range []string{"developer", "researcher", "operator", "tester", "designer", "naruto", "sasuke", "sakura", "kakashi", "itachi"} {
		want = append(want, id+"  builtin  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2")
	}
	want = append(want,
		"bot-01  user  idle 1 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 0",
		"kit  user  idle 1 · think 2 · read 0 · write 0 · tool 0 · hi 0 · success 0", // working/ is think's old name
		"",
		"bot-02  Boxy", // bot-01 is a set now: its sprite is gone from the list
	)
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: %q, want %q", i, lines[i], w)
		}
	}
	if strings.Count(out.String(), "bot-01") != 1 || !strings.Contains(out.String(), "\nbot-05  ") {
		t.Errorf("sprites:\n%s", out.String())
	}
	if !strings.Contains(errs.String(), `"noidle"`) || strings.Count(errs.String(), "\n") != 1 {
		t.Errorf("stderr: %q", errs.String())
	}
}

// pngOf is a small valid PNG, a different one for every label.
func pngOf(label string) []byte {
	h := fnv.New32a()
	h.Write([]byte(label))
	im := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(im, im.Bounds(), &image.Uniform{color.NRGBA{uint8(h.Sum32()), uint8(h.Sum32() >> 8), uint8(h.Sum32() >> 16), 255}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}

// `agmux avatars import`: flags before or after the sheet, usage errors exit 2, a set that exists
// needs --force, and the new set shows up in `agmux avatars` with its seven states.
func TestAvatarsImport(t *testing.T) {
	t.Cleanup(func() { avatar.Sets = nil })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	sheet := filepath.Join(t.TempDir(), "sheet.png")
	im := image.NewNRGBA(image.Rect(0, 0, 240, 200))
	draw.Draw(im, im.Bounds(), &image.Uniform{color.NRGBA{20, 20, 60, 255}}, image.Point{}, draw.Src)
	f, _ := os.Create(sheet)
	png.Encode(f, im)
	f.Close()
	run := func(args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		return avatarsCmd(args, &out, &errs), out.String(), errs.String()
	}
	for _, args := range [][]string{{"nope"}, {"import"}, {"import", sheet}, {"import", "--name", "kit"}, {"import", sheet, "extra", "--name", "kit"}, {"import", "--bad"}} {
		if code, _, errs := run(args...); code != 2 || !strings.Contains(errs, "usage") && !strings.Contains(errs, "flag provided") {
			t.Errorf("%v: exit %d, %q", args, code, errs)
		}
	}
	if _, _, errs := run("import", "-h"); !strings.Contains(errs, "<sheet.png>") || !strings.Contains(errs, "-force") {
		t.Errorf("-h prints %q", errs)
	}
	if code, out, errs := run("import", sheet, "--name", "kit"); code != 0 || !strings.Contains(out, "imported") {
		t.Fatalf("exit %d, %q %q", code, out, errs)
	}
	if code, _, errs := run("import", "--name", "kit", sheet); code != 1 || !strings.Contains(errs, "--force") {
		t.Errorf("existing set: exit %d, %q", code, errs)
	}
	if code, _, errs := run("import", "--name", "kit", "--force", sheet); code != 0 {
		t.Errorf("--force: exit %d, %q", code, errs)
	}
	var out, errs bytes.Buffer
	avatars(&out, &errs)
	if want := "kit  user  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2"; !strings.Contains(out.String(), want) {
		t.Errorf("avatars lists:\n%s\nwant a line %q", out.String(), want)
	}
}

// `agmux show` has the session's agent definition (empty if it has none) and then bg (empty unless a background session).
func TestShowAgentColumn(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s.jsonl")
	os.WriteFile(log, []byte(`{"type":"agent-setting","agentSetting":"orchestrator"}`+"\n"), 0o644)
	p := &store.Poller{StateDir: dir, Sources: []store.Source{store.SourceFunc(func() ([]store.Raw, error) {
		return []store.Raw{{ID: "a", Name: "one", Transcript: log}, {ID: "b", Name: "two"}, {ID: "c", Name: "three", Background: true}}, nil
	})}}
	var out bytes.Buffer
	if err := show(&out, p); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "\torchestrator\t") || !strings.HasSuffix(lines[1], "\t\t") || strings.Count(lines[1], "\t") != 7 || !strings.HasSuffix(lines[2], "\t\tbg") {
		t.Errorf("show:\n%s", out.String())
	}
}

func TestBoardCmdArguments(t *testing.T) {
	for _, args := range [][]string{{"--stay", "x"}, {"--sty"}, {"stay"}} {
		if code := boardCmd(args); code != 2 {
			t.Errorf("board %v: exit %d, want 2", args, code)
		}
	}
}

// `agmux config` prints the commented template with the effective values: what is in config.toml
// shows, everything else is the default, and the output saved as config.toml loads back as the same config.
func TestConfigCmdPrintsTheTemplate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "agmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agmux", "config.toml"), []byte("[layout]\nmax_columns = 6\nflow = \"stack\"\n[keys]\nquit = [\"q\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := configCmd(nil)
	os.Stdout = old
	w.Close()
	var out bytes.Buffer
	out.ReadFrom(r)
	got := out.String()
	if code != 0 || !strings.HasPrefix(got, "# agmux configuration") {
		t.Fatalf("exit %d:\n%s", code, got)
	}
	for _, want := range []string{"max_columns = 6 ", "# default: 4", `flow = "stack" `, `quit = ["q"] `, "# default: q, esc", "\n[avatars.bind]\n# session name = avatar id"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	saved := filepath.Join(dir, "saved.toml")
	if err := os.WriteFile(saved, []byte(got), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := config.Parse(saved, []byte(got))
	if err != nil || back.MaxColumns != 6 || back.Flow != "stack" || len(back.Keys["quit"]) != 1 || back.Keys["exit"][0] != "Q" {
		t.Errorf("saved as config.toml: %v %+v", err, back)
	}
}

// --version says what the build put in; what it did not, Go's record of the module and the vcs
// (a go install ...@v0.1.0, or a build in a checkout); dev, none and unknown with neither.
func TestVersionLine(t *testing.T) {
	defer func(v, c, d string) { version, commit, date = v, c, d }(version, commit, date)
	info := func(v string, vcs ...string) *debug.BuildInfo {
		b := &debug.BuildInfo{Main: debug.Module{Version: v}}
		for i := 0; i+1 < len(vcs); i += 2 {
			b.Settings = append(b.Settings, debug.BuildSetting{Key: vcs[i], Value: vcs[i+1]})
		}
		return b
	}
	rev, tm := "3f2a1b9c0d4e5f60718293a4b5c6d7e8f9012345", "2026-10-17T06:01:34Z"
	for _, c := range []struct {
		info          *debug.BuildInfo
		v, cm, d, out string
	}{
		{nil, "dev", "none", "unknown", "agmux dev (none, unknown)"},
		{info("(devel)"), "dev", "none", "unknown", "agmux dev (none, unknown)"},
		{info(""), "dev", "none", "unknown", "agmux dev (none, unknown)"},
		{info("v0.1.0"), "dev", "none", "unknown", "agmux v0.1.0 (none, unknown)"},
		{info("(devel)", "vcs.revision", rev, "vcs.time", tm), "dev", "none", "unknown", "agmux dev (3f2a1b9, " + tm + ")"},
		{info("(devel)", "vcs.revision", "abc"), "dev", "none", "unknown", "agmux dev (abc, unknown)"},
		{info("v0.1.0", "vcs.revision", rev, "vcs.time", tm), "v0.2.0", "abc1234", "2026-01-01T00:00:00Z", "agmux v0.2.0 (abc1234, 2026-01-01T00:00:00Z)"}, // ldflags win
	} {
		version, commit, date = c.v, c.cm, c.d
		if got := versionLine(c.info); got != c.out {
			t.Errorf("%v with %q %q %q: %q, want %q", c.info, c.v, c.cm, c.d, got, c.out)
		}
	}
}

// `agmux version` and `agmux --version` print the one line from the build's record and exit 0;
// with more words they are a usage error.
func TestVersionCommand(t *testing.T) {
	defer func(f func() (*debug.BuildInfo, bool)) { readBuildInfo = f }(readBuildInfo)
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v9.8.7"}}, true
	}
	for _, arg := range []string{"version", "--version"} {
		var out, errs bytes.Buffer
		if code := run([]string{arg}, &out, &errs); code != 0 || errs.Len() != 0 ||
			out.String() != "agmux v9.8.7 (none, unknown)\n" { // no ldflags in a test: the record's version
			t.Errorf("%s: exit %d, %q, %q", arg, code, out.String(), errs.String())
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{"--version", "x"}, &out, &errs); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "usage") {
		t.Errorf("--version x: exit %d, %q, %q", code, out.String(), errs.String())
	}
}

// `agmux setup` does the status line and then the pi extension; --write is for both, a usage error stops before either.
func TestSetupInstallsThePiExtensionToo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent := filepath.Join(home, "pi-agent")
	os.MkdirAll(agent, 0o755)
	t.Setenv("PI_CODING_AGENT_DIR", agent)
	ext := filepath.Join(agent, "extensions", "agmux.js")
	var out, errs bytes.Buffer
	if code := run([]string{"setup"}, &out, &errs); code != 0 || !strings.Contains(out.String(), "would install the extension") {
		t.Errorf("dry run: %d %q %q", code, out.String(), errs.String())
	}
	if _, err := os.Stat(ext); err == nil {
		t.Fatal("a dry run installed it")
	}
	out.Reset()
	if code := run([]string{"setup", "--write"}, &out, &errs); code != 0 || !strings.Contains(out.String(), "installed "+ext) || !strings.Contains(out.String(), "statusLine.command") {
		t.Errorf("write: %d %q %q", code, out.String(), errs.String())
	}
	if _, err := os.Stat(ext); err != nil {
		t.Error(err)
	}
	out.Reset()
	if code := run([]string{"setup", "--bogus"}, &out, &errs); code != 2 || strings.Contains(out.String(), "pi:") {
		t.Errorf("usage: %d %q", code, out.String())
	}
}
