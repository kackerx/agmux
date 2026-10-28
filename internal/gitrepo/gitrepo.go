// Package gitrepo names the repository a directory belongs to: the board groups sessions by it, and
// recall shows it as the project.
package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info is what git says of a directory.
type Info struct {
	Project  string // the main repository's name (a worktree is under the repository it was made from); outside git, the directory's own name
	Root     string // the main checkout's directory, "" outside git
	Worktree string // the linked worktree's own directory name, "" when the directory is not in one
}

// Query asks git for the common dir, the git dir and the top level of a directory, a line each; the tests replace it.
var Query = func(cwd string) ([]byte, error) {
	return exec.Command("git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir", "--git-dir", "--show-toplevel").Output()
}

// Lookup is the Info of cwd. A directory that is gone, or not in a repository, has its own name as the project.
func Lookup(cwd string) Info {
	info := Info{Project: filepath.Base(cwd)}
	if cwd == "" {
		return Info{}
	}
	if _, err := os.Stat(cwd); err != nil {
		return info
	}
	out, err := Query(cwd)
	if err != nil {
		return info
	}
	f := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(f) != 3 {
		return info
	}
	common, gitDir, top := f[0], f[1], f[2]
	info.Root = top // a submodule, a separate git dir: the checkout itself
	if filepath.Base(common) == ".git" {
		info.Root = filepath.Dir(common)
	}
	info.Project = filepath.Base(info.Root)
	if gitDir != common { // a linked worktree has a git dir of its own
		info.Worktree = filepath.Base(top)
	}
	return info
}
