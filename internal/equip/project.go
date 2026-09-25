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
	out, err := m.Git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel")
	if err != nil {
		// ponytail: any git failure (no repo, no git binary) reads as outside
		// git; match git's exit status if a real repo ever fails here.
		return Project{Path: dir}, nil
	}
	common, root, _ := strings.Cut(strings.TrimSpace(out), "\n")
	// A worktree shares the main checkout's .git, whose parent is the main
	// checkout. A submodule's lives under .git/modules, so it keeps its own
	// top level.
	if main, ok := strings.CutSuffix(common, "/.git"); ok {
		root = main
	}
	// Read in dir: the main checkout may be on an unborn branch while this
	// worktree has history. Fails with no commits, which leaves no root commit.
	// Newest first, so the last root is the oldest: merging in an unrelated
	// history keeps the root commit.
	commits, _ := m.Git(dir, "rev-list", "--max-parents=0", "HEAD")
	commit := strings.TrimSpace(commits)
	if _, last, ok := strings.CutLast(commit, "\n"); ok {
		commit = last
	}
	return Project{Path: root, RootCommit: commit}, nil
}
