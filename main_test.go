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
	"time"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/i18n"
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
	t.Setenv("XDG_CONFIG_HOME", home) // setup reads [ui] language: never the real config
	t.Setenv("AGMUX_LANG", "en")
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

// T74: `agmux setup` speaks $AGMUX_LANG, else [ui] language of the config, else English; a config that
// does not load does not stop it.
func TestSetupLanguage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	agent := filepath.Join(home, "pi-agent")
	os.MkdirAll(agent, 0o755)
	t.Setenv("PI_CODING_AGENT_DIR", agent)
	t.Cleanup(func() { i18n.Set("en") })
	say := func(env string) string {
		t.Setenv("AGMUX_LANG", env)
		var out, errs bytes.Buffer
		if code := run([]string{"setup"}, &out, &errs); code != 0 {
			t.Fatalf("setup: %d %q", code, errs.String())
		}
		return out.String()
	}
	if got := say(""); !strings.Contains(got, "dry run: add --write to apply") {
		t.Errorf("no config, no env: %q", got)
	}
	if got := say("zh"); !strings.Contains(got, "试运行：加 --write 才会写入") || !strings.Contains(got, "将安装扩展") || strings.Contains(got, "dry run") {
		t.Errorf("AGMUX_LANG=zh: %q", got)
	}
	os.MkdirAll(filepath.Join(home, "agmux"), 0o755)
	cfg := filepath.Join(home, "agmux", "config.toml")
	os.WriteFile(cfg, []byte("[ui]\nlanguage = \"zh\"\n"), 0o644)
	if got := say(""); !strings.Contains(got, "试运行") {
		t.Errorf("config zh: %q", got)
	}
	if got := say("en"); !strings.Contains(got, "dry run") {
		t.Errorf("the env wins over the config: %q", got)
	}
	os.WriteFile(cfg, []byte("[ui]\nlanguage = \"fr\"\n"), 0o644) // broken: setup goes on, in English
	if got := say(""); !strings.Contains(got, "dry run") {
		t.Errorf("broken config: %q", got)
	}
}

