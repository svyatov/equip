package equip

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Agent is a coding agent that equip manages.
type Agent int

// The agents.
const (
	ClaudeCode Agent = iota
	Codex
)

func (a Agent) String() string { return [...]string{ClaudeCode: "Claude Code", Codex: "Codex"}[a] }

// Source is a directory an agent loads an extension from.
type Source struct {
	Path  string
	Agent Agent
}

// Extension is one skill, plugin or MCP server, the same in every agent that
// has it.
type Extension struct {
	Key         string // a skill's key is its directory name
	Description string
	Sources     []Source // in discovery order
}

// has reports whether agent loads the extension.
func (e Extension) has(agent Agent) bool {
	return slices.ContainsFunc(e.Sources, func(s Source) bool { return s.Agent == agent })
}

// discover finds the extensions installed for a session started in dir,
// sorted by key, without running anything.
func discover(machine Machine, project Project, dir string) ([]Extension, error) {
	inv := inventory{byKey: map[string]*Extension{}, seen: map[Source]bool{}, err: nil}
	inv.add(ClaudeCode, filepath.Join(machine.Home, ".claude", "skills"))

	parents := upTo(dir, project.checkout)

	found := 0
	for _, d := range parents {
		found += inv.add(ClaudeCode, filepath.Join(d, ".claude", "skills"))
	}
	// Claude Code in a worktree with no skills of its own loads the main
	// checkout's. Outside a worktree, that dir was just read.
	if found == 0 {
		inv.add(ClaudeCode, filepath.Join(project.Path, ".claude", "skills"))
	}
	// Codex takes the nearest .git as the root, a worktree's own too.
	for _, d := range parents {
		inv.add(Codex, filepath.Join(d, ".agents", "skills"))
		inv.add(Codex, filepath.Join(d, ".codex", "skills"))
	}

	inv.add(Codex, filepath.Join(machine.Home, ".agents", "skills"))
	inv.add(Codex, filepath.Join(machine.CodexHome, "skills"))
	inv.add(Codex, filepath.Join(machine.CodexHome, "skills", ".system"))
	inv.add(Codex, filepath.Join(machine.CodexSystem, "skills"))

	if inv.err != nil {
		return nil, inv.err
	}

	exts := make([]Extension, 0, len(inv.byKey))
	for _, ext := range inv.byKey {
		exts = append(exts, *ext)
	}

	slices.SortFunc(exts, func(a, b Extension) int { return cmp.Compare(a.Key, b.Key) })

	return exts, nil
}

// inventory collects the skills of the dirs discover reads.
type inventory struct {
	byKey map[string]*Extension
	seen  map[Source]bool // the dirs read, by the agent that reads them
	err   error           // the first dir that failed to list
}

// add adds the skills agent loads from root and returns how many it found.
// A root reached twice, as a home dir is from itself, counts once.
func (inv *inventory) add(agent Agent, root string) int {
	// os.ReadDir sorts by name.
	entries, err := os.ReadDir(root)
	if inv.err != nil || errors.Is(err, fs.ErrNotExist) || inv.seen[Source{Path: root, Agent: agent}] {
		return 0
	}

	inv.seen[Source{Path: root, Agent: agent}] = true

	if err != nil {
		inv.err = fmt.Errorf("list skills: %w", err)

		return 0
	}

	found := 0

	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		// ReadFile follows symlinks, as both agents do.
		data, err := os.ReadFile(filepath.Join(path, "SKILL.md")) //nolint:gosec // equip builds the path
		if err != nil {
			continue
		}

		found++

		ext := inv.byKey[entry.Name()]
		if ext == nil {
			ext = &Extension{Key: entry.Name(), Description: "", Sources: nil}
			inv.byKey[entry.Name()] = ext
		}

		ext.Description = cmp.Or(ext.Description, description(data))
		ext.Sources = append(ext.Sources, Source{Path: path, Agent: agent})
	}

	return found
}

// description reads the description in the frontmatter of a SKILL.md.
func description(skill []byte) string {
	front, _, _ := strings.Cut(string(skill), "\n---")
	lines := strings.Split(front, "\n")

	for n, line := range lines {
		if value, found := strings.CutPrefix(line, "description:"); found {
			return yamlString(strings.TrimSpace(value), lines[n+1:])
		}
	}

	return ""
}

// yamlString reads the YAML string value, followed by the lines after it.
// ponytail: reads a plain, quoted or block value, not all of YAML; take a
// YAML parser once a skill needs more.
func yamlString(value string, after []string) string {
	sep := " " // a folded block joins its lines with spaces

	switch {
	case strings.HasPrefix(value, "|"):
		sep = "\n"
	case len(value) > 1 && value[0] == '\'' && value[len(value)-1] == '\'':
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	case !strings.HasPrefix(value, ">"):
		// YAML's double-quoted escapes are close enough to Go's.
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return value
		}

		return unquoted
	}

	var block []string

	for _, line := range after {
		if !strings.HasPrefix(line, " ") {
			break
		}

		block = append(block, strings.TrimSpace(line))
	}

	return strings.Join(block, sep)
}

// upTo lists dir and each of its parents up to top, nearest first.
func upTo(dir, top string) []string {
	dirs := []string{dir}
	for dir != top && dir != filepath.Dir(dir) {
		dir = filepath.Dir(dir)
		dirs = append(dirs, dir)
	}

	return dirs
}
