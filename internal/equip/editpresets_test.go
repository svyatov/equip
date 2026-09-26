package equip_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// create creates the preset name in session and returns its id.
func create(t *testing.T, session *equip.Session, name string) string {
	t.Helper()

	id, err := session.CreatePreset(name)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// write writes the preset with unwritten edits in session.
func write(t *testing.T, session *equip.Session) {
	t.Helper()

	err := session.WritePreset()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreatedPresetKeepsItsIDOnceWritten(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)

	docsID := create(t, session, "Docs")
	add(t, session, docsID, "docs")

	if docs := session.Presets()[0]; !docs.New || !docs.Unwritten {
		t.Errorf("Docs = %+v, want new and unwritten", docs)
	}

	write(t, session)

	if docs := session.Presets()[0]; docs.New || docs.Unwritten {
		t.Errorf("Docs after the write = %+v, want written", docs)
	}

	got := library(newSession(t, machine, machine.Root))
	if want := []string{"Docs " + docsID + " 1", "Ruby r1 2", "Writing w1 1"}; docsID == "" || !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

// members are the members of the preset with id in session as "name edit",
// the edit "+" for an added one, "-" for a removed one, else "=".
func members(session *equip.Session, id string) []string {
	var out []string

	for _, preset := range session.Presets() {
		if preset.ID != id {
			continue
		}

		for _, member := range preset.Members {
			edit := "="
			if member.Added {
				edit = "+"
			}

			if member.Removed {
				edit = "-"
			}

			out = append(out, member.Name+" "+edit)
		}
	}

	return out
}

func TestEditsStayPendingAndMarkedUntilWritten(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)

	add(t, session, "r1", "review")
	remove(t, session, "r1", "lint")

	if got, want := members(session, "r1"), []string{"lint -", "review +", "rspec ="}; !slices.Equal(got, want) {
		t.Errorf("Ruby's members = %q, want %q", got, want)
	}

	if !session.Presets()[0].Unwritten {
		t.Error("Ruby does not show unwritten edits")
	}

	if got := members(newSession(t, machine, machine.Root), "r1"); !slices.Equal(got, []string{"lint =", "rspec ="}) {
		t.Errorf("Ruby's members on disk = %q, want lint and rspec", got)
	}

	write(t, session)

	if got, want := members(session, "r1"), []string{"review =", "rspec ="}; !slices.Equal(got, want) ||
		session.Presets()[0].Unwritten {
		t.Errorf("Ruby's members after the write = %q, want %q and no unwritten edits", got, want)
	}
}

func TestOnlyOnePresetHasUnwrittenEdits(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	add(t, session, "r1", "review")

	err := session.AddMember("w1", "review")
	if !errors.Is(err, equip.ErrUnwrittenEdits) {
		t.Errorf("AddMember on Writing = %v, want ErrUnwrittenEdits", err)
	}

	remove(t, session, "r1", "review")

	if session.Presets()[0].Unwritten {
		t.Error("Ruby shows unwritten edits after the edit was undone")
	}

	add(t, session, "w1", "review")

	if got, want := members(session, "w1"), []string{"docs =", "review +"}; !slices.Equal(got, want) {
		t.Errorf("Writing's members = %q, want %q", got, want)
	}
}

func TestDiscardDropsTheEditsAndAPresetNotWrittenYet(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	add(t, session, "r1", "review")

	session.DiscardPreset()

	if got := members(session, "r1"); !slices.Equal(got, []string{"lint =", "rspec ="}) {
		t.Errorf("Ruby's members = %q, want lint and rspec", got)
	}

	create(t, session, "Docs")
	session.DiscardPreset()

	if got, want := library(session), []string{"Ruby r1 2", "Writing w1 1"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

func TestRenameRenamesTheFileAtOnceAndRewritesNoProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)

	settings, _ := os.ReadFile(settingsLocal(repo))
	record, _ := os.ReadFile(recordFile(t, machine))

	err := session.RenamePreset("r1", "Rails")
	if err != nil {
		t.Fatal(err)
	}

	reopened := newSession(t, machine, repo)
	if got, want := library(reopened), []string{"Rails r1 2", "Writing w1 1"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}

	if view := reopened.View(); !slices.Equal(view.Presets, []string{"Rails"}) || view.Unsaved != 0 {
		t.Errorf("Presets = %q with %d unsaved, want Rails with none", view.Presets, view.Unsaved)
	}

	after, _ := os.ReadFile(settingsLocal(repo))
	recordAfter, _ := os.ReadFile(recordFile(t, machine))

	if string(after) != string(settings) || string(recordAfter) != string(record) {
		t.Error("the rename rewrote the project")
	}
}

func TestAPresetNeedsAFreeNameThatCanNameItsFile(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", " ", "writing", "a/b", ".hidden"} {
		machine := equiptest.WithPresets(t)
		session := newSession(t, machine, machine.Root)

		_, createErr := session.CreatePreset(name)
		renameErr := session.RenamePreset("r1", name)

		if !errors.Is(createErr, equip.ErrPresetName) || !errors.Is(renameErr, equip.ErrPresetName) {
			t.Errorf("%q: CreatePreset = %v, RenamePreset = %v, want ErrPresetName", name, createErr, renameErr)
		}

		got, want := library(newSession(t, machine, machine.Root)), []string{"Ruby r1 2", "Writing w1 1"}
		if !slices.Equal(got, want) {
			t.Errorf("%q: library = %q, want %q", name, got, want)
		}
	}
}

