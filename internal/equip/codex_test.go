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
	machine.CodexPlugin("github@official")
	machine.CodexPlugin("unnamed@official")
	writeFile(t, machine.CodexConfig(), "[plugins.\"github@official\"]\nenabled = true\n"+
		"[plugins.\"gone@official\"]\nenabled = true\n")
	session := newSession(t, machine, machine.Root)

	if got, want := names(session.View()), []string{"github@official"}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	detail := session.Detail("github@official")
	want := []equip.Agent{equip.Codex}

	if !slices.Equal(detail.Agents, want) || detail.Description != "The github plugin." {
		t.Errorf("Agents, Description = %v, %q, want %v, %q", detail.Agents, detail.Description, want, "The github plugin.")
	}
}

func TestCodexSkipsAPluginSkillWithASymlinkedSkillFile(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	skills := filepath.Join(codexPlugin(t, machine, "github@official"), "skills")
	src := machine.Skill(filepath.Join(machine.Root, "repos", "tools"), "source-name")
	machine.Skill(skills, "review")
	machine.Mkdir(filepath.Join(skills, "linked"))

	err := os.Symlink(filepath.Join(src, "SKILL.md"), filepath.Join(skills, "linked", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	contents := newSession(t, machine, machine.Root).Detail("github@official").Contents

	got := make([]string, 0, len(contents))
	for _, content := range contents {
		got = append(got, content.Name)
	}

	if want := []string{"review"}; !slices.Equal(got, want) {
		t.Errorf("contents = %q, want %q", got, want)
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
	claudeDir := machine.Plugin("github@official", "user", "")
	codexDir := codexPlugin(t, machine, "github@official")
	session := newSession(t, machine, machine.Root)

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
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	writeFile(t, machine.CodexConfig(), "[mcp_servers.github]\ncommand = \"gh\"\n[mcp_servers.search]\ncommand = \"s\"\n")
	session := newSession(t, machine, machine.Root)

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
	machine.CodexPlugin("github@official")
	writeFile(t, machine.CodexConfig(), "[plugins.\"github@official\"]\nenabled = false\n"+
		"[mcp_servers.search]\ncommand = \"s\"\nenabled = false\n")

	view := open(t, machine, machine.Root)
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

func TestSaveRemovesTheCodexEntryOfAServerGoneFromTheUserConfig(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	writeFile(t, machine.CodexConfig(), "[mcp_servers.search]\ncommand = \"s\"\n")
	trust(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("mcp:search", equip.Off)
	save(t, session)
	// Codex refuses a server table with no command or url.
	writeFile(t, machine.CodexConfig(), "")
	trust(t, machine, repo)
	session = newSession(t, machine, repo)

	if view := session.View(); len(view.Rows) != 0 || view.Unsaved != 1 {
		t.Errorf("rows = %q, Unsaved = %d, want none and 1", names(view), view.Unsaved)
	}

	save(t, session)

	if got := readTOML(t, codexProject(repo)); len(got) != 0 {
		t.Errorf(".codex/config.toml = %v, want it empty", got)
	}

	if view := session.View(); view.Unsaved != 0 {
		t.Errorf("Unsaved = %d after save, want 0", view.Unsaved)
	}
}

func TestServerDefinedOnlyInTheProjectsCodexConfigIsKept(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "[mcp_servers.db]\ncommand = \"db\"\nenabled = false\n")
	session := newSession(t, machine, repo)

	if got := states(session.View()); !slices.Equal(got, []string{"MCP server db off"}) {
		t.Errorf("rows = %q, want db off", got)
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

// savedCodex saves the change to a trusted Project whose Codex config holds
// before, with the github@official plugin and the db and search MCP servers
// in the user config, and returns the Project's Codex config after the save.
func savedCodex(t *testing.T, before string, change func(*equip.Session)) string {
	t.Helper()

	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	appendFile(t, machine.CodexConfig(), "[mcp_servers.db]\ncommand = \"db\"\n[mcp_servers.search]\ncommand = \"s\"\n")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), before)
	session := newSession(t, machine, repo)
	change(session)
	save(t, session)

	data, err := os.ReadFile(codexProject(repo))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestSaveChangesOnlyTheEnabledValueInTheCodexConfig(t *testing.T) {
	t.Parallel()

	before := "# my precious comment\nmodel = \"gpt-5\" # inline note\n\n" +
		"[mcp_servers.search]\nstartup_timeout_sec = 5\nenabled = true # on for now\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.SetState("mcp:search", equip.Off) })

	want := "# my precious comment\nmodel = \"gpt-5\" # inline note\n\n" +
		"[mcp_servers.search]\nstartup_timeout_sec = 5\nenabled = false # on for now\n"
	if got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSaveInsertsEnabledUnderTheCodexTableWithoutIt(t *testing.T) {
	t.Parallel()

	before := "# notes\n[mcp_servers.search] # the search server\nstartup_timeout_sec = 5\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.SetState("mcp:search", equip.Off) })

	want := "# notes\n[mcp_servers.search] # the search server\nenabled = false\nstartup_timeout_sec = 5\n"
	if got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSaveAppendsTheCodexTableOfAPluginWithoutOne(t *testing.T) {
	t.Parallel()

	before := "# notes\n[mcp_servers.search]\nstartup_timeout_sec = 5\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.SetState("github@official", equip.Off) })

	want := before + "\n[plugins.\"github@official\"]\nenabled = false\n"
	if got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSaveIntoAnEmptyCodexConfigStartsWithTheTable(t *testing.T) {
	t.Parallel()

	got := savedCodex(t, "", func(s *equip.Session) {
		s.SetState("mcp:search", equip.Off)
		s.SetState("github@official", equip.On)
	})

	want := "[plugins.\"github@official\"]\nenabled = true\n\n[mcp_servers.search]\nenabled = false\n"
	if got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSavingADroppedOverrideRemovesOnlyItsEnabledLine(t *testing.T) {
	t.Parallel()

	before := "# notes\n[mcp_servers.search]\nstartup_timeout_sec = 5\n  enabled = false # off\n# after\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.DropOverride("mcp:search") })

	want := "# notes\n[mcp_servers.search]\nstartup_timeout_sec = 5\n# after\n"
	if got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSavingADroppedOverrideRemovesTheCodexTableItWasAlone(t *testing.T) {
	t.Parallel()

	before := "# top\n[mcp_servers.db]\ncwd = \"/\"\n[mcp_servers.search] # note\nenabled = false\n# end\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.DropOverride("mcp:search") })

	if want := "# top\n[mcp_servers.db]\ncwd = \"/\"\n# end\n"; got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSavingADroppedOverrideRemovesTheParentCodexTablesItLeavesEmpty(t *testing.T) {
	t.Parallel()

	before := "# top\n[mcp_servers]\n[mcp_servers.db]\nenabled = true\n[mcp_servers.search]\nenabled = false\n# end\n"
	got := savedCodex(t, before, func(s *equip.Session) {
		s.DropOverride("mcp:db")
		s.DropOverride("mcp:search")
	})

	if want := "# top\n# end\n"; got != want {
		t.Errorf(".codex/config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestSavingADroppedOverrideOfADottedCodexKeyWritesTheConfigInFull(t *testing.T) {
	t.Parallel()

	before := "[mcp_servers]\nsearch.startup_timeout_sec = 5\nsearch.enabled = false\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.DropOverride("mcp:search") })

	var doc map[string]any

	err := toml.Unmarshal([]byte(got), &doc)
	want := map[string]any{"mcp_servers": map[string]any{"search": map[string]any{"startup_timeout_sec": int64(5)}}}

	if err != nil || !reflect.DeepEqual(doc, want) {
		t.Errorf(".codex/config.toml = %v, %v, want %v", doc, err, want)
	}
}

func TestSavingDroppedOverridesOfAnInlineAndAFirstCodexTableRemovesBoth(t *testing.T) {
	t.Parallel()

	before := "[mcp_servers.db]\nenabled = false\n[mcp_servers]\nsearch = { enabled = false }\n"
	got := savedCodex(t, before, func(s *equip.Session) {
		s.DropOverride("mcp:db")
		s.DropOverride("mcp:search")
	})

	var doc map[string]any

	err := toml.Unmarshal([]byte(got), &doc)
	if err != nil || len(doc) != 0 {
		t.Errorf(".codex/config.toml = %v, %v, want it empty", doc, err)
	}
}

func TestSaveSetsTheStateOfAnInlineCodexTable(t *testing.T) {
	t.Parallel()

	before := "[mcp_servers]\nsearch = { startup_timeout_sec = 5 }\n"
	got := savedCodex(t, before, func(s *equip.Session) { s.SetState("mcp:search", equip.Off) })

	var doc map[string]any

	err := toml.Unmarshal([]byte(got), &doc)
	want := map[string]any{"mcp_servers": map[string]any{
		"search": map[string]any{"startup_timeout_sec": int64(5), "enabled": false},
	}}

	if err != nil || !reflect.DeepEqual(doc, want) {
		t.Errorf(".codex/config.toml = %v, %v, want %v", doc, err, want)
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

func TestCodexConfigTrackedThroughASymlinkedDirIsNotWritten(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)

	const config = "model = \"o3\"\n"
	writeFile(t, filepath.Join(repo, "tools", "codex", "config.toml"), config)

	err := os.Symlink(filepath.Join("tools", "codex"), filepath.Join(repo, ".codex"))
	if err != nil {
		t.Fatal(err)
	}

	machine.RunGit(repo, "add", ".")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	save(t, session)

	if data, _ := os.ReadFile(filepath.Join(repo, "tools", "codex", "config.toml")); string(data) != config {
		t.Errorf("tools/codex/config.toml = %q, want it as it was", data)
	}

	if got := session.Detail("github@official").NotApplied[equip.Codex]; !strings.Contains(got, "tracked by git") {
		t.Errorf("NotApplied[Codex] = %q, want the file tracked by git", got)
	}
}

func TestBrokenCodexUserConfigStillOpensAndSaves(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, machine.CodexConfig(), "[")
	session := newSession(t, machine, machine.Root)

	if got, want := names(session.View()), []string{"review"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}

	session.SetState("review", equip.Off)
	save(t, session)
}

func TestBrokenTrustedProjectCodexConfigIsNotAppliedAndSaysWhy(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "[")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	save(t, session)

	if got := session.Detail("github@official").NotApplied[equip.Codex]; !strings.Contains(got, codexProject(repo)) {
		t.Errorf("NotApplied[Codex] = %q, want the unreadable file named", got)
	}

	if data, _ := os.ReadFile(codexProject(repo)); string(data) != "[" {
		t.Errorf(".codex/config.toml = %q, want it as it was", data)
	}
}

func TestSaveAfterAFailedCodexWriteKeepsWhatClaudeCodeGotAsEquipsOwn(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root writes every dir")
	}

	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)
	writeFile(t, codexProject(repo), "")
	locked := filepath.Join(repo, ".codex")

	err := os.Chmod(locked, 0o555)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	session.SetState("github@official", equip.Off)

	_, err = session.Save()
	if err == nil {
		t.Fatal("Save = nil, want an error")
	}

	err = os.Chmod(locked, 0o755)
	if err != nil {
		t.Fatal(err)
	}

	save(t, session)

	if got := row(t, session.View(), "review"); got.ChangedOutside || got.Unsaved {
		t.Errorf("review row = %+v, want saved and not changed outside", got)
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

// codexWorktree makes a linked worktree of a committed repo whose main
// checkout the Codex user config trusts, with the Codex plugin github. It
// returns the main checkout and the worktree.
func codexWorktree(t *testing.T, machine *equiptest.Machine) (string, string) {
	t.Helper()

	repo := machine.Repo("app")
	machine.Commit(repo)
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)

	return repo, machine.Worktree(repo, "app-feature")
}

func TestSaveInAWorktreeWritesTheWorktreesCodexConfig(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo, tree := codexWorktree(t, machine)
	session := newSession(t, machine, tree)
	session.SetState("github@official", equip.Off)

	save(t, session)

	want := map[string]any{"plugins": map[string]any{"github@official": map[string]any{"enabled": false}}}
	if got := readTOML(t, codexProject(tree)); !reflect.DeepEqual(got, want) {
		t.Errorf("worktree .codex/config.toml = %v, want %v", got, want)
	}

	noCodexConfig(t, repo)
}

func TestOpenInAWorktreeReadsTheWorktreesCodexConfig(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo, tree := codexWorktree(t, machine)
	writeFile(t, codexProject(repo), "[plugins.\"github@official\"]\nenabled = true\n")
	writeFile(t, codexProject(tree), "[plugins.\"github@official\"]\nenabled = false\n")

	got := row(t, open(t, machine, tree), "github@official")
	if got.State != equip.Off || !got.Override || got.Unsaved {
		t.Errorf("row = %+v, want a saved off Override", got)
	}
}

func TestWorktreeWithNoCodexConfigOpensWithTheRecordsCodexStatesUnsaved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo, tree := codexWorktree(t, machine)
	main := newSession(t, machine, repo)
	main.SetState("github@official", equip.Off)
	save(t, main)
	session := newSession(t, machine, tree)

	got := row(t, session.View(), "github@official")
	if got.State != equip.Off || !got.Override || !got.Unsaved {
		t.Errorf("row = %+v, want an unsaved off Override", got)
	}

	save(t, session)

	want := map[string]any{"plugins": map[string]any{"github@official": map[string]any{"enabled": false}}}
	if got := readTOML(t, codexProject(tree)); !reflect.DeepEqual(got, want) {
		t.Errorf("worktree .codex/config.toml = %v, want %v", got, want)
	}
}

func TestSaveInAWorktreeFailsOnAnEditToTheWorktreesCodexConfigSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, tree := codexWorktree(t, machine)
	session := newSession(t, machine, tree)
	session.SetState("github@official", equip.On)
	writeFile(t, codexProject(tree), "[plugins.\"github@official\"]\nenabled = false\n")

	_, err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Errorf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}
}

func TestCodexConfigTrackedOnlyInTheWorktreesBranchIsNotWritten(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, tree := codexWorktree(t, machine)

	const config = "model = \"o3\"\n"
	writeFile(t, codexProject(tree), config)
	machine.RunGit(tree, "add", ".codex/config.toml")
	session := newSession(t, machine, tree)
	session.SetState("github@official", equip.Off)

	save(t, session)

	if data, _ := os.ReadFile(codexProject(tree)); string(data) != config {
		t.Errorf(".codex/config.toml = %q, want it as it was", data)
	}
}

func TestSaveInAWorktreeExcludesItsCodexConfigFromGitWhenOnlyTheMainCheckoutIgnoresIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo, tree := codexWorktree(t, machine)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".codex/\n")
	session := newSession(t, machine, tree)
	session.SetState("github@official", equip.Off)

	save(t, session)

	if st := machine.RunGit(tree, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("worktree git status shows %q, want nothing", st)
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

func TestFirstOpenWithAgentsDisagreeingShowsTheCodexStateChangedOutside(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	codexPlugin(t, machine, "github@official")
	trust(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"enabledPlugins": {"github@official": false}}`)
	writeFile(t, codexProject(repo), "[plugins.\"github@official\"]\nenabled = true\n")
	session := newSession(t, machine, repo)

	got := row(t, session.View(), "github@official")
	if got.State != equip.On || !got.Override || !got.Unsaved || !got.ChangedOutside {
		t.Errorf("row = %+v, want an unsaved on Override changed outside", got)
	}

	if got := session.Detail("github@official").ChangedIn; got != equip.Codex {
		t.Errorf("ChangedIn = %v, want Codex", got)
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

	_, err := session.Save()
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
	// "github:review" and "The review skill.": 30 bytes, 8 tokens in Codex.
	machine.Skill(filepath.Join(codexPlugin(t, machine, "github@official"), "skills"), "review")
	session := newSession(t, machine, machine.Root)

	if got := row(t, session.View(), "github@official").Cost; got != 8 {
		t.Errorf("Cost = %d, want 8", got)
	}

	want := []equip.Content{
		{
			Key: "", Name: "review", Description: "The review skill.", Kind: equip.Skill, State: equip.On, Cost: 8,
			CostUnknown: false, ByName: false, Override: false, Unsaved: false, ChangedOutside: false,
			ChangedIn:    equip.ClaudeCode,
			Unmeasurable: "",
		},
	}
	if got := session.Detail("github@official").Contents; !slices.Equal(got, want) {
		t.Errorf("Contents = %+v, want %+v", got, want)
	}
}

func TestCodexPluginSkillThatDisallowsImplicitInvocationIsByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	skill := machine.Skill(filepath.Join(codexPlugin(t, machine, "grill-me@tk"), "skills"), "grill-me")
	writeFile(t, filepath.Join(skill, "agents", "openai.yaml"), "policy:\n  allow_implicit_invocation: false\n")
	session := newSession(t, machine, machine.Root)

	if got := row(t, session.View(), "grill-me@tk"); got.Cost != 0 || !got.ByName {
		t.Errorf("Cost = %d, ByName = %v, want 0 and true", got.Cost, got.ByName)
	}

	if got := session.Detail("grill-me@tk").Contents[0].ByName; !got {
		t.Error("the skill's ByName = false, want true")
	}
}

func TestPluginSkillBothAgentsHaveIsByNameOnlyWhenBothCallItByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	claude := filepath.Join(machine.Plugin("github@official", "user", ""), "skills")
	codex := filepath.Join(codexPlugin(t, machine, "github@official"), "skills")
	// Claude Code only calls ship by name; Codex lists it: "github:ship" and
	// "x", 12 bytes, 3 tokens.
	writeFile(t, filepath.Join(claude, "ship", "SKILL.md"),
		"---\nname: ship\ndescription: x\ndisable-model-invocation: true\n---\n")
	writeFile(t, filepath.Join(codex, "ship", "SKILL.md"), "---\nname: ship\ndescription: x\n---\n")
	// Both only call lint by name.
	writeFile(t, filepath.Join(claude, "lint", "SKILL.md"),
		"---\nname: lint\ndescription: x\ndisable-model-invocation: true\n---\n")
	machine.Skill(codex, "lint")
	writeFile(t, filepath.Join(codex, "lint", "agents", "openai.yaml"), "policy:\n  allow_implicit_invocation: false\n")
	// Only Codex has scan.
	machine.Skill(codex, "scan")

	skills := map[string]equip.Content{}
	for _, content := range newSession(t, machine, machine.Root).Detail("github@official").Contents {
		skills[content.Name] = content
	}

	if got := skills["ship"]; got.ByName || got.Cost != 3 {
		t.Errorf("ship: ByName = %v, Cost = %d, want false and Codex's 3", got.ByName, got.Cost)
	}

	if got := skills["lint"]; !got.ByName {
		t.Error("lint: ByName = false, want true")
	}

	if _, ok := skills["scan"]; !ok {
		t.Errorf("contents = %v, want scan among them", skills)
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
	if got := session.View(); row(t, got, "github@official").Cost != 8 || got.Totals[equip.Codex].Tokens != 958 {
		t.Errorf("untrusted: Cost = %d, Codex total = %d, want 8 and 958",
			row(t, got, "github@official").Cost, got.Totals[equip.Codex].Tokens)
	}

	trust(t, machine, repo)
	session = newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)

	if got := session.View(); row(t, got, "github@official").Cost != 0 || got.Totals[equip.Codex].Tokens != 0 {
		t.Errorf("trusted: Cost = %d, Codex total = %d, want 0 and 0",
			row(t, got, "github@official").Cost, got.Totals[equip.Codex].Tokens)
	}
}

func TestPluginOffInTheCodexUserConfigCostsNothingInAnUntrustedProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.CodexPlugin("github@official"), "skills"), "review")
	writeFile(t, machine.CodexConfig(), "[plugins.\"github@official\"]\nenabled = false\n")

	got := open(t, machine, machine.Root)
	if row(t, got, "github@official").Cost != 0 || got.Totals[equip.Codex].Tokens != 0 {
		t.Errorf("Cost = %d, Codex total = %d, want 0 and 0",
			row(t, got, "github@official").Cost, got.Totals[equip.Codex].Tokens)
	}
}

func TestPluginBothAgentsHaveKeepsEachAgentsDefault(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(machine.CodexPlugin("github@official"), "skills"), "review")
	writeFile(t, machine.CodexConfig(), "[plugins.\"github@official\"]\nenabled = false\n")
	session := newSession(t, machine, machine.Root)

	if got := row(t, session.View(), "github@official"); got.State != equip.On || got.Fallback != equip.On {
		t.Errorf("row = %+v, want Claude Code's on default", got)
	}

	want := map[equip.Agent]int{equip.ClaudeCode: 0, equip.Codex: 0}
	if got := session.Detail("github@official").Costs; !maps.Equal(got, want) {
		t.Errorf("Costs = %v, want %v", got, want)
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
	if got := totalTokens(open(t, machine, repo)); !maps.Equal(got, want) {
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

// globalCodexProject writes a global Codex config that defines db and trusts
// project, whose Codex config it also is, and returns its content.
func globalCodexProject(t *testing.T, machine *equiptest.Machine, project string) string {
	t.Helper()
	appendFile(t, machine.CodexConfig(), "[mcp_servers.db]\ncommand = \"db\"\n")
	trust(t, machine, project)

	data, err := os.ReadFile(machine.CodexConfig())
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// globalCodexLeftAlone checks that the global Codex config still holds config
// and that db's detail names it once, and as why Codex is not applied.
func globalCodexLeftAlone(t *testing.T, machine *equiptest.Machine, session *equip.Session, config string) {
	t.Helper()

	if data, _ := os.ReadFile(machine.CodexConfig()); string(data) != config {
		t.Errorf("global config = %q, want it as it was", data)
	}

	detail := session.Detail("mcp:db")
	if got := detail.NotApplied[equip.Codex]; !strings.Contains(got, "global Codex config") {
		t.Errorf("NotApplied[Codex] = %q, want the global Codex config named", got)
	}

	want := []equip.Location{{Path: machine.CodexConfig(), Agent: equip.Codex}}
	if got := detail.Locations; !slices.Equal(got, want) {
		t.Errorf("Locations = %v, want %v", got, want)
	}
}

func TestSaveInTheHomeDirectoryLeavesTheGlobalCodexConfigAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	config := globalCodexProject(t, machine, machine.Home)
	session := newSession(t, machine, machine.Home)
	session.SetState("mcp:db", equip.Off)
	session.SetState("review", equip.Off)

	save(t, session)

	globalCodexLeftAlone(t, machine, session, config)

	want := map[string]any{"review": "off"}
	if got := readJSON(t, settingsLocal(machine.Home))["skillOverrides"]; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want review off", got)
	}
}

func TestCodexHomeInTheProjectLeavesItsCodexConfigAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.CodexHome = machine.Mkdir(filepath.Join(repo, ".codex"))
	config := globalCodexProject(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("mcp:db", equip.Off)

	save(t, session)

	globalCodexLeftAlone(t, machine, session, config)
}

func TestCodexHomeLinkedToTheProjectLeavesItsCodexConfigAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	link := filepath.Join(machine.Root, "codex")

	err := os.Symlink(machine.Mkdir(filepath.Join(repo, ".codex")), link)
	if err != nil {
		t.Fatal(err)
	}

	machine.CodexHome = link
	config := globalCodexProject(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("mcp:db", equip.Off)

	save(t, session)

	globalCodexLeftAlone(t, machine, session, config)
}
