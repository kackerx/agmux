package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// A background session has no summary: the name `claude --bg -n <name>` gave it is in its
// transcript, as {"type":"custom-title","customTitle":"<name>"} lines (written again on resume).

// titleState is what is known of one transcript's title: the last one found, and how far the
// file has been scanned, so that a transcript without one is not read again from the top.
type titleState struct {
	title   string
	path    string // the transcript it is about: another one starts over
	scanned int64  // the file up to here (a whole number of lines) has been looked at
}

// titleIn is the customTitle of the last custom-title line among lines, "" if none.
func titleIn(lines [][]byte) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if t := parseTitle(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func parseTitle(line []byte) string {
	if !bytes.Contains(line, []byte(`"custom-title"`)) {
		return ""
	}
	var e struct {
		Type        string `json:"type"`
		CustomTitle string `json:"customTitle"`
	}
	if json.Unmarshal(line, &e) == nil && e.Type == "custom-title" {
		return e.CustomTitle
	}
	return ""
}

// title is the session's custom title: the last one in the transcript. The tail the caller read
// anyway has the recent ones; the rest of the file is streamed (it reaches 16MB) from where the last
// scan ended, so each byte is read once, and what is found there is newer than what was known. A
// renamed session gets its new name this way, however much is written after the rename. "" when
// there is none or the file can't be read.
func (p *Poller) title(id, path string, tail [][]byte) string {
	if p.titles == nil {
		p.titles = map[string]titleState{}
	}
	st := p.titles[id]
	fi, err := os.Stat(path)
	if err != nil {
		return st.title
	}
	if st.path != path || fi.Size() < st.scanned { // not the file it was: from the top
		st = titleState{path: path}
	}
	if t := titleIn(tail); t != "" {
		st.title = t
		if st.scanned == 0 { // the tail is the end of the file: nothing before it needs a scan to find a title
			st.scanned = fi.Size()
		}
	}
	if fi.Size() > st.scanned {
		p.titleScans++
		t, end := scanTitle(path, st.scanned)
		p.titleBytes += end - st.scanned
		st.scanned = end
		if t != "" {
			st.title = t
		}
	}
	p.titles[id] = st
	return st.title
}

// scanTitle reads path from off to its last complete line and returns the last title in it and
// where it stopped.
func scanTitle(path string, off int64) (title string, end int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", off
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", off
	}
	r := bufio.NewReaderSize(f, 1<<20)
	end = off
	for {
		line, err := r.ReadBytes('\n')
		if err != nil { // the last line may still be being written: left for the next scan
			return title, end
		}
		end += int64(len(line))
		if t := parseTitle(line); t != "" {
			title = t
		}
	}
}
