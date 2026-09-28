package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

// presetModel is a TUI over equiptest.WithPresets.
func presetModel(t *testing.T) *model {
	t.Helper()

	return newModel(t, equiptest.WithPresets(t))
}

// topLine is the first line of the view.
func topLine(tui *model) string {
	top, _, _ := strings.Cut(plain(tui), "\n")

	return top
}

// writeCodexOverBudget adds Codex skills at the listing budget to machine, and
// the Codex plugin github@official, whose skill puts them past it, on in the
// trusted machine.Root.
func writeCodexOverBudget(machine *equiptest.Machine) {
	machine.SkillsAtCodexBudget(machine.CodexSkills())
	machine.Skill(filepath.Join(machine.CodexPlugin("github@official"), "skills"), "review")
	machine.WriteFile(machine.CodexConfig(),
		"[plugins.\"github@official\"]\nenabled = true\n[projects.\""+machine.Root+"\"]\ntrust_level = \"trusted\"\n")
}

func TestWorkspaceTopLineMarksTheSideOverBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	writeCodexOverBudget(machine)
	tui := newModel(t, machine)

	// Ruby turns the plugin off: its skill, 8, and the plugins block, 250.
	press(tui, key('p'), key(' '))

	if top := topLine(tui); !strings.Contains(top, "Codex ~6398, skills over budget → ~6140") {
		t.Errorf("top line %q does not mark the total before over budget", top)
	}

	press(tui, esc(), key('s'), key('p'), key(' '))

	if top := topLine(tui); !strings.Contains(top, "Codex ~6140 → ~6398, skills over budget") {
		t.Errorf("top line %q does not mark the total after over budget", top)
	}
}

func TestWriteConfirmMarksTheSideOverBudget(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	writeCodexOverBudget(machine)
	using(t, machine, machine.Root, "r1")
	tui := newModel(t, machine)

	press(tui, key('p'), key('a'), key('/'))
	press(tui, typed("github")...)
	press(tui, enter(), key(' '), esc(), key('w'))

	if line(tui, "Codex ~6140 → ~6398, skills over budget") == "" {
		t.Errorf("confirm does not mark the total after over budget:\n%s", tui.View().Content)
	}

	// The members pane keeps lint highlighted; the plugin is two rows down.
	press(tui, key('y'), down(), down(), key(' '), key('w'))

	if line(tui, "Codex ~6398, skills over budget → ~6140") == "" {
		t.Errorf("confirm does not mark the total before over budget:\n%s", tui.View().Content)
	}
}

func TestPresetsKeyOpensTheLibrary(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'))

	for _, want := range []string{"○ Ruby", "○ Writing", "rspec", "not installed"} {
		if line(tui, want) == "" {
			t.Errorf("workspace does not show %q:\n%s", want, tui.View().Content)
		}
	}

	if top := topLine(tui); !strings.Contains(top, "none (agent defaults)") {
		t.Errorf("top line %q does not say no preset is active", top)
	}
}

func TestSpaceActivatesThePresetAndTheTopLineShowsTheEffect(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '))

	if line(tui, "● Ruby") == "" {
		t.Errorf("workspace does not check Ruby:\n%s", tui.View().Content)
	}

	top := topLine(tui)
	for _, want := range []string{"active here: Ruby", "Claude Code ~22 → ~7", "Codex ~0 → ~0", "0 on, 2 off"} {
		if !strings.Contains(top, want) {
			t.Errorf("top line %q does not show %q", top, want)
		}
	}

	press(tui, tea.KeyPressMsg{Code: tea.KeyEscape})

	if top := topLine(tui); !strings.Contains(top, "presets Ruby") {
		t.Errorf("main top line %q does not show the active preset", top)
	}
}

