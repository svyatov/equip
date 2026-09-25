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
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "docs")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "user-invocable-only", "review": "off"}}`)

	view := newSession(t, machine, repo).View()

	want := []equip.Row{
		{
			Name: "docs", Cost: 0, State: equip.ManualOnly, Override: true, Fallback: equip.On, Unsaved: false,
			ChangedOutside: false,
		},
		{
			Name: "review", Cost: 0, State: equip.Off, Override: true, Fallback: equip.On, Unsaved: false,
			ChangedOutside: false,
		},
	}
	if !slices.Equal(view.Rows, want) || view.Unsaved != 0 {
		t.Errorf("rows = %+v, Unsaved = %d, want %+v and 0", view.Rows, view.Unsaved, want)
	}
}

func TestFirstSaveRecordsTheImportedStates(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "off"}}`)

	save(t, newSession(t, machine, repo))

	got := readRecord(t, machine)["overrides"]
	if want := map[string]any{"skills": map[string]any{"review": "off"}, "plugins": map[string]any{}}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("record overrides = %v, want %v", got, want)
	}
}

func TestQuittingWithoutSavingLeavesHandEditsToImportAgain(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)

	const edit = `{"skillOverrides": {"review": "on"}}`
	writeFile(t, settingsLocal(repo), edit)
	newSession(t, machine, repo).SetState("review", equip.ManualOnly)

	view := newSession(t, machine, repo).View()

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != edit {
		t.Errorf("settings = %s, want the hand edit", data)
	}

	want := equip.Row{
		Name: "review", Cost: 8, State: equip.On, Override: true, Fallback: equip.On, Unsaved: true,
		ChangedOutside: true,
	}
	if view.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", view.Rows[0], want)
	}
}

func TestSaveKeepsAnImportAndClearsItsNote(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "on"}}`)
	session := newSession(t, machine, repo)

	save(t, session)

	want := equip.Row{
		Name: "review", Cost: 8, State: equip.On, Override: true, Fallback: equip.On, Unsaved: false, ChangedOutside: false,
	}
	if view := session.View(); view.Rows[0] != want || view.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", view.Rows[0], view.Unsaved, want)
	}

	got := readRecord(t, machine)["overrides"]
	if want := map[string]any{"skills": map[string]any{"review": "on"}, "plugins": map[string]any{}}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("record overrides = %v, want %v", got, want)
	}
}

// savedOff opens repo, sets review off and saves.
func savedOff(t *testing.T, machine *equiptest.Machine, repo string) {
	t.Helper()
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	save(t, session)
}

func TestEntryMissingOnDiskShowsTheRecordStateUnsaved(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{}`)

	view := newSession(t, machine, repo).View()

	want := equip.Row{
		Name: "review", Cost: 0, State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true, ChangedOutside: false,
	}
	if view.Rows[0] != want || view.Unsaved != 1 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 1", view.Rows[0], view.Unsaved, want)
	}
}

func TestEntryChangedOnDiskIsImportedAsAnUnsavedOverride(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		settings string
		want     equip.State
		saved    bool // whether the record has an Override for review
		cost     int
	}{
		"another value": {`{"skillOverrides": {"review": "on"}}`, equip.On, true, 8},
		"none wanted":   {`{"skillOverrides": {"docs": "off", "review": "user-invocable-only"}}`, equip.ManualOnly, false, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			machine := equiptest.New(t)
			repo := machine.Repo("app")
			machine.Skill(machine.ClaudeSkills(), "review")
			machine.Skill(machine.ClaudeSkills(), "docs")

			if testCase.saved {
				savedOff(t, machine, repo)
			} else {
				session := newSession(t, machine, repo)
				session.SetState("docs", equip.Off)
				save(t, session)
			}

			writeFile(t, settingsLocal(repo), testCase.settings)

			view := newSession(t, machine, repo).View()

			want := equip.Row{
				Name: "review", Cost: testCase.cost, State: testCase.want, Override: true, Fallback: equip.On,
				Unsaved: true, ChangedOutside: true,
			}
			if view.Rows[1] != want {
				t.Errorf("row = %+v, want %+v", view.Rows[1], want)
			}
		})
	}
}

func TestSaveAfterAnOutsideChangeWritesNothingAndImportsIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "docs")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	session := newSession(t, machine, repo)

	const outside = `{"skillOverrides": {"review": "on"}}`
	writeFile(t, settingsLocal(repo), outside)
	session.SetState("docs", equip.ManualOnly)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != outside {
		t.Errorf("settings = %s, want them untouched", data)
	}

	got := readRecord(t, machine)["overrides"]
	if want := map[string]any{"skills": map[string]any{"review": "off"}, "plugins": map[string]any{}}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("record overrides = %v, want %v", got, want)
	}

	want := []equip.Row{
		{
			Name: "docs", Cost: 0, State: equip.ManualOnly, Override: true, Fallback: equip.On, Unsaved: true,
			ChangedOutside: false,
		},
		{
			Name: "review", Cost: 8, State: equip.On, Override: true, Fallback: equip.On, Unsaved: true,
			ChangedOutside: true,
		},
	}
	if view := session.View(); !slices.Equal(view.Rows, want) {
		t.Errorf("rows = %+v, want %+v", view.Rows, want)
	}
}

func TestSaveReportsSettingsBrokenSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	session := newSession(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": `)

	err := session.Save()
	if err == nil || errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Errorf("Save = %v, want the read error", err)
	}

	if view := session.View(); view.Unsaved != 0 {
		t.Errorf("Unsaved = %d, want 0", view.Unsaved)
	}
}

func TestNameOnlyOnDiskMatchesARecordedOn(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.On)
	save(t, session)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "name-only"}}`)

	want := equip.Row{
		Name: "review", Cost: 8, State: equip.On, Override: true, Fallback: equip.On, Unsaved: false, ChangedOutside: false,
	}
	if view := newSession(t, machine, repo).View(); view.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", view.Rows[0], want)
	}
}

func TestSaveAfterAnImportIsRemovedOutsideDropsIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.ClaudeSkills(), "docs")
	session := newSession(t, machine, repo)
	session.SetState("docs", equip.Off)
	save(t, session)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "off", "review": "on"}}`)
	session = newSession(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "off"}}`)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	want := equip.Row{
		Name: "review", Cost: 8, State: equip.On, Override: false, Fallback: equip.On, Unsaved: false, ChangedOutside: false,
	}
	if view := session.View(); view.Rows[1] != want || view.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", view.Rows[1], view.Unsaved, want)
	}
}

func TestSaveAfterAnOutsideRemovalWritesNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	session := newSession(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{}`)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	if data, _ := os.ReadFile(settingsLocal(repo)); string(data) != `{}` {
		t.Errorf("settings = %s, want them untouched", data)
	}

	want := equip.Row{
		Name: "review", Cost: 0, State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true, ChangedOutside: false,
	}
	if view := session.View(); view.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", view.Rows[0], want)
	}
}

func TestSavingADroppedImportRemovesItsEntry(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	session := newSession(t, machine, repo)
	session.DropOverride("review")
	save(t, session)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "on"}}`)
	session = newSession(t, machine, repo)

	session.DropOverride("review")
	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestSaveAfterAnOutsideChangeBackShowsTheRecordState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "on"}}`)
	session := newSession(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"review": "off"}}`)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	want := equip.Row{
		Name: "review", Cost: 0, State: equip.Off, Override: true, Fallback: equip.On, Unsaved: false, ChangedOutside: false,
	}
	if view := session.View(); view.Rows[0] != want || view.Unsaved != 0 {
		t.Errorf("row = %+v, Unsaved = %d, want %+v and 0", view.Rows[0], view.Unsaved, want)
	}
}

func TestSaveMarksAPendingToggleReplacedByAnOutsideChange(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	savedOff(t, machine, repo)
	session := newSession(t, machine, repo)
	session.SetState("review", equip.ManualOnly)
	writeFile(t, settingsLocal(repo), `{}`)

	err := session.Save()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Fatalf("Save = %v, want %v", err, equip.ErrChangedSinceOpen)
	}

	want := equip.Row{
		Name: "review", Cost: 0, State: equip.Off, Override: true, Fallback: equip.On, Unsaved: true, ChangedOutside: true,
	}
	if view := session.View(); view.Rows[0] != want {
		t.Errorf("row = %+v, want %+v", view.Rows[0], want)
	}
}

func TestUnknownValuesOnDiskAreNotImportedAndSaveKeepsThem(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	repo := machine.Repo("app")
	for _, name := range []string{"docs", "lint", "review"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"lint": 1, "review": "sometimes"}}`)

	session := newSession(t, machine, repo)
	for _, r := range session.View().Rows {
		if r.Override {
			t.Errorf("row %+v imported an unknown value", r)
		}
	}

	session.SetState("docs", equip.Off)
	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"docs": "off", "lint": 1.0, "review": "sometimes"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}
}

func TestSaveAfterAFailedRecordWriteSucceeds(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)

	records := filepath.Join(machine.StateHome, "equip")
	writeFile(t, records, "") // a file where the records dir belongs

	err := session.Save()
	if err == nil {
		t.Fatal("Save wrote a record into a file")
	}

	err = os.Remove(records)
	if err != nil {
		t.Fatal(err)
	}

	err = session.Save()
	if err != nil {
		t.Errorf("Save = %v, want it to write", err)
	}
}
