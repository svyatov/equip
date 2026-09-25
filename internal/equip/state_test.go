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

func session(t *testing.T, m *equiptest.Machine, dir string) *equip.Session {
	t.Helper()

	s, err := equip.Open(m.Machine, dir)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestSetStateMakesAnUnsavedOverride(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, m.Root)

	s.SetState("review", equip.Off)

	v := s.View()

	want := equip.Row{Name: "review", State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true}
	if v.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", v.Rows[0], want)
	}

	if v.Unsaved != 1 {
		t.Errorf("Unsaved = %d, want 1", v.Unsaved)
	}
}

func TestDropOverrideReturnsSkillToDefault(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, m.Root)
	s.SetState("review", equip.Off)

	s.DropOverride("review")

	v := s.View()
	if want := (equip.Row{Name: "review", State: equip.On, Fallback: equip.On}); v.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", v.Rows[0], want)
	}

	if v.Unsaved != 0 {
		t.Errorf("Unsaved = %d, want 0", v.Unsaved)
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

func save(t *testing.T, s *equip.Session) {
	t.Helper()

	err := s.Save()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSaveWritesSkillOverridesForClaudeCode(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)

	repo := m.Repo("app")
	for _, name := range []string{"docs", "lint", "review"} {
		m.Skill(m.ClaudeSkills(), name)
	}

	s := session(t, m, repo)
	s.SetState("docs", equip.ManualOnly)
	s.SetState("lint", equip.On)
	s.SetState("review", equip.Off)

	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]

	want := map[string]any{"docs": "user-invocable-only", "lint": "on", "review": "off"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	if n := s.View().Unsaved; n != 0 {
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
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"permissions": {"allow": ["Bash(ls)"]}}`)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	got := readJSON(t, settingsLocal(repo))["permissions"]

	want := map[string]any{"allow": []any{"Bash(ls)"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("permissions = %v, want %v", got, want)
	}
}

func TestSaveKeepsNumbersExactly(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"big": 12345678901234567890}`)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

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
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)

	old := filepath.Join(m.Root, "old.json")

	err := os.Link(settingsLocal(repo), old)
	if err != nil {
		t.Fatal(err)
	}

	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

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
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	if st := m.RunGit(repo, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status shows %q, want nothing", st)
	}
}

func TestSaveLeavesExcludeAloneWhenGitIgnoresTheSettingsFile(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, filepath.Join(repo, ".gitignore"), ".claude/\n")
	excludeFile := filepath.Join(repo, ".git", "info", "exclude")
	before, _ := os.ReadFile(excludeFile)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	if after, _ := os.ReadFile(excludeFile); string(after) != string(before) {
		t.Errorf("exclude changed to %q", after)
	}
}

func TestSaveExcludesOnANewLineAfterAnUnterminatedExclude(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "notes.txt")
	writeFile(t, filepath.Join(repo, "notes.txt"), "")
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	if st := m.RunGit(repo, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Errorf("git status shows %q, want nothing", st)
	}
}

func TestSaveDoesNotExcludeASettingsFileItDidNotCreate(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	if st := m.RunGit(repo, "status", "--porcelain", "--untracked-files=all"); st != "?? .claude/settings.local.json" {
		t.Errorf("git status shows %q, want the settings file untracked", st)
	}
}

//nolint:paralleltest // t.Chdir changes the whole process
func TestSaveOutsideGitWritesOnlyTheSettingsFile(t *testing.T) {
	m := equiptest.New(t)
	dir := m.Mkdir(filepath.Join(m.Root, "scratch"))
	t.Chdir(dir) // a stray relative write would land here
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, dir)
	s.SetState("review", equip.Off)

	save(t, s)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != ".claude" {
		t.Errorf("project holds %v, want only .claude", entries)
	}
}

// readRecord decodes the one project record on m.
func readRecord(t *testing.T, m *equiptest.Machine) map[string]any {
	t.Helper()

	files, _ := filepath.Glob(filepath.Join(m.StateHome, "equip", "*.toml"))
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
	m := equiptest.New(t)
	repo := m.Repo("app")
	root := m.Commit(repo)
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.ManualOnly)

	save(t, s)

	want := map[string]any{
		"path":        repo,
		"root_commit": root,
		"overrides":   map[string]any{"skills": map[string]any{"review": "manual-only"}},
	}
	if got := readRecord(t, m); !reflect.DeepEqual(got, want) {
		t.Errorf("record = %v, want %v", got, want)
	}
}

