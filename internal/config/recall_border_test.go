package config

import (
	"fmt"
	"strings"
	"testing"
)

// T89: [layout] recall_border is "thick" (default) or "rounded"; anything else is one line naming it.
func TestRecallBorder(t *testing.T) {
	if Default().RecallBorder != "thick" {
		t.Errorf("default %q", Default().RecallBorder)
	}
	for _, b := range []string{"thick", "rounded"} {
		c, err := Parse("cfg", []byte(fmt.Sprintf("[layout]\nrecall_border = %q\n", b)))
		if err != nil || c.RecallBorder != b {
			t.Fatalf("%s: %v %q", b, err, c.RecallBorder)
		}
		if back, err := Parse("cfg", []byte(c.String())); err != nil || back.RecallBorder != b || !strings.Contains(c.String(), fmt.Sprintf("recall_border = %q ", b)) {
			t.Errorf("%s printed back: %v", b, err)
		}
	}
	for toml, want := range map[string]string{
		"[layout]\nrecall_border = \"square\"\n": `cfg: layout.recall_border: "square" is not a border (use "thick" or "rounded")`,
		"[layout]\nrecall_border = \"\"\n":       `layout.recall_border: "" is not a border`,
		"[layout]\nrecall_border = 3\n":          "cfg: toml:",
	} {
		_, err := Parse("cfg", []byte(toml))
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%q: %v, want one line with %q", toml, err, want)
		}
	}
}
