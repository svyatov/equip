package equip_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func TestViewListsOnlyPluginsInstalledForThisProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	other := machine.Repo("other")
	machine.Plugin("mine@official", "local", repo)
	machine.Plugin("shared@official", "project", repo)
	machine.Plugin("theirs@official", "local", other)
	machine.Plugin("team@official", "project", other)

	got := names(open(t, machine, repo))
	if want := []string{"mine@official", "shared@official"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewSkipsAPluginWithoutFilesInTheCache(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("kept@official", "user", "")

	err := os.RemoveAll(machine.Plugin("gone@official", "user", ""))
	if err != nil {
		t.Fatal(err)
	}

	got := names(open(t, machine, machine.Root))
	if want := []string{"kept@official"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestPluginOffersOnlyOnAndOff(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, machine.Root)

	got, want := session.Detail("github@official").States, []equip.State{equip.On, equip.Off}
	if !slices.Equal(got, want) {
		t.Errorf("plugin States = %v, want %v", got, want)
	}

	want = []equip.State{equip.On, equip.ManualOnly, equip.Off}
	if got := session.Detail("review").States; !slices.Equal(got, want) {
		t.Errorf("skill States = %v, want %v", got, want)
	}
}

func TestSettingAPluginToManualOnlyChangesNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	session := newSession(t, machine, machine.Root)

	session.SetState("github@official", equip.ManualOnly)

	if got := row(t, session.View(), "github@official"); got.State != equip.On || got.Override {
		t.Errorf("row = %+v, want on with no Override", got)
	}
}

func TestSaveWritesEnabledPluginsForClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	machine.Plugin("sentry@official", "user", "")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)
	session.SetState("sentry@official", equip.On)

	save(t, session)

	settings := readJSON(t, settingsLocal(repo))
	if want := map[string]any{"github@official": false, "sentry@official": true}; !reflect.DeepEqual(
		settings["enabledPlugins"], want) {
		t.Errorf("enabledPlugins = %v, want %v", settings["enabledPlugins"], want)
	}

	if got, ok := settings["skillOverrides"]; ok {
		t.Errorf("skillOverrides = %v, want none", got)
	}
}

func TestSaveRecordsPluginOverridesApartFromSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("github@official", equip.Off)
	session.SetState("review", equip.ManualOnly)

	save(t, session)

	want := map[string]any{
		"skills":      map[string]any{"review": "manual-only"},
		"plugins":     map[string]any{"github@official": "off"},
		"mcp_servers": map[string]any{},
	}
	if got := readRecord(t, machine)["overrides"]; !reflect.DeepEqual(got, want) {
		t.Errorf("overrides = %v, want %v", got, want)
	}

	if got := row(t, open(t, machine, repo), "github@official"); got.State != equip.Off || !got.Override || got.Unsaved {
		t.Errorf("reopened row = %+v, want a saved off Override", got)
	}
}

func TestPluginSkillHasARowThatFollowsItsPlugin(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// "github:review" and "The review skill.": 30 bytes, 10 tokens in Claude Code.
	machine.Skill(filepath.Join(machine.Plugin("github@official", "user", ""), "skills"), "review")
	session := newSession(t, machine, machine.Root)

	want := equip.Row{
		Key: "github@official/review", Name: "review", Plugin: "github@official", Kind: equip.Skill, Cost: 10,
		State: equip.On, Fallback: equip.On, CostUnknown: false, ByName: false, Override: false, Unsaved: false,
		ChangedOutside: false,
	}
	if got := row(t, session.View(), "review"); got != want {
		t.Errorf("row = %+v, want %+v", got, want)
	}

	if got := session.View().Totals[equip.ClaudeCode].Tokens; got != 10 {
		t.Errorf("Claude Code total = %d, want 10, the skill counted once", got)
	}

	session.SetState("github@official", equip.Off)

	if got := row(t, session.View(), "review"); got.State != equip.Off || got.Cost != 0 {
		t.Errorf("row after the plugin turns off = %+v, want off with no cost", got)
	}
}

func TestTurnedSinceLeavesOutAPluginsSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Plugin("github@official", "user", ""), "skills"), "review")
	session := newSession(t, machine, machine.Root)
	before := session.View()

	session.SetState("github@official", equip.Off)

	turned := session.View().TurnedSince(before)
	if len(turned) != 1 || turned[0].Key != "github@official" {
		t.Errorf("turned = %+v, want the plugin only", turned)
	}
}

func TestPluginSkillTakesNoOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Plugin("github@official", "user", ""), "skills"), "review")
	session := newSession(t, machine, machine.Root)

	session.SetState("github@official/review", equip.Off)

	if got := session.View().Unsaved; got != 0 {
		t.Errorf("Unsaved = %d, want 0, as the skill follows its plugin", got)
	}
}

