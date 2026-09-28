package equip

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
)

// skillValue is st as Claude Code's skillOverrides spells it.
func skillValue(st State) string {
	return [...]string{On: "on", ManualOnly: "user-invocable-only", Off: "off"}[st]
}

// claudeKind is how Claude Code keeps the states of one kind of extension.
type claudeKind struct {
	value       func(State) json.RawMessage         // a state as Claude Code spells it
	state       func(json.RawMessage) (State, bool) // reads an entry, reporting whether equip knows it
	settingsKey string                              // the settings key that holds the states
}

// claude is how Claude Code keeps the states of an extension of kind k.
func (k Kind) claude() claudeKind {
	return [...]claudeKind{
		Skill: {
			value:       func(st State) json.RawMessage { return json.RawMessage(strconv.Quote(skillValue(st))) },
			state:       skillState,
			settingsKey: "skillOverrides",
		},
		Plugin: {
			value:       func(st State) json.RawMessage { return json.RawMessage(strconv.FormatBool(st == On)) },
			state:       pluginState,
			settingsKey: "enabledPlugins",
		},
		// Claude Code keeps an MCP server's state in lists, not in a settings key.
		MCPServer: {value: nil, state: nil, settingsKey: ""},
	}[k]
}

// settingsRel is the Project's Claude Code settings file that equip writes.
const settingsRel = ".claude/settings.local.json"

// settingsFile is a Claude Code settings file as equip reads it.
type settingsFile struct {
	keys    jsonObject                          // raw, so every other key stays exactly as it was
	entries map[Kind]map[string]json.RawMessage // the value of each kind's settings key
}

// readSettings reads the settings file at path. A missing file reads as
// empty.
func readSettings(path string) (settingsFile, error) {
	settings := settingsFile{
		keys:    jsonObject{},
		entries: map[Kind]map[string]json.RawMessage{Skill: {}, Plugin: {}},
	}

	_, err := readDoc(path, json.Unmarshal, &settings.keys)
	if err != nil {
		return settingsFile{}, err
	}

	for kind, entries := range settings.entries {
		if raw, ok := settings.keys[kind.claude().settingsKey]; ok {
			err = json.Unmarshal(raw, &entries)
			if err != nil {
				return settingsFile{}, fmt.Errorf("read %s: %w", path, err)
			}
			// JSON null decodes to a nil map. A nil keys map only gets read.
			if entries != nil {
				settings.entries[kind] = entries
			}
		}
	}

	return settings, nil
}

// readSharedSettings reads the user's and the Project's shared Claude Code
// settings, the files equip does not write.
func readSharedSettings(machine Machine, project Project) ([]settingsFile, error) {
	dirs := []string{machine.Home, project.Path}
	sharedSettings := make([]settingsFile, 0, len(dirs))

	for _, dir := range dirs {
		settings, err := readSettings(filepath.Join(dir, ".claude", "settings.json"))
		if err != nil {
			return nil, err
		}

		sharedSettings = append(sharedSettings, settings)
	}

	return sharedSettings, nil
}

// sharedEntries are the entries of kind in sharedSettings as Claude Code
// merges them, key by key: the project's shared settings win over the user's.
func sharedEntries(sharedSettings []settingsFile, kind Kind) map[string]json.RawMessage {
	entries := map[string]json.RawMessage{}

	for _, settings := range sharedSettings {
		maps.Copy(entries, settings.entries[kind])
	}

	return entries
}

// readClaude reads the states Claude Code has for exts in the Project. A
// value equip does not know reads as no entry.
func readClaude(machine Machine, project Project, exts []Extension) (map[string]State, error) {
	states := map[string]State{}

	settings, err := readSettings(filepath.Join(project.Path, settingsRel))
	if err != nil {
		return states, err
	}

	config, err := readJSONObject(claudeJSONPath(machine))
	if err != nil {
		return states, err
	}

	projectEntry := config.object("projects").object(project.Path)

	for _, ext := range exts {
		if state, ok := ext.claudeState(settings, projectEntry); ok {
			states[ext.Key] = state
		}
	}

	return states, nil
}

// claudeState reads the extension's entry in the Project's settings or, for an
// MCP server Claude Code keeps elsewhere, in projectEntry, the Project's entry
// in ~/.claude.json. It reports whether equip knows an entry.
func (e Extension) claudeState(settings settingsFile, projectEntry jsonObject) (State, bool) {
	switch {
	case e.Kind != MCPServer:
		return e.Kind.claude().state(settings.entries[e.Kind][e.Key])
	case e.lists.settings:
		return e.lists.state(settings.keys, e.name())
	}

	return e.lists.state(projectEntry, e.listName())
}

