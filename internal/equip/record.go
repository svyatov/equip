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
	return [...]string{Skill: "skills", Plugin: "plugins"}[k]
}

// recordPath is the record file of project, one per path, so two clones of a
// repo keep their own.
func recordPath(machine Machine, project Project) string {
	sum := sha256.Sum256([]byte(project.Path))
	name := filepath.Base(project.Path) + "-" + hex.EncodeToString(sum[:8]) + ".toml"

	return filepath.Join(machine.StateHome, "equip", name)
}

// readRecord reads the Overrides in the record of project. A kind the record
// has no table for, with no record or one from before equip knew the kind,
// takes its states set by hand from disk, so a save keeps them.
func readRecord(machine Machine, project Project, disk map[string]State) (map[string]State, error) {
	path := recordPath(machine, project)

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

	for _, kind := range []Kind{Skill, Plugin} {
		for key, name := range rec.Overrides[kind.recordTable()] {
			st, ok := parseState(name)
			if !ok || !slices.Contains(kind.claude().states, st) {
				return nil, fmt.Errorf("read %s: %s %q: %w %q", path, kind, key, errUnknownState, name)
			}

			overrides[key] = st
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
	byKind := map[string]map[string]string{Skill.recordTable(): {}, Plugin.recordTable(): {}}

	for key, state := range overrides {
		byKind[keyKind(key).recordTable()][key] = state.String()
	}

	rec := record{Path: project.Path, RootCommit: project.RootCommit, Overrides: byKind}

	data, err := toml.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode record: %w", err)
	}

	return writeFile(recordPath(machine, project), data)
}