// T75: `agmux recall --dump [query]` lists the sessions of both agents, one tab-separated line each:
// source, id, name, project, branch, mtime (UTC), hits, snippet. The cache is under $XDG_CACHE_HOME.
func TestRecallDump(t *testing.T) {
	claude, agent, cache := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("PI_CODING_AGENT_DIR", agent)
	t.Setenv("XDG_CACHE_HOME", cache)
	put := func(dst, src string) string {
		b, err := os.ReadFile(filepath.Join("internal", "recall", "testdata", src))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(dst, b, 0o644)
		return dst
	}
	cp := put(filepath.Join(claude, "projects", "-work-shopcart", "3f6c1a52.jsonl"), "claude-session.jsonl")
	pp := put(filepath.Join(agent, "sessions", "--work-ledger--", "2026-09-02T08-00-00-000Z_019e0000.jsonl"), "pi-session.jsonl")
	os.Chtimes(cp, time.Date(2026, 10, 20, 3, 5, 0, 0, time.UTC), time.Date(2026, 10, 20, 3, 5, 0, 0, time.UTC))
	os.Chtimes(pp, time.Date(2026, 9, 2, 8, 5, 0, 0, time.UTC), time.Date(2026, 9, 2, 8, 5, 0, 0, time.UTC))
	dump := func(args ...string) (string, int) {
		var out, errs bytes.Buffer
		code := run(append([]string{"recall"}, args...), &out, &errs)
		if code == 0 && errs.Len() > 0 {
			t.Errorf("stderr: %q", errs.String())
		}
		return out.String(), code
	}
	out, code := dump("--dump")
	rows := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if code != 0 || len(rows) != 2 {
		t.Fatalf("%d %q", code, out)
	}
	wantClaude := "claude\t3f6c1a52\tcart-fix-2\tshopcart\tfix/coupon-order\t2026-10-20T03:05:00Z\t4\tnow add a regression test for the coupon order"
	wantPi := "pi\t019e0000-aaaa-7bbb-8ccc-000000000001\taudit-export-v2\tapi\t\t2026-09-02T08:05:00Z\t3\tswitch to a left join and add a test"
	if rows[0] != wantClaude || rows[1] != wantPi {
		t.Errorf("rows:\n%q\n%q\nwant\n%q\n%q", rows[0], rows[1], wantClaude, wantPi)
	}
	if _, err := os.Stat(filepath.Join(cache, "agmux", "recall", "claude-3f6c1a52.json")); err != nil {
		t.Error("the cache is not under $XDG_CACHE_HOME/agmux/recall:", err)
	}
	if out, _ := dump("--dump", "'left", "join"); strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "pi\t") || !strings.Contains(out, "\t1\t") {
		t.Errorf("a query, split over arguments: %q", out)
	}
	if out, _ := dump("--dump", "zzzqqq"); out != "" {
		t.Errorf("no match: %q", out)
	}
	// --reindex throws the cache away
	stale := filepath.Join(cache, "agmux", "recall", "claude-3f6c1a52.json")
	b, _ := os.ReadFile(stale)
	os.WriteFile(stale, []byte(strings.Replace(string(b), "cart-fix-2", "STALE", 1)), 0o644)
	if out, _ := dump("--dump"); !strings.Contains(out, "STALE") {
		t.Fatalf("the cache is trusted: %q", out)
	}
	if out, _ := dump("--dump", "--reindex"); strings.Contains(out, "STALE") || !strings.Contains(out, "cart-fix-2") {
		t.Errorf("--reindex: %q", out)
	}
	// "--" ends the options: a query may start with --
	if out, code := dump("--dump", "--", "--reindex"); code != 0 || out != "" {
		t.Errorf("-- then a query: exit %d %q", code, out)
	}
	if out, code := dump("--dump", "--", "'left"); code != 0 || !strings.HasPrefix(out, "pi\t") {
		t.Errorf("-- then a query: exit %d %q", code, out)
	}
	// a tab or newline in a branch name does not break the columns
	odd := filepath.Join(claude, "projects", "-w", "odd.jsonl")
	os.MkdirAll(filepath.Dir(odd), 0o755)
	os.WriteFile(odd, []byte(`{"type":"user","gitBranch":"x\ty\nz","cwd":"/w","message":{"role":"user","content":"sentence with a strange branch"}}`+"\n"), 0o644)
	out, _ = dump("--dump", "'strange")
	if row := strings.TrimRight(out, "\n"); strings.Count(row, "\t") != 7 || strings.Contains(row, "\n") || !strings.Contains(row, "\tx y z\t") {
		t.Errorf("columns: %q", out)
	}
	for _, args := range [][]string{{"--bogus"}, {"--dump", "--bogus"}} {
		var out, errs bytes.Buffer
		if code := run(append([]string{"recall"}, args...), &out, &errs); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "usage: agmux recall [--dump]") {
			t.Errorf("recall %q: exit %d, %q", args, code, errs.String())
		}
	}
	// the screen needs a terminal (a test has none); -r is the same command
	for _, args := range [][]string{{"recall"}, {"recall", "'x"}, {"-r"}, {"-r", "x", "y"}} {
		var out, errs bytes.Buffer
		if code := run(args, &out, &errs); code != 1 || out.Len() != 0 || !strings.Contains(errs.String(), "needs a terminal") || !strings.Contains(errs.String(), "--dump") {
			t.Errorf("%q: exit %d, %q", args, code, errs.String())
		}
	}
	var viaR, viaRecall, errs bytes.Buffer
	if run([]string{"-r", "--dump", "'left"}, &viaR, &errs) != 0 || run([]string{"recall", "--dump", "'left"}, &viaRecall, &errs) != 0 || viaR.String() != viaRecall.String() || viaR.Len() == 0 {
		t.Errorf("-r --dump: %q, recall --dump: %q", viaR.String(), viaRecall.String())
	}
}

// T76: resuming ends the process by becoming the agent, in the session's directory.
func TestExecResume(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	defer func(f func(string, []string, []string) error) { syscallExec = f }(syscallExec)
	var gotBin, gotCwd string
	var gotArgv, gotEnv []string
	t.Setenv("PWD", "/somewhere/else")
	syscallExec = func(b string, argv, env []string) error {
		gotBin, gotArgv, gotEnv = b, argv, env
		gotCwd, _ = os.Getwd()
		return nil
	}
	if err := execResume(dir, []string{"claude", "--resume", "abc"}); err != nil {
		t.Fatal(err)
	}
	if real, _ := filepath.EvalSymlinks(dir); gotBin != bin || strings.Join(gotArgv, " ") != "claude --resume abc" || gotCwd != real {
		t.Errorf("exec %q %q in %q, want %q in %q", gotBin, gotArgv, gotCwd, bin, real)
	}
	if err := execResume(dir, []string{"no-such-agent-xyz"}); err == nil {
		t.Error("a program that is not there is an error")
	}
	var pwds []string
	for _, kv := range gotEnv {
		if strings.HasPrefix(kv, "PWD=") {
			pwds = append(pwds, kv)
		}
	}
	if len(pwds) != 1 || pwds[0] != "PWD="+dir {
		t.Errorf("$PWD follows the directory: %q", pwds)
	}
	if err := execResume(filepath.Join(dir, "gone"), []string{"claude"}); err == nil {
		t.Error("a directory that is gone is an error")
	}
}
