package equip_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// claudeJSON is Claude Code's own ~/.claude.json.
func claudeJSON(machine *equiptest.Machine) string {
	return filepath.Join(machine.Home, ".claude.json")
}

// projectEntry reads the entry of project in ~/.claude.json.
func projectEntry(t *testing.T, machine *equiptest.Machine, project string) map[string]any {
	t.Helper()

	projects, _ := readJSON(t, claudeJSON(machine))["projects"].(map[string]any)

	entry, ok := projects[project].(map[string]any)
	if !ok {
		t.Fatalf("~/.claude.json has no entry for %s", project)
	}

	return entry
}

// states lists each row as its kind, name and state.
func states(view equip.View) []string {
	out := make([]string, 0, len(view.Rows))
	for _, r := range view.Rows {
		out = append(out, r.Kind.String()+" "+r.Name+" "+r.State.String())
	}

	return out
}

func TestViewListsAUserMCPServerByItsName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)

	view := open(t, machine, repo)
	if got, want := names(view), []string{"github"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	if got := view.Rows[0]; got.Kind != equip.MCPServer || got.State != equip.On {
		t.Errorf("row = %+v, want an MCP server that is on", got)
	}
}

func TestViewListsLocalMCPServersOfThisProjectOnly(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	other := machine.Repo("other")
	writeFile(t, claudeJSON(machine), `{"projects": {
		"`+repo+`": {"mcpServers": {"db": {"command": "db"}}},
		"`+other+`": {"mcpServers": {"theirs": {"command": "theirs"}}}
	}}`)

	if got, want := names(open(t, machine, repo)), []string{"db"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewListsTheProjectsMCPJSONServers(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)

	if got, want := names(open(t, machine, repo)), []string{"search"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestMCPServerInSeveralPlacesIsOneRowWithEachLocation(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}},
		"projects": {"`+repo+`": {"mcpServers": {"github": {"command": "gh"}}}}}`)
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)

	if got, want := names(session.View()), []string{"github"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	want := []equip.Location{
		{Path: claudeJSON(machine), Agent: equip.ClaudeCode},
		{Path: filepath.Join(repo, ".mcp.json"), Agent: equip.ClaudeCode},
	}
	if got := session.Detail("mcp:github").Locations; !slices.Equal(got, want) {
		t.Errorf("Locations = %v, want %v", got, want)
	}
}

func TestSkillAndMCPServerWithOneNameKeepTheirOwnStates(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "github")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)

	session.SetState("mcp:github", equip.Off)

	got := states(session.View())
	if want := []string{"skill github on", "MCP server github off"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

// withBuiltins gives machine Claude Code's real built-in MCP servers, which a
// test machine leaves out.
func withBuiltins(t *testing.T, machine *equiptest.Machine) {
	t.Helper()

	fromEnv, err := equip.MachineFromEnv()
	if err != nil {
		t.Fatal(err)
	}

	machine.ClaudeBuiltins = fromEnv.ClaudeBuiltins
}

func TestViewListsClaudeCodesBuiltInMCPServersWithTheirDefaults(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	withBuiltins(t, machine)

	got := states(open(t, machine, repo))
	if want := []string{"MCP server claude-in-chrome on", "MCP server computer-use off"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestMCPServerOffersOnlyOnAndOff(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)

	if got, want := session.Detail("mcp:github").States, []equip.State{equip.On, equip.Off}; !slices.Equal(got, want) {
		t.Errorf("States = %v, want %v", got, want)
	}

	session.SetState("mcp:github", equip.ManualOnly)

	if got := session.View().Rows[0]; got.State != equip.On || got.Override {
		t.Errorf("row = %+v, want on with no Override", got)
	}
}

func TestSaveWritesAUserServerOffToDisabledMCPServersInClaudeJSON(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"numStartups": 5, "mcpServers": {"github": {"command": "gh"}},
		"projects": {"/elsewhere": {"allowedTools": []}, "`+repo+`": {"allowedTools": ["Bash(a && b)"]}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.Off)

	save(t, session)

	want := map[string]any{
		"numStartups": 5.0,
		"mcpServers":  map[string]any{"github": map[string]any{"command": "gh"}},
		"projects": map[string]any{
			"/elsewhere": map[string]any{"allowedTools": []any{}},
			repo:         map[string]any{"allowedTools": []any{"Bash(a && b)"}, "disabledMcpServers": []any{"github"}},
		},
	}
	if got := readJSON(t, claudeJSON(machine)); !reflect.DeepEqual(got, want) {
		t.Errorf("~/.claude.json = %v, want %v", got, want)
	}

	data, err := os.ReadFile(claudeJSON(machine))
	if err != nil || !strings.Contains(string(data), "a && b") {
		t.Errorf("~/.claude.json = %s, %v, want the permission rule unescaped", data, err)
	}
}

func TestSavedMCPServerOverrideReopensSaved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.Off)

	save(t, session)

	if n := session.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d after save, want 0", n)
	}

	view := open(t, machine, repo)
	if got := view.Rows[0]; got.State != equip.Off || !got.Override || got.Unsaved || got.ChangedOutside {
		t.Errorf("reopened row = %+v, want a saved off Override", got)
	}
}

func TestSaveRecordsMCPServerOverridesByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.Off)

	save(t, session)

	want := map[string]any{
		"skills": map[string]any{}, "plugins": map[string]any{}, "mcp_servers": map[string]any{"github": "off"},
	}
	if got := readRecord(t, machine)["overrides"]; !reflect.DeepEqual(got, want) {
		t.Errorf("overrides = %v, want %v", got, want)
	}
}

func TestSaveWritesADefaultOffBuiltInOnToEnabledMCPServers(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	withBuiltins(t, machine)
	session := newSession(t, machine, repo)
	session.SetState("mcp:computer-use", equip.On)
	session.SetState("mcp:claude-in-chrome", equip.Off)

	save(t, session)

	want := map[string]any{"enabledMcpServers": []any{"computer-use"}, "disabledMcpServers": []any{"claude-in-chrome"}}
	if got := projectEntry(t, machine, repo); !reflect.DeepEqual(got, want) {
		t.Errorf("project entry = %v, want %v", got, want)
	}
}

func TestSavedOnOverrideOfADefaultOnServerReopensSaved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.On)

	save(t, session)

	view := open(t, machine, repo)
	if got := view.Rows[0]; got.State != equip.On || !got.Override || got.Unsaved {
		t.Errorf("reopened row = %+v, want a saved on Override", got)
	}
}

func TestSaveWritesMCPJSONServersAsExplicitEntriesInSettingsLocal(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"db": {"command": "db"}, "search": {"command": "s"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:db", equip.On)
	session.SetState("mcp:search", equip.Off)

	save(t, session)

	settings := readJSON(t, settingsLocal(repo))
	if got, want := settings["enabledMcpjsonServers"], []any{"db"}; !reflect.DeepEqual(got, want) {
		t.Errorf("enabledMcpjsonServers = %v, want %v", got, want)
	}

	if got, want := settings["disabledMcpjsonServers"], []any{"search"}; !reflect.DeepEqual(got, want) {
		t.Errorf("disabledMcpjsonServers = %v, want %v", got, want)
	}

	_, err := os.Stat(claudeJSON(machine))
	if err == nil {
		t.Error("save wrote ~/.claude.json, want no file")
	}
}

func TestSaveWritesAServerInSeveralPlacesWhereClaudeCodeTakesItFrom(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// Local wins over .mcp.json, which wins over user.
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"search": {"command": "s"}},
		"projects": {"`+repo+`": {"mcpServers": {"db": {"command": "db"}}}}}`)
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"db": {"command": "db"}, "search": {"command": "s"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:db", equip.Off)
	session.SetState("mcp:search", equip.Off)

	save(t, session)

	project := projectEntry(t, machine, repo)
	if got, want := project["disabledMcpServers"], []any{"db"}; !reflect.DeepEqual(got, want) {
		t.Errorf("disabledMcpServers = %v, want %v", got, want)
	}

	if got, want := readJSON(t, settingsLocal(repo))["disabledMcpjsonServers"], []any{"search"}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("disabledMcpjsonServers = %v, want %v", got, want)
	}
}

