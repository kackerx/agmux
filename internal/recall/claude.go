package recall

import (
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Claude reads Claude Code's transcripts: <dir>/projects/<project>/<session id>.jsonl.
type Claude struct{ Dir string }

// ClaudeDir is Claude Code's config directory: $CLAUDE_CONFIG_DIR, else ~/.claude.
func ClaudeDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".claude"), err
}

func (Claude) Name() string { return "claude" }

func (c Claude) Files() ([]string, error) {
	return glob(c.Dir, "projects", "*", "*.jsonl")
}

func (Claude) Resume(h Head) []string { return []string{"claude", "--resume", h.ID} }

type claudeLine struct {
	Type             string `json:"type"`
	IsMeta           bool   `json:"isMeta"`
	IsSidechain      bool   `json:"isSidechain"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Timestamp        string `json:"timestamp"`
	CWD              string `json:"cwd"`
	GitBranch        string `json:"gitBranch"`
	CustomTitle      string `json:"customTitle"`
	AITitle          string `json:"aiTitle"`
	AgentName        string `json:"agentName"`
	Message          struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

var (
	typeUser, typeAssistant = []byte(`"type":"user"`), []byte(`"type":"assistant"`)
	toolResult, textBlock   = []byte(`"type":"tool_result"`), []byte(`"type":"text"`)
	titleLines              = [][]byte{[]byte(`"custom-title"`), []byte(`"ai-title"`), []byte(`"agent-name"`)}
)

// Read picks the sentences out of the lines that hold any: a user line that is not a tool result,
// an assistant line with a text block. The rest (tool calls, results, thinking: nearly all of a
// transcript, and the biggest lines) is skipped by a substring test and never parsed.
// ponytail: a user message that quotes a tool_result block in its text is skipped with them.
func (Claude) Read(path string, from int64) (Head, []Line, int64, error) {
	h := Head{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path}
	var lines []Line
	end, err := eachLine(path, from, func(b []byte) {
		user := bytes.Contains(b, typeUser) && !bytes.Contains(b, toolResult)
		reply := !user && bytes.Contains(b, typeAssistant) && bytes.Contains(b, textBlock)
		if !user && !reply && !containsAny(b, titleLines) {
			return
		}
		var l claudeLine
		if json.Unmarshal(b, &l) != nil || l.IsSidechain {
			return
		}
		switch l.Type {
		case "custom-title":
			if l.CustomTitle != "" {
				h.Name, h.Custom = l.CustomTitle, true
			}
		case "ai-title":
			if l.AITitle != "" && !h.Custom {
				h.Name = l.AITitle
			}
		case "agent-name":
			h.Agent = cmp.Or(l.AgentName, h.Agent)
		case "user", "assistant":
			if h.CWD == "" {
				h.CWD = l.CWD
			}
			if l.GitBranch != "" && l.GitBranch != "HEAD" {
				h.Branch = l.GitBranch
			}
			if l.Type == "user" && (l.IsMeta || l.IsCompactSummary) {
				return
			}
			role := byte('>')
			if l.Type == "assistant" {
				role = '<'
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

func containsAny(b []byte, subs [][]byte) bool {
	for _, s := range subs {
		if bytes.Contains(b, s) {
			return true
		}
	}
	return false
}

// texts are the text of a message's content: the string itself, or the text blocks of a list.
func texts(content json.RawMessage) []string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return []string{s}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var out []string
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" {
				out = append(out, b.Text)
			}
		}
	}
	return out
}