func TestDetailPaneShowsThePresetsAsTheOrigin(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '), tea.KeyPressMsg{Code: tea.KeyEscape})

	if line(tui, "Origin  presets") == "" {
		t.Errorf("view does not show the presets origin:\n%s", tui.View().Content)
	}

	press(tui, key('1'))

	if line(tui, "without it: off (presets)") == "" {
		t.Errorf("view does not show the presets' state as the fallback:\n%s", tui.View().Content)
	}
}

func TestWorkspaceComparesWithTheViewAtItsOpening(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '), tea.KeyPressMsg{Code: tea.KeyEscape}, key('p'))

	if top := topLine(tui); strings.Contains(top, "→") {
		t.Errorf("top line %q shows a change since opening", top)
	}
}

func TestMissingPresetShowsMissingAndCanOnlyBeToggled(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	old := filepath.Join(machine.ConfigHome, "equip", "presets", "Old.toml")
	machine.WriteFile(old, `id = "o1"`)

	session, err := equip.Open(machine.Machine, machine.Root)
	if err != nil {
		t.Fatal(err)
	}

	session.SetPresets([]string{"o1"})

	_, err = session.Save()
	if err == nil {
		err = os.Remove(old)
	}

	if err != nil {
		t.Fatal(err)
	}

	tui := newModel(t, machine)
	press(tui, key('p'), key('d'))

	if got := line(tui, "● Old"); !strings.Contains(got, "missing") {
		t.Errorf("Old line %q, want it checked and missing:\n%s", got, tui.View().Content)
	}

	if line(tui, "preset Old missing: sync its file") == "" || line(tui, "Delete preset Old?") != "" {
		t.Errorf("d on a missing preset did not refuse:\n%s", tui.View().Content)
	}

	press(tui, key(' '))

	if got := line(tui, "○ Old"); !strings.Contains(got, "missing") {
		t.Errorf("space on a missing preset did not uncheck it:\n%s", tui.View().Content)
	}
}

func TestDetailPaneNotesAPresetChangedOutside(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)

	session, err := equip.Open(machine.Machine, machine.Root)
	if err != nil {
		t.Fatal(err)
	}

	session.SetPresets([]string{"r1"})

	_, err = session.Save()
	if err != nil {
		t.Fatal(err)
	}

	machine.Preset("Ruby", "id = \"r1\"\nskills = [\"docs\", \"lint\"]\n")

	if tui := newModel(t, machine); line(tui, "preset Ruby changed outside equip") == "" {
		t.Errorf("detail pane of docs does not show the note:\n%s", tui.View().Content)
	}
}

func TestSpaceRemovesOnlyTheHighlightedPreset(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '), down(), key(' '), key('k'), key(' '))

	if line(tui, "○ Ruby") == "" || line(tui, "● Writing") == "" {
		t.Errorf("want only Writing active:\n%s", tui.View().Content)
	}
}

// typed are the key presses that type text.
func typed(text string) []tea.KeyPressMsg {
	keys := make([]tea.KeyPressMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, key(r))
	}

	return keys
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func tab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab} }

func esc() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

func TestWorkspaceFitsTheTerminalAndScrollsTheMembers(t *testing.T) {
	t.Parallel()
	machine := withSkills(t, 30)
	machine.Preset("All", `id = "a1"`+"\nskills = [\"s"+strings.Join(names(30), `", "s`)+"\"]\n")
	tui := newModel(t, machine)
	resize(tui, 80, 20)

	press(tui, key('p'), tab())

	for range 29 {
		press(tui, down())
	}

	if why := fits(tui, 80, 20); why != "" {
		t.Errorf("workspace does not fit 80x20: %s\n%s", why, tui.View().Content)
	}

	if line(tui, "▸ ●   s29") == "" {
		t.Errorf("highlighted s29 is off screen:\n%s", tui.View().Content)
	}

	lines := strings.Split(styleCodes.ReplaceAllString(tui.View().Content, ""), "\n")
	if !strings.Contains(lines[len(lines)-1], "esc  back") {
		t.Errorf("last line %q does not show esc back", lines[len(lines)-1])
	}
}

