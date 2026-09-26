package equip

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// codexLayer is one Codex config file as equip reads it.
type codexLayer struct {
	data  map[string]any
	path  string
	owned bool // the Project's, which equip writes
}

// codexConfig is the Codex config of a Project as equip reads it.
type codexConfig struct {
	notApplied string       // why equip does not write the Project's config; empty when it does
	layers     []codexLayer // the ones Codex reads, the user's first
}

const (
	// codexConfigRel is the Project's Codex config, the one equip writes.
	codexConfigRel = ".codex/config.toml"
	// codexPluginsBlockBytes is the size of the fixed "## Plugins" block
	// Codex puts into a session once when any plugin is on.
	codexPluginsBlockBytes = 1000
)

// readCodexConfig reads the Codex config of the Project. Codex reads the
// Project's config only when the user's trusts the Project, and equip never
// sets trust.
func readCodexConfig(machine Machine, project Project) (codexConfig, error) {
	userPath := filepath.Join(machine.CodexHome, "config.toml")

	user, err := readTOML(userPath)
	if err != nil {
		return codexConfig{}, err
	}

	cfg := codexConfig{notApplied: "", layers: []codexLayer{{data: user, path: userPath, owned: false}}}
	if table(table(user, "projects"), project.Path)["trust_level"] != "trusted" {
		cfg.notApplied = "this Project is not trusted"

		return cfg, nil
	}

	path := filepath.Join(project.Path, codexConfigRel)

	data, err := readTOML(path)
	if err != nil {
		return codexConfig{}, err
	}

	// A tracked file belongs to everyone who clones the repo.
	if tracked(machine, project, codexConfigRel) {
		cfg.notApplied = codexConfigRel + " is tracked by git"
	}

	cfg.layers = append(cfg.layers, codexLayer{data: data, path: path, owned: cfg.notApplied == ""})

	return cfg, nil
}

// readCodex reads the states Codex has for exts in the Project's config. A
// value equip does not know reads as no entry.
func readCodex(project Project, exts []Extension) (map[string]State, error) {
	states := map[string]State{}
	// No exts when equip does not write the file, so it is not read.
	if len(exts) == 0 {
		return states, nil
	}

	doc, err := readTOML(filepath.Join(project.Path, codexConfigRel))
	if err != nil {
		return states, err
	}

	for _, ext := range exts {
		if st, ok := codexState(doc, ext.Key); ok {
			states[ext.Key] = st
		}
	}

	return states, nil
}

// writeCodex writes the states of overrides for exts into the Project's
// .codex/config.toml, keeping every key equip does not own, and keeps the
// file out of git. It leaves the file alone when no entry changes.
// ponytail: re-encodes the file, which drops its comments and key order; edit
// the TOML in place if users keep notes there.
func writeCodex(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	if len(exts) == 0 {
		return nil
	}

	path := filepath.Join(project.Path, codexConfigRel)

	doc, err := readTOML(path)
	if err != nil {
		return err
	}

	changed := false

	for _, ext := range exts {
		st, set := overrides[ext.Key]
		changed = setCodexState(doc, ext.Key, st, set) || changed
	}

	if !changed {
		return nil
	}

	data, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	err = exclude(machine, project, codexConfigRel)
	if err != nil {
		return err
	}

	return writeFile(path, data)
}

// setCodexState sets the state of the extension with key in doc to state, or
// removes it with no state, reporting whether doc changed. A value equip does
// not know is not equip's to remove.
func setCodexState(doc map[string]any, key string, state State, set bool) bool {
	parent, name := codexTable(key), keyName(key)

	was, known := codexState(doc, key)

	switch {
	case set && known && was == state, !set && !known:
		return false
	case set:
		tableAt(tableAt(doc, parent), name)["enabled"] = state == On

		return true
	}
	// Tables that held only equip's entry go with it.
	tables := table(doc, parent)
	entry := table(tables, name)
	delete(entry, "enabled")

	if len(entry) == 0 {
		delete(tables, name)
	}

	if len(tables) == 0 {
		delete(doc, parent)
	}

	return true
}

// tableAt returns the table under key in doc, creating it when missing.
func tableAt(doc map[string]any, key string) map[string]any {
	found := table(doc, key)
	if found == nil {
		found = map[string]any{}
		doc[key] = found
	}

	return found
}

