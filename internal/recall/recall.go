// Package recall finds a closed agent session by what was said in it: it reads the transcripts of
// the agents agmux knows (Claude Code, pi) into a small cache and matches sentences against it.
package recall

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Source is one agent's transcripts. New agents (codex, opencode) are one more Source.
type Source interface {
	Name() string                                              // "claude" | "pi"
	Files() ([]string, error)                                  // every transcript on this machine
	Read(path string, from int64) (Head, []Line, int64, error) // from byte offset from to the end: what the head says there (it may be only part of it), the new sentences, the offset to go on from
	Resume(h Head) []string                                    // the command that opens the session again
}

// Head is what a transcript says about the session. Read gives only what it found in the part it
// read; Merge puts the parts together.
type Head struct {
	ID     string `json:"id"`
	CWD    string `json:"cwd"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Agent  string `json:"agent"`
	Path   string `json:"path"`             // the transcript (Resume of pi needs it)
	Custom bool   `json:"custom,omitempty"` // Name was given by the user (Claude's custom-title), not made up (ai-title)
	Live   bool   `json:"-"`                // a running session: set by whoever knows what runs, never by a Source, never cached
}

// Merge is h with what a later part of the transcript says: the session id and the directory it
// started in stay as first seen (a session may cd), the name, branch and agent are the latest; a
// name the user gave stays over one that was made up.
func (h Head) Merge(later Head) Head {
	if h.ID == "" {
		h.ID = later.ID
	}
	if h.CWD == "" {
		h.CWD = later.CWD
	}
	if h.Path == "" {
		h.Path = later.Path
	}
	if later.Name != "" && (later.Custom || !h.Custom) {
		h.Name, h.Custom = later.Name, later.Custom
	}
	for _, f := range []struct{ to, from *string }{{&h.Branch, &later.Branch}, {&h.Agent, &later.Agent}} {
		if *f.from != "" {
			*f.to = *f.from
		}
	}
	return h
}

// Line is one sentence of the conversation.
type Line struct {
	Role byte      `json:"-"` // '>' you, '<' the agent (not cached: the cache keeps yours and the agent's apart)
	At   time.Time `json:"t"` // zero when the transcript has no time
	Text string    `json:"x"` // one line, at most maxText characters
}

const (
	maxText  = 200 // characters of a sentence that are kept
	minText  = 4   // shorter ones are noise ("ok", "y")
	maxLines = 300 // sentences kept per role in a session
)

// noise are the starts of what a transcript holds that nobody said: the harness's own messages.
// [Image #n] is not among them: the user said that.
var noise = []string{
	"This session is being continued", "<local-command", "<command-name", "<command-message", "<bash-",
	"<task-notification", "<system-reminder", "[Request interrupted",
}

// Clean is the sentence a text makes: white space folded to single spaces, cut to maxText characters;
// ok is false for what is noise (see noise) or too short to find anything by.
func Clean(text string) (s string, ok bool) {
	text = strings.TrimSpace(text)
	for _, n := range noise {
		if strings.HasPrefix(text, n) {
			return "", false
		}
	}
	s = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(s) < minText {
		return "", false
	}
	if r := []rune(s); len(r) > maxText {
		s = string(r[:maxText])
	}
	return s, true
}

// eachLine calls fn with every complete line of the file from byte offset from on (the slice is
// only good during the call) and returns the offset after the last one: a line the writer has not
// finished (no newline yet) is left for next time. Lines reach megabytes: a long one is put together.
func eachLine(path string, from int64, fn func(line []byte)) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return from, err
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return from, err
	}
	r := bufio.NewReaderSize(f, 256<<10)
	var long []byte
	end := from
	for {
		chunk, err := r.ReadSlice('\n')
		switch {
		case err == bufio.ErrBufferFull:
			long = append(long, chunk...)
			continue
		case err != nil: // end of file, or a read error: the unfinished line is not consumed
			return end, ignoreEOF(err)
		}
		line := chunk
		if long != nil {
			line, long = append(long, chunk...), nil
		}
		end += int64(len(line))
		fn(bytes.TrimRight(line, "\r\n"))
	}
}

func ignoreEOF(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

// glob is the pattern for the files of a directory: its own characters are not pattern syntax.
func glob(dir string, pattern ...string) ([]string, error) {
	return filepath.Glob(filepath.Join(strings.NewReplacer(`\`, `\\`, "[", `\[`, "*", `\*`, "?", `\?`).Replace(dir), filepath.Join(pattern...)))
}

// stamp reads an RFC 3339 time, zero when it is not one.
func stamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
