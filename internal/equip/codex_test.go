package equip_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func TestViewListsACodexPluginFromTheUserConfigWithFilesInTheCache(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.CodexPlugin("github@official")
	machine.CodexPlugin("unnamed@official")
	writeFile(t, machine.CodexConfig(), "[plugins.\"github@official\"]\nenabled = true\n"+
		"[plugins.\"gone@official\"]\nenabled = true\n")
	session := newSession(t, machine, repo)

	if got, want := names(session.View()), []string{"github@official"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	detail := session.Detail("github@official")
	want := []equip.Agent{equip.Codex}

	if !slices.Equal(detail.Agents, want) || detail.Description != "The github plugin." {
		t.Errorf("Agents, Description = %v, %q, want %v, %q", detail.Agents, detail.Description, want, "The github plugin.")
	}
}

// codexPlugin installs the Codex plugin key in machine's user config and
// returns its dir in the plugin cache.
func codexPlugin(t *testing.T, machine *equiptest.Machine, key string) string {
	t.Helper()

	dir := machine.CodexPlugin(key)
	appendFile(t, machine.CodexConfig(), "[plugins.\""+key+"\"]\nenabled = true\n")

	return dir
}

// appendFile appends content to path, creating it.
func appendFile(t *testing.T, path, content string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = file.WriteString(content)
		err = errors.Join(err, file.Close())
	}

	if err != nil {
		t.Fatal(err)
	}
}

func TestPluginBothAgentsHaveIsOneRow(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	claudeDir := machine.Plugin("github@official", "user", "")
	codexDir := codexPlugin(t, machine, "github@official")
	session := newSession(t, machine, repo)

	if got, want := names(session.View()), []string{"github@official"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	want := []equip.Location{{Path: claudeDir, Agent: equip.ClaudeCode}, {Path: codexDir, Agent: equip.Codex}}
	if got := session.Detail("github@official").Locations; !slices.Equal(got, want) {
		t.Errorf("Locations = %v, want %v", got, want)
	}
}

func TestViewListsCodexMCPServersFromTheUserConfigAsOneRowWithClaudeCodes(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	writeFile(t, machine.CodexConfig(), "[mcp_servers.github]\ncommand = \"gh\"\n[mcp_servers.search]\ncommand = \"s\"\n")
	session := newSession(t, machine, repo)

	got := states(session.View())
	if want := []string{"MCP server github on", "MCP server search on"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	want := []equip.Location{
		{Path: claudeJSON(machine), Agent: equip.ClaudeCode}, {Path: machine.CodexConfig(), Agent: equip.Codex},
	}
	if got := session.Detail("mcp:github").Locations; !slices.Equal(got, want) {
		t.Errorf("Locations = %v, want %v", got, want)
	}
}

func TestCodexExtensionDefaultsToItsStateInTheUserConfig(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.CodexPlugin("github@official")
	writeFile(t, machine.CodexConfig(), "[plugins.\"github@official\"]\nenabled = false\n"+
		"[mcp_servers.search]\ncommand = \"s\"\nenabled = false\n")

	view := open(t, machine, repo)
	if got := states(view); !slices.Equal(got, []string{"plugin github@official off", "MCP server search off"}) {
		t.Errorf("rows = %q, want both off", got)
	}

	if got := row(t, view, "search"); got.Override || got.Fallback != equip.Off {
		t.Errorf("row = %+v, want an off default with no Override", got)
	}
}

// codexProject is the Codex config of project.
func codexProject(project string) string { return filepath.Join(project, ".codex", "config.toml") }

// trust marks project trusted in machine's Codex user config.
func trust(t *testing.T, machine *equiptest.Machine, project string) {
	t.Helper()
	appendFile(t, machine.CodexConfig(), "[projects.\""+project+"\"]\ntrust_level = \"trusted\"\n")
}

func TestViewListsCodexExtensionsOfTheProjectConfigOnlyWhenTrusted(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	other := machine.Repo("other")
	machine.CodexPlugin("github@official")
	trust(t, machine, other)
	appendFile(t, machine.CodexConfig(), "[mcp_servers.db]\ncommand = \"db\"\n")
	writeFile(t, codexProject(repo), "[plugins.\"github@official\"]\nenabled = true\n[mcp_servers.db]\ncwd = \"/\"\n")

	if got, want := names(open(t, machine, repo)), []string{"db"}; !slices.Equal(got, want) {
		t.Errorf("untrusted: rows = %q, want %q", got, want)
	}

	trust(t, machine, repo)
	session := newSession(t, machine, repo)

	if got, want := names(session.View()), []string{"db", "github@official"}; !slices.Equal(got, want) {
		t.Fatalf("trusted: rows = %q, want %q", got, want)
	}

	want := []equip.Location{
		{Path: machine.CodexConfig(), Agent: equip.Codex}, {Path: codexProject(repo), Agent: equip.Codex},
	}
	if got := session.Detail("mcp:db").Locations; !slices.Equal(got, want) {
		t.Errorf("Locations = %v, want %v", got, want)
	}
}

// noCodexConfig fails the test when project has a Codex config.
func noCodexConfig(t *testing.T, project string) {
	t.Helper()

	_, err := os.Stat(codexProject(project))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat .codex/config.toml = %v, want it missing", err)
	}
}

// readTOML reads the TOML file at path.
func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any

	err = toml.Unmarshal(data, &doc)
	if err != nil {
		t.Fatal(err)
	}

	return doc
}

