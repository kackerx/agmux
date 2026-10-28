package recall

import (
	"fmt"
	"testing"
	"time"
)

// idx is an index with these entries (as Update would leave them, newest first).
func idx(es ...*Entry) *Index {
	ix := &Index{entries: es, gen: 1}
	return ix
}

func ent(id, source, project, branch, name string, mtime int, users, replies []string) *Entry {
	e := &Entry{V: Version, Source: source, Project: project, Head: Head{ID: id, Name: name, Branch: branch, Path: "/p/" + id}, MTime: int64(mtime)}
	for _, u := range users {
		e.Users = append(e.Users, Line{Text: u, At: time.Unix(1, 0)})
	}
	for _, r := range replies {
		e.Replies = append(e.Replies, Line{Text: r})
	}
	return e
}

func ids(rs []Result) string {
	s := ""
	for _, r := range rs {
		s += r.ID + " "
	}
	return s
}

func TestSearchHitsSnippetAndTies(t *testing.T) {
	a := ent("a", "claude", "shop", "main", "cart-fix", 100,
		[]string{"the coupon is applied twice", "unrelated words here"},
		[]string{"the coupon total is wrong"})
	b := ent("b", "claude", "shop", "main", "other", 300, []string{"nothing about it"}, []string{"a coupon appears once"})
	c := ent("c", "pi", "ledger", "", "audit", 200, []string{"audit export"}, nil)
	ix := idx(b, c, a)
	rs := ix.Search("'coupon")
	if ids(rs) != "b a " && ids(rs) != "a b " {
		t.Fatalf("%q: only sessions that match", ids(rs))
	}
	for _, r := range rs {
		switch r.ID {
		case "a":
			if r.Hits != 2 || r.Snippet.Role != '>' || r.Snippet.Text != "the coupon is applied twice" {
				t.Errorf("a: hits %d snippet %q %c (a tie goes to what you said)", r.Hits, r.Snippet.Text, r.Snippet.Role)
			}
		case "b":
			if r.Hits != 1 || r.Snippet.Role != '<' || r.Snippet.Text != "a coupon appears once" {
				t.Errorf("b: hits %d snippet %q %c", r.Hits, r.Snippet.Text, r.Snippet.Role)
			}
		}
	}
	// a better match goes first whatever the transcript's age
	ix = idx(b, a)
	if rs := ix.Search("'unrelated"); ids(rs) != "a " {
		t.Errorf("%q", ids(rs))
	}
}

func TestSearchTieGoesToTheNewestTranscript(t *testing.T) {
	old := ent("old", "claude", "p", "", "n", 100, []string{"same sentence here"}, nil)
	mid := ent("mid", "claude", "p", "", "n", 200, []string{"same sentence here"}, nil)
	newest := ent("new", "claude", "p", "", "n", 300, []string{"same sentence here"}, nil)
	if got := ids(idx(old, newest, mid).Search("'same")); got != "new mid old " {
		t.Errorf("%q", got)
	}
}

