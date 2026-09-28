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

// writeSkill writes the skill abc into skills.
func writeSkill(t *testing.T, skills, description string) {
	t.Helper()

	skill := "---\nname: abc\ndescription: " + description + "\n---\nThe body is not counted.\n"
	writeFile(t, filepath.Join(skills, "abc", "SKILL.md"), skill)
}

func row(t *testing.T, view equip.View, name string) equip.Row {
	t.Helper()

	i := slices.IndexFunc(view.Rows, func(r equip.Row) bool { return r.Name == name })
	if i < 0 {
		t.Fatalf("no row %q", name)
	}

	return view.Rows[i]
}

// totalTokens is the tokens of each total in view.
func totalTokens(view equip.View) map[equip.Agent]int {
	tokens := map[equip.Agent]int{}
	for agent, total := range view.Totals {
		tokens[agent] = total.Tokens
	}

	return tokens
}

func TestClaudeCodeSkillCostComesFromThePersonalLocation(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// 3 + 27 = 30 bytes; the project Location loses to the personal one.
	writeSkill(t, machine.ClaudeSkills(), strings.Repeat("x", 27))
	writeSkill(t, claudeProjectSkills(repo), strings.Repeat("x", 90))

	if got := row(t, open(t, machine, repo), "abc").Cost; got != 10 {
		t.Errorf("Cost = %d, want 10", got)
	}
}

func TestDetailShowsTheCostInEachAgent(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// "review" and "The review skill.": 23 bytes.
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.CodexSkills(), "review")
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
	if got := totalTokens(session.View()); !maps.Equal(got, want) {
		t.Errorf("Totals = %v, want %v", got, want)
	}
}

func TestCodexTotalCountsTheSkillsIntroOnce(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.CodexSkills(), "review") // 23 bytes: 6
	machine.Skill(machine.CodexSkills(), "docs")   // 19 bytes: 5

	want := map[equip.Agent]int{equip.ClaudeCode: 0, equip.Codex: 711}
	if got := totalTokens(open(t, machine, machine.Root)); !maps.Equal(got, want) {
		t.Errorf("Totals = %v, want %v", got, want)
	}
}

func TestRowShowsTheHigherOfTheAgentsCosts(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	// "review" and "The review skill.": 23 bytes, 8 in Claude Code and 6 in Codex.
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.CodexSkills(), "review")
	session := newSession(t, machine, machine.Root)

	if got := row(t, session.View(), "review").Cost; got != 8 {
		t.Errorf("on: Cost = %d, want 8", got)
	}

	session.SetState("review", equip.Off)

	if got := row(t, session.View(), "review").Cost; got != 6 {
		t.Errorf("off: Cost = %d, want 6", got)
	}
}

func TestCodexSkillCostCountsEveryLocation(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	// Codex lists both: 3 + 29 = 32 bytes, and 3 + 13 = 16 bytes.
	writeSkill(t, machine.CodexSkills(), strings.Repeat("x", 29))
	writeSkill(t, filepath.Join(repo, ".agents", "skills"), strings.Repeat("x", 13))

	if got := row(t, open(t, machine, repo), "abc").Cost; got != 12 {
		t.Errorf("Cost = %d, want 12", got)
	}
}

func TestSkillOnlyItsNameCallsIsByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	writeFile(t, filepath.Join(machine.ClaudeSkills(), "abc", "SKILL.md"),
		"---\nname: abc\ndescription: x\ndisable-model-invocation: true\n---\n")
	machine.Skill(machine.ClaudeSkills(), "listed")
	// Codex does not read the key, so it lists this one.
	writeFile(t, filepath.Join(machine.CodexSkills(), "both", "SKILL.md"),
		"---\nname: both\ndescription: x\ndisable-model-invocation: true\n---\n")
	writeFile(t, filepath.Join(machine.ClaudeSkills(), "both", "SKILL.md"),
		"---\nname: both\ndescription: x\ndisable-model-invocation: true\n---\n")
	// Each agent's own file keeps it from calling this one.
	writeFile(t, filepath.Join(machine.ClaudeSkills(), "flagged", "SKILL.md"),
		"---\nname: flagged\ndescription: x\ndisable-model-invocation: true\n---\n")
	machine.Skill(machine.CodexSkills(), "flagged")
	writeFile(t, filepath.Join(machine.CodexSkills(), "flagged", "agents", "openai.yaml"),
		"policy:\n  allow_implicit_invocation: false\n")

	view := open(t, machine, machine.Root)
	for name, want := range map[string]bool{"abc": true, "listed": false, "both": false, "flagged": true} {
		if got := row(t, view, name).ByName; got != want {
			t.Errorf("%s: ByName = %v, want %v", name, got, want)
		}
	}
}

