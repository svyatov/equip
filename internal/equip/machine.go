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
	Home       string
	ConfigHome string // $XDG_CONFIG_HOME
	StateHome  string // $XDG_STATE_HOME
	CacheHome  string // $XDG_CACHE_HOME
	CodexHome  string // $CODEX_HOME
	WorkDir    string

	// Git runs git with args in dir and returns its standard output.
	Git func(dir string, args ...string) (string, error)
}

// MachineFromEnv builds the Machine from the real environment.
func MachineFromEnv() (Machine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Machine{}, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return Machine{}, err
	}
	env := func(key string, def ...string) string {
		return cmp.Or(os.Getenv(key), filepath.Join(append([]string{home}, def...)...))
	}
	return Machine{
		Home:       home,
		ConfigHome: env("XDG_CONFIG_HOME", ".config"),
		StateHome:  env("XDG_STATE_HOME", ".local", "state"),
		CacheHome:  env("XDG_CACHE_HOME", ".cache"),
		CodexHome:  env("CODEX_HOME", ".codex"),
		WorkDir:    wd,
		Git:        RunGit(nil),
	}, nil
}

// RunGit returns a Machine.Git that runs the git binary with env added to the
// process environment.
func RunGit(env []string) func(dir string, args ...string) (string, error) {
	return func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), err
	}
}
