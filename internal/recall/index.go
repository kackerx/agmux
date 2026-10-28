package recall

import (
	"cmp"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kackerx/agmux/internal/gitrepo"
)

// Version is the cache format: an entry of another version is read again from its transcript.
const Version = 2

// Entry is what is known of one transcript: its head, how far it has been read, and its sentences.
// It is the cache file <cache dir>/<source>-<transcript name>.json.
type Entry struct {
	V        int    `json:"v"`
	Source   string `json:"source"`
	Project  string `json:"project"`      // the main repository's name for a worktree, else the directory's
	Worktree string `json:"wt,omitempty"` // the worktree's own directory name, "" when the directory is the main checkout (or not in git)
	Head
	MTime   int64  `json:"mtime"` // of the transcript, ns since the epoch
	Size    int64  `json:"size"`
	Offset  int64  `json:"offset"` // the transcript is read up to here: a whole number of lines
	Users   []Line `json:"users"`  // your sentences, oldest first; Role is not stored
	Replies []Line `json:"replies"`
}

// Title is the session's name; with none, the first thing you said. Control characters (a name is
// whatever the user typed) are spaces: it ends up on one line of a screen.
func (e *Entry) Title() string {
	t := e.Name
	if t == "" && len(e.Users) > 0 {
		t = e.Users[0].Text
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, t)
}

// Index is the entries of every transcript of its sources, kept in a cache directory.
type Index struct {
	Dir      string                // the cache directory
	Sources  []Source              // at most one per Name
	Progress func(done, total int) // called after each transcript is looked at (on any goroutine); may be nil
	Workers  int                   // transcripts read at once; 0: a few per CPU

	mu      sync.Mutex
	entries []*Entry
	gen     int // counts changes: what is derived from entries is stale when it moved
	repos   map[string][2]string

	cached        *corpus // what Search matches, built from cachedEntries when gen was cachedGen
	cachedEntries []*Entry
	cachedGen     int
	cachedKey     string
}

// Reset removes the cache: the next Update reads everything again.
func (ix *Index) Reset() error { return os.RemoveAll(ix.Dir) }

// Entries are the sessions now known, newest transcript first. The slice is a copy; the entries
// are shared and must not be changed.
func (ix *Index) Entries() []*Entry {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return slices.Clone(ix.entries)
}

type job struct {
	src  Source
	path string
	fi   fs.FileInfo
}