func TestPluginSkillJoinsNoPreset(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Plugin("github@official", "user", ""), "skills"), "review")
	session := newSession(t, machine, machine.Root)

	id, err := session.CreatePreset("Ruby")
	if err != nil {
		t.Fatal(err)
	}

	err = session.AddMember(id, "github@official/review")
	if err == nil {
		t.Error("AddMember of a plugin's skill = nil, want an error")
	}

	if got := session.Presets()[0].Members; len(got) != 0 {
		t.Errorf("members = %+v, want none", got)
	}
}

func TestPluginSkillDetailOffersNoState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(dir, "skills"), "review")

	session := newSession(t, machine, machine.Root)
	if none := session.Detail("github@official/nope"); none.Description != "" || none.Locations != nil {
		t.Errorf("Detail of a skill the plugin lacks = %+v, want an empty one", none)
	}

	got := session.Detail("github@official/review")
	if got.Description != "The review skill." || got.States != nil || got.Contents != nil ||
		got.Marketplace != "official" {
		t.Errorf("Detail = %+v, want the skill's description, no states, no contents, from official", got)
	}

	if got.Costs[equip.ClaudeCode] != 10 || len(got.Locations) != 1 || got.Locations[0].Path != dir {
		t.Errorf("Costs = %v, Locations = %v, want 10 in Claude Code at %s", got.Costs, got.Locations, dir)
	}
}

func TestPluginDefaultsToItsStateInUserSettings(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(machine.Home, ".claude", "settings.json"), `{"enabledPlugins": {"github@official": false}}`)

	want := equip.Row{
		Key: "github@official", Name: "github@official", Kind: equip.Plugin, Cost: 0, State: equip.Off, Fallback: equip.Off,
		Plugin: "", CostUnknown: false, ByName: false, Override: false, Unsaved: false, ChangedOutside: false,
	}
	if got := row(t, open(t, machine, machine.Root), "github@official"); got != want {
		t.Errorf("row = %+v, want %+v", got, want)
	}
}

func TestPluginDefaultsToItsStateInProjectSettingsOverUserSettings(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(machine.Home, ".claude", "settings.json"), `{"enabledPlugins": {"github@official": true}}`)
	writeFile(t, filepath.Join(repo, ".claude", "settings.json"), `{"enabledPlugins": {"github@official": false}}`)

	if got := row(t, open(t, machine, repo), "github@official"); got.State != equip.Off || got.Override {
		t.Errorf("row = %+v, want off with no Override", got)
	}
}

func TestPluginNoSettingsNameDefaultsToItsManifest(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name": "github", "defaultEnabled": false}`)

	if got := row(t, open(t, machine, machine.Root), "github@official"); got.State != equip.Off || got.Override {
		t.Errorf("row = %+v, want off with no Override", got)
	}
}

func TestPluginSettingOnWinsOverItsManifest(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name": "github", "defaultEnabled": false}`)
	writeFile(t, filepath.Join(machine.Home, ".claude", "settings.json"), `{"enabledPlugins": {"github@official": true}}`)

	if got := row(t, open(t, machine, machine.Root), "github@official"); got.State != equip.On || got.Override {
		t.Errorf("row = %+v, want on with no Override", got)
	}
}

func TestPluginDetailShowsItsMarketplaceAndDescription(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")

	detail := newSession(t, machine, machine.Root).Detail("github@official")
	if detail.Marketplace != "official" || detail.Description != "The github plugin." {
		t.Errorf("Marketplace, Description = %q, %q, want %q, %q",
			detail.Marketplace, detail.Description, "official", "The github plugin.")
	}
}

func TestPluginDetailListsItsSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	// Claude Code lists the skill under the manifest name: "gh:review" and
	// "The review skill.", 26 bytes.
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name": "gh"}`)
	machine.Skill(filepath.Join(dir, "skills"), "review")

	want := []equip.Content{
		{
			Key: "", Name: "review", Description: "The review skill.", Kind: equip.Skill, State: equip.On, Cost: 9,
			CostUnknown: false, ByName: false, Override: false, Unsaved: false, ChangedOutside: false,
			ChangedIn:    equip.ClaudeCode,
			Unmeasurable: "",
		},
	}
	if got := newSession(t, machine, machine.Root).Detail("github@official").Contents; !slices.Equal(got, want) {
		t.Errorf("Contents = %+v, want %+v", got, want)
	}
}

