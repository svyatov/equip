package equip

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
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
	// codexConfigRel is the Project's Codex config, the one equip writes, in
	// the checkout equip runs in.
	codexConfigRel = ".codex/config.toml"
	// codexPlugins and codexServers are the Codex config tables of plugins
	// and of MCP servers, each by its name.
	codexPlugins = "plugins"
	codexServers = "mcp_servers"
	// codexPluginsBlockBytes is the size of the fixed "## Plugins" block
	// Codex puts into a session once when any plugin is on.
	codexPluginsBlockBytes = 1000
)

// codexConfigPath is the Project's Codex config. Codex 0.155.1 reads it at the
// root of the worktree it runs in.
func codexConfigPath(project Project) string { return filepath.Join(project.checkout, codexConfigRel) }

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
	// Codex 0.155.1 trusts a worktree through its main checkout.
	if table(table(user, "projects"), project.Path)["trust_level"] != "trusted" {
		cfg.notApplied = "this Project is not trusted"

		return cfg
	}

	path := codexConfigPath(project)

	data, err := readTOML(path)
	if err != nil {
		cfg.notApplied = err.Error()

		return cfg
	}

	cfg.notApplied = trackedReason(machine, project, project.checkout, codexConfigRel)
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

	doc, err := readTOML(codexConfigPath(project))
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

// writeCodex writes the states of overrides for exts into the Project's Codex
// config, keeping every key equip does not own, and keeps the file out of git.
// It removes the dead entries, and leaves the file alone when no entry
// changes. It edits the file in place, keeping its comments and key order.
func writeCodex(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	// Read again, as the file may have become tracked since open.
	cfg := readCodexConfig(machine, project)
	if cfg.notApplied != "" {
		return nil
	}

	path := codexConfigPath(project)
	doc := cfg.layers[len(cfg.layers)-1].data

	var changed [][]string

	for _, name := range cfg.dead() {
		setCodexState(doc, []string{codexServers, name}, On, false)
		changed = append(changed, []string{codexServers, name})
	}

	for _, ext := range exts {
		st, set := overrides[ext.Key]
		if setCodexState(doc, ext.codexPath(), st, set) {
			changed = append(changed, ext.codexPath())
		}
	}

	if len(changed) == 0 {
		return nil
	}

	before, _ := os.ReadFile(path) //nolint:gosec // equip builds the path

	data, ok := editTOML(before, doc, changed)
	if !ok {
		var err error

		data, err = toml.Marshal(doc)
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
	}

	err := exclude(machine, project, project.checkout, codexConfigRel)
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
	tables := tablesOn(doc, path)
	delete(tables[len(path)], "enabled")

	for i := len(path); i > 0 && len(tables[i]) == 0; i-- {
		delete(tables[i-1], path[i-1])
	}

	return true
}

// editTOML returns data with the enabled key of the table at each of paths
// set as in doc, keeping every other byte, or false when the edited data does
// not read as doc, as when data holds a table in a shape it does not edit.
func editTOML(data []byte, doc map[string]any, paths [][]string) ([]byte, bool) {
	tables := tomlTables(data)
	edits := make([]tomlEdit, 0, len(paths))

	for _, path := range paths {
		edits = append(edits, tables[fmt.Sprintf("%q", path)].edit(data, path, tablesOn(doc, path)[len(path)])...)
	}

	edited := applyEdits(data, edits)
	if len(data) == 0 {
		edited = bytes.TrimPrefix(edited, []byte("\n"))
	}

	var got map[string]any

	err := toml.Unmarshal(edited, &got)

	return edited, err == nil && reflect.DeepEqual(got, doc)
}

// tomlTable is where a table's header line and its enabled's value and line
// are in a TOML file. Each is empty when the file has none.
type tomlTable struct {
	header  unstable.Range
	enabled unstable.Range
	line    unstable.Range
}

// edit lists the edits that set the enabled of the table at path in data as
// in entry, the table after the change.
func (t tomlTable) edit(data []byte, path []string, entry map[string]any) []tomlEdit {
	value, set := entry["enabled"]
	none := unstable.Range{Offset: 0, Length: 0}

	switch {
	case set && t.enabled != none:
		return []tomlEdit{{text: fmt.Sprint(value), at: t.enabled}}
	case set && t.header != none:
		at := unstable.Range{Offset: lineEnd(data, t.header.Offset), Length: 0}

		return []tomlEdit{{text: fmt.Sprint("\nenabled = ", value), at: at}}
	case set:
		end := unstable.Range{Offset: uint32(len(data)), Length: 0} //nolint:gosec // the parser takes no data past 4 GiB

		return []tomlEdit{{text: fmt.Sprintf("\n[%s]\nenabled = %v\n", tomlHeader(path), value), at: end}}
	case entry == nil:
		// A table that held only equip's entry goes with it.
		return []tomlEdit{{text: "", at: t.line}, {text: "", at: t.header}}
	}

	return []tomlEdit{{text: "", at: t.line}}
}

// tomlHeader writes path as the key of a TOML table header.
func tomlHeader(path []string) string {
	parts := make([]string, len(path))

	for i, part := range path {
		parts[i] = part
		// A key of other bytes needs quotes.
		if strings.Trim(part, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			parts[i] = strconv.Quote(part)
		}
	}

	return strings.Join(parts, ".")
}

