package fuzzy

import (
	"fmt"
	"reflect"
	"testing"
)

func idx(ms []Match) []int {
	out := []int{}
	for _, m := range ms {
		out = append(out, m.Index)
	}
	return out
}

func TestFilter(t *testing.T) {
	items := []string{"sqlmux-orchestrator main ~/proj/sqlmux", "agmux-dev main ~/proj/agmux", "agmux-test feat/x ~/proj/agmux", "mysql-dev mainline ~/w"}
	for _, c := range []struct {
		pattern string
		want    []int
	}{
		{"", []int{0, 1, 2, 3}},    // no pattern: all, in order
		{"orch", []int{0}},         // fuzzy
		{"^sql", []int{0}},         // prefix: not mysql
		{"!test", []int{0, 1, 3}},  // exclude
		{"'main ", []int{0, 1, 3}}, // exact (mainline too)
		{"agmux !test", []int{1}},  // AND
		{"MAIN", []int{}},          // smartcase: upper case is case-sensitive
		{"agmux$", []int{1, 2}},    // suffix
		{"^agmux-dev$", []int{}},   // equal: the whole item
		{"amx", []int{1, 2, 0}},    // ranked by score; equal scores keep their order
	} {
		if got := idx(Filter(c.pattern, items)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %v, want %v", c.pattern, got, c.want)
		}
	}
	if got := idx(Filter("ab", []string{"ab and more", "ab"})); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("equal scores keep the order, the shorter item doesn't go first: %v", got)
	}
	many := make([]string, 64)
	for i := range many {
		many[i] = fmt.Sprintf("ab%02d", i)
	}
	for i, m := range Filter("ab", many) {
		if m.Index != i {
			t.Fatalf("64 equal scores: #%d is item %d", i, m.Index)
		}
	}
	if m := Filter("orch", items)[0]; !reflect.DeepEqual(m.Pos, []int{7, 8, 9, 10}) {
		t.Errorf("positions are rune offsets: %v", m.Pos)
	}
	if m := Filter("dé", []string{"→ défaut"}); len(m) != 1 || !reflect.DeepEqual(m[0].Pos, []int{2, 3}) {
		t.Errorf("rune offsets past multi-byte runes: %v", m)
	}
}
