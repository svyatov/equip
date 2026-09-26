package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	top, _, _ := strings.Cut(tui.View().Content, "\n")

	return top
}

func TestPresetsKeyOpensTheLibrary(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'))

	for _, want := range []string{"[ ] Ruby", "[ ] Writing", "rspec", "not installed"} {
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

	if line(tui, "[x] Ruby") == "" {
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

func TestCtrlCInTheWorkspaceAsksBeforeDroppingUnsavedChanges(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	cmd := press(tui, key('p'), key(' '), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if quits(cmd) || line(tui, "Quit without saving?") == "" {
		t.Errorf("ctrl+c did not ask first:\n%s", tui.View().Content)
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

func TestSpaceKeepsAnActivePresetWhoseFileIsMissing(t *testing.T) {
	t.Parallel()
	machine := equiptest.WithPresets(t)
	old := filepath.Join(machine.ConfigHome, "equip", "presets", "Old.toml")
	machine.WriteFile(old, `id = "o1"`)

	session, err := equip.Open(machine.Machine, machine.Root)
	if err != nil {
		t.Fatal(err)
	}

	session.SetPresets([]string{"o1"})

	err = session.Save()
	if err == nil {
		err = os.Remove(old)
	}

	if err != nil {
		t.Fatal(err)
	}

	tui := newModel(t, machine)
	press(tui, key('p'), key(' '))

	err = tui.s.Save()
	if err != nil {
		t.Fatal(err)
	}

	files, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))
	data, _ := os.ReadFile(files[0])

	for _, want := range []string{`'o1'`, `'r1'`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("record does not keep %s active:\n%s", want, data)
		}
	}
}

func TestSpaceRemovesOnlyTheHighlightedPreset(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '), down(), key(' '), key('k'), key(' '))

	if line(tui, "[ ] Ruby") == "" || line(tui, "[x] Writing") == "" {
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

	if line(tui, "[x] Docs") == "" || !strings.Contains(topLine(tui), "active here: Docs") {
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

	if line(tui, "Members of Writing") == "" || line(tui, "Ruby*") != "" || line(tui, "[ ] Ruby ") == "" {
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
		err = session.Save()
	}

	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteConfirmListsTheOtherProjectsAndTheSkippedOnes(t *testing.T) {
	t.Parallel()
	tui, machine := ruby(t)
	other, edited := machine.Repo("other"), machine.Repo("edited")
	using(t, machine, other, "r1")
	using(t, machine, edited, "r1")
	machine.WriteFile(filepath.Join(edited, ".claude", "settings.local.json"), `{"skillOverrides": {"docs": "on"}}`)

	// The add list offers docs, then review; its esc leaves lint highlighted
	// in the members pane.
	press(tui, key('a'), down(), key(' '), esc(), key(' '), key('w'))

	for _, want := range []string{other, "    + review", "    - lint", edited + "  skipped: changed outside equip"} {
		if line(tui, want) == "" {
			t.Errorf("confirm does not show %q:\n%s", want, tui.View().Content)
		}
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

	if line(tui, "delete failed: changed outside equip since open") == "" || line(tui, "[x] Ruby") == "" {
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
	if lint := line(tui, "- lint ~0 ovr"); !strings.Contains(lint, "\x1b[9m") {
		t.Errorf("members pane does not strike lint through:\n%s", tui.View().Content)
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

	if line(tui, "[ ] Rails") == "" || line(tui, "Rails*") != "" || line(tui, "Members of Rails") == "" {
		t.Errorf("r does not rename Ruby to Rails at once:\n%s", tui.View().Content)
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

	if line(tui, "write the new preset first") == "" || line(tui, "[ ] Docs") == "" {
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

func TestQuitAsksWithUnwrittenPresetEdits(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	cmd := press(tui, key('p'), tab(), key(' '), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if quits(cmd) || line(tui, "Quit without saving?") == "" {
		t.Errorf("ctrl+c did not ask first:\n%s", tui.View().Content)
	}
}

func TestSpaceOnAnActivePresetRemovesIt(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), down(), key(' '), key(' '))

	if line(tui, "[ ] Writing") == "" || strings.Contains(topLine(tui), "→") {
		t.Errorf("Writing is still active:\n%s", tui.View().Content)
	}
}
