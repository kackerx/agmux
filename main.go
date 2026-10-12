// agmux: a TUI board for the coding agents ccmux tracks in tmux.
package main

import (
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
  board   full-screen session board (open it in a tmux popup; AGMUX_CLIENT: see README)
  show    print sessions one per line: id, name, group, status, model, context%
  avatars print every avatar sprite
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
		case "config":
			os.Exit(configCmd(os.Args[2:]))
		}
	}
	if len(os.Args) != 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "board":
		err = runBoard()
	case "show":
		var p *store.Poller
		if p, err = store.NewPoller(); err == nil {
			err = show(os.Stdout, p)
		}
	case "avatars":
		for _, s := range avatar.Sprites {
			fmt.Printf("%s  %s\n%s\n\n", s.ID, s.Name, avatar.Render(s, "claude", avatar.Normal, 0))
		}
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
		fmt.Fprintln(w, strings.Join([]string{s.ID, s.Name, s.GroupKey, s.Status, s.Model, pct}, "\t"))
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
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agmux:", err)
		return 1
	}
	fmt.Print(c)
	return 0
}

func runBoard() error {
	cfg, err := config.Load() // a broken config stops here, before the screen is taken over
	if err != nil {
		return err
	}
	p, err := store.NewPoller()
	if err != nil {
		return err
	}
	if err := p.Refresh(); err != nil {
		return err
	}
	return board.Run(p, cfg)
}
