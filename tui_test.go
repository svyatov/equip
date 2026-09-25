package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func newModel(t *testing.T, machine *equiptest.Machine) *model {
	t.Helper()

	session, err := equip.Open(machine.Machine, machine.Root)
	if err != nil {
		t.Fatal(err)
	}

	return newTUI(session)
}

// press feeds keys to tui and returns the last command.
func press(tui *model, keys ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = tui.Update(k)
	}

	return cmd
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func down() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown} }

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}

	_, ok := cmd().(tea.QuitMsg)

	return ok
}

// line returns the first line of the view that contains s.
func line(tui *model, s string) string {
	for l := range strings.Lines(tui.View().Content) {
		if strings.Contains(l, s) {
			return l
		}
	}

	return ""
}

func TestMainScreenShowsProjectPathAndSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")

	top, list, _ := strings.Cut(newModel(t, machine).View().Content, "\n")
	if !strings.Contains(top, machine.Root) {
		t.Errorf("top line %q does not show the Project path %q", top, machine.Root)
	}

	if !strings.Contains(list, "review") {
		t.Errorf("list %q does not show the skill", list)
	}
}

func TestStateKeySetsAnUnsavedOverrideOnTheHighlightedSkill(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "alpha")
	machine.Skill(machine.ClaudeSkills(), "beta")
	tui := newModel(t, machine)

	press(tui, down(), key('3'))

	if r := tui.s.View().Rows[1]; r.State != equip.Off || !r.Override {
		t.Errorf("beta = %+v, want an Override off", r)
	}

	top, _, _ := strings.Cut(tui.View().Content, "\n")
	if !strings.Contains(top, "1 unsaved") {
		t.Errorf("top line %q does not count 1 unsaved", top)
	}

	if strings.Count(tui.View().Content, "*") != 1 || strings.Contains(line(tui, "alpha"), "*") {
		t.Errorf("unsaved marker on the wrong row:\n%s", tui.View().Content)
	}
}

func TestDetailPaneShowsStatesOriginAndFallback(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")

	tui := newModel(t, machine)
	if line(tui, "Origin  default") == "" {
		t.Errorf("view does not show the default origin:\n%s", tui.View().Content)
	}

	press(tui, key('3'))

	for _, want := range []string{
		"( ) 1 on", "( ) 2 manual-only", "(○) 3 off", "set by hand here", "without it: on (default)",
	} {
		if line(tui, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, tui.View().Content)
		}
	}
}

func TestStateKeysPickFromThePluginsOnAndOff(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	press(tui, key('2'))

	if r := tui.s.View().Rows[0]; r.State != equip.Off || !r.Override {
		t.Errorf("github@official = %+v, want an Override off", r)
	}

	for _, want := range []string{"( ) 1 on", "(○) 2 off"} {
		if line(tui, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, tui.View().Content)
		}
	}
}

func TestThirdStateKeyLeavesAPluginAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	press(tui, key('3'))

	if r := tui.s.View().Rows[0]; r.State != equip.On || r.Override {
		t.Errorf("github@official = %+v, want on with no Override", r)
	}
}

func TestDetailPaneShowsThePluginsMarketplace(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	if line(tui, "Marketplace  official") == "" {
		t.Errorf("view does not show the Marketplace:\n%s", tui.View().Content)
	}

	press(tui, down())

	for _, pluginOnly := range []string{"Marketplace  ", "Contents  "} {
		if line(tui, pluginOnly) != "" {
			t.Errorf("a skill shows %q:\n%s", pluginOnly, tui.View().Content)
		}
	}
}

func TestDetailPaneMarksAPluginWithHooks(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, "hooks", "hooks.json"), `{"hooks": {}}`)

	if got := line(newModel(t, machine), "Cost  "); !strings.Contains(got, "Claude Code ~0 + hook output, unknown") {
		t.Errorf("cost line %q does not mark the hooks", got)
	}
}

func TestDetailPaneListsThePluginsContents(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(dir, "skills"), "review") // "github:review" and "The review skill.": 10
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	tui := newModel(t, machine)

	press(tui, key('2'))

	if got := line(tui, "○ skill review ~0"); !strings.Contains(got, "The review skill.") {
		t.Errorf("skill line %q does not show the skill's state, cost and description", got)
	}

	if line(tui, "○ MCP server search unknown") == "" {
		t.Errorf("view does not show the MCP server:\n%s", tui.View().Content)
	}
}

func TestDetailPaneCutsAPluginSkillsDescriptionShort(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	long := strings.Repeat("word ", 20) + "end"
	machine.WriteFile(filepath.Join(machine.Plugin("github@official", "user", ""), "skills", "review", "SKILL.md"),
		"---\nname: review\ndescription: |\n  "+long+"\n  second line\n---\n")
	tui := newModel(t, machine)

	got := line(tui, "skill review")
	if !strings.Contains(got, "word") || strings.Contains(got, "end") {
		t.Errorf("skill line %q does not cut the description short", got)
	}

	if line(tui, "second line") != "" {
		t.Errorf("view shows the description's second line:\n%s", tui.View().Content)
	}
}

