// agmux: a TUI board for the coding agents (Claude Code, pi) running in tmux.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/board"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/pi"
	"github.com/kackerx/agmux/internal/recall"
	"github.com/kackerx/agmux/internal/statusline"
	"github.com/kackerx/agmux/internal/store"
)

// Set by the release build (-ldflags "-X main.version=…", .goreleaser.yaml); a go install has none.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionLine is what --version prints: "agmux v0.1.0 (3f2a1b9, 2026-10-17T06:01:34Z)". What the
// build did not put in is taken from what Go recorded in info: the module's version (the tag of a
// go install ...@v0.1.0) and the vcs revision and time; dev, none and unknown are left without.
func versionLine(info *debug.BuildInfo) string {
	v, c, d := version, commit, date
	if info != nil {
		if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch {
			case s.Key == "vcs.revision" && c == "none":
				c = s.Value[:min(7, len(s.Value))]
			case s.Key == "vcs.time" && d == "unknown":
				d = s.Value
			}
		}
	}
	return fmt.Sprintf("agmux %s (%s, %s)", v, c, d)
}

const usage = `usage: agmux [--version] <command>

commands:
  board [--stay]
          full-screen session board (open it in a tmux popup; AGMUX_CLIENT: see README);
          --stay: in its own tmux session, enter / q only close the popup, Q ends the board
  show    print sessions one per line: id, name, group, status, model, context%, agent, bg (bg for a background session)
  avatars print every avatar: image sets, then pixel sprites
  avatars import <sheet.png> --name <id> [--layout 30|19] [--force]
          cut a pi-agent-portrait sheet (6 × 5 cells; 19: 5 × 4) into the image set <id>
  recall [--reindex] [--] [query...]      (also: agmux -r [query...])
          find a closed session by what was said in it, and resume it where it was
  recall --dump [--reindex] [--] [query...]
          the sessions that match, one per line, tab-separated: source, id, name, project,
          branch, mtime (UTC), hits, snippet
  statusline [-- <command>]
          Claude Code status line wrapper: saves the payload for the board, runs <command>
  config [--path]
          print the effective config (defaults + ~/.config/agmux/config.toml), or its path
  shell zsh|bash
          print a shell function to eval in your rc file: eval "$(agmux shell zsh)"; after a session
          resumed in place (agmux -r, the board's ?) your shell is in that session's directory
  setup [--write]
          point ~/.claude/settings.json's statusLine.command at agmux statusline, and install the pi
          extension that shows pi sessions on the board (dry run without --write)
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// readBuildInfo is debug.ReadBuildInfo; the tests put their own record in.
var readBuildInfo = debug.ReadBuildInfo

// run is the command line: args without the program name, the exit code back.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) >= 1 { // the commands that take arguments
		switch args[0] {
		case "statusline":
			return statusline.Run(args[1:], os.Stdin, stdout, stderr)
		case "setup":
			setupLang()
			code := statusline.Setup(args[1:], stdout, stderr)
			if code == 2 { // a usage error: nothing more to do
				return code
			}
			fmt.Fprintln(stdout)
			code = max(code, pi.Setup(slices.Contains(args[1:], "--write"), stdout, stderr))
			fmt.Fprintln(stdout, "\n"+i18n.T("setup.shell"))
			return code
		case "shell":
			return shellCmd(args[1:], stdout, stderr)
		case "board":
			return boardCmd(args[1:])
		case "config":
			return configCmd(args[1:])
		case "recall", "-r":
			return recallCmd(args[1:], stdout, stderr)
		case "avatars":
			if len(args) > 1 {
				return avatarsCmd(args[1:], stdout, stderr)
			}
		}
	}
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "show":
		var p *store.Poller
		if p, err = store.NewPoller(); err == nil {
			err = show(stdout, p)
		}
	case "avatars":
		avatars(stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "--version", "version":
		info, _ := readBuildInfo()
		fmt.Fprintln(stdout, versionLine(info))
		return 0
	default:
		fmt.Fprintf(stderr, "agmux: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "agmux:", err)
		return 1
	}
	return 0
}

// avatars prints the image sets (id, where from, frames per state) and then every sprite with its
// picture; sets that were skipped are said on errw.
func avatars(w, errw io.Writer) {
	avatar.LoadSets()
	for _, p := range avatar.Problems() {
		fmt.Fprintln(errw, "agmux:", p)
	}
	for _, s := range avatar.Sets {
		fmt.Fprintf(w, "%s  %s  %s\n", s.ID, s.Source, s.FrameCounts())
	}
	if len(avatar.Sets) > 0 {
		fmt.Fprintln(w)
	}
	for _, s := range avatar.Sprites {
		if _, shadowed := avatar.SetByID(s.ID); !shadowed {
			fmt.Fprintf(w, "%s  %s\n%s\n\n", s.ID, s.Name, avatar.Render(s, "claude", avatar.Normal, 0))
		}
	}
}

// boardCmd is `agmux board [--stay]`.
func boardCmd(args []string) int {
	if len(args) > 1 || len(args) == 1 && args[0] != "--stay" {
		fmt.Fprintf(os.Stderr, "agmux: board: unknown argument %q\nusage: agmux board [--stay]\n", strings.Join(args, " "))
		return 2
	}
	if err := runBoard(len(args) == 1); err != nil {
		fmt.Fprintln(os.Stderr, "agmux:", err)
		return 1
	}
	return 0
}

// avatarsCmd is `agmux avatars import <sheet.png> --name <id> [--layout 30|19] [--force]`.
func avatarsCmd(args []string, w, errw io.Writer) int {
	const usage = "usage: agmux avatars import <sheet.png> --name <id> [--layout 30|19] [--force]"
	if args[0] != "import" {
		fmt.Fprintf(errw, "agmux: avatars %q: unknown subcommand\n%s\n", args[0], usage)
		return 2
	}
	fs := flag.NewFlagSet("avatars import", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() {
		fmt.Fprintln(errw, usage)
		fs.PrintDefaults()
	}
	name := fs.String("name", "", "ID of the new set (its directory name)")
	layout := fs.String("layout", "30", "cells on the sheet: 30 (6 × 5) or 19 (5 × 4)")
	force := fs.Bool("force", false, "replace a set that exists")
	var sheet string
	if fs.Parse(args[1:]) != nil {
		return 2
	}
	if fs.NArg() > 0 { // flags may come before or after the sheet
		sheet = fs.Arg(0)
		if fs.Parse(fs.Args()[1:]) != nil {
			return 2
		}
	}
	if sheet == "" || *name == "" || fs.NArg() > 0 {
		fmt.Fprintln(errw, "agmux: avatars import needs the sheet and --name")
		fmt.Fprintln(errw, usage)
		return 2
	}
	dir, err := avatar.Import(sheet, *name, *layout, *force)
	if err != nil {
		fmt.Fprintln(errw, "agmux:", err)
		return 1
	}
	fmt.Fprintf(w, "imported %s into %s\nrun agmux avatars to list it; pick it on the board with a\n", sheet, dir)
	return 0
}

// show is a debug / test probe: tab-separated, one session per line.
func show(w io.Writer, p *store.Poller) error {
	if err := p.Refresh(); err != nil {
		return err
	}
	for _, s := range p.Sessions() {
		pct := "--"
		if s.ContextPct >= 0 {
			pct = fmt.Sprintf("%d%%", s.ContextPct)
		}
		bg := ""
		if s.Background {
			bg = "bg"
		}
		fmt.Fprintln(w, strings.Join([]string{s.ID, s.Name, s.GroupKey, s.Status, s.Model, pct, s.Agent, bg}, "\t"))
	}
	return nil
}

// recallCmd is `agmux recall --dump [--reindex] [query...]`: the sessions that match, one per line.
func recallCmd(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: agmux recall [--dump] [--reindex] [--] [query...]"
	var dump, reindex bool
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if args[0] == "--" { // what follows is the query, even if it starts with --
			args = args[1:]
			break
		}
		switch args[0] {
		case "--dump":
			dump = true
		case "--reindex":
			reindex = true
		default:
			fmt.Fprintf(stderr, "agmux: recall: unknown option %s\n%s\n", args[0], usage)
			return 2
		}
		args = args[1:]
	}
	ix, err := recall.Default()
	if err != nil {
		fmt.Fprintln(stderr, "agmux:", err)
		return 1
	}
	if reindex {
		if err := ix.Reset(); err != nil {
			fmt.Fprintln(stderr, "agmux:", err)
			return 1
		}
	}
	if !dump {
		if err := runRecall(ix, strings.Join(args, " ")); err != nil {
			fmt.Fprintln(stderr, "agmux:", err)
			return 1
		}
		return 0
	}
	failed := ix.Update() // some transcript that could not be read: said, and the rest shown, and the exit code says it
	if failed != nil {
		fmt.Fprintln(stderr, "agmux: recall:", failed)
	}
	one := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	for _, r := range ix.Search(strings.Join(args, " ")) {
		fmt.Fprintln(stdout, strings.Join([]string{r.Source, one.Replace(r.ID), one.Replace(r.Title()), one.Replace(r.Project), one.Replace(r.Branch),
			time.Unix(0, r.MTime).UTC().Format(time.RFC3339), strconv.Itoa(r.Hits), one.Replace(r.Snippet.Text)}, "\t"))
	}
	if failed != nil {
		return 1
	}
	return 0
}

// runRecall is the recall screen; when the user resumes a session it ends by becoming the agent, in
// the session's directory, in this very terminal.
func runRecall(ix *recall.Index, query string) error {
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return errors.New("recall needs a terminal; agmux recall --dump prints the sessions instead")
	}
	avatar.LoadSets() // config.toml may name image sets
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	useLang(cfg.Language)
	var live []store.Session
	if p, err := store.NewPoller(); err == nil && p.Refresh() == nil { // one look at what runs; no tmux or no Claude: nothing runs
		live = p.Sessions()
	}
	cwd, _ := os.Getwd()
	res, err := board.RunRecall(ix, cfg, live, ix.ProjectOf(cwd), query)
	if err != nil || res == nil {
		return err
	}
	return execResume(res.Dir, res.Argv)
}

// shellFunc is what `agmux shell zsh|bash` prints: an agmux function that gives agmux a file to write the
// directory of a session resumed in place into ($AGMUX_CWD_FILE, see execResume), and cds there when agmux
// is done: a child process can not change its parent's directory. zsh's always block removes the file
// even when ctrl+c ends the function; bash has no such block and leaves a file in that case.
const (
	zshFunc  = "agmux() { local f d rc; f=$(mktemp) || return; { AGMUX_CWD_FILE=$f command agmux \"$@\"; rc=$?; [[ -s $f ]] && d=$(<\"$f\") && cd -- \"$d\"; } always { rm -f -- \"$f\"; }; return $rc; }\n"
	bashFunc = `agmux() { local f d; f=$(mktemp) || return; AGMUX_CWD_FILE=$f command agmux "$@"; local rc=$?; [[ -s $f ]] && d=$(<"$f") && cd -- "$d"; rm -f -- "$f"; return $rc; }
