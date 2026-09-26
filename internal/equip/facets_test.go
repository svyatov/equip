package equip_test

import (
	"testing"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// counts are the facets of v by name, with their counts.
func counts(v equip.View) map[string]int {
	out := map[string]int{}
	for _, f := range v.Facets {
		out[f.Name] = f.Count()
	}

	return out
}

// wantCounts reports the facets of v whose counts differ from want.
func wantCounts(t *testing.T, v equip.View, want map[string]int) {
	t.Helper()

	got := counts(v)
	for name, n := range want {
		if got[name] != n {
			t.Errorf("facet %q count = %d, want %d (all: %v)", name, got[name], n, got)
		}
	}
}

func TestFacetsCountRowsByKind(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.ClaudeSkills(), "lint")
	machine.Plugin("github@official", "user", "")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"db": {"command": "db"}}}`)

	wantCounts(t, open(t, machine, repo), map[string]int{"All": 4, "Skills": 2, "Plugins": 1, "MCP servers": 1})
}

func TestFacetsCountRowsOnlyOneAgentHas(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.CodexSkills(), "notes")
	machine.Skill(machine.CodexSkills(), "lint")
	machine.Skill(machine.ClaudeSkills(), "shared")
	machine.Skill(machine.CodexSkills(), "shared")

	wantCounts(t, open(t, machine, repo), map[string]int{"All": 4, "Claude Code only": 1, "Codex only": 2})
}

func TestFacetsCountRowsByState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")

	for _, name := range []string{"review", "lint", "notes", "docs"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	s := newSession(t, machine, repo)
	s.SetState("lint", equip.ManualOnly)
	s.SetState("notes", equip.Off)
	s.SetState("docs", equip.Off)

	wantCounts(t, s.View(), map[string]int{"On": 1, "Manual-only": 1, "Off": 2})
}

func TestFacetsCountOverridesAndUnsavedChanges(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")

	for _, name := range []string{"review", "lint", "docs"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	s := newSession(t, machine, repo)
	s.SetState("lint", equip.Off)
	save(t, s)
	s.SetState("docs", equip.ManualOnly)

	wantCounts(t, s.View(), map[string]int{"Overrides": 2, "Unsaved changes": 1})
}

func TestOverridesFacetKeepsAPluginWhoseMCPServerHasAnOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	repo := machine.Repo("app")
	pluginServer(t, machine)

	s := newSession(t, machine, repo)
	s.SetState(search(t, s).Key, equip.Off)

	wantCounts(t, s.View(), map[string]int{"Overrides": 1})
}
