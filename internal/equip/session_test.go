package equip_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func open(t *testing.T, m *equiptest.Machine, dir string) equip.View {
	t.Helper()
	s, err := equip.Open(m.Machine, dir)
	if err != nil {
		t.Fatal(err)
	}
	return s.View()
}

func TestProjectIsRepoRootFromSubdirectory(t *testing.T) {
	m := equiptest.New(t)
	repo := m.Repo("app")
	sub := m.Mkdir(filepath.Join(repo, "lib", "deep"))

	if got := open(t, m, sub).Project.Path; got != repo {
		t.Errorf("Project.Path = %q, want %q", got, repo)
	}
}

func TestHarnessIgnoresInheritedGitEnvironment(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim.git")
	t.Setenv("GIT_DIR", victim)
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Commit(repo)

	if got := open(t, m, repo).Project.Path; got != repo {
		t.Errorf("Project.Path = %q, want %q", got, repo)
	}
	if _, err := os.Stat(victim); err == nil {
		t.Errorf("git wrote to the inherited GIT_DIR %s", victim)
	}
}

func TestProjectIsSubmoduleCheckoutInsideSubmodule(t *testing.T) {
	m := equiptest.New(t)
	lib := m.Repo("lib")
	m.Commit(lib)
	super := m.Repo("super")
	m.RunGit(super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "mod")
	mod := filepath.Join(super, "mod")

	if got := open(t, m, mod).Project.Path; got != mod {
		t.Errorf("Project.Path = %q, want %q", got, mod)
	}
}

func TestProjectIsTheDirectoryOutsideGit(t *testing.T) {
	m := equiptest.New(t)
	dir := m.Mkdir(filepath.Join(m.Root, "scratch", "notes"))

	if got := open(t, m, dir).Project.Path; got != dir {
		t.Errorf("Project.Path = %q, want %q", got, dir)
	}
}

func TestProjectPathHasSymlinksResolved(t *testing.T) {
	m := equiptest.New(t)
	dir := m.Mkdir(filepath.Join(m.Root, "scratch"))
	link := filepath.Join(m.Root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	if got := open(t, m, link).Project.Path; got != dir {
		t.Errorf("Project.Path = %q, want %q", got, dir)
	}
}

func TestProjectReportsRootCommit(t *testing.T) {
	m := equiptest.New(t)
	repo := m.Repo("app")
	root := m.Commit(repo)
	m.Commit(repo)

	if got := open(t, m, repo).Project.RootCommit; got != root {
		t.Errorf("RootCommit = %q, want %q", got, root)
	}
}

func TestProjectReportsRootCommitWhenMainCheckoutHasNoCommits(t *testing.T) {
	m := equiptest.New(t)
	repo := m.Repo("app")
	root := m.Commit(repo)
	wt := m.Worktree(repo, "app-feature")
	m.RunGit(repo, "switch", "-q", "--orphan", "fresh")

	if got := open(t, m, wt).Project.RootCommit; got != root {
		t.Errorf("RootCommit = %q, want %q", got, root)
	}
}

func TestProjectHasNoRootCommitWithoutCommits(t *testing.T) {
	m := equiptest.New(t)
	repo := m.Repo("app")

	if got := open(t, m, repo).Project.RootCommit; got != "" {
		t.Errorf("RootCommit = %q, want none", got)
	}
}

func names(v equip.View) []string {
	var out []string
	for _, r := range v.Rows {
		out = append(out, r.Name)
	}
	return out
}

func TestViewListsClaudeCodeUserSkillsSortedByName(t *testing.T) {
	m := equiptest.New(t)
	for _, name := range []string{"zeta", "alpha", "mid"} {
		m.Skill(m.ClaudeSkills(), name)
	}

	got := names(open(t, m, m.Root))
	if want := []string{"alpha", "mid", "zeta"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewSkipsEntriesWithoutSkillFile(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "real")
	m.Skill(filepath.Join(m.ClaudeSkills(), "synced"), "from-claude-ai")
	m.Mkdir(filepath.Join(m.ClaudeSkills(), "empty"))
	if err := os.WriteFile(filepath.Join(m.ClaudeSkills(), "README.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := names(open(t, m, m.Root))
	if want := []string{"real"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewFollowsSymlinkedSkillDirectories(t *testing.T) {
	m := equiptest.New(t)
	src := m.Skill(filepath.Join(m.Root, "repos", "tools"), "source-name")
	m.Mkdir(m.ClaudeSkills())
	if err := os.Symlink(src, filepath.Join(m.ClaudeSkills(), "linked")); err != nil {
		t.Fatal(err)
	}

	got := names(open(t, m, m.Root))
	if want := []string{"linked"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestProjectIsMainCheckoutRootFromWorktree(t *testing.T) {
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Commit(repo)
	wt := m.Worktree(repo, "app-feature")
	sub := m.Mkdir(filepath.Join(wt, "lib"))

	if got := open(t, m, sub).Project.Path; got != repo {
		t.Errorf("Project.Path = %q, want %q", got, repo)
	}
}