func TestFirstOpenImportsADisabledServerAndSaveKeepsEntriesNamingNothingDiscovered(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}},
		"projects": {"`+repo+`": {"disabledMcpServers": ["claude.ai Gmail", "github", "plugin:sentry:sentry"]}}}`)
	session := newSession(t, machine, repo)

	if got := session.View().Rows[0]; got.State != equip.Off || !got.Override {
		t.Fatalf("row = %+v, want an off Override", got)
	}

	session.DropOverride("mcp:github")
	save(t, session)

	project := projectEntry(t, machine, repo)
	if got, want := project["disabledMcpServers"], []any{"claude.ai Gmail", "plugin:sentry:sentry"}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("disabledMcpServers = %v, want %v", got, want)
	}
}

func TestSaveImportsAServerDisabledOutsideSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}},
		"projects": {"`+repo+`": {"disabledMcpServers": ["github"]}}}`)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	got := session.View().Rows[0]
	if got.State != equip.Off || !got.Override || !got.Unsaved || !got.ChangedOutside {
		t.Errorf("row = %+v, want an unsaved off Override changed outside", got)
	}
}

func TestOpenImportsServersARecordFromBeforeMCPServersDoesNotName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}},
		"projects": {"`+repo+`": {"disabledMcpServers": ["github"]}}}`)
	savedSkillOff(t, machine, repo, `{"skillOverrides": {"review": "off"}}`,
		"path = '"+repo+"'\n[overrides.skills]\nreview = 'off'\n[overrides.plugins]\n")

	view := open(t, machine, repo)

	got := row(t, view, "github")
	if got.State != equip.Off || !got.Override || got.Unsaved || got.ChangedOutside {
		t.Errorf("row = %+v, want a saved off Override", got)
	}

	if view.Unsaved != 0 {
		t.Errorf("Unsaved = %d, want 0", view.Unsaved)
	}
}

func TestFirstOpenImportsMCPJSONEntriesInSettingsLocal(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"db": {"command": "db"}, "search": {"command": "s"}}}`)
	// A rejection wins over an approval, as in Claude Code.
	writeFile(t, settingsLocal(repo),
		`{"enabledMcpjsonServers": ["db", "search"], "disabledMcpjsonServers": ["search"]}`)

	view := open(t, machine, repo)
	if got, want := states(view), []string{"MCP server db on", "MCP server search off"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}

	if db, search := row(t, view, "db"), row(t, view, "search"); !db.Override || !search.Override {
		t.Errorf("rows = %+v and %+v, want both Overrides", db, search)
	}
}