func TestCodexSkillThatDisallowsImplicitInvocationIsByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.CodexSkills(), "quiet")
	writeFile(t, filepath.Join(machine.CodexSkills(), "quiet", "agents", "openai.yaml"),
		"interface:\n  display_name: \"Quiet\"\npolicy:\n  allow_implicit_invocation: false\n")
	machine.Skill(machine.CodexSkills(), "loud")
	writeFile(t, filepath.Join(machine.CodexSkills(), "loud", "agents", "openai.yaml"),
		"policy:\n  allow_implicit_invocation: true\n")

	view := open(t, machine, machine.Root)
	if got := row(t, view, "quiet"); got.Cost != 0 || !got.ByName {
		t.Errorf("quiet: Cost = %d, ByName = %v, want 0 and true", got.Cost, got.ByName)
	}

	if got := row(t, view, "loud"); got.Cost == 0 || got.ByName {
		t.Errorf("loud: Cost = %d, ByName = %v, want a cost and false", got.Cost, got.ByName)
	}
}

func TestCodexListingPastTheBudgetIsOverBudgetAndKeepsEverySkill(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.SkillsAtCodexBudget(machine.CodexSkills())
	writeSkill(t, machine.CodexSkills(), "x") // 3 + 1 = 4 bytes: 1

	got := open(t, machine, machine.Root).Totals[equip.Codex]
	if !got.OverBudget || got.Tokens != 6141 {
		t.Errorf("Codex over = %v, total = %d, want true and 6141", got.OverBudget, got.Tokens)
	}
}

func TestCodexListingAtTheBudgetIsNotOverBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.SkillsAtCodexBudget(machine.CodexSkills())

	// The skills intro puts the total past the budget; it is not in the listing.
	got := open(t, machine, machine.Root).Totals[equip.Codex]
	if got.OverBudget || got.Tokens != 6140 {
		t.Errorf("Codex over = %v, total = %d, want false and 6140", got.OverBudget, got.Tokens)
	}
}

func TestPluginSkillsCountTowardTheListingBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.SkillsAtCodexBudget(machine.CodexSkills())
	machine.Skill(filepath.Join(codexPlugin(t, machine, "github@official"), "skills"), "review")

	if got := open(t, machine, machine.Root).Totals; !got[equip.Codex].OverBudget {
		t.Errorf("Totals = %v, want Codex over budget", got)
	}
}

func TestMCPServersDoNotCountTowardTheListingBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.SkillsAtCodexBudget(machine.CodexSkills())
	fakeCodexServer(t, machine) // 4 tokens
	probedRow(t, machine, repo)

	got := open(t, machine, repo).Totals[equip.Codex]
	if got.OverBudget || got.Tokens != 6144 {
		t.Errorf("Codex over = %v, total = %d, want false and 6144", got.OverBudget, got.Tokens)
	}
}

func TestPluginMCPServersDoNotCountTowardTheListingBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.SkillsAtCodexBudget(machine.CodexSkills())
	writeJSON(t, filepath.Join(codexPlugin(t, machine, "github@official"), ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{"fake": fakeConfig(t, "modern", "")},
	})

	session := newSession(t, machine, repo)

	err := session.ProbeCost("mcp:github@official:fake")()
	if err != nil {
		t.Fatal(err)
	}

	// The skills, the skills intro, the plugins block and the server's 4.
	if got := session.View().Totals[equip.Codex]; got.OverBudget || got.Tokens != 6394 {
		t.Errorf("Codex over = %v, total = %d, want false and 6394", got.OverBudget, got.Tokens)
	}
}

func TestClaudeCodeTotalIsNeverOverBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.SkillsAtCodexBudget(machine.ClaudeSkills())
	writeSkill(t, machine.ClaudeSkills(), "x")

	if got := open(t, machine, machine.Root).Totals; got[equip.ClaudeCode].OverBudget {
		t.Errorf("Totals = %v, want Claude Code not over budget", got)
	}
}
