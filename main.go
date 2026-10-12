// agmux: a TUI board for the coding agents ccmux tracks in tmux.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/store"
)

const usage = `usage: agmux <command>

commands:
  board   full-screen session board (open it in a tmux popup)
  show    print sessions one per line: id, name, group, status, model, context%
`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "board":
		err = board()
	case "show":
		err = show(os.Stdout, store.NewPoller())
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

// board is the S1 skeleton; cards and layout land once the visual design is settled.
func board() error {
	p := store.NewPoller()
	if err := p.Refresh(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	_, err := tea.NewProgram(boardModel{p}, tea.WithAltScreen()).Run()
	return err
}

type boardModel struct{ p *store.Poller }

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(store.PollInterval, func(_ time.Time) tea.Msg { return tickMsg{} })
}

func (m boardModel) Init() tea.Cmd { return tick() }

func (m boardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tickMsg:
		return m, tick()
	}
	return m, nil
}

func (m boardModel) View() string {
	var b strings.Builder
	for _, s := range m.p.Sessions() {
		fmt.Fprintf(&b, "%-10s %s\n", s.Status, s.Name)
	}
	if err := m.p.Err(); err != nil {
		fmt.Fprintf(&b, "\n%v\n", err)
	}
	b.WriteString("\nq quit")
	return b.String()
}
