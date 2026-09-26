package equip_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// library is each preset of session as "name id members".
func library(session *equip.Session) []string {
	presets := session.Presets()
	out := make([]string, 0, len(presets))

	for _, preset := range presets {
		out = append(out, fmt.Sprintf("%s %s %d", preset.Name, preset.ID, len(preset.Members)))
	}

	return out
}

func TestLibraryListsEveryPresetByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Preset("Writing", `id = "w1"
skills = ["docs"]`)
	machine.Preset("Ruby", `id = "r1"
skills = ["rspec", "rubocop"]
plugins = ["rails@official"]
mcp_servers = ["github"]`)

	got := library(newSession(t, machine, machine.Root))

	if want := []string{"Ruby r1 4", "Writing w1 1"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

func TestLibraryShowsWhereEachPresetIsActive(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	app := newSession(t, machine, machine.Repo("app"))
	app.SetPresets([]string{"r1"})
	save(t, app)
	session := newSession(t, machine, machine.Repo("other"))

	session.SetPresets([]string{"w1"})

	presets := session.Presets()

	got := make([]string, 0, len(presets))
	for _, preset := range presets {
		got = append(got, fmt.Sprintf("%s active=%t projects=%d", preset.Name, preset.Active, preset.Projects))
	}

	if want := []string{"Ruby active=false projects=1", "Writing active=true projects=0"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

func TestLibraryDoesNotCountAProjectWhosePathIsGone(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	app := newSession(t, machine, repo)
	app.SetPresets([]string{"r1"})
	save(t, app)

	err := os.RemoveAll(repo)
	if err != nil {
		t.Fatal(err)
	}

	if got := newSession(t, machine, machine.Repo("other")).Presets()[0].Projects; got != 0 {
		t.Errorf("Ruby's projects = %d, want 0", got)
	}
}

func TestLibraryKeepsAndMarksMembersNotInstalled(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)

	members := newSession(t, machine, machine.Root).Presets()[0].Members

	got := make([]string, 0, len(members))
	for _, member := range members {
		got = append(got, fmt.Sprintf("%s installed=%t", member.Name, member.Installed))
	}

	if want := []string{"lint installed=true", "rspec installed=false"}; !slices.Equal(got, want) {
		t.Errorf("Ruby's members = %q, want %q", got, want)
	}
}

func TestViewNamesTheActivePresets(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)

	session.SetPresets([]string{"w1", "r1"})

	if got, want := session.View().Presets, []string{"Ruby", "Writing"}; !slices.Equal(got, want) {
		t.Errorf("Presets = %q, want %q", got, want)
	}
}

func TestAdoptingKeepsTheActivePresetsOfAMovedRepo(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	machine.Commit(repo)
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)

	moved := filepath.Join(machine.Root, "moved")

	err := os.Rename(repo, moved)
	if err != nil {
		t.Fatal(err)
	}

	session = newSession(t, machine, moved)

	err = session.Adopt(repo)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := session.View().Presets, []string{"Ruby"}; !slices.Equal(got, want) {
		t.Errorf("Presets = %q, want %q", got, want)
	}

	// The adopted record keeps them too.
	if got, want := newSession(t, machine, moved).View().Presets, []string{"Ruby"}; !slices.Equal(got, want) {
		t.Errorf("reopened: Presets = %q, want %q", got, want)
	}
}

func TestPresetWithoutAnIDFailsToOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Preset("Ruby", `skills = ["lint"]`)

	_, err := equip.Open(machine.Machine, machine.Root)
	if err == nil || !strings.Contains(err.Error(), "Ruby.toml") {
		t.Errorf("Open = %v, want an error naming Ruby.toml", err)
	}
}

func TestTwoPresetsWithOneIDFailToOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	machine.Preset("Ruby copy", `id = "r1"
skills = ["review"]`)

	_, err := equip.Open(machine.Machine, machine.Root)
	if err == nil || !strings.Contains(err.Error(), "Ruby copy.toml") || !strings.Contains(err.Error(), "Ruby.toml") {
		t.Errorf("Open = %v, want an error naming Ruby.toml and Ruby copy.toml", err)
	}
}

func TestPresetWithAnUnknownKeyFailsToOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Preset("Data", `id = "data"
mcp = ["github"]`)

	_, err := equip.Open(machine.Machine, machine.Root)
	if err == nil || !strings.Contains(err.Error(), "Data.toml") {
		t.Errorf("Open = %v, want an error naming Data.toml", err)
	}
}

func TestActivePresetTurnsOnItsMembersAndOffEverythingElse(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)

	session.SetPresets([]string{"r1"})

	got, want := states(session.View()), []string{"skill docs off", "skill lint on", "skill review off"}
	if !slices.Equal(got, want) {
		t.Errorf("states = %q, want %q", got, want)
	}
}

func TestSaveWithAnActivePresetWritesAnEntryForEveryInstalledExtension(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})

	// The three entries, and the preset the record gains.
	if got := session.View().Unsaved; got != 4 {
		t.Errorf("Unsaved = %d, want 4", got)
	}

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	want := map[string]any{"docs": "off", "lint": "on", "review": "off"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	if unsaved := session.View().Unsaved; unsaved != 0 {
		t.Errorf("Unsaved after save = %d, want 0", unsaved)
	}
}

