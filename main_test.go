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
	"strings"
	"testing"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/ccmux"
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
	// the ten built-in sets, with the frames of each state that are not alternate takes (_v2…)
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

// `agmux show` ends each line with the session's agent definition (empty if it has none).
func TestShowAgentColumn(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s.jsonl")
	os.WriteFile(log, []byte(`{"type":"agent-setting","agentSetting":"orchestrator"}`+"\n"), 0o644)
	p := &store.Poller{StateDir: dir, Show: func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a", Summary: "one", LogPath: log}, {ID: "b", Summary: "two"}}, nil
	}}
	var out bytes.Buffer
	if err := show(&out, p); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "\torchestrator") || !strings.HasSuffix(lines[1], "\t") || strings.Count(lines[1], "\t") != 6 {
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