// tomlTables reads the tables of the TOML data by their keys, up to the first
// error.
func tomlTables(data []byte) map[string]tomlTable {
	tables := map[string]tomlTable{}

	var parser unstable.Parser

	parser.Reset(data)

	var key string

	for parser.NextExpression() {
		expr := parser.Expression()

		switch expr.Kind { //nolint:exhaustive // no other kind of expression holds a table's enabled
		case unstable.Table, unstable.ArrayTable:
			parts, at := tomlKey(expr.Key())
			key = fmt.Sprintf("%q", parts)
			none := unstable.Range{Offset: 0, Length: 0}
			tables[key] = tomlTable{header: lines(data, at), enabled: none, line: none}
		case unstable.KeyValue:
			if parts, _ := tomlKey(expr.Key()); slices.Equal(parts, []string{"enabled"}) {
				table := tables[key]
				table.enabled, table.line = expr.Value().Raw, lines(data, expr.Raw)
				tables[key] = table
			}
		}
	}

	return tables
}

// tomlKey reads the parts of a TOML key and where its last part is.
func tomlKey(key unstable.Iterator) ([]string, unstable.Range) {
	var (
		parts []string
		last  unstable.Range
	)

	for key.Next() {
		parts = append(parts, string(key.Node().Data))
		last = key.Node().Raw
	}

	return parts, last
}

// lineEnd is where the line at offset ends in data, before its newline.
func lineEnd(data []byte, offset uint32) uint32 {
	line, _, _ := bytes.Cut(data[offset:], []byte("\n"))

	return offset + uint32(len(line)) //nolint:gosec // the parser takes no data past 4 GiB
}

// lines is the lines of data that r is on, with the newline that ends them.
func lines(data []byte, r unstable.Range) unstable.Range {
	start := bytes.LastIndexByte(data[:r.Offset], '\n') + 1
	end := min(int(lineEnd(data, r.Offset+r.Length))+1, len(data))

	return unstable.Range{Offset: uint32(start), Length: uint32(end - start)} //nolint:gosec // as in lineEnd
}

// tomlEdit replaces the bytes at with text.
type tomlEdit struct {
	text string
	at   unstable.Range
}

// applyEdits returns data with each of edits made, those at one offset in
// order. The edits do not overlap.
func applyEdits(data []byte, edits []tomlEdit) []byte {
	slices.SortStableFunc(edits, func(a, b tomlEdit) int { return cmp.Compare(a.at.Offset, b.at.Offset) })

	out := make([]byte, 0, len(data))
	done := 0

	for _, edit := range edits {
		out = append(out, data[done:edit.at.Offset]...)
		out = append(out, edit.text...)
		done = int(edit.at.Offset + edit.at.Length)
	}

	return append(out, data[done:]...)
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

	_, err := readDoc(path, toml.Unmarshal, &doc)
	if err != nil {
		return nil, err
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
			server := newExtension(MCPServer, key)
			byKey[key] = &server
		}

		byKey[key].Locations = append(byKey[key].Locations, Location{Path: layer.path, Agent: Codex})
		// Codex merges the server's table key by key.
		config := map[string]any{}
		_ = json.Unmarshal(byKey[key].config[Codex], &config)
		maps.Copy(config, table(table(layer.data, codexServers), name))

		byKey[key].config[Codex], _ = json.Marshal(config) //nolint:errchkjson // TOML values always encode
	}
}

// codexPath is the path of the Codex config table that holds the extension's
// state. Codex 0.155.1 reads the states of a plugin's MCP servers from the
// merged config, so from a trusted Project's config too.
func (e Extension) codexPath() []string {
	switch {
	case e.plugin != "":
		return []string{codexPlugins, e.plugin, codexServers, e.name()}
	case e.Kind == MCPServer:
		return []string{codexServers, e.name()}
	}

	return []string{codexPlugins, e.Key}
}

// tablesOn lists doc and each table on path in it, nil from the first one
// missing.
func tablesOn(doc map[string]any, path []string) []map[string]any {
	tables := make([]map[string]any, 1, len(path)+1)
	tables[0] = doc

	for _, key := range path {
		tables = append(tables, table(tables[len(tables)-1], key))
	}

	return tables
}

// codexState reads the state in the table at path in doc, reporting whether
// the table holds one.
func codexState(doc map[string]any, path []string) (State, bool) {
	enabled, ok := tablesOn(doc, path)[len(path)]["enabled"].(bool)
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
		maps.Copy(exts[same].config, ext.config)
		exts[same].contents = mergeContents(exts[same].contents, ext.contents)
	}

	return exts
}

// mergeContents adds more, one agent's contents of a plugin, to contents,
// another agent's. A skill both have costs the higher of its two costs, and
// is a By-name skill only when it is one in both.
func mergeContents(contents, more []Content) []Content {
	for _, content := range more {
		same := slices.IndexFunc(contents, func(c Content) bool { return c.Kind == content.Kind && c.Name == content.Name })
		if same < 0 {
			contents = append(contents, content)

			continue
		}

		contents[same].Cost = max(contents[same].Cost, content.Cost)
		contents[same].ByName = contents[same].ByName && content.ByName
		contents[same].Description = cmp.Or(contents[same].Description, content.Description)
	}

	return contents
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

	plugin := newExtension(Plugin, key)
	plugin.Description, plugin.contents = man.Description, contents
	plugin.cost[Codex] = contentsCost(contents)
	plugin.Locations = []Location{{Path: dir, Agent: Codex}}

	return append([]Extension{plugin}, pluginServers(Codex, key, dir, man)...)
}