func TestSaveRecordsTheActivePresetsWithAHashOfTheirMembers(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"w1", "r1"})
	save(t, session)

	presets, _ := readRecord(t, machine)["presets"].([]any)
	ids := make([]string, 0, len(presets))
	hashes := map[string]bool{}

	for _, preset := range presets {
		preset, _ := preset.(map[string]any)
		hash, _ := preset["hash"].(string)
		ids = append(ids, fmt.Sprint(preset["id"]))
		hashes[hash] = true
	}

	if want := []string{"r1", "w1"}; !slices.Equal(ids, want) {
		t.Errorf("recorded presets = %q, want %q", ids, want)
	}
	// Ruby and Writing have other members, so other hashes.
	if len(hashes) != 2 || hashes[""] {
		t.Errorf("recorded hashes = %v, want two", hashes)
	}

	reopened := newSession(t, machine, repo)

	got, want := states(reopened.View()), []string{"skill docs on", "skill lint on", "skill review off"}
	if !slices.Equal(got, want) {
		t.Errorf("states after reopen = %q, want %q", got, want)
	}

	if unsaved := reopened.View().Unsaved; unsaved != 0 {
		t.Errorf("Unsaved after reopen = %d, want 0", unsaved)
	}
}

func TestSessionTotalCountsOnlyWhatActivePresetsTurnOn(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	lint := row(t, session.View(), "lint").Cost

	session.SetPresets([]string{"r1"})

	if got := session.View().Totals[equip.ClaudeCode]; got != lint {
		t.Errorf("Claude Code total = %d, want lint's %d", got, lint)
	}
}

func TestPluginMCPServerFollowsItsPluginUnderPresets(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)
	machine.Preset("GitHub", `id = "gh"
plugins = ["github@official"]`)
	session := newSession(t, machine, repo)

	session.SetPresets([]string{"gh"})

	if got := search(t, session); got.State != equip.On {
		t.Errorf("search = %s, want on", got.State)
	}

	save(t, session)

	// On is no entry in disabledMcpServers, so ~/.claude.json stays unwritten.
	_, err := os.Stat(claudeJSON(machine))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat ~/.claude.json = %v, want it missing", err)
	}
}

func TestSaveWithAnActivePresetWritesEachEntryOnlyToTheAgentsThatHaveIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "lint")
	appendFile(t, machine.CodexConfig(), "[mcp_servers.db]\ncommand = \"db\"\n")
	trust(t, machine, repo)
	machine.Preset("Data", `id = "data"
mcp_servers = ["db"]`)
	session := newSession(t, machine, repo)

	session.SetPresets([]string{"data"})
	save(t, session)

	want := map[string]any{"mcp_servers": map[string]any{"db": map[string]any{"enabled": true}}}
	if got := readTOML(t, codexProject(repo)); !reflect.DeepEqual(got, want) {
		t.Errorf(".codex/config.toml = %v, want %v", got, want)
	}

	settings := readJSON(t, settingsLocal(repo))
	if got, want := settings["skillOverrides"], map[string]any{"lint": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	if got := settings["disabledMcpjsonServers"]; got != nil {
		t.Errorf("disabledMcpjsonServers = %v, want none", got)
	}
}

func TestHandEditUnderAnActivePresetIsImportedAsAnOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "off", "lint": "on", "review": "on"}}`)

	view := newSession(t, machine, repo).View()

	if got := row(t, view, "review"); got.State != equip.On || !got.Override || !got.ChangedOutside {
		t.Errorf("review = %+v, want an Override on, changed outside", got)
	}

	if got := row(t, view, "docs"); got.Override || got.ChangedOutside || got.State != equip.Off {
		t.Errorf("docs = %+v, want off from the preset", got)
	}
}

func TestRemovingTheLastActivePresetReturnsEverythingToAgentDefaults(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)
	session = newSession(t, machine, repo)

	session.SetPresets(nil)

	got, want := states(session.View()), []string{"skill docs on", "skill lint on", "skill review on"}
	if !slices.Equal(got, want) {
		t.Errorf("states = %q, want %q", got, want)
	}

	save(t, session)

	if got := readJSON(t, settingsLocal(repo))["skillOverrides"]; !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("skillOverrides = %v, want none", got)
	}

	if got := readRecord(t, machine)["presets"]; got != nil {
		t.Errorf("recorded presets = %v, want none", got)
	}
}

func TestActivePresetsTurnOnTheUnionOfTheirMembersAndOverridesWin(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	session.SetState("docs", equip.ManualOnly)
	session.SetState("review", equip.On)

	session.SetPresets([]string{"r1", "w1"})

	got, want := states(session.View()), []string{"skill docs manual-only", "skill lint on", "skill review on"}
	if !slices.Equal(got, want) {
		t.Errorf("states = %q, want %q", got, want)
	}

	session.SetPresets([]string{"w1"})

	got, want = states(session.View()), []string{"skill docs manual-only", "skill lint off", "skill review on"}
	if !slices.Equal(got, want) {
		t.Errorf("states = %q, want %q", got, want)
	}
}
