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

// writeClaude writes the skill states of overrides into the Project's
// .claude/settings.local.json, keeping every key equip does not own.
func writeClaude(m Machine, p Project, exts []Extension, overrides map[string]State) error {
	const rel = ".claude/settings.local.json"
	path := filepath.Join(p.Path, rel)
	// Raw values keep every other key exactly as it was.
	settings := map[string]json.RawMessage{}
	skills := map[string]json.RawMessage{}
	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	created := errors.Is(err, fs.ErrNotExist)
	if err == nil {
		err = json.Unmarshal(data, &settings)
	}
	if raw, ok := settings["skillOverrides"]; ok && err == nil {
		err = json.Unmarshal(raw, &skills)
	}
	if err != nil && !created {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// JSON null decodes to a nil map. A nil settings map only gets read.
	if skills == nil {
		skills = map[string]json.RawMessage{}
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
		if err := exclude(m, p, rel); err != nil {
			return err
		}
	}
	return writeFile(path, buf.Bytes())
}
