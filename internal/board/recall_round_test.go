package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/kackerx/agmux/internal/config"
)

// T89: [layout] recall_border = "rounded" draws all three boxes with rounded corners (the filter panel
// always was); the default is thick. Same size either way.
func TestRecallRoundedBorder(t *testing.T) {
	defer useConfig(config.Default())
	w := newRWorld(t)
	w.fill()
	thick := rview(w.model())
	cfg, err := config.Parse("cfg", []byte("[layout]\nrecall_border = \"rounded\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	useConfig(cfg)
	m := w.model()
	round := rview(m)
	lines := strings.Split(round, "\n")
	if !strings.HasPrefix(lines[0], "╭──") || !strings.HasPrefix(lines[1], "│ / ") || !strings.HasPrefix(lines[2], "╰──") {
		t.Errorf("the query box:\n%s", strings.Join(lines[:3], "\n"))
	}
	if !strings.HasPrefix(lines[3], "╭─ 4/4 ─") || !strings.Contains(lines[3], "╮╭─ cart-fix ─") || !strings.HasSuffix(lines[3], "╮") {
		t.Errorf("the list and the preview: %q", lines[3])
	}
	if last := lines[len(lines)-3]; !strings.HasPrefix(last, "╰─") || strings.Count(last, "╯") != 2 {
		t.Errorf("bottom: %q", last)
	}
	if strings.ContainsAny(round, "┏┓┗┛┃━") {
		t.Errorf("a thick character is left:\n%s", round)
	}
	tl := strings.Split(thick, "\n") // the same cells: only the characters differ
	for i := range lines {
		if ansi.StringWidth(lines[i]) != ansi.StringWidth(tl[i]) {
			t.Errorf("line %d is %d wide, thick %d", i, ansi.StringWidth(lines[i]), ansi.StringWidth(tl[i]))
		}
	}
	useConfig(config.Default())
	if v := rview(w.model()); !strings.Contains(v, "┏━") || strings.Contains(v, "╭") {
		t.Errorf("back to thick:\n%s", v)
	}
}
