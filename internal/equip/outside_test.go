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

func TestFirstOpenImportsHandSetStatesAsOverrides(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "docs")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "user-invocable-only", "review": "off"}}`)

	v := session(t, m, repo).View()

	want := []equip.Row{
		{Name: "docs", State: equip.ManualOnly, Override: true, Fallback: equip.On},
		{Name: "review", State: equip.Off, Override: true, Fallback: equip.On},
	}
	if !slices.Equal(v.Rows, want) || v.Unsaved != 0 {
		t.Errorf("rows = %+v, Unsaved = %d, want %+v and 0", v.Rows, v.Unsaved, want)
	}
}

func TestFirstSaveRecordsTheImportedStates(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "off"}}`)

	save(t, session(t, m, repo))

	got := readRecord(t, m)["overrides"]
	if want := map[string]any{"skills": map[string]any{"review": "off"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("record overrides = %v, want %v", got, want)
	}
}

func TestQuittingWithoutSavingLeavesHandEditsToImportAgain(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)

	const edit = `{"skillOverrides": {"review": "on"}}`
	writeFile(t, settingsLocal(repo), edit)
	session(t, m, repo).SetState("review", equip.ManualOnly)

	v := session(t, m, repo).View()

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != edit {
		t.Errorf("settings = %s, want the hand edit", data)
	}

	want := equip.Row{
		Name: "review", State: equip.On, Override: true, Fallback: equip.On, Unsaved: true,
		ChangedOutside: true,
	}
	if v.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", v.Rows[0], want)
	}
}

func TestSaveKeepsAnImportAndClearsItsNote(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "on"}}`)
	s := session(t, m, repo)

	save(t, s)

	want := equip.Row{Name: "review", State: equip.On, Override: true, Fallback: equip.On}
	if v := s.View(); v.Rows[0] != want || v.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", v.Rows[0], v.Unsaved, want)
	}

	got := readRecord(t, m)["overrides"]
	if want := map[string]any{"skills": map[string]any{"review": "on"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("record overrides = %v, want %v", got, want)
	}
}

// savedOff opens repo, sets review off and saves.
func savedOff(t *testing.T, m *equiptest.Machine, repo string) {
	t.Helper()
	s := session(t, m, repo)
	s.SetState("review", equip.Off)
	save(t, s)
}

func TestEntryMissingOnDiskShowsTheRecordStateUnsaved(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	writeFile(t, settingsLocal(repo), `{}`)

	v := session(t, m, repo).View()

	want := equip.Row{Name: "review", State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true}
	if v.Rows[0] != want || v.Unsaved != 1 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 1", v.Rows[0], v.Unsaved, want)
	}
}

func TestEntryChangedOnDiskIsImportedAsAnUnsavedOverride(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		saved    bool // whether the record has an Override for review
		settings string
		want     equip.State
	}{
		"another value": {true, `{"skillOverrides": {"review": "on"}}`, equip.On},
		"none wanted":   {false, `{"skillOverrides": {"docs": "off", "review": "user-invocable-only"}}`, equip.ManualOnly},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := equiptest.New(t)
			repo := m.Repo("app")
			m.Skill(m.ClaudeSkills(), "review")
			m.Skill(m.ClaudeSkills(), "docs")

			if tc.saved {
				savedOff(t, m, repo)
			} else {
				s := session(t, m, repo)
				s.SetState("docs", equip.Off)
				save(t, s)
			}

			writeFile(t, settingsLocal(repo), tc.settings)

			v := session(t, m, repo).View()

			want := equip.Row{
				Name: "review", State: tc.want, Override: true, Fallback: equip.On, Unsaved: true,
				ChangedOutside: true,
			}
			if v.Rows[1] != want {
				t.Errorf("row = %+v, want %+v", v.Rows[1], want)
			}
		})
	}
}

func TestSaveAfterAnOutsideChangeWritesNothingAndImportsIt(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "docs")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	s := session(t, m, repo)

	const outside = `{"skillOverrides": {"review": "on"}}`
	writeFile(t, settingsLocal(repo), outside)
	s.SetState("docs", equip.ManualOnly)

	err := s.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != outside {
		t.Errorf("settings = %s, want them untouched", data)
	}

	got := readRecord(t, m)["overrides"]
	if want := map[string]any{"skills": map[string]any{"review": "off"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("record overrides = %v, want %v", got, want)
	}

	want := []equip.Row{
		{Name: "docs", State: equip.ManualOnly, Override: true, Fallback: equip.On, Unsaved: true},
		{
			Name: "review", State: equip.On, Override: true, Fallback: equip.On, Unsaved: true,
			ChangedOutside: true,
		},
	}
	if v := s.View(); !slices.Equal(v.Rows, want) {
		t.Errorf("rows = %+v, want %+v", v.Rows, want)
	}
}

func TestSaveReportsSettingsBrokenSinceOpen(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	s := session(t, m, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": `)

	err := s.Save()
	if err == nil || errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Errorf("Save = %v, want the read error", err)
	}

	if v := s.View(); v.Unsaved != 0 {
		t.Errorf("Unsaved = %d, want 0", v.Unsaved)
	}
}

