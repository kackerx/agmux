package recall

import (
	"bytes"
	"encoding/json"
)

// Pi reads pi's session files: <dir>/sessions/<directory>/<time>_<session id>.jsonl.
type Pi struct{ Dir string }

func (Pi) Name() string { return "pi" }

func (p Pi) Files() ([]string, error) {
	return glob(p.Dir, "sessions", "*", "*.jsonl")
}

func (Pi) Resume(h Head) []string { return []string{"pi", "--session", h.Path} }

type piLine struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	Name      string `json:"name"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

var (
	piMessage, piSession, piInfo = []byte(`"type":"message"`), []byte(`"type":"session"`), []byte(`"type":"session_info"`)
	piUser, piAssistant          = []byte(`"role":"user"`), []byte(`"role":"assistant"`)
)

// Read: the first line (type session) has the id and the directory, a session_info line the name
// (the last one counts), a message line of role user or assistant its text blocks. Tool results,
// tool calls and thinking are other roles or other blocks, and lines that hold none are not parsed.
func (Pi) Read(path string, from int64) (Head, []Line, int64, error) {
	h := Head{Path: path}
	var lines []Line
	end, err := eachLine(path, from, func(b []byte) {
		msg := bytes.Contains(b, piMessage) && (bytes.Contains(b, piUser) || bytes.Contains(b, piAssistant) && bytes.Contains(b, textBlock))
		if !msg && !bytes.Contains(b, piSession) && !bytes.Contains(b, piInfo) {
			return
		}
		var l piLine
		if json.Unmarshal(b, &l) != nil {
			return
		}
		switch l.Type {
		case "session":
			h.ID, h.CWD = l.ID, l.CWD
		case "session_info":
			h.Name = l.Name
		case "message":
			role := byte(0)
			switch l.Message.Role {
			case "user":
				role = '>'
			case "assistant":
				role = '<'
			}
			if role == 0 {
				return
			}
			for _, t := range texts(l.Message.Content) {
				if s, ok := Clean(t); ok {
					lines = append(lines, Line{Role: role, At: stamp(l.Timestamp), Text: s})
				}
			}
		}
	})
	return h, lines, end, err
}
