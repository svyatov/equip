package equip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// record is what equip keeps of a Project on this machine.
type record struct {
	Path       string `toml:"path"`
	RootCommit string `toml:"root_commit"`
	Overrides  struct {
		Skills map[string]string `toml:"skills"`
	} `toml:"overrides"`
}

// recordPath is the record file of project, one per path, so two clones of a
// repo keep their own.
func recordPath(machine Machine, project Project) string {
	sum := sha256.Sum256([]byte(project.Path))
	name := filepath.Base(project.Path) + "-" + hex.EncodeToString(sum[:8]) + ".toml"

	return filepath.Join(machine.StateHome, "equip", name)
}

// readRecord reads the Overrides in the record of project. With no record, it
// returns nil.
func readRecord(machine Machine, project Project) (map[string]State, error) {
	path := recordPath(machine, project)

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // no record is not an error
	}

	if err != nil {
		return nil, fmt.Errorf("read record: %w", err)
	}

	var rec record

	err = toml.Unmarshal(data, &rec)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	overrides := map[string]State{}

	for key, name := range rec.Overrides.Skills {
		st, ok := parseState(name)
		if !ok {
			return nil, fmt.Errorf("read %s: skill %q: %w %q", path, key, errUnknownState, name)
		}

		overrides[key] = st
	}

	return overrides, nil
}

// errUnknownState is the error of a record with a state equip does not know.
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
	rec := record{Path: project.Path, RootCommit: project.RootCommit}

	rec.Overrides.Skills = map[string]string{}
	for key, st := range overrides {
		rec.Overrides.Skills[key] = st.String()
	}

	data, err := toml.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode record: %w", err)
	}

	return writeFile(recordPath(machine, project), data)
}
