package equip

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// skillValues are Claude Code's skillOverrides values.
var skillValues = map[State]string{On: "on", ManualOnly: "user-invocable-only", Off: "off"}

// skillStates reads Claude Code's skillOverrides values. "name-only" reads as
// on.
var skillStates = func() map[string]State {
	states := map[string]State{"name-only": On}
	for st, v := range skillValues {
		states[v] = st
	}

	return states
}()

// settingsRel is the Project's Claude Code settings file that equip writes.
const settingsRel = ".claude/settings.local.json"

// readSettings reads every key of the settings file at path and the
// skillOverrides in it. A missing file reads as empty.
func readSettings(path string) (settings, skills map[string]json.RawMessage, missing bool, err error) {
	// Raw values keep every other key exactly as it was.
	settings = map[string]json.RawMessage{}
	skills = map[string]json.RawMessage{}

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return settings, skills, true, nil
	}

	if err == nil {
		err = json.Unmarshal(data, &settings)
	}

	if raw, ok := settings["skillOverrides"]; ok && err == nil {
		err = json.Unmarshal(raw, &skills)
	}

	if err != nil {
		return nil, nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	// JSON null decodes to a nil map. A nil settings map only gets read.
	if skills == nil {
		skills = map[string]json.RawMessage{}
	}

	return settings, skills, false, nil
}

// readClaude reads the skill states Claude Code has for exts in the
// Project. A value equip does not know reads as no entry.
func readClaude(p Project, exts []Extension) (map[string]State, error) {
	states := map[string]State{}

	_, skills, _, err := readSettings(filepath.Join(p.Path, settingsRel))
	if err != nil {
		return states, err
	}

	for _, e := range exts {
		if st, ok := skillState(skills[e.Key]); ok {
			states[e.Key] = st
		}
	}

	return states, nil
}

// skillState reads one skillOverrides value, reporting whether equip knows it.
func skillState(raw json.RawMessage) (State, bool) {
	var v string

	_ = json.Unmarshal(raw, &v) // leaves v empty on a non-string
	st, ok := skillStates[v]

	return st, ok
}

// writeClaude writes the skill states of overrides into the Project's
// .claude/settings.local.json, keeping every key equip does not own.
func writeClaude(m Machine, p Project, exts []Extension, overrides map[string]State) error {
	path := filepath.Join(p.Path, settingsRel)

	settings, skills, created, err := readSettings(path)
	if err != nil {
		return err
	}

	for _, e := range exts {
		st, ok := overrides[e.Key]

		_, known := skillState(skills[e.Key])
		switch {
		case !ok && known:
			delete(skills, e.Key)
		case !ok:
			// A value equip does not know is not equip's to remove.
		case st == On && string(skills[e.Key]) == `"name-only"`:
			// "name-only" reads as on, so it already holds.
		default:
			skills[e.Key] = json.RawMessage(strconv.Quote(skillValues[st]))
		}
	}

	out := map[string]any{"skillOverrides": skills}

	for key, v := range settings {
		if key != "skillOverrides" {
			out[key] = v
		}
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keeps "&&" in permission rules readable
	enc.SetIndent("", "  ")

	err = enc.Encode(out)
	if err != nil {
		return err
	}

	if created {
		err := exclude(m, p, settingsRel)
		if err != nil {
			return err
		}
	}

	return writeFile(path, buf.Bytes())
}
