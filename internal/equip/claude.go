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

// settingsKey is the settings key that holds the states of kind.
func settingsKey(kind Kind) string {
	return [...]string{Skill: "skillOverrides", Plugin: "enabledPlugins"}[kind]
}

// entryValue is st as Claude Code spells it for kind.
func entryValue(kind Kind, st State) json.RawMessage {
	if kind == Plugin {
		return json.RawMessage(strconv.FormatBool(st == On))
	}

	return json.RawMessage(strconv.Quote(skillValue(st)))
}

// settingsRel is the Project's Claude Code settings file that equip writes.
const settingsRel = ".claude/settings.local.json"

// settingsFile is a Claude Code settings file as equip reads it.
type settingsFile struct {
	keys    map[string]json.RawMessage          // raw, so every other key stays exactly as it was
	entries map[Kind]map[string]json.RawMessage // the value of each kind's settingsKey
	missing bool
}

// readSettings reads the settings file at path. A missing file reads as
// empty.
func readSettings(path string) (settingsFile, error) {
	settings := settingsFile{
		keys:    map[string]json.RawMessage{},
		entries: map[Kind]map[string]json.RawMessage{Skill: {}, Plugin: {}},
		missing: false,
	}

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		settings.missing = true

		return settings, nil
	}

	if err == nil {
		err = json.Unmarshal(data, &settings.keys)
	}

	for kind, entries := range settings.entries {
		if raw, ok := settings.keys[settingsKey(kind)]; ok && err == nil {
			err = json.Unmarshal(raw, &entries)
			// JSON null decodes to a nil map. A nil keys map only gets read.
			if entries != nil {
				settings.entries[kind] = entries
			}
		}
	}

	if err != nil {
		return settingsFile{}, fmt.Errorf("read %s: %w", path, err)
	}

	return settings, nil
}

// readClaude reads the states Claude Code has for exts in the Project. A
// value equip does not know reads as no entry.
func readClaude(project Project, exts []Extension) (map[string]State, error) {
	states := map[string]State{}

	settings, err := readSettings(filepath.Join(project.Path, settingsRel))
	if err != nil {
		return states, err
	}

	for _, e := range exts {
		if st, ok := entryState(e.Kind, settings.entries[e.Kind][e.Key]); ok {
			states[e.Key] = st
		}
	}

	return states, nil
}

// entryState reads one entry for kind, reporting whether equip knows it.
func entryState(kind Kind, raw json.RawMessage) (State, bool) {
	if kind == Plugin {
		switch string(raw) {
		case "true":
			return On, true
		case "false":
			return Off, true
		}

		return 0, false
	}

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

// writeClaude writes the states of overrides into the Project's
// .claude/settings.local.json, keeping every key equip does not own.
func writeClaude(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	path := filepath.Join(project.Path, settingsRel)

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	mergeEntries(settings.entries, exts, overrides)

	out := map[string]any{}

	for key, v := range settings.keys {
		out[key] = v
	}
	// An owned key is written once it holds an entry, and kept once it is there.
	for kind, entries := range settings.entries {
		if _, had := settings.keys[settingsKey(kind)]; had || len(entries) > 0 {
			out[settingsKey(kind)] = entries
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

// mergeEntries sets the entry of each of exts to its state in overrides, or
// removes it when overrides has none.
func mergeEntries(entries map[Kind]map[string]json.RawMessage, exts []Extension, overrides map[string]State) {
	for _, ext := range exts {
		kind := entries[ext.Kind]
		state, overridden := overrides[ext.Key]

		was, known := entryState(ext.Kind, kind[ext.Key])
		switch {
		case !overridden && known:
			delete(kind, ext.Key)
		case !overridden:
			// A value equip does not know is not equip's to remove.
		case known && was == state:
			// It already holds, as "name-only" does for on.
		default:
			kind[ext.Key] = entryValue(ext.Kind, state)
		}
	}
}
