// Package board is the full-screen session board (`agmux board`).
package board

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kackerx/agmux/internal/store"
)

// Source is what the board reads: the store plus its last poll error.
type Source interface {
	store.Store
	Err() error
}

const frameEvery = 500 * time.Millisecond

type Model struct {
	src   Source
	now   func() time.Time
	w, h  int
	frame int
	focus string // session ID; follows the session across polls
	msg   string // one line above the footer: ccmux stderr, hints
}

func New(src Source) Model { return Model{src: src, now: time.Now} }

// Run polls p in the background and runs the board until the user quits.
func Run(p *store.Poller) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	_, err := tea.NewProgram(New(p), tea.WithAltScreen()).Run()
	return err
}

type frameMsg struct{}

func nextFrame() tea.Cmd { return tea.Tick(frameEvery, func(time.Time) tea.Msg { return frameMsg{} }) }

func (m Model) Init() tea.Cmd { return nextFrame() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case frameMsg:
		m.frame++
		return m, nextFrame()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// sessions returns the current sessions and makes sure focus points at one of them.
func (m *Model) sessions() []store.Session {
	ss := m.src.Sessions()
	gs := groups(ss)
	for _, s := range ss {
		if s.ID == m.focus {
			return ss
		}
	}
	m.focus = ""
	if len(gs) > 0 {
		m.focus = gs[0].sessions[0].ID
	}
	return ss
}

func (m Model) View() string {
	if m.w == 0 {
		return ""
	}
	ss := m.sessions()
	msg := m.msg
	if err := m.src.Err(); err != nil {
		msg = fg(cRed, " "+err.Error())
	}
	out, _, _ := view(ss, m.w, m.h, m.focus, m.frame, m.now(), msg)
	return out
}
