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
	scanned int64 // the file up to here (a whole number of lines) has been looked at
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

// title is the session's custom title: the last one in the tail the caller read anyway; else the
// one found before; else the rest of the transcript, scanned (streamed: it reaches 16MB) from where
// the last scan ended, so each byte is read once. "" when there is none or the file can't be read.
func (p *Poller) title(id, path string, tail [][]byte) string {
	if t := titleIn(tail); t != "" {
		return t
	}
	if p.titles == nil {
		p.titles = map[string]titleState{}
	}
	st := p.titles[id]
	if st.title != "" {
		return st.title
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() == st.scanned {
		return ""
	} else if fi.Size() < st.scanned { // not the file it was
		st.scanned = 0
	}
	p.titleScans++
	from := st.scanned
	st.title, st.scanned = scanTitle(path, from)
	p.titleBytes += st.scanned - from
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
