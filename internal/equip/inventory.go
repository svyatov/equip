package equip

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Extension is one skill, plugin or MCP server, the same in every agent that
// has it.
type Extension struct {
	Key string // a skill's key is its directory name
}

// discover finds the installed extensions, sorted by key, without running
// anything.
func discover(m Machine) ([]Extension, error) {
	dir := filepath.Join(m.Home, ".claude", "skills")
	// os.ReadDir sorts by name.
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var exts []Extension

	for _, e := range entries {
		// Stat follows symlinks, as Claude Code does.
		_, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}

		exts = append(exts, Extension{Key: e.Name()})
	}

	return exts, nil
}
