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

// skillValue is st as Claude Code's skillOverrides spells it.
func skillValue(st State) string {
	return [...]string{On: "on", ManualOnly: "user-invocable-only", Off: "off"}[st]
}

// settingsRel is the Project's Claude Code settings file that equip writes.
const settingsRel = ".claude/settings.local.json"

// settingsFile is a Claude Code settings file as equip reads it.
type settingsFile struct {
	keys    map[string]json.RawMessage // raw, so every other key stays exactly as it was
	skills  map[string]json.RawMessage // the skillOverrides
	missing bool
}

// readSettings reads the settings file at path. A missing file reads as
// empty.
func readSettings(path string) (settingsFile, error) {
	settings := settingsFile{keys: map[string]json.RawMessage{}, skills: map[string]json.RawMessage{}, missing: false}

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		settings.missing = true

		return settings, nil
	}

	if err == nil {
		err = json.Unmarshal(data, &settings.keys)
	}

	if raw, ok := settings.keys["skillOverrides"]; ok && err == nil {
		err = json.Unmarshal(raw, &settings.skills)
	}

	if err != nil {
		return settingsFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	// JSON null decodes to a nil map. A nil keys map only gets read.
	if settings.skills == nil {
		settings.skills = map[string]json.RawMessage{}
	}

	return settings, nil
}

// readClaude reads the skill states Claude Code has for exts in the
// Project. A value equip does not know reads as no entry.
func readClaude(project Project, exts []Extension) (map[string]State, error) {
	states := map[string]State{}

	settings, err := readSettings(filepath.Join(project.Path, settingsRel))
	if err != nil {
		return states, err
	}

	for _, e := range exts {
		if st, ok := skillState(settings.skills[e.Key]); ok {
			states[e.Key] = st
		}
	}

	return states, nil
}

// skillState reads one skillOverrides value, reporting whether equip knows it.
func skillState(raw json.RawMessage) (State, bool) {
	var value string

	_ = json.Unmarshal(raw, &value) // leaves value empty on a non-string
	if value == "name-only" {
		return On, true // it reads as on
	}

	for _, st := range States() {
		if skillValue(st) == value {
			return st, true
		}
	}

	return 0, false
}

// writeClaude writes the skill states of overrides into the Project's
// .claude/settings.local.json, keeping every key equip does not own.
func writeClaude(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	path := filepath.Join(project.Path, settingsRel)

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	mergeSkills(settings.skills, exts, overrides)

	out := map[string]any{"skillOverrides": settings.skills}

	for key, v := range settings.keys {
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
		return fmt.Errorf("encode %s: %w", path, err)
	}

	if settings.missing {
		err = exclude(machine, project, settingsRel)
		if err != nil {
			return err
		}
	}

	return writeFile(path, buf.Bytes())
}

// mergeSkills sets the skillOverrides value of each of exts to its state in
// overrides, or removes it when overrides has none.
func mergeSkills(skills map[string]json.RawMessage, exts []Extension, overrides map[string]State) {
	for _, ext := range exts {
		state, overridden := overrides[ext.Key]

		_, known := skillState(skills[ext.Key])
		switch {
		case !overridden && known:
			delete(skills, ext.Key)
		case !overridden:
			// A value equip does not know is not equip's to remove.
		case state == On && string(skills[ext.Key]) == `"name-only"`:
			// "name-only" reads as on, so it already holds.
		default:
			skills[ext.Key] = json.RawMessage(strconv.Quote(skillValue(state)))
		}
	}
}
