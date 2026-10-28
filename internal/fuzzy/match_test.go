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

// Equal scores keep the item order even among mixed scores (an all-equal input never tells a
// stable sort from an unstable one).
func TestTiesKeepOrderMixed(t *testing.T) {
	tiers := []string{"ab%03d", "xab%03d", "x-ab%03d", "a-b%03d", "a--b%03d"}
	var items []string
	for i := range 100 {
		items = append(items, fmt.Sprintf(tiers[(i*7+i/3)%len(tiers)], i))
	}
	ms := Filter("ab", items)
	if len(ms) != len(items) {
		t.Fatalf("%d of %d", len(ms), len(items))
	}
	for i := 1; i < len(ms); i++ {
		a, b := ms[i-1], ms[i]
		if a.Score < b.Score || a.Score == b.Score && a.Index > b.Index {
			t.Fatalf("at %d: score %d item %d, then score %d item %d", i, a.Score, a.Index, b.Score, b.Index)
		}
	}
}

// smartcase both ways: a lower-case term ignores case, one capital makes the term exact-case.
func TestSmartcase(t *testing.T) {
	items := []string{"Agmux-Dev main ~/x", "agmux-dev main ~/x"}
	if got := idx(Filter("agmux", items)); !reflect.DeepEqual(got, []int{0, 1}) && !reflect.DeepEqual(got, []int{1, 0}) {
		t.Errorf("lower-case term finds both: %v", got)
	}
	if got := idx(Filter("Agmux", items)); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("a capital: case-sensitive: %v", got)
	}
}

// Terms the syntax strips down to nothing don't filter; terms add up; positions are listed once;
// accents fold (é finds e) unless the term has some itself.
func TestEdgeTerms(t *testing.T) {
	items := []string{"my project main", "other"}
	for _, q := range []string{"!", "'", "^", "!'", "  ", "\t"} {
		if got := idx(Filter(q, items)); !reflect.DeepEqual(got, []int{0, 1}) {
			t.Errorf("%q keeps every item: %v", q, got)
		}
	}
	a, b, both := Filter("my", items)[0], Filter("pro", items)[0], Filter("my pro", items)[0]
	if both.Score != a.Score+b.Score {
		t.Errorf("scores add over terms: %d + %d != %d", a.Score, b.Score, both.Score)
	}
	if got := Filter("'my 'my", items)[0].Pos; !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("a position listed twice by two terms is listed once: %v", got)
	}
	if got := idx(Filter("cafe", []string{"café", "cafe"})); len(got) != 2 {
		t.Errorf("accents fold: %v", got)
	}
	if got := idx(Filter("café", []string{"café", "cafe"})); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("a term with an accent keeps it: %v", got)
	}
}

// FilterParallel gives the very same matches as Filter: scores, positions and order (ties in the
// items' order), for lists that are one piece and lists that are cut into several.
func TestFilterParallelEqualsFilter(t *testing.T) {
	words := []string{"coupon", "cart", "total", "deploy", "pipeline", "kitty", "tmux", "popup", "审计", "导出", "Build", "STEP", "claude", "pi"}
	for _, n := range []int{0, 1, 100, minChunk*2 - 1, minChunk * 2, 7000, 20001} {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf("%s %s %s %d", words[i%len(words)], words[(i*7+3)%len(words)], words[(i/3)%len(words)], i%13)
		}
		for _, p := range []string{"", "coupon", "cart total", "'tmux !popup", "^claude", "pi$", "审计", "BUILD", "zzzz", "dep pip", "!kitty"} {
			want, got := Filter(p, items), FilterParallel(p, items)
			if len(want) != len(got) || len(want) > 0 && !reflect.DeepEqual(want, got) {
				t.Fatalf("%d items, %q: %d matches, want %d (first differing: %v)", n, p, len(got), len(want), firstDiff(want, got))
			}
		}
	}
}

func firstDiff(a, b []Match) string {
	for i := range min(len(a), len(b)) {
		if !reflect.DeepEqual(a[i], b[i]) {
			return fmt.Sprintf("#%d %+v vs %+v", i, a[i], b[i])
		}
	}
	return "length"
}