func TestReopenShowsSavedOverrides(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.ManualOnly)
	save(t, s)

	v := session(t, m, repo).View()

	want := equip.Row{Name: "review", State: equip.ManualOnly, Override: true, Fallback: equip.On}
	if v.Rows[0] != want || v.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", v.Rows[0], v.Unsaved, want)
	}
}

func TestOpenRefusesABrokenRecord(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"bad TOML":      "path = ",
		"unknown state": "[overrides.skills]\nreview = \"sometimes\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := equiptest.New(t)
			repo := m.Repo("app")
			m.Skill(m.ClaudeSkills(), "review")
			s := session(t, m, repo)
			s.SetState("review", equip.Off)
			save(t, s)

			files, _ := filepath.Glob(filepath.Join(m.StateHome, "equip", "*.toml"))
			writeFile(t, files[0], body)

			_, err := equip.Open(m.Machine, repo)
			if err == nil {
				t.Error("Open accepted a broken record")
			}
		})
	}
}

func TestSaveRefusesBrokenSettings(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"permissions": `)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	err := s.Save()
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
			m := equiptest.New(t)
			repo := m.Repo("app")
			m.Skill(m.ClaudeSkills(), "review")
			writeFile(t, settingsLocal(repo), body)
			s := session(t, m, repo)
			s.SetState("review", equip.Off)

			save(t, s)

			got := readJSON(t, settingsLocal(repo))["skillOverrides"]
			if want := map[string]any{"review": "off"}; !reflect.DeepEqual(got, want) {
				t.Errorf("skillOverrides = %v, want %v", got, want)
			}
		})
	}
}

func TestSaveWritesThroughASymlinkedSettingsFile(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	target := filepath.Join(m.Root, "dotfiles", "settings.json")
	writeFile(t, target, `{}`)
	m.Mkdir(filepath.Join(repo, ".claude"))

	err := os.Symlink(target, settingsLocal(repo))
	if err != nil {
		t.Fatal(err)
	}

	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

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
	m := equiptest.New(t)
	repo := m.Repo("app")
	gone := m.Skill(m.ClaudeSkills(), "gone")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("gone", equip.Off)
	save(t, s)

	err := os.RemoveAll(gone)
	if err != nil {
		t.Fatal(err)
	}

	err = os.Remove(settingsLocal(repo))
	if err != nil {
		t.Fatal(err)
	}

	s = session(t, m, repo)
	s.SetState("review", equip.Off)
	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	m.Skill(m.ClaudeSkills(), "gone")
	s = session(t, m, repo)
	save(t, s)

	got = readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"gone": "off", "review": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides after reinstall = %v, want %v", got, want)
	}
}

func TestSaveWithNothingUnsavedWritesNothing(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")

	save(t, session(t, m, repo))

	_, err := os.Stat(filepath.Join(repo, ".claude"))
	if err == nil {
		t.Error("save created .claude with nothing to write")
	}

	if files, _ := filepath.Glob(filepath.Join(m.StateHome, "equip", "*")); len(files) != 0 {
		t.Errorf("save wrote records %q with nothing to write", files)
	}
}

func TestSaveKeepsSettingsTextReadable(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"permissions": {"allow": ["Bash(make && make test > log)"]}}`)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	if data, _ := os.ReadFile(settingsLocal(repo)); !strings.Contains(string(data), "Bash(make && make test > log)") {
		t.Errorf("settings %s escaped the permission", data)
	}
}

func TestSaveKeepsTheSettingsFileMode(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)

	err := os.Chmod(settingsLocal(repo), 0o640)
	if err != nil {
		t.Fatal(err)
	}

	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	fi, err := os.Stat(settingsLocal(repo))
	if err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("settings mode = %v (%v), want 0640", fi.Mode().Perm(), err)
	}
}

func TestSaveKeepsSkillOverridesForUndiscoveredSkills(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"synced": "off"}}`)
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "off", "synced": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestDroppingASavedOverrideIsUnsaved(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.Off)
	save(t, s)

	s.DropOverride("review")

	v := s.View()
	if !v.Rows[0].Unsaved || v.Unsaved != 1 {
		t.Errorf("row unsaved = %t, Unsaved = %d, want true and 1", v.Rows[0].Unsaved, v.Unsaved)
	}
}

func TestSavingADroppedOverrideRemovesItsEntry(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.Off)
	save(t, s)
	s.DropOverride("review")

	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestSavingOnKeepsNameOnlyWhichReadsAsOn(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "name-only"}}`)
	s := session(t, m, repo)
	s.SetState("review", equip.On)

	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "name-only"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}