func TestPluginOfSkillsOnlyTheirNamesCallIsByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	writeFile(t, filepath.Join(machine.Plugin("github@official", "user", ""), "skills", "review", "SKILL.md"),
		"---\nname: review\ndescription: x\ndisable-model-invocation: true\n---\n")
	machine.Plugin("empty@official", "user", "")

	session := newSession(t, machine, machine.Root)
	if got := session.Detail("github@official").Contents; len(got) != 1 || !got[0].ByName {
		t.Errorf("Contents = %+v, want review by name", got)
	}

	view := session.View()
	for name, want := range map[string]bool{"github@official": true, "empty@official": false} {
		if got := row(t, view, name).ByName; got != want {
			t.Errorf("%s: ByName = %v, want %v", name, got, want)
		}
	}
}

func TestPluginSkillFollowsItsPlugin(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Plugin("github@official", "user", ""), "skills"), "review")
	session := newSession(t, machine, machine.Root)

	session.SetState("github@official", equip.Off)

	want := []equip.Content{
		{
			Key: "", Name: "review", Description: "The review skill.", Kind: equip.Skill, State: equip.Off, Cost: 0,
			CostUnknown: false, ByName: false, Override: false, Unsaved: false, ChangedOutside: false,
			ChangedIn:    equip.ClaudeCode,
			Unmeasurable: "",
		},
	}
	if got := session.Detail("github@official").Contents; !slices.Equal(got, want) {
		t.Errorf("Contents = %+v, want %+v", got, want)
	}
}

func TestPluginCostsItsSkillsCommandsAndAgents(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	// "github:review" and "The review skill.": 30 bytes, 10 tokens.
	machine.Skill(filepath.Join(dir, "skills"), "review")
	// "github:sync" and "Sync it.": 19 bytes, 7 tokens.
	writeFile(t, filepath.Join(dir, "commands", "sync.md"), "---\ndescription: Sync it.\n---\nThe body is not counted.\n")
	// "github:triage" and "Triage.": 20 bytes, 7 tokens.
	writeFile(t, filepath.Join(dir, "agents", "triage.md"), "---\nname: triage\ndescription: Triage.\n---\nNot counted.\n")

	if got := row(t, open(t, machine, machine.Root), "github@official").Cost; got != 24 {
		t.Errorf("Cost = %d, want 24", got)
	}
}

func TestPluginCostSkipsFilesClaudeCodeDoesNotList(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Mkdir(filepath.Join(dir, "skills", "empty"))
	writeFile(t, filepath.Join(dir, "commands", "sync.toml"), "description = \"Sync it.\"\n")

	session := newSession(t, machine, machine.Root)

	if got := row(t, session.View(), "github@official").Cost; got != 0 {
		t.Errorf("Cost = %d, want 0", got)
	}

	if got := session.Detail("github@official").Contents; len(got) != 0 {
		t.Errorf("Contents = %+v, want none", got)
	}
}

func TestPluginWithoutManifestListsItsSkillsUnderItsKeyName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	// "github:review" and "The review skill.": 30 bytes, 10 tokens.
	machine.Skill(filepath.Join(dir, "skills"), "review")

	err := os.RemoveAll(filepath.Join(dir, ".claude-plugin"))
	if err != nil {
		t.Fatal(err)
	}

	if got := row(t, open(t, machine, machine.Root), "github@official").Cost; got != 10 {
		t.Errorf("Cost = %d, want 10", got)
	}
}

func TestPluginDetailListsItsMCPServersAfterItsSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(dir, "skills"), "review")
	writeFile(t, filepath.Join(dir, ".mcp.json"),
		`{"mcpServers": {"search": {"command": "search"}, "issues": {"url": "https://example.com/mcp"}}}`)

	contents := newSession(t, machine, machine.Root).Detail("github@official").Contents

	got := make([]string, 0, len(contents))
	for _, content := range contents {
		got = append(got, content.Kind.String()+" "+content.Name)
	}

	if want := []string{"skill review", "MCP server issues", "MCP server search"}; !slices.Equal(got, want) {
		t.Errorf("Contents = %q, want %q", got, want)
	}
}

func TestPluginDetailListsMCPServersDeclaredInItsManifest(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name": "github", "mcpServers": {"api": {"command": "api"}}, "defaultEnabled": false}`)
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)

	detail := newSession(t, machine, machine.Root).Detail("github@official")

	got := make([]string, 0, len(detail.Contents))
	for _, content := range detail.Contents {
		got = append(got, content.Name)
	}

	if want := []string{"api", "search"}; !slices.Equal(got, want) {
		t.Errorf("Contents = %q, want %q", got, want)
	}
}

func TestPluginManifestWithMCPServersInAFileKeepsItsOtherFields(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name": "github", "mcpServers": "./servers.json", "defaultEnabled": false}`)

	if got := row(t, open(t, machine, machine.Root), "github@official").State; got != equip.Off {
		t.Errorf("State = %v, want off", got)
	}
}