func TestSaveWritesEnabledIntoTheTrustedProjectsCodexConfigKeepingOtherKeys(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	appendFile(t, machine.CodexConfig(), "[mcp_servers.search]\ncommand = \"s\"\n")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "model = \"o3\"\n[mcp_servers.search]\nstartup_timeout_sec = 5\n")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)
	session.SetState("mcp:search", equip.Off)

	save(t, session)

	want := map[string]any{
		"model":       "o3",
		"plugins":     map[string]any{"github@official": map[string]any{"enabled": false}},
		"mcp_servers": map[string]any{"search": map[string]any{"startup_timeout_sec": int64(5), "enabled": false}},
	}
	if got := readTOML(t, codexProject(repo)); !reflect.DeepEqual(got, want) {
		t.Errorf(".codex/config.toml = %v, want %v", got, want)
	}

	if view := session.View(); view.Unsaved != 0 {
		t.Errorf("Unsaved = %d after save, want 0", view.Unsaved)
	}
}

func TestSavingADroppedOverrideRemovesItsCodexEntry(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	appendFile(t, machine.CodexConfig(), "[mcp_servers.search]\ncommand = \"s\"\n")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "[mcp_servers.search]\nstartup_timeout_sec = 5\n")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)
	session.SetState("mcp:search", equip.Off)
	save(t, session)

	session.DropOverride("github@official")
	session.DropOverride("mcp:search")
	save(t, session)

	want := map[string]any{"mcp_servers": map[string]any{"search": map[string]any{"startup_timeout_sec": int64(5)}}}
	if got := readTOML(t, codexProject(repo)); !reflect.DeepEqual(got, want) {
		t.Errorf(".codex/config.toml = %v, want %v", got, want)
	}
}

func TestUnknownCodexValueIsNotImportedAndSaveKeepsIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	codexPlugin(t, machine, "sentry@official")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "[plugins.\"github@official\"]\nenabled = \"yes\"\n")
	session := newSession(t, machine, repo)

	if got := row(t, session.View(), "github@official"); got.Override {
		t.Errorf("row = %+v, want no Override", got)
	}

	session.SetState("sentry@official", equip.Off)
	save(t, session)

	want := map[string]any{"plugins": map[string]any{
		"github@official": map[string]any{"enabled": "yes"}, "sentry@official": map[string]any{"enabled": false},
	}}
	if got := readTOML(t, codexProject(repo)); !reflect.DeepEqual(got, want) {
		t.Errorf(".codex/config.toml = %v, want %v", got, want)
	}
}

func TestSaveWithNoCodexChangeLeavesTheCodexConfigAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	machine.Skill(machine.ClaudeSkills(), "review")
	trust(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	noCodexConfig(t, repo)
}

func TestSaveInAnUntrustedProjectIgnoresABrokenCodexConfig(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, codexProject(repo), "[")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)
}

func TestSkillStateIsNotWrittenForCodexInATrustedProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// "review" and "The review skill.": 23 bytes, 6 tokens in Codex.
	machine.Skill(machine.CodexSkills(), "review")
	trust(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	noCodexConfig(t, repo)

	if got := row(t, session.View(), "review").Cost; got != 6 {
		t.Errorf("Cost = %d, want 6", got)
	}
}

func TestSaveExcludesTheCodexConfigItWritesFromGit(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "model = \"o3\"\n")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	save(t, session)

	if st := machine.RunGit(repo, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status shows %q, want nothing", st)
	}
}

func TestCodexEntrySetByHandAfterASaveIsChangedOutsideInCodex(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)
	save(t, session)
	writeFile(t, codexProject(repo), "[plugins.\"github@official\"]\nenabled = true\n")

	session = newSession(t, machine, repo)

	got := row(t, session.View(), "github@official")
	if got.State != equip.On || !got.Override || !got.Unsaved || !got.ChangedOutside {
		t.Errorf("row = %+v, want an unsaved on Override changed outside", got)
	}

	if got := session.Detail("github@official").ChangedIn; got != equip.Codex {
		t.Errorf("ChangedIn = %v, want Codex", got)
	}
}