func TestTabShiftTabEnterHAndLMoveTheKeysBetweenTheLibraryAndTheMembers(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'))

	for _, step := range []struct {
		key       tea.KeyPressMsg
		inMembers bool
	}{
		{tab(), true},
		{tab(), false},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, true},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, false},
		{enter(), true},
		{key('h'), false},
		{key('l'), true},
	} {
		press(tui, step.key)

		if tui.ws.inMembers != step.inMembers {
			t.Fatalf("%s: inMembers = %v, want %v", step.key, tui.ws.inMembers, step.inMembers)
		}
	}
}

func TestLeftColumnKeepsItsWidthAcrossScreens(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	// leftWidth is the width of the first pane, on the view's first border
	// line.
	leftWidth := func() int {
		top := strings.Split(plain(tui), "\n")[lineIndex(tui, 0, "╭")]
		before, _, _ := strings.Cut(top, "╮")

		return lipgloss.Width(before) + 1
	}

	resize(tui, 160, 30)

	main := leftWidth()

	for _, width := range []int{160, 80} {
		resize(tui, width, 30)
		press(tui, key('p'))

		if got := leftWidth(); got != main {
			t.Errorf("library at width %d is %d cells, want the sidebar's %d", width, got, main)
		}

		press(tui, esc())
	}
}

// names are the numbers 00 to count-1, two digits each.
func names(count int) []string {
	out := make([]string, 0, count)
	for i := range count {
		out = append(out, fmt.Sprintf("%02d", i))
	}

	return out
}

func TestNewPresetIsWrittenThenOfferedAsActiveHere(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('n'))
	press(tui, typed("Docs")...)
	press(tui, enter(), key('a'), key(' '), esc())

	if line(tui, "Docs*") == "" || line(tui, "+ docs") == "" {
		t.Errorf("workspace does not show Docs with docs added, unwritten:\n%s", tui.View().Content)
	}

	press(tui, key('w'))

	if line(tui, "Create preset Docs?") == "" {
		t.Errorf("w does not ask to create Docs:\n%s", tui.View().Content)
	}

	press(tui, key('y'))

	if line(tui, "Make it active here? y/n") == "" {
		t.Errorf("the write does not offer to make Docs active:\n%s", tui.View().Content)
	}

	press(tui, key('y'))

	if line(tui, "● Docs") == "" || !strings.Contains(topLine(tui), "active here: Docs") {
		t.Errorf("Docs is not active here:\n%s", tui.View().Content)
	}
}

func TestMovingOffAPresetWithUnwrittenEditsAsksToWriteOrDiscard(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), tab(), key(' '), tab(), down())

	if line(tui, "Ruby has unwritten edits") == "" || line(tui, "Members of Ruby") == "" {
		t.Errorf("moving off Ruby does not ask first:\n%s", tui.View().Content)
	}

	press(tui, key('d'))

	if line(tui, "Members of Writing") == "" || line(tui, "Ruby*") != "" || line(tui, "○ Ruby ") == "" {
		t.Errorf("d does not discard Ruby's edit and move on:\n%s", tui.View().Content)
	}

	press(tui, tab(), key(' '), esc())

	if !tui.ws.open || line(tui, "Writing has unwritten edits") == "" {
		t.Errorf("esc leaves with unwritten edits:\n%s", tui.View().Content)
	}

	press(tui, key('w'), key('y'))

	if tui.ws.open || tui.s.Unwritten() {
		t.Errorf("w then y does not write Writing and go back:\n%s", tui.View().Content)
	}
}

// ruby is a TUI over equiptest.WithPresets with Ruby saved active in the
// project, and the workspace open.
func ruby(t *testing.T) (*model, *equiptest.Machine) {
	t.Helper()
	machine := equiptest.WithPresets(t)
	using(t, machine, machine.Root, "r1")

	tui := newModel(t, machine)
	press(tui, key('p'))

	return tui, machine
}