func TestMCPServerNamedWithAnAtHasNoMarketplace(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"db@staging": {"command": "db"}}}`)

	if got := newSession(t, machine, repo).Detail("mcp:db@staging").Marketplace; got != "" {
		t.Errorf("Marketplace = %q, want none", got)
	}
}

func TestOpenRefusesABrokenClaudeJSON(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), "{")

	_, err := equip.Open(machine.Machine, repo)
	if err == nil {
		t.Error("Open = nil, want an error")
	}
}

func TestSaveInAWorktreeWritesUnderTheMainCheckoutInClaudeJSON(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Commit(repo)
	tree := machine.Worktree(repo, "feature")
	// Claude Code 2.1.282 started in a worktree keys its entry on the main
	// checkout too.
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}, "projects": {"`+repo+`": {
		"mcpServers": {"db": {"command": "db"}}, "disabledMcpServers": ["claude.ai Gmail", "db"]}}}`)
	session := newSession(t, machine, tree)

	if got := row(t, session.View(), "db"); got.State != equip.Off {
		t.Errorf("db row = %+v, want off", got)
	}

	session.SetState("mcp:github", equip.Off)
	save(t, session)

	want := []any{"claude.ai Gmail", "db", "github"}
	if got := projectEntry(t, machine, repo)["disabledMcpServers"]; !reflect.DeepEqual(
		got, want) {
		t.Errorf("disabledMcpServers = %v, want %v", got, want)
	}
}
