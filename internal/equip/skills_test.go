package equip_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func claudeProjectSkills(dir string) string { return filepath.Join(dir, ".claude", "skills") }

func TestViewListsClaudeCodeProjectSkillsFromStartDirUpToRepoRoot(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(claudeProjectSkills(machine.Root), "above-repo")
	repo := machine.Repo("app")
	lib := filepath.Join(repo, "lib")
	start := filepath.Join(lib, "deep")

	machine.Skill(claudeProjectSkills(repo), "at-root")
	machine.Skill(claudeProjectSkills(lib), "in-parent")
	machine.Skill(claudeProjectSkills(start), "in-start")
	machine.Skill(claudeProjectSkills(filepath.Join(start, "below")), "below-start")

	got := names(open(t, machine, start))
	if want := []string{"at-root", "in-parent", "in-start"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestSkillWalkStopsAtRepoRootTypedInAnotherCase(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(claudeProjectSkills(machine.Root), "above-repo")
	machine.Skill(filepath.Join(machine.Repo("App"), ".agents", "skills"), "at-root")

	typed := filepath.Join(machine.Root, "app")

	_, err := os.Stat(typed)
	if err != nil {
		t.Skip("the file system is case-sensitive")
	}

	got := names(open(t, machine, typed))
	if want := []string{"at-root"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewListsCodexRepoSkillsFromRepoRootDownToStartDir(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Root, ".agents", "skills"), "above-repo")
	repo := machine.Repo("app")
	start := filepath.Join(repo, "lib")
	machine.Skill(filepath.Join(repo, ".agents", "skills"), "agents-at-root")
	machine.Skill(filepath.Join(start, ".codex", "skills"), "codex-in-start")
	machine.Skill(filepath.Join(start, "below", ".agents", "skills"), "below-start")

	got := names(open(t, machine, start))
	if want := []string{"agents-at-root", "codex-in-start"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestViewListsCodexUserAndSystemSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Home, ".agents", "skills"), "agents-user")
	machine.Skill(filepath.Join(machine.CodexHome, "skills"), "codex-home")
	machine.Skill(filepath.Join(machine.CodexHome, "skills", ".system"), "bundled")
	machine.Skill(filepath.Join(machine.CodexSystem, "skills"), "admin")

	got := names(open(t, machine, machine.Mkdir(filepath.Join(machine.Root, "scratch"))))
	if want := []string{"admin", "agents-user", "bundled", "codex-home"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestSkillInSeveralDirsOfBothAgentsIsOneRowWithEveryLocation(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	claude := machine.Skill(machine.ClaudeSkills(), "review")
	project := machine.Skill(claudeProjectSkills(repo), "review")
	codex := machine.Skill(filepath.Join(machine.Home, ".agents", "skills"), "review")
	writeFile(t, filepath.Join(codex, "SKILL.md"), "---\nname: review\ndescription: The Codex copy.\n---\n")
	session := newSession(t, machine, repo)

	if got := names(session.View()); !slices.Equal(got, []string{"review"}) {
		t.Errorf("rows = %q, want one review row", got)
	}

	detail := session.Detail("review")

	wantLocations := []equip.Location{
		{Path: claude, Agent: equip.ClaudeCode},
		{Path: project, Agent: equip.ClaudeCode},
		{Path: codex, Agent: equip.Codex},
	}
	if !slices.Equal(detail.Locations, wantLocations) {
		t.Errorf("Locations = %+v, want %+v", detail.Locations, wantLocations)
	}

	if want := []equip.Agent{equip.ClaudeCode, equip.Codex}; !slices.Equal(detail.Agents, want) {
		t.Errorf("Agents = %v, want %v", detail.Agents, want)
	}

	if want := "The review skill."; detail.Description != want {
		t.Errorf("Description = %q, want %q", detail.Description, want)
	}
}

func TestSkillStateIsNotAppliedInCodex(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "claude-only")
	machine.Skill(machine.ClaudeSkills(), "both")
	machine.Skill(filepath.Join(machine.Home, ".agents", "skills"), "both")
	session := newSession(t, machine, machine.Root)

	if got := session.Detail("both").NotApplied; len(got) != 1 || got[equip.Codex] == "" {
		t.Errorf("both: NotApplied = %q, want a reason for Codex only", got)
	}

	if got := session.Detail("claude-only").NotApplied; len(got) != 0 {
		t.Errorf("claude-only: NotApplied = %q, want none", got)
	}
}

func TestSaveWritesNoClaudeCodeEntryForACodexOnlySkill(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(filepath.Join(repo, ".agents", "skills"), "codex-only")
	session := newSession(t, machine, repo)
	session.SetState("review", equip.Off)
	session.SetState("codex-only", equip.Off)

	save(t, session)

	got := readJSON(t, settingsLocal(repo))["skillOverrides"]
	if want := map[string]any{"review": "off"}; !reflect.DeepEqual(got, want) {
		t.Errorf("skillOverrides = %v, want %v", got, want)
	}

	if n := session.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d after save, want 0", n)
	}
}

func TestSkillDirReachedTwiceIsOneLocation(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	claude := machine.Skill(machine.ClaudeSkills(), "review")
	agents := machine.Skill(filepath.Join(machine.Home, ".agents", "skills"), "review")
	codex := machine.Skill(filepath.Join(machine.CodexHome, "skills"), "review")

	got := newSession(t, machine, machine.Home).Detail("review").Locations

	want := []equip.Location{
		{Path: claude, Agent: equip.ClaudeCode},
		{Path: agents, Agent: equip.Codex},
		{Path: codex, Agent: equip.Codex},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Locations = %+v, want %+v", got, want)
	}
}

func TestSkillDirUnderAFileIsSkipped(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, machine.CodexSystem, "not a dir")

	got := names(open(t, machine, machine.Root))
	if want := []string{"review"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

// unreadable makes dir unreadable until the test ends.
func unreadable(t *testing.T, dir string) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root reads every dir")
	}

	err := os.Chmod(dir, 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestUnreadableSkillDirIsSkipped(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	unreadable(t, machine.Mkdir(filepath.Join(machine.CodexSystem, "skills")))

	got := names(open(t, machine, machine.Root))
	if want := []string{"review"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestOpenFailsOnUnreadableClaudeCodeUserSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	unreadable(t, machine.Mkdir(machine.ClaudeSkills()))

	_, err := equip.Open(machine.Machine, machine.Root)
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Open error = %v, want permission denied", err)
	}
}

// skillDescription writes a skill whose frontmatter holds description as
// written and returns the description the Session reads from it.
func skillDescription(t *testing.T, description string) string {
	t.Helper()
	machine := equiptest.New(t)
	dir := machine.Skill(machine.ClaudeSkills(), "review")
	skill := "---\nname: review\ndescription:" + description + "\nlicense: MIT\n---\nBody.\n"
	writeFile(t, filepath.Join(dir, "SKILL.md"), skill)

	return newSession(t, machine, machine.Root).Detail("review").Description
}

func TestSkillDescriptionReadsALiteralBlock(t *testing.T) {
	t.Parallel()

	got := skillDescription(t, " |\n  Reviews code.\n  Use before a merge.")
	if want := "Reviews code.\nUse before a merge."; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

func TestSkillDescriptionFoldsAFoldedBlock(t *testing.T) {
	t.Parallel()

	got := skillDescription(t, " >-\n  Reviews code.\n  Use before a merge.")
	if want := "Reviews code. Use before a merge."; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

func TestSkillDescriptionReadsAPlainValueOnTheNextLines(t *testing.T) {
	t.Parallel()

	got := skillDescription(t, "\n  Reviews code.\n  Use before a merge.")
	if want := "Reviews code. Use before a merge."; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

func TestSkillDescriptionUnquotesADoubleQuotedValue(t *testing.T) {
	t.Parallel()

	got := skillDescription(t, ` "Reviews \"code\": use before a merge."`)
	if want := `Reviews "code": use before a merge.`; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

func TestSkillDescriptionUnquotesASingleQuotedValue(t *testing.T) {
	t.Parallel()

	got := skillDescription(t, ` 'Reviews the user''s code.'`)
	if want := "Reviews the user's code."; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

// worktree makes a repo whose main checkout has the Claude Code skill
// "in-main" and the Codex skill "codex-in-main", which git does not track, and
// returns a worktree of it.
func worktree(machine *equiptest.Machine) string {
	repo := machine.Repo("app")
	machine.Commit(repo)
	machine.Skill(claudeProjectSkills(repo), "in-main")
	machine.Skill(filepath.Join(repo, ".agents", "skills"), "codex-in-main")

	return machine.Worktree(repo, "app-feature")
}

func TestWorktreeWithoutSkillsFallsBackToMainCheckoutSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	wt := worktree(machine)

	got := names(open(t, machine, wt))
	if want := []string{"in-main"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func TestWorktreeWithSkillsListsOnlyItsOwn(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	wt := worktree(machine)
	machine.Skill(claudeProjectSkills(wt), "in-worktree")
	machine.Skill(claudeProjectSkills(filepath.Dir(wt)), "above-worktree")

	got := names(open(t, machine, wt))
	if want := []string{"in-worktree"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}
