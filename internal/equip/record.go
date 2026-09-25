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

// stateNames are the states as the record spells them.
var stateNames = map[State]string{On: "on", ManualOnly: "manual-only", Off: "off"}

// recordPath is the record file of p, one per path, so two clones of a repo
// keep their own.
func recordPath(m Machine, p Project) string {
	sum := sha256.Sum256([]byte(p.Path))
	return filepath.Join(m.StateHome, "equip", filepath.Base(p.Path)+"-"+hex.EncodeToString(sum[:8])+".toml")
}

// readRecord reads the Overrides in the record of p. With no record, there
// are none.
func readRecord(m Machine, p Project) (map[string]State, error) {
	overrides := map[string]State{}
	path := recordPath(m, p)
	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return overrides, nil
	}
	if err != nil {
		return nil, err
	}
	var rec record
	if err := toml.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	for key, name := range rec.Overrides.Skills {
		st, ok := parseState(name)
		if !ok {
			return nil, fmt.Errorf("read %s: skill %q has unknown state %q", path, key, name)
		}
		overrides[key] = st
	}
	return overrides, nil
}

// parseState reads a state as the record spells it.
func parseState(name string) (State, bool) {
	for st, n := range stateNames {
		if n == name {
			return st, true
		}
	}
	return 0, false
}

// writeRecord writes the record of p with overrides.
func writeRecord(m Machine, p Project, overrides map[string]State) error {
	rec := record{Path: p.Path, RootCommit: p.RootCommit}
	rec.Overrides.Skills = map[string]string{}
	for key, st := range overrides {
		rec.Overrides.Skills[key] = stateNames[st]
	}
	data, err := toml.Marshal(rec)
	if err != nil {
		return err
	}
	return writeFile(recordPath(m, p), data)
}