func TestSearchMatchesSourceProjectBranchAndName(t *testing.T) {
	a := ent("a", "claude", "shopcart", "feat/x", "cart-fix", 100, []string{"zzz unrelated"}, nil)
	b := ent("b", "pi", "ledger", "audit", "export", 90, []string{"zzz unrelated"}, nil)
	ix := idx(a, b)
	for q, want := range map[string]string{"'shopcart": "a ", "'feat/x": "a ", "'cart-fix": "a ", "'pi": "b ", "'ledger 'zzz": "b ", "'claude 'unrelated": "a ", "'audit": "b "} {
		if got := ids(ix.Search(q)); got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
	// a name from the first thing you said when there is none
	n := ent("n", "claude", "p", "", "", 80, []string{"plan the migration", "later"}, nil)
	if got := ids(idx(n).Search("'migration")); got != "n " {
		t.Errorf("%q", got)
	}
	// fzf syntax: all terms, exclusion
	c := ent("c", "claude", "p", "", "x", 70, []string{"coupon with tax", "coupon only"}, nil)
	rs := idx(c).Search("'coupon !tax")
	if len(rs) != 1 || rs[0].Hits != 1 || rs[0].Snippet.Text != "coupon only" {
		t.Errorf("%+v", rs)
	}
	if rs := idx(c).Search("zzzqq"); len(rs) != 0 {
		t.Errorf("no match: %v", ids(rs))
	}
}

// No query: every session that said anything, newest first, with the last thing you said.
func TestSearchWithoutQuery(t *testing.T) {
	a := ent("a", "claude", "p", "", "n", 300, []string{"first words", "last words of yours"}, []string{"the agent's last"})
	b := ent("b", "claude", "p", "", "n", 200, nil, []string{"only a reply"})
	e := ent("empty", "claude", "p", "", "n", 100, nil, nil)
	for _, q := range []string{"", "   "} {
		rs := idx(a, b, e).Search(q)
		if ids(rs) != "a b " {
			t.Fatalf("%q: %q (a session with no sentence is not listed)", q, ids(rs))
		}
		if rs[0].Snippet.Text != "last words of yours" || rs[0].Hits != 3 || rs[1].Snippet.Text != "only a reply" || rs[1].Snippet.Role != '<' {
			t.Errorf("%q: %+v / %+v", q, rs[0].Snippet, rs[1].Snippet)
		}
	}
}

// What Search matches is built once per change of the index, and again after it changed.
func TestSearchSeesAChangedIndex(t *testing.T) {
	a := ent("a", "claude", "p", "", "n", 100, []string{"alpha sentence"}, nil)
	ix := idx(a)
	if ids(ix.Search("'alpha")) != "a " || ids(ix.Search("'beta")) != "" {
		t.Fatal("first")
	}
	first := ix.cached
	ix.Search("'alpha")
	if ix.cached != first {
		t.Error("rebuilt for nothing")
	}
	ix.mu.Lock()
	ix.entries = append(ix.entries, ent("b", "claude", "p", "", "n", 90, []string{"beta sentence"}, nil))
	ix.gen++
	ix.mu.Unlock()
	if ids(ix.Search("'beta")) != "b " {
		t.Error("the new entry is not found")
	}
}

func BenchmarkSearch(b *testing.B) {
	var es []*Entry
	for i := range 400 {
		var us, rs []string
		for j := range 30 {
			us = append(us, fmt.Sprintf("session %d: please look at the failing build step %d in the deploy pipeline", i, j))
			rs = append(rs, fmt.Sprintf("the build %d step %d fails because the cache key changed", i, j))
		}
		es = append(es, ent(fmt.Sprint(i), "claude", "proj", "main", "name", i, us, rs))
	}
	ix := idx(es...)
	ix.Search("x")
	b.ResetTimer()
	for range b.N {
		ix.Search("deploy cache")
	}
}

// A better match goes first whatever the age of the transcript, and the best sentence of a session
// is the snippet whatever its role: only a tie goes to what you said.
func TestSearchScoreBeatsAgeAndRole(t *testing.T) {
	weak := ent("weak", "claude", "p", "", "n", 300, []string{"c o u p o n scattered through a long sentence"}, nil)
	strong := ent("strong", "claude", "p", "", "n", 100, []string{"nothing here"}, []string{"coupon"})
	rs := idx(weak, strong).Search("coupon")
	if ids(rs) != "strong weak " {
		t.Fatalf("%q", ids(rs))
	}
	if rs[0].Snippet.Text != "coupon" || rs[0].Snippet.Role != '<' || rs[0].Score <= rs[1].Score {
		t.Errorf("snippet %q %c, scores %d %d", rs[0].Snippet.Text, rs[0].Snippet.Role, rs[0].Score, rs[1].Score)
	}
	both := ent("both", "claude", "p", "", "n", 100, []string{"c o u p o n scattered through a long sentence"}, []string{"coupon"})
	if r := idx(both).Search("coupon")[0]; r.Snippet.Text != "coupon" || r.Hits != 2 {
		t.Errorf("the better sentence is the snippet even when yours comes first: %q hits %d", r.Snippet.Text, r.Hits)
	}
}

// The hits are the sentences that match by their own text; a query that only names the project or the
// branch finds the session and has none, and the snippet is the best sentence that matched by its text.
func TestSearchHitsCountOnlyTheSentenceText(t *testing.T) {
	a := ent("a", "claude", "shopcart", "main", "named", 100, []string{"first words", "second words"}, []string{"third words"})
	b := ent("b", "claude", "other", "main", "named", 90, []string{"about the shopcart total", "nothing"}, []string{"shopcart again"})
	rs := idx(a, b).Search("'shopcart")
	if len(rs) != 2 {
		t.Fatalf("both match: %v", ids(rs))
	}
	for _, r := range rs {
		switch r.ID {
		case "a":
			if r.Hits != 0 || r.Snippet.Text == "" {
				t.Errorf("a matches by its project only: hits %d snippet %q", r.Hits, r.Snippet.Text)
			}
		case "b":
			if r.Hits != 2 || r.Snippet.Text != "about the shopcart total" {
				t.Errorf("b: hits %d snippet %q (yours first among equals)", r.Hits, r.Snippet.Text)
			}
		}
	}
	// a sentence that matched only through the prefix is not the snippet when another matched by its text
	c := ent("c", "claude", "p", "", "payments", 80, []string{"hello there", "the payments table is empty"}, nil)
	if r := idx(c).Search("'payments")[0]; r.Hits != 1 || r.Snippet.Text != "the payments table is empty" {
		t.Errorf("hits %d snippet %q", r.Hits, r.Snippet.Text)
	}
}

func TestSearchFilters(t *testing.T) {
	a := ent("a", "claude", "shop", "", "n", 300, []string{"deploy it"}, nil)
	b := ent("b", "pi", "shop", "", "n", 200, []string{"deploy it"}, nil)
	c := ent("c", "claude", "ledger", "", "n", 100, []string{"deploy it"}, nil)
	ix := idx(a, b, c)
	for _, tc := range []struct {
		f    Filter
		want string
	}{
		{Filter{}, "a b c "}, {Filter{Sources: map[string]bool{"pi": true}}, "b "}, {Filter{Sources: map[string]bool{"claude": true, "pi": false}}, "a c "},
		{Filter{Sources: map[string]bool{}}, ""}, {Filter{Project: "shop"}, "a b "}, {Filter{Project: "shop", Sources: map[string]bool{"claude": true}}, "a "}, {Filter{Project: "nowhere"}, ""},
	} {
		if got := ids(ix.SearchIn("'deploy", tc.f)); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.f, got, tc.want)
		}
		if got := ids(ix.SearchIn("", tc.f)); got != tc.want { // no query: the same sessions, by age
			t.Errorf("%+v, no query: %q, want %q", tc.f, got, tc.want)
		}
	}
}