func TestMembersShowTheirStateCostAndOverrideHere(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	session.SetState("lint", equip.ManualOnly)
	docs := row(t, session.View(), "docs").Cost

	var got []string

	for _, preset := range session.Presets() {
		for _, m := range preset.Members {
			if m.Installed {
				got = append(got, fmt.Sprintf("%s %s %d ovr=%t", m.Name, m.State, m.Cost, m.Override))
			}
		}
	}

	want := []string{"lint manual-only 0 ovr=true", fmt.Sprintf("docs on %d ovr=false", docs)}
	if docs == 0 || !slices.Equal(got, want) {
		t.Errorf("members = %q, want %q", got, want)
	}
}

func TestLibraryNamesTheProjectsThatUseAPreset(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	app := newSession(t, machine, repo)
	app.SetPresets([]string{"r1"})
	save(t, app)

	if got := newSession(t, machine, machine.Repo("other")).Presets()[0].Projects; !slices.Equal(got, []string{repo}) {
		t.Errorf("Ruby's projects = %q, want %q", got, repo)
	}
}

func TestWritingAnActivePresetRewritesThisProjectAndKeepsPendingTogglesPending(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)

	record, _ := os.ReadFile(recordFile(t, machine))

	session.SetState("docs", equip.ManualOnly)
	add(t, session, "r1", "review")
	write(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"docs": "off", "lint": "on", "review": "on"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	view := session.View()
	if docs, review := row(t, view, "docs"), row(t, view, "review"); docs.State != equip.ManualOnly || !docs.Unsaved ||
		review.State != equip.On || review.Unsaved || view.Unsaved != 1 {
		t.Errorf("docs = %+v, review = %+v, %d unsaved; want only docs pending", docs, review, view.Unsaved)
	}

	if after, _ := os.ReadFile(recordFile(t, machine)); string(after) == string(record) {
		t.Errorf("record keeps Ruby's old hash:\n%s", after)
	}
}

