package gitrepo

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestLookup(t *testing.T) {
	dir := t.TempDir()
	defer func(f func(string) ([]byte, error)) { Query = f }(Query)
	asks := 0
	for _, c := range []struct {
		name string
		out  string
		err  error
		want Info
	}{
		{"main checkout", "/m/.git\n/m/.git\n/m\n", nil, Info{"m", "/m", ""}},
		{"linked worktree", "/m/.git\n/m/.git/worktrees/a\n/w/a\n", nil, Info{"m", "/m", "a"}},
		{"submodule", "/p/.git/modules/s\n/p/.git/modules/s\n/p/s\n", nil, Info{"s", "/p/s", ""}},
		{"git dir elsewhere", "/g/real.git\n/g/real.git\n/w/sep\n", nil, Info{"sep", "/w/sep", ""}},
		{"not git", "", fmt.Errorf("not a git repository"), Info{filepath.Base(dir), "", ""}},
		{"an answer that is not three lines", "/m/.git\n", nil, Info{filepath.Base(dir), "", ""}},
	} {
		Query = func(string) ([]byte, error) { asks++; return []byte(c.out), c.err }
		if got := Lookup(dir); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	before := asks
	if got := Lookup(filepath.Join(dir, "gone")); got != (Info{Project: "gone"}) || asks != before {
		t.Errorf("a directory that is gone is not asked about: %+v, %d asks", got, asks-before)
	}
	if got := Lookup(""); got != (Info{}) {
		t.Errorf("no directory: %+v", got)
	}
}
