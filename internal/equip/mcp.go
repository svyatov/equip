package equip

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// mcpPrefix starts the key of an MCP server, as a skill or a plugin may have
// the same name.
const mcpPrefix = "mcp:"

// name is what the list shows of the extension: an MCP server's name without
// its prefix, else its key.
func (e Extension) name() string { return strings.TrimPrefix(e.Key, mcpPrefix) }

// jsonObject is a JSON object with every value kept raw.
type jsonObject map[string]json.RawMessage

// readJSONObject reads the JSON object at path. A missing file reads as an
// empty object.
func readJSONObject(path string) (jsonObject, error) {
	obj := jsonObject{}

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return obj, nil
	}

	if err == nil {
		err = json.Unmarshal(data, &obj)
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return obj, nil
}

// object reads the object under key. Anything else reads as an empty object.
func (o jsonObject) object(key string) jsonObject {
	obj := jsonObject{}
	_ = json.Unmarshal(o[key], &obj)

	return obj
}

// list reads the list of strings under key. Anything else reads as empty.
func (o jsonObject) list(key string) []string {
	var list []string

	_ = json.Unmarshal(o[key], &list)

	return list
}

// setMember puts name in the list under key, or takes it out, reporting
// whether the list changed. It leaves the object alone for no key.
func (o jsonObject) setMember(key, name string, member bool) bool {
	list := o.list(key)
	if key == "" || slices.Contains(list, name) == member {
		return false
	}

	if member {
		list = append(list, name)
	} else {
		list = slices.DeleteFunc(list, func(s string) bool { return s == name })
	}

	o[key], _ = json.Marshal(list) //nolint:errchkjson // a list of strings always encodes

	return true
}

// with is the object with value under key, to encode.
func (o jsonObject) with(key string, value any) map[string]any {
	out := make(map[string]any, len(o)+1)
	for k, raw := range o {
		out[k] = raw
	}

	out[key] = value

	return out
}

// mcpLists are the lists Claude Code keeps an MCP server's state in: the one
// that turns it on and the one that turns it off. Either may be none.
type mcpLists struct {
	on, off  string
	settings bool // in the Project's settings.local.json, else under the Project in ~/.claude.json
}

// claudeJSONLists are the lists of an MCP server whose state Claude Code keeps
// in ~/.claude.json, one that is on or off without an entry. Claude Code lists
// the servers that differ from their default.
func claudeJSONLists(fallback State) mcpLists {
	if fallback == Off {
		return mcpLists{on: "enabledMcpServers", off: "", settings: false}
	}

	return mcpLists{on: "", off: "disabledMcpServers", settings: false}
}

// mcpJSONLists are the lists of a .mcp.json server. Off must be an entry:
// claude -p loads a server neither list names.
func mcpJSONLists() mcpLists {
	return mcpLists{on: "enabledMcpjsonServers", off: "disabledMcpjsonServers", settings: true}
}

// entry reports whether Claude Code's config holds an entry for the extension
// once equip writes st. An MCP server's lists hold none for its default.
func (e Extension) entry(st State) bool {
	return e.Kind != MCPServer || st == On && e.lists.on != "" || st == Off && e.lists.off != ""
}

// state reads name's entry in the lists of obj, reporting whether it has one.
// An entry that turns it off wins, as it does in Claude Code.
func (l mcpLists) state(obj jsonObject, name string) (State, bool) {
	switch {
	case l.off != "" && slices.Contains(obj.list(l.off), name):
		return Off, true
	case l.on != "" && slices.Contains(obj.list(l.on), name):
		return On, true
	}

	return 0, false
}

// write sets name's entries in the lists of obj to st, or removes them with
// no state, reporting whether a list changed.
func (l mcpLists) write(obj jsonObject, name string, st State, set bool) bool {
	on := obj.setMember(l.on, name, set && st == On)
	off := obj.setMember(l.off, name, set && st == Off)

	return on || off
}

