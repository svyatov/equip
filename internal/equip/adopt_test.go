package equip_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// savedRepo saves github off in a repo with a commit and returns the repo.
// ~/.claude.json keeps the entry under the repo's path.
func savedRepo(t *testing.T, machine *equiptest.Machine) string {
	t.Helper()

	repo := machine.Repo("app")
	machine.Commit(repo)
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.Off)
	save(t, session)

	return repo
}

// movedRepo moves a savedRepo and returns the old path and the new one. Only
// the record carries github's state to the new one.
func movedRepo(t *testing.T, machine *equiptest.Machine) (string, string) {
	t.Helper()

	repo := savedRepo(t, machine)
	moved := filepath.Join(machine.Root, "moved")

	err := os.Rename(repo, moved)
	if err != nil {
		t.Fatal(err)
	}

	return repo, moved
}

func TestFirstOpenOffersTheRecordOfAMovedRepo(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	old, moved := movedRepo(t, machine)

	if got, want := open(t, machine, moved).Orphans, []string{old}; !slices.Equal(got, want) {
		t.Errorf("Orphans = %q, want %q", got, want)
	}
}

func TestNoRecordOfAnotherMovedRepoIsOffered(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	movedRepo(t, machine)
	other := machine.Repo("other")
	// Its own message, as equal empty commits in one second share a hash.
	machine.RunGit(other, "commit", "-q", "--allow-empty", "-m", "other")

	if got := open(t, machine, other).Orphans; len(got) != 0 {
		t.Errorf("Orphans = %q, want none", got)
	}
}

func TestABrokenRecordIsNotOffered(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	root := machine.Commit(repo)
	gone := filepath.Join(machine.Root, "gone")
	writeFile(t, filepath.Join(machine.StateHome, "equip", "gone.toml"),
		"path = \""+gone+"\"\nroot_commit = \""+root+"\"\noverrides = 5\n")

	if got := open(t, machine, repo).Orphans; len(got) != 0 {
		t.Errorf("Orphans = %q, want none", got)
	}
}

func TestNotAdoptingImportsTheStatesSetByHand(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, moved := movedRepo(t, machine)
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(moved), `{"skillOverrides": {"review": "off"}}`)

	got := states(open(t, machine, moved))
	if want := []string{"MCP server github on", "skill review off"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestAdoptingMovesTheRecordToTheProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	old, moved := movedRepo(t, machine)
	session := newSession(t, machine, moved)

	err := session.Adopt(old)
	if err != nil {
		t.Fatal(err)
	}

	// ~/.claude.json has no entry under the new path yet: a save writes it.
	view := session.View()
	if row := view.Rows[0]; row.State != equip.Off || !row.Override || !row.Unsaved || len(view.Orphans) != 0 {
		t.Errorf("github = %+v, Orphans = %q, want an unsaved override off and none", row, view.Orphans)
	}

	rec := readRecord(t, machine)

	overrides, _ := rec["overrides"].(map[string]any)
	if rec["path"] != moved || !reflect.DeepEqual(overrides["mcp_servers"], map[string]any{"github": "off"}) {
		t.Errorf("record = %v, want path %s with github off", rec, moved)
	}
}

func TestAdoptingInASecondSessionKeepsTheRecordTheFirstAdopted(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	old, moved := movedRepo(t, machine)
	first, second := newSession(t, machine, moved), newSession(t, machine, moved)

	err := first.Adopt(old)
	if err != nil {
		t.Fatal(err)
	}

	err = second.Adopt(old)

	overrides, _ := readRecord(t, machine)["overrides"].(map[string]any)
	if err == nil || !reflect.DeepEqual(overrides["mcp_servers"], map[string]any{"github": "off"}) {
		t.Errorf("second Adopt = %v, record overrides %v, want an error and github off", err, overrides)
	}
}

func TestSavingWithNothingToWriteEndsTheOffer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, moved := movedRepo(t, machine)
	// As in a fresh clone, which git leaves without it.
	err := os.Remove(settingsLocal(moved))
	if err != nil {
		t.Fatal(err)
	}

	session := newSession(t, machine, moved)
	save(t, session)

	if got := open(t, machine, moved).Orphans; len(got) != 0 || len(session.View().Orphans) != 0 {
		t.Errorf("Orphans = %q, want none, in this session too", got)
	}

	_, err = os.Stat(settingsLocal(moved))
	if err == nil {
		t.Error("save created settings.local.json with nothing to write")
	}
}

func TestNoRecordIsOfferedOnceTheProjectHasItsOwn(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, moved := movedRepo(t, machine)
	session := newSession(t, machine, moved)
	session.SetState("mcp:github", equip.Off)
	save(t, session)

	if got := open(t, machine, moved).Orphans; len(got) != 0 {
		t.Errorf("Orphans = %q, want none", got)
	}
}

func TestNoRecordIsOfferedWithoutARootCommit(t *testing.T) {
	t.Parallel()

	for name, dir := range map[string]func(*equiptest.Machine, string) string{
		"outside git":     func(m *equiptest.Machine, name string) string { return m.Mkdir(filepath.Join(m.Root, name)) },
		"with no commits": (*equiptest.Machine).Repo,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			machine := equiptest.New(t)
			old := dir(machine, "app")
			machine.Skill(machine.ClaudeSkills(), "review")
			session := newSession(t, machine, old)
			session.SetState("review", equip.Off)
			save(t, session)

			err := os.RemoveAll(old)
			if err != nil {
				t.Fatal(err)
			}

			if got := open(t, machine, dir(machine, "other")).Orphans; len(got) != 0 {
				t.Errorf("Orphans = %q, want none", got)
			}
		})
	}
}

func TestSecondCloneOfARepoKeepsItsOwnRecord(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := savedRepo(t, machine)
	clone := filepath.Join(machine.Root, "clone")
	machine.RunGit(machine.Root, "clone", "-q", repo, clone)

	view := open(t, machine, clone)
	if len(view.Orphans) != 0 || view.Rows[0].State != equip.On {
		t.Errorf("Orphans = %q, github %v, want none and on", view.Orphans, view.Rows[0].State)
	}

	err := newSession(t, machine, clone).Adopt(repo)
	if err == nil || readRecord(t, machine)["path"] != repo {
		t.Errorf("Adopt(%s) = %v, want an error and the record left to it", repo, err)
	}
}
