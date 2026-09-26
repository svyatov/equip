// Package equip is the core of equip: it finds the Project, discovers its
// extensions and exposes them through a Session.
package equip

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Machine is everything equip reads from the environment. No other part of
// equip reads the environment.
type Machine struct {
	Git GitFunc
	// ClaudeBuiltins are the MCP servers built into Claude Code, each with its
	// default state.
	ClaudeBuiltins map[string]State

	Home        string
	ConfigHome  string // $XDG_CONFIG_HOME
	StateHome   string // $XDG_STATE_HOME
	CacheHome   string // $XDG_CACHE_HOME
	CodexHome   string // $CODEX_HOME
	CodexSystem string // /etc/codex
	WorkDir     string
	// Env is the environment, as os.Environ gives it, that an MCP server
	// equip probes starts with.
	Env []string
}

// GitFunc runs git with args in dir and returns its standard output.
type GitFunc func(dir string, args ...string) (string, error)

// MachineFromEnv builds the Machine from the real environment.
func MachineFromEnv() (Machine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Machine{}, fmt.Errorf("find home: %w", err)
	}

	workDir, err := os.Getwd()
	if err != nil {
		return Machine{}, fmt.Errorf("find working dir: %w", err)
	}

	env := func(key string, def ...string) string {
		return cmp.Or(os.Getenv(key), filepath.Join(append([]string{home}, def...)...))
	}

	return Machine{
		Home:        home,
		ConfigHome:  env("XDG_CONFIG_HOME", ".config"),
		StateHome:   env("XDG_STATE_HOME", ".local", "state"),
		CacheHome:   env("XDG_CACHE_HOME", ".cache"),
		CodexHome:   env("CODEX_HOME", ".codex"),
		CodexSystem: "/etc/codex",
		WorkDir:     workDir,
		Env:         os.Environ(),
		Git:         GitRunner(nil),
		// As of Claude Code 2.1.282, computer-use is the one built-in that is
		// off until enabled.
		ClaudeBuiltins: map[string]State{"claude-in-chrome": On, "computer-use": Off},
	}, nil
}

// GitRunner returns a GitFunc that runs the git binary with env as its whole
// environment, or with the process environment when env is nil.
func GitRunner(env []string) GitFunc {
	return func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", args...) //nolint:gosec,noctx // equip builds git's args itself; nothing cancels git yet
		cmd.Dir = dir
		cmd.Env = env

		out, err := cmd.Output()
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}

		return string(out), err
	}
}