func TestDetailPaneShowsTheNote(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	press(newModel(t, machine), key('3'), key('s'))

	settings := filepath.Join(machine.Root, ".claude", "settings.local.json")

	err := os.WriteFile(settings, []byte(`{"skillOverrides": {"review": "on"}}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	if tui := newModel(t, machine); line(tui, "changed outside equip in Claude Code") == "" {
		t.Errorf("view does not show the note:\n%s", tui.View().Content)
	}
}

func TestDropKeyDropsTheOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, key('3'), key('x'))

	if r := tui.s.View().Rows[0]; r.Override {
		t.Errorf("review = %+v, want no Override", r)
	}
}

func TestSaveKeyWritesTheOverrides(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, key('3'), key('s'))

	if n := tui.s.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d after s, want 0", n)
	}
}

func TestSaveKeyShowsTheError(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	settings := filepath.Join(machine.Root, ".claude", "settings.local.json")
	machine.Mkdir(filepath.Dir(settings))

	err := os.WriteFile(settings, []byte("{"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	tui := newModel(t, machine)

	press(tui, key('3'), key('s'))

	if line(tui, "settings.local.json") == "" {
		t.Errorf("view does not show the save error:\n%s", tui.View().Content)
	}

	press(tui, down())

	if line(tui, "settings.local.json") != "" {
		t.Error("the save error stays after the next key")
	}
}

func TestUpKeyMovesBack(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "alpha")
	machine.Skill(machine.ClaudeSkills(), "beta")
	tui := newModel(t, machine)

	press(tui, down(), tea.KeyPressMsg{Code: tea.KeyUp}, key('3'))

	if r := tui.s.View().Rows[0]; !r.Override {
		t.Errorf("alpha = %+v, want an Override", r)
	}
}

func TestKeysWithNoSkillsDoNothing(t *testing.T) {
	t.Parallel()
	tui := newModel(t, equiptest.New(t))

	press(tui, down(), key('1'), key('x'))

	if n := tui.s.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d, want 0", n)
	}
}

func TestQuitKeysQuit(t *testing.T) {
	t.Parallel()

	machine := equiptest.New(t)
	for _, k := range []tea.KeyPressMsg{
		key('q'),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		if !quits(press(newModel(t, machine), k)) {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestQuitWithUnsavedChangesAsksFirst(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	if quits(press(tui, key('3'), key('q'))) {
		t.Fatal("q quit with unsaved changes")
	}

	if line(tui, "Quit without saving? y/n") == "" {
		t.Errorf("view does not ask:\n%s", tui.View().Content)
	}

	if quits(press(tui, key('n'))) || line(tui, "Quit without saving?") != "" {
		t.Error("n did not cancel the quit")
	}

	if !quits(press(tui, key('q'), key('y'))) {
		t.Error("y did not quit")
	}
}

func TestDetailPaneShowsDescriptionAgentsLocationsAndCodexNote(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	claude := machine.Skill(machine.ClaudeSkills(), "review")
	codex := machine.Skill(filepath.Join(machine.Home, ".agents", "skills"), "review")
	tui := newModel(t, machine)

	for _, want := range []string{"The review skill.", "Claude Code, Codex", "not applied in Codex: "} {
		if line(tui, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, tui.View().Content)
		}
	}

	if got := line(tui, claude); !strings.Contains(got, "Claude Code") {
		t.Errorf("location line %q does not name Claude Code", got)
	}

	if got := line(tui, codex); !strings.Contains(got, "Codex") {
		t.Errorf("location line %q does not name Codex", got)
	}
}

func TestRowShowsItsCost(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review") // 23 bytes: 8

	if got := line(newModel(t, machine), "●"); !strings.Contains(got, "~8") {
		t.Errorf("row line %q does not show the cost ~8", got)
	}
}

func TestTopLineShowsTheTotalOfEachAgentAsStatesChange(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review") // 23 bytes: 8
	machine.Skill(machine.CodexSkills(), "review")  // 23 bytes: 6, and the intro's 700
	tui := newModel(t, machine)

	press(tui, key('3'))

	top, _, _ := strings.Cut(tui.View().Content, "\n")
	for _, want := range []string{"Claude Code ~0", "Codex ~706"} {
		if !strings.Contains(top, want) {
			t.Errorf("top line %q does not show %q", top, want)
		}
	}
}

func TestDetailPaneShowsTheCostInEachAgent(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review") // 23 bytes: 8
	machine.Skill(machine.CodexSkills(), "review")  // 23 bytes: 6

	if got := line(newModel(t, machine), "Cost  "); !strings.Contains(got, "Claude Code ~8, Codex ~6") {
		t.Errorf("cost line %q does not show both costs", got)
	}
}

func TestMCPServerRowAndDetailPaneShowTheCostAsUnknown(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)
	tui := newModel(t, machine)

	if got := line(tui, "github"); !strings.Contains(got, "unknown") {
		t.Errorf("row line %q does not show the cost as unknown", got)
	}

	if got := line(tui, "Cost  "); !strings.Contains(got, "Claude Code unknown") {
		t.Errorf("cost line %q does not show the cost as unknown", got)
	}
}

func TestStateKeysSetTheHighlightedMCPServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)
	tui := newModel(t, machine)

	press(tui, key('2'))

	if got := line(tui, "github"); !strings.Contains(got, "○") {
		t.Errorf("row line %q, want github off", got)
	}
}

func TestDetailPaneNamesTheKind(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)

	// The list and the detail pane's title share the first line.
	if got := line(newModel(t, machine), "github"); !strings.Contains(got, "MCP server") {
		t.Errorf("line %q does not name the kind", got)
	}
}

func TestDetailPaneSaysABuiltInServerIsBuiltIntoClaudeCode(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.ClaudeBuiltins = map[string]equip.State{"computer-use": equip.Off}

	if line(newModel(t, machine), "built into Claude Code") == "" {
		t.Error("detail pane does not say the server is built into Claude Code")
	}
}
