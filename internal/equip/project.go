package equip

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

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

// exclude adds rel, a path in the Project, to the main checkout's
// .git/info/exclude unless git already ignores it. Outside git it does
// nothing.
func exclude(machine Machine, project Project, rel string) error {
	if project.gitDir == "" {
		return nil
	}
	// ponytail: any check-ignore failure reads as not ignored, which at worst
	// adds a line git did not need.
	_, err := machine.Git(project.Path, "check-ignore", "-q", rel)
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