// What was searched for last does not stand in for what is asked now: the filters that differ only in
// the project, or in nil against empty sources, are different questions.
func TestSearchFilterChangesAreSeen(t *testing.T) {
	a := ent("a", "claude", "shop", "", "n", 300, []string{"deploy it"}, nil)
	c := ent("c", "claude", "ledger", "", "n", 100, []string{"deploy it"}, nil)
	ix := idx(a, c)
	steps := []struct {
		f    Filter
		want string
	}{
		{Filter{Project: "shop"}, "a "}, {Filter{Project: "ledger"}, "c "}, {Filter{}, "a c "}, {Filter{Sources: map[string]bool{}}, ""}, {Filter{}, "a c "},
		{Filter{Sources: map[string]bool{"claude": false}}, ""}, {Filter{Sources: map[string]bool{"claude": true}}, "a c "},
	}
	for i, s := range steps {
		if got := ids(ix.SearchIn("'deploy", s.f)); got != s.want {
			t.Errorf("step %d %+v: %q, want %q", i, s.f, got, s.want)
		}
	}
}

// The matches come in the order the sentences were said, not the order of their scores.
func TestMatchesAreInTheOrderSaid(t *testing.T) {
	e := &Entry{Users: []Line{{Text: "d e p l o y spread out", At: time.Unix(1, 0)}, {Text: "deploy exact", At: time.Unix(2, 0)}}}
	ms := e.Matches("deploy")
	if len(ms) != 2 || ms[0].I != 0 || ms[1].I != 1 {
		t.Errorf("%+v", ms)
	}
}

func TestChronoAndMatches(t *testing.T) {
	e := &Entry{Users: []Line{{Text: "u1 deploy", At: time.Unix(10, 0)}, {Text: "u2 later", At: time.Unix(30, 0)}}, Replies: []Line{{Text: "r1 deploy done", At: time.Unix(20, 0)}}}
	var got []string
	for _, l := range e.Chrono() {
		got = append(got, string(l.Role)+l.Text)
	}
	if want := "[>u1 deploy <r1 deploy done >u2 later]"; fmt.Sprint(got) != want {
		t.Errorf("%v, want %s", got, want)
	}
	ms := e.Matches("'deploy")
	if len(ms) != 2 || ms[0].I != 0 || ms[1].I != 1 || len(ms[0].Pos) != 6 || ms[0].Pos[0] != 3 {
		t.Errorf("%+v", ms)
	}
	if e.Matches("  ") != nil {
		t.Error("no query, no matches")
	}
	e.Users[1].At = time.Time{} // a sentence without a time: the order of the lists
	if l := e.Chrono(); l[0].Text != "u1 deploy" || l[1].Text != "u2 later" || l[2].Role != '<' {
		t.Errorf("no times: %+v", l)
	}
}

// Between sessions, a tie in score goes by who said the best sentence (you first), and only then by
// the age of the transcript.
func TestSearchTiesGoToWhatYouSaidThenToTheNewest(t *testing.T) {
	yours := ent("yours", "claude", "p", "", "n", 100, []string{"the same sentence"}, nil)
	agents := ent("agents", "claude", "p", "", "n", 300, nil, []string{"the same sentence"})
	newer := ent("newer", "claude", "p", "", "n", 200, []string{"the same sentence"}, nil)
	if got := ids(idx(agents, newer, yours).Search("'same")); got != "newer yours agents " {
		t.Errorf("%q: yours (newer first), then the agent's though it is the newest", got)
	}
}
