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

func TestSpaceOnAnActivePresetRemovesIt(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), down(), key(' '), key(' '))

	if line(tui, "[ ] Writing") == "" || strings.Contains(topLine(tui), "→") {
		t.Errorf("Writing is still active:\n%s", tui.View().Content)
	}
}
