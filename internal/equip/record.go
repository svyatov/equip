package equip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/pelletier/go-toml/v2"
)

// record is what equip keeps of a Project on this machine.
type record struct {
	// The Overrides, one table per kind. A map, as a struct field reads an
	// empty table as absent.
	Overrides  map[string]map[string]string `toml:"overrides"`
	Path       string                       `toml:"path"`
	RootCommit string                       `toml:"root_commit"`
}

// recordTable is the record table that holds the Overrides of kind k.
func (k Kind) recordTable() string {
	return [...]string{Skill: "skills", Plugin: "plugins", MCPServer: "mcp_servers"}[k]
}

// kinds are the kinds, each with its own record table.
func kinds() []Kind { return []Kind{Skill, Plugin, MCPServer} }

// keyOf is the key of the extension of kind k named name in its record table.
func (k Kind) keyOf(name string) string {
	if k == MCPServer {
		return mcpPrefix + name
	}

	return name
}

// recordPath is the record file of the Project at path, one per path, so two
// clones of a repo keep their own.
func recordPath(machine Machine, path string) string {
	sum := sha256.Sum256([]byte(path))
	name := filepath.Base(path) + "-" + hex.EncodeToString(sum[:8]) + ".toml"

	return filepath.Join(machine.StateHome, "equip", name)
}

// readRecord reads the Overrides in the record at path. A kind the record
// has no table for, with no record or one from before equip knew the kind,
// takes its states set by hand from disk, so a save keeps them.
func readRecord(path string, disk map[string]State) (map[string]State, error) {
	rec, err := decodeRecord(path)
	if err != nil {
		return nil, err
	}

	overrides := map[string]State{}

	for key, st := range disk {
		if _, known := rec.Overrides[keyKind(key).recordTable()]; !known {
			overrides[key] = st
		}
	}

	for _, kind := range kinds() {
		for name, value := range rec.Overrides[kind.recordTable()] {
			st, ok := parseState(value)
			if !ok || !slices.Contains(kind.claude().states, st) {
				return nil, fmt.Errorf("read %s: %s %q: %w %q", path, kind, name, errUnknownState, value)
			}

			overrides[kind.keyOf(name)] = st
		}
	}

	return overrides, nil
}

// decodeRecord decodes the record at path. No record decodes as one with no
// tables.
func decodeRecord(path string) (record, error) {
	var rec record

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return rec, nil
	}

	if err != nil {
		return rec, fmt.Errorf("read record: %w", err)
	}

	err = toml.Unmarshal(data, &rec)
	if err != nil {
		return rec, fmt.Errorf("read %s: %w", path, err)
	}

	return rec, nil
}

// orphans are the paths of the records project can adopt: of a repo with its
// root commit whose path no longer exists. A second clone keeps its own.
// Only a first open offers them, and without a root commit nothing ties a
// record to project.
func orphans(machine Machine, project Project) []string {
	_, err := os.Stat(recordPath(machine, project.Path))
	if project.RootCommit == "" || !errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))

	var paths []string

	for _, file := range files {
		rec, err := decodeRecord(file)
		if err != nil || rec.RootCommit != project.RootCommit {
			continue
		}

		_, err = os.Stat(rec.Path)
		if errors.Is(err, fs.ErrNotExist) {
			paths = append(paths, rec.Path)
		}
	}

	return paths
}

// errUnknownState is the error of a record with a state equip does not know,
// or one the extension's kind does not offer.
var errUnknownState = errors.New("unknown state")

// parseState reads a state as State.String spells it.
func parseState(name string) (State, bool) {
	for _, st := range States() {
		if st.String() == name {
			return st, true
		}
	}

	return 0, false
}

// writeRecord writes the record of project with overrides.
func writeRecord(machine Machine, project Project, overrides map[string]State) error {
	// Every kind gets its table, empty too, so a read knows the record knows it.
	byKind := map[string]map[string]string{}
	for _, kind := range kinds() {
		byKind[kind.recordTable()] = map[string]string{}
	}

	for key, state := range overrides {
		byKind[keyKind(key).recordTable()][keyName(key)] = state.String()
	}

	rec := record{Path: project.Path, RootCommit: project.RootCommit, Overrides: byKind}

	data, err := toml.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode record: %w", err)
	}

	return writeFile(recordPath(machine, project.Path), data)
}
