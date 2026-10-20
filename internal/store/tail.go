package store

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
)

// branchIn is the gitBranch of the last entry of the tail that has one.
func branchIn(lines [][]byte) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"gitBranch"`)) {
			continue
		}
		var e struct {
			GitBranch string `json:"gitBranch"`
		}
		if json.Unmarshal(lines[i], &e) == nil && e.GitBranch != "" {
			return e.GitBranch
		}
	}
	return ""
}

// pendingIn is the name of the last tool_use in the tail that has no tool_result yet: the tool the
// session is running, or waits for you to allow. "" when every one is answered.
func pendingIn(lines [][]byte) string {
	type use struct{ id, name string }
	var open []use
	for _, l := range lines {
		isUse, isResult := bytes.Contains(l, []byte(`"tool_use"`)), bytes.Contains(l, []byte(`"tool_result"`))
		if !isUse && !isResult {
			continue
		}
		var e struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		var blocks []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			ToolUseID string `json:"tool_use_id"`
		}
		if json.Unmarshal(l, &e) != nil || json.Unmarshal(e.Message.Content, &blocks) != nil { // content may be a string
			continue
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				open = append(open, use{b.ID, b.Name})
			case "tool_result":
				for i := range open {
					if open[i].id == b.ToolUseID {
						open = append(open[:i], open[i+1:]...)
						break
					}
				}
			}
		}
	}
	if len(open) == 0 {
		return ""
	}
	return open[len(open)-1].name
}

type repoInfo struct{ project, root string }

// repo is the project and the main repo root of a directory: git's common dir is the main repo's
// .git, also from a worktree; outside git the project is the directory's name and there is no
// root. Asked once per directory (the answer is kept until the board restarts).
func (p *Poller) repo(cwd string) (project, root string) {
	if cwd == "" {
		return "", ""
	}
	if r, ok := p.repos[cwd]; ok {
		return r.project, r.root
	}
	r := repoInfo{project: filepath.Base(cwd)}
	if out, err := exec.Command("git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		if common := strings.TrimSpace(string(out)); common != "" {
			r.root = filepath.Dir(common)
			r.project = filepath.Base(r.root)
		}
	}
	if p.repos == nil {
		p.repos = map[string]repoInfo{}
	}
	p.repos[cwd] = r
	return r.project, r.root
}