func TestFirstOpenImportsAHandSetCodexEntryAsAnOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	appendFile(t, machine.CodexConfig(), "[mcp_servers.search]\ncommand = \"s\"\n")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "[mcp_servers.search]\nenabled = false\n")

	got := row(t, open(t, machine, repo), "search")
	if got.State != equip.Off || !got.Override || got.Fallback != equip.On || got.Unsaved || got.ChangedOutside {
		t.Errorf("row = %+v, want a saved off Override over an on default", got)
	}
}

func TestSaveImportsACodexEntryChangedOutsideSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.On)
	writeFile(t, codexProject(repo), "[plugins.\"github@official\"]\nenabled = false\n")

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	got := row(t, session.View(), "github@official")
	if got.State != equip.Off || !got.Override || !got.Unsaved || !got.ChangedOutside {
		t.Errorf("row = %+v, want an unsaved off Override changed outside", got)
	}

	if data, _ := os.ReadFile(codexProject(repo)); !strings.Contains(string(data), "false") {
		t.Errorf(".codex/config.toml = %q, want the hand edit", data)
	}
}

func TestCodexPluginCostsItsSkillsListedUnderItsName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// "github:review" and "The review skill.": 30 bytes, 8 tokens in Codex.
	machine.Skill(filepath.Join(codexPlugin(t, machine, "github@official"), "skills"), "review")
	session := newSession(t, machine, repo)

	if got := row(t, session.View(), "github@official").Cost; got != 8 {
		t.Errorf("Cost = %d, want 8", got)
	}

	want := []equip.Content{
		{Name: "review", Description: "The review skill.", Kind: equip.Skill, State: equip.On, Cost: 8},
	}
	if got := session.Detail("github@official").Contents; !slices.Equal(got, want) {
		t.Errorf("Contents = %+v, want %+v", got, want)
	}
}

func TestOffCodexPluginCostsNothingOnlyWhereCodexAppliesIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// "github:review" and "The review skill.": 30 bytes, 8 tokens in Codex.
	machine.Skill(filepath.Join(codexPlugin(t, machine, "github@official"), "skills"), "review")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	// Codex still loads it: its skill, the skills intro and the plugins block.
	if got := session.View(); row(t, got, "github@official").Cost != 8 || got.Totals[equip.Codex] != 958 {
		t.Errorf("untrusted: Cost = %d, Codex total = %d, want 8 and 958",
			row(t, got, "github@official").Cost, got.Totals[equip.Codex])
	}

	trust(t, machine, repo)
	session = newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	if got := session.View(); row(t, got, "github@official").Cost != 0 || got.Totals[equip.Codex] != 0 {
		t.Errorf("trusted: Cost = %d, Codex total = %d, want 0 and 0",
			row(t, got, "github@official").Cost, got.Totals[equip.Codex])
	}
}

func TestCodexTotalCountsThePluginsBlockOnce(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	codexPlugin(t, machine, "sentry@official")
	trust(t, machine, repo)

	// The block is about 1,000 bytes: 250 tokens.
	want := map[equip.Agent]int{equip.ClaudeCode: 0, equip.Codex: 250}
	if got := open(t, machine, repo).Totals; !maps.Equal(got, want) {
		t.Errorf("Totals = %v, want %v", got, want)
	}
}

func TestUntrustedProjectGetsNoCodexEntriesAndTheDetailSaysWhy(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	save(t, session)

	noCodexConfig(t, repo)

	if got := session.Detail("github@official").NotApplied[equip.Codex]; !strings.Contains(got, "not trusted") {
		t.Errorf("NotApplied[Codex] = %q, want the Project not trusted", got)
	}

	if view := session.View(); view.Unsaved != 0 {
		t.Errorf("Unsaved = %d after save, want 0", view.Unsaved)
	}
}

func TestTrackedCodexConfigIsReadButNotWritten(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	trust(t, machine, repo)

	const config = "[mcp_servers.db]\ncommand = \"db\"\nenabled = false\n"
	writeFile(t, codexProject(repo), config)
	machine.RunGit(repo, "add", ".codex/config.toml")
	session := newSession(t, machine, repo)

	if got := row(t, session.View(), "db"); got.State != equip.Off || got.Override {
		t.Errorf("row = %+v, want an off default with no Override", got)
	}

	session.SetState("mcp:db", equip.On)
	save(t, session)

	if data, _ := os.ReadFile(codexProject(repo)); string(data) != config {
		t.Errorf(".codex/config.toml = %q, want it as it was", data)
	}

	if got := session.Detail("mcp:db").NotApplied[equip.Codex]; !strings.Contains(got, "tracked by git") {
		t.Errorf("NotApplied[Codex] = %q, want the file tracked by git", got)
	}
}
