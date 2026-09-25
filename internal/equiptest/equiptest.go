// Package equiptest builds throwaway machines for tests: a fake home, the XDG
// dirs and real git repos, all in t.TempDir().
package equiptest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equip"
)

// Machine is an equip.Machine on a temp dir, with helpers to build fixtures.
type Machine struct {
	equip.Machine
	Root string // the temp dir, symlinks resolved
	t    testing.TB
}

// New builds a machine whose home, XDG dirs and git config all live in a
// fresh temp dir. The user's own git config is never read.
func New(t testing.TB) *Machine {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	m := &Machine{Root: root, t: t, Machine: equip.Machine{
		Home:       home,
		ConfigHome: filepath.Join(home, ".config"),
		StateHome:  filepath.Join(home, ".local", "state"),
		CacheHome:  filepath.Join(home, ".cache"),
		CodexHome:  filepath.Join(home, ".codex"),
		WorkDir:    root,
		Git: equip.RunGit([]string{
			"HOME=" + home,
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=equip", "GIT_AUTHOR_EMAIL=equip@example.com",
			"GIT_COMMITTER_NAME=equip", "GIT_COMMITTER_EMAIL=equip@example.com",
		}),
	}}
	for _, d := range []string{m.ConfigHome, m.StateHome, m.CacheHome, m.CodexHome, filepath.Join(home, ".claude")} {
		m.Mkdir(d)
	}
	return m
}

// Mkdir creates dir and its parents and returns it.
func (m *Machine) Mkdir(dir string) string {
	m.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.t.Fatal(err)
	}
	return dir
}

// RunGit runs git in dir and fails the test on error. It returns the trimmed output.
func (m *Machine) RunGit(dir string, args ...string) string {
	m.t.Helper()
	out, err := m.Git(dir, args...)
	if err != nil {
		m.t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

// Repo creates a git repo with no commits at Root/name and returns its path.
func (m *Machine) Repo(name string) string {
	m.t.Helper()
	dir := m.Mkdir(filepath.Join(m.Root, name))
	m.RunGit(dir, "init", "-q", "-b", "main")
	return dir
}

// Commit makes an empty commit in repo and returns its hash.
func (m *Machine) Commit(repo string) string {
	m.t.Helper()
	m.RunGit(repo, "commit", "-q", "--allow-empty", "-m", "commit")
	return m.RunGit(repo, "rev-parse", "HEAD")
}

// Worktree adds a worktree of repo at Root/name and returns its path. The
// repo needs a commit.
func (m *Machine) Worktree(repo, name string) string {
	m.t.Helper()
	dir := filepath.Join(m.Root, name)
	m.RunGit(repo, "worktree", "add", "-q", "-b", name, dir)
	return dir
}

// Skill writes a skill named name into the skills dir and returns its dir.
func (m *Machine) Skill(skills, name string) string {
	m.t.Helper()
	dir := m.Mkdir(filepath.Join(skills, name))
	body := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		m.t.Fatal(err)
	}
	return dir
}

// ClaudeSkills is the Claude Code user skills dir.
func (m *Machine) ClaudeSkills() string { return filepath.Join(m.Home, ".claude", "skills") }
