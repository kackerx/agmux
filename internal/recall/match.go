package recall

import (
	"slices"
	"strings"

	"github.com/kackerx/agmux/internal/fuzzy"
)

// Result is a session that matched, with what matched best.
type Result struct {
	*Entry
	Score   int  // of the best sentence, with the source, project, branch and name in front of it
	Hits    int  // sentences whose own text matched (all of them, for no query)
	Snippet Line // the best sentence whose text matched; none did: the best one; for no query, the last thing you said
}

// Filter narrows what is searched, before anything is matched. The zero Filter lets everything through.
type Filter struct {
	Sources map[string]bool // the sources to search; nil: all
	Project string          // only this project; "": all
}

func (f Filter) keeps(e *Entry) bool {
	return (f.Sources == nil || f.Sources[e.Source]) && (f.Project == "" || f.Project == e.Project)
}

func (f Filter) key() string {
	var srcs []string
	for s, on := range f.Sources {
		if on {
			srcs = append(srcs, s)
		}
	}
	slices.Sort(srcs)
	return strings.Join(srcs, ",") + "\x00" + f.Project + "\x00" + map[bool]string{true: "all", false: "some"}[f.Sources == nil]
}

// corpus is every sentence as the string it is matched as: "source project branch name sentence".
type corpus struct {
	items []string
	texts []string // per item: the sentence alone
	of    []ref    // per item: where it came from
	lines [][]Line // per entry: Entry.Lines()
}

type ref struct {
	entry int // into the entries it was built from
	line  int // into Entry.Lines()
}

func (ix *Index) corpus(f Filter) (*corpus, []*Entry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	key := f.key()
	if ix.cached != nil && ix.cachedGen == ix.gen && ix.cachedKey == key {
		return ix.cached, ix.cachedEntries
	}
	c := &corpus{}
	var es []*Entry
	for _, e := range ix.entries {
		if !f.keeps(e) {
			continue
		}
		i := len(es)
		es = append(es, e)
		prefix := e.Source + " " + e.Project + " " + e.Branch + " " + e.Title() + " "
		ls := e.Lines()
		c.lines = append(c.lines, ls)
		for j, l := range ls {
			c.items = append(c.items, prefix+l.Text)
			c.texts = append(c.texts, l.Text)
			c.of = append(c.of, ref{i, j})
		}
	}
	ix.cached, ix.cachedEntries, ix.cachedGen, ix.cachedKey = c, es, ix.gen, key
	return c, es
}

// Search is SearchIn with nothing filtered out.
func (ix *Index) Search(query string) []Result { return ix.SearchIn(query, Filter{}) }

// SearchIn matches query (fzf's syntax, see internal/fuzzy) against every sentence of the sessions f
// lets through. A session's score is its best sentence's, a sentence of yours winning a tie;
// sessions go best score first, then newest transcript. The hits are the sentences whose own text
// matches: a query that only names the project matches a session by it and has none. No query:
// every session that has a sentence, newest first.
func (ix *Index) SearchIn(query string, f Filter) []Result {
	c, es := ix.corpus(f)
	if strings.TrimSpace(query) == "" {
		var out []Result
		for i, e := range es {
			if ls := c.lines[i]; len(ls) > 0 {
				snip := ls[len(ls)-1]
				if len(e.Users) > 0 {
					snip = ls[len(e.Users)-1]
				}
				out = append(out, Result{Entry: e, Hits: len(ls), Snippet: snip})
			}
		}
		return out
	}
	ms := fuzzy.FilterParallel(query, c.items)
	bodies := make([]string, len(ms))
	for i, m := range ms {
		bodies[i] = c.texts[m.Index]
	}
	inBody := make([]bool, len(ms)) // does the sentence match without what is in front of it?
	for _, b := range fuzzy.FilterParallel(query, bodies) {
		inBody[b.Index] = true
	}
	best := map[int]*Result{}
	onBody := map[int]bool{} // the session's snippet is a sentence that matched by its text
	for i, m := range ms {   // best score first, a sentence of yours first among equals
		r := c.of[m.Index]
		res := best[r.entry]
		if res == nil {
			res = &Result{Entry: es[r.entry], Score: m.Score, Snippet: c.lines[r.entry][r.line]}
			best[r.entry] = res
		}
		if inBody[i] {
			res.Hits++
			if !onBody[r.entry] {
				res.Snippet, onBody[r.entry] = c.lines[r.entry][r.line], true
			}
		}
	}
	out := make([]Result, 0, len(best))
	for _, r := range best {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b Result) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.MTime != b.MTime {
			return cmpInt(b.MTime, a.MTime)
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// Chrono is the session's sentences in the order they were said (yours first when there are no times).
func (e *Entry) Chrono() []Line {
	out := e.Lines()
	if !slices.ContainsFunc(out, func(l Line) bool { return l.At.IsZero() }) {
		slices.SortStableFunc(out, func(a, b Line) int { return a.At.Compare(b.At) })
	}
	return out
}

// LineMatch is a sentence that matched, as its place in Chrono and the offsets (in runes) to highlight.
type LineMatch struct {
	I   int
	Pos []int
}

// Matches are the sentences of e whose text matches query, in the order they were said; none for no query.
func (e *Entry) Matches(query string) []LineMatch {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	ls := e.Chrono()
	texts := make([]string, len(ls))
	for i, l := range ls {
		texts[i] = l.Text
	}
	var out []LineMatch
	for _, m := range fuzzy.Filter(query, texts) {
		out = append(out, LineMatch{m.Index, m.Pos})
	}
	slices.SortFunc(out, func(a, b LineMatch) int { return a.I - b.I })
	return out
}
