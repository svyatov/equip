package equip_test

import (
	"path/filepath"
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func TestCodexOnlySkillIsNotApplied(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.CodexSkills(), "codex-only")

	if got := row(t, open(t, machine, machine.Root), "codex-only"); !got.NotApplied {
		t.Errorf("row = %+v, want it not applied", got)
	}
}

func TestNotAppliedFacetKeepsTheRowsNoAgentApplies(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.CodexSkills(), "codex-only")
	machine.Skill(machine.ClaudeSkills(), "claude-only")
	session := newSession(t, machine, machine.Root)
	session.SetState("codex-only", equip.Off)
	session.SetState("claude-only", equip.Off)

	wantCounts(t, session.View(), map[string]int{"Not applied": 1, "Off": 2, "Unsaved changes": 2})
}

func TestSkillClaudeCodeAppliesIsNotMarkedButTheDetailSaysCodexDoesNot(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "both")
	machine.Skill(machine.CodexSkills(), "both")
	session := newSession(t, machine, machine.Root)

	if got := row(t, session.View(), "both"); got.NotApplied {
		t.Errorf("row = %+v, want it applied", got)
	}

	if got := session.Detail("both").NotApplied; got[equip.Codex] == "" {
		t.Errorf("NotApplied = %q, want a reason for Codex", got)
	}
}

func TestCodexOnlyPluginAndMCPServerAreNotAppliedInAnUntrustedProject(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(filepath.Join(codexPlugin(t, machine, "github@official"), "skills"), "review")
	appendFile(t, machine.CodexConfig(), "[mcp_servers.db]\ncommand = \"db\"\n")
	view := open(t, machine, repo)

	for _, name := range []string{"github@official", "review", "db"} {
		if got := row(t, view, name); !got.NotApplied {
			t.Errorf("row = %+v, want it not applied", got)
		}
	}
}

func TestClaudeOnlySkillIsNotAppliedWithATrackedSettingsFile(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	writeFile(t, settingsLocal(repo), `{}`)
	machine.RunGit(repo, "add", ".claude/settings.local.json")

	if got := row(t, open(t, machine, repo), "review"); !got.NotApplied {
		t.Errorf("row = %+v, want it not applied", got)
	}
}
