package equip_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func open(t *testing.T, machine *equiptest.Machine, dir string) equip.View {
	t.Helper()

	s, err := equip.Open(machine.Machine, dir)
	if err != nil {
		t.Fatal(err)
	}

	return s.View()
}

func TestProjectIsRepoRootFromSubdirectory(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	sub := machine.Mkdir(filepath.Join(repo, "lib", "deep"))

	if got := open(t, machine, sub).Project.Path; got != repo {
		t.Errorf("Project.Path = %q, want %q", got, repo)
	}
}

func TestHarnessIgnoresInheritedGitEnvironment(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim.git")
	t.Setenv("GIT_DIR", victim)
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Commit(repo)

	if got := open(t, machine, repo).Project.Path; got != repo {
		t.Errorf("Project.Path = %q, want %q", got, repo)
	}

	_, err := os.Stat(victim)
	if err == nil {
		t.Errorf("git wrote to the inherited GIT_DIR %s", victim)
	}
}

func TestProjectIsSubmoduleCheckoutInsideSubmodule(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	lib := machine.Repo("lib")
	machine.Commit(lib)
	super := machine.Repo("super")
	machine.RunGit(super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "mod")
	mod := filepath.Join(super, "mod")

	if got := open(t, machine, mod).Project.Path; got != mod {
		t.Errorf("Project.Path = %q, want %q", got, mod)
	}
}

func TestProjectIsTheDirectoryOutsideGit(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Mkdir(filepath.Join(machine.Root, "scratch", "notes"))

	if got := open(t, machine, dir).Project.Path; got != dir {
		t.Errorf("Project.Path = %q, want %q", got, dir)
	}
}

func TestProjectPathHasSymlinksResolved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Mkdir(filepath.Join(machine.Root, "scratch"))

	link := filepath.Join(machine.Root, "link")

	err := os.Symlink(dir, link)
	if err != nil {
		t.Fatal(err)
	}

	if got := open(t, machine, link).Project.Path; got != dir {
		t.Errorf("Project.Path = %q, want %q", got, dir)
	}
}

func TestProjectReportsRootCommit(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	root := machine.Commit(repo)
	machine.Commit(repo)

	if got := open(t, machine, repo).Project.RootCommit; got != root {
		t.Errorf("RootCommit = %q, want %q", got, root)
	}
}

func TestProjectKeepsOldestRootCommitAfterMergingUnrelatedHistory(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	oldest := machine.Commit(repo)
	machine.RunGit(repo, "switch", "-q", "--orphan", "other")
	machine.Commit(repo)
	machine.RunGit(repo, "switch", "-q", "main")
	machine.RunGit(repo, "merge", "-q", "--allow-unrelated-histories", "-m", "merge", "other")

	if got := open(t, machine, repo).Project.RootCommit; got != oldest {
		t.Errorf("RootCommit = %q, want the oldest root %q", got, oldest)
	}
}

func TestProjectReportsRootCommitWhenMainCheckoutHasNoCommits(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	root := machine.Commit(repo)
	wt := machine.Worktree(repo, "app-feature")
	machine.RunGit(repo, "switch", "-q", "--orphan", "fresh")

	if got := open(t, machine, wt).Project.RootCommit; got != root {
		t.Errorf("RootCommit = %q, want %q", got, root)
	}
}

func TestProjectHasNoRootCommitWithoutCommits(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")

	if got := open(t, machine, repo).Project.RootCommit; got != "" {
		t.Errorf("RootCommit = %q, want none", got)
	}
}

func names(v equip.View) []string {
	out := make([]string, 0, len(v.Rows))
	for _, r := range v.Rows {
		out = append(out, r.Name)
	}

	return out
}

func TestViewListsClaudeCodeUserSkillsSortedByName(t *testing.T) {
	t.Parallel()

	machine := equiptest.New(t)
	for _, name := range []string{"zeta", "alpha", "mid"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	got := names(open(t, machine, machine.Root))
	if want := []string{"alpha", "mid", "zeta"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewSkipsEntriesWithoutSkillFile(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "real")
	machine.Skill(filepath.Join(machine.ClaudeSkills(), "synced"), "from-claude-ai")
	machine.Mkdir(filepath.Join(machine.ClaudeSkills(), "empty"))

	err := os.WriteFile(filepath.Join(machine.ClaudeSkills(), "README.md"), nil, 0o644)
	if err != nil {
		t.Fatal(err)
	}

	got := names(open(t, machine, machine.Root))
	if want := []string{"real"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewFollowsSymlinkedSkillDirectories(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	src := machine.Skill(filepath.Join(machine.Root, "repos", "tools"), "source-name")
	machine.Mkdir(machine.ClaudeSkills())

	err := os.Symlink(src, filepath.Join(machine.ClaudeSkills(), "linked"))
	if err != nil {
		t.Fatal(err)
	}

	got := names(open(t, machine, machine.Root))
	if want := []string{"linked"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestProjectIsMainCheckoutRootFromWorktree(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Commit(repo)
	wt := machine.Worktree(repo, "app-feature")
	sub := machine.Mkdir(filepath.Join(wt, "lib"))

	if got := open(t, machine, sub).Project.Path; got != repo {
		t.Errorf("Project.Path = %q, want %q", got, repo)
	}
}