func TestNameOnlyOnDiskMatchesARecordedOn(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.On)
	save(t, s)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "name-only"}}`)

	want := equip.Row{Name: "review", State: equip.On, Override: true, Fallback: equip.On}
	if v := session(t, m, repo).View(); v.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", v.Rows[0], want)
	}
}

func TestSaveAfterAnImportIsRemovedOutsideDropsIt(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	m.Skill(m.ClaudeSkills(), "docs")
	s := session(t, m, repo)
	s.SetState("docs", equip.Off)
	save(t, s)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "off", "review": "on"}}`)
	s = session(t, m, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "off"}}`)

	err := s.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	want := equip.Row{Name: "review", State: equip.On, Fallback: equip.On}
	if v := s.View(); v.Rows[1] != want || v.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", v.Rows[1], v.Unsaved, want)
	}
}

func TestSaveAfterAnOutsideRemovalWritesNothing(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	s := session(t, m, repo)
	writeFile(t, settingsLocal(repo), `{}`)

	err := s.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != `{}` {
		t.Errorf("settings = %s, want them untouched", data)
	}

	want := equip.Row{Name: "review", State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true}
	if v := s.View(); v.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", v.Rows[0], want)
	}
}

func TestSavingADroppedImportRemovesItsEntry(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	s := session(t, m, repo)
	s.DropOverride("review")
	save(t, s)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "on"}}`)
	s = session(t, m, repo)

	s.DropOverride("review")
	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestSaveAfterAnOutsideChangeBackShowsTheRecordState(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "on"}}`)
	s := session(t, m, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "off"}}`)

	err := s.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	want := equip.Row{Name: "review", State: equip.Off, Override: true, Fallback: equip.On}
	if v := s.View(); v.Rows[0] != want || v.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", v.Rows[0], v.Unsaved, want)
	}
}

func TestSaveMarksAPendingToggleReplacedByAnOutsideChange(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	savedOff(t, m, repo)
	s := session(t, m, repo)
	s.SetState("review", equip.ManualOnly)
	writeFile(t, settingsLocal(repo), `{}`)

	err := s.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	want := equip.Row{Name: "review", State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true, ChangedOutside: true}
	if v := s.View(); v.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", v.Rows[0], want)
	}
}

func TestUnknownValuesOnDiskAreNotImportedAndSaveKeepsThem(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)

	repo := m.Repo("app")
	for _, name := range []string{"docs", "lint", "review"} {
		m.Skill(m.ClaudeSkills(), name)
	}

	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"lint": 1, "review": "sometimes"}}`)

	s := session(t, m, repo)
	for _, r := range s.View().Rows {
		if r.Override {
			t.Errorf("row %+v imported an unknown value", r)
		}
	}

	s.SetState("docs", equip.Off)
	save(t, s)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"docs": "off", "lint": 1.0, "review": "sometimes"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestSaveAfterAFailedRecordWriteSucceeds(t *testing.T) {
	t.Parallel()
	m := equiptest.New(t)
	repo := m.Repo("app")
	m.Skill(m.ClaudeSkills(), "review")
	s := session(t, m, repo)
	s.SetState("review", equip.Off)

	records := filepath.Join(m.StateHome, "equip")
	writeFile(t, records, "") // a file where the records dir belongs

	err := s.Save()
	if err == nil {
		t.Fatal("Save wrote a record into a file")
	}

	err = os.Remove(records)
	if err != nil {
		t.Fatal(err)
	}

	err = s.Save()
	if err != nil {
		t.Errorf("Save = %v, want it to write", err)
	}
}