func TestWriteStopsWhenThisProjectChangedOutsideSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)
	add(t, session, "r1", "review")
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "on", "lint": "on", "review": "off"}}`)

	err := session.WritePreset()
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Errorf("WritePreset = %v, want ErrChangedSinceOpen", err)
	}

	if got := members(newSession(t, machine, repo), "r1"); !slices.Equal(got, []string{"lint =", "rspec ="}) {
		t.Errorf("Ruby's members on disk = %q, want lint and rspec", got)
	}

	if !session.Presets()[0].Unwritten {
		t.Error("Ruby lost its unwritten edits")
	}
}

func TestPreviewShowsWhatTheWriteChangesInTheSavedStatesAndWritesNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)
	// Pending, so the write still rewrites the project with Ruby active.
	session.SetPresets(nil)
	add(t, session, "r1", "review")

	before, after, _ := session.PreviewWrite()

	if was, got := row(t, before, "review"), row(t, after, "review"); was.State != equip.Off || got.State != equip.On {
		t.Errorf("review = %s, then %s after the write; want off, then on", was.State, got.State)
	}

	if got, was := after.Totals[equip.ClaudeCode], before.Totals[equip.ClaudeCode]; got <= was {
		t.Errorf("Claude Code total after the write = %d, want more than %d", got, was)
	}

	if view := session.View(); len(view.Presets) != 0 || !session.Presets()[0].Unwritten {
		t.Errorf("Presets = %q, want none pending with Ruby's edit still unwritten", view.Presets)
	}

	if got := members(newSession(t, machine, repo), "r1"); !slices.Equal(got, []string{"lint =", "rspec ="}) {
		t.Errorf("Ruby's members on disk = %q, want lint and rspec", got)
	}
}

func TestPreviewOfAPresetOnlyPendingActiveChangesNothingHere(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	session.SetPresets([]string{"r1"})
	add(t, session, "r1", "review")

	before, after, _ := session.PreviewWrite()

	if turned := after.TurnedSince(before); len(turned) != 0 {
		t.Errorf("turned = %+v, want none", turned)
	}
}

func TestAFailedWriteKeepsTheEditsToWriteAgain(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	session := newSession(t, machine, repo)
	session.SetPresets([]string{"r1"})
	save(t, session)
	add(t, session, "r1", "review")

	records := filepath.Dir(recordFile(t, machine))
	record, _ := os.ReadFile(recordFile(t, machine))
	chmod(t, records, 0o500)

	err := session.WritePreset()
	if err == nil || !session.Presets()[0].Unwritten {
		t.Fatalf("WritePreset = %v with the record unwritable, want an error and the edit kept", err)
	}

	chmod(t, records, 0o750)
	write(t, session)

	if after, _ := os.ReadFile(recordFile(t, machine)); string(after) == string(record) || session.Presets()[0].Unwritten {
		t.Errorf("the second write did not land:\n%s", after)
	}
}

// chmod sets the mode of path, and gives the owner back full access once the
// test ends, so its temp dir can go.
func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	err := os.Chmod(path, mode)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
}

func TestRenamingANewPresetNamesTheFileItsWriteCreates(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	docsID := create(t, session, "Docs")

	err := session.RenamePreset(docsID, "Notes")
	if err != nil {
		t.Fatal(err)
	}

	write(t, session)

	got := library(newSession(t, machine, machine.Root))
	if want := []string{"Notes " + docsID + " 0", "Ruby r1 2", "Writing w1 1"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

// recordFile is the one record on machine.
func recordFile(t *testing.T, machine *equiptest.Machine) string {
	t.Helper()

	files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))
	if len(files) != 1 {
		t.Fatalf("records = %q, want one", files)
	}

	return files[0]
}

// remove removes the extension with key from the preset with id in session.
func remove(t *testing.T, session *equip.Session, id, key string) {
	t.Helper()

	err := session.RemoveMember(id, key)
	if err != nil {
		t.Fatal(err)
	}
}

// add adds the extension with key to the preset with id in session.
func add(t *testing.T, session *equip.Session, id, key string) {
	t.Helper()

	err := session.AddMember(id, key)
	if err != nil {
		t.Fatal(err)
	}
}

// using opens repo on machine and saves the presets with ids active there.
func using(t *testing.T, machine *equiptest.Machine, repo string, ids ...string) {
	t.Helper()

	session := newSession(t, machine, repo)
	session.SetPresets(ids)
	save(t, session)
}

func TestWriteRewritesTheOtherProjectsThatUseThePreset(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	other := machine.Repo("other")
	using(t, machine, other, "r1")
	session := newSession(t, machine, machine.Repo("app"))

	add(t, session, "r1", "review")
	write(t, session)

	got := readJSON(t, settingsLocal(other))["skillOverrides"]
	if want := map[string]any{"docs": "off", "lint": "on", "review": "on"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides there = %v, want %v", got, want)
	}

	if n := newSession(t, machine, other).View().Unsaved; n != 0 {
		t.Errorf("Unsaved there = %d, want 0: the record keeps Ruby's old hash", n)
	}
}

// handEdited is a machine with Ruby saved active in the projects app and
// other, and a hand edit that turns docs on in other. It returns the path of
// each and the files of other, its settings and its record, as they are.
func handEdited(t *testing.T) (*equiptest.Machine, string, string, map[string]string) {
	t.Helper()
	machine := equiptest.WithPresets(t)
	app, other := machine.Repo("app"), machine.Repo("other")
	using(t, machine, app, "r1")
	using(t, machine, other, "r1")
	writeFile(t, settingsLocal(other), `{"skillOverrides": {"docs": "on", "lint": "on", "review": "off"}}`)

	files := map[string]string{}

	for _, path := range []string{settingsLocal(other), otherRecord(t, machine, other)} {
		data, _ := os.ReadFile(path)
		files[path] = string(data)
	}

	return machine, app, other, files
}

// otherRecord is the record of the project at path on machine.
func otherRecord(t *testing.T, machine *equiptest.Machine, path string) string {
	t.Helper()

	files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", filepath.Base(path)+"-*.toml"))
	if len(files) != 1 {
		t.Fatalf("records of %s = %q, want one", path, files)
	}

	return files[0]
}

// unchanged reports an error for each of files whose content changed.
func unchanged(t *testing.T, files map[string]string) {
	t.Helper()

	for path, was := range files {
		if now, _ := os.ReadFile(path); string(now) != was {
			t.Errorf("%s changed:\n%s", path, now)
		}
	}
}

// affected is each project of others as "path: turned", each row that turns
// there as "name state", or as "path: skipped: why".
func affected(others []equip.Affected) []string {
	var out []string

	for _, project := range others {
		if project.Skipped != "" {
			out = append(out, project.Path+": skipped: "+project.Skipped)

			continue
		}

		var turned []string
		for _, row := range project.Turned {
			turned = append(turned, row.Name+" "+row.State.String())
		}

		out = append(out, project.Path+": "+strings.Join(turned, ", "))
	}

	return out
}

func TestPreviewListsWhatTurnsInEachOtherProjectAndWritesNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	other := machine.Repo("other")
	using(t, machine, other, "w1")
	using(t, machine, machine.Repo("ruby"), "r1")

	settings, _ := os.ReadFile(settingsLocal(other))
	session := newSession(t, machine, machine.Repo("app"))
	add(t, session, "w1", "review")
	remove(t, session, "w1", "docs")

	_, _, others := session.PreviewWrite()

	if got, want := affected(others), []string{other + ": docs off, review on"}; !slices.Equal(got, want) {
		t.Errorf("affected = %q, want %q", got, want)
	}

	unchanged(t, map[string]string{settingsLocal(other): string(settings)})
}

func TestWriteSkipsAProjectWithHandEdits(t *testing.T) {
	t.Parallel()
	machine, app, other, files := handEdited(t)
	session := newSession(t, machine, app)

	add(t, session, "r1", "review")

	_, _, others := session.PreviewWrite()
	if got, want := affected(others), []string{other + ": skipped: changed outside equip"}; !slices.Equal(got, want) {
		t.Errorf("affected = %q, want %q", got, want)
	}

	write(t, session)

	unchanged(t, files)
}

func TestASkippedProjectShowsThePresetChangeUnsavedOnItsNextOpen(t *testing.T) {
	t.Parallel()
	machine, app, other, _ := handEdited(t)
	session := newSession(t, machine, app)
	add(t, session, "r1", "review")
	write(t, session)

	review := row(t, newSession(t, machine, other).View(), "review")
	if review.State != equip.On || !review.Unsaved || review.Override {
		t.Errorf("review there = %+v, want on and unsaved, not an override", review)
	}
}

func TestWriteSkipsAProjectWhosePresetChangedOutsideSinceItsSave(t *testing.T) {
	t.Parallel()
	machine, app, other, files := handEdited(t)
	first := newSession(t, machine, app)
	add(t, first, "r1", "review")
	write(t, first)
	// Skipped by the first write, other has no hand edit the change check
	// can see now, as its record's hash is Ruby's old one.
	session := newSession(t, machine, app)
	remove(t, session, "r1", "review")

	_, _, others := session.PreviewWrite()
	if got, want := affected(others), []string{other + ": skipped: changed outside equip"}; !slices.Equal(got, want) {
		t.Errorf("affected = %q, want %q", got, want)
	}

	write(t, session)

	unchanged(t, files)
}

func TestDeleteRemovesThePresetFromEveryProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	app, other, gone := machine.Repo("app"), machine.Repo("other"), machine.Repo("gone")
	using(t, machine, app, "r1", "w1")
	using(t, machine, other, "r1")
	using(t, machine, gone, "r1")

	err := os.RemoveAll(gone)
	if err != nil {
		t.Fatal(err)
	}

	session := newSession(t, machine, app)

	err = session.DeletePreset("r1")
	if err != nil {
		t.Fatal(err)
	}

	if view := session.View(); !slices.Equal(view.Presets, []string{"Writing"}) || view.Unsaved != 0 {
		t.Errorf("Presets = %q with %d unsaved, want Writing with none", view.Presets, view.Unsaved)
	}

	here := newSession(t, machine, app)
	if got, want := library(here), []string{"Writing w1 1"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}

	// Ruby turned lint on in app, where Writing stays, and docs off in other.
	type turned struct {
		name  string
		state equip.State
	}

	for path, want := range map[string]turned{app: {"lint", equip.Off}, other: {"docs", equip.On}} {
		view := newSession(t, machine, path).View()
		if got := row(t, view, want.name); got.State != want.state || view.Unsaved != 0 {
			t.Errorf("%s: %s = %s with %d unsaved, want %s with none",
				path, want.name, got.State, view.Unsaved, want.state)
		}
	}

	if data, _ := os.ReadFile(otherRecord(t, machine, gone)); strings.Contains(string(data), "r1") {
		t.Errorf("the record of the gone project keeps Ruby:\n%s", data)
	}
}

func TestDeleteStopsWhenThisProjectChangedOutsideSinceOpen(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	using(t, machine, repo, "r1")
	session := newSession(t, machine, repo)
	writeFile(t, settingsLocal(repo), `{"skillOverrides": {"docs": "on", "lint": "on", "review": "off"}}`)

	err := session.DeletePreset("r1")
	if !errors.Is(err, equip.ErrChangedSinceOpen) {
		t.Errorf("DeletePreset = %v, want ErrChangedSinceOpen", err)
	}

	got, want := library(newSession(t, machine, repo)), []string{"Ruby r1 2", "Writing w1 1"}
	if !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

func TestDeleteOfAPresetNotUsedHereLeavesThisProjectAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	repo := machine.Repo("app")
	using(t, machine, repo, "r1")

	edited := `{"skillOverrides": {"docs": "on", "lint": "on", "review": "off"}}`
	writeFile(t, settingsLocal(repo), edited)

	err := newSession(t, machine, repo).DeletePreset("w1")
	if err != nil {
		t.Fatal(err)
	}

	unchanged(t, map[string]string{settingsLocal(repo): edited})
}

func TestDeleteDropsThePresetsUnwrittenEdits(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	add(t, session, "r1", "review")

	err := session.DeletePreset("r1")
	if err != nil || session.Unwritten() {
		t.Errorf("DeletePreset = %v, unwritten edits %t; want nil and none", err, session.Unwritten())
	}
}

func TestDeleteOfAPresetNotWrittenYetWritesNothing(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	session := newSession(t, machine, machine.Root)
	notesID := create(t, session, "Notes")

	err := session.DeletePreset(notesID)
	if err != nil || session.Unwritten() {
		t.Errorf("DeletePreset = %v, unwritten edits %t; want nil and none", err, session.Unwritten())
	}

	if got, want := library(session), []string{"Ruby r1 2", "Writing w1 1"}; !slices.Equal(got, want) {
		t.Errorf("library = %q, want %q", got, want)
	}
}

func TestDeleteOfNoSuchPresetFails(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)

	err := newSession(t, machine, machine.Root).DeletePreset("x1")
	if err == nil {
		t.Error("DeletePreset of no such preset = nil, want an error")
	}
}

func TestDeleteSkipsAProjectWithHandEditsButForgetsThePresetThere(t *testing.T) {
	t.Parallel()
	machine, app, other, files := handEdited(t)
	session := newSession(t, machine, app)

	_, _, others := session.PreviewDelete("r1")
	if got, want := affected(others), []string{other + ": skipped: changed outside equip"}; !slices.Equal(got, want) {
		t.Errorf("affected = %q, want %q", got, want)
	}

	err := session.DeletePreset("r1")
	if err != nil {
		t.Fatal(err)
	}

	unchanged(t, map[string]string{settingsLocal(other): files[settingsLocal(other)]})

	if data, _ := os.ReadFile(otherRecord(t, machine, other)); strings.Contains(string(data), "r1") {
		t.Errorf("the record there keeps Ruby:\n%s", data)
	}
}

func TestWriteReportsAnotherProjectItFailedToRewrite(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	other := machine.Repo("other")
	using(t, machine, other, "r1")
	writeFile(t, settingsLocal(other), "{")
	session := newSession(t, machine, machine.Repo("app"))
	add(t, session, "r1", "review")

	err := session.WritePreset()
	if err == nil || !strings.Contains(err.Error(), "settings.local.json") {
		t.Errorf("WritePreset = %v, want the error of other's settings", err)
	}
}

func TestWriteSkipsAProjectEquipCannotRead(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	other := machine.Repo("other")
	using(t, machine, other, "r1")

	rec := otherRecord(t, machine, other)
	data, _ := os.ReadFile(rec)
	broken := strings.Replace(string(data), "[overrides.skills]", "[overrides.skills]\ndocs = 'bogus'", 1)
	writeFile(t, rec, broken)

	session := newSession(t, machine, machine.Repo("app"))
	add(t, session, "r1", "review")

	_, _, others := session.PreviewWrite()
	if got := affected(others); len(got) != 1 || !strings.Contains(got[0], ": skipped: read "+rec) {
		t.Errorf("affected = %q, want other skipped with the read error", got)
	}

	write(t, session)

	unchanged(t, map[string]string{rec: broken})
}

func TestWriteSkipsAProjectWhosePathIsGone(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	gone := machine.Repo("gone")
	using(t, machine, gone, "r1")

	err := os.RemoveAll(gone)
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{}
	data, _ := os.ReadFile(otherRecord(t, machine, gone))
	files[otherRecord(t, machine, gone)] = string(data)
	session := newSession(t, machine, machine.Repo("app"))
	add(t, session, "r1", "review")

	_, _, others := session.PreviewWrite()
	if got, want := affected(others), []string{gone + ": skipped: path no longer exists"}; !slices.Equal(got, want) {
		t.Errorf("affected = %q, want %q", got, want)
	}

	write(t, session)

	unchanged(t, files)
}
