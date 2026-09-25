package equip_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func newSession(t *testing.T, machine *equiptest.Machine, dir string) *equip.Session {
	t.Helper()

	session, err := equip.Open(machine.Machine, dir)
	if err != nil {
		t.Fatal(err)
	}

	return session
}

func TestSetStateMakesAnUnsavedOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, machine.Root)

	session.SetState("review", equip.Off)

	view := session.View()

	want := equip.Row{
		Name: "review", Cost: 0, State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true, ChangedOutside: false,
	}
	if view.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", view.Rows[0], want)
	}

	if view.Unsaved != 1 {
		t.Errorf("Unsaved = %d, want 1", view.Unsaved)
	}
}

func TestDropOverrideReturnsSkillToDefault(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, machine.Root)
	session.SetState("review", equip.Off)

	session.DropOverride("review")

	want := equip.Row{
		Name: "review", Cost: 8, State: equip.On, Override: false, Fallback: equip.On, Unsaved: false, ChangedOutside: false,
	}

	view := session.View()
	if view.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", view.Rows[0], want)
	}

	if view.Unsaved != 0 {
		t.Errorf("Unsaved = %d, want 0", view.Unsaved)
	}
}

func settingsLocal(project string) string {
	return filepath.Join(project, ".claude", "settings.local.json")
}

// readJSON decodes the JSON object in path.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any

	err = json.Unmarshal(data, &obj)
	if err != nil {
		t.Fatal(err)
	}

	return obj
}

func save(t *testing.T, session *equip.Session) {
	t.Helper()

	err := session.Save()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSaveWritesSkillOverridesForClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	repo := machine.Repo("app")
	for _, name := range []string{"docs", "lint", "review"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	session := newSession(t, machine, repo)
	session.SetState("docs", equip.ManualOnly)
	session.SetState("lint", equip.On)
	session.SetState("review", equip.Off)

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]

	want := map[string]any{"docs": "user-invocable-only", "lint": "on", "review": "off"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	if n := session.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d after save, want 0", n)
	}
}

// writeFile writes content to path, creating its parents.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSaveKeepsSettingsEquipDoesNotOwn(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"permissions": {"allow": ["Bash(ls)"]}}`)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["permissions"]

	want := map[string]any{"allow": []any{"Bash(ls)"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("permissions = %v, want %v", got, want)
	}
}

func TestSaveKeepsNumbersExactly(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"big": 12345678901234567890}`)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	data, err := os.ReadFile(settingsLocal(repo))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "12345678901234567890") {
		t.Errorf("settings %s lost the number 12345678901234567890", data)
	}
}

func TestSaveReplacesSettingsWithANewFile(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)

	old := filepath.Join(machine.Root, "old.json")

	err := os.Link(settingsLocal(repo), old)
	if err != nil {
		t.Fatal(err)
	}

	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	if data, _ := os.ReadFile(old); string(data) != `{}` {
		t.Errorf("save wrote into the old file in place: %s", data)
	}

	entries, _ := os.ReadDir(filepath.Dir(settingsLocal(repo)))
	if len(entries) != 1 {
		t.Errorf(".claude holds %v, want only settings.local.json", entries)
	}
}

func TestSaveExcludesTheSettingsFileItCreatesFromGit(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	if st := machine.RunGit(repo, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status shows %q, want nothing", st)
	}
}

func TestSaveLeavesExcludeAloneWhenGitIgnoresTheSettingsFile(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, filepath.Join(repo, ".gitignore"), ".claude/\n")
	excludeFile := filepath.Join(repo, ".git", "info", "exclude")
	before, _ := os.ReadFile(excludeFile)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	if after, _ := os.ReadFile(excludeFile); string(after) != string(before) {
		t.Errorf("exclude changed to %q", after)
	}
}

func TestSaveExcludesOnANewLineAfterAnUnterminatedExclude(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "notes.txt")
	writeFile(t, filepath.Join(repo, "notes.txt"), "")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	if st := machine.RunGit(repo, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status shows %q, want nothing", st)
	}
}

func TestSaveDoesNotExcludeASettingsFileItDidNotCreate(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	status := machine.RunGit(repo, "status", "--porcelain", "--untracked-files=all")
	if status != "?? .claude/settings.local.json" {
		t.Errorf("git status shows %q, want the settings file untracked", status)
	}
}

//nolint:paralleltest // t.Chdir changes the whole process
func TestSaveOutsideGitWritesOnlyTheSettingsFile(t *testing.T) {
	machine := equiptest.New(t)
	dir := machine.Mkdir(filepath.Join(machine.Root, "scratch"))
	t.Chdir(dir) // a stray relative write would land here
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, dir)
	session.SetState("review", equip.Off)

	save(t, session)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != ".claude" {
		t.Errorf("project holds %v, want only .claude", entries)
	}
}

// readRecord decodes the one project record on machine.
func readRecord(t *testing.T, machine *equiptest.Machine) map[string]any {
	t.Helper()

	files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))
	if len(files) != 1 {
		t.Fatalf("records = %q, want one", files)
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}

	var rec map[string]any

	err = toml.Unmarshal(data, &rec)
	if err != nil {
		t.Fatal(err)
	}

	return rec
}

func TestSaveWritesTheProjectRecord(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	root := machine.Commit(repo)
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.ManualOnly)

	save(t, session)

	want := map[string]any{
		"path":        repo,
		"root_commit": root,
		"overrides":   map[string]any{"skills": map[string]any{"review": "manual-only"}, "plugins": map[string]any{}},
	}
	if got := readRecord(t, machine); !reflect.DeepEqual(got, want) {
		t.Errorf("record = %v, want %v", got, want)
	}
}

func TestReopenShowsSavedOverrides(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.ManualOnly)
	save(t, session)

	view := newSession(t, machine, repo).View()

	want := equip.Row{
		Name: "review", Cost: 0, State: equip.ManualOnly, Override: true, Fallback: equip.On, Unsaved: false,
		ChangedOutside: false,
	}
	if view.Rows[0] != want || view.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", view.Rows[0], view.Unsaved, want)
	}
}

func TestOpenRefusesABrokenRecord(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"bad TOML":                        "path = ",
		"unknown state":                   "[overrides.skills]\nreview = \"sometimes\"\n",
		"state the plugin does not offer": "[overrides.plugins]\n\"github@official\" = \"manual-only\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			machine := equiptest.New(t)
			repo := machine.Repo("app")
			machine.Skill(machine.ClaudeSkills(), "review")
			session := newSession(t, machine, repo)
			session.SetState("review", equip.Off)
			save(t, session)

			files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))
			writeFile(t, files[0], body)

			_, err := equip.Open(machine.Machine, repo)
			if err == nil {
				t.Error("Open accepted a broken record")
			}
		})
	}
}

func TestSaveRefusesBrokenSettings(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"permissions": `)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	err := session.Save()
	if err == nil {
		t.Error("Save overwrote broken settings")
	}

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != `{"permissions": ` {
		t.Errorf("settings = %q, want them untouched", data)
	}
}

