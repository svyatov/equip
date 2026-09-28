package equip

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Project is the git repo equip runs in, taken at the main checkout's root,
// or the directory itself outside git.
type Project struct {
	Path       string // symlinks resolved
	RootCommit string // empty outside git or with no commits
	gitDir     string // the main checkout's .git; empty outside git
	checkout   string // the root of the checkout equip runs in: a worktree's own; Path outside git
}

// locate finds the Project of dir, a path with symlinks resolved: the main
// checkout's root in git, else dir.
func locate(machine Machine, dir string) Project {
	out, err := machine.Git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel")
	if err != nil {
		// ponytail: any git failure (no repo, no git binary) reads as outside
		// git; match git's exit status if a real repo ever fails here.
		return Project{Path: dir, RootCommit: "", gitDir: "", checkout: dir}
	}

	common, checkout, _ := strings.Cut(strings.TrimSpace(out), "\n")
	root := checkout
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
	commits, _ := machine.Git(dir, "rev-list", "--max-parents=0", "HEAD")

	commit := strings.TrimSpace(commits)
	if _, last, ok := strings.CutLast(commit, "\n"); ok {
		commit = last
	}

	return Project{Path: root, RootCommit: commit, gitDir: common, checkout: checkout}
}

// tracked reports whether git tracks rel, a path under root, a checkout of the
// Project, or the file the symlinks on its way lead to. Outside git nothing is
// tracked.
func tracked(machine Machine, project Project, root, rel string) bool {
	if project.gitDir == "" {
		return false
	}
	// git does not follow symlinks, so ask about the file they lead to. A file
	// outside the repo reads as untracked.
	path := filepath.Join(root, rel)

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}

	_, err = machine.Git(root, "ls-files", "--error-unmatch", "--", path)

	return err == nil
}

// trackedReason is why equip does not write rel, a config under root, a
// checkout of the Project, when git tracks it. It is empty when git does not.
func trackedReason(machine Machine, project Project, root, rel string) string {
	// A tracked file belongs to everyone who clones the repo.
	if tracked(machine, project, root, rel) {
		return rel + " is tracked by git"
	}

	return ""
}

// exclude adds rel, a path under root, a checkout of the Project, to the main
// checkout's .git/info/exclude unless git already ignores it there. Every
// worktree reads that file. Outside git it does nothing.
func exclude(machine Machine, project Project, root, rel string) error {
	if project.gitDir == "" {
		return nil
	}
	// ponytail: any check-ignore failure reads as not ignored, which at worst
	// adds a line git did not need.
	_, err := machine.Git(root, "check-ignore", "-q", rel)
	if err == nil {
		return nil
	}

	path := filepath.Join(project.gitDir, "info", "exclude")

	data, err := os.ReadFile(path) //nolint:gosec // git names the path
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read git exclude: %w", err)
	}

	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}

	return writeFile(path, append(data, "/"+rel+"\n"...))
}
