// Package equiptest builds throwaway machines for tests: a fake home, the XDG
// dirs and real git repos, all in t.TempDir().
package equiptest

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equip"
)

// The modes of the fixtures, as a user's own files usually have them.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// Machine is an equip.Machine on a temp dir, with helpers to build fixtures.
type Machine struct { //nolint:govet // fieldalignment wants the embedded Machine last, embeddedstructfieldcheck first
	equip.Machine

	t    testing.TB
	Root string // the temp dir, symlinks resolved
}

// New builds a machine whose home, XDG dirs and git config all live in a
// fresh temp dir. The user's own git config is never read, and git never sees
// an inherited GIT_* variable (a git hook sets GIT_DIR). The process
// environment is left alone, so tests using New can run in parallel.
func New(tb testing.TB) *Machine {
	tb.Helper()

	var env []string

	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}

	root, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}

	home := filepath.Join(root, "home")

	base := equip.Machine{
		Home:        home,
		ConfigHome:  filepath.Join(home, ".config"),
		StateHome:   filepath.Join(home, ".local", "state"),
		CacheHome:   filepath.Join(home, ".cache"),
		CodexHome:   filepath.Join(home, ".codex"),
		CodexSystem: filepath.Join(root, "etc", "codex"),
		WorkDir:     root,
		// Clipped, so a test that appends to it cannot write into git's.
		Env: slices.Clip(env),
		// None, so a test sees only the extensions it installs. A test of the
		// built-ins sets them.
		ClaudeBuiltins: nil,
		Git: equip.GitRunner(append(env,
			"HOME="+home,
			"GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=equip", "GIT_AUTHOR_EMAIL=equip@example.com",
			"GIT_COMMITTER_NAME=equip", "GIT_COMMITTER_EMAIL=equip@example.com",
		)),
	}
	machine := &Machine{Machine: base, Root: root, t: tb}

	for _, dir := range []string{
		machine.ConfigHome, machine.StateHome, machine.CacheHome, machine.CodexHome, filepath.Join(home, ".claude"),
	} {
		machine.Mkdir(dir)
	}

	return machine
}

// Mkdir creates dir and its parents and returns it.
func (m *Machine) Mkdir(dir string) string {
	m.t.Helper()

	err := os.MkdirAll(dir, dirMode)
	if err != nil {
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

// Commit makes an empty commit in repo and returns its hash. The message
// names repo, as equal empty commits made in one second share a hash, and
// two repos would share a root commit.
func (m *Machine) Commit(repo string) string {
	m.t.Helper()
	m.RunGit(repo, "commit", "-q", "--allow-empty", "-m", "commit in "+repo)

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

	err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), fileMode)
	if err != nil {
		m.t.Fatal(err)
	}

	return dir
}

// ClaudeSkills is the Claude Code user skills dir.
func (m *Machine) ClaudeSkills() string { return filepath.Join(m.Home, ".claude", "skills") }

// CodexSkills is the Codex user skills dir.
func (m *Machine) CodexSkills() string { return filepath.Join(m.Home, ".agents", "skills") }

// Plugin installs the Claude Code plugin key, "name@marketplace", in scope
// ("user", "project" or "local", with projectPath for the last two). It writes
// the plugin's manifest into the plugin cache and returns the plugin's dir
// there.
func (m *Machine) Plugin(key, scope, projectPath string) string {
	m.t.Helper()

	name, marketplace, _ := strings.Cut(key, "@")
	dir := m.Mkdir(filepath.Join(m.Home, ".claude", "plugins", "cache", marketplace, name, "1.0.0"))
	m.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name": "`+name+`", "description": "The `+name+` plugin."}`)

	path := filepath.Join(m.Home, ".claude", "plugins", "installed_plugins.json")
	installed := map[string]any{"plugins": map[string]any{}}

	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &installed)
	}

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.t.Fatal(err)
	}

	plugins, _ := installed["plugins"].(map[string]any)
	installs, _ := plugins[key].([]any)
	install := map[string]any{"scope": scope, "installPath": dir, "version": "1.0.0"}

	if projectPath != "" {
		install["projectPath"] = projectPath
	}

	plugins[key] = append(installs, install)

	data, err = json.Marshal(installed)
	if err != nil {
		m.t.Fatal(err)
	}

	m.WriteFile(path, string(data))

	return dir
}

// WriteFile writes content to path, creating its parents.
func (m *Machine) WriteFile(path, content string) {
	m.t.Helper()
	m.Mkdir(filepath.Dir(path))

	err := os.WriteFile(path, []byte(content), fileMode)
	if err != nil {
		m.t.Fatal(err)
	}
}

// Preset writes the preset named name, a TOML body, into the presets dir.
func (m *Machine) Preset(name, body string) {
	m.t.Helper()
	m.WriteFile(filepath.Join(m.ConfigHome, "equip", "presets", name+".toml"), body)
}

// CodexConfig is the Codex user config.
func (m *Machine) CodexConfig() string { return filepath.Join(m.CodexHome, "config.toml") }

// CodexPlugin writes the Codex plugin key, "name@marketplace", into the Codex
// plugin cache and returns its dir there. Codex installs it once a config
// names it.
func (m *Machine) CodexPlugin(key string) string {
	m.t.Helper()

	name, marketplace, _ := strings.Cut(key, "@")
	dir := m.Mkdir(filepath.Join(m.CodexHome, "plugins", "cache", marketplace, name, "1.0.0"))
	m.WriteFile(filepath.Join(dir, ".codex-plugin", "plugin.json"),
		`{"name": "`+name+`", "description": "The `+name+` plugin."}`)

	return dir
}
