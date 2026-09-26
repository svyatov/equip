package equip_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// pluginServer installs the Claude Code plugin github@official, named gh in
// its manifest, with the MCP server search in its .mcp.json.
func pluginServer(t *testing.T, machine *equiptest.Machine) {
	t.Helper()

	dir := machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name": "gh"}`)
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
}

// search returns the MCP server search among the contents of github@official.
func search(t *testing.T, session *equip.Session) equip.Content {
	t.Helper()

	contents := session.Detail("github@official").Contents

	i := slices.IndexFunc(contents, func(c equip.Content) bool { return c.Name == "search" })
	if i < 0 {
		t.Fatalf("Contents = %+v, want search among them", contents)
	}

	return contents[i]
}

func TestPluginMCPServerShowsOnlyAmongItsPluginsContents(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)

	if got, want := names(open(t, machine, repo)), []string{"github@official"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestFirstOpenImportsAPluginMCPServerSetOffByHandInClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)
	writeFile(t, claudeJSON(machine), `{"projects": {"`+repo+`": {"disabledMcpServers": ["plugin:gh:search"]}}}`)

	got := search(t, newSession(t, machine, repo))
	if got.State != equip.Off || !got.Override {
		t.Errorf("search = %+v, want an off Override", got)
	}
}

func TestSaveTurnsAPluginMCPServerOffInCodex(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, filepath.Join(codexPlugin(t, machine, "github@official"), ".mcp.json"),
		`{"mcpServers": {"search": {"command": "search"}}}`)
	trust(t, machine, repo)
	session := newSession(t, machine, repo)

	session.SetState(search(t, session).Key, equip.Off)
	save(t, session)

	want := map[string]any{"github@official": map[string]any{
		"mcp_servers": map[string]any{"search": map[string]any{"enabled": false}},
	}}
	if got := readTOML(t, codexProject(repo))["plugins"]; !reflect.DeepEqual(got, want) {
		t.Errorf("plugins = %v, want %v", got, want)
	}
}

// codexPluginServer installs the Codex plugin github@official with the MCP
// server search in its .mcp.json, in a trusted repo, and returns the repo.
func codexPluginServer(t *testing.T, machine *equiptest.Machine) string {
	t.Helper()

	repo := machine.Repo("app")
	writeFile(t, filepath.Join(codexPlugin(t, machine, "github@official"), ".mcp.json"),
		`{"mcpServers": {"search": {"command": "search"}}}`)
	trust(t, machine, repo)

	return repo
}

func TestDroppingAPluginMCPServerOverrideRemovesItsCodexTables(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := codexPluginServer(t, machine)
	writeFile(t, codexProject(repo), "model = \"o3\"\n")
	session := newSession(t, machine, repo)
	key := search(t, session).Key
	session.SetState(key, equip.Off)
	save(t, session)

	session.DropOverride(key)
	save(t, session)

	if got, want := readTOML(t, codexProject(repo)), map[string]any{"model": "o3"}; !reflect.DeepEqual(got, want) {
		t.Errorf(".codex/config.toml = %v, want %v", got, want)
	}
}

func TestPluginMCPServerDefaultsToItsStateInTheCodexUserConfig(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := codexPluginServer(t, machine)
	appendFile(t, machine.CodexConfig(), "[plugins.\"github@official\".mcp_servers.search]\nenabled = false\n")

	got := search(t, newSession(t, machine, repo))
	if got.State != equip.Off || got.Override {
		t.Errorf("search = %+v, want off with no Override", got)
	}
}

func TestPluginMCPServerBothAgentsHaveIsOneContentThatSaveTurnsOffInBoth(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := codexPluginServer(t, machine)
	pluginServer(t, machine)
	session := newSession(t, machine, repo)

	contents := session.Detail("github@official").Contents

	servers := make([]string, 0, len(contents))
	for _, c := range contents {
		servers = append(servers, c.Name)
	}

	if want := []string{"search"}; !slices.Equal(servers, want) {
		t.Fatalf("Contents = %q, want %q", servers, want)
	}

	session.SetState(search(t, session).Key, equip.Off)
	save(t, session)

	if got, want := projectEntry(t, machine, repo)["disabledMcpServers"], []any{"plugin:gh:search"}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("disabledMcpServers = %v, want %v", got, want)
	}

	if _, ok := readTOML(t, codexProject(repo))["plugins"]; !ok {
		t.Error(".codex/config.toml has no plugins table, want the server off there")
	}
}

func TestSavedPluginMCPServerOverrideIsSavedOnReopen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)
	session := newSession(t, machine, repo)
	session.SetState(search(t, session).Key, equip.Off)
	save(t, session)

	reopened := newSession(t, machine, repo)
	if got := search(t, reopened); got.State != equip.Off || !got.Override {
		t.Errorf("search = %+v, want an off Override", got)
	}

	if got := reopened.View().Unsaved; got != 0 {
		t.Errorf("Unsaved = %d, want 0", got)
	}
}

func TestPluginMCPServerFollowsItsPluginWithoutAnOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)
	session := newSession(t, machine, repo)

	session.SetState("github@official", equip.Off)

	if got := search(t, session); got.State != equip.Off || got.Override {
		t.Errorf("search = %+v, want off with no Override", got)
	}
}

func TestSaveTurnsAPluginMCPServerOffUnderItsManifestNameInClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)
	session := newSession(t, machine, repo)

	session.SetState(search(t, session).Key, equip.Off)
	save(t, session)

	if got, want := projectEntry(t, machine, repo)["disabledMcpServers"], []any{"plugin:gh:search"}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("disabledMcpServers = %v, want %v", got, want)
	}
}
