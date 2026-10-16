package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/store"
)

// ---- T59: a background session is marked ----

func bgSession(mod func(*store.Session)) store.Session {
	s := store.Session{ID: "b1", Name: "bg-probe", GroupKey: "/x", Status: "idle", Background: true, ContextPct: -1}
	if mod != nil {
		mod(&s)
	}
	return s
}

// bg sits at the right end of the branch line, after the agent if there is one; the branch gives
// way to it; an ordinary card has none.
func TestBackgroundTagOnCard(t *testing.T) {
	line := func(s store.Session, w int) string { return ansi.Strip(line2(s, w, nil)) }
	if got := line(bgSession(func(s *store.Session) { s.Branch = "main" }), 30); got != " main"+strings.Repeat(" ", 30-len(" main")-2)+"bg" && !strings.HasSuffix(got, " bg") {
		t.Errorf("%q", got)
	}
	if got := line(bgSession(func(s *store.Session) { s.Branch, s.Agent = "main", "orchestrator" }), 40); !strings.HasSuffix(got, "orchestrator bg") || ansi.StringWidth(got) != 40 {
		t.Errorf("with an agent: %q (%d wide)", got, ansi.StringWidth(got))
	}
	long := bgSession(func(s *store.Session) { s.Branch = "feature/a-very-long-branch-name-that-cannot-fit" })
	if got := line(long, 20); !strings.HasSuffix(got, " bg") || ansi.StringWidth(got) != 20 {
		t.Errorf("a branch that gives way: %q", got)
	}
	plain := bgSession(func(s *store.Session) { s.Background, s.Branch = false, "main" })
	if got := line(plain, 30); strings.Contains(got, "bg") {
		t.Errorf("an ordinary card: %q", got)
	}
	if !strings.HasSuffix(line2(long, 20, nil), fg(cMuted, "bg")) {
		t.Errorf("bg is muted: %q", line2(long, 20, nil))
	}
}

// The preview title says bg where the pane is (bg · %id once it has one); without a pane the
// message line says what Enter does; an ordinary session without a pane keeps the old message.
func TestBackgroundPreviewAndMessage(t *testing.T) {
	title := func(s store.Session) string {
		return ansi.Strip(previewLines(s, true, previewMsg{}, 100, 12, 0, false)[0])
	}
	if got := title(bgSession(nil)); !strings.Contains(got, "bg-probe · bg · ") || strings.Contains(got, "%") {
		t.Errorf("no pane: %q", got)
	}
	if got := title(bgSession(func(s *store.Session) { s.TmuxPane = "%7" })); !strings.Contains(got, "bg-probe · bg · %7 · ") {
		t.Errorf("with a pane: %q", got)
	}
	if got := title(bgSession(func(s *store.Session) { s.Background, s.TmuxPane = false, "%7" })); strings.Contains(got, "· bg ·") || !strings.Contains(got, "· %7 ·") {
		t.Errorf("ordinary: %q", got)
	}
	m := psized(staticSrc{bgSession(nil)}, 120, 30)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "background session · enter or i attaches it") || strings.Contains(v, "can't jump") {
		t.Errorf("message line:\n%s", v)
	}
	m = psized(staticSrc{bgSession(func(s *store.Session) { s.Background = false })}, 120, 30)
	if v := ansi.Strip(m.View()); !strings.Contains(v, noPane) || strings.Contains(v, "attaches it") {
		t.Errorf("a session that is not a background one: %s", v)
	}
}