// using opens dir on machine and saves the presets with ids active there.
func using(t *testing.T, machine *equiptest.Machine, dir string, ids ...string) {
	t.Helper()

	session, err := equip.Open(machine.Machine, dir)
	if err == nil {
		session.SetPresets(ids)
		_, err = session.Save()
	}

	if err != nil {
		t.Fatal(err)
	}
}

func TestOthersListsWhatTurnsInEachProjectAndWhyOneIsSkipped(t *testing.T) {
	t.Parallel()

	var tui model

	tui.style = newStyles()

	var review, lint equip.Row

	review.Name, review.State = "review", equip.On
	lint.Name, lint.State = "lint", equip.Off

	lines := tui.others([]equip.Affected{
		{Path: "/other", Skipped: "", Turned: []equip.Row{review, lint}},
		{Path: "/edited", Skipped: "changed outside equip", Turned: nil},
	})

	got := styleCodes.ReplaceAllString(strings.Join(lines, "\n"), "")
	want := "Other projects\n  /other\n    + review\n    - lint\n  /edited  skipped: changed outside equip"

	if got != want {
		t.Errorf("others =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteConfirmShowsTheChangesHereAndNCancels(t *testing.T) {
	t.Parallel()
	tui, machine := ruby(t)

	// The add list offers docs, then review.
	press(tui, key('a'), down(), key(' '), esc(), key('w'))

	for _, want := range []string{"Write preset Ruby?", "○ → ● review", "Claude Code ~7 → ~15"} {
		if line(tui, want) == "" {
			t.Errorf("confirm does not show %q:\n%s", want, tui.View().Content)
		}
	}

	press(tui, key('n'))

	file := filepath.Join(machine.ConfigHome, "equip", "presets", "Ruby.toml")
	if data, _ := os.ReadFile(file); strings.Contains(string(data), "review") || line(tui, "Ruby*") == "" {
		t.Errorf("n wrote Ruby or dropped the edit:\n%s", data)
	}

	press(tui, key('w'), key('y'))

	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "review") {
		t.Errorf("y did not write Ruby:\n%s", data)
	}
	// The write saved review on, so nothing is left unsaved.
	if top := topLine(tui); strings.Contains(top, "unsaved") {
		t.Errorf("top line %q shows the written change as unsaved", top)
	}
}

func TestWriteOfAPresetChangedOutsideSaysTheEditsMergedIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	tui := newModel(t, machine)

	press(tui, key('p'), tab(), key(' '))
	machine.Preset("Ruby", "id = \"r1\"\nskills = [\"docs\", \"lint\", \"rspec\"]\n")
	press(tui, key('w'), key('y'))

	if line(tui, "preset changed outside equip since open: merged into the edits, w writes them") == "" ||
		line(tui, "+ docs") != "" || line(tui, "Ruby*") == "" {
		t.Errorf("w does not show the merged edits:\n%s", tui.View().Content)
	}
}

func TestWriteOfAPresetDeletedOutsideSaysWWritesItAgain(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	tui := newModel(t, machine)

	press(tui, key('p'), tab(), key(' '))

	err := os.Remove(filepath.Join(machine.ConfigHome, "equip", "presets", "Ruby.toml"))
	if err != nil {
		t.Fatal(err)
	}

	press(tui, key('w'), key('y'))

	if line(tui, "preset file deleted outside equip since open: w writes it again") == "" {
		t.Errorf("w does not say the file was deleted:\n%s", tui.View().Content)
	}
}

