package equip

import (
	"path/filepath"
	"strings"
)

// locate finds the Project of dir: the main checkout's root in git, else dir.
// The path has symlinks resolved, as ~/.claude.json keys projects that way.
func locate(m Machine, dir string) (Project, error) {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Project{}, err
	}
	out, err := m.Git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		// ponytail: any git failure (no repo, no git binary) reads as outside
		// git; match git's exit status if a real repo ever fails here.
		return Project{Path: dir}, nil
	}
	line, _, _ := strings.Cut(out, "\n")
	root := strings.TrimPrefix(line, "worktree ")
	// Read in dir: the main checkout may be on an unborn branch while this
	// worktree has history. Fails with no commits, which leaves no root commit.
	commits, _ := m.Git(dir, "rev-list", "--max-parents=0", "HEAD")
	commit, _, _ := strings.Cut(commits, "\n")
	return Project{Path: root, RootCommit: commit}, nil
}
