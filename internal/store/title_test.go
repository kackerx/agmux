package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kackerx/agmux/internal/ccmux"
)

const titleLine = `{"type":"custom-title","customTitle":"%s"}` + "\n"

func filler(n int) string {
	return strings.Repeat(`{"type":"user","message":"`+strings.Repeat("x", 90)+`"}`+"\n", n/100)
}

func titled(t *testing.T, content string) (*Poller, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(log, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := []ccmux.Session{{ID: "a", Project: "proj", LogPath: log}}
	return &Poller{Show: func() ([]ccmux.Session, error) { return raw, nil }, StateDir: t.TempDir()}, log
}

func nameOf(t *testing.T, p *Poller) string {
	t.Helper()
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	return p.Sessions()[0].Name
}

// A session without a summary is called what `claude --bg -n` named it: the last custom-title in
// the transcript, the project only when there is none.
func TestTitleFromTail(t *testing.T) {
	p, _ := titled(t, filler(1000)+strings.Replace(titleLine, "%s", "old-name", 1)+filler(1000)+strings.Replace(titleLine, "%s", "bg-probe", 1)+filler(1000))
	if got := nameOf(t, p); got != "bg-probe" {
		t.Errorf("name %q, want the last title", got)
	}
	if p.titleScans != 0 {
		t.Errorf("the tail had it: %d scans", p.titleScans)
	}
	p, _ = titled(t, filler(2000))
	if got := nameOf(t, p); got != "proj" {
		t.Errorf("no title: name %q, want the project", got)
	}
	p, _ = titled(t, `{"type":"custom-title","customTitle":""}`+"\n")
	if got := nameOf(t, p); got != "proj" {
		t.Errorf("an empty title: %q", got)
	}
}

// Past the tail: the file is scanned once, and what was scanned is not read again.
func TestTitleScanOnce(t *testing.T) {
	p, log := titled(t, strings.Replace(titleLine, "%s", "far-up", 1)+filler(200<<10)) // 200KB below it
	if got := nameOf(t, p); got != "far-up" || p.titleScans != 1 {
		t.Errorf("name %q after %d scans", got, p.titleScans)
	}
	for range 3 {
		if got := nameOf(t, p); got != "far-up" || p.titleScans != 1 {
			t.Fatalf("a later poll: name %q, %d scans", got, p.titleScans)
		}
	}
	// no title yet: each poll looks only at what was added since the last
	p, log = titled(t, filler(200<<10))
	if got := nameOf(t, p); got != "proj" || p.titleScans != 1 {
		t.Fatalf("name %q after %d scans", got, p.titleScans)
	}
	if nameOf(t, p); p.titleScans != 1 {
		t.Errorf("the file did not change: %d scans", p.titleScans)
	}
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(strings.Replace(titleLine, "%s", "named-later", 1) + filler(200<<10)) // out of the tail again
	f.Close()
	before := p.titleBytes
	if got := nameOf(t, p); got != "named-later" || p.titleScans != 2 {
		t.Errorf("a title written after the first scan: name %q, %d scans", got, p.titleScans)
	}
	if fi, _ := os.Stat(log); p.titleBytes-before > fi.Size()/2+1000 {
		t.Errorf("the second scan read %d of %d bytes: only what was added is new", p.titleBytes-before, fi.Size())
	}
	if nameOf(t, p); p.titleScans != 2 {
		t.Errorf("found: not scanned again: %d", p.titleScans)
	}
}

// A line still being written is not taken as scanned; a summary, or no transcript, never looks.
func TestTitleEdges(t *testing.T) {
	p, log := titled(t, filler(200<<10)+`{"type":"custom-title","customTitle":"half`)
	if got := nameOf(t, p); got != "proj" {
		t.Fatalf("half a line: %q", got)
	}
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("-done\"}\n" + filler(200<<10))
	f.Close()
	if got := nameOf(t, p); got != "half-done" {
		t.Errorf("the line completed: %q", got)
	}
	// a summary is the name, and the transcript is not looked at; without a transcript, the project
	p, log = titled(t, strings.Replace(titleLine, "%s", "from-jsonl", 1)+filler(200<<10))
	raw := []ccmux.Session{{ID: "a", Summary: "the summary", Project: "proj", LogPath: log}, {ID: "b", Project: "other"}, {ID: "c", Project: "gone", LogPath: log + ".missing"}}
	p.Show = func() ([]ccmux.Session, error) { return raw, nil }
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	ss := p.Sessions()
	if ss[0].Name != "the summary" || ss[1].Name != "other" || ss[2].Name != "gone" || p.titleScans != 0 {
		t.Errorf("names %q %q %q, %d scans", ss[0].Name, ss[1].Name, ss[2].Name, p.titleScans)
	}
}

// Background is trackingMode "background", nothing else.
func TestBackgroundFlag(t *testing.T) {
	raw := []ccmux.Session{{ID: "a", TrackingMode: "background", LastPrompt: "x"}, {ID: "b", TrackingMode: "native"}, {ID: "c"}}
	ss := poller(t, raw).Sessions()
	if !ss[0].Background || ss[1].Background || ss[2].Background {
		t.Errorf("%v %v %v", ss[0].Background, ss[1].Background, ss[2].Background)
	}
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

// A session renamed later is called by the new name, whether the rename is in the tail or has
// scrolled out of it since.
func TestTitleRename(t *testing.T) {
	p, log := titled(t, strings.Replace(titleLine, "%s", "A", 1)+filler(200<<10))
	if got := nameOf(t, p); got != "A" {
		t.Fatalf("name %q", got)
	}
	appendTo(t, log, strings.Replace(titleLine, "%s", "B", 1))
	if got := nameOf(t, p); got != "B" {
		t.Errorf("renamed, in the tail: %q", got)
	}
	appendTo(t, log, filler(100<<10)) // B is out of the tail now
	if got := nameOf(t, p); got != "B" {
		t.Errorf("the rename scrolled out of the tail: %q", got)
	}
	// and a rename that went out of the tail between two polls
	appendTo(t, log, strings.Replace(titleLine, "%s", "C", 1)+filler(100<<10))
	if got := nameOf(t, p); got != "C" {
		t.Errorf("renamed and buried before the next poll: %q", got)
	}
}

// A transcript that is shorter than what was scanned is another file: it is read from the top; the
// whole file is read for the last title, not the first; and so is another path for the same session.
func TestTitleOtherFile(t *testing.T) {
	p, log := titled(t, strings.Replace(titleLine, "%s", "old", 1)+filler(200<<10))
	if got := nameOf(t, p); got != "old" {
		t.Fatalf("name %q", got)
	}
	os.WriteFile(log, []byte(strings.Replace(titleLine, "%s", "new", 1)+filler(100<<10)), 0o644) // shorter, the title out of the tail
	if got := nameOf(t, p); got != "new" {
		t.Errorf("a shorter file: %q", got)
	}
	os.WriteFile(log, []byte(filler(50<<10)), 0o644) // and now one with none
	if got := nameOf(t, p); got != "proj" {
		t.Errorf("the title of the file that was: %q", got)
	}
	p, _ = titled(t, strings.Replace(titleLine, "%s", "first", 1)+filler(100<<10)+strings.Replace(titleLine, "%s", "last", 1)+filler(100<<10))
	if got := nameOf(t, p); got != "last" {
		t.Errorf("a whole-file scan takes the last: %q", got)
	}
	// the same session in another transcript: nothing of the first one's scan is taken for it
	p, _ = titled(t, filler(200<<10))
	if got := nameOf(t, p); got != "proj" {
		t.Fatalf("name %q", got)
	}
	other := filepath.Join(t.TempDir(), "b.jsonl")
	os.WriteFile(other, []byte(strings.Replace(titleLine, "%s", "elsewhere", 1)+filler(300<<10)), 0o644) // longer than the first
	p.Show = func() ([]ccmux.Session, error) {
		return []ccmux.Session{{ID: "a", Project: "proj", LogPath: other}}, nil
	}
	if got := nameOf(t, p); got != "elsewhere" {
		t.Errorf("another path: %q", got)
	}
}
