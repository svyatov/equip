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
)

// installedPlugins is Claude Code's ~/.claude/plugins/installed_plugins.json
// as equip reads it.
type installedPlugins struct {
	Plugins map[string][]pluginInstall `json:"plugins"`
}

// pluginInstall is one install of a plugin.
type pluginInstall struct {
	ProjectPath string `json:"projectPath"` // a project or local install's
	InstallPath string `json:"installPath"`
}

// marketplaceOf is the marketplace in key, a plugin's name@marketplace. A
// skill's name has no @, and an MCP server's may, so neither has one.
func marketplaceOf(key string) string {
	if strings.HasPrefix(key, mcpPrefix) {
		return ""
	}

	_, marketplace, _ := strings.Cut(key, "@")

	return marketplace
}

// keyKind is the kind of the extension with key. An Override may name an
// extension that is not installed, so only its key tells its kind.
func keyKind(key string) Kind {
	switch {
	case strings.HasPrefix(key, mcpPrefix):
		return MCPServer
	case marketplaceOf(key) != "":
		return Plugin
	}

	return Skill
}

// discoverPlugins finds the Claude Code plugins installed for the Project.
func discoverPlugins(machine Machine, project Project) ([]Extension, error) {
	path := filepath.Join(machine.Home, ".claude", "plugins", "installed_plugins.json")

	data, err := os.ReadFile(path) //nolint:gosec // equip builds the path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	var installed installedPlugins
	if err == nil {
		err = json.Unmarshal(data, &installed)
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Claude Code merges enabledPlugins key by key; the project's shared
	// settings win over the user's. The local file is equip's to write.
	defaults := map[string]json.RawMessage{}

	for _, dir := range []string{machine.Home, project.Path} {
		settings, err := readSettings(filepath.Join(dir, ".claude", "settings.json"))
		if err != nil {
			return nil, err
		}

		maps.Copy(defaults, settings.entries[Plugin])
	}

	exts := make([]Extension, 0, len(installed.Plugins))

	for key, installs := range installed.Plugins {
		// A user or managed install has no project path and applies to every
		// Project. ponytail: takes the first install that applies; prefer this
		// Project's own if its version ever matters.
		applies := slices.IndexFunc(installs, func(in pluginInstall) bool {
			return in.ProjectPath == "" || in.ProjectPath == project.Path
		})
		if applies < 0 {
			continue
		}

		dir := installs[applies].InstallPath
		// Claude Code loads nothing it has no files of.
		_, err := os.Stat(dir)
		if err != nil {
			continue
		}

		exts = append(exts, readPlugin(key, dir, defaults[key]))
	}

	return exts, nil
}

// manifest is a plugin's .claude-plugin/plugin.json as equip reads it.
type manifest struct {
	DefaultEnabled *bool           `json:"defaultEnabled"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	MCPServers     json.RawMessage `json:"mcpServers"` // a map, a path or a list of either
	Hooks          json.RawMessage `json:"hooks"`      // loaded with hooks/hooks.json
}

// readPlugin reads the plugin key from its dir in the plugin cache. setting is
// its enabledPlugins entry in the settings equip does not write.
func readPlugin(key, dir string, setting json.RawMessage) Extension {
	var man manifest
	// The manifest is optional; one Claude Code cannot read reads as none.
	data, _ := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json")) //nolint:gosec // equip builds the path
	_ = json.Unmarshal(data, &man)
	// With no manifest name, the marketplace entry name names the plugin.
	man.Name = cmp.Or(man.Name, strings.TrimSuffix(key, "@"+marketplaceOf(key)))

	fallback, set := pluginState(setting)
	if !set {
		fallback = On
		if man.DefaultEnabled != nil && !*man.DefaultEnabled {
			fallback = Off
		}
	}

	contents := pluginSkills(ClaudeCode, dir, man.Name)
	cost := listingCost(dir, man.Name)

	for _, content := range contents {
		cost += content.Cost
	}

	contents = append(contents, pluginServers(dir, man)...)

	_, err := os.Stat(filepath.Join(dir, "hooks", "hooks.json"))

	return Extension{
		Kind: Plugin, Key: key, Description: man.Description, cost: map[Agent]int{ClaudeCode: cost}, fallback: fallback,
		Locations: []Location{{Path: dir, Agent: ClaudeCode}}, contents: contents, hooks: err == nil || man.Hooks != nil,
		lists: mcpLists{on: "", off: "", settings: false}, builtIn: false,
	}
}

// pluginSkills reads the skills of the plugin named name in dir, with their
// costs in agent.
func pluginSkills(agent Agent, dir, name string) []Content {
	var contents []Content

	skills := filepath.Join(dir, "skills")
	entries, _ := os.ReadDir(skills) // a plugin may have no skills

	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(skills, entry.Name(), "SKILL.md")) //nolint:gosec // equip builds the path
		if err != nil {
			continue
		}

		contents = append(contents, Content{
			Name: entry.Name(), Description: field(data, "description"), Kind: Skill, State: On,
			// Both agents list it under the plugin's name.
			Cost: skillCost(agent, name+":"+entry.Name(), data),
		})
	}

	return contents
}

// listingCost estimates the tokens of the commands and agents of the plugin
// named name in dir. Claude Code lists commands as skills, and agents the
// same way. ponytail: reads the default dirs only, not paths the manifest
// names.
func listingCost(dir, name string) int {
	cost := 0

	for _, sub := range []string{"commands", "agents"} {
		entries, _ := os.ReadDir(filepath.Join(dir, sub))

		for _, entry := range entries {
			stem, isMarkdown := strings.CutSuffix(entry.Name(), ".md")
			if !isMarkdown {
				continue
			}

			data, _ := os.ReadFile(filepath.Join(dir, sub, entry.Name())) //nolint:gosec // equip builds the path
			cost += skillCost(ClaudeCode, name+":"+stem, data)
		}
	}

	return cost
}

// pluginServers reads the MCP servers of the plugin in dir, with manifest man.
// An MCP server's cost stays unknown until it is measured, so it is 0.
func pluginServers(dir string, man manifest) []Content {
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // equip builds the path
	_ = json.Unmarshal(data, &mcp)
	// ponytail: reads servers the manifest declares inline, not in the files
	// or bundles it names.
	_ = json.Unmarshal(man.MCPServers, &mcp.Servers)

	contents := make([]Content, 0, len(mcp.Servers))
	for _, name := range slices.Sorted(maps.Keys(mcp.Servers)) {
		contents = append(contents, Content{Name: name, Description: "", Kind: MCPServer, State: On, Cost: 0})
	}

	return contents
}