func TestPluginWithAHooksFileHasHooks(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("plain@official", "user", "")
	writeFile(t, filepath.Join(machine.Plugin("hooked@official", "user", ""), "hooks", "hooks.json"), `{"hooks": {}}`)
	session := newSession(t, machine, machine.Root)

	if !session.Detail("hooked@official").Hooks || session.Detail("plain@official").Hooks {
		t.Errorf("Hooks = %v and %v, want true for hooked and false for plain",
			session.Detail("hooked@official").Hooks, session.Detail("plain@official").Hooks)
	}
}

func TestPluginWithHooksInItsManifestHasHooks(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	writeFile(t, filepath.Join(machine.Plugin("hooked@official", "user", ""), ".claude-plugin", "plugin.json"),
		`{"name": "hooked", "hooks": "./hooks/extra.json"}`)

	if !newSession(t, machine, machine.Root).Detail("hooked@official").Hooks {
		t.Error("Hooks = false, want true")
	}
}

func TestOpenRefusesABrokenInstalledPluginsList(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	writeFile(t, filepath.Join(machine.Home, ".claude", "plugins", "installed_plugins.json"), "{")

	_, err := equip.Open(machine.Machine, machine.Root)
	if err == nil {
		t.Error("Open = nil, want an error")
	}
}

func TestOpenRefusesBrokenSharedSettings(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	writeFile(t, filepath.Join(repo, ".claude", "settings.json"), "{")

	_, err := equip.Open(machine.Machine, repo)
	if err == nil {
		t.Error("Open = nil, want an error")
	}
}

func TestUnknownPluginValueOnDiskIsNotImportedAndSaveKeepsIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"enabledPlugins": {"github@official": "yes"}}`)
	session := newSession(t, machine, repo)

	if got := row(t, session.View(), "github@official"); got.Override {
		t.Errorf("row = %+v, want no Override", got)
	}

	session.SetState("review", equip.Off)
	save(t, session)

	want := map[string]any{"github@official": "yes"}
	if got := readJSON(t, settingsLocal(repo))["enabledPlugins"]; !reflect.DeepEqual(got, want) {
		t.Errorf("enabledPlugins = %v, want %v", got, want)
	}
}

func TestFirstOpenImportsAHandSetPluginAsAnOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	writeFile(t, settingsLocal(repo), `{"enabledPlugins": {"github@official": false}}`)

	if got := row(t, open(t, machine, repo), "github@official"); got.State != equip.Off || !got.Override {
		t.Errorf("row = %+v, want an off Override", got)
	}
}

// savedSkillOff saves review off in repo, then writes the settings and the
// record that save left as settings and record.
func savedSkillOff(t *testing.T, machine *equiptest.Machine, repo, settings, record string) {
	t.Helper()
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	save(t, session)
	writeFile(t, settingsLocal(repo), settings)

	files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))
	writeFile(t, files[0], record)
}

func TestOpenImportsPluginsARecordFromBeforePluginsDoesNotName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Plugin("github@official", "user", "")
	savedSkillOff(t, machine, repo,
		`{"skillOverrides": {"review": "off"}, "enabledPlugins": {"github@official": false}}`,
		"path = '"+repo+"'\n[overrides.skills]\nreview = 'off'\n")

	view := open(t, machine, repo)

	got := row(t, view, "github@official")
	if got.State != equip.Off || !got.Override || got.Unsaved || got.ChangedOutside {
		t.Errorf("row = %+v, want a saved off Override", got)
	}

	if view.Unsaved != 0 {
		t.Errorf("Unsaved = %d, want 0", view.Unsaved)
	}
}

func TestRecordFromBeforePluginsStillSeesSkillsChangedOutside(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Plugin("github@official", "user", "")
	savedSkillOff(t, machine, repo,
		`{"skillOverrides": {"review": "on"}}`,
		"path = '"+repo+"'\n[overrides.skills]\nreview = 'off'\n")

	if got := row(t, open(t, machine, repo), "review"); !got.ChangedOutside {
		t.Errorf("row = %+v, want changed outside", got)
	}
}

func TestPluginSetByHandAfterASaveIsChangedOutside(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Plugin("github@official", "user", "")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	save(t, session)
	writeFile(t, settingsLocal(repo),
		`{"skillOverrides": {"review": "off"}, "enabledPlugins": {"github@official": false}}`)

	if got := row(t, open(t, machine, repo), "github@official"); !got.ChangedOutside || !got.Unsaved {
		t.Errorf("row = %+v, want an unsaved Override changed outside", got)
	}
}

func TestSaveImportsAPluginChangedOutsideSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Plugin("github@official", "user", "")
	session := newSession(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"enabledPlugins": {"github@official": false}}`)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	got := row(t, session.View(), "github@official")
	if got.State != equip.Off || !got.Override || !got.Unsaved || !got.ChangedOutside {
		t.Errorf("row = %+v, want an unsaved off Override changed outside", got)
	}
}
