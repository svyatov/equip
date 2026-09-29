package equip_test

import (
	"path/filepath"
	"slices"
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

func TestFacetsGroupIntoKindsAgentsStatesAndChanges(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	var got []string

	for _, f := range open(t, machine, machine.Root).Facets {
		if f.NewGroup {
			got = append(got, f.Name)
		}
	}

	if want := []string{"Claude Code", "On", "Overrides"}; !slices.Equal(got, want) {
		t.Errorf("facets that start a group = %q, want %q", got, want)
	}
}

func TestFacetsCountRowsByKind(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.ClaudeSkills(), "lint")
	machine.Plugin("github@official", "user", "")
	writeFile(t, claudeJSON(machine), `{"mcpServers": {"db": {"command": "db"}}}`)

	writeFile(t, filepath.Join(machine.ClaudeSkills(), "ship", "SKILL.md"),
		"---\nname: ship\ndescription: x\ndisable-model-invocation: true\n---\n")

	wantCounts(t, open(t, machine, machine.Root),
		map[string]int{"All": 5, "Skills": 3, "Plugins": 1, "MCP servers": 1, "By name": 1})
}

func TestFacetsLeaveOutAPluginsSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	skills := filepath.Join(machine.Plugin("github@official", "user", ""), "skills")
	machine.Skill(skills, "review")
	writeFile(t, filepath.Join(skills, "ship", "SKILL.md"),
		"---\nname: ship\ndescription: x\ndisable-model-invocation: true\n---\n")
	session := newSession(t, machine, machine.Root)

	wantCounts(t, session.View(), map[string]int{
		"All": 1, "Skills": 0, "Plugins": 1, "By name": 0, "Claude Code": 1, "Codex": 0,
	})

	session.SetState("github@official", equip.Off)

	wantCounts(t, session.View(), map[string]int{"Overrides": 1, "Off": 1})
}

func TestFacetsCountRowsEachAgentHas(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.CodexSkills(), "notes")
	machine.Skill(machine.CodexSkills(), "lint")
	machine.Skill(machine.ClaudeSkills(), "shared")
	machine.Skill(machine.CodexSkills(), "shared")

	wantCounts(t, open(t, machine, machine.Root), map[string]int{"All": 4, "Claude Code": 2, "Codex": 3})
}

func TestFacetsCountRowsByState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	for _, name := range []string{"review", "lint", "notes", "docs"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	s := newSession(t, machine, machine.Root)
	s.SetState("lint", equip.ManualOnly)
	s.SetState("notes", equip.Off)
	s.SetState("docs", equip.Off)

	wantCounts(t, s.View(), map[string]int{"On": 1, "Manual-only": 1, "Off": 2})
}

func TestFacetsCountOverridesAndUnsavedChanges(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	for _, name := range []string{"review", "lint", "docs"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	s := newSession(t, machine, machine.Root)
	s.SetState("lint", equip.Off)
	save(t, s)
	s.SetState("docs", equip.ManualOnly)

	wantCounts(t, s.View(), map[string]int{"Overrides": 2, "Unsaved changes": 1})
}

func TestOverridesFacetKeepsAPluginWhoseMCPServerHasAnOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	pluginServer(t, machine)

	s := newSession(t, machine, machine.Root)
	s.SetState(search(t, s).Key, equip.Off)

	wantCounts(t, s.View(), map[string]int{"Overrides": 1})
}