func TestDKeyDeletesThePresetAfterAConfirm(t *testing.T) {
	t.Parallel()
	tui, machine := ruby(t)
	file := filepath.Join(machine.ConfigHome, "equip", "presets", "Ruby.toml")

	press(tui, key('d'))

	for _, want := range []string{"Delete preset Ruby?", "no other project uses it", "○ → ● review"} {
		if line(tui, want) == "" {
			t.Errorf("confirm does not show %q:\n%s", want, tui.View().Content)
		}
	}

	press(tui, key('n'))

	_, err := os.Stat(file)
	if err != nil {
		t.Errorf("n deleted Ruby: %v", err)
	}

	press(tui, key('d'), key('y'))

	_, err = os.Stat(file)
	if err == nil || line(tui, "Ruby") != "" {
		t.Errorf("y did not delete Ruby:\n%s", tui.View().Content)
	}

	if top := topLine(tui); !strings.Contains(top, "none (agent defaults)") || strings.Contains(top, "unsaved") {
		t.Errorf("top line %q, want no preset active and nothing unsaved", top)
	}
}

func TestDeleteShowsTheError(t *testing.T) {
	t.Parallel()
	tui, machine := ruby(t)
	machine.WriteFile(filepath.Join(machine.Root, ".claude", "settings.local.json"), `{"skillOverrides": {"docs": "on"}}`)

	press(tui, key('d'), key('y'))

	if line(tui, "delete failed: changed outside equip since open") == "" || line(tui, "● Ruby") == "" {
		t.Errorf("view does not show the failed delete:\n%s", tui.View().Content)
	}
}

func TestDKeyDropsAPresetNotWrittenYetAtOnce(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('n'))
	press(tui, typed("Docs")...)
	press(tui, enter(), key('d'))

	if line(tui, "Docs") != "" || tui.s.Unwritten() {
		t.Errorf("d does not drop Docs:\n%s", tui.View().Content)
	}
}

func TestMembersPaneMarksEditsAndOverrides(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)
	tui.s.SetState("lint", equip.ManualOnly)

	press(tui, key('p'), key('a'), key(' '), esc(), down(), key(' '))

	if line(tui, "+ docs") == "" {
		t.Errorf("members pane does not mark docs added:\n%s", tui.View().Content)
	}

	// lint has an Override, and is removed: marked and struck through.
	at := lineIndex(tui, 0, "- lint ovr")
	if at < 0 || !strings.Contains(strings.Split(tui.View().Content, "\n")[at], "\x1b[9m") {
		t.Errorf("members pane does not strike lint through:\n%s", tui.View().Content)
	}
}

func TestXInTheWorkspaceKeepsTheOverride(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)
	tui.s.SetState("lint", equip.ManualOnly)

	press(tui, key('p'), key('x'), tab(), key('x'))

	if line(tui, "lint ovr") == "" {
		t.Errorf("x dropped lint's Override in the workspace:\n%s", tui.View().Content)
	}
}

func TestRightPaneShowsTheUsersOrTheHighlightedExtensionsPresets(t *testing.T) {
	t.Parallel()
	tui, machine := ruby(t)

	if line(tui, "Used by 1 projects") == "" || line(tui, machine.Root+" (here)") == "" {
		t.Errorf("right pane does not show the project using Ruby:\n%s", tui.View().Content)
	}

	press(tui, tab())

	if line(tui, "Presets  Ruby (active)") == "" || line(tui, "Origin  presets") == "" {
		t.Errorf("right pane does not show lint's detail:\n%s", tui.View().Content)
	}

	press(tui, tab(), down(), tab())

	if docs := line(tui, "Presets  Writing"); docs == "" || strings.Contains(docs, "(active)") {
		t.Errorf("right pane does not show docs in Writing, not active:\n%s", tui.View().Content)
	}
}

func TestRenameAppliesAtOnce(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('r'))
	press(tui, slices.Repeat([]tea.KeyPressMsg{{Code: tea.KeyBackspace}}, 4)...)
	press(tui, typed("Rails")...)
	press(tui, enter())

	if line(tui, "○ Rails") == "" || line(tui, "Rails*") != "" || line(tui, "Members of Rails") == "" {
		t.Errorf("r does not rename Ruby to Rails at once:\n%s", tui.View().Content)
	}
}

