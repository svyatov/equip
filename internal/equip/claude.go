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
var skillStates = map[string]State{"on": On, "name-only": On, "user-invocable-only": ManualOnly, "off": Off}

// settingsRel is the Project's Claude Code settings file that equip writes.
const settingsRel = ".claude/settings.local.json"

// readSettings reads every key of the settings file at path and the
// skillOverrides in it. A missing file reads as empty, with an error that is
// fs.ErrNotExist.
func readSettings(path string) (settings, skills map[string]json.RawMessage, err error) {
	// Raw values keep every other key exactly as it was.
	settings = map[string]json.RawMessage{}
	skills = map[string]json.RawMessage{}
	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if err == nil {
		err = json.Unmarshal(data, &settings)
	}
	if raw, ok := settings["skillOverrides"]; ok && err == nil {
		err = json.Unmarshal(raw, &skills)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		err = fmt.Errorf("read %s: %w", path, err)
	}
	// JSON null decodes to a nil map. A nil settings map only gets read.
	if skills == nil {
		skills = map[string]json.RawMessage{}
	}
	return settings, skills, err
}

// readClaude reads the skill states Claude Code has for exts in the
// Project. A value equip does not know reads as no entry.
func readClaude(p Project, exts []Extension) (map[string]State, error) {
	states := map[string]State{}
	_, skills, err := readSettings(filepath.Join(p.Path, settingsRel))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return states, err
	}
	for _, e := range exts {
		var v string
		_ = json.Unmarshal(skills[e.Key], &v) // leaves v empty on a non-string
		if st, ok := skillStates[v]; ok {
			states[e.Key] = st
		}
	}
	return states, nil
}

// writeClaude writes the skill states of overrides into the Project's
// .claude/settings.local.json, keeping every key equip does not own.
func writeClaude(m Machine, p Project, exts []Extension, overrides map[string]State) error {
	path := filepath.Join(p.Path, settingsRel)
	settings, skills, err := readSettings(path)
	created := errors.Is(err, fs.ErrNotExist)
	if err != nil && !created {
		return err
	}
	for _, e := range exts {
		st, ok := overrides[e.Key]
		switch {
		case !ok:
			delete(skills, e.Key)
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
	if err := enc.Encode(out); err != nil {
		return err
	}
	if created {
		if err := exclude(m, p, settingsRel); err != nil {
			return err
		}
	}
	return writeFile(path, buf.Bytes())
}
