package equip

import (
	"cmp"
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
	// codexPlugins and codexServers are the Codex config tables of plugins
	// and of MCP servers, each by its name.
	codexPlugins = "plugins"
	codexServers = "mcp_servers"
	// codexPluginsBlockBytes is the size of the fixed "## Plugins" block
	// Codex puts into a session once when any plugin is on.
	codexPluginsBlockBytes = 1000
)

// readCodexConfig reads the Codex config of the Project. Codex reads the
// Project's config only when the user's config trusts the Project, and equip
// never sets trust. A file equip cannot read leaves Codex out, so Claude Code
// still opens and saves.
func readCodexConfig(machine Machine, project Project) codexConfig {
	userPath := filepath.Join(machine.CodexHome, "config.toml")

	user, err := readTOML(userPath)
	if err != nil {
		return codexConfig{notApplied: err.Error(), layers: nil}
	}

	cfg := codexConfig{notApplied: "", layers: []codexLayer{{data: user, path: userPath, owned: false}}}
	if table(table(user, "projects"), project.Path)["trust_level"] != "trusted" {
		cfg.notApplied = "this Project is not trusted"

		return cfg
	}

	path := filepath.Join(project.Path, codexConfigRel)

	data, err := readTOML(path)
	if err != nil {
		cfg.notApplied = err.Error()

		return cfg
	}

	// A tracked file belongs to everyone who clones the repo.
	if tracked(machine, project, codexConfigRel) {
		cfg.notApplied = codexConfigRel + " is tracked by git"
	}

	cfg.layers = append(cfg.layers, codexLayer{data: data, path: path, owned: cfg.notApplied == ""})

	return cfg
}

// dead lists the MCP servers whose table in the Project's config holds only
// equip's enabled and that no other layer defines, as when the user removed
// the server. Codex refuses a config with such a table.
func (c codexConfig) dead() []string {
	var dead []string

	for _, layer := range c.layers {
		if !layer.owned {
			continue
		}

		for name, value := range table(layer.data, codexServers) {
			entry, _ := value.(map[string]any)
			_, enabled := entry["enabled"]
			defined := slices.ContainsFunc(c.layers, func(l codexLayer) bool {
				return !l.owned && table(table(l.data, codexServers), name) != nil
			})

			if len(entry) == 1 && enabled && !defined {
				dead = append(dead, name)
			}
		}
	}

	return dead
}

// readCodex reads the states Codex has for exts in the Project's config. A
// value equip does not know reads as no entry.
func readCodex(_ Machine, project Project, exts []Extension) (map[string]State, error) {
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
		if st, ok := codexState(doc, ext.codexPath()); ok {
			states[ext.Key] = st
		}
	}

	return states, nil
}

// writeCodex writes the states of overrides for exts into the Project's
// .codex/config.toml, keeping every key equip does not own, and keeps the
// file out of git. It removes the dead entries, and leaves the file alone when
// no entry changes.
// ponytail: re-encodes the file, which drops its comments and key order; edit
// the TOML in place if users keep notes there.
func writeCodex(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	// Read again, as the file may have become tracked since open.
	cfg := readCodexConfig(machine, project)
	if cfg.notApplied != "" {
		return nil
	}

	path := filepath.Join(project.Path, codexConfigRel)
	doc := cfg.layers[len(cfg.layers)-1].data
	dead := cfg.dead()
	changed := len(dead) > 0

	for _, name := range dead {
		setCodexState(doc, []string{codexServers, name}, On, false)
	}

	for _, ext := range exts {
		st, set := overrides[ext.Key]
		changed = setCodexState(doc, ext.codexPath(), st, set) || changed
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

// setCodexState sets the state in the table at path in doc to state, or
// removes it with no state, reporting whether doc changed. A value equip does
// not know is not equip's to remove.
func setCodexState(doc map[string]any, path []string, state State, set bool) bool {
	was, known := codexState(doc, path)

	switch {
	case set && known && was == state, !set && !known:
		return false
	case set:
		entry := doc
		for _, key := range path {
			entry = tableAt(entry, key)
		}

		entry["enabled"] = state == On

		return true
	}
	// Tables that held only equip's entry go with it.
	tables := []map[string]any{doc}
	for _, key := range path {
		tables = append(tables, table(tables[len(tables)-1], key))
	}

	delete(tables[len(path)], "enabled")

	for i := len(path); i > 0 && len(tables[i]) == 0; i-- {
		delete(tables[i-1], path[i-1])
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

	dead := cfg.dead()

	for _, layer := range cfg.layers {
		for key := range table(layer.data, codexPlugins) {
			// Codex loads nothing it has no files of.
			dir := codexPluginDir(machine, key)
			if dir != "" {
				for _, ext := range readCodexPlugin(key, dir) {
					byKey[ext.Key] = &ext
				}
			}
		}

		addCodexServers(byKey, layer, dead)
	}

	exts := make([]Extension, 0, len(byKey))

	for _, ext := range byKey {
		// Codex merges its layers table by table; the last one wins. The
		// states equip writes are Overrides, not defaults.
		for _, layer := range cfg.layers {
			if st, ok := codexState(layer.data, ext.codexPath()); ok && !layer.owned {
				ext.fallback[Codex] = st
			}
		}

		exts = append(exts, *ext)
	}

	return exts
}

// addCodexServers adds each MCP server in layer to byKey, but the dead ones.
func addCodexServers(byKey map[string]*Extension, layer codexLayer, dead []string) {
	for name := range table(layer.data, codexServers) {
		key := mcpPrefix + name
		if slices.Contains(dead, name) {
			continue
		}

		if byKey[key] == nil {
			byKey[key] = &Extension{
				Kind: MCPServer, Key: key, Description: "", cost: map[Agent]int{}, fallback: map[Agent]State{},
				Locations: nil, contents: nil, hooks: false, lists: mcpLists{on: "", off: "", settings: false},
				builtIn: false, plugin: "", listed: "",
			}
		}

		byKey[key].Locations = append(byKey[key].Locations, Location{Path: layer.path, Agent: Codex})
	}
}

// codexPath is the path of the Codex config table that holds the extension's
// state. Codex 0.155.1 reads a plugin's MCP server's from the merged config,
// so from a trusted Project's config too.
func (e Extension) codexPath() []string {
	switch {
	case e.plugin != "":
		return []string{codexPlugins, e.plugin, codexServers, e.name()}
	case e.Kind == MCPServer:
		return []string{codexServers, e.name()}
	}

	return []string{codexPlugins, e.Key}
}

// codexState reads the state in the table at path in doc, reporting whether
// the table holds one.
func codexState(doc map[string]any, path []string) (State, bool) {
	entry := doc
	for _, key := range path {
		entry = table(entry, key)
	}

	enabled, ok := entry["enabled"].(bool)
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
		maps.Copy(exts[same].fallback, ext.fallback)
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

// readCodexPlugin reads the Codex plugin key from its dir in the plugin cache,
// followed by its MCP servers.
func readCodexPlugin(key, dir string) []Extension {
	man := readManifest(key, dir, ".codex-plugin")
	contents := pluginSkills(Codex, dir, man.Name)

	plugin := Extension{
		Kind: Plugin, Key: key, Description: man.Description, fallback: map[Agent]State{},
		cost:      map[Agent]int{Codex: contentsCost(contents)},
		Locations: []Location{{Path: dir, Agent: Codex}}, contents: contents, hooks: false,
		lists: mcpLists{on: "", off: "", settings: false}, builtIn: false, plugin: "", listed: "",
	}

	return append([]Extension{plugin}, pluginServers(Codex, key, dir, man)...)
}
