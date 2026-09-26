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

// movedRepo saves github off in a repo with a commit, then moves the repo. It
// returns the old path and the new one. ~/.claude.json keeps the entry under
// the old path, so only the record carries the state to the new one.
func movedRepo(t *testing.T, machine *equiptest.Machine) (string, string) {
	t.Helper()

	repo := machine.Repo("app")
	machine.Commit(repo)
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.Off)
	save(t, session)

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
	repo := machine.Repo("app")
	machine.Commit(repo)
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"github": {"command": "gh"}}}`)
	session := newSession(t, machine, repo)
	session.SetState("mcp:github", equip.Off)
	save(t, session)

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