// readTOML reads the TOML file at path. A missing file reads as empty.
func readTOML(path string) (map[string]any, error) {
	doc := map[string]any{}

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return doc, nil
	}

	if err == nil {
		err = toml.Unmarshal(data, &doc)
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return doc, nil
}

// table reads the table under key. Anything else reads as none.
func table(doc map[string]any, key string) map[string]any {
	t, _ := doc[key].(map[string]any)

	return t
}

// discoverCodex finds the plugins and MCP servers Codex has in cfg.
func discoverCodex(machine Machine, cfg codexConfig) []Extension {
	byKey := map[string]*Extension{}

	for _, layer := range cfg.layers {
		for key := range table(layer.data, "plugins") {
			// Codex loads nothing it has no files of.
			dir := codexPluginDir(machine, key)
			if dir != "" {
				ext := readCodexPlugin(key, dir)
				byKey[key] = &ext
			}
		}

		for name := range table(layer.data, "mcp_servers") {
			key := mcpPrefix + name
			if byKey[key] == nil {
				byKey[key] = &Extension{
					Kind: MCPServer, Key: key, Description: "", cost: map[Agent]int{}, fallback: On,
					Locations: nil, contents: nil, hooks: false, lists: mcpLists{on: "", off: "", settings: false},
					builtIn: false,
				}
			}

			byKey[key].Locations = append(byKey[key].Locations, Location{Path: layer.path, Agent: Codex})
		}
	}

	exts := make([]Extension, 0, len(byKey))

	for _, ext := range byKey {
		// Codex merges its layers table by table; the last one wins. The
		// states equip writes are Overrides, not defaults.
		for _, layer := range cfg.layers {
			if st, ok := codexState(layer.data, ext.Key); ok && !layer.owned {
				ext.fallback = st
			}
		}

		exts = append(exts, *ext)
	}

	return exts
}

// codexTable is the Codex config table that holds the kind of the extension
// with key, each by its name.
func codexTable(key string) string {
	if keyKind(key) == MCPServer {
		return "mcp_servers"
	}

	return "plugins"
}

// codexState reads the state in the table of the extension with key in doc,
// reporting whether the table holds one.
func codexState(doc map[string]any, key string) (State, bool) {
	enabled, ok := table(table(doc, codexTable(key)), keyName(key))["enabled"].(bool)
	if !enabled {
		return Off, ok
	}

	return On, ok
}

// merge adds each of more to exts, into the extension of the same kind and
// key when exts has one.
func merge(exts, more []Extension) []Extension {
	for _, ext := range more {
		same := slices.IndexFunc(exts, func(e Extension) bool { return e.Kind == ext.Kind && e.Key == ext.Key })
		if same < 0 {
			exts = append(exts, ext)

			continue
		}

		exts[same].Locations = append(exts[same].Locations, ext.Locations...)
		exts[same].Description = cmp.Or(exts[same].Description, ext.Description)
		maps.Copy(exts[same].cost, ext.cost)
	}

	return exts
}

// codexPluginDir is the dir of the plugin key in the Codex plugin cache, or
// empty when it has none. ponytail: takes the last version dir by name, so
// 1.10.0 sorts before 1.9.0; read the version Codex records if that matters.
func codexPluginDir(machine Machine, key string) string {
	name, marketplace, _ := strings.Cut(key, "@")
	versions := filepath.Join(machine.CodexHome, "plugins", "cache", marketplace, name)

	entries, _ := os.ReadDir(versions)
	if len(entries) == 0 {
		return ""
	}

	return filepath.Join(versions, entries[len(entries)-1].Name())
}

// readCodexPlugin reads the Codex plugin key from its dir in the plugin cache.
func readCodexPlugin(key, dir string) Extension {
	var man manifest
	// The manifest is optional; one equip cannot read reads as none.
	data, _ := os.ReadFile(filepath.Join(dir, ".codex-plugin", "plugin.json")) //nolint:gosec // equip builds the path
	_ = json.Unmarshal(data, &man)
	man.Name = cmp.Or(man.Name, strings.TrimSuffix(key, "@"+marketplaceOf(key)))

	contents := pluginSkills(Codex, dir, man.Name)
	cost := 0

	for _, content := range contents {
		cost += content.Cost
	}

	return Extension{
		Kind: Plugin, Key: key, Description: man.Description, cost: map[Agent]int{Codex: cost}, fallback: On,
		Locations: []Location{{Path: dir, Agent: Codex}}, contents: contents, hooks: false,
		lists: mcpLists{on: "", off: "", settings: false}, builtIn: false,
	}
}
