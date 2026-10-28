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
