package equip_test

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// abc writes the skill abc into skills, with a when_to_use when whenToUse is
// not empty.
func abc(t *testing.T, skills, description, whenToUse string) {
	t.Helper()

	front := "name: abc\ndescription: " + description + "\n"
	if whenToUse != "" {
		front += "when_to_use: " + whenToUse + "\n"
	}

	writeFile(t, filepath.Join(skills, "abc", "SKILL.md"), "---\n"+front+"---\nThe body is not counted.\n")
}

func codexSkills(machine *equiptest.Machine) string {
	return filepath.Join(machine.Home, ".agents", "skills")
}

func row(t *testing.T, view equip.View, name string) equip.Row {
	t.Helper()

	i := slices.IndexFunc(view.Rows, func(r equip.Row) bool { return r.Name == name })
	if i < 0 {
		t.Fatalf("no row %q", name)
	}

	return view.Rows[i]
}

func TestClaudeCodeSkillCostsItsNameAndDescriptionBytesOverThree(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// 3 + 27 = 30 bytes.
	abc(t, machine.ClaudeSkills(), strings.Repeat("x", 27), "")

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 10 {
		t.Errorf("Cost = %d, want 10", got)
	}
}

func TestCostRoundsUp(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// 3 + 28 = 31 bytes.
	abc(t, machine.ClaudeSkills(), strings.Repeat("x", 28), "")

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 11 {
		t.Errorf("Cost = %d, want 11", got)
	}
}

func TestManualOnlySkillCostsNothingInClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, machine.Root)
	session.SetState("review", equip.ManualOnly)

	if got := row(t, session.View(), "review").Cost; got != 0 {
		t.Errorf("Cost = %d, want 0", got)
	}
}

func TestOffSkillCostsNothingInClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	session := newSession(t, machine, machine.Root)
	session.SetState("review", equip.Off)

	if got := row(t, session.View(), "review").Cost; got != 0 {
		t.Errorf("Cost = %d, want 0", got)
	}
}

func TestClaudeCodeSkillCostCountsWhenToUse(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// 3 + 12 + 15 = 30 bytes.
	abc(t, machine.ClaudeSkills(), strings.Repeat("x", 12), strings.Repeat("y", 15))

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 10 {
		t.Errorf("Cost = %d, want 10", got)
	}
}

func TestClaudeCodeSkillCostCapsTheTextAt1536Characters(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// "é" is 2 bytes: 3 + 1,536 × 2 = 3,075 bytes.
	abc(t, machine.ClaudeSkills(), strings.Repeat("é", 1000), strings.Repeat("é", 600))

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 1025 {
		t.Errorf("Cost = %d, want 1025", got)
	}
}

func TestClaudeCodeSkillCostComesFromThePersonalCopy(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// 3 + 27 = 30 bytes; the project copy loses to the personal one.
	abc(t, machine.ClaudeSkills(), strings.Repeat("x", 27), "")
	abc(t, claudeProjectSkills(repo), strings.Repeat("x", 90), "")

	if got := row(t, open(t, machine, repo), "abc").Cost; got != 10 {
		t.Errorf("Cost = %d, want 10", got)
	}
}

func TestCodexSkillCostsItsNameAndDescriptionBytesOverFour(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// 3 + 29 = 32 bytes.
	abc(t, codexSkills(machine), strings.Repeat("x", 29), "")

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 8 {
		t.Errorf("Cost = %d, want 8", got)
	}
}

func TestCodexSkillCostLeavesOutWhenToUse(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// 3 + 13 = 16 bytes.
	abc(t, codexSkills(machine), strings.Repeat("x", 13), strings.Repeat("y", 50))

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 4 {
		t.Errorf("Cost = %d, want 4", got)
	}
}

func TestCodexSkillCostCapsTheDescriptionAt1024Characters(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// "é" is 2 bytes: 3 + 1,024 × 2 = 2,051 bytes.
	abc(t, codexSkills(machine), strings.Repeat("é", 1200), "")

	if got := row(t, open(t, machine, machine.Root), "abc").Cost; got != 513 {
		t.Errorf("Cost = %d, want 513", got)
	}
}

func TestOffSkillKeepsItsFullCostInCodex(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// 3 + 29 = 32 bytes.
	abc(t, codexSkills(machine), strings.Repeat("x", 29), "")
	session := newSession(t, machine, machine.Root)
	session.SetState("abc", equip.Off)

	if got := row(t, session.View(), "abc").Cost; got != 8 {
		t.Errorf("Cost = %d, want 8", got)
	}
}

func TestDetailShowsTheCostInEachAgent(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// "review" and "The review skill.": 23 bytes.
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(codexSkills(machine), "review")
	session := newSession(t, machine, machine.Root)
	session.SetState("review", equip.ManualOnly)

	want := map[equip.Agent]int{equip.ClaudeCode: 0, equip.Codex: 6}
	if got := session.Detail("review").Costs; !maps.Equal(got, want) {
		t.Errorf("Costs = %v, want %v", got, want)
	}
}

func TestTotalSumsTheCostsInClaudeCodeBeforeSaving(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review") // 23 bytes: 8
	machine.Skill(machine.ClaudeSkills(), "docs")   // 19 bytes: 7
	machine.Skill(machine.ClaudeSkills(), "lint")   // 19 bytes: 7
	session := newSession(t, machine, machine.Root)
	session.SetState("docs", equip.Off)

	want := map[equip.Agent]int{equip.ClaudeCode: 15, equip.Codex: 0}
	if got := session.View().Totals; !maps.Equal(got, want) {
		t.Errorf("Totals = %v, want %v", got, want)
	}
}

func TestCodexTotalCountsTheSkillsIntroOnce(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(codexSkills(machine), "review") // 23 bytes: 6
	machine.Skill(codexSkills(machine), "docs")   // 19 bytes: 5

	want := map[equip.Agent]int{equip.ClaudeCode: 0, equip.Codex: 711}
	if got := open(t, machine, machine.Root).Totals; !maps.Equal(got, want) {
		t.Errorf("Totals = %v, want %v", got, want)
	}
}