// inOrder reports whether the view shows, from the line with first on, each
// of want below the one before it.
func inOrder(tui *model, first string, want ...string) bool {
	index := lineIndex(tui, 0, first)
	if index < 0 {
		return false
	}

	for _, s := range want {
		index = lineIndex(tui, index+1, s)
		if index < 0 {
			return false
		}
	}

	return true
}

func TestMembersAndTheAddListAreGroupedByKind(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	machine.Plugin("github@official", "user", "")
	machine.Plugin("atlas@official", "user", "")
	machine.Preset("Ruby", "id = \"r1\"\nskills = [\"lint\"]\nplugins = [\"github@official\"]\n")
	tui := newModel(t, machine)

	press(tui, key('p'))

	if !inOrder(tui, "Members of Ruby", "skill", "lint", "plugin", "github@official") {
		t.Errorf("members are not grouped by kind:\n%s", tui.View().Content)
	}

	press(tui, key('a'))

	if !inOrder(tui, "Add to Ruby", "skill", "docs", "review", "plugin", "atlas@official") {
		t.Errorf("the add list is not grouped by kind:\n%s", tui.View().Content)
	}
}

func TestAddListLeavesOutAPluginsSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	machine.Skill(filepath.Join(machine.Plugin("atlas@official", "user", ""), "skills"), "mapper")
	tui := newModel(t, machine)

	press(tui, key('p'), key('a'))

	if line(tui, "atlas@official") == "" || line(tui, "mapper") != "" {
		t.Errorf("the add list does not offer the plugin alone:\n%s", tui.View().Content)
	}
}

func TestAddListSearchesOnRequest(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('a'), key('/'))
	press(tui, typed("rev")...)

	if line(tui, "○ docs") != "" || line(tui, "● docs") != "" || line(tui, "review") == "" {
		t.Errorf("the search does not keep only review:\n%s", tui.View().Content)
	}
}

func TestANewPresetIsWrittenBeforeItCanBeActive(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('n'))
	press(tui, typed("Docs")...)
	press(tui, enter(), key(' '))

	if line(tui, "write the new preset first") == "" || line(tui, "○ Docs") == "" {
		t.Errorf("space makes a new preset active:\n%s", tui.View().Content)
	}
}

func TestNewAsksFirstWithUnwrittenEditsAndEscCancelsTheName(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('n'), esc())

	if line(tui, "name:") != "" || tui.ws.naming != "" {
		t.Errorf("esc does not cancel the name:\n%s", tui.View().Content)
	}

	press(tui, tab(), key(' '), key('n'))

	if line(tui, "Ruby has unwritten edits") == "" {
		t.Errorf("n does not ask first:\n%s", tui.View().Content)
	}
}

func TestWriteWithNoUnwrittenEditsSaysSo(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key('w'))

	if line(tui, "no unwritten edits in Ruby") == "" || line(tui, "Write preset Ruby?") != "" {
		t.Errorf("w asks to write a preset with no edits:\n%s", tui.View().Content)
	}
}

func TestSpaceAddsBackAMemberItRemoved(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), tab(), key(' '), key(' '))

	if line(tui, "Ruby*") != "" || line(tui, "- ") != "" {
		t.Errorf("space does not add lint back:\n%s", tui.View().Content)
	}
}

func TestPresetsWithNoPresetSayWhatAPresetIs(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 2))

	press(tui, key('p'))

	if line(tui, "A preset is") == "" {
		t.Errorf("empty library does not say what a preset is:\n%s", plain(tui))
	}
}

func TestQuitAsksWithUnwrittenPresetEdits(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	cmd := press(tui, key('p'), tab(), key(' '), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if quits(cmd) || line(tui, "Quit without saving?") == "" {
		t.Errorf("ctrl+c did not ask first:\n%s", tui.View().Content)
	}
}
