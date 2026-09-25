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

// claudeKind is how Claude Code keeps the states of one kind of extension.
type claudeKind struct {
	value       func(State) json.RawMessage         // a state as Claude Code spells it
	state       func(json.RawMessage) (State, bool) // reads an entry, reporting whether equip knows it
	settingsKey string                              // the settings key that holds the states
	states      []State                             // the states the user can pick
}

// claude is how Claude Code keeps the states of an extension of kind k.
func (k Kind) claude() claudeKind {
	return [...]claudeKind{
		Skill: {
			value:       func(st State) json.RawMessage { return json.RawMessage(strconv.Quote(skillValue(st))) },
			state:       skillState,
			settingsKey: "skillOverrides",
			states:      States(),
		},
		// A plugin is all or nothing, so it has no manual-only.
		Plugin: {
			value:       func(st State) json.RawMessage { return json.RawMessage(strconv.FormatBool(st == On)) },
			state:       pluginState,
			settingsKey: "enabledPlugins",
			states:      []State{On, Off},
		},
		// Claude Code keeps an MCP server's state in lists, not in a settings key.
		MCPServer: {value: nil, state: nil, settingsKey: "", states: []State{On, Off}},
	}[k]
}

// settingsRel is the Project's Claude Code settings file that equip writes.
const settingsRel = ".claude/settings.local.json"

// settingsFile is a Claude Code settings file as equip reads it.
type settingsFile struct {
	keys    map[string]json.RawMessage          // raw, so every other key stays exactly as it was
	entries map[Kind]map[string]json.RawMessage // the value of each kind's settings key
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
		if raw, ok := settings.keys[kind.claude().settingsKey]; ok && err == nil {
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

	lists := config.object("projects").object(project.Path)

	for _, ext := range exts {
		if state, ok := ext.claudeState(settings, lists); ok {
			states[ext.Key] = state
		}
	}

	return states, nil
}

// claudeState reads the extension's entry in the Project's settings or, for an
// MCP server Claude Code keeps there, in lists, the Project's entry in
// ~/.claude.json. It reports whether equip knows an entry.
func (e Extension) claudeState(settings settingsFile, lists jsonObject) (State, bool) {
	switch {
	case e.Kind != MCPServer:
		return e.Kind.claude().state(settings.entries[e.Kind][e.Key])
	case e.lists.settings:
		return e.lists.state(settings.keys, e.name())
	}

	return e.lists.state(lists, e.name())
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
// .claude/settings.local.json, keeping every key equip does not own.
func writeClaude(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
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

	if settings.missing {
		err = exclude(machine, project, settingsRel)
		if err != nil {
			return err
		}
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
