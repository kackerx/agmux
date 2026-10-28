package recall

import (
	"slices"
	"strings"

	"github.com/kackerx/agmux/internal/fuzzy"
)

// Result is a session that matched, with what matched best.
type Result struct {
	*Entry
	Score   int  // of the best sentence
	Hits    int  // sentences that matched (all of them, for no query)
	Snippet Line // the best sentence; for no query, the last thing you said
}

// corpus is every sentence as the string it is matched as: "source project branch name sentence".
type corpus struct {
	items []string
	of    []ref    // per item: where it came from
	lines [][]Line // per entry: Entry.Lines()
}

type ref struct {
	entry int // into the entries it was built from
	line  int // into Entry.Lines()
}

func (ix *Index) corpus() (*corpus, []*Entry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.cached != nil && ix.cachedGen == ix.gen {
		return ix.cached, ix.cachedEntries
	}
	c := &corpus{}
	es := slices.Clone(ix.entries)
	for i, e := range es {
		prefix := e.Source + " " + e.Project + " " + e.Branch + " " + e.Title() + " "
		ls := e.Lines()
		c.lines = append(c.lines, ls)
		for j, l := range ls {
			c.items = append(c.items, prefix+l.Text)
			c.of = append(c.of, ref{i, j})
		}
	}
	ix.cached, ix.cachedEntries, ix.cachedGen = c, es, ix.gen
	return c, es
}

// Search matches query (fzf's syntax, see internal/fuzzy) against every sentence of every session.
// A session's score is its best sentence's, a sentence of yours winning a tie; sessions go best
// score first, then newest transcript. No query: every session that has a sentence, newest first.
func (ix *Index) Search(query string) []Result {
	c, es := ix.corpus()
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
	best := map[int]*Result{}
	for _, m := range fuzzy.Filter(query, c.items) {
		r := c.of[m.Index]
		e := es[r.entry]
		l := c.lines[r.entry][r.line]
		res := best[r.entry]
		if res == nil {
			res = &Result{Entry: e, Score: m.Score, Snippet: l}
			best[r.entry] = res
		} else if m.Score > res.Score || m.Score == res.Score && l.Role == '>' && res.Snippet.Role != '>' {
			res.Score, res.Snippet = m.Score, l
		}
		res.Hits++
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
