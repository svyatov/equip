package equip_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

	before, after := session.PreviewWrite()

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

	before, after := session.PreviewWrite()

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
