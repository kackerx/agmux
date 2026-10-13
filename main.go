// agmux: a TUI board for the coding agents ccmux tracks in tmux.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/board"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/statusline"
	"github.com/kackerx/agmux/internal/store"
)

const usage = `usage: agmux <command>

commands:
  board [--stay]
          full-screen session board (open it in a tmux popup; AGMUX_CLIENT: see README);
          --stay: in its own tmux session, enter / q only close the popup, Q ends the board
  show    print sessions one per line: id, name, group, status, model, context%, agent
  avatars print every avatar: image sets, then pixel sprites
  avatars import <sheet.png> --name <id> [--layout 30|19] [--force]
          cut a pi-agent-portrait sheet (6 × 5 cells; 19: 5 × 4) into the image set <id>
  statusline [-- <command>]
          Claude Code status line wrapper: saves the payload for the board, runs <command>
  config [--path]
          print the effective config (defaults + ~/.config/agmux/config.toml), or its path
  setup [--write]
          point ~/.claude/settings.json's statusLine.command at agmux statusline (dry run without --write)
`

func main() {
	if len(os.Args) >= 2 { // the commands that take arguments
		switch os.Args[1] {
		case "statusline":
			os.Exit(statusline.Run(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
		case "setup":
			os.Exit(statusline.Setup(os.Args[2:], os.Stdout, os.Stderr))
		case "board":
			os.Exit(boardCmd(os.Args[2:]))
		case "config":
			os.Exit(configCmd(os.Args[2:]))
		case "avatars":
			if len(os.Args) > 2 {
				os.Exit(avatarsCmd(os.Args[2:], os.Stdout, os.Stderr))
			}
		}
	}
	if len(os.Args) != 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "show":
		var p *store.Poller
		if p, err = store.NewPoller(); err == nil {
			err = show(os.Stdout, p)
		}
	case "avatars":
		avatars(os.Stdout, os.Stderr)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "agmux: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agmux:", err)
		os.Exit(1)
	}
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
		fmt.Fprintln(w, strings.Join([]string{s.ID, s.Name, s.GroupKey, s.Status, s.Model, pct, s.Agent}, "\t"))
	}
	return nil
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

func runBoard(stay bool) error {
	avatar.LoadSets()         // before the config: it may name image sets
	cfg, err := config.Load() // a broken config stops here, before the screen is taken over
	if err != nil {
		return err
	}
	p, err := store.NewPoller()
	if err != nil {
		return err
	}
	p.AvatarBind, p.AvatarDefault = cfg.AvatarBind, cfg.AvatarDefault
	img := board.DetectImages(cfg) // before the first poll: it decides what a hash may pick
	if err := p.Refresh(); err != nil {
		return err
	}
	return board.Run(p, cfg, img, stay)
}