// Update brings the index up to date with the transcripts: a cache entry whose transcript is
// unchanged is taken as it is, one that grew is read on from its offset, one that shrank is read
// again, one whose transcript is gone is deleted. The newest transcripts go first, and Entries
// holds each as soon as it is done, so a caller may show what is there while the rest is built.
func (ix *Index) Update() error {
	if err := os.MkdirAll(ix.Dir, 0o755); err != nil {
		return err
	}
	var jobs []job
	live := map[string]bool{} // cache files that still have a transcript
	for _, src := range ix.Sources {
		files, err := src.Files()
		if err != nil {
			return err
		}
		for _, p := range files {
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				jobs = append(jobs, job{src, p, fi})
				live[cacheName(src, p)] = true
			}
		}
	}
	slices.SortStableFunc(jobs, func(a, b job) int { return b.fi.ModTime().Compare(a.fi.ModTime()) })
	if old, _ := glob(ix.Dir, "*.json"); len(old) > 0 {
		for _, f := range old {
			if !live[filepath.Base(f)] {
				os.Remove(f)
			}
		}
	}
	// a temp file that a write left behind (a crash between the write and the rename); a young one
	// may be another agmux's, writing right now
	if tmps, _ := glob(ix.Dir, ".tmp-*"); len(tmps) > 0 {
		for _, f := range tmps {
			if fi, err := os.Stat(f); err == nil && time.Since(fi.ModTime()) > time.Minute {
				os.Remove(f)
			}
		}
	}
	ix.mu.Lock()
	ix.entries = nil
	ix.gen++
	ix.mu.Unlock()

	var (
		wg     sync.WaitGroup
		next   = make(chan job)
		done   int
		progMu sync.Mutex // done and Progress go together, so what Progress is told only goes up
		fail   error
	)
	for range cmp.Or(ix.Workers, max(runtime.NumCPU(), 2)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range next {
				e, err := ix.entry(j)
				ix.mu.Lock()
				if err != nil && fail == nil {
					fail = err
				}
				if e != nil { // in its place: newest first, now and while the rest is built
					i, _ := slices.BinarySearchFunc(ix.entries, e, func(have, e *Entry) int {
						if c := cmpInt(e.MTime, have.MTime); c != 0 {
							return c
						}
						return strings.Compare(have.Path, e.Path)
					})
					ix.entries = slices.Insert(ix.entries, i, e)
					ix.gen++
				}
				ix.mu.Unlock()
				progMu.Lock()
				done++
				if ix.Progress != nil {
					ix.Progress(done, len(jobs))
				}
				progMu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		next <- j
	}
	close(next)
	wg.Wait()
	return fail
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cacheName is the cache file of a transcript.
func cacheName(src Source, path string) string {
	return src.Name() + "-" + strings.TrimSuffix(filepath.Base(path), ".jsonl") + ".json"
}

// entry is the up to date entry of one transcript: the cached one, read on as far as the
// transcript has grown. A transcript that can't be read gives no entry and an error.
func (ix *Index) entry(j job) (*Entry, error) {
	cache := filepath.Join(ix.Dir, cacheName(j.src, j.path))
	e := load(cache)
	mtime, size := j.fi.ModTime().UnixNano(), j.fi.Size()
	if e != nil && e.Path == j.path && e.MTime == mtime && e.Size == size {
		return e, nil
	}
	if e == nil || e.Path != j.path || size < e.Offset {
		e = &Entry{V: Version, Source: j.src.Name()}
	}
	head, lines, end, err := j.src.Read(j.path, e.Offset)
	if err != nil { // what the cache knows is still worth finding by
		if e.Path != "" {
			return e, err
		}
		return nil, err
	}
	e.Head = e.Head.Merge(head)
	e.Path, e.MTime, e.Size, e.Offset = j.path, mtime, size, end
	e.add(lines)
	if e.Project == "" {
		e.Project, e.Worktree = ix.project(e.CWD)
	}
	return e, save(cache, e)
}

// add puts new sentences after the old ones: a sentence already there (by role) is not kept again, and
// of more than maxLines the first stays and the newest follow.
func (e *Entry) add(lines []Line) {
	for _, role := range []byte{'>', '<'} {
		list := &e.Users
		if role == '<' {
			list = &e.Replies
		}
		seen := map[string]bool{}
		for _, l := range *list {
			seen[l.Text] = true
		}
		for _, l := range lines {
			if l.Role == role && !seen[l.Text] {
				seen[l.Text] = true
				*list = append(*list, l)
			}
		}
		if len(*list) > maxLines {
			*list = append((*list)[:1], (*list)[len(*list)-(maxLines-1):]...)
		}
	}
}

// Lines are every sentence of the session, with the roles set: yours, then the agent's.
func (e *Entry) Lines() []Line {
	out := make([]Line, 0, len(e.Users)+len(e.Replies))
	for _, l := range e.Users {
		l.Role = '>'
		out = append(out, l)
	}
	for _, l := range e.Replies {
		l.Role = '<'
		out = append(out, l)
	}
	return out
}

// load reads a cache file; nil when it is not there, is not valid or is of another version.
func load(path string) *Entry {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var e Entry
	if json.Unmarshal(b, &e) != nil || e.V != Version {
		return nil
	}
	return &e
}

// save writes a cache file whole or not at all: another agmux opening at the same time never reads half of it.
func save(path string, e *Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// project is the name of the repository the directory belongs to and, for a linked worktree, the
// name of the worktree's own directory (see gitrepo). One question to git per directory.
func (ix *Index) project(cwd string) (project, worktree string) {
	if cwd == "" {
		return "", ""
	}
	ix.mu.Lock()
	r, ok := ix.repos[cwd]
	ix.mu.Unlock()
	if ok {
		return r[0], r[1]
	}
	g := gitrepo.Lookup(cwd)
	r[0], r[1] = g.Project, g.Worktree
	ix.mu.Lock()
	if ix.repos == nil {
		ix.repos = map[string][2]string{}
	}
	ix.repos[cwd] = r
	ix.mu.Unlock()
	return r[0], r[1]
}

// Resume is the command that opens e again, "" argv when its source is not one of the index's.
func (ix *Index) Resume(e *Entry) []string {
	for _, s := range ix.Sources {
		if s.Name() == e.Source {
			return s.Resume(e.Head)
		}
	}
	return nil
}

// ProjectOf is the project of a directory, as the sessions' are named (see Entry.Project).
func (ix *Index) ProjectOf(cwd string) string {
	p, _ := ix.project(cwd)
	return p
}

// Gen counts the changes of the entries: what is derived from them is stale when it moved.
func (ix *Index) Gen() int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.gen
}
