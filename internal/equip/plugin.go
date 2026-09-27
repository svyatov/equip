package equip

import (
	"cmp"
	"encoding/json"
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

// discoverPlugins finds the Claude Code plugins installed for the Project,
// with its sharedSettings.
func discoverPlugins(machine Machine, project Project, sharedSettings []settingsFile) ([]Extension, error) {
	path := filepath.Join(machine.Home, ".claude", "plugins", "installed_plugins.json")

	var installed installedPlugins

	missing, err := readDoc(path, json.Unmarshal, &installed)
	if missing || err != nil {
		return nil, err
	}

	// The local file is equip's to write.
	defaults := sharedEntries(sharedSettings, Plugin)

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

		exts = append(exts, readPlugin(key, dir, defaults[key])...)
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

// readPlugin reads the plugin key from its dir in the plugin cache, followed
// by its MCP servers. setting is its enabledPlugins entry in the settings
// equip does not write.
func readPlugin(key, dir string, setting json.RawMessage) []Extension {
	man := readManifest(key, dir, ".claude-plugin")

	fallback, set := pluginState(setting)
	if !set {
		fallback = On
		if man.DefaultEnabled != nil && !*man.DefaultEnabled {
			fallback = Off
		}
	}

	contents := pluginSkills(ClaudeCode, dir, man.Name)

	_, err := os.Stat(filepath.Join(dir, "hooks", "hooks.json"))

	plugin := newExtension(Plugin, key)
	plugin.Description, plugin.contents, plugin.hooks = man.Description, contents, err == nil || man.Hooks != nil
	plugin.fallback[ClaudeCode] = fallback
	plugin.cost[ClaudeCode] = contentsCost(contents) + listingCost(dir, man.Name)
	plugin.Locations = []Location{{Path: dir, Agent: ClaudeCode}}

	return append([]Extension{plugin}, pluginServers(ClaudeCode, key, dir, man)...)
}

// readManifest reads the manifest of the plugin key in dir, in the sub dir
// its agent reads.
func readManifest(key, dir, sub string) manifest {
	var man manifest
	// The manifest is optional; one the agent cannot read reads as none.
	data, _ := os.ReadFile(filepath.Join(dir, sub, "plugin.json")) //nolint:gosec // equip builds the path
	_ = json.Unmarshal(data, &man)
	// With no manifest name, the marketplace entry name names the plugin.
	man.Name = cmp.Or(man.Name, strings.TrimSuffix(key, "@"+marketplaceOf(key)))

	return man
}

// pluginSkills reads the skills of the plugin named name in dir, with their
// costs in agent.
func pluginSkills(agent Agent, dir, name string) []Content {
	var contents []Content

	skills := filepath.Join(dir, "skills")
	entries, _ := os.ReadDir(skills) // a plugin may have no skills

	for _, entry := range entries {
		dir := filepath.Join(skills, entry.Name())

		data, loads := readSkill(agent, dir)
		if !loads {
			continue
		}

		// Both agents list it under the plugin's name.
		cost := skillCost(agent, dir, name+":"+entry.Name(), data)
		contents = append(contents, Content{
			Key: "", Name: entry.Name(), Description: field(data, "description"), Kind: Skill, State: On, Override: false,
			Unsaved: false, ChangedOutside: false, ChangedIn: ClaudeCode, CostUnknown: false, Unmeasurable: "",
			Cost: cost, ByName: cost == 0, // a skill costs nothing only when its name alone calls it
		})
	}

	return contents
}

// contentsCost is the sum of the costs of contents.
func contentsCost(contents []Content) int {
	cost := 0
	for _, content := range contents {
		cost += content.Cost
	}

	return cost
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
			cost += skillCost(ClaudeCode, "", name+":"+stem, data)
		}
	}

	return cost
}

// pluginServers reads the MCP servers agent has from the plugin key in dir,
// with manifest man. An MCP server's cost stays unknown until it is measured,
// so it is 0.
func pluginServers(agent Agent, key, dir string, man manifest) []Extension {
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // equip builds the path
	_ = json.Unmarshal(data, &mcp)
	// ponytail: reads servers the manifest declares inline, not in the files
	// or bundles it names.
	_ = json.Unmarshal(man.MCPServers, &mcp.Servers)

	exts := make([]Extension, 0, len(mcp.Servers))
	for name := range mcp.Servers {
		server := newExtension(MCPServer, mcpPrefix+key+":"+name)
		server.config[agent] = mcp.Servers[name]
		server.Locations = []Location{{Path: dir, Agent: agent}}
		server.lists, server.plugin, server.server = claudeJSONLists(On), key, name
		// Claude Code 2.1.283 names a plugin's server by the plugin's manifest
		// name, which falls back to its marketplace entry name. ponytail: two
		// plugins with the same manifest name share an entry, so one's Override
		// undoes the other's; key the lists by that name if such plugins show
		// up.
		server.listed = "plugin:" + man.Name + ":" + name
		exts = append(exts, server)
	}

	return exts
}