// inSettings reports whether Claude Code keeps the extension's state in the
// Project's settings.local.json.
func (e Extension) inSettings() bool { return e.Kind != MCPServer || e.lists.settings }

// claudeDefaults are the Claude Code defaults of the exts kept in
// settings.local.json, by key.
func claudeDefaults(exts []Extension) map[string]State {
	defaults := map[string]State{}

	for _, ext := range exts {
		if ext.inSettings() {
			defaults[ext.Key] = ext.fallback[ClaudeCode]
		}
	}

	return defaults
}

// takeClaudeDefaults sets the Claude Code default of each of exts kept in
// settings.local.json. While git tracks the file, it is the ext's entry
// there: Claude Code reads the file, and equip leaves it alone. Else, and for
// an ext with no entry, it is the one in discovered. A file equip cannot read
// has no entries.
func takeClaudeDefaults(machine Machine, project Project, exts []Extension, discovered map[string]State, tracked bool) {
	entries := map[string]State{}
	if tracked {
		entries, _ = readClaude(machine, project, exts)
	}

	for _, ext := range exts {
		if !ext.inSettings() {
			continue
		}

		st, ok := entries[ext.Key]
		if !ok {
			st = discovered[ext.Key]
		}

		ext.fallback[ClaudeCode] = st
	}
}

// excludeSettings keeps the Project's .claude/settings.local.json out of git
// when it exists. git ignores the line for a file it tracks.
func excludeSettings(machine Machine, project Project) error {
	_, err := os.Stat(filepath.Join(project.Path, settingsRel))
	if err != nil {
		return nil //nolint:nilerr // no file to keep out
	}

	return exclude(machine, project, project.Path, settingsRel)
}

// pluginState reads one enabledPlugins value, reporting whether equip knows it.
func pluginState(raw json.RawMessage) (State, bool) {
	switch string(raw) {
	case "true":
		return On, true
	case "false":
		return Off, true
	}

	return 0, false
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

// writeClaude writes the states of overrides into the Project's
// .claude/settings.local.json, keeping every key equip does not own, and into
// ~/.claude.json. It never writes a settings.local.json git tracks.
func writeClaude(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	// Checked here, as the file may have become tracked since open.
	if tracked(machine, project, project.Path, settingsRel) {
		return writeClaudeJSON(machine, project, exts, overrides)
	}

	path := filepath.Join(project.Path, settingsRel)

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	mergeEntries(settings.entries, exts, overrides)

	writeLists(settings.keys, exts, overrides, true)

	out := map[string]any{}

	for key, v := range settings.keys {
		out[key] = v
	}
	// An owned key is written once it holds an entry, and kept once it is there.
	for kind, entries := range settings.entries {
		key := kind.claude().settingsKey
		if _, had := settings.keys[key]; had || len(entries) > 0 {
			out[key] = entries
		}
	}

	data, err := indentJSON(out)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	err = exclude(machine, project, project.Path, settingsRel)
	if err != nil {
		return err
	}

	err = writeFile(path, data)
	if err != nil {
		return err
	}

	return writeClaudeJSON(machine, project, exts, overrides)
}

// indentJSON encodes value as a file equip writes: indented as Claude Code
// does.
func indentJSON(value any) ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keeps "&&" in permission rules readable
	enc.SetIndent("", "  ")

	err := enc.Encode(value)

	return buf.Bytes(), err
}

// mergeEntries sets the entry of each of exts to its state in overrides, or
// removes it when overrides has none.
func mergeEntries(entries map[Kind]map[string]json.RawMessage, exts []Extension, overrides map[string]State) {
	for _, ext := range exts {
		if ext.Kind == MCPServer {
			continue
		}

		byKey, claude := entries[ext.Kind], ext.Kind.claude()
		state, overridden := overrides[ext.Key]

		was, known := claude.state(byKey[ext.Key])
		switch {
		case !overridden && known:
			delete(byKey, ext.Key)
		case !overridden:
			// A value equip does not know is not equip's to remove.
		case known && was == state:
			// It already holds, as "name-only" does for on.
		default:
			byKey[ext.Key] = claude.value(state)
		}
	}
}