func TestSaveReadsNullSettingsAsEmpty(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`null`, `{"skillOverrides": null}`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			machine := equiptest.New(t)
			repo := machine.Repo("app")
			machine.Skill(machine.ClaudeSkills(), "review")
			writeFile(t, settingsLocal(repo), body)
			session := newSession(t, machine, repo)
			session.SetState("review", equip.Off)

			save(t, session)

			got := readJSON(t, settingsLocal(repo))["skillOverrides"]
			if want := map[string]any{"review": "off"}; !reflect.DeepEqual(got, want) {
				t.Errorf("skillOverrides = %v, want %v", got, want)
			}
		})
	}
}

func TestSaveWritesThroughASymlinkedSettingsFile(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	target := filepath.Join(machine.Root, "dotfiles", "settings.json")
	writeFile(t, target, `{}`)
	machine.Mkdir(filepath.Join(repo, ".claude"))

	err := os.Symlink(target, settingsLocal(repo))
	if err != nil {
		t.Fatal(err)
	}

	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	fi, err := os.Lstat(settingsLocal(repo))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("settings.local.json is no longer a symlink: %v", err)
	}

	got := readJSON(t, target)["skillOverrides"]
	if want := map[string]any{"review": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("target skillOverrides = %v, want %v", got, want)
	}
}

func TestOverrideForAnUninstalledSkillIsKeptAndAppliesOnceItReturns(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	gone := machine.Skill(machine.ClaudeSkills(), "gone")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("gone", equip.Off)
	save(t, session)

	err := os.RemoveAll(gone)
	if err != nil {
		t.Fatal(err)
	}

	err = os.Remove(settingsLocal(repo))
	if err != nil {
		t.Fatal(err)
	}

	session = newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	machine.Skill(machine.ClaudeSkills(), "gone")
	session = newSession(t, machine, repo)
	save(t, session)

	got = readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"gone": "off", "review": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides after reinstall = %v, want %v", got, want)
	}
}

func TestSaveWithNothingUnsavedWritesNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")

	save(t, newSession(t, machine, repo))

	_, err := os.Stat(filepath.Join(repo, ".claude"))
	if err == nil {
		t.Error("save created .claude with nothing to write")
	}

	if files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*")); len(files) != 0 {
		t.Errorf("save wrote records %q with nothing to write", files)
	}
}

func TestSaveKeepsSettingsTextReadable(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"permissions": {"allow": ["Bash(make && make test > log)"]}}`)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	if data, _ := os.ReadFile(settingsLocal(repo)); !strings.Contains(string(data), "Bash(make && make test > log)") {
		t.Errorf("settings %s escaped the permission", data)
	}
}

func TestSaveKeepsTheSettingsFileMode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)

	err := os.Chmod(settingsLocal(repo), 0o640)
	if err != nil {
		t.Fatal(err)
	}

	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	fi, err := os.Stat(settingsLocal(repo))
	if err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("settings mode = %v (%v), want 0640", fi.Mode().Perm(), err)
	}
}

func TestSaveKeepsSkillOverridesForUndiscoveredSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"synced": "off"}}`)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "off", "synced": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestDroppingASavedOverrideIsUnsaved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	save(t, session)

	session.DropOverride("review")

	view := session.View()
	if !view.Rows[0].Unsaved || view.Unsaved != 1 {
		t.Errorf("row unsaved = %t, Unsaved = %d, want true and 1", view.Rows[0].Unsaved, view.Unsaved)
	}
}

func TestSavingADroppedOverrideRemovesItsEntry(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	save(t, session)
	session.DropOverride("review")

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestSavingOnKeepsNameOnlyWhichReadsAsOn(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "name-only"}}`)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.On)

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "name-only"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}