`
)

// shellCmd is `agmux shell zsh|bash`.
func shellCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "zsh" && args[0] != "bash" {
		fmt.Fprintln(stderr, `usage: agmux shell zsh|bash   (in your rc file: eval "$(agmux shell zsh)"; fish is not supported)`)
		return 2
	}
	fmt.Fprint(stdout, map[string]string{"zsh": zshFunc, "bash": bashFunc}[args[0]])
	return 0
}

// syscallExec is syscall.Exec; the tests put their own in.
var syscallExec = syscall.Exec

// execResume becomes argv in dir: the terminal, the environment and the process stay, the program is replaced.
func execResume(dir string, argv []string) error {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	// eval "$(agmux shell zsh)": the shell's function waits for the directory in this file; the agent has no use for it
	if f := os.Getenv("AGMUX_CWD_FILE"); f != "" {
		if err := os.WriteFile(f, []byte(dir), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "agmux: %v: your shell will stay where it is (AGMUX_CWD_FILE is set by the function `agmux shell` prints)\n", err)
		}
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "PWD=") || strings.HasPrefix(kv, "AGMUX_CWD_FILE=")
	})
	err = syscallExec(bin, argv, append(env, "PWD="+dir)) // Chdir does not move $PWD, and tools believe it
	if f := os.Getenv("AGMUX_CWD_FILE"); f != "" && err != nil {
		os.Truncate(f, 0) // nothing was resumed: the shell stays where it is
	}
	return err
}

// configCmd is `agmux config [--path]`: the effective config (defaults + config.toml), or its path.
func configCmd(args []string) int {
	switch {
	case len(args) == 1 && args[0] == "--path":
		path, err := config.Path()
		if err != nil {
			fmt.Fprintln(os.Stderr, "agmux:", err)
			return 1
		}
		fmt.Println(path)
		return 0
	case len(args) != 0:
		fmt.Fprintln(os.Stderr, "usage: agmux config [--path]")
		return 2
	}
	avatar.LoadSets() // config.toml may name image sets
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agmux:", err)
		return 1
	}
	fmt.Print(c)
	return 0
}

// setupLang picks the language of `agmux setup`'s output: $AGMUX_LANG, else the config's [ui]
// language. A config that does not load is not setup's business: English then.
func setupLang() {
	avatar.LoadSets() // config.toml may name image sets
	lang := ""
	if c, err := config.Load(); err == nil {
		lang = c.Language
	}
	useLang(lang)
}

// useLang is the language in use for the rest of the run: $AGMUX_LANG, else the config's.
func useLang(cfgLang string) { i18n.Set(i18n.Resolve(cfgLang, os.Getenv("AGMUX_LANG"))) }

func runBoard(stay bool) error {
	avatar.LoadSets()         // before the config: it may name image sets
	cfg, err := config.Load() // a broken config stops here, before the screen is taken over
	if err != nil {
		return err
	}
	useLang(cfg.Language)
	p, err := store.NewPoller()
	if err != nil {
		return err
	}
	p.AvatarBind, p.AvatarDefault = cfg.AvatarBind, cfg.AvatarDefault
	img := board.DetectImages(cfg) // before the first poll: it decides what a hash may pick
	if err := p.Refresh(); err != nil {
		return err
	}
	res, err := board.Run(p, cfg, img, stay)
	if err != nil || res == nil {
		return err
	}
	return execResume(res.Dir, res.Argv) // a session resumed from the board's recall screen, in the pane the board was in
}