// writeLists writes the states of overrides into obj for the MCP servers among
// exts whose lists are in settings.local.json, or else in ~/.claude.json, as
// settings says. It reports whether a list changed.
func writeLists(obj jsonObject, exts []Extension, overrides map[string]State, settings bool) bool {
	changed := false

	for _, ext := range exts {
		if ext.Kind == MCPServer && ext.lists.settings == settings {
			st, set := overrides[ext.Key]
			changed = ext.lists.write(obj, ext.name(), st, set) || changed
		}
	}

	return changed
}

// writeClaudeJSON writes the states of overrides for exts, the MCP servers
// Claude Code keeps in ~/.claude.json, under the Project there. It keeps every
// other key, and leaves the file alone when no list changes, as Claude Code
// rewrites it too.
func writeClaudeJSON(machine Machine, project Project, exts []Extension, overrides map[string]State) error {
	path := claudeJSONPath(machine)

	config, err := readJSONObject(path)
	if err != nil {
		return err
	}

	projects := config.object("projects")
	entry := projects.object(project.Path)

	if !writeLists(entry, exts, overrides, false) {
		return nil
	}

	data, err := indentJSON(config.with("projects", projects.with(project.Path, entry)))
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	return writeFile(path, data)
}

// claudeJSONPath is Claude Code's own ~/.claude.json. It keeps each Project's
// entry under projects["<path>"]. Claude Code 2.1.282 takes the path of the git
// root it starts in, and for a worktree follows .git and its commondir back to
// the main checkout, so the path is Project.Path in a worktree too.
func claudeJSONPath(machine Machine) string { return filepath.Join(machine.Home, ".claude.json") }

// discoverServers finds the MCP servers Claude Code has for the Project,
// without starting any.
func discoverServers(machine Machine, project Project) ([]Extension, error) {
	path := claudeJSONPath(machine)

	config, err := readJSONObject(path)
	if err != nil {
		return nil, err
	}

	mcpJSONPath := filepath.Join(project.checkout, ".mcp.json")

	mcpJSON, err := readJSONObject(mcpJSONPath)
	if err != nil {
		return nil, err
	}

	byName := map[string]*Extension{}
	// Claude Code takes a server named in several places from the last of
	// these, and keeps its state in that place's lists.
	addServers(byName, config.object("mcpServers"), path, claudeJSONLists(On))
	addServers(byName, mcpJSON.object("mcpServers"), mcpJSONPath, mcpJSONLists())
	addServers(byName, config.object("projects").object(project.Path).object("mcpServers"), path, claudeJSONLists(On))

	for name, fallback := range machine.ClaudeBuiltins {
		byName[name] = &Extension{
			Kind: MCPServer, Key: mcpPrefix + name, Description: "", cost: map[Agent]int{}, fallback: fallback,
			Locations: []Location{{Path: "built into Claude Code", Agent: ClaudeCode}}, contents: nil, hooks: false,
			lists: claudeJSONLists(fallback),
		}
	}

	exts := make([]Extension, 0, len(byName))
	for _, ext := range byName {
		exts = append(exts, *ext)
	}

	return exts, nil
}

// addServers adds each server in servers, read from path, to byName, with its
// state in lists.
func addServers(byName map[string]*Extension, servers jsonObject, path string, lists mcpLists) {
	for name := range servers {
		ext := byName[name]
		if ext == nil {
			ext = &Extension{
				Kind: MCPServer, Key: mcpPrefix + name, Description: "", cost: map[Agent]int{}, fallback: On,
				Locations: nil, contents: nil, hooks: false, lists: lists,
			}
			byName[name] = ext
		}

		ext.lists = lists

		loc := Location{Path: path, Agent: ClaudeCode}
		if !slices.Contains(ext.Locations, loc) {
			ext.Locations = append(ext.Locations, loc)
		}
	}
}
